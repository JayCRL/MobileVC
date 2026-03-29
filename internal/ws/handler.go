package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"mobilevc/internal/logx"
	"mobilevc/internal/protocol"
	"mobilevc/internal/runner"
	runtimepkg "mobilevc/internal/runtime"
	"mobilevc/internal/session"
	"mobilevc/internal/skills"
	"mobilevc/internal/store"
)

const wsDebugPreviewLimit = 240

type Handler struct {
	AuthToken     string
	NewExecRunner func() runner.Runner
	NewPtyRunner  func() runner.Runner
	Upgrader      websocket.Upgrader
	SkillLauncher *skills.Launcher
	SessionStore  store.Store
}

func NewHandler(authToken string, sessionStore store.Store) *Handler {
	return &Handler{
		AuthToken: authToken,
		NewExecRunner: func() runner.Runner {
			return runner.NewExecRunner()
		},
		NewPtyRunner: func() runner.Runner {
			return runner.NewPtyRunner()
		},
		SkillLauncher: skills.NewLauncher(sessionStore),
		SessionStore:  sessionStore,
		Upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	connectionID := fmt.Sprintf("conn-%d", time.Now().UTC().UnixNano())
	selectedSessionID := ""
	remoteAddr := r.RemoteAddr
	connected := false
	var conn *websocket.Conn

	emitIfPossible := func(event any) {
		if conn == nil {
			return
		}
		_ = conn.WriteJSON(event)
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			stack := logx.StackTrace()
			logx.Error("ws", "serve panic recovered: connectionID=%s sessionID=%s remoteAddr=%s panic=%v\n%s", connectionID, selectedSessionID, remoteAddr, recovered, stack)
			if connected {
				emitIfPossible(protocol.NewErrorEvent(selectedSessionID, "internal server error", stack))
			}
		}
	}()

	token := r.URL.Query().Get("token")
	if token == "" || token != h.AuthToken {
		logx.Warn("ws", "reject unauthorized request: connectionID=%s remoteAddr=%s", connectionID, remoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	upgradedConn, err := h.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		logx.Error("ws", "websocket upgrade failed: connectionID=%s remoteAddr=%s err=%v", connectionID, remoteAddr, err)
		return
	}
	conn = upgradedConn
	connected = true
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	runtimeSvc := runtimepkg.NewService(selectedSessionID, runtimepkg.Dependencies{
		NewExecRunner: h.NewExecRunner,
		NewPtyRunner:  h.NewPtyRunner,
	})
	writeCh := make(chan any, 128)
	writeErrCh := make(chan error, 1)
	var writerWG sync.WaitGroup
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		defer func() {
			if recovered := recover(); recovered != nil {
				stack := logx.StackTrace()
				logx.Error("ws", "writer panic recovered: connectionID=%s sessionID=%s remoteAddr=%s panic=%v\n%s", connectionID, selectedSessionID, remoteAddr, recovered, stack)
				select {
				case writeErrCh <- fmt.Errorf("writer panic: %v", recovered):
				default:
				}
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-writeCh:
				if !ok {
					return
				}
				if err := conn.WriteJSON(event); err != nil {
					logx.Error("ws", "write websocket event failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
					select {
					case writeErrCh <- err:
					default:
					}
					return
				}
			}
		}
	}()

	logx.Info("ws", "connection established: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)

	buildProjectionSnapshotFor := func(sessionID string) store.ProjectionSnapshot {
		projection := store.ProjectionSnapshot{
			RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		}
		loaded := readProjectionFromSessionStore(h.SessionStore, ctx, sessionID, connectionID, remoteAddr)
		projection = loaded
		projection = withRuntimeSnapshot(projection, runtimeSvc)
		if diff := loaded.CurrentDiff; diff != nil {
			projection.CurrentDiff = diff
		}
		if len(projection.Diffs) == 0 && projection.CurrentDiff != nil {
			projection.Diffs = []session.DiffContext{*projection.CurrentDiff}
		}
		return projection
	}

	persistProjectionFor := func(sessionID string, snapshot store.ProjectionSnapshot) {
		if h.SessionStore == nil || strings.TrimSpace(sessionID) == "" {
			if h.SessionStore == nil {
				logx.Warn("ws", "skip projection persistence because session store is unavailable: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, sessionID, remoteAddr)
			}
			return
		}
		if _, err := h.SessionStore.SaveProjection(ctx, sessionID, snapshot); err != nil {
			logx.Error("ws", "save session projection failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
		}
	}

	emit := func(event any) {
		runtimepkg.Enqueue(ctx, writeCh, event)
	}

	switchRuntimeSession := func(sessionID string) {
		logx.Info("ws", "switch runtime session: connectionID=%s previousSessionID=%s nextSessionID=%s remoteAddr=%s", connectionID, selectedSessionID, sessionID, remoteAddr)
		runtimeSvc.Cleanup()
		selectedSessionID = sessionID
		runtimeSvc = runtimepkg.NewService(selectedSessionID, runtimepkg.Dependencies{NewExecRunner: h.NewExecRunner, NewPtyRunner: h.NewPtyRunner})
	}

	emitSessionList := func() []store.SessionSummary {
		if h.SessionStore == nil {
			logx.Warn("ws", "session list requested but session store unavailable: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
			return nil
		}
		items, err := h.SessionStore.ListSessions(ctx)
		if err != nil {
			logx.Error("ws", "list sessions failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
			emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
			return nil
		}
		emit(protocol.NewSessionListResultEvent(selectedSessionID, toProtocolSummaries(items)))
		return items
	}

	emitEmptySessionState := func() {
		emit(protocol.NewSessionStateEvent(selectedSessionID, string(session.StateActive), "session cleared"))
	}

	emitAndPersistFor := func(sessionID string) func(any) {
		return func(event any) {
			switch e := event.(type) {
			case protocol.CatalogAuthoringResultEvent:
				if e.Domain == "skill" {
					if e.Skill == nil {
						emit(protocol.NewErrorEvent(sessionID, "catalog authoring 缺少 skill payload", ""))
						return
					}
					if err := upsertLocalSkill(h.SessionStore, ctx, *e.Skill); err != nil {
						emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
						return
					}
					h.SkillLauncher = skills.NewLauncher(h.SessionStore)
					emitSkillCatalogResult(emit, h.SessionStore, ctx, sessionID)
					return
				}
				if e.Domain == "memory" {
					if e.Memory == nil {
						emit(protocol.NewErrorEvent(sessionID, "catalog authoring 缺少 memory payload", ""))
						return
					}
					if err := upsertMemoryItem(h.SessionStore, ctx, *e.Memory); err != nil {
						emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
						return
					}
					emitMemoryListResult(emit, h.SessionStore, ctx, sessionID)
					return
				}
				emit(protocol.NewErrorEvent(sessionID, "未知 catalog authoring domain", ""))
				return
			default:
				emit(event)
				snapshot, ok := applyEventToProjection(buildProjectionSnapshotFor(sessionID), event)
				if ok {
					persistProjectionFor(sessionID, snapshot)
				}
			}
		}
	}

	defer func() {
		cancel()
		runtimeSvc.Cleanup()
		writerWG.Wait()
		logx.Info("ws", "connection closed: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
	}()

	emit(protocol.NewSessionStateEvent(selectedSessionID, string(session.StateActive), "connected"))
	emit(runtimeSvc.InitialEvent())
	if h.SessionStore != nil {
		emitSkillCatalogResult(emit, h.SessionStore, ctx, selectedSessionID)
		emitMemoryListResult(emit, h.SessionStore, ctx, selectedSessionID)
		items, err := h.SessionStore.ListSessions(ctx)
		if err != nil {
			logx.Warn("ws", "initial session list restore failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
		} else {
			emit(protocol.NewSessionListResultEvent(selectedSessionID, toProtocolSummaries(items)))
			if strings.TrimSpace(selectedSessionID) != "" {
				record, err := h.SessionStore.GetSession(ctx, selectedSessionID)
				if err != nil {
					logx.Warn("ws", "initial session history restore skipped: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				} else {
					emit(newSessionHistoryEventFromRecord(record))
					emitReviewStateFromProjection(emit, selectedSessionID, record.Projection)
				}
			}
		}
	}

	for {
		select {
		case err := <-writeErrCh:
			if err != nil {
				logx.Error("ws", "writer terminated with error: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
			}
			return
		default:
		}

		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logx.Warn("ws", "unexpected websocket close: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
			} else {
				logx.Info("ws", "websocket read loop ended: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
			}
			return
		}

		if messageType != websocket.TextMessage {
			logx.Warn("ws", "reject non-text websocket message: connectionID=%s sessionID=%s remoteAddr=%s type=%d", connectionID, selectedSessionID, remoteAddr, messageType)
			emit(protocol.NewErrorEvent(selectedSessionID, "only text messages are supported", ""))
			continue
		}

		var clientEvent protocol.ClientEvent
		if err := json.Unmarshal(payload, &clientEvent); err != nil {
			logx.Warn("ws", "invalid websocket json payload: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
			emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid json: %v", err), ""))
			continue
		}

		switch clientEvent.Action {
		case "session_create":
			var req protocol.SessionCreateRequestEvent
			if err := json.Unmarshal(payload, &req); err != nil {
				logx.Warn("ws", "invalid session_create request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_create request: %v", err), ""))
				continue
			}
			if h.SessionStore == nil {
				logx.Error("ws", "session store unavailable for session_create: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			created, err := h.SessionStore.CreateSession(ctx, req.Title)
			if err != nil {
				logx.Error("ws", "create session failed: connectionID=%s sessionID=%s remoteAddr=%s title=%q err=%v", connectionID, selectedSessionID, remoteAddr, req.Title, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			switchRuntimeSession(created.ID)
			emit(protocol.NewSessionCreatedEvent(selectedSessionID, toProtocolSummary(created)))
			emit(protocol.NewSessionStateEvent(selectedSessionID, string(session.StateActive), "session selected"))
			emitSessionList()
		case "session_list":
			if h.SessionStore == nil {
				logx.Error("ws", "session store unavailable for session_list: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			emitSessionList()
		case "session_load":
			var req protocol.SessionLoadRequestEvent
			if err := json.Unmarshal(payload, &req); err != nil {
				logx.Warn("ws", "invalid session_load request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_load request: %v", err), ""))
				continue
			}
			if h.SessionStore == nil {
				logx.Error("ws", "session store unavailable for session_load: connectionID=%s sessionID=%s remoteAddr=%s requestedSessionID=%s", connectionID, selectedSessionID, remoteAddr, req.SessionID)
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			record, err := h.SessionStore.GetSession(ctx, req.SessionID)
			if err != nil {
				logx.Warn("ws", "load session failed: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, req.SessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			switchRuntimeSession(req.SessionID)
			emit(newSessionHistoryEventFromRecord(record))
			emitReviewStateFromProjection(emit, selectedSessionID, record.Projection)
			emit(protocol.NewSessionStateEvent(selectedSessionID, string(session.StateActive), "history loaded"))
		case "session_delete":
			var req protocol.SessionDeleteRequestEvent
			if err := json.Unmarshal(payload, &req); err != nil {
				logx.Warn("ws", "invalid session_delete request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_delete request: %v", err), ""))
				continue
			}
			if h.SessionStore == nil {
				logx.Error("ws", "session store unavailable for session_delete: connectionID=%s sessionID=%s remoteAddr=%s requestedSessionID=%s", connectionID, selectedSessionID, remoteAddr, req.SessionID)
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			if err := h.SessionStore.DeleteSession(ctx, req.SessionID); err != nil {
				logx.Warn("ws", "delete session failed: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, req.SessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			deletingCurrent := req.SessionID == selectedSessionID
			items := emitSessionList()
			if !deletingCurrent {
				continue
			}
			fallbackSessionID := ""
			for _, item := range items {
				if strings.TrimSpace(item.ID) != "" {
					fallbackSessionID = item.ID
					break
				}
			}
			switchRuntimeSession(fallbackSessionID)
			if fallbackSessionID == "" {
				emitEmptySessionState()
				continue
			}
			record, err := h.SessionStore.GetSession(ctx, fallbackSessionID)
			if err != nil {
				logx.Warn("ws", "load fallback session after delete failed: connectionID=%s sessionID=%s fallbackSessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, fallbackSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			emit(newSessionHistoryEventFromRecord(record))
			emit(protocol.NewSessionStateEvent(selectedSessionID, string(session.StateActive), "history loaded"))
		case "session_context_get":
			if strings.TrimSpace(selectedSessionID) == "" {
				emit(protocol.NewSessionContextResultEvent(selectedSessionID, toProtocolSessionContext(store.SessionContext{})))
				continue
			}
			record, ok := loadSelectedSessionRecord(h.SessionStore, ctx, selectedSessionID, emit)
			if !ok {
				continue
			}
			emit(protocol.NewSessionContextResultEvent(selectedSessionID, toProtocolSessionContext(record.Projection.SessionContext)))
		case "review_state_get":
			emitReviewStateFromProjection(emit, selectedSessionID, buildProjectionSnapshotFor(selectedSessionID))
		case "session_context_update":
			var req protocol.SessionContextUpdateRequestEvent
			if err := json.Unmarshal(payload, &req); err != nil {
				logx.Warn("ws", "invalid session_context_update request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_context_update request: %v", err), ""))
				continue
			}
			if strings.TrimSpace(selectedSessionID) == "" {
				emit(protocol.NewErrorEvent(selectedSessionID, "请先创建或加载会话后再更新 session context", ""))
				continue
			}
			record, ok := loadSelectedSessionRecord(h.SessionStore, ctx, selectedSessionID, emit)
			if !ok {
				continue
			}
			record.Projection.SessionContext = store.SessionContext{EnabledSkillNames: req.EnabledSkillNames, EnabledMemoryIDs: req.EnabledMemoryIDs}
			if _, err := h.SessionStore.SaveProjection(ctx, selectedSessionID, record.Projection); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			emit(protocol.NewSessionContextResultEvent(selectedSessionID, toProtocolSessionContext(record.Projection.SessionContext)))
		case "skill_catalog_get":
			emitSkillCatalogResult(emit, h.SessionStore, ctx, selectedSessionID)
		case "skill_catalog_upsert":
			var req protocol.SkillCatalogRequestEvent
			if err := json.Unmarshal(payload, &req); err != nil {
				logx.Warn("ws", "invalid skill_catalog_upsert request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid skill_catalog_upsert request: %v", err), ""))
				continue
			}
			if err := upsertLocalSkill(h.SessionStore, ctx, req.Skill); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			h.SkillLauncher = skills.NewLauncher(h.SessionStore)
			emitSkillCatalogResult(emit, h.SessionStore, ctx, selectedSessionID)
		case "skill_sync_pull":
			if h.SessionStore == nil {
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			snapshot, err := h.SessionStore.GetSkillCatalogSnapshot(ctx)
			if err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			snapshot.Meta.SourceOfTruth = store.CatalogSourceTruthClaude
			snapshot.Meta.SyncState = store.CatalogSyncStateSyncing
			snapshot.Meta.LastError = ""
			if err := h.SessionStore.SaveSkillCatalogSnapshot(ctx, snapshot); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			emit(protocol.NewCatalogSyncStatusEvent(selectedSessionID, string(store.CatalogDomainSkill), toProtocolCatalogMetadata(snapshot.Meta)))
			if err := syncExternalSkills(h.SessionStore, ctx); err != nil {
				snapshot.Meta.SyncState = store.CatalogSyncStateFailed
				snapshot.Meta.LastError = err.Error()
				_ = h.SessionStore.SaveSkillCatalogSnapshot(ctx, snapshot)
				emit(protocol.NewCatalogSyncResultEvent(selectedSessionID, string(store.CatalogDomainSkill), false, err.Error(), toProtocolCatalogMetadata(snapshot.Meta)))
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			h.SkillLauncher = skills.NewLauncher(h.SessionStore)
			updatedSnapshot, err := h.SessionStore.GetSkillCatalogSnapshot(ctx)
			if err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			emit(protocol.NewSkillSyncResultEvent(selectedSessionID, "skill 同步完成"))
			emit(protocol.NewCatalogSyncResultEvent(selectedSessionID, string(store.CatalogDomainSkill), true, "skill 同步完成", toProtocolCatalogMetadata(updatedSnapshot.Meta)))
			emitSkillCatalogResult(emit, h.SessionStore, ctx, selectedSessionID)
		case "memory_list":
			emitMemoryListResult(emit, h.SessionStore, ctx, selectedSessionID)
		case "memory_sync_pull":
			if h.SessionStore == nil {
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			snapshot, err := h.SessionStore.GetMemoryCatalogSnapshot(ctx)
			if err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			snapshot.Meta.SourceOfTruth = store.CatalogSourceTruthClaude
			snapshot.Meta.SyncState = store.CatalogSyncStateSyncing
			snapshot.Meta.LastError = ""
			if err := h.SessionStore.SaveMemoryCatalogSnapshot(ctx, snapshot); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			emit(protocol.NewCatalogSyncStatusEvent(selectedSessionID, string(store.CatalogDomainMemory), toProtocolCatalogMetadata(snapshot.Meta)))
			if err := syncExternalMemories(h.SessionStore, ctx); err != nil {
				snapshot.Meta.SyncState = store.CatalogSyncStateFailed
				snapshot.Meta.LastError = err.Error()
				_ = h.SessionStore.SaveMemoryCatalogSnapshot(ctx, snapshot)
				emit(protocol.NewCatalogSyncResultEvent(selectedSessionID, string(store.CatalogDomainMemory), false, err.Error(), toProtocolCatalogMetadata(snapshot.Meta)))
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			updatedSnapshot, err := h.SessionStore.GetMemoryCatalogSnapshot(ctx)
			if err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			emit(protocol.NewCatalogSyncResultEvent(selectedSessionID, string(store.CatalogDomainMemory), true, "memory 同步完成", toProtocolCatalogMetadata(updatedSnapshot.Meta)))
			emitMemoryListResult(emit, h.SessionStore, ctx, selectedSessionID)
		case "memory_upsert":
			var req protocol.MemoryRequestEvent
			if err := json.Unmarshal(payload, &req); err != nil {
				logx.Warn("ws", "invalid memory_upsert request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid memory_upsert request: %v", err), ""))
				continue
			}
			if err := upsertMemoryItem(h.SessionStore, ctx, req.Item); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			emitMemoryListResult(emit, h.SessionStore, ctx, selectedSessionID)
		case "exec":
			var reqEvent protocol.ExecRequestEvent
			if err := json.Unmarshal(payload, &reqEvent); err != nil {
				logx.Warn("ws", "invalid exec request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid exec request: %v", err), ""))
				continue
			}
			logx.Info("ws", "incoming action: connectionID=%s sessionID=%s remoteAddr=%s action=exec cmd=%q cwd=%q mode=%q permissionMode=%q source=%q target=%q targetType=%q contextID=%q preview=%q", connectionID, selectedSessionID, remoteAddr, reqEvent.Command, reqEvent.CWD, reqEvent.Mode, reqEvent.PermissionMode, reqEvent.Source, reqEvent.Target, reqEvent.TargetType, reqEvent.ContextID, wsDebugPreview(reqEvent.Command))
			if strings.TrimSpace(reqEvent.Command) == "" {
				logx.Warn("ws", "reject empty exec command: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
				emit(protocol.NewErrorEvent(selectedSessionID, "cmd is required", ""))
				continue
			}
			if strings.TrimSpace(selectedSessionID) == "" || selectedSessionID == connectionID {
				emit(protocol.NewErrorEvent(selectedSessionID, "请先创建或加载会话后再发送命令", ""))
				continue
			}
			sessionID := selectedSessionID
			service := runtimeSvc
			emitAndPersist := emitAndPersistFor(sessionID)
			appendUserProjectionEntry(h.SessionStore, ctx, sessionID, reqEvent.Command, "命令", connectionID, remoteAddr)
			mode, err := runtimepkg.ParseMode(reqEvent.Mode)
			if err != nil {
				logx.Warn("ws", "parse exec mode failed: connectionID=%s sessionID=%s remoteAddr=%s mode=%q err=%v", connectionID, sessionID, remoteAddr, reqEvent.Mode, err)
				emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
				continue
			}
			logx.Info("ws", "dispatch exec: connectionID=%s sessionID=%s remoteAddr=%s action=exec mode=%s cwd=%q permissionMode=%q preview=%q", connectionID, sessionID, remoteAddr, mode, reqEvent.CWD, reqEvent.PermissionMode, wsDebugPreview(reqEvent.Command))
			err = service.Execute(ctx, sessionID, runtimepkg.ExecuteRequest{
				Command:        reqEvent.Command,
				CWD:            reqEvent.CWD,
				Mode:           mode,
				PermissionMode: reqEvent.PermissionMode,
				RuntimeMeta: protocol.RuntimeMeta{
					Source:         fallback(reqEvent.Source, "command"),
					SkillName:      reqEvent.SkillName,
					Target:         reqEvent.Target,
					TargetType:     reqEvent.TargetType,
					TargetPath:     reqEvent.TargetPath,
					ResultView:     reqEvent.ResultView,
					ContextID:      reqEvent.ContextID,
					ContextTitle:   reqEvent.ContextTitle,
					TargetText:     reqEvent.TargetText,
					Command:        reqEvent.Command,
					Engine:         reqEvent.Engine,
					CWD:            reqEvent.CWD,
					PermissionMode: reqEvent.PermissionMode,
				},
			}, emitAndPersist)
			if err != nil {
				logx.Error("ws", "service execute failed: connectionID=%s sessionID=%s remoteAddr=%s action=exec err=%v", connectionID, sessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
			}
		case "input":
			var inputEvent protocol.InputRequestEvent
			if err := json.Unmarshal(payload, &inputEvent); err != nil {
				logx.Warn("ws", "invalid input request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid input request: %v", err), ""))
				continue
			}
			logx.Info("ws", "incoming action: connectionID=%s sessionID=%s remoteAddr=%s action=input permissionMode=%q dataPreview=%q", connectionID, selectedSessionID, remoteAddr, inputEvent.PermissionMode, wsDebugPreview(inputEvent.Data))
			if inputEvent.Data == "" {
				logx.Warn("ws", "reject empty input payload: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
				emit(protocol.NewErrorEvent(selectedSessionID, "input data is required", ""))
				continue
			}
			if strings.TrimSpace(selectedSessionID) == "" || selectedSessionID == connectionID {
				emit(protocol.NewErrorEvent(selectedSessionID, "请先创建或加载会话后再发送命令", ""))
				continue
			}
			sessionID := selectedSessionID
			service := runtimeSvc
			emitAndPersist := emitAndPersistFor(sessionID)
			appendUserProjectionEntry(h.SessionStore, ctx, sessionID, strings.TrimRight(inputEvent.Data, "\n"), "回复", connectionID, remoteAddr)
			service.RecordUserInput(inputEvent.Data)
			inputMeta := protocol.RuntimeMeta{}
			if pm := inputEvent.PermissionMode; pm != "" {
				service.UpdatePermissionMode(pm)
				inputMeta.PermissionMode = pm
			}
			logx.Info("ws", "dispatch input: connectionID=%s sessionID=%s remoteAddr=%s action=input permissionMode=%q preview=%q", connectionID, sessionID, remoteAddr, inputMeta.PermissionMode, wsDebugPreview(inputEvent.Data))
			controller := service.ControllerSnapshot()
			projection := buildProjectionSnapshotFor(sessionID)
			snapshot := service.RuntimeSnapshot()
			if snapshot.TemporaryElevated {
				restoreReq := runtimepkg.ExecuteRequest{
					Command:        firstNonEmptyString(snapshot.ActiveMeta.Command, controller.CurrentCommand),
					CWD:            snapshot.ActiveMeta.CWD,
					Mode:           runner.ModePTY,
					PermissionMode: firstNonEmptyString(snapshot.SafePermissionMode, snapshot.ActiveMeta.PermissionMode, "default"),
					RuntimeMeta: protocol.RuntimeMeta{
						Source:          "input",
						ResumeSessionID: firstNonEmptyString(snapshot.ResumeSessionID, snapshot.ActiveMeta.ResumeSessionID),
						Command:         firstNonEmptyString(snapshot.ActiveMeta.Command, controller.CurrentCommand),
						CWD:             snapshot.ActiveMeta.CWD,
						PermissionMode:  firstNonEmptyString(snapshot.SafePermissionMode, snapshot.ActiveMeta.PermissionMode, "default"),
					},
				}
				if err := service.RestoreSafePermissionModeBeforeInput(ctx, sessionID, restoreReq, inputEvent.Data, emitAndPersist); err != nil {
					message := err.Error()
					if errors.Is(err, runtimepkg.ErrResumeSessionUnavailable) {
						message = "当前没有可恢复的 Claude 会话，无法先恢复到安全权限模式"
					} else if errors.Is(err, runtimepkg.ErrHotSwapUnsupportedRunner) {
						message = "当前活跃会话不是可热重启恢复的 Claude PTY 会话"
					} else if errors.Is(err, runtimepkg.ErrRunnerNotInteractive) {
						message = "Claude 恢复到安全模式后未进入可输入状态，请稍后重试"
					} else if errors.Is(err, runtimepkg.ErrNoActiveRunner) {
						message = "安全权限模式恢复失败，当前没有可交互会话"
					}
					logx.Warn("ws", "restore safe permission mode before input failed: connectionID=%s sessionID=%s remoteAddr=%s action=input err=%v", connectionID, sessionID, remoteAddr, err)
					emit(protocol.NewErrorEvent(sessionID, message, ""))
				}
				continue
			}
			resumeReq := runtimepkg.ExecuteRequest{
				Command: firstNonEmptyString(
					snapshot.ActiveMeta.Command,
					controller.CurrentCommand,
					projection.Runtime.Command,
					"claude",
				),
				CWD: firstNonEmptyString(
					snapshot.ActiveMeta.CWD,
					controller.ActiveMeta.CWD,
					projection.Runtime.CWD,
				),
				Mode: runner.ModePTY,
				PermissionMode: firstNonEmptyString(
					inputEvent.PermissionMode,
					snapshot.SafePermissionMode,
					snapshot.ActiveMeta.PermissionMode,
					controller.ActiveMeta.PermissionMode,
					projection.Runtime.PermissionMode,
					"default",
				),
				RuntimeMeta: protocol.RuntimeMeta{
					Source: "input",
					ResumeSessionID: firstNonEmptyString(
						snapshot.ResumeSessionID,
						snapshot.ActiveMeta.ResumeSessionID,
						controller.ResumeSession,
						projection.Runtime.ResumeSessionID,
					),
					Command: firstNonEmptyString(
						snapshot.ActiveMeta.Command,
						controller.CurrentCommand,
						projection.Runtime.Command,
						"claude",
					),
					CWD: firstNonEmptyString(
						snapshot.ActiveMeta.CWD,
						controller.ActiveMeta.CWD,
						projection.Runtime.CWD,
					),
					PermissionMode: firstNonEmptyString(
						inputEvent.PermissionMode,
						snapshot.SafePermissionMode,
						snapshot.ActiveMeta.PermissionMode,
						controller.ActiveMeta.PermissionMode,
						projection.Runtime.PermissionMode,
						"default",
					),
				},
			}
			if err := service.SendInputOrResume(ctx, sessionID, resumeReq, runtimepkg.InputRequest{Data: inputEvent.Data, RuntimeMeta: inputMeta}, emitAndPersist); err != nil {
				message := err.Error()
				if errors.Is(err, runner.ErrInputNotSupported) {
					message = "input is only supported for pty sessions"
				} else if errors.Is(err, runtimepkg.ErrNoActiveRunner) {
					message = "当前没有活跃会话，且没有可恢复的 Claude 会话，请重新发起命令"
				} else if errors.Is(err, runtimepkg.ErrResumeSessionUnavailable) {
					message = "当前没有 resume id，无法恢复 Claude 会话，请重新发起命令"
				} else if errors.Is(err, runtimepkg.ErrResumeConversationNotFound) {
					message = "当前 Claude 会话的 resume id 已失效或不存在，请重新发起命令"
				} else if errors.Is(err, runtimepkg.ErrRunnerNotInteractive) {
					message = "Claude 恢复后未进入可输入状态，请稍后重试"
				}
				logx.Warn("ws", "service send input failed: connectionID=%s sessionID=%s remoteAddr=%s action=input err=%v", connectionID, sessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(sessionID, message, ""))
			}
		case "permission_decision":
			var permissionEvent protocol.PermissionDecisionRequestEvent
			if err := json.Unmarshal(payload, &permissionEvent); err != nil {
				logx.Warn("ws", "invalid permission decision request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid permission decision request: %v", err), ""))
				continue
			}
			logx.Info("ws", "incoming action: connectionID=%s sessionID=%s remoteAddr=%s action=permission_decision decision=%q permissionMode=%q resumeSessionID=%q targetPath=%q contextID=%q fallbackCWD=%q fallbackCommandPreview=%q promptPreview=%q", connectionID, selectedSessionID, remoteAddr, permissionEvent.Decision, permissionEvent.PermissionMode, permissionEvent.ResumeSessionID, permissionEvent.TargetPath, permissionEvent.ContextID, permissionEvent.FallbackCWD, wsDebugPreview(permissionEvent.FallbackCommand), wsDebugPreview(permissionEvent.PromptMessage))
			decision := strings.TrimSpace(strings.ToLower(permissionEvent.Decision))
			if decision != "approve" && decision != "deny" {
				logx.Warn("ws", "reject invalid permission decision: connectionID=%s sessionID=%s remoteAddr=%s decision=%q", connectionID, selectedSessionID, remoteAddr, permissionEvent.Decision)
				emit(protocol.NewErrorEvent(selectedSessionID, "permission decision must be one of: approve, deny", ""))
				continue
			}
			sessionID := selectedSessionID
			service := runtimeSvc
			emitAndPersist := emitAndPersistFor(sessionID)
			projection := buildProjectionSnapshotFor(sessionID)
			controller := service.ControllerSnapshot()
			effectivePermissionMode := strings.TrimSpace(permissionEvent.PermissionMode)
			if effectivePermissionMode == "" {
				effectivePermissionMode = strings.TrimSpace(controller.ActiveMeta.PermissionMode)
			}
			if effectivePermissionMode == "" {
				effectivePermissionMode = strings.TrimSpace(projection.Runtime.PermissionMode)
			}
			if effectivePermissionMode != "" {
				service.UpdatePermissionMode(effectivePermissionMode)
			}
			currentRunner := service.CurrentRunner()
			inputMeta := protocol.RuntimeMeta{
				Source:          "permission-decision",
				ResumeSessionID: firstNonEmptyString(permissionEvent.ResumeSessionID, controller.ResumeSession, controller.ActiveMeta.ResumeSessionID, projection.Runtime.ResumeSessionID),
				ContextID:       firstNonEmptyString(permissionEvent.ContextID, controller.ActiveMeta.ContextID),
				ContextTitle:    firstNonEmptyString(permissionEvent.ContextTitle, controller.ActiveMeta.ContextTitle),
				TargetPath:      firstNonEmptyString(permissionEvent.TargetPath, controller.ActiveMeta.TargetPath),
				TargetText:      decision,
				Command:         firstNonEmptyString(permissionEvent.FallbackCommand, projection.Runtime.Command, controller.CurrentCommand, controller.ActiveMeta.Command),
				Engine:          firstNonEmptyString(permissionEvent.FallbackEngine, controller.ActiveMeta.Engine),
				CWD:             firstNonEmptyString(permissionEvent.FallbackCWD, controller.ActiveMeta.CWD, projection.Runtime.CWD),
				Target:          firstNonEmptyString(permissionEvent.FallbackTarget, controller.ActiveMeta.Target),
				TargetType:      firstNonEmptyString(permissionEvent.FallbackTargetType, controller.ActiveMeta.TargetType),
				PermissionMode:  effectivePermissionMode,
			}
			appendUserProjectionEntry(h.SessionStore, ctx, sessionID, strings.TrimSpace(permissionEvent.PromptMessage), "权限决策", connectionID, remoteAddr)
			if decision == "deny" {
				prompt, err := buildPermissionDecisionPrompt(decision, permissionEvent)
				if err != nil {
					logx.Warn("ws", "build permission deny prompt failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
					emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
					continue
				}
				if err := service.SendInput(ctx, sessionID, runtimepkg.InputRequest{Data: prompt, RuntimeMeta: inputMeta}, emitAndPersist); err != nil {
					message := err.Error()
					if errors.Is(err, runner.ErrInputNotSupported) {
						message = "当前会话不支持交互输入，请先恢复 Claude PTY 会话"
					} else if errors.Is(err, runtimepkg.ErrNoActiveRunner) {
						message = "当前没有可交互的 Claude 会话，无法继续处理该权限请求"
					} else if errors.Is(err, runtimepkg.ErrRunnerNotInteractive) {
						message = "当前 Claude 会话尚未进入可直接确认的交互阶段，请先等待当前会话就绪后再提交权限决策"
					}
					logx.Warn("ws", "send permission deny prompt failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
					emit(protocol.NewErrorEvent(sessionID, message, ""))
				}
				continue
			}
			replayInput := hotSwapApproveContinuation(permissionEvent)
			req := runtimepkg.ExecuteRequest{
				Command:        inputMeta.Command,
				CWD:            inputMeta.CWD,
				Mode:           runner.ModePTY,
				PermissionMode: inputMeta.PermissionMode,
				RuntimeMeta:    inputMeta,
			}
			err := func() error {
				if currentRunner != nil {
					if err := service.HotSwapApproveWithTemporaryElevation(ctx, sessionID, req, replayInput, emitAndPersist); err == nil || !errors.Is(err, runtimepkg.ErrNoActiveRunner) {
						return err
					}
				}
				if !service.CanHotSwapClaudeSession(req) || !service.HasResumeSession(req) {
					return runtimepkg.ErrNoActiveRunner
				}
				return service.HotSwapApproveFromResume(ctx, sessionID, req, replayInput, emitAndPersist)
			}()
			if err != nil {
				message := err.Error()
				if errors.Is(err, runtimepkg.ErrNoActiveRunner) {
					message = "当前没有可交互的 Claude 会话，无法继续处理该权限请求"
				} else if errors.Is(err, runtimepkg.ErrResumeSessionUnavailable) {
					message = "当前没有可恢复的 Claude 会话，无法通过热重启继续此次权限批准"
				} else if errors.Is(err, runtimepkg.ErrHotSwapUnsupportedRunner) {
					message = "当前活跃会话不是可热重启恢复的 Claude PTY 会话"
				} else if errors.Is(err, runtimepkg.ErrResumeConversationNotFound) {
					message = "当前 Claude 会话的 resume id 已失效或不存在，无法通过热重启继续此次权限批准"
				} else if errors.Is(err, runtimepkg.ErrRunnerNotInteractive) {
					message = "Claude 恢复后未进入可输入状态，无法继续刚才的操作"
				}
				logx.Warn("ws", "hot swap approve failed: connectionID=%s sessionID=%s remoteAddr=%s decision=%s err=%v", connectionID, sessionID, remoteAddr, decision, err)
				emit(protocol.NewErrorEvent(sessionID, message, ""))
			}
		case "review_decision":
			var reviewEvent protocol.ReviewDecisionRequestEvent
			if err := json.Unmarshal(payload, &reviewEvent); err != nil {
				logx.Warn("ws", "invalid review decision request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid review decision request: %v", err), ""))
				continue
			}
			logx.Info("ws", "incoming action: connectionID=%s sessionID=%s remoteAddr=%s action=review_decision decision=%q executionID=%q groupID=%q groupTitle=%q contextID=%q contextTitle=%q targetPath=%q permissionMode=%q", connectionID, selectedSessionID, remoteAddr, reviewEvent.Decision, reviewEvent.ExecutionID, reviewEvent.GroupID, reviewEvent.GroupTitle, reviewEvent.ContextID, reviewEvent.ContextTitle, reviewEvent.TargetPath, reviewEvent.PermissionMode)
			decision := strings.TrimSpace(strings.ToLower(reviewEvent.Decision))
			if decision != "accept" && decision != "revert" && decision != "revise" {
				logx.Warn("ws", "reject invalid review decision: connectionID=%s sessionID=%s remoteAddr=%s decision=%q", connectionID, selectedSessionID, remoteAddr, reviewEvent.Decision)
				emit(protocol.NewErrorEvent(selectedSessionID, "review decision must be one of: accept, revert, revise", ""))
				continue
			}
			sessionID := selectedSessionID
			service := runtimeSvc
			emitAndPersist := emitAndPersistFor(sessionID)
			projection := buildProjectionSnapshotFor(sessionID)
			controller := service.ControllerSnapshot()
			effectivePermissionMode := strings.TrimSpace(reviewEvent.PermissionMode)
			if effectivePermissionMode == "" {
				effectivePermissionMode = strings.TrimSpace(controller.ActiveMeta.PermissionMode)
			}
			if effectivePermissionMode == "" {
				effectivePermissionMode = strings.TrimSpace(projection.Runtime.PermissionMode)
			}
			if !service.CanAcceptInteractiveInput() {
				emit(protocol.NewErrorEvent(selectedSessionID, "当前 Claude 会话尚未进入可直接确认的交互阶段，请先等待当前会话就绪后再提交审核决策", ""))
				continue
			}
			if effectivePermissionMode != "" {
				service.UpdatePermissionMode(effectivePermissionMode)
			}
			var currentDiff session.DiffContext
			if projection.CurrentDiff != nil {
				currentDiff = *projection.CurrentDiff
			}
			if err := service.ReviewDecision(ctx, sessionID, runtimepkg.ReviewDecisionRequest{
				Decision: decision,
				RuntimeMeta: protocol.RuntimeMeta{
					Source:         "review-decision",
					ExecutionID:    firstNonEmptyString(reviewEvent.ExecutionID, currentDiff.ExecutionID),
					GroupID:        firstNonEmptyString(reviewEvent.GroupID, reviewEvent.ExecutionID, currentDiff.GroupID, reviewEvent.ContextID),
					GroupTitle:     firstNonEmptyString(reviewEvent.GroupTitle, currentDiff.GroupTitle, reviewEvent.ContextTitle),
					ContextID:      firstNonEmptyString(reviewEvent.ContextID, currentDiff.ContextID),
					ContextTitle:   firstNonEmptyString(reviewEvent.ContextTitle, currentDiff.Title),
					TargetPath:     firstNonEmptyString(reviewEvent.TargetPath, currentDiff.Path),
					TargetText:     decision,
					Command:        firstNonEmptyString(controller.ActiveMeta.Command, projection.Runtime.Command),
					CWD:            firstNonEmptyString(controller.ActiveMeta.CWD, projection.Runtime.CWD),
					PermissionMode: effectivePermissionMode,
				},
			}, emitAndPersist); err != nil {
				message := err.Error()
				if errors.Is(err, runner.ErrInputNotSupported) {
					message = "当前会话不支持交互输入，请先恢复 Claude PTY 会话"
				} else if errors.Is(err, runtimepkg.ErrNoActiveRunner) {
					message = "当前没有可交互会话，请先恢复会话后再审核 diff"
				} else if errors.Is(err, runtimepkg.ErrRunnerNotInteractive) {
					message = "当前 Claude 会话尚未进入可直接确认的交互阶段，请先等待当前会话就绪后再提交审核决策"
				}
				logx.Warn("ws", "send review decision failed: connectionID=%s sessionID=%s remoteAddr=%s decision=%s err=%v", connectionID, sessionID, remoteAddr, decision, err)
				emit(protocol.NewErrorEvent(sessionID, message, ""))
				continue
			}
			projection = applyReviewDecisionToProjection(projection, reviewEvent, decision, currentDiff)
			persistProjectionFor(sessionID, projection)
			emitReviewStateFromProjection(emit, sessionID, projection)
		case "plan_decision":
			var planEvent protocol.PlanDecisionRequestEvent
			if err := json.Unmarshal(payload, &planEvent); err != nil {
				logx.Warn("ws", "invalid plan decision request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid plan decision request: %v", err), ""))
				continue
			}
			logx.Info("ws", "incoming action: connectionID=%s sessionID=%s remoteAddr=%s action=plan_decision decision=%q executionID=%q groupID=%q contextID=%q promptPreview=%q", connectionID, selectedSessionID, remoteAddr, planEvent.Decision, planEvent.ExecutionID, planEvent.GroupID, planEvent.ContextID, wsDebugPreview(planEvent.PromptMessage))
			service := runtimeSvc
			emitAndPersist := emitAndPersistFor(selectedSessionID)
			req := runtimepkg.PlanDecisionRequest{
				Decision: planEvent.Decision,
				RuntimeMeta: protocol.RuntimeMeta{
					Source:          "plan-decision",
					ResumeSessionID: planEvent.ResumeSessionID,
					ExecutionID:     planEvent.ExecutionID,
					GroupID:         planEvent.GroupID,
					GroupTitle:      planEvent.GroupTitle,
					ContextID:       planEvent.ContextID,
					ContextTitle:    planEvent.ContextTitle,
					TargetPath:      planEvent.TargetPath,
					TargetText:      planEvent.TargetText,
					Command:         firstNonEmptyString(planEvent.Command, service.ControllerSnapshot().ActiveMeta.Command, buildProjectionSnapshotFor(selectedSessionID).Runtime.Command),
					Engine:          firstNonEmptyString(planEvent.Engine, service.ControllerSnapshot().ActiveMeta.Engine),
					CWD:             firstNonEmptyString(planEvent.CWD, service.ControllerSnapshot().ActiveMeta.CWD, buildProjectionSnapshotFor(selectedSessionID).Runtime.CWD),
					Target:          firstNonEmptyString(planEvent.Target, service.ControllerSnapshot().ActiveMeta.Target),
					TargetType:      firstNonEmptyString(planEvent.TargetType, service.ControllerSnapshot().ActiveMeta.TargetType),
					PermissionMode:  firstNonEmptyString(planEvent.PermissionMode, service.ControllerSnapshot().ActiveMeta.PermissionMode, buildProjectionSnapshotFor(selectedSessionID).Runtime.PermissionMode),
				},
			}
			if err := service.PlanDecision(ctx, selectedSessionID, req, emitAndPersist); err != nil {
				message := err.Error()
				if errors.Is(err, runtimepkg.ErrNoActiveRunner) {
					message = "当前没有可交互的 Claude 会话，无法继续处理该 plan 请求"
				} else if errors.Is(err, runtimepkg.ErrRunnerNotInteractive) {
					message = "当前 Claude 会话尚未进入可提交 plan 的交互阶段，请先等待当前会话就绪后再提交"
				}
				logx.Warn("ws", "send plan decision failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, message, ""))
			}
		case "set_permission_mode":
			var modeEvent protocol.PermissionModeUpdateRequestEvent
			if err := json.Unmarshal(payload, &modeEvent); err != nil {
				logx.Warn("ws", "invalid permission mode request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid permission mode request: %v", err), ""))
				continue
			}
			service := runtimeSvc
			service.UpdatePermissionMode(modeEvent.PermissionMode)
			emit(protocol.ApplyRuntimeMeta(service.InitialEvent(), protocol.RuntimeMeta{PermissionMode: modeEvent.PermissionMode}))
		case "skill_exec":
			var skillEvent protocol.SkillRequestEvent
			if err := json.Unmarshal(payload, &skillEvent); err != nil {
				logx.Warn("ws", "invalid skill request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid skill request: %v", err), ""))
				continue
			}
			if h.SkillLauncher == nil {
				logx.Error("ws", "skill launcher unavailable: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
				emit(protocol.NewErrorEvent(selectedSessionID, "skill launcher is unavailable", ""))
				continue
			}
			sessionContext := store.SessionContext{}
			if h.SessionStore != nil {
				record, ok := loadSelectedSessionRecord(h.SessionStore, ctx, selectedSessionID, emit)
				if !ok {
					continue
				}
				sessionContext = record.Projection.SessionContext
			}
			sessionID := selectedSessionID
			service := runtimeSvc
			emitAndPersist := emitAndPersistFor(sessionID)
			if err := executeSkillRequest(ctx, sessionID, skillEvent, sessionContext, service, h.SkillLauncher, emitAndPersist); err != nil {
				logx.Error("ws", "execute skill request failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
			}
		case "runtime_info":
			var infoReq protocol.RuntimeInfoRequestEvent
			if err := json.Unmarshal(payload, &infoReq); err != nil {
				logx.Warn("ws", "invalid runtime_info request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid runtime_info request: %v", err), ""))
				continue
			}
			result, err := runtimepkg.BuildRuntimeInfoResult(selectedSessionID, infoReq.Query, fallback(infoReq.CWD, "."), runtimeSvc)
			if err != nil {
				logx.Warn("ws", "build runtime info failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			emit(result)
		case "slash_command":
			var slashReq protocol.SlashCommandRequestEvent
			if err := json.Unmarshal(payload, &slashReq); err != nil {
				logx.Warn("ws", "invalid slash_command request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid slash_command request: %v", err), ""))
				continue
			}
			sessionContext := store.SessionContext{}
			if h.SessionStore != nil {
				record, ok := loadSelectedSessionRecord(h.SessionStore, ctx, selectedSessionID, emit)
				if !ok {
					continue
				}
				sessionContext = record.Projection.SessionContext
			}
			sessionID := selectedSessionID
			service := runtimeSvc
			emitAndPersist := emitAndPersistFor(sessionID)
			if err := handleSlashCommand(ctx, sessionID, slashReq, sessionContext, service, h.SkillLauncher, emitAndPersist); err != nil {
				logx.Error("ws", "handle slash command failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
			}
		case "fs_list":
			var fsListReq protocol.FSListRequestEvent
			if err := json.Unmarshal(payload, &fsListReq); err != nil {
				logx.Warn("ws", "invalid fs_list request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid fs_list request: %v", err), ""))
				continue
			}
			result, err := listDirectory(selectedSessionID, fsListReq.Path)
			if err != nil {
				logx.Warn("ws", "list directory failed: connectionID=%s sessionID=%s remoteAddr=%s path=%q err=%v", connectionID, selectedSessionID, remoteAddr, fsListReq.Path, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("list directory: %v", err), ""))
				continue
			}
			emit(result)
		case "fs_read":
			var fsReadReq protocol.FSReadRequestEvent
			if err := json.Unmarshal(payload, &fsReadReq); err != nil {
				logx.Warn("ws", "invalid fs_read request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid fs_read request: %v", err), ""))
				continue
			}
			result, err := readFile(selectedSessionID, fsReadReq.Path)
			if err != nil {
				logx.Warn("ws", "read file failed: connectionID=%s sessionID=%s remoteAddr=%s path=%q err=%v", connectionID, selectedSessionID, remoteAddr, fsReadReq.Path, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("read file: %v", err), ""))
				continue
			}
			emit(result)
		default:
			logx.Warn("ws", "unknown action: connectionID=%s sessionID=%s remoteAddr=%s action=%s", connectionID, selectedSessionID, remoteAddr, clientEvent.Action)
			emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("unknown action: %s", clientEvent.Action), ""))
		}
	}
}

func appendUserProjectionEntry(sessionStore store.Store, ctx context.Context, sessionID, text, label, connectionID, remoteAddr string) {
	if sessionStore == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(text) == "" {
		if sessionStore == nil {
			logx.Warn("ws", "skip append user projection entry because session store unavailable: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, sessionID, remoteAddr)
		}
		return
	}
	record, err := sessionStore.GetSession(ctx, sessionID)
	if err != nil {
		logx.Warn("ws", "get session before append projection entry failed: connectionID=%s sessionID=%s remoteAddr=%s label=%s err=%v", connectionID, sessionID, remoteAddr, label, err)
		return
	}
	projection := normalizeProjectionSnapshot(record.Projection)
	projection.LogEntries = append(projection.LogEntries, store.SnapshotLogEntry{
		Kind:      "user",
		Message:   text,
		Label:     label,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	if _, err := sessionStore.SaveProjection(ctx, sessionID, projection); err != nil {
		logx.Error("ws", "save projection after append user entry failed: connectionID=%s sessionID=%s remoteAddr=%s label=%s err=%v", connectionID, sessionID, remoteAddr, label, err)
	}
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

func buildPermissionDecisionPrompt(decision string, req protocol.PermissionDecisionRequestEvent) (string, error) {
	decision = strings.TrimSpace(strings.ToLower(decision))
	if decision == "" {
		return "", fmt.Errorf("permission decision is required")
	}
	subject := strings.TrimSpace(req.TargetPath)
	if subject == "" {
		subject = strings.TrimSpace(req.ContextTitle)
	}
	if subject == "" {
		subject = "刚才请求的操作"
	}
	lines := []string{}
	if req.ResumeSessionID != "" {
		lines = append(lines, fmt.Sprintf("ResumeSessionID: %s", req.ResumeSessionID))
	}
	if req.TargetPath != "" {
		lines = append(lines, fmt.Sprintf("TargetPath: %s", req.TargetPath))
	}
	if req.ContextID != "" {
		lines = append(lines, fmt.Sprintf("ContextID: %s", req.ContextID))
	}
	if req.ContextTitle != "" {
		lines = append(lines, fmt.Sprintf("ContextTitle: %s", req.ContextTitle))
	}
	if req.PermissionMode != "" {
		lines = append(lines, fmt.Sprintf("PermissionMode: %s", req.PermissionMode))
	}
	if strings.TrimSpace(req.PromptMessage) != "" {
		lines = append(lines, fmt.Sprintf("OriginalPrompt: %s", strings.TrimSpace(req.PromptMessage)))
	}
	switch decision {
	case "approve":
		return fmt.Sprintf("用户已批准刚才请求的文件修改/写入权限。请在当前已保存的会话上下文中继续刚才被权限拦截的任务。执行要求：先重新读取目标文件的当前内容，再基于最新内容继续刚才的修改；只有在重新读取完成后，才重试刚才被拦截的编辑或写入工具调用。不要再次向用户请求同一权限，直接继续完成即可。目标：%s\n%s\n", subject, strings.Join(lines, "\n")), nil
	case "deny":
		return fmt.Sprintf("用户拒绝了刚才请求的文件修改/写入权限。请不要继续写入或编辑该目标，并基于当前上下文给出不写文件的替代方案或下一步建议。目标：%s\n%s\n", subject, strings.Join(lines, "\n")), nil
	default:
		return "", fmt.Errorf("permission decision must be one of: approve, deny")
	}
}

func hotSwapApproveContinuation(req protocol.PermissionDecisionRequestEvent) string {
	targetPath := strings.TrimSpace(req.TargetPath)
	promptMessage := strings.TrimSpace(req.PromptMessage)

	promptMessage = strings.ReplaceAll(promptMessage, "\r", " ")
	promptMessage = strings.ReplaceAll(promptMessage, "\n", " ")

	lines := []string{
		"我已经批准这次文件修改权限。",
		"不要继续复用刚才那次失败的工具调用；请基于这次已批准的权限，立即发起新的 Write/Edit 操作。",
	}

	if targetPath != "" {
		lines = append(lines, "本次已授权的目标文件：`"+targetPath+"`。")
	}

	lines = append(lines, "不要再次请求权限，不要只做解释，直接完成这次文件修改。")

	if promptMessage != "" {
		lines = append(lines, "你上一次请求权限时的原始上下文如下，请按该上下文重新完成写入："+promptMessage)
	}

	finalPrompt := strings.Join(lines, "  ")
	return finalPrompt + "\n"
}

func wsDebugPreview(value string) string {
	trimmed := strings.ReplaceAll(strings.TrimSpace(value), "\n", `\n`)
	trimmed = strings.ReplaceAll(trimmed, "\r", `\r`)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= wsDebugPreviewLimit {
		return trimmed
	}
	return string(runes[:wsDebugPreviewLimit]) + "…"
}

func wsDebugBoolLabel(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func readProjectionFromSessionStore(sessionStore store.Store, ctx context.Context, sessionID, connectionID, remoteAddr string) store.ProjectionSnapshot {
	if sessionStore == nil || strings.TrimSpace(sessionID) == "" {
		if sessionStore == nil {
			logx.Warn("ws", "projection restore skipped because session store unavailable: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, sessionID, remoteAddr)
		}
		return store.ProjectionSnapshot{RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""}}
	}
	record, err := sessionStore.GetSession(ctx, sessionID)
	if err != nil {
		logx.Warn("ws", "read projection from session store failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
		return store.ProjectionSnapshot{RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""}}
	}
	return record.Projection
}

func withRuntimeSnapshot(snapshot store.ProjectionSnapshot, svc *runtimepkg.Service) store.ProjectionSnapshot {
	snapshot = normalizeProjectionSnapshot(snapshot)
	if svc == nil {
		return snapshot
	}
	controller := svc.ControllerSnapshot()
	runtimeMeta := controller.ActiveMeta
	runtimeSnapshot := svc.RuntimeSnapshot()
	currentLifecycle := normalizeProjectionLifecycle(
		firstNonEmptyString(controller.ClaudeLifecycle, runtimeMeta.ClaudeLifecycle, runtimeSnapshot.ClaudeLifecycle),
		firstNonEmptyString(controller.ResumeSession, runtimeMeta.ResumeSessionID, runtimeSnapshot.ResumeSessionID),
	)
	snapshot.Controller = controller
	snapshot.Runtime = store.SessionRuntime{
		ResumeSessionID: firstNonEmptyString(controller.ResumeSession, runtimeMeta.ResumeSessionID),
		Command:         firstNonEmptyString(controller.CurrentCommand, runtimeMeta.Command),
		Engine:          firstNonEmptyString(runtimeMeta.Engine, runtimeMeta.SkillName),
		PermissionMode:  runtimeMeta.PermissionMode,
		CWD:             runtimeMeta.CWD,
		ClaudeLifecycle: currentLifecycle,
	}
	return snapshot
}

func toProtocolSummary(item store.SessionSummary) protocol.SessionSummary {
	return protocol.SessionSummary{
		ID:          item.ID,
		Title:       item.Title,
		CreatedAt:   item.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   item.UpdatedAt.Format(time.RFC3339),
		LastPreview: item.LastPreview,
		EntryCount:  item.EntryCount,
		Runtime: protocol.RuntimeMeta{
			ResumeSessionID: item.Runtime.ResumeSessionID,
			Command:         item.Runtime.Command,
			Engine:          item.Runtime.Engine,
			CWD:             item.Runtime.CWD,
			PermissionMode:  item.Runtime.PermissionMode,
			ClaudeLifecycle: item.Runtime.ClaudeLifecycle,
		},
	}
}

func toProtocolSummaries(items []store.SessionSummary) []protocol.SessionSummary {
	result := make([]protocol.SessionSummary, 0, len(items))
	for _, item := range items {
		result = append(result, toProtocolSummary(item))
	}
	return result
}

func toProtocolCatalogMetadata(meta store.CatalogMetadata) protocol.CatalogMetadata {
	lastSyncedAt := ""
	if !meta.LastSyncedAt.IsZero() {
		lastSyncedAt = meta.LastSyncedAt.Format(time.RFC3339)
	}
	return protocol.CatalogMetadata{
		Domain:        string(meta.Domain),
		SourceOfTruth: string(meta.SourceOfTruth),
		SyncState:     string(meta.SyncState),
		DriftDetected: meta.DriftDetected,
		LastSyncedAt:  lastSyncedAt,
		VersionToken:  meta.VersionToken,
		LastError:     meta.LastError,
	}
}

func toProtocolSessionContext(ctx store.SessionContext) protocol.SessionContext {
	return protocol.SessionContext{
		EnabledSkillNames: append([]string(nil), ctx.EnabledSkillNames...),
		EnabledMemoryIDs:  append([]string(nil), ctx.EnabledMemoryIDs...),
	}
}

func toProtocolSkillDefinitions(items []store.SkillDefinition) []protocol.SkillDefinition {
	result := make([]protocol.SkillDefinition, 0, len(items))
	for _, item := range items {
		updatedAt := ""
		if !item.UpdatedAt.IsZero() {
			updatedAt = item.UpdatedAt.Format(time.RFC3339)
		}
		lastSyncedAt := ""
		if !item.LastSyncedAt.IsZero() {
			lastSyncedAt = item.LastSyncedAt.Format(time.RFC3339)
		}
		result = append(result, protocol.SkillDefinition{
			Name:          item.Name,
			Description:   item.Description,
			Prompt:        item.Prompt,
			ResultView:    item.ResultView,
			TargetType:    item.TargetType,
			Source:        string(item.Source),
			SourceOfTruth: string(item.SourceOfTruth),
			SyncState:     string(item.SyncState),
			Editable:      item.Editable,
			DriftDetected: item.DriftDetected,
			UpdatedAt:     updatedAt,
			LastSyncedAt:  lastSyncedAt,
		})
	}
	return result
}

func toProtocolMemoryItems(items []store.MemoryItem) []protocol.MemoryItem {
	result := make([]protocol.MemoryItem, 0, len(items))
	for _, item := range items {
		updatedAt := ""
		if !item.UpdatedAt.IsZero() {
			updatedAt = item.UpdatedAt.Format(time.RFC3339)
		}
		lastSyncedAt := ""
		if !item.LastSyncedAt.IsZero() {
			lastSyncedAt = item.LastSyncedAt.Format(time.RFC3339)
		}
		result = append(result, protocol.MemoryItem{
			ID:            item.ID,
			Title:         item.Title,
			Content:       item.Content,
			Source:        item.Source,
			SourceOfTruth: string(item.SourceOfTruth),
			SyncState:     string(item.SyncState),
			Editable:      item.Editable,
			DriftDetected: item.DriftDetected,
			UpdatedAt:     updatedAt,
			LastSyncedAt:  lastSyncedAt,
		})
	}
	return result
}

func loadSelectedSessionRecord(sessionStore store.Store, ctx context.Context, sessionID string, emit func(any)) (store.SessionRecord, bool) {
	if sessionStore == nil {
		emit(protocol.NewErrorEvent(sessionID, "session store unavailable", ""))
		return store.SessionRecord{}, false
	}
	record, err := sessionStore.GetSession(ctx, sessionID)
	if err != nil {
		emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
		return store.SessionRecord{}, false
	}
	return record, true
}

func emitSkillCatalogResult(emit func(any), sessionStore store.Store, ctx context.Context, sessionID string) {
	if sessionStore == nil {
		emit(protocol.NewErrorEvent(sessionID, "session store unavailable", ""))
		return
	}
	snapshot, err := sessionStore.GetSkillCatalogSnapshot(ctx)
	if err != nil {
		emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
		return
	}
	emit(protocol.NewSkillCatalogResultEvent(sessionID, toProtocolCatalogMetadata(snapshot.Meta), toProtocolSkillDefinitions(snapshot.Items)))
}

func emitMemoryListResult(emit func(any), sessionStore store.Store, ctx context.Context, sessionID string) {
	if sessionStore == nil {
		emit(protocol.NewErrorEvent(sessionID, "session store unavailable", ""))
		return
	}
	snapshot, err := sessionStore.GetMemoryCatalogSnapshot(ctx)
	if err != nil {
		emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
		return
	}
	emit(protocol.NewMemoryListResultEvent(sessionID, toProtocolCatalogMetadata(snapshot.Meta), toProtocolMemoryItems(snapshot.Items)))
}

func upsertLocalSkill(sessionStore store.Store, ctx context.Context, item protocol.SkillDefinition) error {
	if sessionStore == nil {
		return fmt.Errorf("session store unavailable")
	}
	snapshot, err := sessionStore.GetSkillCatalogSnapshot(ctx)
	if err != nil {
		return err
	}
	updatedAt := time.Now().UTC()
	next := store.SkillDefinition{
		Name:          strings.TrimSpace(item.Name),
		Description:   strings.TrimSpace(item.Description),
		Prompt:        strings.TrimSpace(item.Prompt),
		ResultView:    strings.TrimSpace(item.ResultView),
		TargetType:    strings.TrimSpace(item.TargetType),
		Source:        store.SkillSourceLocal,
		SourceOfTruth: store.CatalogSourceTruthClaude,
		SyncState:     store.CatalogSyncStateDraft,
		Editable:      true,
		DriftDetected: true,
		UpdatedAt:     updatedAt,
	}
	if next.Name == "" {
		return fmt.Errorf("skill name is required")
	}
	found := false
	for i := range snapshot.Items {
		if snapshot.Items[i].Name == next.Name {
			snapshot.Items[i] = next
			found = true
			break
		}
	}
	if !found {
		snapshot.Items = append(snapshot.Items, next)
	}
	snapshot.Meta.SyncState = store.CatalogSyncStateDraft
	snapshot.Meta.DriftDetected = true
	snapshot.Meta.SourceOfTruth = store.CatalogSourceTruthClaude
	snapshot.Meta.LastError = ""
	return sessionStore.SaveSkillCatalogSnapshot(ctx, snapshot)
}

func syncExternalSkills(sessionStore store.Store, ctx context.Context) error {
	if sessionStore == nil {
		return fmt.Errorf("session store unavailable")
	}
	snapshot, err := sessionStore.GetSkillCatalogSnapshot(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	filtered := make([]store.SkillDefinition, 0, len(snapshot.Items)+1)
	for _, item := range snapshot.Items {
		if item.Source == store.SkillSourceLocal {
			filtered = append(filtered, item)
		}
	}
	filtered = append(filtered, store.SkillDefinition{
		Name:          "external-diff-summary",
		Description:   "外部同步示例 skill",
		Prompt:        "请总结下面上下文的关键变化、影响范围和验证建议。",
		ResultView:    "review-card",
		TargetType:    "diff",
		Source:        store.SkillSourceExternal,
		SourceOfTruth: store.CatalogSourceTruthClaude,
		SyncState:     store.CatalogSyncStateSynced,
		Editable:      false,
		DriftDetected: false,
		UpdatedAt:     now,
		LastSyncedAt:  now,
	})
	snapshot.Items = filtered
	snapshot.Meta.SourceOfTruth = store.CatalogSourceTruthClaude
	snapshot.Meta.SyncState = store.CatalogSyncStateSynced
	snapshot.Meta.DriftDetected = false
	snapshot.Meta.LastSyncedAt = now
	snapshot.Meta.LastError = ""
	snapshot.Meta.VersionToken = fmt.Sprintf("skills-%d", now.UnixNano())
	return sessionStore.SaveSkillCatalogSnapshot(ctx, snapshot)
}

func upsertMemoryItem(sessionStore store.Store, ctx context.Context, item protocol.MemoryItem) error {
	if sessionStore == nil {
		return fmt.Errorf("session store unavailable")
	}
	snapshot, err := sessionStore.GetMemoryCatalogSnapshot(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	next := store.MemoryItem{
		ID:            strings.TrimSpace(item.ID),
		Title:         strings.TrimSpace(item.Title),
		Content:       strings.TrimSpace(item.Content),
		Source:        firstNonEmptyString(strings.TrimSpace(item.Source), "local"),
		SourceOfTruth: store.CatalogSourceTruthClaude,
		SyncState:     store.CatalogSyncStateDraft,
		Editable:      true,
		DriftDetected: true,
		UpdatedAt:     now,
	}
	if next.ID == "" {
		next.ID = fmt.Sprintf("memory-%d", now.UnixNano())
	}
	if next.Title == "" {
		return fmt.Errorf("memory title is required")
	}
	found := false
	for i := range snapshot.Items {
		if snapshot.Items[i].ID == next.ID {
			next.LastSyncedAt = snapshot.Items[i].LastSyncedAt
			snapshot.Items[i] = next
			found = true
			break
		}
	}
	if !found {
		snapshot.Items = append(snapshot.Items, next)
	}
	snapshot.Meta.SourceOfTruth = store.CatalogSourceTruthClaude
	snapshot.Meta.SyncState = store.CatalogSyncStateDraft
	snapshot.Meta.DriftDetected = true
	snapshot.Meta.LastError = ""
	return sessionStore.SaveMemoryCatalogSnapshot(ctx, snapshot)
}

func syncExternalMemories(sessionStore store.Store, ctx context.Context) error {
	if sessionStore == nil {
		return fmt.Errorf("session store unavailable")
	}
	snapshot, err := sessionStore.GetMemoryCatalogSnapshot(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	filtered := make([]store.MemoryItem, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		if item.Source == "local" {
			item.SourceOfTruth = store.CatalogSourceTruthClaude
			item.SyncState = store.CatalogSyncStateDraft
			item.Editable = true
			item.DriftDetected = true
			filtered = append(filtered, item)
		}
	}
	snapshot.Items = filtered
	snapshot.Meta.SourceOfTruth = store.CatalogSourceTruthClaude
	snapshot.Meta.SyncState = store.CatalogSyncStateSynced
	snapshot.Meta.DriftDetected = false
	snapshot.Meta.LastSyncedAt = now
	snapshot.Meta.LastError = ""
	snapshot.Meta.VersionToken = fmt.Sprintf("memory-%d", now.UnixNano())
	return sessionStore.SaveMemoryCatalogSnapshot(ctx, snapshot)
}

func toHistoryContext(ctx *store.SnapshotContext) *protocol.HistoryContext {
	if ctx == nil {
		return nil
	}
	return &protocol.HistoryContext{
		ID:            ctx.ID,
		Type:          ctx.Type,
		Message:       ctx.Message,
		Status:        ctx.Status,
		Target:        ctx.Target,
		TargetPath:    ctx.TargetPath,
		Tool:          ctx.Tool,
		Command:       ctx.Command,
		Timestamp:     ctx.Timestamp,
		Title:         ctx.Title,
		Stack:         ctx.Stack,
		Code:          ctx.Code,
		RelatedStep:   ctx.RelatedStep,
		Path:          ctx.Path,
		Diff:          ctx.Diff,
		Lang:          ctx.Lang,
		PendingReview: ctx.PendingReview,
		Source:        ctx.Source,
		SkillName:     ctx.SkillName,
		ExecutionID:   ctx.ExecutionID,
		GroupID:       ctx.GroupID,
		GroupTitle:    ctx.GroupTitle,
		ReviewStatus:  ctx.ReviewStatus,
	}
}

func fromReviewFile(file session.ReviewFile) protocol.ReviewFile {
	return protocol.ReviewFile{
		ID:            file.ContextID,
		Path:          file.Path,
		Title:         file.Title,
		Diff:          file.Diff,
		Lang:          file.Lang,
		PendingReview: file.PendingReview,
		ReviewStatus:  file.ReviewStatus,
		ExecutionID:   file.ExecutionID,
	}
}

func fromReviewGroup(group *session.ReviewGroup) *protocol.ReviewGroup {
	if group == nil {
		return nil
	}
	files := make([]protocol.ReviewFile, 0, len(group.Files))
	for _, file := range group.Files {
		files = append(files, fromReviewFile(file))
	}
	return &protocol.ReviewGroup{
		ID:            group.ID,
		Title:         group.Title,
		ExecutionID:   group.ExecutionID,
		PendingReview: group.PendingReview,
		ReviewStatus:  group.ReviewStatus,
		CurrentFileID: group.CurrentFileID,
		CurrentPath:   group.CurrentPath,
		PendingCount:  group.PendingCount,
		AcceptedCount: group.AcceptedCount,
		RevertedCount: group.RevertedCount,
		RevisedCount:  group.RevisedCount,
		Files:         files,
	}
}

func fromReviewGroups(groups []session.ReviewGroup) []protocol.ReviewGroup {
	if len(groups) == 0 {
		return nil
	}
	result := make([]protocol.ReviewGroup, 0, len(groups))
	for _, group := range groups {
		item := fromReviewGroup(&group)
		if item != nil {
			result = append(result, *item)
		}
	}
	return result
}

func fromDiffContext(diff *session.DiffContext) *protocol.HistoryContext {
	if diff == nil {
		return nil
	}
	return &protocol.HistoryContext{
		ID:            diff.ContextID,
		Type:          "diff",
		Path:          diff.Path,
		Title:         diff.Title,
		Diff:          diff.Diff,
		Lang:          diff.Lang,
		PendingReview: diff.PendingReview,
		ExecutionID:   diff.ExecutionID,
		GroupID:       diff.GroupID,
		GroupTitle:    diff.GroupTitle,
		ReviewStatus:  diff.ReviewStatus,
	}
}

func fromDiffContexts(diffs []session.DiffContext) []protocol.HistoryContext {
	if len(diffs) == 0 {
		return nil
	}
	result := make([]protocol.HistoryContext, 0, len(diffs))
	for _, diff := range diffs {
		ctx := fromDiffContext(&diff)
		if ctx != nil {
			result = append(result, *ctx)
		}
	}
	return result
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func emitReviewStateFromProjection(emit func(any), sessionID string, projection store.ProjectionSnapshot) {
	projection = normalizeProjectionSnapshot(projection)
	emit(protocol.NewReviewStateEvent(
		sessionID,
		fromReviewGroups(projection.ReviewGroups),
		fromReviewGroup(projection.ActiveReviewGroup),
	))
}

func applyReviewDecisionToProjection(snapshot store.ProjectionSnapshot, reviewEvent protocol.ReviewDecisionRequestEvent, decision string, currentDiff session.DiffContext) store.ProjectionSnapshot {
	snapshot = normalizeProjectionSnapshot(snapshot)
	targetContextID := firstNonEmptyString(reviewEvent.ContextID, currentDiff.ContextID)
	targetPath := firstNonEmptyString(reviewEvent.TargetPath, currentDiff.Path)
	targetExecutionID := firstNonEmptyString(reviewEvent.ExecutionID, currentDiff.ExecutionID)
	targetGroupID := firstNonEmptyString(reviewEvent.GroupID, reviewEvent.ExecutionID, currentDiff.GroupID, targetContextID, targetPath)
	targetGroupTitle := firstNonEmptyString(reviewEvent.GroupTitle, currentDiff.GroupTitle, currentDiff.Title)
	reviewStatus := reviewStatusFromDecision(decision)
	pending := decision == "revise"

	for i := range snapshot.Diffs {
		item := &snapshot.Diffs[i]
		if !snapshotDiffMatches(*item, targetContextID, targetPath) {
			continue
		}
		item.PendingReview = pending
		item.ReviewStatus = reviewStatus
		if item.GroupID == "" {
			item.GroupID = targetGroupID
		}
		if item.GroupTitle == "" {
			item.GroupTitle = targetGroupTitle
		}
		if item.ExecutionID == "" {
			item.ExecutionID = targetExecutionID
		}
	}

	snapshot.ReviewGroups = upsertReviewGroupState(snapshot.ReviewGroups, snapshot.Diffs, targetGroupID, targetGroupTitle, targetExecutionID)
	active := pickActiveReviewGroup(snapshot.ReviewGroups)
	if active != nil {
		snapshot.ActiveReviewGroup = active
	}
	activeDiff := pickActiveSnapshotDiff(snapshot.Diffs)
	if strings.TrimSpace(activeDiff.ContextID+activeDiff.Path+activeDiff.Title) != "" {
		snapshot.CurrentDiff = &activeDiff
	}
	return snapshot
}

func reviewStatusFromDecision(decision string) string {
	switch strings.TrimSpace(strings.ToLower(decision)) {
	case "accept":
		return "accepted"
	case "revert":
		return "reverted"
	case "revise":
		return "revised"
	default:
		return "pending"
	}
}

func snapshotDiffMatches(item session.DiffContext, contextID, targetPath string) bool {
	if strings.TrimSpace(contextID) != "" && strings.TrimSpace(item.ContextID) == strings.TrimSpace(contextID) {
		return true
	}
	if strings.TrimSpace(targetPath) != "" && strings.TrimSpace(item.Path) == strings.TrimSpace(targetPath) {
		return true
	}
	return false
}

func upsertReviewGroupState(groups []session.ReviewGroup, diffs []session.DiffContext, targetGroupID, targetGroupTitle, targetExecutionID string) []session.ReviewGroup {
	groupID := strings.TrimSpace(targetGroupID)
	if groupID == "" {
		return rebuildReviewGroups(diffs)
	}
	return rebuildReviewGroups(diffs)
}

func rebuildReviewGroups(diffs []session.DiffContext) []session.ReviewGroup {
	if len(diffs) == 0 {
		return nil
	}
	groupOrder := make([]string, 0)
	byGroup := map[string][]session.DiffContext{}
	for _, diff := range diffs {
		groupID := firstNonEmptyString(diff.GroupID, diff.ExecutionID, diff.ContextID, diff.Path)
		if groupID == "" {
			continue
		}
		if _, ok := byGroup[groupID]; !ok {
			groupOrder = append(groupOrder, groupID)
		}
		if diff.GroupID == "" {
			diff.GroupID = groupID
		}
		if diff.GroupTitle == "" {
			diff.GroupTitle = firstNonEmptyString(diff.Title, diff.Path, groupID)
		}
		byGroup[groupID] = append(byGroup[groupID], diff)
	}
	groups := make([]session.ReviewGroup, 0, len(groupOrder))
	for _, groupID := range groupOrder {
		items := byGroup[groupID]
		if len(items) == 0 {
			continue
		}
		files := make([]session.ReviewFile, 0, len(items))
		pendingCount := 0
		acceptedCount := 0
		revertedCount := 0
		revisedCount := 0
		for _, item := range items {
			files = append(files, session.ReviewFile{
				ContextID:     item.ContextID,
				Title:         item.Title,
				Path:          item.Path,
				Diff:          item.Diff,
				Lang:          item.Lang,
				PendingReview: item.PendingReview,
				ExecutionID:   item.ExecutionID,
				ReviewStatus:  item.ReviewStatus,
			})
			if item.PendingReview {
				pendingCount++
			}
			switch strings.TrimSpace(item.ReviewStatus) {
			case "accepted":
				acceptedCount++
			case "reverted":
				revertedCount++
			case "revised":
				revisedCount++
			}
		}
		reviewStatus := "pending"
		switch {
		case pendingCount == len(files):
			reviewStatus = "pending"
		case acceptedCount == len(files):
			reviewStatus = "accepted"
		case revertedCount == len(files):
			reviewStatus = "reverted"
		case revisedCount == len(files):
			reviewStatus = "revised"
		default:
			reviewStatus = "mixed"
		}
		current := pickActiveReviewFile(files)
		groups = append(groups, session.ReviewGroup{
			ID:            groupID,
			Title:         firstNonEmptyString(items[len(items)-1].GroupTitle, items[len(items)-1].Title, items[len(items)-1].Path, groupID),
			ExecutionID:   firstNonEmptyString(items[len(items)-1].ExecutionID, groupID),
			PendingReview: pendingCount > 0,
			ReviewStatus:  reviewStatus,
			CurrentFileID: current.ContextID,
			CurrentPath:   current.Path,
			PendingCount:  pendingCount,
			AcceptedCount: acceptedCount,
			RevertedCount: revertedCount,
			RevisedCount:  revisedCount,
			Files:         files,
		})
	}
	return groups
}

func pickActiveReviewFile(files []session.ReviewFile) session.ReviewFile {
	for _, file := range files {
		if file.PendingReview {
			return file
		}
	}
	if len(files) > 0 {
		return files[len(files)-1]
	}
	return session.ReviewFile{}
}

func pickActiveReviewGroup(groups []session.ReviewGroup) *session.ReviewGroup {
	for i := len(groups) - 1; i >= 0; i-- {
		if groups[i].PendingReview {
			group := groups[i]
			return &group
		}
	}
	if len(groups) > 0 {
		group := groups[len(groups)-1]
		return &group
	}
	return nil
}

func newSessionHistoryEventFromRecord(record store.SessionRecord) protocol.SessionHistoryEvent {
	projection := normalizeProjectionSnapshot(record.Projection)
	entries := make([]protocol.HistoryLogEntry, 0, len(projection.LogEntries))
	for _, entry := range projection.LogEntries {
		entries = append(entries, protocol.HistoryLogEntry{
			Kind:        entry.Kind,
			Message:     entry.Message,
			Label:       entry.Label,
			Timestamp:   entry.Timestamp,
			Stream:      entry.Stream,
			Text:        entry.Text,
			ExecutionID: entry.ExecutionID,
			Phase:       entry.Phase,
			ExitCode:    entry.ExitCode,
			Context:     toHistoryContext(entry.Context),
		})
	}
	executions := make([]protocol.TerminalExecution, 0, len(projection.TerminalExecutions))
	for _, item := range projection.TerminalExecutions {
		executions = append(executions, protocol.TerminalExecution{
			ExecutionID: item.ExecutionID,
			Command:     item.Command,
			CWD:         item.CWD,
			StartedAt:   item.StartedAt,
			FinishedAt:  item.FinishedAt,
			ExitCode:    item.ExitCode,
			Stdout:      item.Stdout,
			Stderr:      item.Stderr,
		})
	}
	resumeMeta := protocol.RuntimeMeta{
		ResumeSessionID: projection.Runtime.ResumeSessionID,
		Command:         projection.Runtime.Command,
		Engine:          projection.Runtime.Engine,
		CWD:             projection.Runtime.CWD,
		PermissionMode:  projection.Runtime.PermissionMode,
		ClaudeLifecycle: normalizeProjectionLifecycle(projection.Runtime.ClaudeLifecycle, projection.Runtime.ResumeSessionID),
	}
	canResume := strings.TrimSpace(resumeMeta.ResumeSessionID) != ""
	return protocol.NewSessionHistoryEvent(
		record.Summary.ID,
		toProtocolSummary(record.Summary),
		entries,
		fromDiffContexts(projection.Diffs),
		fromDiffContext(projection.CurrentDiff),
		fromReviewGroups(projection.ReviewGroups),
		fromReviewGroup(projection.ActiveReviewGroup),
		toHistoryContext(projection.CurrentStep),
		toHistoryContext(projection.LatestError),
		projection.RawTerminalByStream,
		executions,
		toProtocolSessionContext(projection.SessionContext),
		toProtocolCatalogMetadata(projection.SkillCatalogMeta),
		toProtocolCatalogMetadata(projection.MemoryCatalogMeta),
		canResume,
		resumeMeta,
	)
}

func applyEventToProjection(snapshot store.ProjectionSnapshot, event any) (store.ProjectionSnapshot, bool) {
	snapshot = normalizeProjectionSnapshot(snapshot)
	switch e := event.(type) {
	case protocol.SessionStateEvent:
		if e.Message != "" {
			snapshot.LogEntries = append(snapshot.LogEntries, store.SnapshotLogEntry{Kind: "system", Message: e.Message, Timestamp: e.Timestamp.Format(time.RFC3339)})
		}
		return snapshot, true
	case protocol.LogEvent:
		phase := strings.TrimSpace(e.Phase)
		msg := strings.TrimSpace(e.Message)
		entry := store.SnapshotLogEntry{
			Kind:        "terminal",
			Message:     e.Message,
			Timestamp:   e.Timestamp.Format(time.RFC3339),
			Stream:      e.Stream,
			Text:        strings.TrimLeft(e.Message, "\r"),
			ExecutionID: e.ExecutionID,
			Phase:       phase,
			ExitCode:    e.ExitCode,
		}
		if phase == "started" {
			snapshot.TerminalExecutions = upsertTerminalExecution(snapshot.TerminalExecutions, store.TerminalExecution{
				ExecutionID: e.ExecutionID,
				Command:     firstNonEmptyString(e.Command, e.Message),
				CWD:         e.CWD,
				StartedAt:   e.Timestamp.Format(time.RFC3339),
			})
			snapshot.LogEntries = append(snapshot.LogEntries, entry)
			return snapshot, true
		}
		if phase == "finished" {
			snapshot.TerminalExecutions = updateTerminalExecution(snapshot.TerminalExecutions, e.ExecutionID, func(item *store.TerminalExecution) {
				if item.StartedAt == "" {
					item.StartedAt = e.Timestamp.Format(time.RFC3339)
				}
				item.FinishedAt = e.Timestamp.Format(time.RFC3339)
				item.ExitCode = e.ExitCode
			})
			snapshot.LogEntries = append(snapshot.LogEntries, entry)
			return snapshot, true
		}
		if msg == "" {
			return snapshot, false
		}
		if e.Stream != "stderr" && looksLikeMarkdownMessage(msg) {
			snapshot.LogEntries = append(snapshot.LogEntries, store.SnapshotLogEntry{Kind: "markdown", Message: e.Message, Timestamp: e.Timestamp.Format(time.RFC3339), Stream: e.Stream, ExecutionID: e.ExecutionID, Phase: phase, ExitCode: e.ExitCode})
		} else {
			previousIndex := len(snapshot.LogEntries) - 1
			if previousIndex >= 0 && snapshot.LogEntries[previousIndex].Kind == "terminal" && snapshot.LogEntries[previousIndex].Stream == e.Stream && snapshot.LogEntries[previousIndex].ExecutionID == e.ExecutionID && snapshot.LogEntries[previousIndex].Phase == phase {
				prev := snapshot.LogEntries[previousIndex]
				if prev.Text != "" {
					prev.Text += "\n"
				}
				prev.Text += strings.TrimLeft(e.Message, "\r")
				prev.Timestamp = e.Timestamp.Format(time.RFC3339)
				snapshot.LogEntries[previousIndex] = prev
			} else {
				snapshot.LogEntries = append(snapshot.LogEntries, entry)
			}
			stream := fallback(e.Stream, "stdout")
			if snapshot.RawTerminalByStream[stream] != "" {
				snapshot.RawTerminalByStream[stream] += "\n"
			}
			snapshot.RawTerminalByStream[stream] += strings.TrimLeft(e.Message, "\r")
			snapshot.TerminalExecutions = updateTerminalExecution(snapshot.TerminalExecutions, e.ExecutionID, func(item *store.TerminalExecution) {
				if item.ExecutionID == "" {
					item.ExecutionID = e.ExecutionID
				}
				if item.Command == "" {
					item.Command = e.Command
				}
				if item.CWD == "" {
					item.CWD = e.CWD
				}
				if item.StartedAt == "" {
					item.StartedAt = e.Timestamp.Format(time.RFC3339)
				}
				appendExecutionStream(item, stream, strings.TrimLeft(e.Message, "\r"))
			})
		}
		return snapshot, true
	case protocol.ErrorEvent:
		ctx := &store.SnapshotContext{ID: firstNonEmptyString(e.ContextID, fmt.Sprintf("error:%s", e.Timestamp.Format(time.RFC3339Nano))), Message: e.Message, Stack: e.Stack, Code: e.Code, TargetPath: firstNonEmptyString(e.TargetPath, e.RuntimeMeta.TargetPath), RelatedStep: e.Step, Command: e.Command, Timestamp: e.Timestamp.Format(time.RFC3339), Title: firstNonEmptyString(e.ContextTitle, e.Message)}
		snapshot.LatestError = ctx
		snapshot.LogEntries = append(snapshot.LogEntries, store.SnapshotLogEntry{Kind: "error", Context: ctx})
		return snapshot, true
	case protocol.StepUpdateEvent:
		ctx := &store.SnapshotContext{ID: firstNonEmptyString(e.ContextID, fmt.Sprintf("step:%s", e.Timestamp.Format(time.RFC3339Nano))), Type: "step", Message: e.Message, Status: e.Status, Target: e.Target, TargetPath: firstNonEmptyString(e.TargetPath, e.Target), Tool: e.Tool, Command: e.Command, Timestamp: e.Timestamp.Format(time.RFC3339), Title: firstNonEmptyString(e.ContextTitle, e.Message, "当前步骤")}
		snapshot.CurrentStep = ctx
		snapshot.LogEntries = append(snapshot.LogEntries, store.SnapshotLogEntry{Kind: "step", Context: ctx})
		return snapshot, true
	case protocol.FileDiffEvent:
		diff := session.DiffContext{ContextID: firstNonEmptyString(e.ContextID, e.Path, e.Title), Title: firstNonEmptyString(e.Title, e.ContextTitle, "最近改动"), Path: firstNonEmptyString(e.Path, e.TargetPath), Diff: e.Diff, Lang: e.Lang, PendingReview: true, ExecutionID: e.ExecutionID, GroupID: firstNonEmptyString(e.GroupID, e.ExecutionID, e.ContextID, e.Path), GroupTitle: firstNonEmptyString(e.GroupTitle, e.ContextTitle, e.Title), ReviewStatus: "pending"}
		snapshot.Diffs = upsertSnapshotDiff(snapshot.Diffs, diff)
		snapshot.ReviewGroups = rebuildReviewGroups(snapshot.Diffs)
		activeGroup := pickActiveReviewGroup(snapshot.ReviewGroups)
		snapshot.ActiveReviewGroup = activeGroup
		active := pickActiveSnapshotDiff(snapshot.Diffs)
		if strings.TrimSpace(active.ContextID+active.Path+active.Title) != "" {
			snapshot.CurrentDiff = &active
		}
		snapshot.LogEntries = append(snapshot.LogEntries, store.SnapshotLogEntry{Kind: "diff", Context: &store.SnapshotContext{ID: diff.ContextID, Path: diff.Path, Title: diff.Title, Diff: diff.Diff, Lang: diff.Lang, PendingReview: diff.PendingReview, Timestamp: e.Timestamp.Format(time.RFC3339), Source: e.Source, SkillName: e.SkillName, ExecutionID: diff.ExecutionID, GroupID: diff.GroupID, GroupTitle: diff.GroupTitle, ReviewStatus: diff.ReviewStatus}})
		return snapshot, true
	case protocol.AgentStateEvent:
		return snapshot, false
	case protocol.PromptRequestEvent:
		return snapshot, false
	default:
		return snapshot, false
	}
}

func normalizeProjectionSnapshot(snapshot store.ProjectionSnapshot) store.ProjectionSnapshot {
	if snapshot.RawTerminalByStream == nil {
		snapshot.RawTerminalByStream = map[string]string{"stdout": "", "stderr": ""}
	}
	if snapshot.LogEntries == nil {
		snapshot.LogEntries = []store.SnapshotLogEntry{}
	}
	if snapshot.TerminalExecutions == nil {
		snapshot.TerminalExecutions = []store.TerminalExecution{}
	}
	if snapshot.Runtime.ResumeSessionID == "" {
		snapshot.Runtime.ResumeSessionID = snapshot.Controller.ResumeSession
	}
	if snapshot.Runtime.Command == "" {
		snapshot.Runtime.Command = snapshot.Controller.CurrentCommand
	}
	if snapshot.Runtime.Engine == "" {
		snapshot.Runtime.Engine = firstNonEmptyString(snapshot.Controller.ActiveMeta.Engine, snapshot.Controller.ActiveMeta.SkillName)
	}
	if snapshot.Runtime.CWD == "" {
		snapshot.Runtime.CWD = snapshot.Controller.ActiveMeta.CWD
	}
	if snapshot.Runtime.PermissionMode == "" {
		snapshot.Runtime.PermissionMode = snapshot.Controller.ActiveMeta.PermissionMode
	}
	snapshot.Runtime.ClaudeLifecycle = normalizeProjectionLifecycle(
		firstNonEmptyString(snapshot.Controller.ClaudeLifecycle, snapshot.Controller.ActiveMeta.ClaudeLifecycle, snapshot.Runtime.ClaudeLifecycle),
		snapshot.Runtime.ResumeSessionID,
	)
	if snapshot.Runtime.ClaudeLifecycle != "" {
		snapshot.Controller.ClaudeLifecycle = snapshot.Runtime.ClaudeLifecycle
		snapshot.Controller.ActiveMeta.ClaudeLifecycle = snapshot.Runtime.ClaudeLifecycle
	}
	if len(snapshot.SessionContext.EnabledSkillNames) == 0 && len(snapshot.SessionContext.EnabledMemoryIDs) == 0 {
		snapshot.SessionContext = store.SessionContext{}
	}
	if len(snapshot.Diffs) == 0 && snapshot.CurrentDiff != nil {
		snapshot.Diffs = []session.DiffContext{*snapshot.CurrentDiff}
	}
	if len(snapshot.ReviewGroups) == 0 && len(snapshot.Diffs) > 0 {
		snapshot.ReviewGroups = rebuildReviewGroups(snapshot.Diffs)
	}
	activeGroup := pickActiveReviewGroup(snapshot.ReviewGroups)
	if activeGroup != nil {
		snapshot.ActiveReviewGroup = activeGroup
	}
	activeDiff := pickActiveSnapshotDiff(snapshot.Diffs)
	if strings.TrimSpace(activeDiff.ContextID+activeDiff.Path+activeDiff.Title) != "" {
		snapshot.CurrentDiff = &activeDiff
	}
	return snapshot
}

func normalizeProjectionLifecycle(lifecycle string, resumeSessionID string) string {
	normalized := strings.TrimSpace(lifecycle)
	if normalized == "starting" && strings.TrimSpace(resumeSessionID) != "" {
		return "resumable"
	}
	return normalized
}

func upsertTerminalExecution(items []store.TerminalExecution, next store.TerminalExecution) []store.TerminalExecution {
	if strings.TrimSpace(next.ExecutionID) == "" {
		return items
	}
	for i := range items {
		if items[i].ExecutionID == next.ExecutionID {
			if next.Command != "" {
				items[i].Command = next.Command
			}
			if next.CWD != "" {
				items[i].CWD = next.CWD
			}
			if next.StartedAt != "" {
				items[i].StartedAt = next.StartedAt
			}
			if next.FinishedAt != "" {
				items[i].FinishedAt = next.FinishedAt
			}
			if next.ExitCode != nil {
				items[i].ExitCode = next.ExitCode
			}
			if next.Stdout != "" {
				appendExecutionStream(&items[i], "stdout", next.Stdout)
			}
			if next.Stderr != "" {
				appendExecutionStream(&items[i], "stderr", next.Stderr)
			}
			return items
		}
	}
	return append(items, next)
}

func updateTerminalExecution(items []store.TerminalExecution, executionID string, mutate func(item *store.TerminalExecution)) []store.TerminalExecution {
	if strings.TrimSpace(executionID) == "" {
		return items
	}
	for i := range items {
		if items[i].ExecutionID == executionID {
			mutate(&items[i])
			return items
		}
	}
	item := store.TerminalExecution{ExecutionID: executionID}
	mutate(&item)
	return append(items, item)
}

func appendExecutionStream(item *store.TerminalExecution, stream string, text string) {
	if item == nil || text == "" {
		return
	}
	switch stream {
	case "stderr":
		if item.Stderr != "" {
			item.Stderr += "\n"
		}
		item.Stderr += text
	default:
		if item.Stdout != "" {
			item.Stdout += "\n"
		}
		item.Stdout += text
	}
}

func upsertSnapshotDiff(diffs []session.DiffContext, diff session.DiffContext) []session.DiffContext {
	for i := range diffs {
		item := diffs[i]
		if (strings.TrimSpace(diff.ContextID) != "" && strings.TrimSpace(item.ContextID) == strings.TrimSpace(diff.ContextID)) ||
			(strings.TrimSpace(diff.Path) != "" && strings.TrimSpace(item.Path) == strings.TrimSpace(diff.Path)) {
			diffs[i] = diff
			return diffs
		}
	}
	return append(diffs, diff)
}

func pickActiveSnapshotDiff(diffs []session.DiffContext) session.DiffContext {
	for i := len(diffs) - 1; i >= 0; i-- {
		if diffs[i].PendingReview {
			return diffs[i]
		}
	}
	if len(diffs) > 0 {
		return diffs[len(diffs)-1]
	}
	return session.DiffContext{}
}

func looksLikeMarkdownMessage(message string) bool {
	if strings.TrimSpace(message) == "" {
		return false
	}
	return strings.Contains(message, "```") || strings.Contains(message, "# ") || strings.Contains(message, "## ") || strings.Contains(message, "- ") || len(message) > 180
}

func parseMode(raw string) (runner.Mode, error) {
	return runtimepkg.ParseMode(raw)
}

func buildReviewDecisionPrompt(decision string, req protocol.ReviewDecisionRequestEvent) (string, error) {
	decision = strings.TrimSpace(strings.ToLower(decision))
	if decision == "" {
		return "", fmt.Errorf("review decision is required")
	}
	subject := strings.TrimSpace(req.TargetPath)
	if subject == "" {
		subject = strings.TrimSpace(req.ContextTitle)
	}
	if subject == "" {
		subject = "当前 diff"
	}
	switch decision {
	case "accept":
		return fmt.Sprintf("请接受刚刚展示的 diff 变更，并继续保存当前修改。目标：%s\n", subject), nil
	case "revert":
		return fmt.Sprintf("请撤回刚刚展示的 diff 变更，不要保留这次修改。目标：%s\n", subject), nil
	case "revise":
		return fmt.Sprintf("请基于刚刚展示的 diff 继续调整并重新修改。目标：%s\n", subject), nil
	default:
		return "", fmt.Errorf("review decision must be one of: accept, revert, revise")
	}
}

func waitForInteractiveRuntime(ctx context.Context, service *runtimepkg.Service, timeout time.Duration) error {
	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if service.CanAcceptInteractiveInput() {
			return nil
		}
		if !service.IsRunning() {
			return runtimepkg.ErrNoActiveRunner
		}
		select {
		case <-deadlineCtx.Done():
			if service.CanAcceptInteractiveInput() {
				return nil
			}
			if !service.IsRunning() {
				return runtimepkg.ErrNoActiveRunner
			}
			return runtimepkg.ErrRunnerNotInteractive
		case <-ticker.C:
		}
	}
}

func listDirectory(sessionID, rawPath string) (protocol.FSListResultEvent, error) {
	target := strings.TrimSpace(rawPath)
	if target == "" {
		target = "."
	}

	cleanTarget := filepath.Clean(target)
	absPath, err := filepath.Abs(cleanTarget)
	if err != nil {
		return protocol.FSListResultEvent{}, err
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return protocol.FSListResultEvent{}, err
	}

	items := make([]protocol.FSItem, 0, len(entries))
	for _, entry := range entries {
		item := protocol.FSItem{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
		}
		if info, err := entry.Info(); err == nil {
			item.Size = info.Size()
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})

	return protocol.NewFSListResultEvent(sessionID, absPath, items), nil
}

func readFile(sessionID, rawPath string) (protocol.FSReadResultEvent, error) {
	target := strings.TrimSpace(rawPath)
	if target == "" {
		return protocol.FSReadResultEvent{}, fmt.Errorf("path is required")
	}

	cleanTarget := filepath.Clean(target)
	absPath, err := filepath.Abs(cleanTarget)
	if err != nil {
		return protocol.FSReadResultEvent{}, err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return protocol.FSReadResultEvent{}, err
	}
	if info.IsDir() {
		return protocol.FSReadResultEvent{}, fmt.Errorf("path is a directory")
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return protocol.FSReadResultEvent{}, err
	}

	isText := !hasBinaryContent(content)
	textContent := string(content)
	if !isText {
		textContent = ""
	}
	return protocol.NewFSReadResultEvent(sessionID, absPath, textContent, info.Size(), detectLangFromPath(absPath), "utf-8", isText), nil
}

func detectLangFromPath(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch ext {
	case "js", "jsx":
		return "javascript"
	case "ts", "tsx":
		return "typescript"
	case "json":
		return "json"
	case "jsonc":
		return "jsonc"
	case "md":
		return "markdown"
	case "go":
		return "go"
	case "py":
		return "python"
	case "java":
		return "java"
	case "kt":
		return "kotlin"
	case "swift":
		return "swift"
	case "html":
		return "html"
	case "css", "scss":
		return ext
	case "yml", "yaml":
		return "yaml"
	case "xml":
		return "xml"
	case "sh", "bash":
		return "bash"
	case "sql":
		return "sql"
	case "txt":
		return "plaintext"
	default:
		return "plaintext"
	}
}

func hasBinaryContent(content []byte) bool {
	limit := len(content)
	if limit > 1024 {
		limit = 1024
	}
	for i := 0; i < limit; i++ {
		if content[i] == 0 {
			return true
		}
	}
	return false
}
