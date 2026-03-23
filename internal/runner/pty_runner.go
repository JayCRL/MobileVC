package runner

import (
	"bufio"
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

	"github.com/creack/pty"

	"mobilevc/internal/adapter"
	"mobilevc/internal/protocol"
	"mobilevc/internal/session"
)

const ptyReadBufferSize = 4096

type PtyRunner struct {
	mu              sync.Mutex
	writer          io.WriteCloser
	closer          io.Closer
	cmd             *exec.Cmd
	closed          bool
	lazyStart       bool
	processDone     chan struct{}
	processErr      error
	pendingReq      ExecRequest
	pendingCWD      string
	currentDir      string
	sink            EventSink
	claudeSessionID string
	permissionMode  string
	lastToolName    string
	lastToolTarget  string
	fileSnapshots   map[string]fileSnapshot
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
	w.runner.mu.Unlock()
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

	if shouldUseClaudeStreamJSON(req.Command) {
		r.mu.Lock()
		r.lazyStart = true
		r.pendingReq = req
		r.pendingCWD = cwd
		r.currentDir = cwd
		r.sink = sink
		r.closed = false
		r.processDone = make(chan struct{})
		r.processErr = nil
		r.permissionMode = req.PermissionMode
		r.mu.Unlock()
		defer r.clear()

		sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateActive), "command started"))
		sendEvent(sink, protocol.NewPromptRequestEvent(req.SessionID, "Claude 会话已就绪，可继续输入", nil))

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

	cmd := newShellCommand(ctx, req.Command, req.Mode)
	cmd.Dir = cwd

	interactive, err := startInteractiveCommand(cmd)
	if err != nil {
		sendEvent(sink, protocol.NewErrorEvent(req.SessionID, fmt.Sprintf("start pty command: %v", err), ""))
		return fmt.Errorf("start pty command: %w", err)
	}
	defer interactive.closer.Close()

	r.mu.Lock()
	r.writer = interactive.writer
	r.closer = interactive.closer
	r.cmd = cmd
	r.currentDir = cwd
	r.closed = false
	r.mu.Unlock()
	defer r.clear()

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

	r.mu.Lock()
	lazyStart := r.lazyStart
	writer := r.writer
	closed := r.closed
	req := r.pendingReq
	cwd := r.pendingCWD
	sink := r.sink
	r.mu.Unlock()

	if lazyStart {
		return r.startClaudeStreamOnFirstInput(ctx, req, cwd, sink, data)
	}

	if writer == nil || closed {
		return errors.New("no active pty session")
	}

	// 关键修复：对于交互式 AI 工具，确保发送 \r\n 触发执行
	finalData := data
	if isAICommandName(req.Command) && len(data) > 0 && data[len(data)-1] == '\n' {
		if len(data) == 1 || data[len(data)-2] != '\r' {
			finalData = append(data[:len(data)-1], '\r', '\n')
		}
	}

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

func (r *PtyRunner) Close() error {
	r.mu.Lock()
	closer := r.closer
	cmd := r.cmd
	r.closed = true
	r.mu.Unlock()

	if closer != nil {
		_ = closer.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	return nil
}

func (r *PtyRunner) SetPermissionMode(mode string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.permissionMode = mode
}

func (r *PtyRunner) clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writer = nil
	r.closer = nil
	r.cmd = nil
	r.currentDir = ""
	r.lastToolName = ""
	r.lastToolTarget = ""
	r.fileSnapshots = nil
	r.closed = true
}

func (r *PtyRunner) runClaudeStream(ctx context.Context, req ExecRequest, cwd string, sink EventSink) error {
	cmd := newClaudeStreamCommand(ctx, req.Command)
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
		return fmt.Errorf("start claude stream command: %w", err)
	}

	r.mu.Lock()
	r.writer = &claudeStreamWriter{writer: stdin}
	r.closer = stdin
	r.cmd = cmd
	r.currentDir = cwd
	r.closed = false
	r.mu.Unlock()
	defer r.clear()
	defer stdin.Close()

	sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateActive), "command started"))
	sendEvent(sink, protocol.NewPromptRequestEvent(req.SessionID, "AI 会话已就绪，可继续输入", nil))

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
		return waitErr
	}

	sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished"))
	return nil
}

