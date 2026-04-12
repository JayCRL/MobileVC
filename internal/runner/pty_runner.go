package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"

	"mobilevc/internal/adapter"
	"mobilevc/internal/logx"
	"mobilevc/internal/protocol"
	"mobilevc/internal/session"
)

const (
	ptyReadBufferSize       = 4096
	ptyDebugPreviewLimit    = 240
	ptyDebugReasonLog       = "log"
	ptyDebugReasonPrompt    = "prompt_request"
	ptyDebugReasonError     = "error"
	ptyErrNoActiveRunner    = "no active runner"
	ptyErrNotInteractive    = "runner is not ready for interactive input"
	textPermissionRequestID = "__text_permission_prompt__"
)

type PtyRunner struct {
	mu                      sync.Mutex
	runCtx                  context.Context
	writer                  io.WriteCloser
	closer                  io.Closer
	cmd                     *exec.Cmd
	closed                  bool
	suppressExitError       bool
	lazyStart               bool
	interactive             bool
	awaitingReadyPrompt     bool
	processDone             chan struct{}
	processErr              error
	pendingReq              ExecRequest
	pendingCWD              string
	currentDir              string
	sink                    EventSink
	claudeSessionID         string
	codexSession            *codexAppSession
	permissionMode          string
	pendingControlRequestID string
	pendingPromptOptions    []string
	lastToolName            string
	lastToolTarget          string
	fileSnapshots           map[string]fileSnapshot
	catalogAuthoringBuffer  strings.Builder
	lastAssistantTextKey    string
	runGeneration           uint64
}

type fileSnapshot struct {
	exists  bool
	content string
}

type claudeShellOnceWriter struct {
	runner *PtyRunner
}

func (w *claudeShellOnceWriter) Write(data []byte) (int, error) {
	if w.runner == nil {
		return 0, errors.New("no runner")
	}
	w.runner.mu.Lock()
	req := w.runner.pendingReq
	cwd := w.runner.pendingCWD
	sink := w.runner.sink
	permMode := w.runner.permissionMode
	resumeSessionID := w.runner.claudeSessionID
	w.runner.mu.Unlock()
	logx.Info("pty", "lazy shell writer received input: sessionID=%s cwd=%q permissionMode=%q resumeSessionID=%q preview=%q", req.SessionID, cwd, permMode, resumeSessionID, ptyDebugPreview(string(data)))
	if shouldUseCodexAppServer(req.Command) {
		if err := w.runner.startCodexAppServerOnFirstInput(context.Background(), req, cwd, sink, data); err != nil {
			return 0, err
		}
		return len(data), nil
	}
	if err := w.runner.startClaudeStreamOnFirstInput(context.Background(), req, cwd, sink, data); err != nil {
		return 0, err
	}
	return len(data), nil
}

func (w *claudeShellOnceWriter) Close() error {
	return nil
}

type interactiveSession struct {
	stdout io.Reader
	stderr io.Reader
	writer io.WriteCloser
	closer io.Closer
}

func NewPtyRunner() *PtyRunner {
	return &PtyRunner{}
}

func (r *PtyRunner) Run(ctx context.Context, req ExecRequest, sink EventSink) error {
	if req.SessionID == "" {
		return errors.New("session id is required")
	}
	if req.Command == "" {
		return errors.New("command is required")
	}

	cwd := req.CWD
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
	}

	if shouldUseClaudeResumeInteractive(req.Command) {
		r.mu.Lock()
		r.runCtx = ctx
		r.permissionMode = req.PermissionMode
		r.interactive = false
		r.mu.Unlock()
		return r.runClaudeResumeInteractive(ctx, req, cwd, sink)
	}

	if shouldUseCodexAppServer(req.Command) {
		initialPrompt := extractCodexInitialPrompt(req.Command)
		if initialPrompt != "" {
			generation := r.nextRunGeneration()
			r.mu.Lock()
			r.runCtx = ctx
			r.permissionMode = req.PermissionMode
			r.interactive = false
			r.runGeneration = generation
			r.pendingReq = req
			r.pendingCWD = cwd
			r.currentDir = cwd
			r.sink = sink
			r.closed = false
			r.processDone = make(chan struct{})
			r.processErr = nil
			r.lazyStart = false
			r.mu.Unlock()
			return r.runCodexAppServer(ctx, req, cwd, sink, initialPrompt)
		}
		generation := r.nextRunGeneration()
		r.mu.Lock()
		r.runCtx = ctx
		r.lazyStart = true
		r.interactive = false
		r.awaitingReadyPrompt = true
		r.pendingReq = req
		r.pendingCWD = cwd
		r.currentDir = cwd
		r.sink = sink
		r.closed = false
		r.processDone = make(chan struct{})
		r.processErr = nil
		r.permissionMode = req.PermissionMode
		r.runGeneration = generation
		r.mu.Unlock()
		defer r.clearGeneration(generation)

		r.mu.Lock()
		r.writer = &claudeShellOnceWriter{runner: r}
		r.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.processDone:
			r.mu.Lock()
			err := r.processErr
			r.mu.Unlock()
			return err
		}
	}

	if shouldUseClaudeStreamJSON(req.Command) {
		if extractResumeArg(req.Command) != "" && strings.Contains(strings.ToLower(req.Command), " --print") {
			generation := r.nextRunGeneration()
			r.mu.Lock()
			r.runCtx = ctx
			r.permissionMode = req.PermissionMode
			r.interactive = false
			r.pendingReq = req
			r.pendingCWD = cwd
			r.currentDir = cwd
			r.sink = sink
			r.closed = false
			r.processDone = make(chan struct{})
			r.processErr = nil
			r.lazyStart = false
			r.runGeneration = generation
			r.mu.Unlock()
			return r.runClaudeStream(ctx, req, cwd, sink)
		}
		generation := r.nextRunGeneration()
		r.mu.Lock()
		r.runCtx = ctx
		r.lazyStart = true
		r.interactive = false
		r.awaitingReadyPrompt = true
		r.pendingReq = req
		r.pendingCWD = cwd
		r.currentDir = cwd
		r.sink = sink
		r.closed = false
		r.processDone = make(chan struct{})
		r.processErr = nil
		r.permissionMode = req.PermissionMode
		r.runGeneration = generation
		r.mu.Unlock()
		defer r.clearGeneration(generation)

		r.mu.Lock()
		r.writer = &claudeShellOnceWriter{runner: r}
		r.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.processDone:
			r.mu.Lock()
			err := r.processErr
			r.mu.Unlock()
			return err
		}
	}

	generation := r.nextRunGeneration()
	cmd := newShellCommand(ctx, req.Command, req.Mode)
	cmd.Dir = cwd

	interactive, err := startInteractiveCommand(cmd)
	if err != nil {
		sendEvent(sink, protocol.NewErrorEvent(req.SessionID, fmt.Sprintf("start pty command: %v", err), ""))
		return fmt.Errorf("start pty command: %w", err)
	}
	defer interactive.closer.Close()

	r.mu.Lock()
	r.runCtx = ctx
	r.writer = interactive.writer
	r.closer = interactive.closer
	r.cmd = cmd
	r.currentDir = cwd
	r.closed = false
	r.runGeneration = generation
	r.mu.Unlock()
	defer r.clearGeneration(generation)

	sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateActive), "command started"))

	var readWG sync.WaitGroup
	readWG.Add(1)
	go func() {
		defer readWG.Done()
		r.readOutput(ctx, interactive.stdout, req.SessionID, "stdout", true, sink)
	}()
	if interactive.stderr != nil {
		readWG.Add(1)
		go func() {
			defer readWG.Done()
			r.readOutput(ctx, interactive.stderr, req.SessionID, "stderr", false, sink)
		}()
	}

	waitErr := cmd.Wait()
	_ = interactive.closer.Close()
	readWG.Wait()

	if waitErr != nil {
		if r.shouldSuppressExitError() {
			sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished"))
			return nil
		}
		message := waitErr.Error()
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			message = fmt.Sprintf("command exited with code %d", exitErr.ExitCode())
		}
		sendEvent(sink, protocol.NewErrorEvent(req.SessionID, message, ""))
		sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished with error"))
		return waitErr
	}

	sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished"))
	return nil
}

func (r *PtyRunner) Write(ctx context.Context, data []byte) error {
	if len(data) == 0 {
		return errors.New("input data is required")
	}

	for {
		r.mu.Lock()
		lazyStart := r.lazyStart
		interactive := r.interactive
		awaitingReadyPrompt := r.awaitingReadyPrompt
		writer := r.writer
		closed := r.closed
		req := r.pendingReq
		cwd := r.pendingCWD
		sink := r.sink
		permissionMode := r.permissionMode
		resumeSessionID := r.claudeSessionID
		r.mu.Unlock()
		logx.Info("pty", "runner write requested: sessionID=%s lazyStart=%t interactive=%t awaitingReadyPrompt=%t closed=%t cwd=%q permissionMode=%q resumeSessionID=%q preview=%q", req.SessionID, lazyStart, interactive, awaitingReadyPrompt, closed, cwd, permissionMode, resumeSessionID, ptyDebugPreview(string(data)))

		if lazyStart && !interactive {
			if shouldUseCodexAppServer(req.Command) {
				return r.startCodexAppServerOnFirstInput(ctx, req, cwd, sink, data)
			}
			return r.startClaudeStreamOnFirstInput(ctx, req, cwd, sink, data)
		}

		if awaitingReadyPrompt && !interactive {
			if err := r.waitForInteractiveReady(ctx); err != nil {
				return err
			}
			continue
		}

		if writer == nil || closed {
			return errors.New("no active pty session")
		}

		if shouldUseCodexAppServer(req.Command) {
			writeDone := make(chan error, 1)
			go func() {
				_, err := writer.Write(data)
				writeDone <- err
			}()

			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-writeDone:
				if err != nil {
					return fmt.Errorf("write codex input: %w", err)
				}
				return nil
			}
		}

		// 关键修复：对于交互式 AI 工具，确保发送 \r\n 触发执行
		finalData := data
		convertedNewline := false
		if isAICommandName(req.Command) && len(data) > 0 && data[len(data)-1] == '\n' {
			if len(data) == 1 || data[len(data)-2] != '\r' {
				finalData = append(data[:len(data)-1], '\r', '\n')
				convertedNewline = true
			}
		}
		logx.Info("pty", "runner writing to active session: sessionID=%s convertedNewline=%t preview=%q finalPreview=%q", req.SessionID, convertedNewline, ptyDebugPreview(string(data)), ptyDebugPreview(string(finalData)))

		writeDone := make(chan error, 1)
		go func() {
			_, err := writer.Write(finalData)
			writeDone <- err
		}()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-writeDone:
			if err != nil {
				return fmt.Errorf("write pty input: %w", err)
			}
			return nil
		}
	}

	return errors.New("no active pty session")
}

