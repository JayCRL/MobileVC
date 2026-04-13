package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"mobilevc/internal/adapter"
	"mobilevc/internal/logx"
	"mobilevc/internal/protocol"
)

const (
	codexPromptApprove = "approve"
	codexPromptDeny    = "deny"
)

var (
	codexDiffPathPattern         = regexp.MustCompile(`^diff --git a/(.+) b/(.+)$`)
	codexStructuredStderrPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T[^\s]+\s+(TRACE|DEBUG|INFO|WARN|ERROR)\s+[A-Za-z0-9_.:-]+:`)
)

type codexRPCMessage struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *codexRPCError  `json:"error,omitempty"`
}

type codexRPCError struct {
	Code    int             `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type codexRPCResponse struct {
	message codexRPCMessage
	err     error
}

type codexAppSession struct {
	runner    *PtyRunner
	sessionID string
	req       ExecRequest
	cwd       string
	sink      EventSink

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr io.ReadCloser

	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  int64
	pending map[string]chan codexRPCResponse
	readErr error

	threadID        string
	activeTurnID    string
	lastDiff        string
	assistantBuffer strings.Builder
	pendingApproval *codexPendingApproval
}

type codexPendingApproval struct {
	id          json.RawMessage
	method      string
	permissions json.RawMessage
}

type codexThreadEnvelope struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
}

type codexTurnStartResponse struct {
	Turn struct {
		ID string `json:"id"`
	} `json:"turn"`
}

type codexTurnSteerResponse struct {
	TurnID string `json:"turnId"`
}

type codexTurnNotification struct {
	ThreadID string `json:"threadId"`
	Turn     struct {
		ID    string `json:"id"`
		Error *struct {
			Message           string `json:"message"`
			AdditionalDetails string `json:"additionalDetails"`
		} `json:"error"`
		Status string `json:"status"`
	} `json:"turn"`
}

type codexThreadStatusNotification struct {
	ThreadID string `json:"threadId"`
	Status   struct {
		Type string `json:"type"`
	} `json:"status"`
}

type codexAgentDeltaNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
}

type codexTurnDiffNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	Diff     string `json:"diff"`
}

type codexErrorNotification struct {
	ThreadID  string `json:"threadId"`
	TurnID    string `json:"turnId"`
	WillRetry bool   `json:"willRetry"`
	Error     struct {
		Message           string `json:"message"`
		AdditionalDetails string `json:"additionalDetails"`
	} `json:"error"`
}

type codexItemNotification struct {
	ThreadID string         `json:"threadId"`
	TurnID   string         `json:"turnId"`
	Item     map[string]any `json:"item"`
}

type codexCommandApprovalRequest struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Reason   string `json:"reason"`
	Command  string `json:"command"`
	CWD      string `json:"cwd"`
}

type codexFileApprovalRequest struct {
	ThreadID  string `json:"threadId"`
	TurnID    string `json:"turnId"`
	ItemID    string `json:"itemId"`
	Reason    string `json:"reason"`
	GrantRoot string `json:"grantRoot"`
}

type codexPermissionsApprovalRequest struct {
	ThreadID    string          `json:"threadId"`
	TurnID      string          `json:"turnId"`
	ItemID      string          `json:"itemId"`
	Reason      string          `json:"reason"`
	Permissions json.RawMessage `json:"permissions"`
}

type codexAppWriter struct {
	session *codexAppSession
}