func (r *PtyRunner) startClaudeStreamOnFirstInput(ctx context.Context, req ExecRequest, cwd string, sink EventSink, firstInput []byte) error {
	r.mu.Lock()
	if !r.lazyStart {
		writer := r.writer
		closed := r.closed
		r.mu.Unlock()
		if writer == nil || closed {
			return errors.New("no active pty session")
		}
		// Avoid infinite recursion: if writer is still a claudeShellOnceWriter,
		// treat this as a new lazy-start cycle instead of calling Write again.
		if _, ok := writer.(*claudeShellOnceWriter); ok {
			r.mu.Lock()
			r.lazyStart = true
			r.mu.Unlock()
			return r.startClaudeStreamOnFirstInput(ctx, req, cwd, sink, firstInput)
		}
		_, err := writer.Write(firstInput)
		return err
	}
	r.lazyStart = false
	r.mu.Unlock()

	text := strings.TrimSpace(string(firstInput))
	if text == "" {
		return nil
	}

	r.mu.Lock()
	resumeSessionID := r.claudeSessionID
	permMode := r.permissionMode
	r.mu.Unlock()

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
		return fmt.Errorf("start claude prompt command: %w", err)
	}

	r.mu.Lock()
	r.cmd = cmd
	r.currentDir = cwd
	r.closed = false
	r.writer = &claudeShellOnceWriter{runner: r}
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
		r.mu.Lock()
		r.lazyStart = true
		r.writer = &claudeShellOnceWriter{runner: r}
		r.cmd = nil
		r.closed = false
		r.processDone = make(chan struct{})
		r.processErr = nil
		r.mu.Unlock()
		sendEvent(sink, protocol.NewPromptRequestEvent(req.SessionID, "AI 会话已就绪，可继续输入", nil))
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

func shouldUseClaudeStreamJSON(command string) bool {
	return isClaudeCommandName(command)
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
		sendEvent(sink, diffEvent)
		return
	}
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

type claudeStreamWriter struct {
	writer io.Writer
}

func (w *claudeStreamWriter) Write(data []byte) (int, error) {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return len(data), nil
	}
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
	Type          string  `json:"type"`
	Subtype       string  `json:"subtype,omitempty"`
	SessionID     string  `json:"session_id,omitempty"`
	Result        string  `json:"result,omitempty"`
	DurationMs    int64   `json:"duration_ms,omitempty"`
	NumTurns      int     `json:"num_turns,omitempty"`
	TotalCost     float64 `json:"total_cost_usd,omitempty"`
	ToolUseResult *struct {
		Type     string `json:"type,omitempty"`
		FilePath string `json:"filePath,omitempty"`
	} `json:"tool_use_result,omitempty"`
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