func (r *PtyRunner) CanAcceptInteractiveInput() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.interactive && r.writer != nil && !r.closed
}

func (r *PtyRunner) waitForInteractiveReady(ctx context.Context) error {
	deadlineCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		r.mu.Lock()
		interactive := r.interactive && r.writer != nil && !r.closed
		closed := r.closed
		awaitingReadyPrompt := r.awaitingReadyPrompt
		r.mu.Unlock()
		if interactive {
			return nil
		}
		if closed {
			return errors.New(ptyErrNoActiveRunner)
		}
		if !awaitingReadyPrompt {
			return errors.New(ptyErrNotInteractive)
		}
		select {
		case <-deadlineCtx.Done():
			r.mu.Lock()
			interactive = r.interactive && r.writer != nil && !r.closed
			closed = r.closed
			awaitingReadyPrompt = r.awaitingReadyPrompt
			r.mu.Unlock()
			if interactive {
				return nil
			}
			if closed {
				return errors.New(ptyErrNoActiveRunner)
			}
			if !awaitingReadyPrompt {
				return errors.New(ptyErrNotInteractive)
			}
			return errors.New(ptyErrNotInteractive)
		case <-ticker.C:
		}
	}
}

func (r *PtyRunner) HasPendingPermissionRequest() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.codexSession != nil {
		return r.codexSession.HasPendingPermissionRequest()
	}
	return strings.TrimSpace(r.pendingControlRequestID) != ""
}

func (r *PtyRunner) ClaudeSessionID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sessionID := extractClaudeSessionIDArg(r.pendingReq.Command); sessionID != "" {
		return sessionID
	}
	if sessionID := strings.TrimSpace(r.claudeSessionID); sessionID != "" {
		return sessionID
	}
	return ""
}

func (r *PtyRunner) ProcessRef() ProcessRef {
	r.mu.Lock()
	defer r.mu.Unlock()
	ref := ProcessRef{
		ExecutionID: strings.TrimSpace(r.pendingReq.RuntimeMeta.ExecutionID),
		Command:     strings.TrimSpace(r.pendingReq.Command),
		CWD:         strings.TrimSpace(r.currentDir),
		Source:      "pty",
	}
	if ref.Command == "" && r.cmd != nil {
		ref.Command = strings.Join(r.cmd.Args, " ")
	}
	if shouldUseCodexAppServer(ref.Command) {
		ref.Source = "codex"
	} else if isClaudeCommandName(ref.Command) {
		ref.Source = "claude"
	}
	if r.cmd != nil && r.cmd.Process != nil {
		ref.RootPID = r.cmd.Process.Pid
	}
	return ref
}

func (r *PtyRunner) WritePermissionResponse(ctx context.Context, decision string) error {
	behavior := ""
	textDecision := ""
	switch strings.TrimSpace(strings.ToLower(decision)) {
	case "approve":
		behavior = "allow"
		textDecision = "y"
	case "deny":
		behavior = "deny"
		textDecision = "n"
	default:
		return errors.New("permission decision must be one of: approve, deny")
	}

	r.mu.Lock()
	codexSession := r.codexSession
	writer := r.writer
	closed := r.closed
	requestID := strings.TrimSpace(r.pendingControlRequestID)
	promptOptions := append([]string(nil), r.pendingPromptOptions...)
	req := r.pendingReq
	r.mu.Unlock()

	if codexSession != nil {
		if closed {
			return errors.New("no active pty session")
		}
		return codexSession.WritePermissionResponse(ctx, decision)
	}

	if writer == nil || closed {
		return errors.New("no active pty session")
	}
	if requestID == "" {
		return ErrNoPendingControlRequest
	}

	if requestID == textPermissionRequestID {
		token := resolveTextPermissionDecisionToken(decision, promptOptions)
		if token == "" {
			token = textDecision
		}
		writePayload := []byte(token + "\n")
		writeDone := make(chan error, 1)
		go func() {
			_, err := writer.Write(writePayload)
			writeDone <- err
		}()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-writeDone:
			if err != nil {
				return fmt.Errorf("write text permission response: %w", err)
			}
		}
		r.mu.Lock()
		if r.pendingControlRequestID == requestID {
			r.pendingControlRequestID = ""
			r.pendingPromptOptions = nil
		}
		r.mu.Unlock()
		return nil
	}

	payload := map[string]any{
		"type":       "control_response",
		"request_id": requestID,
		"response": map[string]any{
			"behavior": behavior,
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal control response: %w", err)
	}
	encoded = append(encoded, '\n')
	logx.Info("pty", "runner writing control response: sessionID=%s requestID=%q behavior=%q preview=%q", req.SessionID, requestID, behavior, ptyDebugPreview(string(encoded)))

	writeDone := make(chan error, 1)
	go func() {
		_, err := writer.Write(encoded)
		writeDone <- err
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-writeDone:
		if err != nil {
			return fmt.Errorf("write control response: %w", err)
		}
	}

	r.mu.Lock()
	if r.pendingControlRequestID == requestID {
		r.pendingControlRequestID = ""
		r.pendingPromptOptions = nil
	}
	r.mu.Unlock()
	return nil
}

func (r *PtyRunner) Close() error {
	r.mu.Lock()
	closer := r.closer
	cmd := r.cmd
	codexSession := r.codexSession
	r.closed = true
	r.suppressExitError = true
	r.mu.Unlock()

	if codexSession != nil {
		_ = codexSession.Close()
	}
	if closer != nil {
		_ = closer.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	return nil
}

func (r *PtyRunner) markInteractiveReady() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.awaitingReadyPrompt = false
	r.interactive = true
}

func (r *PtyRunner) SetPermissionMode(mode string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.permissionMode = mode
}

func (r *PtyRunner) currentPermissionMode() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.permissionMode
}

func (r *PtyRunner) nextRunGeneration() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runGeneration++
	return r.runGeneration
}

func (r *PtyRunner) clearGeneration(generation uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if generation != 0 && r.runGeneration != generation {
		return
	}
	r.runCtx = nil
	r.writer = nil
	r.closer = nil
	r.cmd = nil
	r.codexSession = nil
	r.currentDir = ""
	r.lastToolName = ""
	r.lastToolTarget = ""
	r.fileSnapshots = nil
	r.catalogAuthoringBuffer.Reset()
	r.lastAssistantTextKey = ""
	r.lazyStart = false
	r.interactive = false
	r.awaitingReadyPrompt = false
	r.pendingControlRequestID = ""
	r.pendingPromptOptions = nil
	r.closed = true
	r.suppressExitError = false
}

func (r *PtyRunner) clear() {
	r.clearGeneration(0)
}

func (r *PtyRunner) commandContext() context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runCtx != nil {
		return r.runCtx
	}
	return context.Background()
}

func (r *PtyRunner) runClaudeStream(ctx context.Context, req ExecRequest, cwd string, sink EventSink) error {
	generation := r.nextRunGeneration()
	cmd := newClaudeStreamCommand(ctx, req.Command, r.claudeSessionID, r.permissionMode)
	logx.Info("pty", "run claude stream: sessionID=%s cwd=%q permissionMode=%q resumeSessionID=%q commandPreview=%q", req.SessionID, cwd, r.permissionMode, r.claudeSessionID, ptyDebugPreview(req.Command))
	cmd.Dir = cwd

	stdin, err := cmd.StdinPipe()
	if err != nil {
		sendEvent(sink, protocol.NewErrorEvent(req.SessionID, fmt.Sprintf("create claude stdin pipe: %v", err), ""))
		return fmt.Errorf("create claude stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		sendEvent(sink, protocol.NewErrorEvent(req.SessionID, fmt.Sprintf("create claude stdout pipe: %v", err), ""))
		return fmt.Errorf("create claude stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		sendEvent(sink, protocol.NewErrorEvent(req.SessionID, fmt.Sprintf("create claude stderr pipe: %v", err), ""))
		return fmt.Errorf("create claude stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		sendEvent(sink, protocol.NewErrorEvent(req.SessionID, fmt.Sprintf("start claude stream command: %v", err), ""))
		logx.Error("pty", "start claude stream command failed: sessionID=%s cwd=%q permissionMode=%q resumeSessionID=%q err=%v", req.SessionID, cwd, r.permissionMode, r.claudeSessionID, err)
		return fmt.Errorf("start claude stream command: %w", err)
	}
	logx.Info("pty", "started claude stream command: sessionID=%s cwd=%q permissionMode=%q resumeSessionID=%q", req.SessionID, cwd, r.permissionMode, r.claudeSessionID)

	r.mu.Lock()
	r.writer = &claudeStreamWriter{writer: stdin}
	r.closer = stdin
	r.cmd = cmd
	r.currentDir = cwd
	r.lazyStart = false
	r.interactive = true
	r.awaitingReadyPrompt = false
	r.closed = false
	r.runGeneration = generation
	r.mu.Unlock()
	defer r.clearGeneration(generation)
	defer func() {
		r.mu.Lock()
		current := r.runGeneration
		r.mu.Unlock()
		if current == generation {
			_ = stdin.Close()
		}
	}()

	sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateActive), "command started"))

	var readWG sync.WaitGroup
	readWG.Add(2)
	go func() {
		defer readWG.Done()
		r.readClaudeStreamJSON(ctx, stdout, req.SessionID, sink)
	}()
	go func() {
		defer readWG.Done()
		r.readOutput(ctx, stderr, req.SessionID, "stderr", false, sink)
	}()

	waitErr := cmd.Wait()
	readWG.Wait()

	if waitErr != nil {
		if r.shouldSuppressExitError() {
			sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished"))
			return nil
		}
		message := waitErr.Error()
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			message = fmt.Sprintf("command exited with code %d", exitErr.ExitCode())
		}
		sendEvent(sink, protocol.NewErrorEvent(req.SessionID, message, ""))
		sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished with error"))
		return waitErr
	}

	sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished"))
	return nil
}