func (w *codexAppWriter) Write(data []byte) (int, error) {
	if w.session == nil {
		return 0, errors.New("codex app session is unavailable")
	}
	if err := w.session.SendUserInput(context.Background(), data); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (w *codexAppWriter) Close() error {
	return nil
}

func newCodexAppSession(processCtx context.Context, rpcCtx context.Context, runner *PtyRunner, req ExecRequest, cwd string, sink EventSink, resumeSessionID string) (*codexAppSession, error) {
	cmd := newCodexAppServerCommand(processCtx, req.Command)
	cmd.Dir = cwd

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create codex app-server stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("create codex app-server stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("create codex app-server stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}

	app := &codexAppSession{
		runner:    runner,
		sessionID: req.SessionID,
		req:       req,
		cwd:       cwd,
		sink:      sink,
		cmd:       cmd,
		stdin:     stdin,
		stderr:    stderr,
		pending:   make(map[string]chan codexRPCResponse),
	}

	go app.readLoop(processCtx, stdout)
	go app.readStderr(processCtx)

	if err := app.initialize(rpcCtx); err != nil {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		return nil, err
	}
	if err := app.startOrResumeThread(rpcCtx, resumeSessionID); err != nil {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		return nil, err
	}
	return app, nil
}

func (s *codexAppSession) initialize(ctx context.Context) error {
	params := map[string]any{
		"clientInfo": map[string]any{
			"name":    "MobileVC",
			"version": "dev",
		},
		"capabilities": nil,
	}
	var result map[string]any
	return s.call(ctx, "initialize", params, &result)
}

func (s *codexAppSession) startOrResumeThread(ctx context.Context, resumeSessionID string) error {
	var (
		method string
		params map[string]any
		resp   codexThreadEnvelope
	)
	resumeSessionID = strings.TrimSpace(resumeSessionID)
	if resumeSessionID != "" {
		method = "thread/resume"
		params = map[string]any{"threadId": resumeSessionID}
	} else {
		method = "thread/start"
		params = map[string]any{
			"cwd":                   s.cwd,
			"approvalPolicy":        codexApprovalPolicy(s.runner.currentPermissionMode()),
			"approvalsReviewer":     "user",
			"sandbox":               "workspace-write",
			"serviceName":           "MobileVC",
			"experimentalRawEvents": false,
		}
		if model := extractCodexModelFlag(s.req.Command); model != "" {
			params["model"] = model
		}
	}
	if err := s.call(ctx, method, params, &resp); err != nil {
		return err
	}
	if strings.TrimSpace(resp.Thread.ID) == "" {
		return errors.New("codex app-server returned an empty thread id")
	}
	s.setThreadID(resp.Thread.ID)
	return nil
}

func (s *codexAppSession) SendUserInput(ctx context.Context, data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil
	}

	threadID, activeTurnID := s.threadAndTurn()
	if threadID == "" {
		return errors.New("codex thread is not ready")
	}

	input := []map[string]any{{
		"type":          "text",
		"text":          text,
		"text_elements": []any{},
	}}

	if activeTurnID != "" {
		var resp codexTurnSteerResponse
		if err := s.call(ctx, "turn/steer", map[string]any{
			"threadId":       threadID,
			"expectedTurnId": activeTurnID,
			"input":          input,
		}, &resp); err != nil {
			return err
		}
		if strings.TrimSpace(resp.TurnID) != "" {
			s.setActiveTurnID(resp.TurnID)
		}
		return nil
	}

	params := map[string]any{
		"threadId":       threadID,
		"input":          input,
		"approvalPolicy": codexApprovalPolicy(s.runner.currentPermissionMode()),
	}
	if model := extractCodexModelFlag(s.req.Command); model != "" {
		params["model"] = model
	}
	if effort := extractCodexReasoningEffortFlag(s.req.Command); effort != "" {
		params["effort"] = effort
	}
	var resp codexTurnStartResponse
	if err := s.call(ctx, "turn/start", params, &resp); err != nil {
		return err
	}
	if strings.TrimSpace(resp.Turn.ID) != "" {
		s.setActiveTurnID(resp.Turn.ID)
	}
	return nil
}

func (s *codexAppSession) WritePermissionResponse(ctx context.Context, decision string) error {
	decision = strings.TrimSpace(strings.ToLower(decision))
	pending := s.pendingApprovalSnapshot()
	if pending == nil {
		return ErrNoPendingControlRequest
	}

	switch pending.method {
	case "item/fileChange/requestApproval":
		if err := s.respond(ctx, pending.id, map[string]any{
			"decision": codexFileApprovalDecision(s.runner.currentPermissionMode(), decision),
		}); err != nil {
			return err
		}
	case "item/commandExecution/requestApproval":
		if err := s.respond(ctx, pending.id, map[string]any{
			"decision": codexCommandApprovalDecision(s.runner.currentPermissionMode(), decision),
		}); err != nil {
			return err
		}
	case "item/permissions/requestApproval":
		if err := s.respond(ctx, pending.id, codexPermissionsApprovalResult(s.runner.currentPermissionMode(), decision, pending.permissions)); err != nil {
			return err
		}
	default:
		return ErrNoPendingControlRequest
	}

	s.clearPendingApproval(string(pending.id))
	return nil
}