func (r *PtyRunner) readClaudeStreamJSON(ctx context.Context, reader io.Reader, sessionID string, sink EventSink) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), scannerMaxTokenSize)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var envelope claudeStreamEnvelope
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			sendEvent(sink, protocol.NewLogEvent(sessionID, line, "stdout"))
			continue
		}
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
		case "assistant":
			for _, block := range envelope.Message.Content {
				switch block.Type {
				case "tool_use":
					target := extractToolTarget(block.Name, block.Input)
					r.noteToolUse(block.Name, target)
					sendEvent(sink, protocol.NewStepUpdateEvent(sessionID, block.Name, "running", target, block.Name, ""))
				case "text":
					if text := strings.TrimSpace(block.Text); text != "" {
						sendEvent(sink, protocol.ApplyRuntimeMeta(
							protocol.NewLogEvent(sessionID, text, "stdout"),
							protocol.RuntimeMeta{ResumeSessionID: envelope.SessionID},
						))
					}
				}
			}
		case "user":
			// Check message content for tool_result errors (Claude internal retries)
			for _, block := range envelope.Message.Content {
				if block.Type == "tool_result" && block.IsError {
					// These are Claude's internal tool retry errors — don't expose to user
					// but update step status to show tool had an issue
					sendEvent(sink, protocol.NewStepUpdateEvent(sessionID, "tool retry", "info", "", "", ""))
					continue
				}
			}
			if envelope.ToolUseResult != nil {
				target := envelope.ToolUseResult.FilePath
				status := "done"
				message := envelope.ToolUseResult.Type
				if message == "" {
					message = "tool completed"
				}
				sendEvent(sink, protocol.NewStepUpdateEvent(sessionID, message, status, target, "", ""))
				r.emitFileDiffIfNeeded(sessionID, target, sink)
			}
		case "result":
			if text := strings.TrimSpace(envelope.Result); text != "" {
				sendEvent(sink, protocol.ApplyRuntimeMeta(
					protocol.NewLogEvent(sessionID, text, "stdout"),
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
	}
	if err := scanner.Err(); err != nil {
		sendEvent(sink, protocol.NewErrorEvent(sessionID, fmt.Sprintf("read claude stream: %v", err), ""))
	}
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
	parser := adapter.NewGenericParser()
	buf := make([]byte, ptyReadBufferSize)
	var pending string
	var emittedTail string
	var promptSent bool

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := reader.Read(buf)
		if n > 0 {
			rawChunk := string(buf[:n])
			chunk := adapter.StripANSI(rawChunk)

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
				if shouldFlushParserBeforeLiveTail(trimmedPending, liveTailPrompt) {
					for _, event := range parser.Flush(sessionID, stream) {
						sendEvent(sink, event)
					}
				}
				if liveTailPrompt {
					if !promptSent {
						sendEvent(sink, protocol.NewPromptRequestEvent(sessionID, trimmedPending, promptOptions(trimmedPending)))
						promptSent = true
					}
				} else if trimmedPending != emittedTail {
					sendEvent(sink, protocol.NewLogEvent(sessionID, trimmedPending, stream))
					emittedTail = trimmedPending
				}
			}
		}

		if err != nil {
			if errors.Is(err, os.ErrClosed) || errors.Is(err, io.EOF) {
				break
			}
			if strings.Contains(err.Error(), "input/output error") || strings.Contains(err.Error(), "file already closed") {
				break
			}
			sendEvent(sink, protocol.NewErrorEvent(sessionID, fmt.Sprintf("read %s: %v", stream, err), ""))
			break
		}
	}

	pending = strings.TrimSuffix(pending, "\r")
	if pending != "" {
		if detectPrompt && isLiveTailPromptText(pending) && !promptSent {
			sendEvent(sink, protocol.NewPromptRequestEvent(sessionID, pending, promptOptions(pending)))
		} else if pending != emittedTail {
			sendEvent(sink, protocol.NewLogEvent(sessionID, pending, stream))
		}
	}

	for _, event := range parser.Flush(sessionID, stream) {
		sendEvent(sink, event)
	}
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
		return false
	}

	for _, suffix := range []string{"[y/N]", "[Y/n]", "(y/n)", "(Y/n)", " (y/n)"} {
		if strings.Contains(trimmed, suffix) || strings.HasSuffix(trimmed, suffix) {
			return true
		}
	}

	lower := strings.ToLower(trimmed)
	if isLiveTailPromptText(trimmed) {
		return true
	}

	// Gemini CLI 使用 ">" 作为提示符
	if trimmed == ">" || strings.HasSuffix(trimmed, " >") || strings.HasSuffix(trimmed, "\n>") {
		return true
	}

	if strings.HasSuffix(trimmed, "?") || strings.HasSuffix(trimmed, ":") || strings.HasSuffix(trimmed, ">") {
		for _, keyword := range []string{"continue", "confirm", "password", "input", "select", "proceed", "approve", "yes/no", "message"} {
			if strings.Contains(lower, keyword) {
				return true
			}
		}
	}

	return false
}

func isLiveTailPromptText(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}

	lower := strings.ToLower(trimmed)

	for _, suffix := range []string{"[y/N]", "[Y/n]", "(y/n)", "(Y/n)", "[yes/no]", "(yes/no)"} {
		if strings.HasSuffix(trimmed, suffix) {
			return true
		}
	}

	if strings.HasSuffix(trimmed, ">") {
		base := strings.TrimSpace(strings.TrimSuffix(lower, ">"))
		if base == "decision" || strings.HasSuffix(base, " decision") || strings.HasSuffix(base, " input") {
			return true
		}
	}

	if strings.HasSuffix(trimmed, ":") {
		base := strings.TrimSpace(strings.TrimSuffix(lower, ":"))
		if base == "password" || strings.HasPrefix(base, "enter ") || strings.HasPrefix(base, "input ") || strings.HasPrefix(base, "select ") {
			return true
		}
	}

	if strings.HasSuffix(trimmed, "?") {
		for _, prefix := range []string{"continue", "proceed", "confirm", "approve"} {
			if strings.HasPrefix(lower, prefix) {
				return true
			}
		}
	}

	return false
}

func promptOptions(text string) []string {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)
	switch {
	case strings.Contains(trimmed, "[y/N]"), strings.Contains(trimmed, "[Y/n]"), strings.Contains(trimmed, "(y/n)"), strings.Contains(trimmed, "(Y/n)"):
		return []string{"y", "n"}
	case strings.Contains(lower, "should i proceed"), strings.Contains(lower, "proceed"), strings.Contains(lower, "approve"), strings.Contains(lower, "yes/no"):
		return []string{"yes", "no"}
	default:
		return nil
	}
}