func (r *PtyRunner) runCodexAppServer(ctx context.Context, req ExecRequest, cwd string, sink EventSink, initialPrompt string) error {
	generation := r.nextRunGeneration()
	resumeSessionID := extractResumeArg(req.Command)
	if resumeSessionID == "" {
		resumeSessionID = strings.TrimSpace(req.RuntimeMeta.ResumeSessionID)
	}

	app, err := newCodexAppSession(ctx, ctx, r, req, cwd, sink, resumeSessionID)
	if err != nil {
		sendEvent(sink, protocol.NewErrorEvent(req.SessionID, err.Error(), ""))
		return err
	}

	r.mu.Lock()
	r.codexSession = app
	r.writer = &codexAppWriter{session: app}
	r.closer = app.stdin
	r.cmd = app.cmd
	r.currentDir = cwd
	r.closed = false
	r.lazyStart = false
	r.interactive = true
	r.awaitingReadyPrompt = false
	r.runGeneration = generation
	r.mu.Unlock()
	defer r.clearGeneration(generation)

	sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateActive), "command started"))

	if strings.TrimSpace(initialPrompt) != "" {
		if err := app.SendUserInput(ctx, []byte(initialPrompt)); err != nil {
			_ = app.Close()
			sendEvent(sink, protocol.NewErrorEvent(req.SessionID, err.Error(), ""))
			return err
		}
	}

	waitErr := app.cmd.Wait()
	if waitErr != nil {
		if r.shouldSuppressExitError() {
			sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished"))
			return nil
		}
		message := waitErr.Error()
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			message = fmt.Sprintf("command exited with code %d", exitErr.ExitCode())
		}
		sendEvent(sink, protocol.NewErrorEvent(req.SessionID, message, ""))
		sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished with error"))
		return waitErr
	}

	sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished"))
	return nil
}

func (r *PtyRunner) runClaudeResumeInteractive(ctx context.Context, req ExecRequest, cwd string, sink EventSink) error {
	generation := r.nextRunGeneration()
	command := appendPermissionModeToCommand(req.Command, req.PermissionMode)
	cmd := newShellCommand(ctx, command, req.Mode)
	cmd.Dir = cwd
	logx.Info("pty", "run claude resume interactive: sessionID=%s cwd=%q permissionMode=%q resumeSessionID=%q commandPreview=%q", req.SessionID, cwd, r.permissionMode, r.claudeSessionID, ptyDebugPreview(command))

	interactive, err := startInteractiveCommand(cmd)
	if err != nil {
		sendEvent(sink, protocol.NewErrorEvent(req.SessionID, fmt.Sprintf("start claude resume interactive command: %v", err), ""))
		return fmt.Errorf("start claude resume interactive command: %w", err)
	}
	defer interactive.closer.Close()

	r.mu.Lock()
	r.writer = interactive.writer
	r.closer = interactive.closer
	r.cmd = cmd
	r.pendingReq = req
	r.pendingCWD = cwd
	r.currentDir = cwd
	r.lazyStart = false
	r.interactive = false
	r.awaitingReadyPrompt = true
	r.closed = false
	r.suppressExitError = false
	r.runGeneration = generation
	r.mu.Unlock()
	defer r.clearGeneration(generation)
	defer func() {
		r.mu.Lock()
		current := r.runGeneration
		r.mu.Unlock()
		if current == generation {
			_ = interactive.closer.Close()
		}
	}()

	sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateActive), "command started"))

	var readWG sync.WaitGroup
	readWG.Add(1)
	go func() {
		defer readWG.Done()
		r.readOutput(ctx, interactive.stdout, req.SessionID, "stdout", true, sink)
	}()
	if interactive.stderr != nil {
		readWG.Add(1)
		go func() {
			defer readWG.Done()
			r.readOutput(ctx, interactive.stderr, req.SessionID, "stderr", false, sink)
		}()
	}

	waitErr := cmd.Wait()
	_ = interactive.closer.Close()
	readWG.Wait()

	if waitErr != nil {
		if r.shouldSuppressExitError() {
			sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished"))
			return nil
		}
		message := waitErr.Error()
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			message = fmt.Sprintf("command exited with code %d", exitErr.ExitCode())
		}
		sendEvent(sink, protocol.NewErrorEvent(req.SessionID, message, ""))
		sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished with error"))
		return waitErr
	}

	sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished"))
	return nil
}

func (r *PtyRunner) shouldSuppressExitError() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.suppressExitError
}

func (r *PtyRunner) startClaudeStreamOnFirstInput(ctx context.Context, req ExecRequest, cwd string, sink EventSink, firstInput []byte) error {
	r.mu.Lock()
	if !r.lazyStart {
		writer := r.writer
		closed := r.closed
		interactive := r.interactive
		permissionMode := r.permissionMode
		resumeSessionID := r.claudeSessionID
		r.mu.Unlock()
		logx.Info("pty", "startClaudeStreamOnFirstInput reuse existing writer: sessionID=%s interactive=%t closed=%t permissionMode=%q resumeSessionID=%q preview=%q", req.SessionID, interactive, closed, permissionMode, resumeSessionID, ptyDebugPreview(string(firstInput)))
		if !interactive || writer == nil || closed {
			return errors.New("runner is not ready for interactive input")
		}
		_, err := writer.Write(firstInput)
		return err
	}
	text := strings.TrimSpace(string(firstInput))
	if text == "" {
		r.mu.Unlock()
		return nil
	}
	r.lazyStart = false
	r.mu.Unlock()

	r.mu.Lock()
	resumeSessionID := r.claudeSessionID
	if resumeSessionID == "" {
		resumeSessionID = extractResumeArg(req.Command)
	}
	if resumeSessionID == "" && !hasClaudeSessionIDArg(req.Command) {
		resumeSessionID = strings.TrimSpace(req.RuntimeMeta.ResumeSessionID)
	}
	permMode := r.permissionMode
	r.mu.Unlock()
	logx.Info("pty", "starting claude stream on first input: sessionID=%s cwd=%q permissionMode=%q resumeSessionID=%q preview=%q", req.SessionID, cwd, permMode, resumeSessionID, ptyDebugPreview(text))

	lowerCommand := strings.ToLower(strings.TrimSpace(req.Command))
	if strings.Contains(lowerCommand, "--input-format") || strings.Contains(lowerCommand, "stream-json") || strings.Contains(lowerCommand, "--permission-prompt-tool") {
		generation := r.nextRunGeneration()
		cmd := newClaudeStreamCommand(r.commandContext(), req.Command, resumeSessionID, permMode)
		cmd.Dir = cwd

		stdin, err := cmd.StdinPipe()
		if err != nil {
			r.finishLazyProcess(err, sink, req.SessionID)
			return fmt.Errorf("create claude stdin pipe: %w", err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			_ = stdin.Close()
			r.finishLazyProcess(err, sink, req.SessionID)
			return fmt.Errorf("create claude stdout pipe: %w", err)
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			_ = stdin.Close()
			r.finishLazyProcess(err, sink, req.SessionID)
			return fmt.Errorf("create claude stderr pipe: %w", err)
		}
		if err := cmd.Start(); err != nil {
			_ = stdin.Close()
			r.finishLazyProcess(err, sink, req.SessionID)
			logx.Error("pty", "start claude stream command failed from first input: sessionID=%s cwd=%q permissionMode=%q resumeSessionID=%q err=%v", req.SessionID, cwd, permMode, resumeSessionID, err)
			return fmt.Errorf("start claude stream command: %w", err)
		}
		logx.Info("pty", "started claude stream command from first input: sessionID=%s cwd=%q permissionMode=%q resumeSessionID=%q", req.SessionID, cwd, permMode, resumeSessionID)
		sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateActive), "command started"))

		streamWriter := &claudeStreamWriter{writer: stdin}
		r.mu.Lock()
		r.cmd = cmd
		r.currentDir = cwd
		r.closed = false
		r.interactive = true
		r.awaitingReadyPrompt = false
		r.writer = streamWriter
		r.closer = stdin
		r.pendingReq = req
		r.pendingCWD = cwd
		r.permissionMode = permMode
		r.runGeneration = generation
		r.mu.Unlock()

		go func() {
			var readWG sync.WaitGroup
			readWG.Add(2)
			go func() {
				defer readWG.Done()
				r.readClaudeStreamJSON(ctx, stdout, req.SessionID, sink)
			}()
			go func() {
				defer readWG.Done()
				r.readOutput(ctx, stderr, req.SessionID, "stderr", false, sink)
			}()
			logx.Info("pty", "waiting for claude stream command from first input: sessionID=%s", req.SessionID)
			waitErr := cmd.Wait()
			logx.Info("pty", "claude stream command from first input exited: sessionID=%s err=%v", req.SessionID, waitErr)
			readWG.Wait()
			if waitErr != nil {
				if r.shouldSuppressExitError() {
					r.finishLazyProcess(nil, sink, req.SessionID)
					return
				}
				message := waitErr.Error()
				var exitErr *exec.ExitError
				if errors.As(waitErr, &exitErr) {
					message = fmt.Sprintf("command exited with code %d", exitErr.ExitCode())
				}
				sendEvent(sink, protocol.NewErrorEvent(req.SessionID, message, ""))
				sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished with error"))
				r.finishLazyProcess(waitErr, sink, req.SessionID)
				return
			}
			sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished"))
			r.finishLazyProcess(nil, sink, req.SessionID)
		}()

		if _, err := streamWriter.Write(firstInput); err != nil {
			_ = stdin.Close()
			r.finishLazyProcess(err, sink, req.SessionID)
			return fmt.Errorf("write first claude stream input: %w", err)
		}
		return nil
	}

	generation := r.nextRunGeneration()
	cmd := newClaudePromptCommand(ctx, req.Command, text, resumeSessionID, permMode)
	cmd.Dir = cwd

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.finishLazyProcess(err, sink, req.SessionID)
		return fmt.Errorf("create claude stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		r.finishLazyProcess(err, sink, req.SessionID)
		return fmt.Errorf("create claude stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		r.finishLazyProcess(err, sink, req.SessionID)
		logx.Error("pty", "start claude prompt command failed: sessionID=%s cwd=%q permissionMode=%q resumeSessionID=%q err=%v", req.SessionID, cwd, permMode, resumeSessionID, err)
		return fmt.Errorf("start claude prompt command: %w", err)
	}
	logx.Info("pty", "started claude prompt command: sessionID=%s cwd=%q permissionMode=%q resumeSessionID=%q", req.SessionID, cwd, permMode, resumeSessionID)
	sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateActive), "command started"))

	r.mu.Lock()
	r.cmd = cmd
	r.currentDir = cwd
	r.closed = false
	r.interactive = false
	r.writer = nil
	r.runGeneration = generation
	r.mu.Unlock()

	go func() {
		var readWG sync.WaitGroup
		readWG.Add(2)
		go func() {
			defer readWG.Done()
			r.readClaudeStreamJSON(ctx, stdout, req.SessionID, sink)
		}()
		go func() {
			defer readWG.Done()
			r.readOutput(ctx, stderr, req.SessionID, "stderr", false, sink)
		}()
		waitErr := cmd.Wait()
		readWG.Wait()
		if waitErr != nil {
			message := waitErr.Error()
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) {
				message = fmt.Sprintf("command exited with code %d", exitErr.ExitCode())
			}
			sendEvent(sink, protocol.NewErrorEvent(req.SessionID, message, ""))
			sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished with error"))
			r.finishLazyProcess(waitErr, sink, req.SessionID)
			return
		}
		sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished"))
		r.finishLazyProcess(nil, sink, req.SessionID)
	}()

	return nil
}