func (s *codexAppSession) HasPendingPermissionRequest() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingApproval != nil && len(s.pendingApproval.id) > 0
}

func (s *codexAppSession) CurrentPermissionRequestID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingApproval == nil {
		return ""
	}
	return strings.TrimSpace(string(s.pendingApproval.id))
}

func (s *codexAppSession) Close() error {
	s.failPending(errors.New("codex app-server session closed"))
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

func (s *codexAppSession) readLoop(ctx context.Context, reader io.Reader) {
	err := forEachLine(reader, func(rawLine []byte) error {
		select {
		case <-ctx.Done():
			s.failPending(ctx.Err())
			return ctx.Err()
		default:
		}
		line := strings.TrimSpace(string(rawLine))
		if line == "" {
			return nil
		}
		var message codexRPCMessage
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			logx.Warn("pty", "codex app-server json parse failed: sessionID=%s err=%v preview=%q", s.sessionID, err, ptyDebugPreview(line))
			return nil
		}
		switch {
		case len(message.ID) > 0 && message.Method == "":
			s.resolvePending(message)
		case len(message.ID) > 0 && message.Method != "":
			s.handleServerRequest(ctx, message)
		case message.Method != "":
			s.handleNotification(message)
		}
		return nil
	})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
		s.failPending(err)
		return
	}
	s.failPending(io.EOF)
}

func (s *codexAppSession) readStderr(ctx context.Context) {
	if s.stderr == nil {
		return
	}
	_ = forEachLine(s.stderr, func(rawLine []byte) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		text := strings.TrimSpace(adapter.StripANSI(string(rawLine)))
		if codexShouldIgnoreStderr(text) || text == "" {
			return nil
		}
		sendEvent(s.sink, protocol.ApplyRuntimeMeta(
			protocol.NewLogEvent(s.sessionID, text, "stderr"),
			s.runtimeMeta("active"),
		))
		return nil
	})
}

func (s *codexAppSession) handleNotification(message codexRPCMessage) {
	switch message.Method {
	case "thread/started":
		var payload codexThreadEnvelope
		if err := json.Unmarshal(message.Params, &payload); err == nil {
			s.setThreadID(payload.Thread.ID)
		}
	case "thread/status/changed":
		var payload codexThreadStatusNotification
		if err := json.Unmarshal(message.Params, &payload); err == nil && strings.EqualFold(payload.Status.Type, "idle") && s.activeTurn() == "" {
			sendEvent(s.sink, protocol.ApplyRuntimeMeta(
				protocol.NewPromptRequestEvent(s.sessionID, "", nil),
				s.runtimeMeta("waiting_input"),
			))
		}
	case "turn/started":
		var payload codexTurnNotification
		if err := json.Unmarshal(message.Params, &payload); err == nil {
			s.setActiveTurnID(payload.Turn.ID)
		}
	case "turn/completed":
		s.handleTurnCompleted(message.Params)
	case "item/agentMessage/delta":
		var payload codexAgentDeltaNotification
		if err := json.Unmarshal(message.Params, &payload); err == nil {
			for _, chunk := range s.appendAssistantDelta(payload.Delta) {
				sendEvent(s.sink, protocol.ApplyRuntimeMeta(
					protocol.NewLogEvent(s.sessionID, chunk, "stdout"),
					s.runtimeMeta("active"),
				))
			}
		}
	case "item/started":
		s.handleItemEvent(message.Params, "running")
	case "item/completed":
		s.handleItemEvent(message.Params, "done")
	case "turn/diff/updated":
		var payload codexTurnDiffNotification
		if err := json.Unmarshal(message.Params, &payload); err == nil {
			s.handleDiffUpdate(payload.TurnID, payload.Diff)
		}
	case "error":
		var payload codexErrorNotification
		if err := json.Unmarshal(message.Params, &payload); err == nil {
			if payload.WillRetry {
				sendEvent(s.sink, protocol.ApplyRuntimeMeta(
					protocol.NewLogEvent(s.sessionID, strings.TrimSpace(payload.Error.Message), "stderr"),
					s.runtimeMeta("active"),
				))
				return
			}
			sendEvent(s.sink, protocol.ApplyRuntimeMeta(
				protocol.NewErrorEvent(s.sessionID, strings.TrimSpace(payload.Error.Message), strings.TrimSpace(payload.Error.AdditionalDetails)),
				s.runtimeMeta("active"),
			))
		}
	case "deprecationNotice":
		// Ignore Codex CLI feature flag upgrade banners in the chat timeline.
	default:
	}
}

