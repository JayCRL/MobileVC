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
		SkillLauncher: skills.NewLauncher(),
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
	selectedSessionID := connectionID
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
			emit(event)
			snapshot, ok := applyEventToProjection(buildProjectionSnapshotFor(sessionID), event)
			if ok {
				persistProjectionFor(sessionID, snapshot)
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
			fallbackSessionID := connectionID
			for _, item := range items {
				if strings.TrimSpace(item.ID) != "" {
					fallbackSessionID = item.ID
					break
				}
			}
			switchRuntimeSession(fallbackSessionID)
			if fallbackSessionID == connectionID {
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
		case "exec":
			var reqEvent protocol.ExecRequestEvent
			if err := json.Unmarshal(payload, &reqEvent); err != nil {
				logx.Warn("ws", "invalid exec request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid exec request: %v", err), ""))
				continue
			}
			if strings.TrimSpace(reqEvent.Command) == "" {
				logx.Warn("ws", "reject empty exec command: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
				emit(protocol.NewErrorEvent(selectedSessionID, "cmd is required", ""))
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
			if inputEvent.Data == "" {
				logx.Warn("ws", "reject empty input payload: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
				emit(protocol.NewErrorEvent(selectedSessionID, "input data is required", ""))
				continue
			}
			sessionID := selectedSessionID
			service := runtimeSvc
			emitAndPersist := emitAndPersistFor(sessionID)
			appendUserProjectionEntry(h.SessionStore, ctx, sessionID, strings.TrimRight(inputEvent.Data, "\n"), "回复", connectionID, remoteAddr)
			inputMeta := protocol.RuntimeMeta{}
			if pm := inputEvent.PermissionMode; pm != "" {
				service.UpdatePermissionMode(pm)
				inputMeta.PermissionMode = pm
			}
			if err := service.SendInput(ctx, sessionID, runtimepkg.InputRequest{Data: inputEvent.Data, RuntimeMeta: inputMeta}, emitAndPersist); err != nil {
				message := err.Error()
				if errors.Is(err, runner.ErrInputNotSupported) {
					message = "input is only supported for pty sessions"
				}
				logx.Warn("ws", "service send input failed: connectionID=%s sessionID=%s remoteAddr=%s action=input err=%v", connectionID, sessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(sessionID, message, ""))
			}
		case "review_decision":
			var reviewEvent protocol.ReviewDecisionRequestEvent
			if err := json.Unmarshal(payload, &reviewEvent); err != nil {
				logx.Warn("ws", "invalid review decision request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid review decision request: %v", err), ""))
				continue
			}
			decision := strings.TrimSpace(strings.ToLower(reviewEvent.Decision))
			if decision != "accept" && decision != "revert" && decision != "revise" {
				logx.Warn("ws", "reject invalid review decision: connectionID=%s sessionID=%s remoteAddr=%s decision=%q", connectionID, selectedSessionID, remoteAddr, reviewEvent.Decision)
				emit(protocol.NewErrorEvent(selectedSessionID, "review decision must be one of: accept, revert, revise", ""))
				continue
			}
			sessionID := selectedSessionID
			service := runtimeSvc
			emitAndPersist := emitAndPersistFor(sessionID)
			effectivePermissionMode := strings.TrimSpace(reviewEvent.PermissionMode)
			if effectivePermissionMode == "" {
				effectivePermissionMode = strings.TrimSpace(service.ControllerSnapshot().ActiveMeta.PermissionMode)
			}
			if effectivePermissionMode == "" {
				effectivePermissionMode = strings.TrimSpace(buildProjectionSnapshotFor(sessionID).Runtime.PermissionMode)
			}
			if decision == "accept" && effectivePermissionMode != "acceptEdits" {
				logx.Warn("ws", "reject accept review decision outside acceptEdits: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, sessionID, remoteAddr)
				emit(protocol.NewErrorEvent(sessionID, "当前 permission mode 不是 acceptEdits，不能直接 accept diff", ""))
				continue
			}
			if effectivePermissionMode != "" {
				service.UpdatePermissionMode(effectivePermissionMode)
			}
			prompt, err := buildReviewDecisionPrompt(decision, reviewEvent)
			if err != nil {
				logx.Warn("ws", "build review decision prompt failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
				continue
			}
			if err := service.SendInput(ctx, sessionID, runtimepkg.InputRequest{
				Data: prompt,
				RuntimeMeta: protocol.RuntimeMeta{
					Source:         "review-decision",
					ContextID:      reviewEvent.ContextID,
					ContextTitle:   reviewEvent.ContextTitle,
					TargetPath:     reviewEvent.TargetPath,
					TargetText:     decision,
					PermissionMode: effectivePermissionMode,
				},
			}, emitAndPersist); err != nil {
				message := err.Error()
				if errors.Is(err, runner.ErrInputNotSupported) {
					message = "当前会话不支持交互输入，请先恢复 Claude PTY 会话"
				} else if strings.Contains(message, "no active runner") {
					message = "当前没有可交互会话，请先恢复会话后再审核 diff"
				}
				logx.Warn("ws", "send review decision failed: connectionID=%s sessionID=%s remoteAddr=%s decision=%s err=%v", connectionID, sessionID, remoteAddr, decision, err)
				emit(protocol.NewErrorEvent(sessionID, message, ""))
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
			sessionID := selectedSessionID
			service := runtimeSvc
			emitAndPersist := emitAndPersistFor(sessionID)
			if err := executeSkillRequest(ctx, sessionID, skillEvent, service, h.SkillLauncher, emitAndPersist); err != nil {
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
			sessionID := selectedSessionID
			service := runtimeSvc
			emitAndPersist := emitAndPersistFor(sessionID)
			if err := handleSlashCommand(ctx, sessionID, slashReq, service, h.SkillLauncher, emitAndPersist); err != nil {
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
	snapshot.Controller = controller
	snapshot.Runtime = store.SessionRuntime{
		ResumeSessionID: firstNonEmptyString(controller.ResumeSession, runtimeMeta.ResumeSessionID),
		Command:         firstNonEmptyString(controller.CurrentCommand, runtimeMeta.Command),
		Engine:          firstNonEmptyString(runtimeMeta.Engine, runtimeMeta.SkillName),
		PermissionMode:  runtimeMeta.PermissionMode,
		CWD:             runtimeMeta.CWD,
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
	}
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

func newSessionHistoryEventFromRecord(record store.SessionRecord) protocol.SessionHistoryEvent {
	entries := make([]protocol.HistoryLogEntry, 0, len(record.Projection.LogEntries))
	for _, entry := range record.Projection.LogEntries {
		entries = append(entries, protocol.HistoryLogEntry{
			Kind:      entry.Kind,
			Message:   entry.Message,
			Label:     entry.Label,
			Timestamp: entry.Timestamp,
			Stream:    entry.Stream,
			Text:      entry.Text,
			Context:   toHistoryContext(entry.Context),
		})
	}
	resumeMeta := protocol.RuntimeMeta{
		ResumeSessionID: record.Projection.Runtime.ResumeSessionID,
		Command:         record.Projection.Runtime.Command,
		Engine:          record.Projection.Runtime.Engine,
		CWD:             record.Projection.Runtime.CWD,
		PermissionMode:  record.Projection.Runtime.PermissionMode,
	}
	canResume := strings.TrimSpace(resumeMeta.ResumeSessionID) != ""
	return protocol.NewSessionHistoryEvent(
		record.Summary.ID,
		toProtocolSummary(record.Summary),
		entries,
		fromDiffContexts(record.Projection.Diffs),
		fromDiffContext(record.Projection.CurrentDiff),
		toHistoryContext(record.Projection.CurrentStep),
		toHistoryContext(record.Projection.LatestError),
		record.Projection.RawTerminalByStream,
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
		msg := strings.TrimSpace(e.Message)
		if msg == "" {
			return snapshot, false
		}
		if e.Stream != "stderr" && looksLikeMarkdownMessage(msg) {
			snapshot.LogEntries = append(snapshot.LogEntries, store.SnapshotLogEntry{Kind: "markdown", Message: e.Message, Timestamp: e.Timestamp.Format(time.RFC3339), Stream: e.Stream})
		} else {
			previousIndex := len(snapshot.LogEntries) - 1
			if previousIndex >= 0 && snapshot.LogEntries[previousIndex].Kind == "terminal" && snapshot.LogEntries[previousIndex].Stream == e.Stream {
				prev := snapshot.LogEntries[previousIndex]
				if prev.Text != "" {
					prev.Text += "\n"
				}
				prev.Text += strings.TrimLeft(e.Message, "\r")
				prev.Timestamp = e.Timestamp.Format(time.RFC3339)
				snapshot.LogEntries[previousIndex] = prev
			} else {
				snapshot.LogEntries = append(snapshot.LogEntries, store.SnapshotLogEntry{Kind: "terminal", Text: strings.TrimLeft(e.Message, "\r"), Timestamp: e.Timestamp.Format(time.RFC3339), Stream: e.Stream})
			}
			stream := fallback(e.Stream, "stdout")
			if snapshot.RawTerminalByStream[stream] != "" {
				snapshot.RawTerminalByStream[stream] += "\n"
			}
			snapshot.RawTerminalByStream[stream] += strings.TrimLeft(e.Message, "\r")
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
		diff := session.DiffContext{ContextID: firstNonEmptyString(e.ContextID, e.Path, e.Title), Title: firstNonEmptyString(e.Title, e.ContextTitle, "最近改动"), Path: firstNonEmptyString(e.Path, e.TargetPath), Diff: e.Diff, Lang: e.Lang, PendingReview: true}
		snapshot.Diffs = upsertSnapshotDiff(snapshot.Diffs, diff)
		active := pickActiveSnapshotDiff(snapshot.Diffs)
		if strings.TrimSpace(active.ContextID+active.Path+active.Title) != "" {
			snapshot.CurrentDiff = &active
		}
		snapshot.LogEntries = append(snapshot.LogEntries, store.SnapshotLogEntry{Kind: "diff", Context: &store.SnapshotContext{ID: diff.ContextID, Path: diff.Path, Title: diff.Title, Diff: diff.Diff, Lang: diff.Lang, PendingReview: diff.PendingReview, Timestamp: e.Timestamp.Format(time.RFC3339), Source: e.Source, SkillName: e.SkillName}})
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
	if len(snapshot.Diffs) == 0 && snapshot.CurrentDiff != nil {
		snapshot.Diffs = []session.DiffContext{*snapshot.CurrentDiff}
	}
	activeDiff := pickActiveSnapshotDiff(snapshot.Diffs)
	if strings.TrimSpace(activeDiff.ContextID+activeDiff.Path+activeDiff.Title) != "" {
		snapshot.CurrentDiff = &activeDiff
	}
	return snapshot
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
