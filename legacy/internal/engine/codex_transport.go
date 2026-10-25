package engine

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
	"time"
	"unicode"

	"mobilevc/internal/logx"
	"mobilevc/internal/protocol"
)

const (
	codexPromptApprove = "approve"
	codexPromptDeny    = "deny"

	codexAssistantInitialChunkRunes = 8
	codexAssistantMinChunkRunes     = 8
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

type codexRPCMethodError struct {
	method  string
	code    int
	message string
}

func (e *codexRPCMethodError) Error() string {
	return fmt.Sprintf("%s: %s", e.method, strings.TrimSpace(e.message))
}

type codexAppSession struct {
	runner    *PtyRunner
	sessionID string
	req       ExecRequest
	cwd       string
	sink      EventSink
	defaults  codexConfigDefaults

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  int64
	pending map[string]chan codexRPCResponse
	readErr error

	threadID                     string
	activeTurnID                 string
	assistantTurnID              string
	lastDiff                     string
	assistantBuffer              strings.Builder
	assistantEmitted             string
	assistantLastFlushed         string
	pendingApproval              *codexPendingApproval
	readyPromptSeq               uint64
	compactionID                 string
	compactionTurnID             string
	compactionStatus             string
	compactionEventID            string
	contextWindowReadUnsupported bool
	reasoningSummaries           map[string]*codexReasoningSummaryState
}

type codexPendingApproval struct {
	id          json.RawMessage
	method      string
	permissions json.RawMessage
}

type codexReasoningSummaryState struct {
	turnID      string
	itemID      string
	parts       []string
	lastEmitted string
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

type codexReasoningSummaryDeltaNotification struct {
	ThreadID     string `json:"threadId"`
	TurnID       string `json:"turnId"`
	ItemID       string `json:"itemId"`
	SummaryIndex int    `json:"summaryIndex"`
	Delta        string `json:"delta"`
}

type codexReasoningSummaryPartNotification struct {
	ThreadID     string `json:"threadId"`
	TurnID       string `json:"turnId"`
	ItemID       string `json:"itemId"`
	SummaryIndex int    `json:"summaryIndex"`
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

type codexHookNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	Run      struct {
		ID        string `json:"id"`
		EventName string `json:"eventName"`
	} `json:"run"`
}

type codexTokenUsageNotification struct {
	ThreadID   string `json:"threadId"`
	TurnID     string `json:"turnId"`
	TokenUsage any    `json:"tokenUsage"`
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
	defaults, err := loadCodexConfigDefaults()
	if err != nil {
		_ = stdin.Close()
		killCommandProcess(cmd)
		return nil, err
	}

	app := &codexAppSession{
		runner:    runner,
		sessionID: req.SessionID,
		req:       req,
		cwd:       cwd,
		sink:      sink,
		defaults:  defaults,
		cmd:       cmd,
		stdin:     stdin,
		stdout:    stdout,
		stderr:    stderr,
		pending:   make(map[string]chan codexRPCResponse),
	}

	go app.readLoop(processCtx, stdout)
	go app.readStderr(processCtx)

	if err := app.initialize(rpcCtx); err != nil {
		_ = stdin.Close()
		killCommandProcess(cmd)
		return nil, err
	}
	if err := app.startOrResumeThread(rpcCtx, resumeSessionID); err != nil {
		_ = stdin.Close()
		killCommandProcess(cmd)
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
		threadParams, err := s.threadRuntimeParams()
		if err != nil {
			return err
		}
		threadParams["threadId"] = resumeSessionID
		delete(threadParams, "approvalsReviewer")
		delete(threadParams, "serviceName")
		delete(threadParams, "experimentalRawEvents")
		params = threadParams
	} else {
		method = "thread/start"
		threadParams, err := s.threadRuntimeParams()
		if err != nil {
			return err
		}
		params = threadParams
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

func (s *codexAppSession) threadRuntimeParams() (map[string]any, error) {
	sandbox, err := normalizeCodexSandboxMode(s.req.RuntimeMeta.CodexSandboxMode, s.defaults)
	if err != nil {
		return nil, err
	}
	params := map[string]any{
		"cwd":                   s.cwd,
		"approvalPolicy":        codexApprovalPolicy(s.runner.currentPermissionMode(), s.defaults),
		"approvalsReviewer":     "user",
		"sandbox":               sandbox,
		"serviceName":           "MobileVC",
		"experimentalRawEvents": false,
	}
	if model := extractCodexModelFlag(s.req.Command); model != "" {
		params["model"] = model
	} else if model := s.defaults.model; model != "" {
		params["model"] = model
	}
	return params, nil
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

	params, err := s.turnStartParams(threadID, input)
	if err != nil {
		return err
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

func (s *codexAppSession) turnStartParams(threadID string, input []map[string]any) (map[string]any, error) {
	sandbox, err := normalizeCodexSandboxMode(s.req.RuntimeMeta.CodexSandboxMode, s.defaults)
	if err != nil {
		return nil, err
	}
	params := map[string]any{
		"threadId":       threadID,
		"input":          input,
		"approvalPolicy": codexApprovalPolicy(s.runner.currentPermissionMode(), s.defaults),
		"sandbox":        sandbox,
	}
	if model := extractCodexModelFlag(s.req.Command); model != "" {
		params["model"] = model
	} else if model := s.defaults.model; model != "" {
		params["model"] = model
	}
	if effort := extractCodexReasoningEffortFlag(s.req.Command); effort != "" {
		params["effort"] = effort
	} else if effort := s.defaults.reasoningEffort; effort != "" {
		params["effort"] = effort
	}
	return params, nil
}

func (s *codexAppSession) Compact(ctx context.Context) error {
	threadID := strings.TrimSpace(s.threadIDValue())
	if threadID == "" {
		return errors.New("codex thread is not ready")
	}
	if strings.TrimSpace(s.activeTurn()) != "" {
		return errors.New("a foreground turn is already active")
	}
	s.resetCompactionLifecycle()
	s.setCompactionID(fmt.Sprintf("compaction:%d", time.Now().UTC().UnixNano()))
	s.emitCompactionEvent("loading", "manual", "")
	var resp codexTurnStartResponse
	if err := s.call(ctx, "thread/compact/start", map[string]any{
		"threadId": threadID,
	}, &resp); err != nil {
		s.emitCompactionEvent("failed", "manual", err.Error())
		return err
	}
	if strings.TrimSpace(resp.Turn.ID) != "" {
		s.setActiveTurnID(resp.Turn.ID)
		s.setCompactionTurnID(resp.Turn.ID)
	}
	return nil
}

func (s *codexAppSession) ContextWindowUsage(ctx context.Context) (protocol.ContextWindowUsage, bool, error) {
	threadID := strings.TrimSpace(s.threadIDValue())
	if threadID == "" {
		return protocol.ContextWindowUsage{}, false, nil
	}
	s.mu.Lock()
	readUnsupported := s.contextWindowReadUnsupported
	s.mu.Unlock()
	if readUnsupported {
		return protocol.ContextWindowUsage{}, false, nil
	}

	var result map[string]any
	if err := s.call(ctx, "thread/contextWindow/read", map[string]any{
		"threadId": threadID,
	}, &result); err != nil {
		if isCodexUnsupportedMethodError(err, "thread/contextWindow/read") {
			s.mu.Lock()
			s.contextWindowReadUnsupported = true
			s.mu.Unlock()
			return protocol.ContextWindowUsage{}, false, nil
		}
		return protocol.ContextWindowUsage{}, false, err
	}

	raw, ok := result["usage"]
	if !ok {
		return protocol.ContextWindowUsage{}, false, nil
	}
	usage, parsed := codexContextWindowUsage(raw)
	if !parsed {
		return protocol.ContextWindowUsage{}, false, nil
	}
	return usage, true, nil
}

func isCodexUnsupportedMethodError(err error, method string) bool {
	var rpcErr *codexRPCMethodError
	if !errors.As(err, &rpcErr) {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(rpcErr.method), strings.TrimSpace(method)) {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(rpcErr.message))
	lowerMethod := strings.ToLower(strings.TrimSpace(method))
	if strings.Contains(message, "unknown variant") && strings.Contains(message, "`"+lowerMethod+"`") {
		return true
	}
	return strings.Contains(message, "method not found") ||
		strings.Contains(message, "unknown method")
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
		killCommandProcess(s.cmd)
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
		if s.runner != nil {
			s.runner.markOutputSeen()
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
		logx.Info("pty", "codex app-server rpc received: sessionID=%s id=%s method=%q hasResult=%t hasError=%t paramsPreview=%q resultPreview=%q", s.sessionID, strings.TrimSpace(string(message.ID)), message.Method, len(message.Result) > 0, message.Error != nil, ptyDebugPreview(string(message.Params)), ptyDebugPreview(string(message.Result)))
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
		text := strings.TrimSpace(StripANSI(string(rawLine)))
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
			s.emitReadyPromptAfterReply()
		}
	case "turn/started":
		var payload codexTurnNotification
		if err := json.Unmarshal(message.Params, &payload); err == nil {
			s.setActiveTurnID(payload.Turn.ID)
			sendEvent(s.sink, protocol.ApplyRuntimeMeta(
				protocol.NewAgentStateEvent(s.sessionID, "THINKING", "处理中", false, "", "", ""),
				s.runtimeMeta("active"),
			))
		}
	case "hook/started":
		s.handleHookEvent(message.Params, "running")
	case "hook/completed":
		s.handleHookEvent(message.Params, "done")
	case "turn/completed":
		s.handleTurnCompleted(message.Params)
	case "item/agentMessage/delta":
		var payload codexAgentDeltaNotification
		if err := json.Unmarshal(message.Params, &payload); err == nil {
			for _, chunk := range s.appendAssistantDelta(payload.Delta) {
				s.emitAssistantChunk(chunk, payload.TurnID)
			}
		}
	case "item/reasoning/summaryTextDelta":
		s.handleReasoningSummaryDelta(message.Params)
	case "item/reasoning/summaryPartAdded":
		s.handleReasoningSummaryPartAdded(message.Params)
	case "item/started":
		s.handleItemEvent(message.Params, "running")
	case "item/completed":
		s.handleItemEvent(message.Params, "done")
	case "rawResponseItem/completed":
		s.handleRawResponseItemCompleted(message.Params)
	case "thread/tokenUsage/updated":
		var payload codexTokenUsageNotification
		if err := json.Unmarshal(message.Params, &payload); err == nil {
			s.handleTokenUsageUpdated(payload.TokenUsage)
		}
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
			if s.isCompactionTurn(payload.TurnID) {
				s.emitCompactionEvent("failed", "manual", strings.TrimSpace(payload.Error.Message))
				s.clearCompactionTurnID(payload.TurnID)
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

func (s *codexAppSession) handleHookEvent(raw json.RawMessage, status string) {
	var payload codexHookNotification
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	label := codexHookStatusLabel(payload.Run.EventName, status)
	if label == "" {
		return
	}
	meta := s.runtimeMeta("active")
	meta.ContextID = "codex-hook:" + strings.TrimSpace(payload.Run.ID)
	sendEvent(s.sink, protocol.NewAIStatusEvent(s.sessionID, status == "running", label, "running_hook", meta))
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
		s.emitAssistantChunk(chunk, payload.Turn.ID)
	}
	s.clearActiveTurnID(payload.Turn.ID)
	if payload.Turn.Error != nil && strings.TrimSpace(payload.Turn.Error.Message) != "" {
		if s.isCompactionTurn(payload.Turn.ID) {
			s.ensureCompactionID(payload.Turn.ID)
			s.emitCompactionEvent("failed", "manual", strings.TrimSpace(payload.Turn.Error.Message))
			s.clearCompactionTurnID(payload.Turn.ID)
		} else {
			sendEvent(s.sink, protocol.ApplyRuntimeMeta(
				protocol.NewErrorEvent(s.sessionID, strings.TrimSpace(payload.Turn.Error.Message), strings.TrimSpace(payload.Turn.Error.AdditionalDetails)),
				s.runtimeMeta("active"),
			))
		}
	}
	if s.isCompactionTurn(payload.Turn.ID) {
		s.ensureCompactionID(payload.Turn.ID)
		s.emitCompactionEvent("completed", "manual", "")
		s.clearCompactionTurnID(payload.Turn.ID)
	}
	s.emitReadyPromptAfterReply()
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
	if itemType == "contextCompaction" {
		s.handleCompactionItem(payload, status)
		if status == "done" {
			return
		}
	}
	if status == "done" && itemType == "agentMessage" {
		if text := codexItemText(payload.Item); text != "" {
			s.emitAssistantCompletedText(text, payload.TurnID)
		} else {
			for _, chunk := range s.flushAssistantDelta() {
				s.emitAssistantChunk(chunk, payload.TurnID)
			}
		}
		return
	}
	if status == "done" && itemType == "reasoning" {
		s.handleCompletedReasoningItem(payload)
		return
	}
	if status == "done" {
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

func (s *codexAppSession) handleCompactionItem(payload codexItemNotification, status string) {
	trigger := "manual"
	s.ensureCompactionID(payload.TurnID)
	if status == "running" {
		if payload.TurnID != "" {
			s.setCompactionTurnID(payload.TurnID)
		}
		s.emitCompactionEvent("loading", trigger, "")
		return
	}
	if status != "done" {
		return
	}
	message := strings.TrimSpace(firstNonEmptyString(
		asString(payload.Item["message"]),
		codexItemText(payload.Item),
	))
	statusValue := strings.TrimSpace(strings.ToLower(asString(payload.Item["status"])))
	if statusValue == "failed" {
		s.emitCompactionEvent("failed", trigger, message)
		if payload.TurnID != "" {
			s.clearCompactionTurnID(payload.TurnID)
		}
		return
	}
	s.emitCompactionEvent("completed", trigger, "")
	if payload.TurnID != "" {
		s.clearCompactionTurnID(payload.TurnID)
	}
}

func (s *codexAppSession) handleRawResponseItemCompleted(raw json.RawMessage) {
	var payload codexItemNotification
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	if strings.TrimSpace(asString(payload.Item["type"])) == "reasoning" {
		s.handleCompletedReasoningItem(payload)
		return
	}
	text := codexItemText(payload.Item)
	if text == "" {
		return
	}
	s.emitAssistantCompletedText(text, payload.TurnID)
}

func (s *codexAppSession) handleTokenUsageUpdated(raw any) {
	usage, ok := codexContextWindowUsage(raw)
	if !ok {
		return
	}
	meta := s.runtimeMeta("active")
	sendEvent(s.sink, protocol.ApplyRuntimeMeta(
		protocol.NewContextWindowUsageEvent(s.sessionID, usage),
		meta,
	))
}

func (s *codexAppSession) emitAssistantChunk(text string, turnID string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	s.mu.Lock()
	s.assistantEmitted += text
	s.mu.Unlock()
	meta := s.assistantRuntimeMeta("active", turnID)
	meta.Source = "codex/assistant"
	sendEvent(s.sink, protocol.ApplyRuntimeMeta(
		protocol.NewLogEvent(s.sessionID, text, "stdout"),
		meta,
	))
}

func codexContextWindowUsage(raw any) (protocol.ContextWindowUsage, bool) {
	record, ok := raw.(map[string]any)
	if !ok || len(record) == 0 {
		return protocol.ContextWindowUsage{}, false
	}
	last := asMap(record["last"])
	total := asMap(record["total"])
	contextWindow := firstPositiveInt(
		record["model_context_window"],
		record["modelContextWindow"],
		record["context_window"],
		record["contextWindow"],
		total["modelContextWindow"],
		total["model_context_window"],
		last["modelContextWindow"],
		last["model_context_window"],
	)
	if contextWindow <= 0 {
		return protocol.ContextWindowUsage{}, false
	}
	usedTokens := firstNonNegativeInt(
		last["total_tokens"],
		last["totalTokens"],
		total["total_tokens"],
		total["totalTokens"],
		record["total_tokens"],
		record["totalTokens"],
	)
	if usedTokens < 0 {
		return protocol.ContextWindowUsage{}, false
	}
	return protocol.NormalizeContextWindowUsage(protocol.ContextWindowUsage{
		TokensUsed: usedTokens,
		TokenLimit: contextWindow,
	}), true
}

func asMap(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return map[string]any{}
}

func firstPositiveInt(values ...any) int {
	for _, value := range values {
		number := intValue(value)
		if number > 0 {
			return number
		}
	}
	return 0
}

func firstNonNegativeInt(values ...any) int {
	for _, value := range values {
		number := intValue(value)
		if number >= 0 {
			return number
		}
	}
	return -1
}

func intValue(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return int(parsed)
		}
	case string:
		if parsed, err := json.Number(strings.TrimSpace(v)).Int64(); err == nil {
			return int(parsed)
		}
	}
	return -1
}

func (s *codexAppSession) emitAssistantCompletedText(text string, turnID string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	s.mu.Lock()
	emitted := s.assistantEmitted
	s.assistantBuffer.Reset()
	s.assistantLastFlushed = ""
	if strings.TrimSpace(emitted) != "" {
		if text == emitted || codexAssistantDedupeText(text) == codexAssistantDedupeText(emitted) {
			s.assistantEmitted = text
			s.mu.Unlock()
			return
		}
		if strings.HasPrefix(text, emitted) {
			text = strings.TrimPrefix(text, emitted)
			if strings.TrimSpace(text) == "" {
				s.assistantEmitted = emitted
				s.mu.Unlock()
				return
			}
		} else if suffix, ok := trimCodexCompletedPrefix(text, emitted); ok {
			text = suffix
			if strings.TrimSpace(text) == "" {
				s.assistantEmitted = emitted
				s.mu.Unlock()
				return
			}
		}
	}
	s.mu.Unlock()
	s.emitAssistantChunk(text, turnID)
}

func (s *codexAppSession) handleReasoningSummaryDelta(raw json.RawMessage) {
	var payload codexReasoningSummaryDeltaNotification
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	if payload.Delta == "" {
		return
	}
	state := s.ensureReasoningSummaryState(payload.TurnID, payload.ItemID)
	if state == nil {
		return
	}
	state.appendDelta(payload.SummaryIndex, payload.Delta)
	s.emitReasoningSummaryState(state, false)
}

func (s *codexAppSession) handleReasoningSummaryPartAdded(raw json.RawMessage) {
	var payload codexReasoningSummaryPartNotification
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	state := s.ensureReasoningSummaryState(payload.TurnID, payload.ItemID)
	if state == nil {
		return
	}
	state.ensurePart(payload.SummaryIndex)
}

func (s *codexAppSession) handleCompletedReasoningItem(payload codexItemNotification) {
	itemID := strings.TrimSpace(asString(payload.Item["id"]))
	if itemID == "" {
		itemID = strings.TrimSpace(payload.TurnID)
	}
	text := codexReasoningSummaryText(payload.Item)
	if strings.TrimSpace(text) == "" {
		return
	}
	state := s.ensureReasoningSummaryState(payload.TurnID, itemID)
	if state == nil {
		return
	}
	state.setCompletedSummary(text)
	s.emitReasoningSummaryState(state, true)
}

func (s *codexAppSession) ensureReasoningSummaryState(turnID, itemID string) *codexReasoningSummaryState {
	turnID = strings.TrimSpace(firstNonEmptyString(turnID, s.activeTurn()))
	itemID = strings.TrimSpace(itemID)
	if turnID == "" || itemID == "" {
		return nil
	}
	key := codexReasoningSummaryKey(turnID, itemID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reasoningSummaries == nil {
		s.reasoningSummaries = make(map[string]*codexReasoningSummaryState)
	}
	state := s.reasoningSummaries[key]
	if state == nil {
		state = &codexReasoningSummaryState{
			turnID: turnID,
			itemID: itemID,
		}
		s.reasoningSummaries[key] = state
	}
	return state
}

func (s *codexAppSession) emitReasoningSummaryState(state *codexReasoningSummaryState, completed bool) {
	content := state.content()
	if strings.TrimSpace(content) == "" {
		return
	}
	if completed && strings.TrimSpace(state.lastEmitted) != "" &&
		codexAssistantDedupeText(content) == codexAssistantDedupeText(state.lastEmitted) {
		return
	}
	if !completed && content == state.lastEmitted {
		return
	}
	state.lastEmitted = content
	meta := s.reasoningRuntimeMeta(state.turnID, state.itemID)
	sendEvent(s.sink, protocol.ApplyRuntimeMeta(
		protocol.NewThinkingEvent(s.sessionID, content),
		meta,
	))
}

func (s *codexAppSession) reasoningRuntimeMeta(turnID, itemID string) protocol.RuntimeMeta {
	meta := s.runtimeMeta("active")
	if trimmed := strings.TrimSpace(turnID); trimmed != "" {
		meta.ExecutionID = trimmed
	}
	meta.Engine = "codex"
	meta.Source = "codex/reasoning-summary"
	meta.ContextID = codexReasoningSummaryKey(meta.ExecutionID, itemID)
	meta.ContextTitle = "Codex reasoning summary"
	return meta
}

func (s *codexAppSession) emitReadyPromptAfterReply() {
	sessionID := s.sessionID
	sink := s.sink
	meta := s.runtimeMeta("waiting_input")
	s.mu.Lock()
	s.readyPromptSeq++
	seq := s.readyPromptSeq
	s.mu.Unlock()
	go func() {
		timer := time.NewTimer(350 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-s.runner.commandContext().Done():
			return
		case <-timer.C:
		}
		s.mu.Lock()
		if seq != s.readyPromptSeq {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		s.runner.markInteractiveReady()
		emitCodexReadyPrompt(sessionID, sink, meta)
	}()
}

func (s *codexAppSession) emitReadyPrompt() {
	emitCodexReadyPrompt(s.sessionID, s.sink, s.runtimeMeta("waiting_input"))
}

func emitCodexReadyPrompt(sessionID string, sink EventSink, meta protocol.RuntimeMeta) {
	sendEvent(sink, protocol.ApplyRuntimeMeta(
		protocol.NewPromptRequestEvent(sessionID, "", nil),
		meta,
	))
}

func codexItemText(item map[string]any) string {
	for _, key := range []string{"text", "message", "content", "output_text"} {
		if text := strings.TrimSpace(asString(item[key])); text != "" {
			return text
		}
	}
	if content, ok := item["content"].([]any); ok {
		var parts []string
		for _, entry := range content {
			entryMap, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			for _, key := range []string{"text", "content", "output_text"} {
				if text := strings.TrimSpace(asString(entryMap[key])); text != "" {
					parts = append(parts, text)
					break
				}
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	}
	return ""
}

func codexReasoningSummaryText(item map[string]any) string {
	parts := codexReasoningSummaryParts(item["summary"])
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func codexReasoningSummaryParts(value any) []string {
	switch v := value.(type) {
	case string:
		if text := strings.TrimSpace(v); text != "" {
			return []string{text}
		}
	case []any:
		parts := make([]string, 0, len(v))
		for _, entry := range v {
			if text := codexReasoningSummaryPartText(entry); text != "" {
				parts = append(parts, text)
			}
		}
		return parts
	case map[string]any:
		if text := codexReasoningSummaryPartText(v); text != "" {
			return []string{text}
		}
	}
	return nil
}

func codexReasoningSummaryPartText(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		for _, key := range []string{"text", "summary", "content"} {
			if text := strings.TrimSpace(asString(v[key])); text != "" {
				return text
			}
		}
	}
	return ""
}

func codexReasoningSummaryKey(turnID, itemID string) string {
	turnID = strings.TrimSpace(turnID)
	itemID = strings.TrimSpace(itemID)
	if turnID == "" {
		return "codex-reasoning:" + itemID
	}
	return "codex-reasoning:" + turnID + ":" + itemID
}

func (s *codexReasoningSummaryState) appendDelta(summaryIndex int, delta string) {
	if summaryIndex < 0 {
		summaryIndex = 0
	}
	s.ensurePart(summaryIndex)
	s.parts[summaryIndex] += delta
}

func (s *codexReasoningSummaryState) ensurePart(summaryIndex int) {
	if summaryIndex < 0 {
		summaryIndex = 0
	}
	for len(s.parts) <= summaryIndex {
		s.parts = append(s.parts, "")
	}
}

func (s *codexReasoningSummaryState) setCompletedSummary(summary string) {
	parts := strings.Split(strings.ReplaceAll(summary, "\r\n", "\n"), "\n\n")
	s.parts = make([]string, 0, len(parts))
	for _, part := range parts {
		text := strings.TrimSpace(part)
		if text == "" {
			continue
		}
		s.parts = append(s.parts, text)
	}
}

func (s *codexReasoningSummaryState) content() string {
	parts := make([]string, 0, len(s.parts))
	for i := range s.parts {
		if text := strings.TrimSpace(s.parts[i]); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
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
			return &codexRPCMethodError{
				method:  method,
				code:    response.message.Error.Code,
				message: response.message.Error.Message,
			}
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
	trimmed := strings.TrimSpace(turnID)
	if trimmed != "" && trimmed != s.activeTurnID {
		s.assistantBuffer.Reset()
		s.assistantEmitted = ""
		s.assistantLastFlushed = ""
		s.assistantTurnID = trimmed
	}
	s.activeTurnID = trimmed
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

func (s *codexAppSession) emitCompactionEvent(status, trigger, message string) {
	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		return
	}
	trigger = strings.TrimSpace(trigger)
	message = strings.TrimSpace(message)
	s.mu.Lock()
	currentStatus := strings.TrimSpace(strings.ToLower(s.compactionStatus))
	contextID := strings.TrimSpace(s.compactionID)
	currentEventID := strings.TrimSpace(s.compactionEventID)
	if currentStatus == status && currentEventID == contextID {
		s.mu.Unlock()
		return
	}
	if (currentStatus == "completed" || currentStatus == "failed") &&
		(status == "completed" || status == "failed") {
		s.mu.Unlock()
		return
	}
	s.compactionStatus = status
	s.compactionEventID = contextID
	s.mu.Unlock()
	meta := s.runtimeMeta("active")
	meta.Source = "compact"
	meta.Target = "compact"
	meta.TargetType = "compact"
	meta.TargetText = trigger
	if contextID != "" {
		meta.ContextID = contextID
		meta.ContextTitle = "Context compaction"
	}
	if status == "completed" || status == "failed" {
		meta.ClaudeLifecycle = "waiting_input"
	}
	sendEvent(s.sink, protocol.ApplyRuntimeMeta(
		protocol.NewCompactionEvent(s.sessionID, contextID, status, trigger, message),
		meta,
	))
}

func (s *codexAppSession) setCompactionID(compactionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.compactionID = strings.TrimSpace(compactionID)
}

func (s *codexAppSession) ensureCompactionID(fallback string) {
	fallback = strings.TrimSpace(fallback)
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(s.compactionID) != "" {
		return
	}
	if fallback != "" {
		s.compactionID = fallback
		return
	}
	s.compactionID = fmt.Sprintf("compaction:%d", time.Now().UTC().UnixNano())
}

func (s *codexAppSession) setCompactionTurnID(turnID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.compactionTurnID = strings.TrimSpace(turnID)
}

func (s *codexAppSession) clearCompactionTurnID(turnID string) {
	normalized := strings.TrimSpace(turnID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if normalized == "" || normalized == strings.TrimSpace(s.compactionTurnID) {
		s.compactionTurnID = ""
	}
}

func (s *codexAppSession) resetCompactionLifecycle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.compactionID = ""
	s.compactionTurnID = ""
	s.compactionStatus = ""
	s.compactionEventID = ""
}

func (s *codexAppSession) isCompactionTurn(turnID string) bool {
	normalized := strings.TrimSpace(turnID)
	if normalized == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.compactionTurnID) == normalized
}

func (s *codexAppSession) HasActiveTurn() bool {
	return strings.TrimSpace(s.activeTurn()) != ""
}

func (s *codexAppSession) threadAndTurn() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.threadID, s.activeTurnID
}

func (s *codexAppSession) runtimeMeta(lifecycle string) protocol.RuntimeMeta {
	threadID, turnID := s.threadAndTurn()
	meta := protocol.RuntimeMeta{
		ResumeSessionID: threadID,
		ExecutionID:     turnID,
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

func (s *codexAppSession) assistantRuntimeMeta(lifecycle string, turnID string) protocol.RuntimeMeta {
	meta := s.runtimeMeta(lifecycle)
	if trimmed := strings.TrimSpace(turnID); trimmed != "" {
		meta.ExecutionID = trimmed
		return meta
	}
	if strings.TrimSpace(meta.ExecutionID) != "" {
		return meta
	}
	s.mu.Lock()
	meta.ExecutionID = strings.TrimSpace(s.assistantTurnID)
	s.mu.Unlock()
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
	s.assistantBuffer.WriteString(delta)
	emitted := s.drainAssistantChunksLocked(false)
	s.mu.Unlock()
	return emitted
}

func (s *codexAppSession) flushAssistantDelta() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drainAssistantChunksLocked(true)
}

func (s *codexAppSession) drainAssistantChunksLocked(flushAll bool) []string {
	emitted := codexDrainAssistantChunks(&s.assistantBuffer, s.assistantLastFlushed, flushAll)
	for _, chunk := range emitted {
		s.assistantLastFlushed += chunk
	}
	if flushAll {
		s.assistantLastFlushed = ""
	}
	return emitted
}

func codexDrainAssistantChunks(buffer *strings.Builder, lastFlushed string, flushAll bool) []string {
	text := buffer.String()
	if text == "" {
		return nil
	}

	base := strings.TrimSpace(lastFlushed)
	fullText := strings.TrimSpace(base + text)
	if fullText == "" {
		buffer.Reset()
		return nil
	}
	if !flushAll && !strings.Contains(text, "\n") && !shouldFlushCodexAssistantChunk(fullText, base) {
		return nil
	}

	var emitted []string
	for {
		idx := strings.IndexByte(text, '\n')
		if idx < 0 {
			break
		}
		chunk := text[:idx+1]
		if strings.TrimSpace(chunk) != "" {
			emitted = append(emitted, chunk)
		}
		text = text[idx+1:]
	}

	trimmed := strings.TrimSpace(text)
	if trimmed != "" {
		runeCount := len([]rune(trimmed))
		if flushAll ||
			runeCount >= codexAssistantMinChunkRunes ||
			endsWithLiveTailBoundary(trimmed) {
			emitted = append(emitted, text)
			text = ""
		}
	} else if flushAll {
		text = ""
	}

	buffer.Reset()
	if text != "" {
		buffer.WriteString(text)
	}
	return emitted
}

func shouldFlushCodexAssistantChunk(fullText string, base string) bool {
	if base == "" {
		return len([]rune(fullText)) >= codexAssistantInitialChunkRunes ||
			endsWithLiveTailBoundary(fullText)
	}
	if !strings.HasPrefix(fullText, base) {
		return true
	}
	delta := strings.TrimSpace(strings.TrimPrefix(fullText, base))
	if delta == "" {
		return false
	}
	return len([]rune(delta)) >= codexAssistantMinChunkRunes ||
		endsWithLiveTailBoundary(delta)
}

func codexAssistantDedupeText(text string) string {
	var builder strings.Builder
	for _, r := range strings.TrimSpace(text) {
		if !unicode.IsSpace(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func trimCodexCompletedPrefix(text string, emitted string) (string, bool) {
	target := codexAssistantDedupeText(emitted)
	if target == "" {
		return "", false
	}
	var consumed strings.Builder
	for index, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		consumed.WriteRune(r)
		current := consumed.String()
		if current == target {
			return text[index+len(string(r)):], true
		}
		if !strings.HasPrefix(target, current) {
			return "", false
		}
	}
	return "", consumed.String() == target
}

func codexApprovalPolicy(permissionMode string, defaults codexConfigDefaults) string {
	// Codex 默认必须走 on-request，避免文件修改或命令执行在未显式授权时直接放行。
	// 只有用户显式配置 bypassPermissions 时，才允许完全跳过审批。
	// 如果线上看起来“Codex 文件修改不需要授权”，更可能是该改动没有走
	// item/fileChange/requestApproval，而是直接以 turn diff 的形式下发，而不是这里默认开了绿灯。
	switch strings.TrimSpace(permissionMode) {
	case "bypassPermissions":
		return "never"
	case "config":
		if policy := strings.TrimSpace(defaults.approvalPolicy); policy != "" {
			return policy
		}
		return "on-request"
	default:
		return "on-request"
	}
}

func normalizeCodexSandboxMode(value string, defaults codexConfigDefaults) (string, error) {
	switch strings.TrimSpace(value) {
	case "", "workspace-write":
		return "workspace-write", nil
	case "read-only":
		return "read-only", nil
	case "danger-full-access":
		return "danger-full-access", nil
	case "config":
		if mode := strings.TrimSpace(defaults.sandboxMode); mode != "" {
			return normalizeCodexSandboxMode(mode, codexConfigDefaults{})
		}
		return "workspace-write", nil
	default:
		return "", fmt.Errorf("invalid Codex sandbox mode: %s", value)
	}
}

func codexFileApprovalDecision(permissionMode, decision string) string {
	if decision == "deny" {
		return "decline"
	}
	if strings.TrimSpace(permissionMode) == "auto" {
		return "acceptForSession"
	}
	return "accept"
}

func codexCommandApprovalDecision(permissionMode, decision string) any {
	if decision == "deny" {
		return "decline"
	}
	if strings.TrimSpace(permissionMode) == "auto" {
		return "acceptForSession"
	}
	return "accept"
}

func codexPermissionsApprovalResult(permissionMode, decision string, requested json.RawMessage) map[string]any {
	scope := "turn"
	if strings.TrimSpace(permissionMode) == "auto" {
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

func codexHookStatusLabel(eventName string, status string) string {
	eventName = strings.TrimSpace(eventName)
	if eventName == "" {
		eventName = "hook"
	}
	if status == "done" {
		return "Hook completed: " + eventName
	}
	return "Running hook: " + eventName
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