func (s *codexAppSession) handleServerRequest(ctx context.Context, message codexRPCMessage) {
	switch message.Method {
	case "item/fileChange/requestApproval":
		var payload codexFileApprovalRequest
		if err := json.Unmarshal(message.Params, &payload); err != nil {
			return
		}
		s.cachePendingApproval(&codexPendingApproval{
			id:     cloneRawMessage(message.ID),
			method: message.Method,
		})
		sendEvent(s.sink, protocol.ApplyRuntimeMeta(
			protocol.NewPromptRequestEvent(s.sessionID, firstNonEmptyString(strings.TrimSpace(payload.Reason), "Codex 请求修改文件"), []string{codexPromptApprove, codexPromptDeny}),
			protocol.MergeRuntimeMeta(s.runtimeMeta("waiting_input"), protocol.RuntimeMeta{BlockingKind: "permission"}),
		))
	case "item/commandExecution/requestApproval":
		var payload codexCommandApprovalRequest
		if err := json.Unmarshal(message.Params, &payload); err != nil {
			return
		}
		s.cachePendingApproval(&codexPendingApproval{
			id:     cloneRawMessage(message.ID),
			method: message.Method,
		})
		sendEvent(s.sink, protocol.ApplyRuntimeMeta(
			protocol.NewPromptRequestEvent(s.sessionID, codexCommandPromptMessage(payload), []string{codexPromptApprove, codexPromptDeny}),
			protocol.MergeRuntimeMeta(s.runtimeMeta("waiting_input"), protocol.RuntimeMeta{BlockingKind: "permission"}),
		))
	case "item/permissions/requestApproval":
		var payload codexPermissionsApprovalRequest
		if err := json.Unmarshal(message.Params, &payload); err != nil {
			return
		}
		s.cachePendingApproval(&codexPendingApproval{
			id:          cloneRawMessage(message.ID),
			method:      message.Method,
			permissions: cloneRawMessage(payload.Permissions),
		})
		sendEvent(s.sink, protocol.ApplyRuntimeMeta(
			protocol.NewPromptRequestEvent(s.sessionID, firstNonEmptyString(strings.TrimSpace(payload.Reason), "Codex 请求额外权限"), []string{codexPromptApprove, codexPromptDeny}),
			protocol.MergeRuntimeMeta(s.runtimeMeta("waiting_input"), protocol.RuntimeMeta{BlockingKind: "permission"}),
		))
	case "item/tool/requestUserInput":
		sendEvent(s.sink, protocol.ApplyRuntimeMeta(
			protocol.NewErrorEvent(s.sessionID, "Codex 请求了结构化用户输入，当前 MobileVC 版本暂未完成该通路", ""),
			s.runtimeMeta("waiting_input"),
		))
		_ = s.respond(ctx, message.ID, map[string]any{"answers": map[string]any{}})
	default:
	}
}

func (s *codexAppSession) handleTurnCompleted(raw json.RawMessage) {
	var payload codexTurnNotification
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	for _, chunk := range s.flushAssistantDelta() {
		sendEvent(s.sink, protocol.ApplyRuntimeMeta(
			protocol.NewLogEvent(s.sessionID, chunk, "stdout"),
			s.runtimeMeta("active"),
		))
	}
	s.clearActiveTurnID(payload.Turn.ID)
	if payload.Turn.Error != nil && strings.TrimSpace(payload.Turn.Error.Message) != "" {
		sendEvent(s.sink, protocol.ApplyRuntimeMeta(
			protocol.NewErrorEvent(s.sessionID, strings.TrimSpace(payload.Turn.Error.Message), strings.TrimSpace(payload.Turn.Error.AdditionalDetails)),
			s.runtimeMeta("active"),
		))
	}
	sendEvent(s.sink, protocol.ApplyRuntimeMeta(
		protocol.NewPromptRequestEvent(s.sessionID, "", nil),
		s.runtimeMeta("waiting_input"),
	))
}