func (r *PtyRunner) finishLazyProcess(err error, sink EventSink, sessionID string) {
	r.mu.Lock()
	r.processErr = err
	done := r.processDone
	r.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		default:
			close(done)
		}
	}
}

func (r *PtyRunner) startCodexAppServerOnFirstInput(ctx context.Context, req ExecRequest, cwd string, sink EventSink, firstInput []byte) error {
	r.mu.Lock()
	if !r.lazyStart {
		writer := r.writer
		closed := r.closed
		interactive := r.interactive
		r.mu.Unlock()
		if !interactive || writer == nil || closed {
			return errors.New("runner is not ready for interactive input")
		}
		_, err := writer.Write(firstInput)
		return err
	}
	text := strings.TrimSpace(string(firstInput))
	if text == "" {
		r.mu.Unlock()
		return nil
	}
	r.lazyStart = false
	r.mu.Unlock()

	r.mu.Lock()
	resumeSessionID := r.claudeSessionID
	if resumeSessionID == "" {
		resumeSessionID = extractResumeArg(req.Command)
	}
	if resumeSessionID == "" {
		resumeSessionID = strings.TrimSpace(req.RuntimeMeta.ResumeSessionID)
	}
	r.mu.Unlock()

	app, err := newCodexAppSession(r.commandContext(), ctx, r, req, cwd, sink, resumeSessionID)
	if err != nil {
		r.finishLazyProcess(err, sink, req.SessionID)
		return err
	}
	sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateActive), "command started"))

	writer := &codexAppWriter{session: app}
	r.mu.Lock()
	r.codexSession = app
	r.cmd = app.cmd
	r.currentDir = cwd
	r.closed = false
	r.interactive = true
	r.awaitingReadyPrompt = false
	r.writer = writer
	r.closer = app.stdin
	r.pendingReq = req
	r.pendingCWD = cwd
	r.mu.Unlock()

	go func() {
		waitErr := app.cmd.Wait()
		if waitErr != nil {
			if r.shouldSuppressExitError() {
				r.finishLazyProcess(nil, sink, req.SessionID)
				return
			}
			message := waitErr.Error()
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) {
				message = fmt.Sprintf("command exited with code %d", exitErr.ExitCode())
			}
			sendEvent(sink, protocol.NewErrorEvent(req.SessionID, message, ""))
			sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished with error"))
			r.finishLazyProcess(waitErr, sink, req.SessionID)
			return
		}
		sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished"))
		r.finishLazyProcess(nil, sink, req.SessionID)
	}()

	if err := app.SendUserInput(ctx, firstInput); err != nil {
		_ = app.Close()
		r.finishLazyProcess(err, sink, req.SessionID)
		return err
	}
	return nil
}

func shouldUseClaudeStreamJSON(command string) bool {
	return isClaudeCommandName(command)
}

func shouldUseCodexAppServer(command string) bool {
	return isCodexCommandName(command)
}

func shouldUseClaudeResumeInteractive(command string) bool {
	if !isClaudeCommandName(command) {
		return false
	}
	fields := strings.Fields(strings.TrimSpace(command))
	hasResume := false
	hasPrint := false
	for i := 0; i < len(fields); i++ {
		switch fields[i] {
		case "--resume":
			if i+1 < len(fields) {
				hasResume = true
			}
		case "--print", "-p":
			hasPrint = true
		}
	}
	return hasResume && !hasPrint
}

func extractToolTarget(toolName string, rawInput json.RawMessage) string {
	if len(rawInput) == 0 {
		return ""
	}
	var input map[string]any
	if err := json.Unmarshal(rawInput, &input); err != nil {
		return ""
	}
	keys := []string{"file_path", "path", "pattern", "command", "query", "url"}
	if isFileMutationTool(toolName) {
		keys = []string{"file_path", "path", "notebook_path", "cell_id", "pattern", "command", "query", "url"}
	}
	for _, key := range keys {
		if v, ok := input[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func isFileMutationTool(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "edit", "write", "multiedit", "notebookedit":
		return true
	default:
		return false
	}
}

func (r *PtyRunner) noteToolUse(toolName, target string) {
	r.mu.Lock()
	r.lastToolName = strings.TrimSpace(toolName)
	r.lastToolTarget = strings.TrimSpace(target)
	shouldSnapshot := isFileMutationTool(toolName)
	cwd := r.currentDir
	if shouldSnapshot {
		if r.fileSnapshots == nil {
			r.fileSnapshots = make(map[string]fileSnapshot)
		}
	}
	r.mu.Unlock()
	if !shouldSnapshot {
		return
	}
	resolved := resolveToolPath(cwd, target)
	if resolved == "" {
		return
	}
	snapshot := captureFileSnapshot(resolved)
	r.mu.Lock()
	if r.fileSnapshots == nil {
		r.fileSnapshots = make(map[string]fileSnapshot)
	}
	r.fileSnapshots[resolved] = snapshot
	r.mu.Unlock()
}

func (r *PtyRunner) emitFileDiffIfNeeded(sessionID, fallbackTarget string, sink EventSink) {
	r.mu.Lock()
	toolName := r.lastToolName
	toolTarget := r.lastToolTarget
	cwd := r.currentDir
	var snapshots map[string]fileSnapshot
	groupMeta := protocol.RuntimeMeta{
		ExecutionID: r.pendingReq.RuntimeMeta.ExecutionID,
		GroupID:     firstNonEmptyString(r.pendingReq.RuntimeMeta.GroupID, r.pendingReq.RuntimeMeta.ExecutionID),
		GroupTitle:  firstNonEmptyString(r.pendingReq.RuntimeMeta.GroupTitle, r.pendingReq.RuntimeMeta.ContextTitle),
	}
	if len(r.fileSnapshots) > 0 {
		snapshots = make(map[string]fileSnapshot, len(r.fileSnapshots))
		for k, v := range r.fileSnapshots {
			snapshots[k] = v
		}
	}
	if isFileMutationTool(toolName) {
		r.lastToolName = ""
		r.lastToolTarget = ""
		r.fileSnapshots = nil
	}
	r.mu.Unlock()
	if !isFileMutationTool(toolName) {
		return
	}
	candidates := uniqueNonEmptyStrings(
		resolveToolPath(cwd, fallbackTarget),
		resolveToolPath(cwd, toolTarget),
	)
	for path := range snapshots {
		candidates = appendUniqueString(candidates, path)
	}
	for _, absolutePath := range candidates {
		diffEvent, ok := buildFileDiffEvent(sessionID, cwd, absolutePath, snapshots[absolutePath])
		if !ok {
			continue
		}
		sendEvent(sink, protocol.ApplyRuntimeMeta(diffEvent, groupMeta))
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func ptyDebugPreview(value string) string {
	trimmed := strings.ReplaceAll(strings.TrimSpace(value), "\n", `\n`)
	trimmed = strings.ReplaceAll(trimmed, "\r", `\r`)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= ptyDebugPreviewLimit {
		return trimmed
	}
	return string(runes[:ptyDebugPreviewLimit]) + "…"
}

func ptyPromptDebugDecision(text string) (bool, []string, string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false, nil, "empty_text"
	}
	if !isPromptText(trimmed) {
		return false, nil, "is_prompt_false"
	}
	options := promptOptions(trimmed)
	if len(options) == 0 {
		return false, nil, "no_prompt_options"
	}
	return true, options, "prompt_detected"
}

func ptyLogPromptClassification(sessionID, location, sourceKind, text string, isPrompt bool, options []string, reason string) {
	classification := ptyDebugReasonLog
	if isPrompt {
		classification = ptyDebugReasonPrompt
	}
	logx.Info("pty", "classify claude text: sessionID=%s location=%s source=%s classification=%s reason=%s options=%v preview=%q", sessionID, location, sourceKind, classification, reason, options, ptyDebugPreview(text))
}

func resolveToolPath(cwd, target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	if cwd == "" {
		return filepath.Clean(target)
	}
	return filepath.Clean(filepath.Join(cwd, target))
}

func captureFileSnapshot(path string) fileSnapshot {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fileSnapshot{}
		}
		return fileSnapshot{}
	}
	return fileSnapshot{exists: true, content: string(content)}
}

func buildFileDiffEvent(sessionID, cwd, absolutePath string, before fileSnapshot) (protocol.FileDiffEvent, bool) {
	after, err := os.ReadFile(absolutePath)
	afterExists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return protocol.FileDiffEvent{}, false
	}
	afterContent := string(after)
	if before.exists == afterExists && before.content == afterContent {
		return protocol.FileDiffEvent{}, false
	}
	relPath := displayPath(cwd, absolutePath)
	diff := buildUnifiedDiff(relPath, before, fileSnapshot{exists: afterExists, content: afterContent})
	if strings.TrimSpace(diff) == "" {
		return protocol.FileDiffEvent{}, false
	}
	title := "Updating " + relPath
	if !before.exists && afterExists {
		title = "Creating " + relPath
	} else if before.exists && !afterExists {
		title = "Deleting " + relPath
	}
	lang := strings.TrimPrefix(filepath.Ext(relPath), ".")
	return protocol.NewFileDiffEvent(sessionID, relPath, title, diff, lang), true
}

