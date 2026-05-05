package gateway

import (
	"context"
	"encoding/base64"
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

	"mobilevc/internal/adb"
	"mobilevc/internal/data"
	"mobilevc/internal/data/claudesync"
	"mobilevc/internal/data/codexsync"
	"mobilevc/internal/data/skills"
	"mobilevc/internal/engine"
	"mobilevc/internal/logx"
	"mobilevc/internal/protocol"
	"mobilevc/internal/push"
	"mobilevc/internal/session"
)

const wsDebugPreviewLimit = 240

func normalizePermissionModeForClaude(mode string) string {
	return session.NormalizeClaudePermissionMode(mode)
}

type Handler struct {
	AuthToken       string
	NewExecRunner   func() engine.Runner
	NewPtyRunner    func() engine.Runner
	Upgrader        websocket.Upgrader
	SkillLauncher   *skills.Launcher
	SessionStore    data.Store
	PushService     push.Service
	runtimeSessions *runtimeSessionRegistry

	muProgressPush   sync.Mutex
	lastProgressPush map[string]time.Time
}

func NewHandler(authToken string, sessionStore data.Store) *Handler {
	handler := &Handler{
		AuthToken: authToken,
		NewExecRunner: func() engine.Runner {
			return engine.NewExecRunner()
		},
		NewPtyRunner: func() engine.Runner {
			return engine.NewPtyRunner()
		},
		SkillLauncher:    skills.NewLauncher(sessionStore),
		SessionStore:     sessionStore,
		PushService:      &push.NoopService{},
		lastProgressPush: make(map[string]time.Time),
		Upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
	handler.runtimeSessions = newRuntimeSessionRegistry(func(sessionID string) *session.Service {
		return session.NewService(sessionID, session.Dependencies{
			NewExecRunner: handler.NewExecRunner,
			NewPtyRunner:  handler.NewPtyRunner,
		})
	}, defaultRuntimeSessionReleaseAfter, func(sessionID string) {
		if sessionStore == nil || strings.TrimSpace(sessionID) == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		record, err := sessionStore.GetSession(ctx, sessionID)
		if err != nil {
			return
		}
		record.Summary.ExecutionActive = false
		sessionStore.UpsertSession(ctx, record)
	})
	return handler
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	connectionID := fmt.Sprintf("conn-%d", time.Now().UTC().UnixNano())
	selectedSessionID := ""
	remoteAddr := r.RemoteAddr
	connected := false
	var conn *websocket.Conn
	sessionListFilterCWD := ""

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

	// 设置 ping handler，确保及时响应客户端的 ping 消息
	conn.SetPingHandler(func(appData string) error {
		// 响应 pong
		if err := conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(10*time.Second)); err != nil {
			logx.Warn("ws", "send pong failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
			return err
		}
		return nil
	})

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	newDetachedRuntimeService := func() *session.Service {
		return session.NewService("", session.Dependencies{
			NewExecRunner: h.NewExecRunner,
			NewPtyRunner:  h.NewPtyRunner,
		})
	}
	runtimeSvc := newDetachedRuntimeService()
	var activeRuntimeSession *runtimeSession // nil for detached service, set when Attach() succeeds
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
				if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
					logx.Error("ws", "set websocket write deadline failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
					select {
					case writeErrCh <- err:
					default:
					}
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

	sessionProjectionContext := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), 2*time.Second)
	}

	finalizeProjectionSnapshotForService := func(sessionID string, service *session.Service, loaded data.ProjectionSnapshot) data.ProjectionSnapshot {
		projection := data.ProjectionSnapshot{
			RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		}
		projection = loaded
		if strings.TrimSpace(sessionID) != "" && service != nil {
			projection = session.WithRuntimeSnapshot(projection, service)
		}
		if diff := loaded.CurrentDiff; diff != nil {
			projection.CurrentDiff = diff
		}
		if len(projection.Diffs) == 0 && projection.CurrentDiff != nil {
			projection.Diffs = []session.DiffContext{*projection.CurrentDiff}
		}
		return projection
	}

	buildProjectionSnapshotForService := func(sessionID string, service *session.Service) data.ProjectionSnapshot {
		storeCtx, storeCancel := sessionProjectionContext()
		defer storeCancel()
		loaded := data.ProjectionSnapshot{}
		if service != nil {
			runtimeSnapshot := service.RuntimeSnapshot()
			if runtimeSnapshot.Running && strings.TrimSpace(runtimeSnapshot.ActiveSession) == strings.TrimSpace(sessionID) {
				loaded = readProjectionFromSessionStoreRaw(h.SessionStore, storeCtx, sessionID, connectionID, remoteAddr)
				return finalizeProjectionSnapshotForService(sessionID, service, loaded)
			}
		}
		loaded = readProjectionFromSessionStore(h.SessionStore, storeCtx, sessionID, connectionID, remoteAddr)
		return finalizeProjectionSnapshotForService(sessionID, service, loaded)
	}

	buildRuntimeProjectionSnapshotForService := func(sessionID string, service *session.Service) data.ProjectionSnapshot {
		storeCtx, storeCancel := sessionProjectionContext()
		defer storeCancel()
		loaded := readProjectionFromSessionStoreRaw(h.SessionStore, storeCtx, sessionID, connectionID, remoteAddr)
		return finalizeProjectionSnapshotForService(sessionID, service, loaded)
	}

	buildProjectionSnapshotFor := func(sessionID string) data.ProjectionSnapshot {
		trimmedSessionID := strings.TrimSpace(sessionID)
		service := runtimeSvc
		if trimmedSessionID == "" {
			service = nil
		} else if trimmedSessionID != strings.TrimSpace(selectedSessionID) {
			service = nil
			if entry := h.runtimeSessions.Ensure(trimmedSessionID); entry != nil {
				service = entry.service
			}
		}
		return buildProjectionSnapshotForService(trimmedSessionID, service)
	}

	runtimeForSession := func(sessionID string) (*runtimeSession, *session.Service) {
		trimmedSessionID := strings.TrimSpace(sessionID)
		if trimmedSessionID == "" {
			return nil, runtimeSvc
		}
		if trimmedSessionID == strings.TrimSpace(selectedSessionID) && activeRuntimeSession != nil {
			return activeRuntimeSession, activeRuntimeSession.service
		}
		entry := h.runtimeSessions.Ensure(trimmedSessionID)
		if entry != nil {
			return entry, entry.service
		}
		return nil, runtimeSvc
	}

	activeRuntimeRecord := func(sessionID string) bool {
		trimmedSessionID := strings.TrimSpace(sessionID)
		if trimmedSessionID == "" || claudesync.IsMirrorSessionID(trimmedSessionID) || codexsync.IsMirrorSessionID(trimmedSessionID) {
			return false
		}
		entry := h.runtimeSessions.Get(trimmedSessionID)
		if entry == nil || entry.service == nil {
			return false
		}
		snapshot := entry.service.RuntimeSnapshot()
		return snapshot.Running && strings.TrimSpace(snapshot.ActiveSession) == trimmedSessionID
	}

	loadSessionRecordForRequest := func(requestedSessionID string) (data.SessionRecord, error) {
		if activeRuntimeRecord(requestedSessionID) {
			return h.SessionStore.GetSession(ctx, strings.TrimSpace(requestedSessionID))
		}
		return loadSessionRecord(ctx, h.SessionStore, requestedSessionID)
	}

	persistProjectionFor := func(sessionID string, snapshot data.ProjectionSnapshot) {
		if h.SessionStore == nil || strings.TrimSpace(sessionID) == "" {
			if h.SessionStore == nil {
				logx.Warn("ws", "skip projection persistence because session store is unavailable: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, sessionID, remoteAddr)
			}
			return
		}
		persistCtx, persistCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer persistCancel()
		if _, err := h.SessionStore.SaveProjection(persistCtx, sessionID, snapshot); err != nil {
			logx.Error("ws", "save session projection failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
			return
		}
		// Sync new user/assistant entries to Claude CLI JSONL.
		syncSessionEntriesToClaudeJSONL(h.SessionStore, sessionID, snapshot)
	}

	lookupClaudeSessionUUID := func(sessionID string) string {
		if h.SessionStore == nil || strings.TrimSpace(sessionID) == "" {
			return ""
		}
		record, err := h.SessionStore.GetSession(ctx, sessionID)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(record.Summary.ClaudeSessionUUID)
	}

	emit := func(event any) {
		session.Enqueue(ctx, writeCh, event)
	}
	ackClientAction := func(sessionID, action string, client protocol.ClientEvent) bool {
		clientActionID := strings.TrimSpace(client.ClientActionID)
		if clientActionID == "" {
			return true
		}
		targetSessionID := strings.TrimSpace(sessionID)
		if targetSessionID == "" {
			targetSessionID = strings.TrimSpace(selectedSessionID)
		}
		sessionRuntime := h.runtimeSessions.Ensure(targetSessionID)
		accepted := true
		if sessionRuntime != nil {
			accepted = sessionRuntime.markClientAction(clientActionID)
		}
		emit(protocol.NewClientActionAckEvent(targetSessionID, action, clientActionID, "accepted", !accepted))
		return accepted
	}

	adbRTC := newADBWebRTCBridge(func() string {
		return selectedSessionID
	}, emit)
	defer adbRTC.Stop("")

	var adbMu sync.Mutex
	var adbCancel context.CancelFunc
	adbActiveSerial := ""

	stopADBStream := func(message string) {
		adbMu.Lock()
		cancel := adbCancel
		activeSerial := adbActiveSerial
		adbCancel = nil
		adbActiveSerial = ""
		adbMu.Unlock()
		if cancel != nil {
			cancel()
		}
		if strings.TrimSpace(message) != "" {
			emit(protocol.NewADBStreamStateEvent(selectedSessionID, false, activeSerial, 0, 0, 0, message))
		}
	}

	emitADBDevices := func(message string) {
		status := adb.DetectStatus(ctx)
		items := make([]protocol.ADBDevice, 0, len(status.Devices))
		for _, item := range status.Devices {
			items = append(items, protocol.ADBDevice{
				Serial:      item.Serial,
				State:       item.State,
				Model:       item.Model,
				Product:     item.Product,
				DeviceName:  item.DeviceName,
				TransportID: item.TransportID,
			})
		}
		statusMessage := strings.TrimSpace(status.Message)
		if strings.TrimSpace(message) != "" {
			statusMessage = message
		}
		emit(protocol.NewADBDevicesResultEvent(
			selectedSessionID,
			items,
			status.PreferredSerial,
			status.AvailableAVDs,
			status.PreferredAVD,
			status.ADBAvailable,
			status.EmulatorAvailable,
			status.SuggestedAction,
			statusMessage,
		))
	}

	startADBStream := func(serial string, interval time.Duration) {
		stopADBStream("")
		streamCtx, cancel := context.WithCancel(ctx)
		adbMu.Lock()
		adbCancel = cancel
		adbMu.Unlock()

		go func(sessionID string, requestedSerial string, frameInterval time.Duration) {
			resolvedSerial, err := adb.ResolveSerial(streamCtx, requestedSerial)
			if err != nil {
				emit(protocol.NewADBStreamStateEvent(sessionID, false, requestedSerial, 0, 0, int(frameInterval/time.Millisecond), err.Error()))
				return
			}

			adbMu.Lock()
			adbActiveSerial = resolvedSerial
			adbMu.Unlock()

			seq := 0
			for {
				frame, frameErr := adb.CaptureFrame(streamCtx, resolvedSerial)
				if frameErr != nil {
					if streamCtx.Err() != nil {
						return
					}
					emit(protocol.NewADBStreamStateEvent(sessionID, false, resolvedSerial, 0, 0, int(frameInterval/time.Millisecond), frameErr.Error()))
					stopADBStream("")
					return
				}
				seq++
				emit(protocol.NewADBFrameEvent(
					sessionID,
					frame.Serial,
					frame.Format,
					base64.StdEncoding.EncodeToString(frame.Data),
					frame.Width,
					frame.Height,
					seq,
				))
				emit(protocol.NewADBStreamStateEvent(sessionID, true, frame.Serial, frame.Width, frame.Height, int(frameInterval/time.Millisecond), "ADB 画面预览中"))

				timer := time.NewTimer(frameInterval)
				select {
				case <-streamCtx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}(selectedSessionID, serial, interval)
	}

	switchRuntimeSession := func(sessionID string) {
		logx.Info("ws", "switch runtime session: connectionID=%s previousSessionID=%s nextSessionID=%s remoteAddr=%s", connectionID, selectedSessionID, sessionID, remoteAddr)
		nextSessionID := strings.TrimSpace(sessionID)
		previousSessionID := strings.TrimSpace(selectedSessionID)
		previousRuntimeSvc := runtimeSvc
		if previousSessionID == nextSessionID && nextSessionID != "" {
			if entry := h.runtimeSessions.Attach(nextSessionID, connectionID, emit); entry != nil {
				runtimeSvc = entry.service
				activeRuntimeSession = entry
			}
			selectedSessionID = nextSessionID
			return
		}
		if previousSessionID != "" {
			h.runtimeSessions.Release(previousSessionID, connectionID, true)
		} else if previousRuntimeSvc != nil {
			previousRuntimeSvc.Cleanup()
		}
		selectedSessionID = nextSessionID
		if nextSessionID == "" {
			runtimeSvc = newDetachedRuntimeService()
			activeRuntimeSession = nil
			return
		}
		if entry := h.runtimeSessions.Attach(nextSessionID, connectionID, emit); entry != nil {
			runtimeSvc = entry.service
			activeRuntimeSession = entry
			return
		}
		runtimeSvc = newDetachedRuntimeService()
	}

	resolvePermissionDecisionRuntime := func(req protocol.PermissionDecisionRequestEvent) (string, *session.Service) {
		targetSessionID := strings.TrimSpace(firstNonEmptyString(req.SessionID, selectedSessionID))
		if targetSessionID != "" {
			if targetSessionID != strings.TrimSpace(selectedSessionID) {
				switchRuntimeSession(targetSessionID)
			}
			return targetSessionID, runtimeSvc
		}
		resumeSessionID := strings.TrimSpace(req.ResumeSessionID)
		if resumeSessionID == "" {
			return "", runtimeSvc
		}
		matchedSessionID, matchedRuntime := h.runtimeSessions.FindByResumeSessionID(resumeSessionID)
		if matchedSessionID == "" || matchedRuntime == nil {
			return "", runtimeSvc
		}
		switchRuntimeSession(matchedSessionID)
		return matchedSessionID, runtimeSvc
	}

	emitSessionList := func(filterCWD string) []data.SessionSummary {
		if h.SessionStore == nil {
			logx.Warn("ws", "session list requested but session store unavailable: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
			return nil
		}
		sessionListFilterCWD = normalizeSessionCWD(filterCWD)
		items, err := h.SessionStore.ListSessions(ctx)
		if err != nil {
			logx.Error("ws", "list sessions failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
			emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
			return nil
		}
		merged, mergeErr := mergeSessionSummaries(ctx, h.SessionStore, items, sessionListFilterCWD)
		if mergeErr != nil {
			logx.Warn("ws", "merge session summaries failed: connectionID=%s sessionID=%s remoteAddr=%s cwd=%q err=%v", connectionID, selectedSessionID, remoteAddr, sessionListFilterCWD, mergeErr)
			merged = filterStoreSessionsByCWD(items, sessionListFilterCWD)
		}
		emit(protocol.NewSessionListResultEvent(selectedSessionID, toProtocolSummaries(merged)))
		return merged
	}

	emitStoredSessionList := func(filterCWD string) []data.SessionSummary {
		if h.SessionStore == nil {
			logx.Warn("ws", "stored session list requested but session store unavailable: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
			return nil
		}
		sessionListFilterCWD = normalizeSessionCWD(filterCWD)
		items, err := h.SessionStore.ListSessions(ctx)
		if err != nil {
			logx.Error("ws", "list stored sessions failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
			emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
			return nil
		}
		filtered := filterStoreSessionsByCWD(items, sessionListFilterCWD)
		emit(protocol.NewSessionListResultEvent(selectedSessionID, toProtocolSummaries(filtered)))
		return filtered
	}

	emitEmptySessionState := func() {
		emit(protocol.NewSessionStateEvent(selectedSessionID, string(session.StateActive), "session cleared"))
	}

	var emitAndPersistFor func(sessionID string) func(any)
	emitAndPersistFor = func(sessionID string) func(any) {
		runtimeSessionID := strings.TrimSpace(sessionID)
		sessionRuntime, sessionRuntimeSvc := runtimeForSession(runtimeSessionID)
		emitSessionEvent := func(event any) {
			if sessionRuntime != nil {
				sessionRuntime.emit(event)
				return
			}
			emit(event)
		}
		emitAIStatusEvent := func(status protocol.AIStatusEvent) {
			event := any(status)
			if sessionRuntime != nil {
				event = prepareSessionEventForResume(sessionRuntime, runtimeSessionID, status)
			}
			emitSessionEvent(event)
		}
		return func(event any) {
			switch event.(type) {
			case protocol.PromptRequestEvent, protocol.InteractionRequestEvent:
				autoApplyCtx, autoApplyCancel := sessionProjectionContext()
				applied, err := maybeAutoApplyPermissionEvent(autoApplyCtx, h.SessionStore, runtimeSessionID, event, sessionRuntimeSvc, emitSessionEvent, emitAndPersistFor(runtimeSessionID))
				autoApplyCancel()
				if err == nil && applied {
					logx.Info("ws", "permission event auto-applied: sessionID=%s", runtimeSessionID)
					return
				}
				logx.Info("ws", "permission event forwarding to client: sessionID=%s eventType=%T", runtimeSessionID, event)
			}
			switch e := event.(type) {
			case protocol.CatalogAuthoringResultEvent:
				if e.Domain == "skill" {
					if e.Skill == nil {
						emitSessionEvent(protocol.NewErrorEvent(runtimeSessionID, "catalog authoring 缺少 skill payload", ""))
						return
					}
					eventCtx, eventCancel := sessionProjectionContext()
					err := upsertLocalSkill(h.SessionStore, eventCtx, *e.Skill)
					eventCancel()
					if err != nil {
						emitSessionEvent(protocol.NewErrorEvent(runtimeSessionID, err.Error(), ""))
						return
					}
					h.SkillLauncher = skills.NewLauncher(h.SessionStore)
					eventCtx, eventCancel = sessionProjectionContext()
					emitSkillCatalogResult(emitSessionEvent, h.SessionStore, eventCtx, runtimeSessionID)
					eventCancel()
					return
				}
				if e.Domain == "memory" {
					if e.Memory == nil {
						emitSessionEvent(protocol.NewErrorEvent(runtimeSessionID, "catalog authoring 缺少 memory payload", ""))
						return
					}
					eventCtx, eventCancel := sessionProjectionContext()
					err := upsertMemoryItem(h.SessionStore, eventCtx, *e.Memory)
					eventCancel()
					if err != nil {
						emitSessionEvent(protocol.NewErrorEvent(runtimeSessionID, err.Error(), ""))
						return
					}
					eventCtx, eventCancel = sessionProjectionContext()
					emitMemoryListResult(emitSessionEvent, h.SessionStore, eventCtx, runtimeSessionID)
					eventCancel()
					return
				}
				emitSessionEvent(protocol.NewErrorEvent(runtimeSessionID, "未知 catalog authoring domain", ""))
				return
			default:
				event = prepareSessionEventForResume(sessionRuntime, runtimeSessionID, event)
				emitSessionEvent(event)
				h.sendPushNotificationIfNeeded(ctx, runtimeSessionID, event)
				projection := buildRuntimeProjectionSnapshotForService(runtimeSessionID, sessionRuntimeSvc)
				snapshot, ok := session.ApplyEventToProjection(projection, event)
				if ok {
					persistProjectionFor(runtimeSessionID, snapshot)
					if status, ok := session.AIStatusEventForBackendEvent(runtimeSessionID, sessionRuntimeSvc, snapshot, event); ok {
						emitAIStatusEvent(status)
					}
					if sessionRuntime != nil {
						sessionRuntime.markPersisted(eventCursorFromEvent(event))
					}
					return
				}
				if status, ok := session.AIStatusEventForBackendEvent(runtimeSessionID, sessionRuntimeSvc, projection, event); ok {
					emitAIStatusEvent(status)
				}
			}
		}
	}

	defer func() {
		stopADBStream("")
		cancel()
		if strings.TrimSpace(selectedSessionID) != "" {
			h.runtimeSessions.Release(selectedSessionID, connectionID, false)
		} else if runtimeSvc != nil {
			runtimeSvc.Cleanup()
		}
		writerWG.Wait()
		logx.Info("ws", "connection closed: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
	}()

	emit(protocol.NewSessionStateEvent(selectedSessionID, string(session.StateActive), "connected"))
	emit(runtimeSvc.InitialEvent())
	if h.SessionStore != nil {
		emitSkillCatalogResult(emit, h.SessionStore, ctx, selectedSessionID)
		emitMemoryListResult(emit, h.SessionStore, ctx, selectedSessionID)
		if emitStoredSessionList(sessionListFilterCWD) != nil {
			if strings.TrimSpace(selectedSessionID) != "" {
				record, err := h.SessionStore.GetSession(ctx, selectedSessionID)
				if err != nil {
					logx.Warn("ws", "initial session history restore skipped: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				} else {
					emit(session.SessionHistoryEventFromRecord(record, sessionRecordRuntimeAlive(record, runtimeSvc)))
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
			logx.Info("ws", "incoming session_create: connectionID=%s sessionID=%s remoteAddr=%s title=%q cwd=%q reason=%q", connectionID, selectedSessionID, remoteAddr, req.Title, req.CWD, req.Reason)
			if strings.TrimSpace(req.Reason) == "auto_bind" && strings.TrimSpace(selectedSessionID) != "" {
				logx.Info("ws", "ignore auto_bind session_create because session already selected: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
				emitSessionList(req.CWD)
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
			if cwd := normalizeSessionCWD(req.CWD); cwd != "" {
				record, err := h.SessionStore.GetSession(ctx, created.ID)
				if err == nil {
					record.Projection.Runtime.CWD = cwd
					record.Projection.Runtime.Source = "mobilevc"
					record.Summary.Runtime = record.Projection.Runtime
					if _, err := h.SessionStore.UpsertSession(ctx, record); err == nil {
						created = record.Summary
					}
				}
			}
			switchRuntimeSession(created.ID)
			emit(protocol.NewSessionCreatedEvent(selectedSessionID, toProtocolSummary(created)))
			emit(protocol.NewSessionStateEvent(selectedSessionID, string(session.StateActive), "session selected"))
			emitSessionList(req.CWD)
		case "ping":
			emit(map[string]any{
				"type":      "pong",
				"sessionId": selectedSessionID,
				"ts":        time.Now().UTC().Format(time.RFC3339Nano),
			})
			if snapshot := runtimeSvc.BuildTaskSnapshotEvent(selectedSessionID, taskCursorSnapshot(activeRuntimeSession), "heartbeat", false); snapshot != nil {
				emit(*snapshot)
				if status, ok := session.AIStatusEventForBackendEvent(selectedSessionID, runtimeSvc, buildProjectionSnapshotForService(selectedSessionID, runtimeSvc), *snapshot); ok {
					emit(status)
				}
			}
		case "task_snapshot_get":
			if snapshot := runtimeSvc.BuildTaskSnapshotEvent(selectedSessionID, taskCursorSnapshot(activeRuntimeSession), "sync", true); snapshot != nil {
				emit(*snapshot)
				if status, ok := session.AIStatusEventForBackendEvent(selectedSessionID, runtimeSvc, buildProjectionSnapshotForService(selectedSessionID, runtimeSvc), *snapshot); ok {
					emit(status)
				}
			}
		case "session_list":
			var req protocol.SessionListRequestEvent
			if err := json.Unmarshal(payload, &req); err != nil {
				logx.Warn("ws", "invalid session_list request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_list request: %v", err), ""))
				continue
			}
			if h.SessionStore == nil {
				logx.Error("ws", "session store unavailable for session_list: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			emitSessionList(req.CWD)
		case "session_load":
			var req protocol.SessionLoadRequestEvent
			if err := json.Unmarshal(payload, &req); err != nil {
				logx.Warn("ws", "invalid session_load request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_load request: %v", err), ""))
				continue
			}
			logx.Info("ws", "incoming session_load: connectionID=%s sessionID=%s remoteAddr=%s requestedSessionID=%s cwd=%q reason=%q", connectionID, selectedSessionID, remoteAddr, req.SessionID, req.CWD, req.Reason)
			if strings.TrimSpace(req.Reason) == "auto_bind" && strings.TrimSpace(selectedSessionID) != "" {
				logx.Info("ws", "ignore auto_bind session_load because session already selected: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s", connectionID, selectedSessionID, req.SessionID, remoteAddr)
				emitSessionList(req.CWD)
				continue
			}
			if h.SessionStore == nil {
				logx.Error("ws", "session store unavailable for session_load: connectionID=%s sessionID=%s remoteAddr=%s requestedSessionID=%s", connectionID, selectedSessionID, remoteAddr, req.SessionID)
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			activeRuntimeLoad := activeRuntimeRecord(req.SessionID)
			record, err := loadSessionRecordForRequest(req.SessionID)
			if err != nil {
				logx.Warn("ws", "load session failed: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, req.SessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			// Merge any new events from Claude CLI JSONL (if this session was continued in CLI).
			// Switch runtime first so merge sees the correct ActiveSession.
			switchRuntimeSession(record.Summary.ID)
			if !activeRuntimeLoad {
				record = mergeClaudeJSONLToRecord(ctx, h.SessionStore, record, runtimeSvc)
			}
			augmentedProjection := session.NormalizeProjectionSnapshot(record.Projection)
			runtimeProjection := buildProjectionSnapshotForService(record.Summary.ID, runtimeSvc)
			if codexsync.IsMirrorSessionID(record.Summary.ID) {
				record.Projection = mergeProjectionWithOptionalRuntime(augmentedProjection, runtimeProjection, runtimeSvc, record.Summary.ID)
			} else {
				record.Projection = runtimeProjection
				if preferAugmentedLogEntries(augmentedProjection.LogEntries, record.Projection.LogEntries) {
					record.Projection.LogEntries = augmentedProjection.LogEntries
				}
			}
			record.Summary.Runtime = record.Projection.Runtime
			loadRuntimeAlive := sessionRecordRuntimeAlive(record, runtimeSvc)
			emit(session.SessionHistoryEventFromRecord(record, loadRuntimeAlive))
			emitReviewStateFromProjection(emit, selectedSessionID, record.Projection)
			if restored := restoredAgentStateEventFromRecord(record, loadRuntimeAlive); restored != nil {
				emit(*restored)
				if status, ok := session.AIStatusEventForBackendEvent(record.Summary.ID, runtimeSvc, record.Projection, *restored); ok {
					emit(status)
				}
			}
			emit(protocol.NewSessionStateEvent(selectedSessionID, string(session.StateActive), "history loaded"))
		case "register_push_token":
			var req protocol.RegisterPushTokenRequestEvent
			if err := json.Unmarshal(payload, &req); err != nil {
				logx.Warn("ws", "invalid register_push_token request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid register_push_token request: %v", err), ""))
				continue
			}
			targetSessionID := strings.TrimSpace(req.SessionID)
			if targetSessionID == "" {
				targetSessionID = strings.TrimSpace(selectedSessionID)
			}
			token := strings.TrimSpace(req.Token)
			platform := strings.TrimSpace(req.Platform)
			logx.Info("ws", "register push token: connectionID=%s sessionID=%s remoteAddr=%s requestedSessionID=%s resolvedSessionID=%s platform=%s token=%s", connectionID, selectedSessionID, remoteAddr, req.SessionID, targetSessionID, platform, token)
			h.handleRegisterPushToken(ctx, targetSessionID, token, platform, emit)
		case "session_delta_get":
			var req protocol.SessionDeltaRequestEvent
			if err := json.Unmarshal(payload, &req); err != nil {
				logx.Warn("ws", "invalid session_delta_get request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_delta_get request: %v", err), ""))
				continue
			}
			if h.SessionStore == nil {
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			targetSessionID := strings.TrimSpace(req.SessionID)
			if targetSessionID == "" {
				targetSessionID = strings.TrimSpace(selectedSessionID)
			}
			record, err := loadSessionRecordForRequest(targetSessionID)
			if err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			switchRuntimeSession(record.Summary.ID)
			sessionRuntime := h.runtimeSessions.Ensure(record.Summary.ID)
			freshProjection := session.NormalizeProjectionSnapshot(record.Projection)
			runtimeProjection := buildProjectionSnapshotForService(record.Summary.ID, runtimeSvc)
			if codexsync.IsMirrorSessionID(record.Summary.ID) {
				record.Projection = mergeProjectionWithOptionalRuntime(freshProjection, runtimeProjection, runtimeSvc, record.Summary.ID)
			} else {
				record.Projection = runtimeProjection
			}
			record.Summary.Runtime = record.Projection.Runtime
			runtimeAlive := sessionRecordRuntimeAlive(record, runtimeSvc)
			logx.Info("ws", "session delta response: sessionID=%s runtimeAlive=%v canResume=%v", record.Summary.ID, runtimeAlive, sessionRuntime != nil && runtimeAlive)
			emit(session.SessionDeltaEventFromRecord(record, req.Known, deltaCursorSnapshot(sessionRuntime), runtimeAlive))
			emitReviewStateFromProjection(emit, selectedSessionID, record.Projection)
			if snapshot := runtimeSvc.BuildTaskSnapshotEvent(record.Summary.ID, taskCursorSnapshot(sessionRuntime), "delta", true); snapshot != nil {
				emit(*snapshot)
				if status, ok := session.AIStatusEventForBackendEvent(record.Summary.ID, runtimeSvc, record.Projection, *snapshot); ok {
					emit(status)
				}
			}
		case "session_resume":
			var req protocol.SessionResumeRequestEvent
			if err := json.Unmarshal(payload, &req); err != nil {
				logx.Warn("ws", "invalid session_resume request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_resume request: %v", err), ""))
				continue
			}
			logx.Info("ws", "incoming session_resume: connectionID=%s sessionID=%s remoteAddr=%s requestedSessionID=%s cwd=%q reason=%q lastSeenEventCursor=%d lastKnownRuntimeState=%q", connectionID, selectedSessionID, remoteAddr, req.SessionID, req.CWD, req.Reason, req.LastSeenEventCursor, req.LastKnownRuntimeState)
			if h.SessionStore == nil {
				logx.Error("ws", "session store unavailable for session_resume: connectionID=%s sessionID=%s remoteAddr=%s requestedSessionID=%s", connectionID, selectedSessionID, remoteAddr, req.SessionID)
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			record, err := loadSessionRecordForRequest(req.SessionID)
			if err != nil {
				logx.Warn("ws", "resume session failed: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, req.SessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			// Merge any new events from Claude CLI JSONL.
			// Switch runtime session first so mergeClaudeJSONLToRecord can
			// check the correct ActiveSession when deciding ownership upgrades.
			switchRuntimeSession(record.Summary.ID)
			sessionRuntime := h.runtimeSessions.Ensure(record.Summary.ID)
			// Re-bind PTY runner output sink to new connection after reconnect.
			resumeEmitAndPersist := emitAndPersistFor(selectedSessionID)
			runtimeSvc.SetSink(sessionRuntime.EnsureBufferedSinkWithProcessor(resumeEmitAndPersist))
			record = mergeClaudeJSONLToRecord(ctx, h.SessionStore, record, runtimeSvc)
			augmentedProjection := session.NormalizeProjectionSnapshot(record.Projection)
			runtimeProjection := buildProjectionSnapshotForService(record.Summary.ID, runtimeSvc)
			projection := runtimeProjection
			if codexsync.IsMirrorSessionID(record.Summary.ID) {
				projection = mergeProjectionWithOptionalRuntime(augmentedProjection, runtimeProjection, runtimeSvc, record.Summary.ID)
			} else if preferAugmentedLogEntries(augmentedProjection.LogEntries, projection.LogEntries) {
				projection.LogEntries = augmentedProjection.LogEntries
			}
			record.Projection = projection
			record.Summary.Runtime = projection.Runtime
			runtimeAlive := sessionRecordRuntimeAlive(record, runtimeSvc)
			if runtimeAlive && session.ShouldEmitResumeRecoveryStateEvent(runtimeSvc, projection, req.LastKnownRuntimeState) {
				recovery := session.BuildResumeRecoveryStateEvent(record.Summary.ID, runtimeSvc, projection, req.LastKnownRuntimeState)
				emit(recovery)
				if status, ok := session.AIStatusEventForBackendEvent(record.Summary.ID, runtimeSvc, projection, recovery); ok {
					emit(status)
				}
			}
			emit(session.SessionHistoryEventFromRecord(record, runtimeAlive))
			logx.Info("ws", "session history emitted: sessionID=%s runtimeAlive=%v ownership=%s", record.Summary.ID, runtimeAlive, record.Summary.Ownership)
			emitReviewStateFromProjection(emit, selectedSessionID, record.Projection)
			restoredState := ""
			var restoredAgentEvent *protocol.AgentStateEvent
			if restored := restoredAgentStateEventFromRecord(record, runtimeAlive); restored != nil {
				restoredState = restored.State
				restoredAgentEvent = restored
			}
			replayedCount := 0
			latestCursor := int64(0)
			currentPermissionID := ""
			replayedCurrentPermission := false
			if sessionRuntime != nil {
				effectiveCursor := req.LastSeenEventCursor
				if persisted := sessionRuntime.persistedCursor.Load(); persisted > effectiveCursor {
					effectiveCursor = persisted
				}
				currentPermissionID = strings.TrimSpace(runtimeSvc.CurrentPermissionRequestID(record.Summary.ID))
				for _, pendingEvent := range sessionRuntime.pendingSince(effectiveCursor) {
					if currentPermissionID != "" {
						if prompt, ok := pendingEvent.(protocol.PromptRequestEvent); ok &&
							strings.TrimSpace(prompt.BlockingKind) == "permission" &&
							strings.TrimSpace(prompt.PermissionRequestID) == currentPermissionID {
							replayedCurrentPermission = true
						}
					}
					replayedCount++
					emit(pendingEvent)
				}
				latestCursor = sessionRuntime.latestCursor()
			}
			if currentPermissionID != "" && !replayedCurrentPermission {
				var prompt *protocol.PromptRequestEvent
				if sessionRuntime != nil {
					prompt = sessionRuntime.latestPendingPermissionPrompt(currentPermissionID)
				}
				if prompt == nil {
					prompt = session.RefreshedPermissionPromptEventWithID(record.Summary.ID, protocol.PermissionDecisionRequestEvent{
						PermissionMode:      projection.Runtime.PermissionMode,
						PermissionRequestID: currentPermissionID,
						ResumeSessionID:     firstNonEmptyString(projection.Runtime.ResumeSessionID, runtimeSvc.RuntimeSnapshot().ResumeSessionID),
						PromptMessage:       "当前操作需要你的授权",
						FallbackCommand:     projection.Runtime.Command,
						FallbackCWD:         projection.Runtime.CWD,
						FallbackEngine:      projection.Runtime.Engine,
					}, runtimeSvc, currentPermissionID)
				}
				if prompt != nil {
					logx.Info("ws", "session resume refreshed pending permission prompt: sessionID=%s requestID=%s", record.Summary.ID, currentPermissionID)
					emit(*prompt)
				}
			}
			// Emit restored agent state AFTER pending replay so that log events
			// (assistant replies) reach Flutter before the state transition,
			// preventing a "paused → reply text appears" flicker.
			if restoredAgentEvent != nil {
				emit(*restoredAgentEvent)
				if status, ok := session.AIStatusEventForBackendEvent(record.Summary.ID, runtimeSvc, record.Projection, *restoredAgentEvent); ok {
					emit(status)
				}
			}
			if snapshot := runtimeSvc.BuildTaskSnapshotEvent(record.Summary.ID, taskCursorSnapshot(sessionRuntime), "resume", true); snapshot != nil {
				emit(*snapshot)
				if status, ok := session.AIStatusEventForBackendEvent(record.Summary.ID, runtimeSvc, record.Projection, *snapshot); ok {
					emit(status)
				}
			}
			emit(protocol.NewSessionResumeResultEvent(
				record.Summary.ID,
				latestCursor,
				runtimeAlive,
				session.ResolvedResumeRuntimeState(restoredState, record, runtimeSvc),
				runtimeAlive,
				replayedCount,
				"session resumed",
			))
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
			record, err := h.SessionStore.GetSession(ctx, req.SessionID)
			if err != nil {
				logx.Warn("ws", "delete session lookup failed: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, req.SessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			if record.Summary.External || strings.EqualFold(strings.TrimSpace(record.Summary.Source), "codex-native") {
				emit(protocol.NewErrorEvent(selectedSessionID, "Codex 原生会话仅支持恢复，不支持在 MobileVC 内删除", ""))
				continue
			}
			if err := h.SessionStore.DeleteSession(ctx, req.SessionID); err != nil {
				logx.Warn("ws", "delete session failed: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, req.SessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			deletingCurrent := req.SessionID == selectedSessionID
			items := emitSessionList(sessionListFilterCWD)
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
			record, err = h.SessionStore.GetSession(ctx, fallbackSessionID)
			if err != nil {
				logx.Warn("ws", "load fallback session after delete failed: connectionID=%s sessionID=%s fallbackSessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, fallbackSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			record.Projection = buildProjectionSnapshotForService(record.Summary.ID, runtimeSvc)
			record.Summary.Runtime = record.Projection.Runtime
			emit(session.SessionHistoryEventFromRecord(record, sessionRecordRuntimeAlive(record, runtimeSvc)))
			emitReviewStateFromProjection(emit, selectedSessionID, record.Projection)
			if restored := restoredAgentStateEventFromRecord(record, sessionRecordRuntimeAlive(record, runtimeSvc)); restored != nil {
				emit(*restored)
			}
			emit(protocol.NewSessionStateEvent(selectedSessionID, string(session.StateActive), "history loaded"))
		case "session_context_get":
			if strings.TrimSpace(selectedSessionID) == "" {
				emit(protocol.NewSessionContextResultEvent(selectedSessionID, toProtocolSessionContext(data.SessionContext{})))
				continue
			}
			record, ok := loadSelectedSessionRecord(h.SessionStore, ctx, selectedSessionID, emit)
			if !ok {
				continue
			}
			emit(protocol.NewSessionContextResultEvent(selectedSessionID, toProtocolSessionContext(record.Projection.SessionContext)))
		case "permission_rule_list":
			emitPermissionRuleList(emit, h.SessionStore, ctx, selectedSessionID)
		case "permission_rule_upsert":
			var req protocol.PermissionRuleRequestEvent
			if err := json.Unmarshal(payload, &req); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid permission_rule_upsert request: %v", err), ""))
				continue
			}
			rule := fromProtocolPermissionRule(req.Rule)
			if strings.TrimSpace(rule.ID) == "" {
				emit(protocol.NewErrorEvent(selectedSessionID, "permission rule id is required", ""))
				continue
			}
			switch rule.Scope {
			case data.PermissionScopePersistent:
				snapshot, err := h.SessionStore.GetPermissionRuleSnapshot(ctx)
				if err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
				snapshot.Items = upsertPermissionRule(snapshot.Items, rule)
				if err := h.SessionStore.SavePermissionRuleSnapshot(ctx, snapshot); err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
			default:
				record, ok := loadSelectedSessionRecord(h.SessionStore, ctx, selectedSessionID, emit)
				if !ok {
					continue
				}
				record.Projection.PermissionRules = upsertPermissionRule(record.Projection.PermissionRules, rule)
				if _, err := h.SessionStore.SaveProjection(ctx, selectedSessionID, record.Projection); err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
			}
			emitPermissionRuleList(emit, h.SessionStore, ctx, selectedSessionID)
		case "permission_rule_delete":
			var req protocol.PermissionRuleDeleteRequestEvent
			if err := json.Unmarshal(payload, &req); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid permission_rule_delete request: %v", err), ""))
				continue
			}
			switch data.PermissionScope(strings.TrimSpace(req.Scope)) {
			case data.PermissionScopePersistent:
				snapshot, err := h.SessionStore.GetPermissionRuleSnapshot(ctx)
				if err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
				snapshot.Items = deletePermissionRule(snapshot.Items, strings.TrimSpace(req.ID))
				if err := h.SessionStore.SavePermissionRuleSnapshot(ctx, snapshot); err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
			default:
				record, ok := loadSelectedSessionRecord(h.SessionStore, ctx, selectedSessionID, emit)
				if !ok {
					continue
				}
				record.Projection.PermissionRules = deletePermissionRule(record.Projection.PermissionRules, strings.TrimSpace(req.ID))
				if _, err := h.SessionStore.SaveProjection(ctx, selectedSessionID, record.Projection); err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
			}
			emitPermissionRuleList(emit, h.SessionStore, ctx, selectedSessionID)
		case "permission_rules_set_enabled":
			var req protocol.PermissionRuleToggleRequestEvent
			if err := json.Unmarshal(payload, &req); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid permission_rules_set_enabled request: %v", err), ""))
				continue
			}
			switch data.PermissionScope(strings.TrimSpace(req.Scope)) {
			case data.PermissionScopePersistent:
				snapshot, err := h.SessionStore.GetPermissionRuleSnapshot(ctx)
				if err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
				snapshot.Enabled = req.Enabled
				if err := h.SessionStore.SavePermissionRuleSnapshot(ctx, snapshot); err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
			default:
				record, ok := loadSelectedSessionRecord(h.SessionStore, ctx, selectedSessionID, emit)
				if !ok {
					continue
				}
				record.Projection.PermissionRulesEnabled = req.Enabled
				if _, err := h.SessionStore.SaveProjection(ctx, selectedSessionID, record.Projection); err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
			}
			emitPermissionRuleList(emit, h.SessionStore, ctx, selectedSessionID)
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
			record.Projection.SessionContext = data.SessionContext{
				EnabledSkillNames: req.EnabledSkillNames,
				EnabledMemoryIDs:  req.EnabledMemoryIDs,
				Configured:        true,
			}
			record.Projection.SessionContextSet = true
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
			sourceOfTruth := resolveCatalogSourceOfTruth(h.SessionStore, ctx, selectedSessionID)
			snapshot, err := h.SessionStore.GetSkillCatalogSnapshot(ctx)
			if err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			snapshot.Meta.SourceOfTruth = sourceOfTruth
			snapshot.Meta.SyncState = data.CatalogSyncStateSyncing
			snapshot.Meta.LastError = ""
			if err := h.SessionStore.SaveSkillCatalogSnapshot(ctx, snapshot); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			emit(protocol.NewCatalogSyncStatusEvent(selectedSessionID, string(data.CatalogDomainSkill), toProtocolCatalogMetadata(snapshot.Meta)))
			if err := syncExternalSkills(h.SessionStore, ctx, sourceOfTruth); err != nil {
				snapshot.Meta.SyncState = data.CatalogSyncStateFailed
				snapshot.Meta.LastError = err.Error()
				_ = h.SessionStore.SaveSkillCatalogSnapshot(ctx, snapshot)
				emit(protocol.NewCatalogSyncResultEvent(selectedSessionID, string(data.CatalogDomainSkill), false, err.Error(), toProtocolCatalogMetadata(snapshot.Meta)))
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
			emit(protocol.NewCatalogSyncResultEvent(selectedSessionID, string(data.CatalogDomainSkill), true, "skill 同步完成", toProtocolCatalogMetadata(updatedSnapshot.Meta)))
			emitSkillCatalogResult(emit, h.SessionStore, ctx, selectedSessionID)
		case "memory_list":
			emitMemoryListResult(emit, h.SessionStore, ctx, selectedSessionID)
		case "memory_sync_pull":
			var req protocol.MemoryRequestEvent
			if err := json.Unmarshal(payload, &req); err != nil {
				logx.Warn("ws", "invalid memory_sync_pull request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid memory_sync_pull request: %v", err), ""))
				continue
			}
			if h.SessionStore == nil {
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			sourceOfTruth := resolveCatalogSourceOfTruth(h.SessionStore, ctx, selectedSessionID)
			snapshot, err := h.SessionStore.GetMemoryCatalogSnapshot(ctx)
			if err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			snapshot.Meta.SourceOfTruth = sourceOfTruth
			snapshot.Meta.SyncState = data.CatalogSyncStateSyncing
			snapshot.Meta.LastError = ""
			if err := h.SessionStore.SaveMemoryCatalogSnapshot(ctx, snapshot); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			emit(protocol.NewCatalogSyncStatusEvent(selectedSessionID, string(data.CatalogDomainMemory), toProtocolCatalogMetadata(snapshot.Meta)))
			syncCWD := resolveCatalogSyncCWD(h.SessionStore, ctx, selectedSessionID, firstNonEmptyString(req.CWD, sessionListFilterCWD))
			logx.Info("ws", "memory sync pull: connectionID=%s sessionID=%s remoteAddr=%s syncCWD=%q sourceOfTruth=%s", connectionID, selectedSessionID, remoteAddr, syncCWD, sourceOfTruth)
			if err := syncExternalMemories(h.SessionStore, ctx, syncCWD, sourceOfTruth); err != nil {
				snapshot.Meta.SyncState = data.CatalogSyncStateFailed
				snapshot.Meta.LastError = err.Error()
				_ = h.SessionStore.SaveMemoryCatalogSnapshot(ctx, snapshot)
				emit(protocol.NewCatalogSyncResultEvent(selectedSessionID, string(data.CatalogDomainMemory), false, err.Error(), toProtocolCatalogMetadata(snapshot.Meta)))
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			updatedSnapshot, err := h.SessionStore.GetMemoryCatalogSnapshot(ctx)
			if err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			emit(protocol.NewCatalogSyncResultEvent(selectedSessionID, string(data.CatalogDomainMemory), true, "memory 同步完成", toProtocolCatalogMetadata(updatedSnapshot.Meta)))
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
		case "ai_turn":
			var aiReq protocol.AITurnRequestEvent
			if err := json.Unmarshal(payload, &aiReq); err != nil {
				logx.Warn("ws", "invalid ai_turn request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid ai_turn request: %v", err), ""))
				continue
			}
			logx.Info("ws", "incoming action: connectionID=%s sessionID=%s remoteAddr=%s action=ai_turn engine=%q permissionMode=%q cwd=%q dataPreview=%q", connectionID, selectedSessionID, remoteAddr, aiReq.Engine, aiReq.PermissionMode, aiReq.CWD, wsDebugPreview(aiReq.Data))
			sessionID := strings.TrimSpace(firstNonEmptyString(aiReq.SessionID, selectedSessionID))
			if sessionID == "" || sessionID == connectionID {
				emit(protocol.NewErrorEvent(selectedSessionID, "请先创建或加载会话后再发送命令", ""))
				continue
			}
			if sessionID != strings.TrimSpace(selectedSessionID) {
				switchRuntimeSession(sessionID)
			}
			if !ackClientAction(sessionID, "ai_turn", aiReq.ClientEvent) {
				logx.Info("ws", "duplicate client action ignored: connectionID=%s sessionID=%s remoteAddr=%s action=ai_turn clientActionID=%s", connectionID, sessionID, remoteAddr, aiReq.ClientActionID)
				continue
			}
			sessionRuntime, service := runtimeForSession(sessionID)
			emitAndPersist := emitAndPersistFor(sessionID)
			if sessionRuntime != nil {
				service.SetSink(sessionRuntime.EnsureBufferedSinkWithProcessor(emitAndPersist))
			}
			if strings.TrimSpace(aiReq.PermissionMode) != "" {
				service.UpdatePermissionMode(aiReq.PermissionMode)
			}
			controller := service.ControllerSnapshot()
			snapshot := service.RuntimeSnapshot()
			projection := buildProjectionSnapshotFor(sessionID)
			engineName := strings.TrimSpace(strings.ToLower(firstNonEmptyString(
				aiReq.Engine,
				aiReq.RuntimeMeta.Engine,
				snapshot.ActiveMeta.Engine,
				controller.ActiveMeta.Engine,
				projection.Runtime.Engine,
			)))
			if engineName == "" {
				engineName = "claude"
			}
			command := strings.TrimSpace(firstNonEmptyString(
				aiReq.RuntimeMeta.Command,
				snapshot.ActiveMeta.Command,
				controller.CurrentCommand,
				controller.ActiveMeta.Command,
				projection.Runtime.Command,
				defaultAICommandFromEngine(engineName),
			))
			commandEngine := strings.TrimSpace(strings.ToLower(commandHead(command)))
			if !isAISessionCommandLike(command) || (commandEngine != "" && engineName != "" && commandEngine != engineName) {
				command = defaultAICommandFromEngine(engineName)
			}
			command = applyAICommandPreferences(command, engineName, aiReq.RuntimeMeta.Model, aiReq.RuntimeMeta.ReasoningEffort)
			cwd := firstNonEmptyString(
				aiReq.CWD,
				aiReq.RuntimeMeta.CWD,
				snapshot.ActiveMeta.CWD,
				controller.ActiveMeta.CWD,
				projection.Runtime.CWD,
				"/",
			)
			permissionMode := normalizePermissionModeForClaude(firstNonEmptyString(
				aiReq.PermissionMode,
				snapshot.ActiveMeta.PermissionMode,
				controller.ActiveMeta.PermissionMode,
				projection.Runtime.PermissionMode,
			))
			inputData := aiReq.Data
			if strings.TrimSpace(inputData) != "" {
				service.RecordUserInput(inputData)
				skillPrefix, err := skills.BuildEnabledSkillsPrefix(h.SessionStore, projection.SessionContext)
				if err != nil {
					logx.Warn("ws", "build enabled skills prefix failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
				}
				memoryPrefix, memoryErr := skills.BuildEnabledMemoryPrefix(h.SessionStore, projection.SessionContext)
				if memoryErr != nil {
					logx.Warn("ws", "build enabled memory prefix failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, memoryErr)
				}
				inputData = skills.InjectConversationPrefixes(inputData, skillPrefix, memoryPrefix)
			}
			reqMeta := protocol.MergeRuntimeMeta(aiReq.RuntimeMeta, protocol.RuntimeMeta{
				Source:            fallback(aiReq.Source, "ai_turn"),
				Command:           command,
				Engine:            engineName,
				CWD:               cwd,
				PermissionMode:    permissionMode,
				ClaudeSessionUUID: lookupClaudeSessionUUID(sessionID),
			})
			execReq := session.ExecuteRequest{
				Command:        command,
				CWD:            cwd,
				Mode:           engine.ModePTY,
				PermissionMode: permissionMode,
				RuntimeMeta:    reqMeta,
			}
			rawUserText := strings.TrimRight(aiReq.Data, "\n")
			if rawUserText == "" {
				appendUserProjectionEntry(h.SessionStore, ctx, sessionID, command, "命令", connectionID, remoteAddr)
				logx.Info("ws", "dispatch ai_turn execute: connectionID=%s sessionID=%s remoteAddr=%s command=%q cwd=%q permissionMode=%q", connectionID, sessionID, remoteAddr, command, cwd, permissionMode)
				if err := service.Execute(ctx, sessionID, execReq, emitAndPersist); err != nil {
					logx.Error("ws", "ai_turn execute failed: connectionID=%s sessionID=%s remoteAddr=%s command=%q err=%v", connectionID, sessionID, remoteAddr, command, err)
					emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
				}
				continue
			}
			inputReq := session.InputRequest{
				Data: inputData,
				RuntimeMeta: protocol.RuntimeMeta{
					Source:         "ai_turn",
					PermissionMode: permissionMode,
				},
			}
			logx.Info("ws", "dispatch ai_turn input/resume: connectionID=%s sessionID=%s remoteAddr=%s command=%q cwd=%q permissionMode=%q preview=%q", connectionID, sessionID, remoteAddr, command, cwd, permissionMode, wsDebugPreview(inputData))
			if service.ShouldEmitTransientResumeThinkingEvent(execReq) {
				emit(protocol.ApplyRuntimeMeta(
					protocol.NewAgentStateEvent(
						sessionID,
						string(session.ControllerStateThinking),
						"恢复会话中",
						false,
						execReq.Command,
						projection.Controller.LastStep,
						projection.Controller.LastTool,
					),
					protocol.MergeRuntimeMeta(projection.Controller.ActiveMeta, protocol.RuntimeMeta{
						Source:          "ai_turn",
						ResumeSessionID: execReq.RuntimeMeta.ResumeSessionID,
						Command:         execReq.Command,
						Engine:          firstNonEmptyString(execReq.RuntimeMeta.Engine, projection.Runtime.Engine),
						CWD:             execReq.CWD,
						PermissionMode:  execReq.PermissionMode,
						ClaudeLifecycle: "active",
					}),
				))
			}
			err := service.SendInputOrResume(ctx, sessionID, execReq, inputReq, emitAndPersist)
			if err != nil {
				if errors.Is(err, session.ErrNoActiveRunner) || errors.Is(err, session.ErrResumeSessionUnavailable) {
					execReq.InitialInput = inputData
					logx.Info("ws", "ai_turn starting new AI runner: connectionID=%s sessionID=%s remoteAddr=%s command=%q cwd=%q", connectionID, sessionID, remoteAddr, execReq.Command, execReq.CWD)
					err = service.Execute(ctx, sessionID, execReq, emitAndPersist)
				}
			}
			if err != nil {
				message := err.Error()
				if errors.Is(err, engine.ErrInputNotSupported) {
					message = "input is only supported for pty sessions"
				} else if errors.Is(err, session.ErrNoActiveRunner) {
					message = "当前没有活跃会话，且没有可恢复的 AI 会话，请重新发起命令"
				} else if errors.Is(err, session.ErrResumeSessionUnavailable) {
					message = "当前没有 resume id，无法恢复 AI 会话，请重新发起命令"
				} else if errors.Is(err, session.ErrResumeConversationNotFound) {
					message = "当前 AI 会话的 resume id 已失效或不存在，请重新发起命令"
				} else if errors.Is(err, session.ErrRunnerNotInteractive) {
					message = "AI 恢复后未进入可输入状态，请稍后重试"
				}
				logx.Warn("ws", "ai_turn failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(sessionID, message, ""))
				continue
			}
			appendUserProjectionEntry(h.SessionStore, ctx, sessionID, rawUserText, "回复", connectionID, remoteAddr)
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
			sessionID := strings.TrimSpace(firstNonEmptyString(reqEvent.SessionID, selectedSessionID))
			if sessionID == "" || sessionID == connectionID {
				emit(protocol.NewErrorEvent(selectedSessionID, "请先创建或加载会话后再发送命令", ""))
				continue
			}
			if sessionID != strings.TrimSpace(selectedSessionID) {
				switchRuntimeSession(sessionID)
			}
			if !ackClientAction(sessionID, "exec", reqEvent.ClientEvent) {
				logx.Info("ws", "duplicate client action ignored: connectionID=%s sessionID=%s remoteAddr=%s action=exec clientActionID=%s", connectionID, sessionID, remoteAddr, reqEvent.ClientActionID)
				continue
			}
			sessionRuntime, service := runtimeForSession(sessionID)
			emitAndPersist := emitAndPersistFor(sessionID)
			appendUserProjectionEntry(h.SessionStore, ctx, sessionID, reqEvent.Command, "命令", connectionID, remoteAddr)
			mode, err := session.ParseMode(reqEvent.Mode)
			if err != nil {
				logx.Warn("ws", "parse exec mode failed: connectionID=%s sessionID=%s remoteAddr=%s mode=%q err=%v", connectionID, sessionID, remoteAddr, reqEvent.Mode, err)
				emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
				continue
			}
			logx.Info("ws", "dispatch exec: connectionID=%s sessionID=%s remoteAddr=%s action=exec mode=%s cwd=%q permissionMode=%q preview=%q", connectionID, sessionID, remoteAddr, mode, reqEvent.CWD, reqEvent.PermissionMode, wsDebugPreview(reqEvent.Command))
			if sessionRuntime != nil {
				service.SetSink(sessionRuntime.EnsureBufferedSinkWithProcessor(emitAndPersist))
			}
			initialInput := reqEvent.InputData
			if initialInput != "" {
				service.RecordUserInput(initialInput)
				projection := buildProjectionSnapshotFor(sessionID)
				if shouldInjectEnabledSkillsForInput(
					reqEvent.Command,
					reqEvent.Engine,
					reqEvent.Engine,
					projection.Runtime.Engine,
				) {
					skillPrefix, err := skills.BuildEnabledSkillsPrefix(h.SessionStore, projection.SessionContext)
					if err != nil {
						logx.Warn("ws", "build enabled skills prefix failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
					}
					memoryPrefix, memoryErr := skills.BuildEnabledMemoryPrefix(h.SessionStore, projection.SessionContext)
					if memoryErr != nil {
						logx.Warn("ws", "build enabled memory prefix failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, memoryErr)
					}
					initialInput = skills.InjectConversationPrefixes(initialInput, skillPrefix, memoryPrefix)
				}
			}
			err = service.Execute(ctx, sessionID, session.ExecuteRequest{
				Command:        reqEvent.Command,
				CWD:            reqEvent.CWD,
				Mode:           mode,
				PermissionMode: reqEvent.PermissionMode,
				InitialInput:   initialInput,
				RuntimeMeta: protocol.RuntimeMeta{
					Source:            fallback(reqEvent.Source, "command"),
					SkillName:         reqEvent.SkillName,
					Target:            reqEvent.Target,
					TargetType:        reqEvent.TargetType,
					TargetPath:        reqEvent.TargetPath,
					ResultView:        reqEvent.ResultView,
					ContextID:         reqEvent.ContextID,
					ContextTitle:      reqEvent.ContextTitle,
					TargetText:        reqEvent.TargetText,
					Command:           reqEvent.Command,
					Engine:            reqEvent.Engine,
					CWD:               reqEvent.CWD,
					PermissionMode:    reqEvent.PermissionMode,
					ClaudeSessionUUID: lookupClaudeSessionUUID(sessionID),
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
			sessionID := strings.TrimSpace(firstNonEmptyString(inputEvent.SessionID, selectedSessionID))
			if sessionID == "" || sessionID == connectionID {
				emit(protocol.NewErrorEvent(selectedSessionID, "请先创建或加载会话后再发送命令", ""))
				continue
			}
			if sessionID != strings.TrimSpace(selectedSessionID) {
				switchRuntimeSession(sessionID)
			}
			if !ackClientAction(sessionID, "input", inputEvent.ClientEvent) {
				logx.Info("ws", "duplicate client action ignored: connectionID=%s sessionID=%s remoteAddr=%s action=input clientActionID=%s", connectionID, sessionID, remoteAddr, inputEvent.ClientActionID)
				continue
			}
			sessionRuntime, service := runtimeForSession(sessionID)
			emitAndPersist := emitAndPersistFor(sessionID)
			if shouldTreatInputAsAICommand(inputEvent.Data) {
				command := strings.TrimSpace(inputEvent.Data)
				permissionMode := normalizePermissionModeForClaude(inputEvent.PermissionMode)
				logx.Info("ws", "promote input to exec: connectionID=%s sessionID=%s remoteAddr=%s action=input command=%q", connectionID, sessionID, remoteAddr, command)
				appendUserProjectionEntry(h.SessionStore, ctx, sessionID, command, "命令", connectionID, remoteAddr)
				if sessionRuntime != nil {
					service.SetSink(sessionRuntime.EnsureBufferedSinkWithProcessor(emitAndPersist))
				}
				if err := service.Execute(ctx, sessionID, session.ExecuteRequest{
					Command:        command,
					CWD:            firstNonEmptyString(inputEvent.CWD, "/"),
					Mode:           engine.ModePTY,
					PermissionMode: permissionMode,
					RuntimeMeta: protocol.RuntimeMeta{
						Source:            "input-promoted-exec",
						Command:           command,
						CWD:               firstNonEmptyString(inputEvent.CWD, "/"),
						PermissionMode:    permissionMode,
						ClaudeSessionUUID: lookupClaudeSessionUUID(sessionID),
					},
				}, emitAndPersist); err != nil {
					logx.Error("ws", "promoted input execute failed: connectionID=%s sessionID=%s remoteAddr=%s command=%q err=%v", connectionID, sessionID, remoteAddr, command, err)
					emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
				}
				continue
			}
			service.RecordUserInput(inputEvent.Data)
			controller := service.ControllerSnapshot()
			projection := buildProjectionSnapshotFor(sessionID)
			snapshot := service.RuntimeSnapshot()
			inputData := inputEvent.Data
			if shouldInjectEnabledSkillsForInput(
				firstNonEmptyString(snapshot.ActiveMeta.Command, controller.CurrentCommand, projection.Runtime.Command),
				snapshot.ActiveMeta.Engine,
				controller.ActiveMeta.Engine,
				projection.Runtime.Engine,
			) {
				skillPrefix, err := skills.BuildEnabledSkillsPrefix(h.SessionStore, projection.SessionContext)
				if err != nil {
					logx.Warn("ws", "build enabled skills prefix failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
				}
				memoryPrefix, memoryErr := skills.BuildEnabledMemoryPrefix(h.SessionStore, projection.SessionContext)
				if memoryErr != nil {
					logx.Warn("ws", "build enabled memory prefix failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, memoryErr)
				}
				inputData = skills.InjectConversationPrefixes(inputData, skillPrefix, memoryPrefix)
			}
			inputMeta := protocol.RuntimeMeta{}
			if pm := inputEvent.PermissionMode; pm != "" {
				service.UpdatePermissionMode(pm)
				inputMeta.PermissionMode = service.ControllerSnapshot().ActiveMeta.PermissionMode
			}
			// 权限请求待处理时，阻止普通文本输入写入 Claude stdin，
			// 防止 Claude 在等待结构化权限响应时收到无效文本导致 hang。
			if currentRunner := service.CurrentRunner(); currentRunner != nil {
				if prw, ok := currentRunner.(engine.PermissionResponseWriter); ok &&
					session.ShouldBlockInputForPendingPermission(prw, service, projection, sessionRuntime) {
					logx.Warn("ws", "input blocked by pending permission request: connectionID=%s sessionID=%s remoteAddr=%s preview=%q", connectionID, sessionID, remoteAddr, wsDebugPreview(inputData))
					emit(protocol.NewErrorEvent(sessionID, "有权限请求待处理，请先在 App 中完成授权", ""))
					continue
				}
			}
			logx.Info("ws", "dispatch input: connectionID=%s sessionID=%s remoteAddr=%s action=input permissionMode=%q preview=%q", connectionID, sessionID, remoteAddr, inputMeta.PermissionMode, wsDebugPreview(inputData))
			resumePermissionMode := normalizePermissionModeForClaude(firstNonEmptyString(
				inputEvent.PermissionMode,
				snapshot.ActiveMeta.PermissionMode,
				controller.ActiveMeta.PermissionMode,
				projection.Runtime.PermissionMode,
			))
			resumeReq := session.ExecuteRequest{
				Command: firstNonEmptyString(
					snapshot.ActiveMeta.Command,
					controller.CurrentCommand,
					projection.Runtime.Command,
					defaultAICommandFromEngine(
						snapshot.ActiveMeta.Engine,
						controller.ActiveMeta.Engine,
						projection.Runtime.Engine,
					),
				),
				CWD: firstNonEmptyString(
					snapshot.ActiveMeta.CWD,
					controller.ActiveMeta.CWD,
					projection.Runtime.CWD,
				),
				Mode:           engine.ModePTY,
				PermissionMode: resumePermissionMode,
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
						defaultAICommandFromEngine(
							snapshot.ActiveMeta.Engine,
							controller.ActiveMeta.Engine,
							projection.Runtime.Engine,
						),
					),
					CWD: firstNonEmptyString(
						snapshot.ActiveMeta.CWD,
						controller.ActiveMeta.CWD,
						projection.Runtime.CWD,
					),
					PermissionMode:    resumePermissionMode,
					ClaudeSessionUUID: lookupClaudeSessionUUID(sessionID),
				},
			}
			if service.ShouldEmitTransientResumeThinkingEvent(resumeReq) {
				emit(protocol.ApplyRuntimeMeta(
					protocol.NewAgentStateEvent(
						sessionID,
						string(session.ControllerStateThinking),
						"恢复会话中",
						false,
						resumeReq.Command,
						projection.Controller.LastStep,
						projection.Controller.LastTool,
					),
					protocol.MergeRuntimeMeta(projection.Controller.ActiveMeta, protocol.RuntimeMeta{
						Source:          "input",
						ResumeSessionID: resumeReq.RuntimeMeta.ResumeSessionID,
						Command:         resumeReq.Command,
						Engine:          firstNonEmptyString(resumeReq.RuntimeMeta.Engine, projection.Runtime.Engine),
						CWD:             resumeReq.CWD,
						PermissionMode:  resumeReq.PermissionMode,
						ClaudeLifecycle: "active",
					}),
				))
			}
			if err := service.SendInputOrResume(ctx, sessionID, resumeReq, session.InputRequest{Data: inputData, RuntimeMeta: inputMeta}, emitAndPersist); err != nil {
				message := err.Error()
				if errors.Is(err, engine.ErrInputNotSupported) {
					message = "input is only supported for pty sessions"
				} else if errors.Is(err, session.ErrNoActiveRunner) {
					message = "当前没有活跃会话，且没有可恢复的 Claude 会话，请重新发起命令"
				} else if errors.Is(err, session.ErrResumeSessionUnavailable) {
					message = "当前没有 resume id，无法恢复 Claude 会话，请重新发起命令"
				} else if errors.Is(err, session.ErrResumeConversationNotFound) {
					message = "当前 Claude 会话的 resume id 已失效或不存在，请重新发起命令"
				} else if errors.Is(err, session.ErrRunnerNotInteractive) {
					message = "Claude 恢复后未进入可输入状态，请稍后重试"
				}
				logx.Warn("ws", "service send input failed: connectionID=%s sessionID=%s remoteAddr=%s action=input err=%v", connectionID, sessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(sessionID, message, ""))
			} else {
				appendUserProjectionEntry(h.SessionStore, ctx, sessionID, strings.TrimRight(inputEvent.Data, "\n"), "回复", connectionID, remoteAddr)
			}
		case "stop":
			if strings.TrimSpace(selectedSessionID) == "" || selectedSessionID == connectionID {
				continue
			}
			_, service := runtimeForSession(selectedSessionID)
			if err := service.StopActive(selectedSessionID, emitAndPersistFor(selectedSessionID)); err != nil && !errors.Is(err, session.ErrNoActiveRunner) {
				logx.Warn("ws", "stop active runner failed: connectionID=%s sessionID=%s remoteAddr=%s action=stop err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
			}
		case "permission_decision":
			var permissionEvent protocol.PermissionDecisionRequestEvent
			if err := json.Unmarshal(payload, &permissionEvent); err != nil {
				logx.Warn("ws", "invalid permission decision request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid permission decision request: %v", err), ""))
				continue
			}
			logx.Info("ws", "incoming action: connectionID=%s sessionID=%s remoteAddr=%s action=permission_decision decision=%q permissionRequestID=%q permissionMode=%q resumeSessionID=%q targetPath=%q contextID=%q fallbackCWD=%q fallbackCommandPreview=%q promptPreview=%q", connectionID, selectedSessionID, remoteAddr, permissionEvent.Decision, permissionEvent.PermissionRequestID, permissionEvent.PermissionMode, permissionEvent.ResumeSessionID, permissionEvent.TargetPath, permissionEvent.ContextID, permissionEvent.FallbackCWD, wsDebugPreview(permissionEvent.FallbackCommand), wsDebugPreview(permissionEvent.PromptMessage))
			decision := strings.TrimSpace(strings.ToLower(permissionEvent.Decision))
			if decision != "approve" && decision != "deny" {
				logx.Warn("ws", "reject invalid permission decision: connectionID=%s sessionID=%s remoteAddr=%s decision=%q", connectionID, selectedSessionID, remoteAddr, permissionEvent.Decision)
				emit(protocol.NewErrorEvent(selectedSessionID, "permission decision must be one of: approve, deny", ""))
				continue
			}
			sessionID, service := resolvePermissionDecisionRuntime(permissionEvent)
			sessionRuntime, _ := runtimeForSession(sessionID)
			emitAndPersist := emitAndPersistFor(sessionID)
			projection := buildProjectionSnapshotFor(sessionID)
			controller := service.ControllerSnapshot()
			if requestedID := strings.TrimSpace(permissionEvent.PermissionRequestID); requestedID != "" {
				currentID := strings.TrimSpace(service.CurrentPermissionRequestID(sessionID))
				if currentID != "" && currentID != requestedID {
					// 用户的 approve 是"对当前可见提示放行"的意图。若客户端拿到的还是上一轮提示的 id，
					// 直接把当前 pending 的 id 套上去执行，避免出现"点击没反应"的二次往返。
					// deny 仍走 refresh 路径，要求用户对真正的当前请求做明确拒绝。
					if decision == "approve" {
						logx.Info("ws", "permission decision request id stale, auto-applying approve to current pending: connectionID=%s sessionID=%s remoteAddr=%s clientRequestID=%q currentRequestID=%q", connectionID, sessionID, remoteAddr, requestedID, currentID)
						permissionEvent.PermissionRequestID = currentID
					} else {
						var refreshed *protocol.PromptRequestEvent
						if sessionRuntime != nil {
							refreshed = sessionRuntime.latestPendingPermissionPrompt(currentID)
						}
						if refreshed == nil {
							refreshed = session.RefreshedPermissionPromptEvent(sessionID, permissionEvent, service)
						}
						if refreshed != nil {
							logx.Info("ws", "permission decision request id stale, refreshing current request id: connectionID=%s sessionID=%s remoteAddr=%s clientRequestID=%q currentRequestID=%q decision=%q", connectionID, sessionID, remoteAddr, requestedID, currentID, decision)
							emit(*refreshed)
							continue
						}
					}
				}
			}
			appendUserProjectionEntry(h.SessionStore, ctx, sessionID, strings.TrimSpace(permissionEvent.PromptMessage), "权限决策", connectionID, remoteAddr)
			scope := strings.TrimSpace(permissionEvent.Scope)
			if decision == "approve" && (scope == string(data.PermissionScopeSession) || scope == string(data.PermissionScopePersistent)) {
				rule := buildPermissionRule(permissionEvent, scope, projection, controller)
				switch data.PermissionScope(scope) {
				case data.PermissionScopePersistent:
					snapshot, err := h.SessionStore.GetPermissionRuleSnapshot(ctx)
					if err == nil {
						snapshot.Enabled = true
						snapshot.Items = upsertPermissionRule(snapshot.Items, rule)
						_ = h.SessionStore.SavePermissionRuleSnapshot(ctx, snapshot)
					}
				default:
					record, err := h.SessionStore.GetSession(ctx, sessionID)
					if err == nil {
						record.Projection = session.NormalizeProjectionSnapshot(record.Projection)
						record.Projection.PermissionRulesEnabled = true
						record.Projection.PermissionRules = upsertPermissionRule(record.Projection.PermissionRules, rule)
						_, _ = h.SessionStore.SaveProjection(ctx, sessionID, record.Projection)
					}
				}
				emitPermissionRuleList(emit, h.SessionStore, ctx, sessionID)
			}
			err := executePermissionDecision(ctx, sessionID, permissionEvent, service, projection, controller, emitAndPersist)
			if err == nil {
				continue
			}
			message := err.Error()
			if errors.Is(err, session.ErrNoActiveRunner) {
				message = "当前没有可交互的 Claude 会话，无法继续处理该权限请求"
			} else if errors.Is(err, session.ErrPermissionRequestExpired) {
				if refreshed := session.RefreshedPermissionPromptEvent(sessionID, permissionEvent, service); refreshed != nil {
					logx.Info("ws", "permission decision expired, refreshing current request id: connectionID=%s sessionID=%s remoteAddr=%s clientRequestID=%q currentRequestID=%q", connectionID, sessionID, remoteAddr, permissionEvent.PermissionRequestID, refreshed.PermissionRequestID)
					emit(*refreshed)
					continue
				}
				message = "当前权限请求已失效，请等待 AI 重新发起操作后再确认"
			} else if errors.Is(err, engine.ErrInputNotSupported) {
				message = "当前会话不支持交互输入，请先恢复 Claude PTY 会话"
			} else if errors.Is(err, session.ErrResumeSessionUnavailable) {
				message = "当前没有可恢复的 Claude 会话，无法继续此次权限批准"
			} else if errors.Is(err, session.ErrResumeConversationNotFound) {
				message = "当前 Claude 会话的 resume id 已失效或不存在，无法继续此次权限批准"
			} else if errors.Is(err, session.ErrRunnerNotInteractive) {
				message = "Claude 恢复后未进入可输入状态，无法继续刚才的操作"
			}
			logx.Warn("ws", "permission decision failed: connectionID=%s sessionID=%s remoteAddr=%s decision=%s err=%v", connectionID, sessionID, remoteAddr, decision, err)
			emit(protocol.NewErrorEvent(sessionID, message, ""))

		case "review_decision":
			var reviewEvent protocol.ReviewDecisionRequestEvent
			if err := json.Unmarshal(payload, &reviewEvent); err != nil {
				logx.Warn("ws", "invalid review decision request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid review decision request: %v", err), ""))
				continue
			}
			logx.Info("ws", "incoming action: connectionID=%s sessionID=%s remoteAddr=%s action=review_decision decision=%q executionID=%q groupID=%q groupTitle=%q contextID=%q contextTitle=%q targetPath=%q permissionMode=%q isReviewOnly=%t", connectionID, selectedSessionID, remoteAddr, reviewEvent.Decision, reviewEvent.ExecutionID, reviewEvent.GroupID, reviewEvent.GroupTitle, reviewEvent.ContextID, reviewEvent.ContextTitle, reviewEvent.TargetPath, reviewEvent.PermissionMode, reviewEvent.IsReviewOnly)
			decision := strings.TrimSpace(strings.ToLower(reviewEvent.Decision))
			if decision != "accept" && decision != "revert" && decision != "revise" {
				logx.Warn("ws", "reject invalid review decision: connectionID=%s sessionID=%s remoteAddr=%s decision=%q", connectionID, selectedSessionID, remoteAddr, reviewEvent.Decision)
				emit(protocol.NewErrorEvent(selectedSessionID, "review decision must be one of: accept, revert, revise", ""))
				continue
			}
			sessionID := selectedSessionID
			_, service := runtimeForSession(sessionID)
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
			interactionType := "none"
			if controller.ActiveMeta.BlockingKind != "" {
				interactionType = controller.ActiveMeta.BlockingKind
			}
			logx.Info("ws", "review_decision routing: connectionID=%s sessionID=%s targetId=%q groupId=%q interactionType=%q isReviewOnly=%t", connectionID, sessionID, firstNonEmptyString(reviewEvent.ContextID, currentDiff.ContextID), firstNonEmptyString(reviewEvent.GroupID, currentDiff.GroupID), interactionType, reviewEvent.IsReviewOnly)
			if err := service.ReviewDecision(ctx, sessionID, session.ReviewDecisionRequest{
				Decision:     decision,
				IsReviewOnly: reviewEvent.IsReviewOnly,
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
				if errors.Is(err, engine.ErrInputNotSupported) {
					message = "当前会话不支持交互输入，请先恢复 Claude PTY 会话"
				} else if errors.Is(err, session.ErrNoActiveRunner) {
					message = "当前没有可交互会话，请先恢复会话后再审核 diff"
				} else if errors.Is(err, session.ErrRunnerNotInteractive) {
					message = "当前 Claude 会话尚未进入可直接确认的交互阶段，请先等待当前会话就绪后再提交审核决策"
				}
				logx.Warn("ws", "send review decision failed: connectionID=%s sessionID=%s remoteAddr=%s decision=%s err=%v", connectionID, sessionID, remoteAddr, decision, err)
				emit(protocol.NewErrorEvent(sessionID, message, ""))
				continue
			}
			projection = session.ApplyReviewDecisionToProjection(projection, reviewEvent, decision, currentDiff)
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
			_, service := runtimeForSession(selectedSessionID)
			emitAndPersist := emitAndPersistFor(selectedSessionID)
			req := session.PlanDecisionRequest{
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
				if errors.Is(err, session.ErrNoActiveRunner) {
					message = "当前没有可交互的 Claude 会话，无法继续处理该 plan 请求"
				} else if errors.Is(err, session.ErrRunnerNotInteractive) {
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
			_, service := runtimeForSession(selectedSessionID)
			service.UpdatePermissionMode(modeEvent.PermissionMode)
			effectivePermissionMode := service.ControllerSnapshot().ActiveMeta.PermissionMode
			emitAndPersistFor(selectedSessionID)(protocol.ApplyRuntimeMeta(service.InitialEvent(), protocol.RuntimeMeta{PermissionMode: effectivePermissionMode}))
			if strings.TrimSpace(effectivePermissionMode) != "" && session.NormalizeClaudePermissionMode(effectivePermissionMode) != "default" {
				projection := session.ApplyAutoReviewAcceptanceToProjection(buildProjectionSnapshotFor(selectedSessionID))
				persistProjectionFor(selectedSessionID, projection)
				emitReviewStateFromProjection(emit, selectedSessionID, projection)
			}
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
			sessionContext := data.SessionContext{}
			if h.SessionStore != nil {
				record, ok := loadSelectedSessionRecord(h.SessionStore, ctx, selectedSessionID, emit)
				if !ok {
					continue
				}
				sessionContext = record.Projection.SessionContext
			}
			sessionID := selectedSessionID
			_, service := runtimeForSession(sessionID)
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
			result, err := session.BuildRuntimeInfoResult(selectedSessionID, infoReq.Query, fallback(infoReq.CWD, "."), runtimeSvc)
			if err != nil {
				logx.Warn("ws", "build runtime info failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			emit(result)
		case "runtime_process_list":
			rootPID, items, err := runtimeSvc.ActiveProcessTree(ctx)
			if err != nil {
				logx.Warn("ws", "build runtime process list failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			message := ""
			if len(items) == 0 {
				message = "当前没有活跃的后台进程"
			}
			emit(protocol.NewRuntimeProcessListResultEvent(selectedSessionID, rootPID, items, message))
		case "runtime_process_log_get":
			var processReq protocol.RuntimeProcessLogRequestEvent
			if err := json.Unmarshal(payload, &processReq); err != nil {
				logx.Warn("ws", "invalid runtime_process_log_get request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid runtime_process_log_get request: %v", err), ""))
				continue
			}
			if processReq.PID <= 0 {
				emit(protocol.NewErrorEvent(selectedSessionID, "pid 必须为正整数", ""))
				continue
			}
			_, items, err := runtimeSvc.ActiveProcessTree(ctx)
			if err != nil {
				logx.Warn("ws", "load runtime process before log failed: connectionID=%s sessionID=%s remoteAddr=%s pid=%d err=%v", connectionID, selectedSessionID, remoteAddr, processReq.PID, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			item, ok := findRuntimeProcessItem(items, processReq.PID)
			if !ok {
				emit(protocol.NewErrorEvent(selectedSessionID, "指定进程不存在或已退出", ""))
				continue
			}
			projection := buildProjectionSnapshotFor(selectedSessionID)
			stdout, stderr, message := resolveRuntimeProcessLogs(item, projection)
			emit(protocol.NewRuntimeProcessLogResultEvent(
				selectedSessionID,
				item.PID,
				item.ExecutionID,
				item.Command,
				item.CWD,
				item.Source,
				stdout,
				stderr,
				message,
			))
		case "adb_devices":
			var adbReq protocol.ADBDevicesRequestEvent
			if err := json.Unmarshal(payload, &adbReq); err != nil {
				logx.Warn("ws", "invalid adb_devices request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid adb_devices request: %v", err), ""))
				continue
			}
			emitADBDevices("ADB 设备列表已刷新")
		case "adb_stream_start":
			var adbReq protocol.ADBStreamStartRequestEvent
			if err := json.Unmarshal(payload, &adbReq); err != nil {
				logx.Warn("ws", "invalid adb_stream_start request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid adb_stream_start request: %v", err), ""))
				continue
			}
			interval := time.Duration(adbReq.IntervalMS) * time.Millisecond
			if interval <= 0 {
				interval = 700 * time.Millisecond
			}
			if interval < 250*time.Millisecond {
				interval = 250 * time.Millisecond
			}
			adbRTC.Stop("")
			startADBStream(adbReq.Serial, interval)
		case "adb_stream_stop":
			var adbReq protocol.ADBStreamStopRequestEvent
			if err := json.Unmarshal(payload, &adbReq); err != nil {
				logx.Warn("ws", "invalid adb_stream_stop request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid adb_stream_stop request: %v", err), ""))
				continue
			}
			stopADBStream("ADB 画面预览已停止")
			adbRTC.Stop("")
		case "adb_emulator_start":
			var adbReq protocol.ADBEmulatorStartRequestEvent
			if err := json.Unmarshal(payload, &adbReq); err != nil {
				logx.Warn("ws", "invalid adb_emulator_start request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid adb_emulator_start request: %v", err), ""))
				continue
			}
			if err := adb.StartEmulator(adbReq.AVD); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			emitADBDevices("模拟器启动中，等待设备上线…")
		case "adb_tap":
			var adbReq protocol.ADBTapRequestEvent
			if err := json.Unmarshal(payload, &adbReq); err != nil {
				logx.Warn("ws", "invalid adb_tap request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid adb_tap request: %v", err), ""))
				continue
			}
			if adbReq.X < 0 || adbReq.Y < 0 {
				emit(protocol.NewErrorEvent(selectedSessionID, "adb tap 坐标必须为非负整数", ""))
				continue
			}
			adbMu.Lock()
			activeSerial := adbActiveSerial
			adbMu.Unlock()
			serial := strings.TrimSpace(adbReq.Serial)
			if serial == "" {
				serial = activeSerial
			}
			if err := adb.Tap(ctx, serial, adbReq.X, adbReq.Y); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
		case "adb_swipe":
			var adbReq protocol.ADBSwipeRequestEvent
			if err := json.Unmarshal(payload, &adbReq); err != nil {
				logx.Warn("ws", "invalid adb_swipe request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid adb_swipe request: %v", err), ""))
				continue
			}
			if adbReq.StartX < 0 || adbReq.StartY < 0 || adbReq.EndX < 0 || adbReq.EndY < 0 {
				emit(protocol.NewErrorEvent(selectedSessionID, "adb swipe 坐标必须为非负整数", ""))
				continue
			}
			adbMu.Lock()
			activeSerial := adbActiveSerial
			adbMu.Unlock()
			serial := strings.TrimSpace(adbReq.Serial)
			if serial == "" {
				serial = activeSerial
			}
			if err := adb.Swipe(ctx, serial, adbReq.StartX, adbReq.StartY, adbReq.EndX, adbReq.EndY, adbReq.DurationMS); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
		case "adb_keyevent":
			var adbReq protocol.ADBKeyeventRequestEvent
			if err := json.Unmarshal(payload, &adbReq); err != nil {
				logx.Warn("ws", "invalid adb_keyevent request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid adb_keyevent request: %v", err), ""))
				continue
			}
			if strings.TrimSpace(adbReq.Keycode) == "" {
				emit(protocol.NewErrorEvent(selectedSessionID, "adb keyevent keycode 不能为空", ""))
				continue
			}
			adbMu.Lock()
			activeSerial := adbActiveSerial
			adbMu.Unlock()
			serial := strings.TrimSpace(adbReq.Serial)
			if serial == "" {
				serial = activeSerial
			}
			if err := adb.Keyevent(ctx, serial, adbReq.Keycode); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
		case "adb_webrtc_offer":
			var adbReq protocol.ADBWebRTCOfferRequestEvent
			if err := json.Unmarshal(payload, &adbReq); err != nil {
				logx.Warn("ws", "invalid adb_webrtc_offer request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid adb_webrtc_offer request: %v", err), ""))
				continue
			}
			logx.Info(
				"ws",
				"incoming adb_webrtc_offer: connectionID=%s sessionID=%s remoteAddr=%s serial=%q sdpType=%q sdpBytes=%d iceServers=%d",
				connectionID,
				selectedSessionID,
				remoteAddr,
				adbReq.Serial,
				adbReq.Type,
				len(strings.TrimSpace(adbReq.SDP)),
				len(adbReq.ICEServers),
			)
			stopADBStream("")
			if err := adbRTC.HandleOffer(ctx, adbReq.Serial, adbReq.Type, adbReq.SDP, adbReq.ICEServers); err != nil {
				logx.Warn(
					"ws",
					"adb_webrtc_offer failed: connectionID=%s sessionID=%s remoteAddr=%s serial=%q err=%v",
					connectionID,
					selectedSessionID,
					remoteAddr,
					adbReq.Serial,
					err,
				)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			logx.Info(
				"ws",
				"adb_webrtc_offer handled: connectionID=%s sessionID=%s remoteAddr=%s serial=%q",
				connectionID,
				selectedSessionID,
				remoteAddr,
				adbReq.Serial,
			)
		case "adb_webrtc_stop":
			var adbReq protocol.ADBWebRTCStopRequestEvent
			if err := json.Unmarshal(payload, &adbReq); err != nil {
				logx.Warn("ws", "invalid adb_webrtc_stop request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid adb_webrtc_stop request: %v", err), ""))
				continue
			}
			logx.Info(
				"ws",
				"incoming adb_webrtc_stop: connectionID=%s sessionID=%s remoteAddr=%s",
				connectionID,
				selectedSessionID,
				remoteAddr,
			)
			adbRTC.Stop("ADB WebRTC 调试已停止")
		case "slash_command":
			var slashReq protocol.SlashCommandRequestEvent
			if err := json.Unmarshal(payload, &slashReq); err != nil {
				logx.Warn("ws", "invalid slash_command request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid slash_command request: %v", err), ""))
				continue
			}
			parsedSlash, err := parseSlashCommand(slashReq.Command)
			if err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			sessionContext := data.SessionContext{}
			if h.SessionStore != nil && strings.TrimSpace(selectedSessionID) != "" {
				record, ok := loadSelectedSessionRecord(h.SessionStore, ctx, selectedSessionID, emit)
				if !ok {
					continue
				}
				sessionContext = record.Projection.SessionContext
			}
			sessionID := strings.TrimSpace(firstNonEmptyString(slashReq.SessionID, selectedSessionID))
			if sessionID == "" && (parsedSlash.spec.category == "local" || parsedSlash.spec.category == "runtime-info") {
				sessionID = selectedSessionID
			}
			if sessionID == "" && parsedSlash.spec.category == "exec" {
				sessionID = connectionID
			}
			if sessionID == "" &&
				parsedSlash.spec.category != "local" &&
				parsedSlash.spec.category != "runtime-info" {
				emit(protocol.NewErrorEvent(selectedSessionID, "请先创建或加载会话后再发送命令", ""))
				continue
			}
			if sessionID == connectionID && parsedSlash.spec.category != "exec" {
				emit(protocol.NewErrorEvent(selectedSessionID, "请先创建或加载会话后再发送命令", ""))
				continue
			}
			if sessionID != strings.TrimSpace(selectedSessionID) {
				switchRuntimeSession(sessionID)
			}
			if !ackClientAction(sessionID, "slash_command", slashReq.ClientEvent) {
				logx.Info("ws", "duplicate client action ignored: connectionID=%s sessionID=%s remoteAddr=%s action=slash_command clientActionID=%s", connectionID, sessionID, remoteAddr, slashReq.ClientActionID)
				continue
			}
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

func appendUserProjectionEntry(sessionStore data.Store, ctx context.Context, sessionID, text, label, connectionID, remoteAddr string) {
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
	projection := session.NormalizeProjectionSnapshot(record.Projection)
	projection.LogEntries = append(projection.LogEntries, data.SnapshotLogEntry{
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

func readProjectionFromSessionStore(sessionStore data.Store, ctx context.Context, sessionID, connectionID, remoteAddr string) data.ProjectionSnapshot {
	record, ok := readSessionRecordForProjection(sessionStore, ctx, sessionID, connectionID, remoteAddr)
	if !ok {
		return data.ProjectionSnapshot{RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""}}
	}
	augmented := augmentLocalClaudeRecordFromNative(ctx, record)
	return augmented.Projection
}

func readProjectionFromSessionStoreRaw(sessionStore data.Store, ctx context.Context, sessionID, connectionID, remoteAddr string) data.ProjectionSnapshot {
	record, ok := readSessionRecordForProjection(sessionStore, ctx, sessionID, connectionID, remoteAddr)
	if !ok {
		return data.ProjectionSnapshot{RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""}}
	}
	return record.Projection
}

func readSessionRecordForProjection(sessionStore data.Store, ctx context.Context, sessionID, connectionID, remoteAddr string) (data.SessionRecord, bool) {
	if sessionStore == nil || strings.TrimSpace(sessionID) == "" {
		if sessionStore == nil {
			logx.Warn("ws", "projection restore skipped because session store unavailable: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, sessionID, remoteAddr)
		}
		return data.SessionRecord{}, false
	}
	record, err := sessionStore.GetSession(ctx, sessionID)
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		fallbackCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		record, err = sessionStore.GetSession(fallbackCtx, sessionID)
	}
	if err != nil {
		logx.Warn("ws", "read projection from session store failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
		return data.SessionRecord{}, false
	}
	return record, true
}

func mergeProjectionWithOptionalRuntime(base data.ProjectionSnapshot, runtimeProjection data.ProjectionSnapshot, svc *session.Service, sessionID string) data.ProjectionSnapshot {
	base = session.NormalizeProjectionSnapshot(base)
	runtimeProjection = session.NormalizeProjectionSnapshot(runtimeProjection)
	if svc == nil {
		return base
	}
	runtimeSnapshot := svc.RuntimeSnapshot()
	runtimeActiveForSession := runtimeSnapshot.Running && strings.TrimSpace(runtimeSnapshot.ActiveSession) == strings.TrimSpace(sessionID)
	if !runtimeActiveForSession {
		return base
	}
	merged := mergeCodexMirrorProjection(base, runtimeProjection)
	merged.LogEntries = mergeSnapshotLogEntries(base.LogEntries, runtimeProjection.LogEntries)
	return session.NormalizeProjectionSnapshot(merged)
}

func sessionRecordRuntimeAlive(record data.SessionRecord, svc *session.Service) bool {
	allowStoredRuntime := record.Summary.External ||
		codexsync.IsMirrorSessionID(record.Summary.ID) ||
		claudesync.IsMirrorSessionID(record.Summary.ID)
	return session.SessionRecordRuntimeAlive(record, svc, allowStoredRuntime)
}

func taskCursorSnapshot(sessionRuntime *runtimeSession) session.TaskCursorSnapshot {
	if sessionRuntime == nil {
		return session.TaskCursorSnapshot{}
	}
	return session.TaskCursorSnapshot{
		LatestCursor: sessionRuntime.latestCursor(),
		LastOutputAt: sessionRuntime.lastOutputTime(),
	}
}

func deltaCursorSnapshot(sessionRuntime *runtimeSession) session.DeltaCursorSnapshot {
	if sessionRuntime == nil {
		return session.DeltaCursorSnapshot{}
	}
	return session.DeltaCursorSnapshot{LatestCursor: sessionRuntime.latestCursor()}
}

func toProtocolSummary(item data.SessionSummary) protocol.SessionSummary {
	return session.ToProtocolSummary(item)
}

func toProtocolSummaries(items []data.SessionSummary) []protocol.SessionSummary {
	result := make([]protocol.SessionSummary, 0, len(items))
	for _, item := range items {
		result = append(result, toProtocolSummary(item))
	}
	return result
}

func toProtocolCatalogMetadata(meta data.CatalogMetadata) protocol.CatalogMetadata {
	return session.ToProtocolCatalogMetadata(meta)
}

func toProtocolSessionContext(ctx data.SessionContext) protocol.SessionContext {
	return session.ToProtocolSessionContext(ctx)
}

func toProtocolSkillDefinitions(items []data.SkillDefinition) []protocol.SkillDefinition {
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

func toProtocolMemoryItems(items []data.MemoryItem) []protocol.MemoryItem {
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

func loadSelectedSessionRecord(sessionStore data.Store, ctx context.Context, sessionID string, emit func(any)) (data.SessionRecord, bool) {
	if sessionStore == nil {
		emit(protocol.NewErrorEvent(sessionID, "session store unavailable", ""))
		return data.SessionRecord{}, false
	}
	record, err := sessionStore.GetSession(ctx, sessionID)
	if err != nil {
		emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
		return data.SessionRecord{}, false
	}
	return record, true
}

func emitSkillCatalogResult(emit func(any), sessionStore data.Store, ctx context.Context, sessionID string) {
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

func emitMemoryListResult(emit func(any), sessionStore data.Store, ctx context.Context, sessionID string) {
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

func findRuntimeProcessItem(items []protocol.RuntimeProcessItem, pid int) (protocol.RuntimeProcessItem, bool) {
	for _, item := range items {
		if item.PID == pid {
			return item, true
		}
	}
	return protocol.RuntimeProcessItem{}, false
}

func resolveRuntimeProcessLogs(item protocol.RuntimeProcessItem, projection data.ProjectionSnapshot) (string, string, string) {
	projection = session.NormalizeProjectionSnapshot(projection)
	if executionID := strings.TrimSpace(item.ExecutionID); executionID != "" {
		for _, execution := range projection.TerminalExecutions {
			if strings.TrimSpace(execution.ExecutionID) != executionID {
				continue
			}
			message := ""
			if strings.TrimSpace(execution.Stdout) == "" && strings.TrimSpace(execution.Stderr) == "" {
				message = "该进程暂无已捕获的 stdout / stderr"
			}
			return execution.Stdout, execution.Stderr, message
		}
	}
	stdout := projection.RawTerminalByStream["stdout"]
	stderr := projection.RawTerminalByStream["stderr"]
	message := ""
	if strings.TrimSpace(stdout) == "" && strings.TrimSpace(stderr) == "" {
		message = "该进程暂无可展示的捕获日志"
	}
	return stdout, stderr, message
}

func upsertLocalSkill(sessionStore data.Store, ctx context.Context, item protocol.SkillDefinition) error {
	if sessionStore == nil {
		return fmt.Errorf("session store unavailable")
	}
	snapshot, err := sessionStore.GetSkillCatalogSnapshot(ctx)
	if err != nil {
		return err
	}
	updatedAt := time.Now().UTC()
	next := data.SkillDefinition{
		Name:          strings.TrimSpace(item.Name),
		Description:   strings.TrimSpace(item.Description),
		Prompt:        strings.TrimSpace(item.Prompt),
		ResultView:    strings.TrimSpace(item.ResultView),
		TargetType:    strings.TrimSpace(item.TargetType),
		Source:        data.SkillSourceLocal,
		SourceOfTruth: data.CatalogSourceTruthClaude,
		SyncState:     data.CatalogSyncStateDraft,
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
	snapshot.Meta.SyncState = data.CatalogSyncStateDraft
	snapshot.Meta.DriftDetected = true
	snapshot.Meta.SourceOfTruth = data.CatalogSourceTruthClaude
	snapshot.Meta.LastError = ""
	return sessionStore.SaveSkillCatalogSnapshot(ctx, snapshot)
}

func syncExternalSkills(sessionStore data.Store, ctx context.Context, sourceOfTruth data.CatalogSourceOfTruth) error {
	if sessionStore == nil {
		return fmt.Errorf("session store unavailable")
	}
	snapshot, err := sessionStore.GetSkillCatalogSnapshot(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	externalItems, err := loadExternalSkillDefinitions(sourceOfTruth, now)
	if err != nil {
		return err
	}
	filtered := make([]data.SkillDefinition, 0, len(snapshot.Items)+len(externalItems))
	seen := make(map[string]struct{}, len(snapshot.Items)+len(externalItems))
	for _, item := range snapshot.Items {
		if item.Source == data.SkillSourceLocal {
			filtered = append(filtered, item)
			seen[item.Name] = struct{}{}
		}
	}
	for _, item := range externalItems {
		if _, ok := seen[item.Name]; ok {
			continue
		}
		filtered = append(filtered, item)
	}
	snapshot.Items = filtered
	snapshot.Meta.SourceOfTruth = sourceOfTruth
	snapshot.Meta.SyncState = data.CatalogSyncStateSynced
	snapshot.Meta.DriftDetected = catalogHasDraftSkill(filtered)
	snapshot.Meta.LastSyncedAt = now
	snapshot.Meta.LastError = ""
	snapshot.Meta.VersionToken = fmt.Sprintf("skills-%d", now.UnixNano())
	return sessionStore.SaveSkillCatalogSnapshot(ctx, snapshot)
}

func upsertMemoryItem(sessionStore data.Store, ctx context.Context, item protocol.MemoryItem) error {
	if sessionStore == nil {
		return fmt.Errorf("session store unavailable")
	}
	snapshot, err := sessionStore.GetMemoryCatalogSnapshot(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	next := data.MemoryItem{
		ID:            strings.TrimSpace(item.ID),
		Title:         strings.TrimSpace(item.Title),
		Content:       strings.TrimSpace(item.Content),
		Source:        firstNonEmptyString(strings.TrimSpace(item.Source), "local"),
		SourceOfTruth: data.CatalogSourceTruthClaude,
		SyncState:     data.CatalogSyncStateDraft,
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
	snapshot.Meta.SourceOfTruth = data.CatalogSourceTruthClaude
	snapshot.Meta.SyncState = data.CatalogSyncStateDraft
	snapshot.Meta.DriftDetected = true
	snapshot.Meta.LastError = ""
	return sessionStore.SaveMemoryCatalogSnapshot(ctx, snapshot)
}

func syncExternalMemories(sessionStore data.Store, ctx context.Context, cwd string, sourceOfTruth data.CatalogSourceOfTruth) error {
	if sessionStore == nil {
		return fmt.Errorf("session store unavailable")
	}
	snapshot, err := sessionStore.GetMemoryCatalogSnapshot(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	externalItems, err := loadExternalMemoryItems(sourceOfTruth, cwd, now)
	if err != nil {
		return err
	}
	filtered := make([]data.MemoryItem, 0, len(snapshot.Items)+len(externalItems))
	seen := make(map[string]struct{}, len(snapshot.Items)+len(externalItems))
	for _, item := range snapshot.Items {
		if item.Source == "local" {
			filtered = append(filtered, item)
			seen[item.ID] = struct{}{}
		}
	}
	for _, item := range externalItems {
		if _, ok := seen[item.ID]; ok {
			continue
		}
		filtered = append(filtered, item)
	}
	snapshot.Items = filtered
	snapshot.Meta.SourceOfTruth = sourceOfTruth
	snapshot.Meta.SyncState = data.CatalogSyncStateSynced
	snapshot.Meta.DriftDetected = catalogHasDraftMemory(filtered)
	snapshot.Meta.LastSyncedAt = now
	snapshot.Meta.LastError = ""
	snapshot.Meta.VersionToken = fmt.Sprintf("memory-%d", now.UnixNano())
	return sessionStore.SaveMemoryCatalogSnapshot(ctx, snapshot)
}

func resolveCatalogSyncCWD(sessionStore data.Store, ctx context.Context, sessionID, fallbackCWD string) string {
	if sessionStore != nil && strings.TrimSpace(sessionID) != "" {
		record, err := sessionStore.GetSession(ctx, sessionID)
		if err == nil {
			if cwd := normalizeSessionCWD(record.Projection.Runtime.CWD); cwd != "" {
				return cwd
			}
			if cwd := normalizeSessionCWD(record.Summary.Runtime.CWD); cwd != "" {
				return cwd
			}
		}
	}
	return normalizeSessionCWD(fallbackCWD)
}

func resolveCatalogSourceOfTruth(sessionStore data.Store, ctx context.Context, sessionID string) data.CatalogSourceOfTruth {
	if sessionStore == nil || strings.TrimSpace(sessionID) == "" {
		return data.CatalogSourceTruthClaude
	}
	record, err := sessionStore.GetSession(ctx, sessionID)
	if err != nil {
		return data.CatalogSourceTruthClaude
	}
	if isCodexRuntime(record.Projection.Runtime) || isCodexRuntime(record.Summary.Runtime) || strings.EqualFold(strings.TrimSpace(record.Summary.Source), "codex-native") {
		return data.CatalogSourceTruthCodex
	}
	return data.CatalogSourceTruthClaude
}

func isCodexRuntime(runtime data.SessionRuntime) bool {
	if strings.EqualFold(strings.TrimSpace(runtime.Source), "codex-native") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(runtime.Engine), "codex") {
		return true
	}
	head := strings.ToLower(strings.TrimSpace(commandHead(runtime.Command)))
	return head == "codex" || strings.HasSuffix(head, "/codex") || strings.HasSuffix(head, `\codex`) || head == "codex.exe"
}

func isClaudeRuntime(runtime data.SessionRuntime) bool {
	if strings.EqualFold(strings.TrimSpace(runtime.Source), "claude-native") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(runtime.Engine), "claude") {
		return true
	}
	head := strings.ToLower(strings.TrimSpace(commandHead(runtime.Command)))
	return head == "claude" || strings.HasSuffix(head, "/claude") || strings.HasSuffix(head, `\claude`) || head == "claude.exe"
}

func commandHead(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func loadExternalSkillDefinitions(sourceOfTruth data.CatalogSourceOfTruth, now time.Time) ([]data.SkillDefinition, error) {
	switch sourceOfTruth {
	case data.CatalogSourceTruthCodex:
		return loadCodexSkillDefinitions(now)
	default:
		return loadClaudeSkillDefinitions(now)
	}
}

func loadExternalMemoryItems(sourceOfTruth data.CatalogSourceOfTruth, cwd string, now time.Time) ([]data.MemoryItem, error) {
	switch sourceOfTruth {
	case data.CatalogSourceTruthCodex:
		return loadCodexMemories(now)
	default:
		return loadClaudeProjectMemories(cwd, now)
	}
}

func loadClaudeSkillDefinitions(now time.Time) ([]data.SkillDefinition, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir for skill sync: %w", err)
	}
	return loadSkillsFromRoot(filepath.Join(homeDir, ".claude", "skills"), now, data.CatalogSourceTruthClaude)
}

func loadCodexSkillDefinitions(now time.Time) ([]data.SkillDefinition, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir for codex skill sync: %w", err)
	}
	return loadSkillsFromRoot(filepath.Join(homeDir, ".codex", "skills"), now, data.CatalogSourceTruthCodex)
}

func loadSkillsFromRoot(root string, now time.Time, sourceOfTruth data.CatalogSourceOfTruth) ([]data.SkillDefinition, error) {
	entries := make([]data.SkillDefinition, 0)
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.EqualFold(d.Name(), "SKILL.md") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("read skill file %s: %w", path, err)
		}
		meta, body := parseMarkdownFrontMatter(string(content))
		name := firstNonEmptyString(strings.TrimSpace(meta["name"]), strings.TrimSpace(filepath.Base(filepath.Dir(path))))
		if name == "" {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat skill file %s: %w", path, err)
		}
		entries = append(entries, data.SkillDefinition{
			Name:          name,
			Description:   strings.TrimSpace(meta["description"]),
			Prompt:        strings.TrimSpace(body),
			ResultView:    firstNonEmptyString(strings.TrimSpace(meta["resultview"]), "review-card"),
			TargetType:    firstNonEmptyString(strings.TrimSpace(meta["targettype"]), "context"),
			Source:        data.SkillSourceExternal,
			SourceOfTruth: sourceOfTruth,
			SyncState:     data.CatalogSyncStateSynced,
			Editable:      false,
			DriftDetected: false,
			UpdatedAt:     info.ModTime().UTC(),
			LastSyncedAt:  now,
		})
		return nil
	})
	if walkErr != nil {
		if errors.Is(walkErr, os.ErrNotExist) {
			return nil, nil
		}
		if sourceOfTruth == data.CatalogSourceTruthCodex {
			return nil, fmt.Errorf("read codex skills dir: %w", walkErr)
		}
		return nil, fmt.Errorf("read claude skills dir: %w", walkErr)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

func loadClaudeProjectMemories(cwd string, now time.Time) ([]data.MemoryItem, error) {
	memoryDir, err := findClaudeProjectMemoryDir(cwd)
	if err != nil {
		return nil, err
	}
	logx.Info("ws", "load claude project memories: cwd=%q memoryDir=%q", cwd, memoryDir)
	if memoryDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(memoryDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read claude memory dir: %w", err)
	}
	items := make([]data.MemoryItem, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		path := filepath.Join(memoryDir, name)
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read claude memory %s: %w", name, err)
		}
		if hasBinaryContent(content) {
			continue
		}
		meta, body := parseMarkdownFrontMatter(string(content))
		id := strings.TrimSuffix(name, filepath.Ext(name))
		title := firstNonEmptyString(
			strings.TrimSpace(meta["title"]),
			strings.TrimSpace(meta["name"]),
			extractMarkdownTitle(body),
			id,
		)
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat claude memory %s: %w", name, err)
		}
		items = append(items, data.MemoryItem{
			ID:            id,
			Title:         title,
			Content:       strings.TrimSpace(body),
			Source:        "claude-project-memory",
			SourceOfTruth: data.CatalogSourceTruthClaude,
			SyncState:     data.CatalogSyncStateSynced,
			Editable:      false,
			DriftDetected: false,
			UpdatedAt:     info.ModTime().UTC(),
			LastSyncedAt:  now,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	logx.Info("ws", "loaded claude project memories: cwd=%q memoryDir=%q count=%d ids=%v", cwd, memoryDir, len(items), ids)
	return items, nil
}

func loadCodexMemories(now time.Time) ([]data.MemoryItem, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir for codex memory sync: %w", err)
	}
	root := filepath.Join(homeDir, ".codex", "memories")
	items := make([]data.MemoryItem, 0)
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		lower := strings.ToLower(d.Name())
		if !strings.HasSuffix(lower, ".md") && !strings.HasSuffix(lower, ".txt") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read codex memory %s: %w", d.Name(), err)
		}
		if hasBinaryContent(content) {
			return nil
		}
		meta, body := parseMarkdownFrontMatter(string(content))
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = d.Name()
		}
		id := strings.TrimSuffix(filepath.ToSlash(rel), filepath.Ext(rel))
		id = strings.ReplaceAll(id, "/", "-")
		title := firstNonEmptyString(
			strings.TrimSpace(meta["title"]),
			strings.TrimSpace(meta["name"]),
			extractMarkdownTitle(body),
			id,
		)
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat codex memory %s: %w", d.Name(), err)
		}
		items = append(items, data.MemoryItem{
			ID:            id,
			Title:         title,
			Content:       strings.TrimSpace(body),
			Source:        "codex-memory",
			SourceOfTruth: data.CatalogSourceTruthCodex,
			SyncState:     data.CatalogSyncStateSynced,
			Editable:      false,
			DriftDetected: false,
			UpdatedAt:     info.ModTime().UTC(),
			LastSyncedAt:  now,
		})
		return nil
	})
	if walkErr != nil {
		if errors.Is(walkErr, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read codex memory dir: %w", walkErr)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func findClaudeProjectMemoryDir(cwd string) (string, error) {
	startVariants := pathLookupVariants(cwd)
	if len(startVariants) == 0 {
		return "", nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir for memory sync: %w", err)
	}
	stopVariants := pathLookupVariants(homeDir)
	checked := make(map[string]struct{})
	for _, start := range startVariants {
		current := start
		for {
			if sameAnyPath(current, stopVariants) {
				break
			}
			candidate := filepath.Join(homeDir, ".claude", "projects", encodeClaudeProjectPath(current), "memory")
			if _, seen := checked[candidate]; !seen {
				checked[candidate] = struct{}{}
				info, err := os.Stat(candidate)
				if err == nil && info.IsDir() {
					return candidate, nil
				}
			}
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	return "", nil
}

func sameFilePath(left, right string) bool {
	return filepath.Clean(strings.TrimSpace(left)) == filepath.Clean(strings.TrimSpace(right))
}

func sameAnyPath(path string, candidates []string) bool {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "" || cleaned == "." {
		return false
	}
	for _, candidate := range candidates {
		if sameFilePath(cleaned, candidate) {
			return true
		}
	}
	return false
}

func pathLookupVariants(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if absPath, err := filepath.Abs(trimmed); err == nil {
		trimmed = absPath
	}
	addVariant := func(items *[]string, seen map[string]struct{}, value string) {
		cleaned := filepath.Clean(strings.TrimSpace(value))
		if cleaned == "" || cleaned == "." {
			return
		}
		if _, ok := seen[cleaned]; ok {
			return
		}
		seen[cleaned] = struct{}{}
		*items = append(*items, cleaned)
	}
	variants := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	addVariant(&variants, seen, trimmed)
	addPrivatePathAliasVariants(&variants, seen, trimmed)
	if resolved, err := filepath.EvalSymlinks(trimmed); err == nil && strings.TrimSpace(resolved) != "" {
		addVariant(&variants, seen, resolved)
		addPrivatePathAliasVariants(&variants, seen, resolved)
	}
	return variants
}

func addPrivatePathAliasVariants(items *[]string, seen map[string]struct{}, value string) {
	cleaned := filepath.Clean(strings.TrimSpace(value))
	if cleaned == "" || cleaned == "." {
		return
	}
	if strings.HasPrefix(cleaned, "/private/") {
		alias := strings.TrimPrefix(cleaned, "/private")
		if alias == "" {
			alias = string(os.PathSeparator)
		}
		if _, err := os.Stat(alias); err == nil {
			cleanedAlias := filepath.Clean(alias)
			if _, ok := seen[cleanedAlias]; !ok {
				seen[cleanedAlias] = struct{}{}
				*items = append(*items, cleanedAlias)
			}
		}
		return
	}
	alias := filepath.Join(string(os.PathSeparator), "private", strings.TrimPrefix(cleaned, string(os.PathSeparator)))
	if _, err := os.Stat(alias); err == nil {
		cleanedAlias := filepath.Clean(alias)
		if _, ok := seen[cleanedAlias]; !ok {
			seen[cleanedAlias] = struct{}{}
			*items = append(*items, cleanedAlias)
		}
	}
}

func encodeClaudeProjectPath(path string) string {
	normalized := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	if normalized == "" || normalized == "." {
		return ""
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "-")
	encoded := replacer.Replace(normalized)
	if !strings.HasPrefix(encoded, "-") {
		encoded = "-" + encoded
	}
	return encoded
}

func parseMarkdownFrontMatter(content string) (map[string]string, string) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return nil, normalized
	}
	end := strings.Index(normalized[4:], "\n---\n")
	if end < 0 {
		return nil, normalized
	}
	rawFrontMatter := normalized[4 : 4+end]
	body := normalized[4+end+5:]
	meta := make(map[string]string)
	for _, line := range strings.Split(rawFrontMatter, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		meta[key] = value
	}
	return meta, strings.TrimSpace(body)
}

func extractMarkdownTitle(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		if title != "" {
			return title
		}
	}
	return ""
}

func catalogHasDraftSkill(items []data.SkillDefinition) bool {
	for _, item := range items {
		if item.Source == data.SkillSourceLocal &&
			(item.SyncState != data.CatalogSyncStateSynced || item.DriftDetected) {
			return true
		}
	}
	return false
}

func catalogHasDraftMemory(items []data.MemoryItem) bool {
	for _, item := range items {
		if item.Source == "local" &&
			(item.SyncState != data.CatalogSyncStateSynced || item.DriftDetected) {
			return true
		}
	}
	return false
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func defaultAICommandFromEngine(values ...string) string {
	for _, value := range values {
		switch strings.TrimSpace(strings.ToLower(value)) {
		case "codex":
			return "codex"
		case "gemini":
			return "gemini"
		case "claude":
			return "claude"
		}
	}
	return "claude"
}

func applyAICommandPreferences(command, engine, model, reasoningEffort string) string {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		trimmed = defaultAICommandFromEngine(engine)
	}
	normalizedEngine := strings.TrimSpace(strings.ToLower(firstNonEmptyString(engine, commandHead(trimmed))))
	fields := strings.Fields(trimmed)
	lower := strings.ToLower(trimmed)
	switch normalizedEngine {
	case "claude":
		model = strings.TrimSpace(model)
		if model == "" || strings.EqualFold(model, "default") || strings.Contains(lower, " --model ") || strings.Contains(lower, " -m ") {
			return trimmed
		}
		return trimmed + " --model " + shellQuote(model)
	case "codex":
		parts := append([]string(nil), fields...)
		model = strings.TrimSpace(model)
		if model != "" && !commandHasFlag(parts, "-m", "--model") {
			parts = append(parts, "-m", model)
		}
		effort := strings.TrimSpace(strings.ToLower(reasoningEffort))
		if effort != "" && !commandHasCodexReasoningEffort(parts) {
			parts = append(parts, "--config", "model_reasoning_effort="+effort)
		}
		return strings.Join(parts, " ")
	default:
		return trimmed
	}
}

func commandHasFlag(fields []string, flags ...string) bool {
	for _, field := range fields {
		for _, flag := range flags {
			if field == flag || strings.HasPrefix(field, flag+"=") {
				return true
			}
		}
	}
	return false
}

func commandHasCodexReasoningEffort(fields []string) bool {
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), "model_reasoning_effort=") {
			return true
		}
	}
	return false
}

func shellQuote(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "''"
	}
	if !strings.ContainsAny(trimmed, " \t\n'\"\\$`!#&;|<>*?()[]{}") {
		return trimmed
	}
	return "'" + strings.ReplaceAll(trimmed, "'", `'\''`) + "'"
}

func shouldInjectEnabledSkillsForInput(command string, engines ...string) bool {
	if isAISessionCommandLike(command) {
		return true
	}
	for _, engine := range engines {
		switch strings.TrimSpace(strings.ToLower(engine)) {
		case "claude", "codex", "gemini":
			return true
		}
	}
	return false
}

func isAISessionCommandLike(command string) bool {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return false
	}
	head := strings.ToLower(strings.TrimSpace(fields[0]))
	return head == "claude" ||
		strings.HasSuffix(head, "/claude") ||
		strings.HasSuffix(head, `\\claude`) ||
		head == "claude.exe" ||
		head == "codex" ||
		strings.HasSuffix(head, "/codex") ||
		strings.HasSuffix(head, `\\codex`) ||
		head == "codex.exe" ||
		head == "gemini" ||
		strings.HasSuffix(head, "/gemini") ||
		strings.HasSuffix(head, `\\gemini`) ||
		head == "gemini.exe"
}

func emitReviewStateFromProjection(emit func(any), sessionID string, projection data.ProjectionSnapshot) {
	emit(session.ReviewStateEventFromProjection(sessionID, projection))
}

func restoredAgentStateEventFromRecord(record data.SessionRecord, hasActiveRunner bool) *protocol.AgentStateEvent {
	return session.RestoredAgentStateEventFromRecord(record, hasActiveRunner, isExternalNativeActiveRecord(record))
}

func isExternalNativeActiveRecord(record data.SessionRecord) bool {
	if !codexsync.IsMirrorSessionID(record.Summary.ID) && !claudesync.IsMirrorSessionID(record.Summary.ID) {
		if !record.Summary.External {
			return false
		}
	}
	projection := session.NormalizeProjectionSnapshot(record.Projection)
	state := strings.TrimSpace(string(projection.Controller.State))
	lifecycle := strings.TrimSpace(firstNonEmptyString(
		projection.Controller.ClaudeLifecycle,
		projection.Controller.ActiveMeta.ClaudeLifecycle,
		projection.Runtime.ClaudeLifecycle,
		record.Summary.Runtime.ClaudeLifecycle,
	))
	return session.IsBusyRuntimeState(state) || lifecycle == "active" || lifecycle == "starting"
}

func prepareSessionEventForResume(sessionRuntime *runtimeSession, sessionID string, event any) any {
	event = session.MarkSystemBootstrapEvent(event)
	if sessionRuntime == nil || strings.TrimSpace(sessionID) == "" {
		return event
	}
	event = sessionRuntime.appendPending(event)
	if sessionRuntime.listenerCount() == 0 {
		if notice := detachedResumeNoticeEvent(sessionID, event); notice != nil {
			sessionRuntime.appendPending(*notice)
		}
	}
	return event
}

func detachedResumeNoticeEvent(sessionID string, event any) *protocol.SessionResumeNoticeEvent {
	switch e := event.(type) {
	case protocol.LogEvent:
		if !session.IsVisibleAssistantReplyLog(e) {
			return nil
		}
		notice := protocol.NewSessionResumeNoticeEvent(sessionID, "assistant_reply", "info", "MobileVC", strings.TrimSpace(e.Message))
		notice.RuntimeMeta = protocol.MergeRuntimeMeta(notice.RuntimeMeta, e.RuntimeMeta)
		return &notice
	case protocol.ErrorEvent:
		if strings.TrimSpace(e.Message) == "" {
			return nil
		}
		if e.Code == "ws_closed" || e.Code == "ws_stream_error" {
			return nil
		}
		notice := protocol.NewSessionResumeNoticeEvent(sessionID, "error", "error", "MobileVC", strings.TrimSpace(e.Message))
		notice.RuntimeMeta = protocol.MergeRuntimeMeta(notice.RuntimeMeta, e.RuntimeMeta)
		return &notice
	default:
		return nil
	}
}

func looksLikeAssistantResumeNotice(event protocol.LogEvent) bool {
	return session.IsVisibleAssistantReplyLog(event)
}

func parseMode(raw string) (engine.Mode, error) {
	return session.ParseMode(raw)
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

func normalizeSessionCWD(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	absPath, err := filepath.Abs(trimmed)
	if err == nil {
		trimmed = absPath
	}
	if resolved, err := filepath.EvalSymlinks(trimmed); err == nil && strings.TrimSpace(resolved) != "" {
		trimmed = resolved
	}
	return filepath.Clean(trimmed)
}

func filterStoreSessionsByCWD(items []data.SessionSummary, filterCWD string) []data.SessionSummary {
	normalized := normalizeSessionCWD(filterCWD)
	if normalized == "" {
		return items
	}
	filtered := make([]data.SessionSummary, 0, len(items))
	for _, item := range items {
		cwd := normalizeSessionCWD(item.Runtime.CWD)
		if cwd == "" || cwd == normalized {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func isExternalCodexSummary(item data.SessionSummary) bool {
	return item.External || strings.EqualFold(strings.TrimSpace(item.Source), "codex-native")
}

func summaryCodexThreadID(item data.SessionSummary) string {
	if resumeID := strings.TrimSpace(item.Runtime.ResumeSessionID); resumeID != "" {
		return resumeID
	}
	if isExternalCodexSummary(item) {
		return codexsync.ThreadIDFromMirror(item.ID)
	}
	return ""
}

func resolveTrackedCodexThreadID(ctx context.Context, sessionStore data.Store, item data.SessionSummary) string {
	if threadID := summaryCodexThreadID(item); threadID != "" {
		return threadID
	}
	if sessionStore == nil || strings.TrimSpace(item.ID) == "" {
		return ""
	}
	record, err := sessionStore.GetSession(ctx, item.ID)
	if err != nil {
		return ""
	}
	if resumeID := strings.TrimSpace(record.Projection.Runtime.ResumeSessionID); resumeID != "" {
		return resumeID
	}
	if resumeID := strings.TrimSpace(record.Projection.Controller.ResumeSession); resumeID != "" {
		return resumeID
	}
	if codexsync.IsMirrorSessionID(record.Projection.Controller.SessionID) {
		return codexsync.ThreadIDFromMirror(record.Projection.Controller.SessionID)
	}
	return ""
}

func trackedMobileVCCodexThreads(ctx context.Context, sessionStore data.Store, items []data.SessionSummary) map[string]struct{} {
	tracked := make(map[string]struct{}, len(items))
	for _, item := range items {
		if isExternalCodexSummary(item) || !isCodexRuntime(item.Runtime) {
			continue
		}
		if threadID := resolveTrackedCodexThreadID(ctx, sessionStore, item); threadID != "" {
			tracked[threadID] = struct{}{}
		}
	}
	return tracked
}

func preferCodexThreadSummary(current, candidate data.SessionSummary) data.SessionSummary {
	currentExternal := isExternalCodexSummary(current)
	candidateExternal := isExternalCodexSummary(candidate)
	if currentExternal != candidateExternal {
		if currentExternal {
			return candidate
		}
		return current
	}
	if candidate.UpdatedAt.After(current.UpdatedAt) {
		return candidate
	}
	if candidate.UpdatedAt.Equal(current.UpdatedAt) && candidate.CreatedAt.After(current.CreatedAt) {
		return candidate
	}
	return current
}

func dedupeCodexThreadSummaries(items []data.SessionSummary) []data.SessionSummary {
	if len(items) <= 1 {
		return items
	}
	selected := make(map[string]data.SessionSummary, len(items))
	order := make([]string, 0, len(items))
	for _, item := range items {
		key := item.ID
		if threadID := summaryCodexThreadID(item); threadID != "" && isCodexRuntime(item.Runtime) {
			key = "codex-thread:" + threadID
		}
		if existing, ok := selected[key]; ok {
			selected[key] = preferCodexThreadSummary(existing, item)
			continue
		}
		selected[key] = item
		order = append(order, key)
	}
	deduped := make([]data.SessionSummary, 0, len(selected))
	for _, key := range order {
		item, ok := selected[key]
		if ok {
			deduped = append(deduped, item)
		}
	}
	sort.Slice(deduped, func(i, j int) bool {
		return deduped[i].UpdatedAt.After(deduped[j].UpdatedAt)
	})
	return deduped
}

func isExternalClaudeSummary(item data.SessionSummary) bool {
	if item.External && strings.EqualFold(strings.TrimSpace(item.Source), "claude-native") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(item.Source), "claude-native") {
		return true
	}
	if claudesync.IsMirrorSessionID(item.ID) {
		return true
	}
	return false
}

func summaryClaudeSessionID(item data.SessionSummary) string {
	if claudesync.IsMirrorSessionID(item.ID) {
		return claudesync.SessionIDFromMirror(item.ID)
	}
	if isClaudeRuntime(item.Runtime) {
		if resumeID := strings.TrimSpace(item.Runtime.ResumeSessionID); resumeID != "" {
			return resumeID
		}
	}
	return ""
}

func resolveTrackedClaudeSessionID(ctx context.Context, sessionStore data.Store, item data.SessionSummary) string {
	if sid := strings.TrimSpace(item.ClaudeSessionUUID); sid != "" {
		return sid
	}
	if sid := summaryClaudeSessionID(item); sid != "" {
		return sid
	}
	if sessionStore == nil || strings.TrimSpace(item.ID) == "" {
		return ""
	}
	record, err := sessionStore.GetSession(ctx, item.ID)
	if err != nil {
		return ""
	}
	if resumeID := strings.TrimSpace(record.Projection.Runtime.ResumeSessionID); resumeID != "" && isClaudeRuntime(record.Projection.Runtime) {
		return resumeID
	}
	if isClaudeRuntime(record.Projection.Runtime) {
		if resumeID := strings.TrimSpace(record.Projection.Controller.ResumeSession); resumeID != "" {
			return resumeID
		}
	}
	if claudesync.IsMirrorSessionID(record.Projection.Controller.SessionID) {
		return claudesync.SessionIDFromMirror(record.Projection.Controller.SessionID)
	}
	return ""
}

func trackedMobileVCClaudeSessions(ctx context.Context, sessionStore data.Store, items []data.SessionSummary) map[string]struct{} {
	tracked := make(map[string]struct{}, len(items))
	for _, item := range items {
		if isExternalClaudeSummary(item) {
			continue
		}
		if strings.TrimSpace(item.ClaudeSessionUUID) == "" && !isClaudeRuntime(item.Runtime) {
			continue
		}
		// Track ClaudeSessionUUID (for sessions created by MobileVC).
		if sid := strings.TrimSpace(item.ClaudeSessionUUID); sid != "" {
			tracked[sid] = struct{}{}
		}
		// Also track resumeSessionId (may differ from ClaudeSessionUUID
		// if the user ran "claude --session-id <other>").
		if sid := summaryClaudeSessionID(item); sid != "" {
			tracked[sid] = struct{}{}
		}
		if sid := resolveTrackedClaudeSessionID(ctx, sessionStore, item); sid != "" {
			tracked[sid] = struct{}{}
		}
	}
	return tracked
}

func mergeSessionSummaries(ctx context.Context, sessionStore data.Store, items []data.SessionSummary, filterCWD string) ([]data.SessionSummary, error) {
	filteredStoreItems := filterStoreSessionsByCWD(items, filterCWD)
	trackedThreads := trackedMobileVCCodexThreads(ctx, sessionStore, filteredStoreItems)
	trackedClaudeSessions := trackedMobileVCClaudeSessions(ctx, sessionStore, filteredStoreItems)
	if normalizeSessionCWD(filterCWD) == "" {
		if len(trackedThreads) == 0 && len(trackedClaudeSessions) == 0 {
			return dedupeCodexThreadSummaries(filteredStoreItems), nil
		}
		filtered := make([]data.SessionSummary, 0, len(filteredStoreItems))
		for _, item := range filteredStoreItems {
			if isExternalCodexSummary(item) {
				if _, ok := trackedThreads[summaryCodexThreadID(item)]; ok {
					continue
				}
			}
			if isExternalClaudeSummary(item) {
				if _, ok := trackedClaudeSessions[summaryClaudeSessionID(item)]; ok {
					continue
				}
			}
			filtered = append(filtered, item)
		}
		return dedupeCodexThreadSummaries(filtered), nil
	}
	nativeThreads, err := codexsync.ListNativeThreads(ctx, filterCWD)
	if err != nil {
		nativeThreads = nil
	}
	nativeClaude, err := claudesync.ListNativeSessions(ctx, filterCWD)
	if err != nil {
		logx.Warn("ws", "list claude native sessions failed: cwd=%q err=%v", filterCWD, err)
		nativeClaude = nil
	}
	merged := make([]data.SessionSummary, 0, len(filteredStoreItems)+len(nativeThreads)+len(nativeClaude))
	seen := make(map[string]struct{}, len(filteredStoreItems)+len(nativeThreads)+len(nativeClaude))
	for _, item := range filteredStoreItems {
		if isExternalCodexSummary(item) {
			if _, ok := trackedThreads[summaryCodexThreadID(item)]; ok {
				continue
			}
		}
		if isExternalClaudeSummary(item) {
			if _, ok := trackedClaudeSessions[summaryClaudeSessionID(item)]; ok {
				continue
			}
		}
		if strings.TrimSpace(item.Source) == "" {
			item.Source = item.Runtime.Source
		}
		if strings.TrimSpace(item.Source) == "" {
			item.Source = "mobilevc"
		}
		merged = append(merged, item)
		seen[item.ID] = struct{}{}
	}
	for _, thread := range nativeThreads {
		if _, ok := trackedThreads[strings.TrimSpace(thread.ThreadID)]; ok {
			continue
		}
		record := codexsync.MirrorRecord(thread)
		if _, ok := seen[record.Summary.ID]; ok {
			continue
		}
		if sessionStore != nil {
			if stored, getErr := sessionStore.GetSession(ctx, record.Summary.ID); getErr == nil {
				record = stored
			}
		}
		merged = append(merged, record.Summary)
		seen[record.Summary.ID] = struct{}{}
	}
	for _, native := range nativeClaude {
		if _, ok := trackedClaudeSessions[strings.TrimSpace(native.SessionID)]; ok {
			continue
		}
		record := claudesync.MirrorRecord(native)
		if _, ok := seen[record.Summary.ID]; ok {
			continue
		}
		if sessionStore != nil {
			if stored, getErr := sessionStore.GetSession(ctx, record.Summary.ID); getErr == nil {
				record = stored
			}
		}
		merged = append(merged, record.Summary)
		seen[record.Summary.ID] = struct{}{}
	}
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].UpdatedAt.After(merged[j].UpdatedAt)
	})
	return dedupeCodexThreadSummaries(merged), nil
}

func loadSessionRecord(ctx context.Context, sessionStore data.Store, sessionID string) (data.SessionRecord, error) {
	if sessionStore == nil {
		return data.SessionRecord{}, fmt.Errorf("session store unavailable")
	}
	if claudesync.IsMirrorSessionID(sessionID) {
		existing, existingErr := sessionStore.GetSession(ctx, sessionID)
		native, nativeErr := claudesync.FindNativeSession(ctx, sessionID)
		if nativeErr == nil {
			record := claudesync.MirrorRecord(native)
			if existingErr == nil && len(existing.Projection.LogEntries) > len(record.Projection.LogEntries) {
				record.Projection.LogEntries = existing.Projection.LogEntries
			}
			if _, upsertErr := sessionStore.UpsertSession(ctx, record); upsertErr != nil {
				logx.Warn("ws", "upsert claude mirror session failed: sessionID=%s err=%v", sessionID, upsertErr)
			}
			return sessionStore.GetSession(ctx, sessionID)
		}
		if existingErr == nil && existing.Summary.ID != "" {
			logx.Warn("ws", "claude sync failed but using cached record: sessionID=%s nativeErr=%v", sessionID, nativeErr)
			return existing, nil
		}
		return data.SessionRecord{}, fmt.Errorf("claude session not found and no valid cache: %w", nativeErr)
	}
	if !codexsync.IsMirrorSessionID(sessionID) {
		record, err := sessionStore.GetSession(ctx, sessionID)
		if err != nil {
			return record, err
		}
		return augmentLocalClaudeRecordFromNative(ctx, record), nil
	}
	existing, existingErr := sessionStore.GetSession(ctx, sessionID)
	thread, nativeErr := codexsync.FindNativeThread(ctx, sessionID)
	if nativeErr == nil {
		record := codexsync.MirrorRecord(thread)
		if existingErr == nil {
			record = mergeCodexMirrorRecord(record, existing)
		}
		if _, upsertErr := sessionStore.UpsertSession(ctx, record); upsertErr != nil {
			logx.Warn("ws", "upsert codex mirror session failed: sessionID=%s err=%v", sessionID, upsertErr)
		}
		return sessionStore.GetSession(ctx, sessionID)
	}
	if existingErr == nil && existing.Summary.ID != "" {
		logx.Warn("ws", "codex sync failed but using cached record: sessionID=%s nativeErr=%v", sessionID, nativeErr)
		return existing, nil
	}
	return data.SessionRecord{}, fmt.Errorf("codex session not found and no valid cache: %w", nativeErr)
}

func preferAugmentedLogEntries(augmented, current []data.SnapshotLogEntry) bool {
	if len(augmented) > len(current) {
		return true
	}
	aMd, cMd := 0, 0
	for _, e := range augmented {
		if e.Kind == "markdown" {
			aMd++
		}
	}
	for _, e := range current {
		if e.Kind == "markdown" {
			cMd++
		}
	}
	return aMd > cMd
}

func augmentLocalClaudeRecordFromNative(ctx context.Context, record data.SessionRecord) data.SessionRecord {
	if !isClaudeRuntime(record.Projection.Runtime) {
		return record
	}
	resumeID := strings.TrimSpace(record.Projection.Runtime.ResumeSessionID)
	if resumeID == "" {
		resumeID = strings.TrimSpace(record.Projection.Controller.ResumeSession)
	}
	if resumeID == "" {
		return record
	}
	hasMarkdown := false
	hasUser := false
	for _, entry := range record.Projection.LogEntries {
		switch entry.Kind {
		case "markdown":
			hasMarkdown = true
		case "user":
			hasUser = true
		}
	}
	if hasMarkdown {
		return record
	}
	if !hasUser {
		return record
	}
	native, err := claudesync.FindNativeSession(ctx, resumeID)
	if err != nil {
		return record
	}
	if len(native.LogEntries) == 0 {
		return record
	}
	logx.Info("ws", "augment local claude record from native jsonl: sessionID=%s resumeID=%s nativeEntries=%d", record.Summary.ID, resumeID, len(native.LogEntries))
	augmented := record
	augmented.Projection.LogEntries = append([]data.SnapshotLogEntry(nil), native.LogEntries...)
	if augmented.Summary.EntryCount < len(augmented.Projection.LogEntries) {
		augmented.Summary.EntryCount = len(augmented.Projection.LogEntries)
	}
	if trimmed := strings.TrimSpace(native.LastAssistantText); trimmed != "" && strings.TrimSpace(augmented.Summary.LastPreview) == "" {
		augmented.Summary.LastPreview = trimmed
	}
	return augmented
}

func mergeCodexMirrorRecord(fresh data.SessionRecord, existing data.SessionRecord) data.SessionRecord {
	fresh.Projection = mergeCodexMirrorProjection(fresh.Projection, existing.Projection)
	fresh.Summary.CreatedAt = laterNonZeroTime(fresh.Summary.CreatedAt, existing.Summary.CreatedAt)
	fresh.Summary.UpdatedAt = laterNonZeroTime(fresh.Summary.UpdatedAt, existing.Summary.UpdatedAt)
	fresh.Summary.Runtime = session.MergeStoreSessionRuntime(data.SessionRuntime{}, fresh.Projection.Runtime)
	if fresh.Summary.Runtime.Source == "" {
		fresh.Summary.Runtime.Source = "codex-native"
	}
	fresh.Summary.Source = firstNonEmptyString(fresh.Summary.Source, existing.Summary.Source, "codex-native")
	fresh.Summary.External = true
	return fresh
}

func preserveCodexMirrorLocalSettings(fresh data.ProjectionSnapshot, existing data.ProjectionSnapshot) data.ProjectionSnapshot {
	fresh = session.NormalizeProjectionSnapshot(fresh)
	existing = session.NormalizeProjectionSnapshot(existing)
	fresh.SessionContext = existing.SessionContext
	fresh.SessionContextSet = existing.SessionContext.Configured
	fresh.PermissionRulesEnabled = existing.PermissionRulesEnabled
	if len(existing.PermissionRules) > 0 {
		fresh.PermissionRules = existing.PermissionRules
	}
	fresh.SkillCatalogMeta = existing.SkillCatalogMeta
	fresh.MemoryCatalogMeta = existing.MemoryCatalogMeta
	return session.NormalizeProjectionSnapshot(fresh)
}

func mergeCodexMirrorProjection(fresh data.ProjectionSnapshot, existing data.ProjectionSnapshot) data.ProjectionSnapshot {
	fresh = session.NormalizeProjectionSnapshot(fresh)
	existing = session.NormalizeProjectionSnapshot(existing)

	fresh.LogEntries = mergeSnapshotLogEntries(fresh.LogEntries, existing.LogEntries)
	fresh.RawTerminalByStream = mergeRawTerminalByStream(fresh.RawTerminalByStream, existing.RawTerminalByStream)
	fresh.TerminalExecutions = mergeTerminalExecutions(fresh.TerminalExecutions, existing.TerminalExecutions)
	fresh.Controller = session.MergeControllerSnapshot(fresh.Controller, existing.Controller)
	fresh.Runtime = session.MergeStoreSessionRuntime(fresh.Runtime, existing.Runtime)
	if fresh.Runtime.Source == "" {
		fresh.Runtime.Source = "codex-native"
	}

	if len(existing.Diffs) > 0 {
		fresh.Diffs = existing.Diffs
	}
	if existing.CurrentDiff != nil {
		fresh.CurrentDiff = existing.CurrentDiff
	}
	if len(existing.ReviewGroups) > 0 {
		fresh.ReviewGroups = existing.ReviewGroups
	}
	if existing.ActiveReviewGroup != nil {
		fresh.ActiveReviewGroup = existing.ActiveReviewGroup
	}
	if existing.CurrentStep != nil {
		fresh.CurrentStep = existing.CurrentStep
	}
	if existing.LatestError != nil {
		fresh.LatestError = existing.LatestError
	}
	fresh.SessionContext = existing.SessionContext
	fresh.SessionContextSet = existing.SessionContext.Configured
	fresh.PermissionRulesEnabled = existing.PermissionRulesEnabled
	if len(existing.PermissionRules) > 0 {
		fresh.PermissionRules = existing.PermissionRules
	}
	fresh.SkillCatalogMeta = existing.SkillCatalogMeta
	fresh.MemoryCatalogMeta = existing.MemoryCatalogMeta

	return session.NormalizeProjectionSnapshot(fresh)
}

func mergeSnapshotLogEntries(base []data.SnapshotLogEntry, overlay []data.SnapshotLogEntry) []data.SnapshotLogEntry {
	if len(base) == 0 {
		return append([]data.SnapshotLogEntry(nil), overlay...)
	}
	merged := append([]data.SnapshotLogEntry(nil), base...)
	seen := make(map[string]struct{}, len(base)+len(overlay))
	for _, entry := range base {
		seen[snapshotLogEntryKey(entry)] = struct{}{}
	}
	for _, entry := range overlay {
		key := snapshotLogEntryKey(entry)
		if _, ok := seen[key]; ok {
			continue
		}
		merged = append(merged, entry)
		seen[key] = struct{}{}
	}
	return merged
}

func snapshotLogEntryKey(entry data.SnapshotLogEntry) string {
	contextKey := ""
	if entry.Context != nil {
		contextKey = strings.Join([]string{
			entry.Context.ID,
			entry.Context.Type,
			entry.Context.Message,
			entry.Context.Path,
			entry.Context.Title,
			entry.Context.Timestamp,
		}, "\x1f")
	}
	exitCode := ""
	if entry.ExitCode != nil {
		exitCode = fmt.Sprintf("%d", *entry.ExitCode)
	}
	return strings.Join([]string{
		entry.Kind,
		entry.Message,
		entry.Label,
		entry.Timestamp,
		entry.Stream,
		entry.Text,
		entry.ExecutionID,
		entry.Phase,
		exitCode,
		contextKey,
	}, "\x1f")
}

func mergeRawTerminalByStream(base map[string]string, overlay map[string]string) map[string]string {
	merged := map[string]string{"stdout": "", "stderr": ""}
	for _, stream := range []string{"stdout", "stderr"} {
		merged[stream] = firstNonEmptyString(overlay[stream], base[stream])
	}
	return merged
}

func mergeTerminalExecutions(base []data.TerminalExecution, overlay []data.TerminalExecution) []data.TerminalExecution {
	if len(base) == 0 {
		return append([]data.TerminalExecution(nil), overlay...)
	}
	merged := append([]data.TerminalExecution(nil), base...)
	indexByID := make(map[string]int, len(merged))
	for i, item := range merged {
		if strings.TrimSpace(item.ExecutionID) != "" {
			indexByID[item.ExecutionID] = i
		}
	}
	for _, item := range overlay {
		if id := strings.TrimSpace(item.ExecutionID); id != "" {
			if index, ok := indexByID[id]; ok {
				current := merged[index]
				merged[index] = data.TerminalExecution{
					ExecutionID: id,
					Command:     firstNonEmptyString(item.Command, current.Command),
					CWD:         firstNonEmptyString(item.CWD, current.CWD),
					StartedAt:   firstNonEmptyString(item.StartedAt, current.StartedAt),
					FinishedAt:  firstNonEmptyString(item.FinishedAt, current.FinishedAt),
					ExitCode:    firstNonNilInt(item.ExitCode, current.ExitCode),
					Stdout:      firstNonEmptyString(item.Stdout, current.Stdout),
					Stderr:      firstNonEmptyString(item.Stderr, current.Stderr),
				}
				continue
			}
			indexByID[id] = len(merged)
		}
		merged = append(merged, item)
	}
	return merged
}

func shouldTreatInputAsAICommand(data string) bool {
	fields := strings.Fields(strings.TrimSpace(data))
	if len(fields) == 0 {
		return false
	}
	if len(fields) > 1 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "codex", "claude", "gemini":
		return true
	default:
		return false
	}
}

func firstNonNilInt(values ...*int) *int {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func laterNonZeroTime(values ...time.Time) time.Time {
	var latest time.Time
	for _, value := range values {
		if value.IsZero() {
			continue
		}
		if latest.IsZero() || value.After(latest) {
			latest = value
		}
	}
	return latest
}

// syncSessionEntriesToClaudeJSONL writes new user/assistant LogEntries to the
// Claude CLI JSONL file for this session, if the session has a ClaudeSessionUUID.
func syncSessionEntriesToClaudeJSONL(sessionStore data.Store, sessionID string, snapshot data.ProjectionSnapshot) {
	if sessionStore == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	record, err := sessionStore.GetSession(ctx, sessionID)
	if err != nil {
		return
	}
	csuuid := strings.TrimSpace(record.Summary.ClaudeSessionUUID)
	if csuuid == "" {
		return
	}
	cwd := strings.TrimSpace(record.Summary.Runtime.CWD)
	if cwd == "" {
		cwd = strings.TrimSpace(record.Projection.Runtime.CWD)
	}
	if cwd == "" {
		return
	}
	startIndex := record.Summary.JSONLSyncEntryCount
	events, newCount := claudesync.ExtractJSONLEvents(snapshot.LogEntries, startIndex)
	if len(events) == 0 {
		return
	}
	if err := claudesync.WriteSessionToJSONL(cwd, csuuid, events); err != nil {
		logx.Error("ws", "sync claude jsonl failed: sessionID=%s err=%v", sessionID, err)
		return
	}
	// Update sync count so we don't re-write the same entries.
	updated := record
	updated.Summary.JSONLSyncEntryCount = newCount
	updated.Projection = snapshot
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	if _, err := sessionStore.UpsertSession(ctx2, updated); err != nil {
		logx.Error("ws", "update jsonl sync count failed: sessionID=%s err=%v", sessionID, err)
	}
}

// lastUserPromptFromEntries returns the text of the most recent user entry.
func lastUserPromptFromEntries(entries []data.SnapshotLogEntry) string {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Kind == "user" {
			text := strings.TrimSpace(entries[i].Message)
			if text == "" {
				text = strings.TrimSpace(entries[i].Text)
			}
			if text != "" {
				return text
			}
		}
	}
	return ""
}

// mergeClaudeJSONLToRecord checks if the session has a linked Claude CLI JSONL
// file. If the JSONL has new entries (added by Claude CLI continuing the
// conversation), they are merged into the record's LogEntries and persisted.
func mergeClaudeJSONLToRecord(ctx context.Context, sessionStore data.Store, record data.SessionRecord, runtimeSvc *session.Service) data.SessionRecord {
	csuuid := strings.TrimSpace(record.Summary.ClaudeSessionUUID)
	if csuuid == "" {
		return record
	}
	cwd := strings.TrimSpace(record.Summary.Runtime.CWD)
	if cwd == "" {
		cwd = strings.TrimSpace(record.Projection.Runtime.CWD)
	}
	if cwd == "" {
		return record
	}
	newEntries, newCount, err := claudesync.MergeJSONLToSession(cwd, csuuid, record.Projection.LogEntries)
	if err != nil || len(newEntries) == 0 {
		// If no new entries but session was already external, re-evaluate
		// lifecycle based on recency of existing log entries.
		if record.Summary.External {
			latest := latestLogEntryTime(record.Projection.LogEntries)
			if !latest.IsZero() && time.Since(latest) < 60*time.Second {
				record.Projection.Runtime.ClaudeLifecycle = "active"
			} else {
				record.Projection.Runtime.ClaudeLifecycle = "resumable"
			}
		}
		return record
	}
	record.Projection.LogEntries = append(record.Projection.LogEntries, newEntries...)
	record.Summary.JSONLSyncEntryCount = newCount
	record.Summary.UpdatedAt = time.Now().UTC()

	// Only upgrade ownership when the session is NOT actively managed
	// by MobileVC's own runtime. If MobileVC launched the Claude CLI,
	// the JSONL entries are ours and ownership stays "mobilevc".
	runtimeManaged := false
	if runtimeSvc != nil {
		snapshot := runtimeSvc.RuntimeSnapshot()
		runtimeManaged = snapshot.Running && strings.TrimSpace(snapshot.ActiveSession) == strings.TrimSpace(record.Summary.ID)
	}
	// 守卫：会话之前已被明确标记为 mobilevc 所有时，即使 runtimeSvc 暂时不存活
	// （例如 server 进程刚重启、runner 尚未恢复），也不要因为 JSONL 出现新条目就升级
	// 为 claude-native。这些"新条目"通常是 mobilevc 自己之前的 runner 写下后未及时
	// 同步到 record 里的 catch-up，并非桌面端 Claude CLI 真的接管。
	previousOwnership := strings.ToLower(strings.TrimSpace(record.Summary.Ownership))
	mobilevcOwned := previousOwnership == "mobilevc"
	if !runtimeManaged && !mobilevcOwned {
		record.Summary.External = true
		record.Summary.Ownership = "claude-native"
		if record.Summary.Source == "" || record.Summary.Source == "mobilevc" {
			record.Summary.Source = "claude-native"
		}
		if record.Summary.Runtime.Source == "" || record.Summary.Runtime.Source == "mobilevc" {
			record.Summary.Runtime.Source = "claude-native"
		}
	} else if runtimeManaged {
		// Reset stale ownership when MobileVC runtime is actively managing
		// this session — undo any External/ownership from previous sessions.
		record.Summary.External = false
		record.Summary.Ownership = "mobilevc"
		if record.Summary.Runtime.Source == "" || record.Summary.Runtime.Source == "claude-native" {
			record.Summary.Runtime.Source = "mobilevc"
		}
	}

	// Set lifecycle based on recency of the latest entry.
	latest := latestLogEntryTime(record.Projection.LogEntries)
	if !latest.IsZero() && time.Since(latest) < 60*time.Second {
		record.Projection.Runtime.ClaudeLifecycle = "active"
	} else {
		record.Projection.Runtime.ClaudeLifecycle = "resumable"
	}

	saveCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := sessionStore.UpsertSession(saveCtx, record); err != nil {
		logx.Error("ws", "merge claude jsonl to session failed: sessionID=%s err=%v", record.Summary.ID, err)
	}
	return record
}

// latestLogEntryTime returns the most recent timestamp among the given log entries.
func latestLogEntryTime(entries []data.SnapshotLogEntry) time.Time {
	var latest time.Time
	for _, e := range entries {
		ts, err := time.Parse(time.RFC3339, e.Timestamp)
		if err != nil {
			continue
		}
		if ts.After(latest) {
			latest = ts
		}
	}
	return latest
}