func (s *codexAppSession) handleItemEvent(raw json.RawMessage, status string) {
	var payload codexItemNotification
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	itemType := strings.TrimSpace(asString(payload.Item["type"]))
	if itemType == "" {
		return
	}
	if status == "done" && itemType == "agentMessage" {
		for _, chunk := range s.flushAssistantDelta() {
			sendEvent(s.sink, protocol.ApplyRuntimeMeta(
				protocol.NewLogEvent(s.sessionID, chunk, "stdout"),
				s.runtimeMeta("active"),
			))
		}
		return
	}
	message, target := codexItemStepSummary(payload.Item, status)
	if message == "" {
		return
	}
	sendEvent(s.sink, protocol.ApplyRuntimeMeta(
		protocol.NewStepUpdateEvent(s.sessionID, message, status, target, itemType, ""),
		s.runtimeMeta("active"),
	))
}

func (s *codexAppSession) handleDiffUpdate(turnID, diff string) {
	diff = strings.TrimSpace(diff)
	if diff == "" {
		return
	}
	s.mu.Lock()
	if diff == s.lastDiff {
		s.mu.Unlock()
		return
	}
	s.lastDiff = diff
	s.mu.Unlock()

	path := codexDiffPath(diff)
	title := "Codex changes"
	if path != "" {
		title = "Updating " + path
	}
	meta := s.runtimeMeta("active")
	meta.ContextID = "codex-turn:" + strings.TrimSpace(turnID)
	meta.ContextTitle = title
	if path != "" {
		meta.TargetPath = path
	}
	sendEvent(s.sink, protocol.ApplyRuntimeMeta(
		protocol.NewFileDiffEvent(s.sessionID, path, title, diff, codexGuessLangFromPath(path)),
		meta,
	))
}

func (s *codexAppSession) call(ctx context.Context, method string, params any, result any) error {
	id := atomic.AddInt64(&s.nextID, 1)
	idRaw := json.RawMessage([]byte(fmt.Sprintf("%d", id)))

	paramsRaw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal %s params: %w", method, err)
	}

	responseCh := make(chan codexRPCResponse, 1)
	key := string(idRaw)
	s.mu.Lock()
	if s.pending == nil {
		s.pending = make(map[string]chan codexRPCResponse)
	}
	s.pending[key] = responseCh
	readErr := s.readErr
	s.mu.Unlock()
	if readErr != nil {
		return readErr
	}

	if err := s.writeMessage(codexRPCMessage{
		JSONRPC: "2.0",
		ID:      idRaw,
		Method:  method,
		Params:  paramsRaw,
	}); err != nil {
		s.removePending(key)
		return err
	}

	select {
	case <-ctx.Done():
		s.removePending(key)
		return ctx.Err()
	case response := <-responseCh:
		if response.err != nil {
			return response.err
		}
		if response.message.Error != nil {
			return fmt.Errorf("%s: %s", method, strings.TrimSpace(response.message.Error.Message))
		}
		if result != nil && len(response.message.Result) > 0 {
			if err := json.Unmarshal(response.message.Result, result); err != nil {
				return fmt.Errorf("decode %s result: %w", method, err)
			}
		}
		return nil
	}
}

func (s *codexAppSession) respond(ctx context.Context, id json.RawMessage, result any) error {
	resultRaw, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal codex response: %w", err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return s.writeMessage(codexRPCMessage{
		JSONRPC: "2.0",
		ID:      cloneRawMessage(id),
		Result:  resultRaw,
	})
}

func (s *codexAppSession) writeMessage(message codexRPCMessage) error {
	if s.stdin == nil {
		return errors.New("codex app-server stdin is unavailable")
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = s.stdin.Write(payload)
	return err
}

func (s *codexAppSession) resolvePending(message codexRPCMessage) {
	key := string(message.ID)
	s.mu.Lock()
	ch := s.pending[key]
	delete(s.pending, key)
	s.mu.Unlock()
	if ch == nil {
		return
	}
	ch <- codexRPCResponse{message: message}
}

func (s *codexAppSession) failPending(err error) {
	if err == nil {
		err = io.EOF
	}
	s.mu.Lock()
	if s.readErr == nil {
		s.readErr = err
	}
	pending := s.pending
	s.pending = make(map[string]chan codexRPCResponse)
	s.mu.Unlock()
	for _, ch := range pending {
		ch <- codexRPCResponse{err: err}
	}
}

func (s *codexAppSession) removePending(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, key)
}