func displayPath(cwd, absolutePath string) string {
	if cwd != "" {
		if rel, err := filepath.Rel(cwd, absolutePath); err == nil && rel != "" && rel != "." {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(absolutePath)
}

func buildUnifiedDiff(path string, before, after fileSnapshot) string {
	beforeLines := splitLinesPreserveEmpty(before.content)
	afterLines := splitLinesPreserveEmpty(after.content)
	var b strings.Builder
	b.WriteString("diff --git a/")
	b.WriteString(path)
	b.WriteString(" b/")
	b.WriteString(path)
	b.WriteByte('\n')
	if !before.exists && after.exists {
		b.WriteString("new file mode 100644\n")
		b.WriteString("--- /dev/null\n")
		b.WriteString("+++ b/")
		b.WriteString(path)
		b.WriteByte('\n')
		b.WriteString(fmt.Sprintf("@@ -0,0 +1,%d @@\n", len(afterLines)))
		for _, line := range afterLines {
			b.WriteString("+")
			b.WriteString(line)
			b.WriteByte('\n')
		}
		return strings.TrimRight(b.String(), "\n")
	}
	if before.exists && !after.exists {
		b.WriteString("deleted file mode 100644\n")
		b.WriteString("--- a/")
		b.WriteString(path)
		b.WriteByte('\n')
		b.WriteString("+++ /dev/null\n")
		b.WriteString(fmt.Sprintf("@@ -1,%d +0,0 @@\n", len(beforeLines)))
		for _, line := range beforeLines {
			b.WriteString("-")
			b.WriteString(line)
			b.WriteByte('\n')
		}
		return strings.TrimRight(b.String(), "\n")
	}
	b.WriteString("--- a/")
	b.WriteString(path)
	b.WriteByte('\n')
	b.WriteString("+++ b/")
	b.WriteString(path)
	b.WriteByte('\n')
	b.WriteString(fmt.Sprintf("@@ -1,%d +1,%d @@\n", len(beforeLines), len(afterLines)))
	for _, line := range beforeLines {
		b.WriteString("-")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, line := range afterLines {
		b.WriteString("+")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func splitLinesPreserveEmpty(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func uniqueNonEmptyStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = appendUniqueString(result, value)
	}
	return result
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func extractResumeArg(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) >= 3 && isCodexCommandName(fields[0]) && strings.EqualFold(strings.TrimSpace(fields[1]), "resume") && !strings.HasPrefix(fields[2], "-") {
		return fields[2]
	}
	for i := 0; i < len(fields); i++ {
		if fields[i] == "--resume" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

func hasClaudeSessionIDArg(command string) bool {
	return extractClaudeSessionIDArg(command) != ""
}

func extractClaudeSessionIDArg(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	for i := 0; i < len(fields); i++ {
		if fields[i] == "--session-id" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

type claudeStreamWriter struct {
	writer io.Writer
}

func (w *claudeStreamWriter) Write(data []byte) (int, error) {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return len(data), nil
	}
	logx.Info("pty", "claude stream writer received input: preview=%q", ptyDebugPreview(text))
	payload := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": text,
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	encoded = append(encoded, '\n')
	_, err = w.writer.Write(encoded)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

func (w *claudeStreamWriter) Close() error {
	if closer, ok := w.writer.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

type claudeStreamEnvelope struct {
	Type          string          `json:"type"`
	Subtype       string          `json:"subtype,omitempty"`
	SessionID     string          `json:"session_id,omitempty"`
	RequestID     string          `json:"request_id,omitempty"`
	Result        string          `json:"result,omitempty"`
	DurationMs    int64           `json:"duration_ms,omitempty"`
	NumTurns      int             `json:"num_turns,omitempty"`
	TotalCost     float64         `json:"total_cost_usd,omitempty"`
	ToolUseResult json.RawMessage `json:"tool_use_result,omitempty"`
	Request       struct {
		Subtype  string          `json:"subtype,omitempty"`
		ToolName string          `json:"tool_name,omitempty"`
		Input    json.RawMessage `json:"input,omitempty"`
	} `json:"request,omitempty"`
	Message struct {
		Content []struct {
			Type    string          `json:"type"`
			Text    string          `json:"text,omitempty"`
			Name    string          `json:"name,omitempty"`
			Input   json.RawMessage `json:"input,omitempty"`
			Content string          `json:"content,omitempty"`
			IsError bool            `json:"is_error,omitempty"`
		} `json:"content"`
	} `json:"message"`
}

func extractControlRequestPrompt(envelope claudeStreamEnvelope) string {
	for _, block := range envelope.Message.Content {
		if block.Type != "text" {
			continue
		}
		if text := strings.TrimSpace(block.Text); text != "" {
			if isStructuredRuntimePhaseText(text) {
				_, _, message := parseStructuredRuntimePhase(text)
				if strings.TrimSpace(message) != "" {
					return strings.TrimSpace(message)
				}
			}
			return text
		}
	}
	if strings.EqualFold(strings.TrimSpace(envelope.Request.Subtype), "can_use_tool") {
		toolName := strings.TrimSpace(envelope.Request.ToolName)
		target := strings.TrimSpace(extractToolTarget(toolName, envelope.Request.Input))
		switch {
		case toolName != "" && target != "":
			return fmt.Sprintf("Claude requested permissions to use %s on %s", toolName, target)
		case toolName != "":
			return fmt.Sprintf("Claude requested permissions to use %s", toolName)
		}
	}
	return "Claude requested permissions to continue"
}

type claudeCatalogAuthoringPayload struct {
	MobileVCCatalogAuthoring bool   `json:"mobilevcCatalogAuthoring"`
	Kind                     string `json:"kind"`
	Skill                    struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Prompt      string `json:"prompt"`
		TargetType  string `json:"targetType"`
		ResultView  string `json:"resultView"`
	} `json:"skill,omitempty"`
	Memory struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Content string `json:"content"`
	} `json:"memory,omitempty"`
}

func (r *PtyRunner) readClaudeStreamJSON(ctx context.Context, reader io.Reader, sessionID string, sink EventSink) {
	err := forEachLine(reader, func(rawLine []byte) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := strings.TrimSpace(string(rawLine))
		if line == "" {
			return nil
		}
		logx.Info("pty", "claude stream raw line: sessionID=%s preview=%q", sessionID, ptyDebugPreview(line))
		var envelope claudeStreamEnvelope
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			logx.Warn("pty", "claude stream json parse failed: sessionID=%s reason=json_unmarshal_failed err=%v preview=%q", sessionID, err, ptyDebugPreview(line))
			if shouldSuppressClaudeRawLine(line) {
				return nil
			}
			sendEvent(sink, protocol.NewLogEvent(sessionID, line, "stdout"))
			return nil
		}
		logx.Info("pty", "claude stream json parsed: sessionID=%s type=%s subtype=%s requestID=%q envelopeSessionID=%q contentBlocks=%d", sessionID, envelope.Type, envelope.Subtype, envelope.RequestID, envelope.SessionID, len(envelope.Message.Content))
		if envelope.SessionID != "" {
			r.mu.Lock()
			changed := r.claudeSessionID != envelope.SessionID
			r.claudeSessionID = envelope.SessionID
			r.mu.Unlock()
			if changed {
				sendEvent(sink, protocol.ApplyRuntimeMeta(protocol.NewSessionStateEvent(sessionID, string(session.StateActive), "AI 会话已续接"), protocol.RuntimeMeta{ResumeSessionID: envelope.SessionID}))
			}
		}
		switch envelope.Type {
		case "control_request":
			requestID := strings.TrimSpace(envelope.RequestID)
			if requestID != "" {
				r.mu.Lock()
				r.pendingControlRequestID = requestID
				r.mu.Unlock()
				logx.Info("pty", "cached control request: sessionID=%s requestID=%q", sessionID, requestID)
			}
			promptMessage := extractControlRequestPrompt(envelope)
			for _, block := range envelope.Message.Content {
				if block.Type != "text" {
					continue
				}
				if text := strings.TrimSpace(block.Text); text != "" {
					if isStructuredRuntimePhaseText(text) {
						phase, kind, message := parseStructuredRuntimePhase(text)
						sendEvent(sink, protocol.ApplyRuntimeMeta(
							protocol.NewRuntimePhaseEvent(sessionID, phase, kind, message),
							protocol.RuntimeMeta{ResumeSessionID: envelope.SessionID},
						))
						if phase == "permission_blocked" || phase == "plan_requested" || phase == "plan_active" || phase == "execute_active" {
							r.markInteractiveReady()
						}
					} else {
						sendEvent(sink, protocol.ApplyRuntimeMeta(
							protocol.NewLogEvent(sessionID, text, "stdout"),
							protocol.RuntimeMeta{ResumeSessionID: envelope.SessionID},
						))
					}
				}
			}
			promptChoices := promptOptions(promptMessage)
			promptMeta := protocol.RuntimeMeta{ResumeSessionID: envelope.SessionID}
			if looksLikePermissionPrompt(promptMessage, promptChoices) {
				promptMeta.BlockingKind = "permission"
			}
			if requestID != "" {
				r.markInteractiveReady()
				r.mu.Lock()
				if r.pendingControlRequestID == requestID {
					r.pendingPromptOptions = append([]string(nil), promptChoices...)
				}
				r.mu.Unlock()
				sendEvent(sink, protocol.ApplyRuntimeMeta(
					protocol.NewPromptRequestEvent(sessionID, promptMessage, promptChoices),
					promptMeta,
				))
			}
		case "assistant":
			for _, block := range envelope.Message.Content {
				switch block.Type {
				case "tool_use":
					target := extractToolTarget(block.Name, block.Input)
					logx.Info("pty", "claude assistant tool_use: sessionID=%s tool=%s target=%q", sessionID, block.Name, target)
					r.noteToolUse(block.Name, target)
					sendEvent(sink, protocol.NewStepUpdateEvent(sessionID, block.Name, "running", target, block.Name, ""))
				case "text":
					if text := strings.TrimSpace(block.Text); text != "" {
						r.tryEmitCatalogAuthoringResult(sessionID, text, sink)
						r.rememberAssistantText(text)
						if isStructuredRuntimePhaseText(text) {
							phase, kind, message := parseStructuredRuntimePhase(text)
							sendEvent(sink, protocol.ApplyRuntimeMeta(protocol.NewRuntimePhaseEvent(sessionID, phase, kind, message), protocol.RuntimeMeta{ResumeSessionID: envelope.SessionID}))
							continue
						}
						var event any = protocol.NewLogEvent(sessionID, text, "stdout")
						sendEvent(sink, protocol.ApplyRuntimeMeta(
							event,
							protocol.RuntimeMeta{ResumeSessionID: envelope.SessionID},
						))
					}
				}
			}
		case "user":
			// Check message content for tool_result errors (Claude internal retries)
			for _, block := range envelope.Message.Content {
				if block.Type == "tool_result" && block.IsError {
					text := strings.TrimSpace(block.Content)
					if isStructuredRuntimePhaseText(text) {
						phase, kind, message := parseStructuredRuntimePhase(text)
						sendEvent(sink, protocol.ApplyRuntimeMeta(
							protocol.NewRuntimePhaseEvent(sessionID, phase, kind, message),
							protocol.RuntimeMeta{ResumeSessionID: envelope.SessionID},
						))
						if phase == "permission_blocked" || phase == "plan_requested" || phase == "plan_active" || phase == "execute_active" {
							r.markInteractiveReady()
						}
						continue
					}
					// These are Claude's internal tool retry errors — don't expose to user
					// but update step status to show tool had an issue
					sendEvent(sink, protocol.NewStepUpdateEvent(sessionID, "tool retry", "info", "", "", ""))
					continue
				}
			}
			if target, message, ok := parseClaudeToolUseResult(envelope.ToolUseResult); ok {
				status := "done"
				if message == "" {
					message = "tool completed"
				}
				sendEvent(sink, protocol.NewStepUpdateEvent(sessionID, message, status, target, "", ""))
				r.emitFileDiffIfNeeded(sessionID, target, sink)
			}
		case "result":
			if text := strings.TrimSpace(envelope.Result); text != "" {
				logx.Info("pty", "claude result text: sessionID=%s preview=%q", sessionID, ptyDebugPreview(text))
				r.tryEmitCatalogAuthoringResult(sessionID, text, sink)
				if r.shouldEmitResultText(text) {
					sendEvent(sink, protocol.ApplyRuntimeMeta(
						protocol.NewLogEvent(sessionID, text, "stdout"),
						protocol.RuntimeMeta{ResumeSessionID: envelope.SessionID},
					))
				}
			}
			if shouldEmitClaudeReadyPrompt(envelope) {
				r.markInteractiveReady()
				sendEvent(sink, protocol.ApplyRuntimeMeta(
					protocol.NewPromptRequestEvent(sessionID, "等待输入", nil),
					protocol.RuntimeMeta{ResumeSessionID: envelope.SessionID},
				))
			}
			if envelope.DurationMs > 0 || envelope.TotalCost > 0 {
				sendEvent(sink, protocol.ProgressEvent{
					Event:   protocol.NewBaseEvent(protocol.EventTypeProgress, sessionID),
					Message: fmt.Sprintf("耗时 %.1fs · %d 轮 · $%.4f", float64(envelope.DurationMs)/1000, envelope.NumTurns, envelope.TotalCost),
					Percent: 100,
				})
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			logx.Info("pty", "claude stream reader stopped by context cancel: sessionID=%s", sessionID)
			return
		}
		if errors.Is(err, os.ErrClosed) || errors.Is(err, io.EOF) || strings.Contains(strings.ToLower(err.Error()), "file already closed") || strings.Contains(strings.ToLower(err.Error()), "use of closed file") {
			logx.Info("pty", "claude stream reader reached closed/EOF: sessionID=%s err=%v", sessionID, err)
			return
		}
		logx.Warn("pty", "claude stream reader failed: sessionID=%s err=%v", sessionID, err)
		sendEvent(sink, protocol.NewErrorEvent(sessionID, fmt.Sprintf("read claude stream: %v", err), ""))
		return
	}
	logx.Info("pty", "claude stream reader finished cleanly: sessionID=%s", sessionID)
}

func shouldEmitClaudeReadyPrompt(envelope claudeStreamEnvelope) bool {
	if strings.TrimSpace(envelope.SessionID) == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(envelope.Subtype), "error") {
		return false
	}
	return true
}

func shouldSuppressClaudeRawLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return true
	}
	if !(strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) {
		return false
	}

	// Claude stream-json occasionally emits oversized or schema-drifting JSON
	// payloads that failed to decode into our envelope model. Showing the raw
	// payload in the mobile timeline creates "garbled command spam" UX.
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return false
	}

	if _, ok := payload["type"]; ok {
		return true
	}
	if _, ok := payload["message"]; ok {
		return true
	}
	if _, ok := payload["tool_use_result"]; ok {
		return true
	}
	if _, ok := payload["session_id"]; ok {
		return true
	}
	return false
}

func (r *PtyRunner) rememberAssistantText(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastAssistantTextKey = normalizeClaudeResultText(text)
}

func (r *PtyRunner) shouldEmitResultText(text string) bool {
	normalized := normalizeClaudeResultText(text)
	if normalized == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if normalized == r.lastAssistantTextKey {
		return false
	}
	r.lastAssistantTextKey = normalized
	return true
}

func normalizeClaudeResultText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func (r *PtyRunner) tryEmitCatalogAuthoringResult(sessionID, text string, sink EventSink) {
	if strings.TrimSpace(r.pendingReq.Source) != "catalog-authoring" {
		return
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}

	r.mu.Lock()
	if r.catalogAuthoringBuffer.Len() > 0 {
		r.catalogAuthoringBuffer.WriteByte('\n')
	}
	r.catalogAuthoringBuffer.WriteString(trimmed)
	combined := strings.TrimSpace(r.catalogAuthoringBuffer.String())
	r.mu.Unlock()

	payload, ok := parseCatalogAuthoringPayload(combined)
	if !ok {
		return
	}

	var event any
	switch payload.Kind {
	case "skill":
		skill := &protocol.SkillDefinition{
			Name:        strings.TrimSpace(payload.Skill.Name),
			Description: strings.TrimSpace(payload.Skill.Description),
			Prompt:      strings.TrimSpace(payload.Skill.Prompt),
			TargetType:  strings.TrimSpace(payload.Skill.TargetType),
			ResultView:  strings.TrimSpace(payload.Skill.ResultView),
		}
		event = protocol.NewCatalogAuthoringResultEvent(sessionID, "skill", "", skill, nil)
	case "memory":
		memory := &protocol.MemoryItem{
			ID:      strings.TrimSpace(payload.Memory.ID),
			Title:   strings.TrimSpace(payload.Memory.Title),
			Content: strings.TrimSpace(payload.Memory.Content),
		}
		event = protocol.NewCatalogAuthoringResultEvent(sessionID, "memory", "", nil, memory)
	default:
		return
	}

	r.mu.Lock()
	r.catalogAuthoringBuffer.Reset()
	meta := r.pendingReq.RuntimeMeta
	r.mu.Unlock()
	sendEvent(sink, protocol.ApplyRuntimeMeta(event, meta))
}

func parseCatalogAuthoringPayload(text string) (claudeCatalogAuthoringPayload, bool) {
	var payload claudeCatalogAuthoringPayload
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return claudeCatalogAuthoringPayload{}, false
	}
	if !payload.MobileVCCatalogAuthoring {
		return claudeCatalogAuthoringPayload{}, false
	}
	switch strings.TrimSpace(payload.Kind) {
	case "skill":
		if strings.TrimSpace(payload.Skill.Name) == "" || strings.TrimSpace(payload.Skill.Prompt) == "" {
			return claudeCatalogAuthoringPayload{}, false
		}
	case "memory":
		if strings.TrimSpace(payload.Memory.Title) == "" || strings.TrimSpace(payload.Memory.Content) == "" {
			return claudeCatalogAuthoringPayload{}, false
		}
	default:
		return claudeCatalogAuthoringPayload{}, false
	}
	return payload, true
}

func parseClaudeToolUseResult(raw json.RawMessage) (targetPath string, message string, ok bool) {
	if len(raw) == 0 {
		return "", "", false
	}

	var objectPayload struct {
		Type     string `json:"type,omitempty"`
		FilePath string `json:"filePath,omitempty"`
	}
	if err := json.Unmarshal(raw, &objectPayload); err == nil {
		return strings.TrimSpace(objectPayload.FilePath), strings.TrimSpace(objectPayload.Type), true
	}

	var stringPayload string
	if err := json.Unmarshal(raw, &stringPayload); err == nil {
		return "", strings.TrimSpace(stringPayload), true
	}

	return "", "", false
}

type claudeRuntimePhasePayload struct {
	MobileVCRuntimePhase bool   `json:"mobilevcRuntimePhase"`
	Phase                string `json:"phase"`
	Kind                 string `json:"kind"`
	Message              string `json:"message,omitempty"`
	Msg                  string `json:"msg,omitempty"`
}

func isStructuredRuntimePhaseText(text string) bool {
	phase, _, _ := parseStructuredRuntimePhase(text)
	return phase != ""
}

func parseStructuredRuntimePhase(text string) (phase, kind, message string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", "", ""
	}
	var payload claudeRuntimePhasePayload
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return "", "", ""
	}
	if !payload.MobileVCRuntimePhase {
		return "", "", ""
	}
	phase = strings.TrimSpace(payload.Phase)
	kind = strings.TrimSpace(payload.Kind)
	message = strings.TrimSpace(firstNonEmptyString(payload.Message, payload.Msg))
	if phase == "" {
		return "", "", ""
	}
	return phase, kind, message
}

func startInteractiveCommand(cmd *exec.Cmd) (*interactiveSession, error) {
	ptmx, err := pty.Start(cmd)
	if err == nil {
		return &interactiveSession{stdout: ptmx, writer: ptmx, closer: ptmx}, nil
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unsupported") {
		return nil, err
	}

	stdin, stdinErr := cmd.StdinPipe()
	if stdinErr != nil {
		return nil, stdinErr
	}
	stdout, stdoutErr := cmd.StdoutPipe()
	if stdoutErr != nil {
		_ = stdin.Close()
		return nil, stdoutErr
	}
	stderr, stderrErr := cmd.StderrPipe()
	if stderrErr != nil {
		_ = stdin.Close()
		return nil, stderrErr
	}
	if startErr := cmd.Start(); startErr != nil {
		_ = stdin.Close()
		return nil, startErr
	}

	return &interactiveSession{
		stdout: stdout,
		stderr: stderr,
		writer: stdin,
		closer: &interactiveCloser{writer: stdin},
	}, nil
}

type interactiveCloser struct {
	reader io.Closer
	writer io.Closer
	output io.Closer
}

func (c *interactiveCloser) Close() error {
	if c.writer != nil {
		_ = c.writer.Close()
	}
	if c.output != nil {
		_ = c.output.Close()
	}
	if c.reader != nil {
		return c.reader.Close()
	}
	return nil
}