func (s *codexAppSession) setThreadID(threadID string) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}
	s.mu.Lock()
	s.threadID = threadID
	s.mu.Unlock()

	if s.runner != nil {
		s.runner.mu.Lock()
		s.runner.claudeSessionID = threadID
		s.runner.mu.Unlock()
	}
}

func (s *codexAppSession) setActiveTurnID(turnID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeTurnID = strings.TrimSpace(turnID)
}

func (s *codexAppSession) clearActiveTurnID(turnID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(turnID) == "" || s.activeTurnID == strings.TrimSpace(turnID) {
		s.activeTurnID = ""
	}
}

func (s *codexAppSession) activeTurn() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeTurnID
}

func (s *codexAppSession) threadAndTurn() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.threadID, s.activeTurnID
}

func (s *codexAppSession) runtimeMeta(lifecycle string) protocol.RuntimeMeta {
	meta := protocol.RuntimeMeta{
		ResumeSessionID: s.threadIDValue(),
		ClaudeLifecycle: lifecycle,
	}
	if meta.ClaudeLifecycle == "" {
		meta.ClaudeLifecycle = "active"
	}
	if meta.ClaudeLifecycle == "waiting_input" {
		meta.BlockingKind = "ready"
	}
	return meta
}

func (s *codexAppSession) threadIDValue() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.threadID
}

func (s *codexAppSession) cachePendingApproval(pending *codexPendingApproval) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingApproval = pending
}

func (s *codexAppSession) pendingApprovalSnapshot() *codexPendingApproval {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingApproval == nil {
		return nil
	}
	copy := *s.pendingApproval
	copy.id = cloneRawMessage(s.pendingApproval.id)
	copy.permissions = cloneRawMessage(s.pendingApproval.permissions)
	return &copy
}

func (s *codexAppSession) clearPendingApproval(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingApproval == nil {
		return
	}
	if id == "" || string(s.pendingApproval.id) == id {
		s.pendingApproval = nil
	}
}

func (s *codexAppSession) appendAssistantDelta(delta string) []string {
	if delta == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assistantBuffer.WriteString(delta)
	return codexDrainAssistantChunks(&s.assistantBuffer, false)
}

func (s *codexAppSession) flushAssistantDelta() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return codexDrainAssistantChunks(&s.assistantBuffer, true)
}

func codexDrainAssistantChunks(buffer *strings.Builder, flushAll bool) []string {
	text := buffer.String()
	if text == "" {
		return nil
	}

	var emitted []string
	for {
		idx := strings.IndexByte(text, '\n')
		if idx < 0 {
			break
		}
		chunk := strings.TrimSpace(text[:idx+1])
		if chunk != "" {
			emitted = append(emitted, chunk)
		}
		text = text[idx+1:]
	}

	trimmed := strings.TrimSpace(text)
	if trimmed != "" {
		runeCount := len([]rune(trimmed))
		if flushAll || runeCount >= 64 || (runeCount >= 24 && endsWithLiveTailBoundary(trimmed)) {
			emitted = append(emitted, trimmed)
			text = ""
		}
	}

	buffer.Reset()
	if text != "" {
		buffer.WriteString(text)
	}
	return emitted
}

func codexApprovalPolicy(permissionMode string) string {
	// Codex 默认必须走 on-request，避免文件修改或命令执行在未显式授权时直接放行。
	// 只有用户显式配置 bypassPermissions 时，才允许完全跳过审批。
	// 如果线上看起来“Codex 文件修改不需要授权”，更可能是该改动没有走
	// item/fileChange/requestApproval，而是直接以 turn diff 的形式下发，而不是这里默认开了绿灯。
	switch strings.TrimSpace(permissionMode) {
	case "bypassPermissions":
		return "never"
	default:
		return "on-request"
	}
}