func (r *PtyRunner) readOutput(ctx context.Context, reader io.Reader, sessionID string, stream string, detectPrompt bool, sink EventSink) {
	logx.Info("pty", "start reading %s stream: sessionID=%s detectPrompt=%t", stream, sessionID, detectPrompt)
	defer logx.Info("pty", "stop reading %s stream: sessionID=%s", stream, sessionID)
	parser := adapter.NewGenericParser()
	buf := make([]byte, ptyReadBufferSize)
	var ansiCarry string
	var pending string
	var emittedTail string
	var promptSent bool
	var lastLiveTailEmit time.Time

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := reader.Read(buf)
		if n > 0 {
			rawChunk := string(buf[:n])
			chunk, nextCarry := adapter.StripANSIChunk(rawChunk, ansiCarry)
			ansiCarry = nextCarry
			chunk = normalizeScreenRedrawChunk(rawChunk, chunk)
			if strings.Contains(chunk, "❯") {
				r.markInteractiveReady()
			}
			logx.Info("pty", "read output chunk: sessionID=%s stream=%s detectPrompt=%t rawPreview=%q strippedPreview=%q", sessionID, stream, detectPrompt, ptyDebugPreview(rawChunk), ptyDebugPreview(chunk))

			// 如果 chunk 以 \r 开头，代表它是对当前行的重写
			// 我们需要保留这个 \r 给前端
			if strings.HasPrefix(rawChunk, "\r") && !strings.HasPrefix(chunk, "\r") {
				chunk = "\r" + chunk
			}

			pending += chunk

			for {
				// 同时查找 \n 和 \r
				idxN := strings.IndexByte(pending, '\n')
				idxR := strings.IndexByte(pending, '\r')

				idx := -1
				isBareR := false
				consume := 1
				if idxN >= 0 && (idxR < 0 || idxN < idxR) {
					idx = idxN
				} else if idxR >= 0 {
					idx = idxR
					if idx+1 < len(pending) && pending[idx+1] == '\n' {
						consume = 2
					} else {
						isBareR = true
					}
				}

				if idx < 0 {
					break
				}

				line := pending[:idx]
				if isBareR {
					line = "\r" + line // 给前端打标记，这是覆盖行
				}

				for _, event := range parser.ParseLine(line, sessionID, stream) {
					sendEvent(sink, event)
				}
				pending = pending[idx+consume:]
				emittedTail = ""
				promptSent = false
			}

			trimmedPending := strings.TrimSuffix(pending, "\r")
			if trimmedPending != "" {
				liveTailPrompt := detectPrompt && isLiveTailPromptText(trimmedPending)
				logx.Info("pty", "evaluate live tail: sessionID=%s stream=%s detectPrompt=%t isPrompt=%t options=%v preview=%q", sessionID, stream, detectPrompt, liveTailPrompt, promptOptions(trimmedPending), ptyDebugPreview(trimmedPending))
				if shouldFlushParserBeforeLiveTail(trimmedPending, liveTailPrompt) {
					for _, event := range parser.Flush(sessionID, stream) {
						sendEvent(sink, event)
					}
				}
				if liveTailPrompt {
					if !promptSent {
						r.markInteractiveReady()
						options := promptOptions(trimmedPending)
						r.cacheTextPermissionPrompt(trimmedPending, options)
						ptyLogPromptClassification(sessionID, "readOutput.liveTail", stream, trimmedPending, true, options, "live_tail_prompt")
						sendEvent(sink, protocol.NewPromptRequestEvent(sessionID, trimmedPending, options))
						promptSent = true
					}
				} else if stream != "stderr" && trimmedPending != emittedTail {
					now := time.Now()
					if shouldDeferLiveTailEmission(trimmedPending, emittedTail, rawChunk, lastLiveTailEmit, now) {
						continue
					}
					ptyLogPromptClassification(sessionID, "readOutput.liveTail", stream, trimmedPending, false, nil, "normal_output")
					sendEvent(sink, protocol.NewLogEvent(sessionID, trimmedPending, stream))
					emittedTail = trimmedPending
					lastLiveTailEmit = now
				}
			}
		}

		if err != nil {
			if errors.Is(err, os.ErrClosed) || errors.Is(err, io.EOF) {
				logx.Info("pty", "read %s reached EOF/closed: sessionID=%s err=%v", stream, sessionID, err)
				break
			}
			if strings.Contains(err.Error(), "input/output error") || strings.Contains(err.Error(), "file already closed") {
				logx.Info("pty", "read %s stopped on pipe close: sessionID=%s err=%v", stream, sessionID, err)
				break
			}
			logx.Warn("pty", "read %s failed: sessionID=%s err=%v", stream, sessionID, err)
			sendEvent(sink, protocol.NewErrorEvent(sessionID, fmt.Sprintf("read %s: %v", stream, err), ""))
			break
		}
	}

	pending = strings.TrimSuffix(pending, "\r")
	if pending != "" {
		if detectPrompt && isLiveTailPromptText(pending) && !promptSent {
			r.markInteractiveReady()
			options := promptOptions(pending)
			r.cacheTextPermissionPrompt(pending, options)
			ptyLogPromptClassification(sessionID, "readOutput.finalPending", stream, pending, true, options, "final_live_tail_prompt")
			sendEvent(sink, protocol.NewPromptRequestEvent(sessionID, pending, options))
		} else if pending != emittedTail {
			ptyLogPromptClassification(sessionID, "readOutput.finalPending", stream, pending, false, nil, "final_normal_output")
			sendEvent(sink, protocol.NewLogEvent(sessionID, pending, stream))
		}
	}

	for _, event := range parser.Flush(sessionID, stream) {
		sendEvent(sink, event)
	}
}

func shouldDeferLiveTailEmission(pending, emittedTail, rawChunk string, lastEmit time.Time, now time.Time) bool {
	trimmed := strings.TrimSpace(pending)
	if trimmed == "" || trimmed == emittedTail {
		return false
	}
	if endsWithLiveTailBoundary(trimmed) {
		return false
	}
	if lastEmit.IsZero() {
		return utf8.RuneCountInString(trimmed) < 12
	}
	if looksLikeScreenRedrawChunk(rawChunk) && now.Sub(lastEmit) < 250*time.Millisecond {
		return true
	}
	if sharedPrefix := commonPrefixRunes(trimmed, emittedTail); sharedPrefix > 0 {
		delta := utf8.RuneCountInString(trimmed) - sharedPrefix
		if delta < 8 && now.Sub(lastEmit) < 180*time.Millisecond {
			return true
		}
	}
	return utf8.RuneCountInString(trimmed) < 16 && now.Sub(lastEmit) < 120*time.Millisecond
}

func normalizeScreenRedrawChunk(rawChunk, chunk string) string {
	if chunk == "" || !looksLikeScreenRedrawChunk(rawChunk) {
		return chunk
	}

	normalized := strings.ReplaceAll(chunk, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	parts := strings.Split(normalized, "\n")

	var compact strings.Builder
	nonEmpty := 0
	empty := 0
	longParts := 0
	for _, part := range parts {
		if part == "" {
			empty++
			continue
		}
		nonEmpty++
		if utf8.RuneCountInString(part) > 2 {
			longParts++
		}
		compact.WriteString(part)
	}

	if nonEmpty < 4 || longParts > 0 || empty < nonEmpty/2 {
		return chunk
	}
	return compact.String()
}

func endsWithLiveTailBoundary(text string) bool {
	if text == "" {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(text)
	switch last {
	case ' ', '\t', '.', ',', '!', '?', ':', ';', ')', ']', '}', '>', '。', '，', '！', '？', '：', '；':
		return true
	default:
		return false
	}
}

func looksLikeScreenRedrawChunk(raw string) bool {
	return strings.Contains(raw, "\x1b[K") ||
		strings.Contains(raw, "\x1b[?2026") ||
		strings.Contains(raw, "\x1b[1;1H") ||
		strings.Contains(raw, "\x1b[?25") ||
		strings.Contains(raw, "\x1b[39;49m")
}

func commonPrefixRunes(left, right string) int {
	lr := []rune(left)
	rr := []rune(right)
	limit := len(lr)
	if len(rr) < limit {
		limit = len(rr)
	}
	count := 0
	for count < limit && lr[count] == rr[count] {
		count++
	}
	return count
}

func shouldFlushParserBeforeLiveTail(text string, isPrompt bool) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	return isPrompt || strings.HasPrefix(trimmed, "diff --git ") || strings.HasPrefix(trimmed, "*** ")
}

func parserHasPendingDiff(parser interface{ HasPendingDiff() bool }) bool {
	return parser != nil && parser.HasPendingDiff()
}

func isPromptText(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		logx.Info("pty", "isPromptText: result=false reason=empty_text preview=%q", ptyDebugPreview(text))
		return false
	}

	for _, suffix := range []string{"[y/N]", "[Y/n]", "(y/n)", "(Y/n)", " (y/n)"} {
		if strings.Contains(trimmed, suffix) || strings.HasSuffix(trimmed, suffix) {
			logx.Info("pty", "isPromptText: result=true reason=suffix_match suffix=%q preview=%q", suffix, ptyDebugPreview(trimmed))
			return true
		}
	}

	lower := strings.ToLower(trimmed)
	if isLiveTailPromptText(trimmed) {
		logx.Info("pty", "isPromptText: result=true reason=live_tail_prompt preview=%q", ptyDebugPreview(trimmed))
		return true
	}

	// Gemini CLI 使用 ">" 作为提示符
	if trimmed == ">" || strings.HasSuffix(trimmed, " >") || strings.HasSuffix(trimmed, "\n>") {
		logx.Info("pty", "isPromptText: result=true reason=gemini_prompt preview=%q", ptyDebugPreview(trimmed))
		return true
	}

	if strings.HasSuffix(trimmed, "?") || strings.HasSuffix(trimmed, ":") || strings.HasSuffix(trimmed, ">") {
		for _, keyword := range []string{"continue", "confirm", "password", "input", "select", "proceed", "approve", "yes/no", "message"} {
			if strings.Contains(lower, keyword) {
				logx.Info("pty", "isPromptText: result=true reason=keyword_match keyword=%q preview=%q", keyword, ptyDebugPreview(trimmed))
				return true
			}
		}
	}

	logx.Info("pty", "isPromptText: result=false reason=no_match preview=%q", ptyDebugPreview(trimmed))
	return false
}

func isLiveTailPromptText(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		logx.Info("pty", "isLiveTailPromptText: result=false reason=empty_text preview=%q", ptyDebugPreview(text))
		return false
	}

	lower := strings.ToLower(trimmed)

	for _, suffix := range []string{"[y/N]", "[Y/n]", "(y/n)", "(Y/n)", "[yes/no]", "(yes/no)"} {
		if strings.HasSuffix(trimmed, suffix) {
			logx.Info("pty", "isLiveTailPromptText: result=true reason=suffix_match suffix=%q preview=%q", suffix, ptyDebugPreview(trimmed))
			return true
		}
	}

	if strings.Contains(lower, "permission") ||
		strings.Contains(lower, "authorize") ||
		strings.Contains(lower, "approval") ||
		strings.Contains(lower, "allow once") ||
		strings.Contains(lower, "allow this time") ||
		strings.Contains(lower, "always allow") ||
		strings.Contains(lower, "always deny") ||
		strings.Contains(trimmed, "授权") ||
		strings.Contains(trimmed, "权限") ||
		strings.Contains(trimmed, "允许") {
		if strings.Contains(lower, "write") ||
			strings.Contains(lower, "edit") ||
			strings.Contains(lower, "overwrite") ||
			strings.Contains(lower, "modify") ||
			strings.Contains(lower, "update") ||
			strings.Contains(trimmed, "写入") ||
			strings.Contains(trimmed, "编辑") ||
			strings.Contains(trimmed, "修改") ||
			strings.Contains(trimmed, "覆盖") {
			logx.Info("pty", "isLiveTailPromptText: result=true reason=permission_write_hint preview=%q", ptyDebugPreview(trimmed))
			return true
		}
	}

	if strings.Contains(trimmed, "还没授权") ||
		strings.Contains(trimmed, "需要你的授权") ||
		strings.Contains(trimmed, "拿到权限后") ||
		strings.Contains(trimmed, "你授权后") {
		logx.Info("pty", "isLiveTailPromptText: result=true reason=permission_phrase preview=%q", ptyDebugPreview(trimmed))
		return true
	}

	if strings.HasSuffix(trimmed, ">") {
		base := strings.TrimSpace(strings.TrimSuffix(lower, ">"))
		if base == "decision" || strings.HasSuffix(base, " decision") || strings.HasSuffix(base, " input") {
			logx.Info("pty", "isLiveTailPromptText: result=true reason=decision_suffix preview=%q", ptyDebugPreview(trimmed))
			return true
		}
	}

	if strings.HasSuffix(trimmed, ":") {
		base := strings.TrimSpace(strings.TrimSuffix(lower, ":"))
		if base == "password" || strings.HasPrefix(base, "enter ") || strings.HasPrefix(base, "input ") || strings.HasPrefix(base, "select ") {
			logx.Info("pty", "isLiveTailPromptText: result=true reason=colon_prompt preview=%q", ptyDebugPreview(trimmed))
			return true
		}
	}

	if strings.HasSuffix(trimmed, "?") {
		for _, prefix := range []string{"continue", "proceed", "confirm", "approve"} {
			if strings.HasPrefix(lower, prefix) {
				logx.Info("pty", "isLiveTailPromptText: result=true reason=question_prefix prefix=%q preview=%q", prefix, ptyDebugPreview(trimmed))
				return true
			}
		}
	}

	if strings.HasPrefix(trimmed, "❯") || strings.HasPrefix(trimmed, ">") {
		logx.Info("pty", "isLiveTailPromptText: result=true reason=shell_prompt_prefix preview=%q", ptyDebugPreview(trimmed))
		return true
	}

	if strings.HasSuffix(trimmed, ">") {
		if strings.Contains(trimmed, " ") || strings.Contains(trimmed, "ready") || strings.Contains(trimmed, "resume") {
			logx.Info("pty", "isLiveTailPromptText: result=true reason=generic_gt_prompt preview=%q", ptyDebugPreview(trimmed))
			return true
		}
	}
	logx.Info("pty", "isLiveTailPromptText: result=false reason=no_match preview=%q", ptyDebugPreview(trimmed))
	return false
}

func promptOptions(text string) []string {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)
	var options []string
	switch {
	case strings.Contains(trimmed, "[y/N]"), strings.Contains(trimmed, "[Y/n]"), strings.Contains(trimmed, "(y/n)"), strings.Contains(trimmed, "(Y/n)"):
		options = []string{"y", "n"}
	case strings.Contains(lower, "should i proceed"), strings.Contains(lower, "proceed"), strings.Contains(lower, "approve"), strings.Contains(lower, "yes/no"):
		options = []string{"yes", "no"}
	case strings.Contains(lower, "permission") ||
		strings.Contains(lower, "authorize") ||
		strings.Contains(lower, "always allow") ||
		strings.Contains(lower, "always deny") ||
		strings.Contains(lower, "allow once") ||
		strings.Contains(lower, "allow this time") ||
		strings.Contains(trimmed, "授权") ||
		strings.Contains(trimmed, "权限") ||
		strings.Contains(trimmed, "允许") ||
		strings.Contains(trimmed, "还没授权") ||
		strings.Contains(trimmed, "需要你的授权") ||
		strings.Contains(trimmed, "拿到权限后") ||
		strings.Contains(trimmed, "你授权后"):
		options = []string{"y", "n"}
	default:
		options = nil
	}
	logx.Info("pty", "promptOptions: options=%v preview=%q", options, ptyDebugPreview(trimmed))
	return options
}

func resolveTextPermissionDecisionToken(decision string, options []string) string {
	normalized := strings.TrimSpace(strings.ToLower(decision))
	candidates := make([]string, 0, len(options))
	for _, option := range options {
		trimmed := strings.TrimSpace(option)
		if trimmed != "" {
			candidates = append(candidates, trimmed)
		}
	}
	if len(candidates) == 0 {
		if normalized == "approve" {
			return "y"
		}
		if normalized == "deny" {
			return "n"
		}
		return ""
	}
	if normalized == "approve" {
		return candidates[0]
	}
	return candidates[len(candidates)-1]
}

func looksLikePermissionPrompt(message string, options []string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(message))
	if strings.Contains(trimmed, "permission") ||
		strings.Contains(trimmed, "authorize") ||
		strings.Contains(trimmed, "approval") ||
		strings.Contains(trimmed, "allow once") ||
		strings.Contains(trimmed, "allow this time") ||
		strings.Contains(trimmed, "always allow") ||
		strings.Contains(trimmed, "always deny") ||
		strings.Contains(trimmed, "授权") ||
		strings.Contains(trimmed, "权限") ||
		strings.Contains(trimmed, "允许") {
		return true
	}
	if len(options) == 0 {
		return false
	}
	for _, option := range options {
		value := strings.TrimSpace(strings.ToLower(option))
		switch value {
		case "y", "n", "yes", "no", "allow", "deny", "approve", "reject", "允许", "拒绝", "同意", "取消":
		default:
			return false
		}
	}
	return true
}

func (r *PtyRunner) cacheTextPermissionPrompt(message string, options []string) {
	if !looksLikePermissionPrompt(message, options) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.TrimSpace(r.pendingControlRequestID) == "" {
		r.pendingControlRequestID = textPermissionRequestID
		r.pendingPromptOptions = append([]string(nil), options...)
	}
}

func shouldEmitClaudeTextAsPrompt(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		logx.Info("pty", "shouldEmitClaudeTextAsPrompt: result=false reason=empty_text preview=%q", ptyDebugPreview(text))
		return false
	}
	if !isPromptText(trimmed) {
		logx.Info("pty", "shouldEmitClaudeTextAsPrompt: result=false reason=is_prompt_false preview=%q", ptyDebugPreview(trimmed))
		return false
	}
	options := promptOptions(trimmed)
	result := len(options) > 0
	logx.Info("pty", "shouldEmitClaudeTextAsPrompt: result=%t reason=options_check options=%v preview=%q", result, options, ptyDebugPreview(trimmed))
	return result
}