func codexFileApprovalDecision(permissionMode, decision string) string {
	if decision == "deny" {
		return "decline"
	}
	if strings.TrimSpace(permissionMode) == "acceptEdits" {
		return "acceptForSession"
	}
	return "accept"
}

func codexCommandApprovalDecision(permissionMode, decision string) any {
	if decision == "deny" {
		return "decline"
	}
	if strings.TrimSpace(permissionMode) == "acceptEdits" {
		return "acceptForSession"
	}
	return "accept"
}

func codexPermissionsApprovalResult(permissionMode, decision string, requested json.RawMessage) map[string]any {
	scope := "turn"
	if strings.TrimSpace(permissionMode) == "acceptEdits" {
		scope = "session"
	}
	if decision == "deny" || len(requested) == 0 {
		return map[string]any{
			"permissions": map[string]any{},
			"scope":       scope,
		}
	}
	var permissions map[string]any
	if err := json.Unmarshal(requested, &permissions); err != nil || permissions == nil {
		permissions = map[string]any{}
	}
	return map[string]any{
		"permissions": permissions,
		"scope":       scope,
	}
}

func codexCommandPromptMessage(payload codexCommandApprovalRequest) string {
	switch {
	case strings.TrimSpace(payload.Command) != "" && strings.TrimSpace(payload.Reason) != "":
		return fmt.Sprintf("%s\n\n命令：%s", strings.TrimSpace(payload.Reason), strings.TrimSpace(payload.Command))
	case strings.TrimSpace(payload.Command) != "":
		return "Codex 请求执行命令：\n" + strings.TrimSpace(payload.Command)
	case strings.TrimSpace(payload.Reason) != "":
		return strings.TrimSpace(payload.Reason)
	default:
		return "Codex 请求执行命令"
	}
}

func codexItemStepSummary(item map[string]any, status string) (string, string) {
	itemType := strings.TrimSpace(asString(item["type"]))
	switch itemType {
	case "commandExecution":
		command := strings.TrimSpace(asString(item["command"]))
		if command == "" {
			command = "shell command"
		}
		if status == "done" {
			return "Completed command", command
		}
		return "Running command", command
	case "fileChange":
		if status == "done" {
			return "Applied file changes", ""
		}
		return "Applying file changes", ""
	case "mcpToolCall", "dynamicToolCall":
		tool := strings.TrimSpace(asString(item["tool"]))
		if status == "done" {
			return "Completed tool call", tool
		}
		return "Running tool call", tool
	case "webSearch":
		query := strings.TrimSpace(asString(item["query"]))
		if status == "done" {
			return "Completed web search", query
		}
		return "Running web search", query
	default:
		return "", ""
	}
}

func codexDiffPath(diff string) string {
	lines := strings.Split(strings.ReplaceAll(diff, "\r\n", "\n"), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if matches := codexDiffPathPattern.FindStringSubmatch(trimmed); len(matches) == 3 {
			return strings.TrimSpace(matches[2])
		}
		if strings.HasPrefix(trimmed, "+++ b/") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "+++ b/"))
		}
	}
	return ""
}

func codexGuessLangFromPath(path string) string {
	switch {
	case strings.HasSuffix(path, ".go"):
		return "go"
	case strings.HasSuffix(path, ".js"), strings.HasSuffix(path, ".ts"), strings.HasSuffix(path, ".tsx"):
		return "javascript"
	case strings.HasSuffix(path, ".dart"):
		return "dart"
	case strings.HasSuffix(path, ".py"):
		return "python"
	case strings.HasSuffix(path, ".md"):
		return "markdown"
	case strings.TrimSpace(path) == "":
		return ""
	default:
		return "text"
	}
}

func codexShouldIgnoreStderr(text string) bool {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)
	if lower == "" {
		return true
	}
	if codexStructuredStderrPattern.MatchString(trimmed) && strings.Contains(lower, "codex_") {
		return true
	}
	if strings.Contains(lower, "could not update path") {
		return true
	}
	if strings.Contains(lower, "failed to record rollout items") {
		return true
	}
	return false
}

func cloneRawMessage(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	cloned := make([]byte, len(value))
	copy(cloned, value)
	return cloned
}

func asString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		return ""
	}
}
