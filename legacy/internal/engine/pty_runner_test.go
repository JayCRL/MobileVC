package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mobilevc/internal/protocol"
)

type nopWriteCloser struct {
	strings.Builder
}

func (w *nopWriteCloser) Close() error {
	return nil
}

type chunkReader struct {
	chunks []string
	index  int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.index >= len(r.chunks) {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[r.index])
	r.index++
	return n, nil
}

func installBlockingFakeClaude(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "claude.cmd")
		script := "@echo off\r\n:loop\r\nset /p line=\r\nif errorlevel 1 exit /b 0\r\nping -n 2 127.0.0.1 >nul\r\ngoto loop\r\n"
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatalf("write fake claude: %v", err)
		}
	} else {
		path := filepath.Join(dir, "claude")
		script := "#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile IFS= read -r _line; do sleep 1; done\n"
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatalf("write fake claude: %v", err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestPtyRunnerPromptAndInput(t *testing.T) {
	runner := NewPtyRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	eventsCh := make(chan any, 32)
	runErrCh := make(chan error, 1)

	go func() {
		runErrCh <- runner.Run(ctx, ExecRequest{
			SessionID: "s1",
			Command: shellTestCommand(
				"printf 'Proceed? [y/N]'; read ans; printf 'got:%s\n' \"$ans\"",
				"Write-Host -NoNewline 'Proceed? [y/N]'; $ans = Read-Host; Write-Output ('got:' + $ans)",
				"set /p ans=Proceed? [y/N] & echo got:%ans%",
			),
			Mode: ModePTY,
		}, func(event any) {
			eventsCh <- event
		})
	}()

	var seen []any
	var sawPrompt bool
	deadline := time.After(5 * time.Second)
	for !sawPrompt {
		select {
		case event := <-eventsCh:
			seen = append(seen, event)
			prompt, ok := event.(protocol.PromptRequestEvent)
			if ok && strings.Contains(prompt.Message, "Proceed? [y/N]") {
				sawPrompt = true
			}
		case err := <-runErrCh:
			if err != nil {
				t.Fatalf("pty run failed before prompt: %v; events=%#v", err, seen)
			}
		case <-deadline:
			t.Fatalf("did not receive prompt event; events=%#v", seen)
		}
	}

	if err := runner.Write(context.Background(), []byte("y\n")); err != nil {
		t.Fatalf("write input: %v", err)
	}

	var sawOutput bool
	var sawClosed bool
	deadline = time.After(5 * time.Second)
	for !(sawOutput && sawClosed) {
		select {
		case event := <-eventsCh:
			switch v := event.(type) {
			case protocol.LogEvent:
				if strings.Contains(v.Message, "got:y") {
					sawOutput = true
				}
			case protocol.SessionStateEvent:
				if v.State == "closed" {
					sawClosed = true
				}
			}
		case err := <-runErrCh:
			if err != nil {
				t.Fatalf("pty run failed: %v", err)
			}
		case <-deadline:
			t.Fatalf("missing output=%v closed=%v", sawOutput, sawClosed)
		}
	}
}

func TestPtyRunnerTextPermissionPromptSupportsPermissionDecision(t *testing.T) {
	runner := NewPtyRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	eventsCh := make(chan any, 32)
	runErrCh := make(chan error, 1)

	go func() {
		runErrCh <- runner.Run(ctx, ExecRequest{
			SessionID: "s-text-permission",
			Command: shellTestCommand(
				"printf 'Need permission to write README.md [y/N]'; read ans; printf 'ans:%s\\n' \"$ans\"",
				"Write-Host -NoNewline 'Need permission to write README.md [y/N]'; $ans = Read-Host; Write-Output ('ans:' + $ans)",
				"set /p ans=Need permission to write README.md [y/N] & echo ans:%ans%",
			),
			Mode: ModePTY,
		}, func(event any) {
			eventsCh <- event
		})
	}()

	var sawPrompt bool
	deadline := time.After(5 * time.Second)
	for !sawPrompt {
		select {
		case event := <-eventsCh:
			prompt, ok := event.(protocol.PromptRequestEvent)
			if ok && strings.Contains(strings.ToLower(prompt.Message), "permission") {
				sawPrompt = true
			}
		case err := <-runErrCh:
			if err != nil {
				t.Fatalf("pty run failed before prompt: %v", err)
			}
		case <-deadline:
			t.Fatal("did not receive permission prompt event")
		}
	}

	if !runner.HasPendingPermissionRequest() {
		t.Fatal("expected pending permission request for text prompt")
	}

	if err := runner.WritePermissionResponse(context.Background(), "approve"); err != nil {
		t.Fatalf("write permission response: %v", err)
	}

	var sawAnswer bool
	var sawClosed bool
	deadline = time.After(5 * time.Second)
	for !(sawAnswer && sawClosed) {
		select {
		case event := <-eventsCh:
			switch v := event.(type) {
			case protocol.LogEvent:
				if strings.Contains(v.Message, "ans:y") {
					sawAnswer = true
				}
			case protocol.SessionStateEvent:
				if v.State == "closed" {
					sawClosed = true
				}
			}
		case err := <-runErrCh:
			if err != nil {
				t.Fatalf("pty run failed: %v", err)
			}
		case <-deadline:
			t.Fatalf("missing answer=%v closed=%v", sawAnswer, sawClosed)
		}
	}
}

func TestPtyRunnerEmitsCarriageReturnUpdates(t *testing.T) {
	runner := NewPtyRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var events []any
	err := runner.Run(ctx, ExecRequest{
		SessionID: "s2",
		Command: shellTestCommand(
			"printf '\rhello'; sleep 1; printf '\rhello world\n'",
			"Write-Host -NoNewline \"`rhello\"; Start-Sleep -Seconds 1; Write-Host \"`rhello world\"",
			"<nul set /p =hello & ping -n 2 127.0.0.1 >nul & echo hello world",
		),
		Mode: ModePTY,
	}, func(event any) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatalf("pty run failed: %v", err)
	}

	var sawHello bool
	var sawHelloWorld bool
	for _, event := range events {
		logEvent, ok := event.(protocol.LogEvent)
		if !ok {
			continue
		}
		if strings.Contains(logEvent.Message, "hello") {
			sawHello = true
		}
		if strings.Contains(logEvent.Message, "hello world") {
			sawHelloWorld = true
		}
	}
	if !sawHello || !sawHelloWorld {
		t.Fatalf("expected carriage return updates, got %#v", events)
	}
}

func TestCodexAppServerCommandResolvesExecutableFromUserShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell resolution only")
	}
	tempDir := t.TempDir()
	codexPath := filepath.Join(tempDir, "codex")
	if err := os.WriteFile(codexPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	shellPath := filepath.Join(tempDir, "shell")
	script := "#!/bin/sh\nif [ \"$1\" = \"-ic\" ]; then PATH=\"" + tempDir + ":$PATH\"; export PATH; /bin/sh -c \"$2\"; exit $?; fi\n/bin/sh \"$@\"\n"
	if err := os.WriteFile(shellPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake shell: %v", err)
	}
	t.Setenv("SHELL", shellPath)
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("MOBILEVC_CODEX_EXECUTABLE", "")

	cmd := newCodexAppServerCommand(context.Background(), "codex")
	if cmd.Path != codexPath {
		t.Fatalf("expected codex resolved from shell PATH, got %q", cmd.Path)
	}
	if !strings.Contains(strings.Join(cmd.Env, "\n"), "PATH="+tempDir) {
		t.Fatalf("expected command env PATH to include fake codex dir, got %#v", cmd.Env)
	}
}

func TestPtyRunnerParsesFileDiffFromCRLFOutput(t *testing.T) {
	runner := NewPtyRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var events []any
	err := runner.Run(ctx, ExecRequest{
		SessionID: "s-diff",
		Command: shellTestCommand(
			"printf 'diff --git a/internal/ws/handler.go b/internal/ws/handler.go\r\n'; printf '--- a/internal/ws/handler.go\r\n'; printf '+++ b/internal/ws/handler.go\r\n'; printf '@@ -1 +1 @@\r\n'; printf '%s\r\n' '-old' '+new'",
			"Write-Host 'diff --git a/internal/ws/handler.go b/internal/ws/handler.go'; Write-Host '--- a/internal/ws/handler.go'; Write-Host '+++ b/internal/ws/handler.go'; Write-Host '@@ -1 +1 @@'; Write-Host '-old'; Write-Host '+new'",
			"echo diff --git a/internal/ws/handler.go b/internal/ws/handler.go && echo --- a/internal/ws/handler.go && echo +++ b/internal/ws/handler.go && echo @@ -1 +1 @@ && echo -old && echo +new",
		),
		Mode: ModePTY,
	}, func(event any) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatalf("pty run failed: %v", err)
	}

	var diffEvent *protocol.FileDiffEvent
	for _, event := range events {
		if v, ok := event.(protocol.FileDiffEvent); ok {
			diffEvent = &v
			break
		}
	}
	if diffEvent == nil {
		t.Fatalf("expected file diff event, got %#v", events)
	}
	if diffEvent.Path != "internal/ws/handler.go" {
		t.Fatalf("unexpected diff path: %q", diffEvent.Path)
	}
	if diffEvent.Title != "Updating internal/ws/handler.go" {
		t.Fatalf("unexpected diff title: %q", diffEvent.Title)
	}
}

func TestPtyRunnerFlushesFileDiffBeforeInteractivePrompt(t *testing.T) {
	runner := NewPtyRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	eventsCh := make(chan any, 64)
	errCh := make(chan error, 1)
	go func() {
		errCh <- runner.Run(ctx, ExecRequest{
			SessionID: "s-diff-prompt",
			Command: shellTestCommand(
				"printf 'diff --git a/internal/ws/handler.go b/internal/ws/handler.go\r\n'; printf '--- a/internal/ws/handler.go\r\n'; printf '+++ b/internal/ws/handler.go\r\n'; printf '@@ -1 +1 @@\r\n'; printf '%s\r\n' '-old' '+new'; printf 'decision> '; IFS= read -r line; printf 'ok:%s\n' \"$line\"",
				"Write-Host 'diff --git a/internal/ws/handler.go b/internal/ws/handler.go'; Write-Host '--- a/internal/ws/handler.go'; Write-Host '+++ b/internal/ws/handler.go'; Write-Host '@@ -1 +1 @@'; Write-Host '-old'; Write-Host '+new'; Write-Host -NoNewline 'decision> '; $line = Read-Host; Write-Output ('ok:' + $line)",
				"echo diff --git a/internal/ws/handler.go b/internal/ws/handler.go && echo --- a/internal/ws/handler.go && echo +++ b/internal/ws/handler.go && echo @@ -1 +1 @@ && echo -old && echo +new && <nul set /p =decision^>  & set /p line= & echo ok:%line%",
			),
			Mode: ModePTY,
		}, func(event any) {
			eventsCh <- event
		})
	}()

	var observed []any
	diffIndex := -1
	promptIndex := -1
	deadline := time.After(5 * time.Second)
	for diffIndex == -1 || promptIndex == -1 {
		select {
		case event := <-eventsCh:
			observed = append(observed, event)
			switch v := event.(type) {
			case protocol.FileDiffEvent:
				if v.Path == "internal/ws/handler.go" && diffIndex == -1 {
					diffIndex = len(observed) - 1
				}
			case protocol.LogEvent:
				if strings.Contains(v.Message, "decision>") && promptIndex == -1 {
					promptIndex = len(observed) - 1
				}
			case protocol.PromptRequestEvent:
				if strings.Contains(v.Message, "decision>") && promptIndex == -1 {
					promptIndex = len(observed) - 1
				}
			}
		case err := <-errCh:
			if err != nil {
				t.Fatalf("pty run failed before prompt: %v; events=%#v", err, observed)
			}
		case <-deadline:
			t.Fatalf("expected diff before interactive prompt, diffIndex=%d promptIndex=%d events=%#v", diffIndex, promptIndex, observed)
		}
	}

	if diffIndex > promptIndex {
		t.Fatalf("expected FileDiffEvent before prompt tail, diffIndex=%d promptIndex=%d events=%#v", diffIndex, promptIndex, observed)
	}

	if err := runner.Write(context.Background(), []byte("accept\n")); err != nil {
		t.Fatalf("write input: %v", err)
	}
}

func TestPtyRunnerLazyStartIgnoresEmptyFirstInput(t *testing.T) {
	runner := NewPtyRunner()
	runner.mu.Lock()
	runner.lazyStart = true
	runner.pendingReq = ExecRequest{SessionID: "s-empty", Command: "claude"}
	runner.pendingCWD = "."
	runner.sink = func(any) {}
	runner.processDone = make(chan struct{})
	runner.closed = false
	runner.mu.Unlock()

	if err := runner.startClaudeStreamOnFirstInput(context.Background(), runner.pendingReq, ".", func(any) {}, []byte("\n")); err != nil {
		t.Fatalf("empty first input should be ignored: %v", err)
	}

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if !runner.lazyStart {
		t.Fatal("expected lazyStart to remain true after empty first input")
	}
}

func TestPtyRunnerSuppressesLazyStreamExitNoiseAfterClose(t *testing.T) {
	installBlockingFakeClaude(t)
	runner := NewPtyRunner()
	runner.mu.Lock()
	runner.lazyStart = true
	runner.pendingReq = ExecRequest{SessionID: "s-close", Command: "claude --print --output-format stream-json --input-format stream-json --permission-prompt-tool stdio"}
	runner.pendingCWD = "."
	runner.sink = func(any) {}
	runner.processDone = make(chan struct{})
	runner.closed = false
	runner.mu.Unlock()

	eventsCh := make(chan any, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- runner.startClaudeStreamOnFirstInput(ctx, runner.pendingReq, ".", func(event any) {
			eventsCh <- event
		}, []byte("hello\n"))
	}()

	time.Sleep(150 * time.Millisecond)
	if err := runner.Close(); err != nil {
		t.Fatalf("close runner: %v", err)
	}
	cancel()

	if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("start claude stream: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			return
		case event := <-eventsCh:
			switch v := event.(type) {
			case protocol.ErrorEvent:
				t.Fatalf("expected suppressed close noise, got error event %#v", v)
			case protocol.SessionStateEvent:
				if v.Message == "command finished with error" {
					t.Fatalf("expected suppressed close noise, got session state %#v", v)
				}
			}
		}
	}
}

func TestIsLiveTailPromptTextRecognizesInteractivePrompts(t *testing.T) {
	tests := []string{
		"decision>",
		"Proceed? [y/N]",
		"Password:",
		"Enter value:",
		"Input your choice:",
		"Select an option:",
		"Continue?",
		"Approve?",
		"p写 README 需要你的授权。拿到权限后我会直接覆盖成新的对外展示版。",
		"你授权后，我就只改这一个位置。",
	}

	for _, text := range tests {
		if !isLiveTailPromptText(text) {
			t.Fatalf("expected %q to be recognized as live-tail prompt", text)
		}
	}
}

func TestIsLiveTailPromptTextDoesNotMisclassifyLogs(t *testing.T) {
	tests := []string{
		"build decision> cache warmed",
		"status: continue processing background jobs",
		"message: prompt rendering complete",
		"progress> 90% done",
		"diff --git a/foo b/foo",
		"done? maybe later",
	}

	for _, text := range tests {
		if isLiveTailPromptText(text) {
			t.Fatalf("expected %q to remain a log tail", text)
		}
	}
}

func TestPromptOptionsRecognizesPermissionPrompts(t *testing.T) {
	tests := map[string][]string{
		"Proceed? [y/N]": {"y", "n"},
		"Approve?":       {"yes", "no"},
		"p写 README 需要你的授权。拿到权限后我会直接覆盖成新的对外展示版。": {"y", "n"},
		"你授权后，我就只改这一个位置。":                       {"y", "n"},
	}

	for text, want := range tests {
		got := promptOptions(text)
		if len(got) != len(want) {
			t.Fatalf("promptOptions(%q) length=%d want=%d", text, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("promptOptions(%q)[%d]=%q want %q", text, i, got[i], want[i])
			}
		}
	}
}

func TestClaudeStreamWriterWrapsInputAsJSON(t *testing.T) {
	var buf strings.Builder
	writer := &claudeStreamWriter{writer: &buf}
	if _, err := writer.Write([]byte("hello\n")); err != nil {
		t.Fatalf("write input: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, `"type":"user"`) || !strings.Contains(output, `"content":"hello"`) {
		t.Fatalf("unexpected encoded payload: %q", output)
	}
}

func TestPtyRunnerCachesControlRequestIDAndEmitsPrompt(t *testing.T) {
	runner := NewPtyRunner()
	var events []any
	sink := func(event any) { events = append(events, event) }

	envelope, err := json.Marshal(map[string]any{
		"type":       "control_request",
		"session_id": "resume-control",
		"request_id": "req-123",
		"message": map[string]any{
			"content": []map[string]any{{
				"type": "text",
				"text": "Claude requested permissions to write to README.md",
			}},
		},
	})
	if err != nil {
		t.Fatalf("marshal control_request envelope: %v", err)
	}

	reader := strings.NewReader(string(envelope) + "\n")
	runner.readClaudeStreamJSON(context.Background(), reader, "s-control", sink)

	if !runner.HasPendingPermissionRequest() {
		t.Fatal("expected pending control request to be cached")
	}

	var promptEvent *protocol.PromptRequestEvent
	for _, event := range events {
		if v, ok := event.(protocol.PromptRequestEvent); ok {
			promptEvent = &v
			break
		}
	}
	if promptEvent == nil {
		t.Fatalf("expected prompt request event, got %#v", events)
	}
	if promptEvent.Message != "Claude requested permissions to write to README.md" {
		t.Fatalf("unexpected prompt message: %q", promptEvent.Message)
	}
}

func TestPtyRunnerEmitsPromptFromControlRequestPayloadWithoutMessageContent(t *testing.T) {
	runner := NewPtyRunner()
	var events []any
	sink := func(event any) { events = append(events, event) }

	envelope, err := json.Marshal(map[string]any{
		"type":       "control_request",
		"request_id": "req-456",
		"request": map[string]any{
			"subtype":   "can_use_tool",
			"tool_name": "Edit",
			"input": map[string]any{
				"file_path": "/Users/wust_lh/MobileVC/README.md",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal control_request envelope: %v", err)
	}

	runner.readClaudeStreamJSON(context.Background(), strings.NewReader(string(envelope)+"\n"), "s-control-payload", sink)

	var promptEvent *protocol.PromptRequestEvent
	for _, event := range events {
		if v, ok := event.(protocol.PromptRequestEvent); ok {
			promptEvent = &v
			break
		}
	}
	if promptEvent == nil {
		t.Fatalf("expected prompt request event, got %#v", events)
	}
	if promptEvent.Message != "Claude requested permissions to use Edit on /Users/wust_lh/MobileVC/README.md" {
		t.Fatalf("unexpected prompt message: %q", promptEvent.Message)
	}
	if len(promptEvent.Options) != 2 || promptEvent.Options[0] != "y" || promptEvent.Options[1] != "n" {
		t.Fatalf("unexpected prompt options: %#v", promptEvent.Options)
	}
}

func TestPtyRunnerCurrentPermissionRequestIDUsesPendingControlID(t *testing.T) {
	runner := NewPtyRunner()
	runner.pendingControlRequestID = "req-approve"
	if got := runner.CurrentPermissionRequestID(); got != "req-approve" {
		t.Fatalf("expected current permission request id req-approve, got %q", got)
	}
}

func TestPtyRunnerWritePermissionResponseApproveEncodesControlResponse(t *testing.T) {
	buf := &nopWriteCloser{}
	runner := NewPtyRunner()
	runner.writer = buf
	runner.pendingReq = ExecRequest{SessionID: "s-control-approve"}
	runner.pendingControlRequestID = "req-approve"

	if err := runner.WritePermissionResponse(context.Background(), "approve"); err != nil {
		t.Fatalf("write permission response: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"type":"control_response"`) {
		t.Fatalf("expected control_response payload, got %q", output)
	}
	if !strings.Contains(output, `"request_id":"req-approve"`) {
		t.Fatalf("expected request_id in payload, got %q", output)
	}
	if !strings.Contains(output, `"subtype":"success"`) {
		t.Fatalf("expected success subtype in payload, got %q", output)
	}
	if !strings.Contains(output, `"behavior":"allow"`) {
		t.Fatalf("expected allow behavior in approve payload, got %q", output)
	}
	if !strings.Contains(output, `"updatedInput":{}`) {
		t.Fatalf("expected updatedInput in approve payload, got %q", output)
	}
	if runner.HasPendingPermissionRequest() {
		t.Fatal("expected pending control request to be cleared after successful write")
	}
}

func TestPtyRunnerWritePermissionResponseClearsPreviousControlRequest(t *testing.T) {
	buf := &nopWriteCloser{}
	runner := NewPtyRunner()
	runner.writer = buf
	runner.pendingReq = ExecRequest{SessionID: "s-control-previous"}
	runner.pendingControlRequestID = "req-current"
	runner.pendingControlRequestIDPrev = "req-previous"

	if err := runner.WritePermissionResponse(context.Background(), "approve"); err != nil {
		t.Fatalf("write permission response: %v", err)
	}

	if runner.HasPendingPermissionRequest() {
		t.Fatalf("expected previous control request to be discarded, current=%q", runner.CurrentPermissionRequestID())
	}
	if strings.Contains(buf.String(), "req-previous") {
		t.Fatalf("unexpected previous request in output: %q", buf.String())
	}
}

func TestPtyRunnerWritePermissionResponseApproveIncludesToolInput(t *testing.T) {
	buf := &nopWriteCloser{}
	runner := NewPtyRunner()
	runner.writer = buf
	runner.pendingReq = ExecRequest{SessionID: "s-control-approve-input"}
	runner.pendingControlRequestID = "req-approve-input"
	runner.pendingControlInput = json.RawMessage(`{"file_path":"/tmp/example.txt","content":"ok\n"}`)

	if err := runner.WritePermissionResponse(context.Background(), "approve"); err != nil {
		t.Fatalf("write permission response: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"behavior":"allow"`) {
		t.Fatalf("expected allow behavior in approve payload, got %q", output)
	}
	if !strings.Contains(output, `"updatedInput":{"content":"ok\n","file_path":"/tmp/example.txt"}`) {
		t.Fatalf("expected updatedInput to include original tool input, got %q", output)
	}
}

func TestPtyRunnerWritePermissionResponseBypassesClaudeUserWrapper(t *testing.T) {
	buf := &nopWriteCloser{}
	runner := NewPtyRunner()
	runner.writer = &claudeStreamWriter{writer: buf}
	runner.pendingReq = ExecRequest{SessionID: "s-control-stream"}
	runner.pendingControlRequestID = "req-stream"
	runner.pendingControlInput = json.RawMessage(`{"command":"echo ok"}`)

	if err := runner.WritePermissionResponse(context.Background(), "approve"); err != nil {
		t.Fatalf("write permission response: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"type":"control_response"`) {
		t.Fatalf("expected raw control_response payload, got %q", output)
	}
	if strings.Contains(output, `"type":"user"`) {
		t.Fatalf("control response must not be wrapped as user input, got %q", output)
	}
	if !strings.Contains(output, `"request_id":"req-stream"`) {
		t.Fatalf("expected request_id in payload, got %q", output)
	}
	if !strings.Contains(output, `"behavior":"allow"`) {
		t.Fatalf("expected allow behavior in payload, got %q", output)
	}
	if !strings.Contains(output, `"updatedInput":{"command":"echo ok"}`) {
		t.Fatalf("expected updatedInput in payload, got %q", output)
	}
}

func TestPtyRunnerWritePermissionResponseDenyEncodesControlResponse(t *testing.T) {
	buf := &nopWriteCloser{}
	runner := NewPtyRunner()
	runner.writer = buf
	runner.pendingReq = ExecRequest{SessionID: "s-control-deny"}
	runner.pendingControlRequestID = "req-deny"

	if err := runner.WritePermissionResponse(context.Background(), "deny"); err != nil {
		t.Fatalf("write permission response: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"behavior":"deny"`) {
		t.Fatalf("expected deny behavior, got %q", output)
	}
	if !strings.Contains(output, `"message":"Permission denied by user"`) {
		t.Fatalf("expected deny message, got %q", output)
	}
}

func TestPtyRunnerWritePermissionResponseWithoutPendingIDReturnsError(t *testing.T) {
	buf := &nopWriteCloser{}
	runner := NewPtyRunner()
	runner.writer = buf

	if err := runner.WritePermissionResponse(context.Background(), "approve"); !errors.Is(err, ErrNoPendingControlRequest) {
		t.Fatalf("expected ErrNoPendingControlRequest, got %v", err)
	}
}

func TestResolveTextPermissionDecisionTokenUsesOptionOrder(t *testing.T) {
	if got := resolveTextPermissionDecisionToken("approve", []string{"yes", "no"}); got != "yes" {
		t.Fatalf("expected approve to map first option, got %q", got)
	}
	if got := resolveTextPermissionDecisionToken("deny", []string{"yes", "no"}); got != "no" {
		t.Fatalf("expected deny to map last option, got %q", got)
	}
	if got := resolveTextPermissionDecisionToken("approve", nil); got != "y" {
		t.Fatalf("expected fallback approve token y, got %q", got)
	}
	if got := resolveTextPermissionDecisionToken("deny", nil); got != "n" {
		t.Fatalf("expected fallback deny token n, got %q", got)
	}
}

func TestPtyRunnerSuppressesDuplicateResultAfterAssistantText(t *testing.T) {
	runner := NewPtyRunner()
	var events []any
	sink := func(event any) { events = append(events, event) }

	assistantEnvelope, err := json.Marshal(map[string]any{
		"type":       "assistant",
		"session_id": "resume-dedup-1",
		"message": map[string]any{
			"content": []map[string]any{{
				"type": "text",
				"text": "Hello world",
			}},
		},
	})
	if err != nil {
		t.Fatalf("marshal assistant envelope: %v", err)
	}
	resultEnvelope, err := json.Marshal(map[string]any{
		"type":        "result",
		"session_id":  "resume-dedup-1",
		"result":      " Hello   world ",
		"duration_ms": 1200,
		"num_turns":   1,
	})
	if err != nil {
		t.Fatalf("marshal result envelope: %v", err)
	}

	runner.readClaudeStreamJSON(context.Background(), strings.NewReader(string(assistantEnvelope)+"\n"+string(resultEnvelope)+"\n"), "s-dedup", sink)

	var logs []protocol.LogEvent
	for _, event := range events {
		if v, ok := event.(protocol.LogEvent); ok {
			logs = append(logs, v)
		}
	}
	if len(logs) != 1 {
		t.Fatalf("expected exactly one visible log, got %#v", logs)
	}
	if logs[0].Message != "Hello world" {
		t.Fatalf("unexpected log message: %#v", logs)
	}
}

func TestPtyRunnerClaudeThinkingEventsCarryStableContextID(t *testing.T) {
	runner := NewPtyRunner()
	runner.pendingReq = ExecRequest{RuntimeMeta: protocol.RuntimeMeta{Engine: "claude"}}
	var events []any
	sink := func(event any) { events = append(events, event) }

	envelope, err := json.Marshal(map[string]any{
		"type":       "assistant",
		"session_id": "claude-session-1",
		"message": map[string]any{
			"content": []map[string]any{
				{
					"type": "thinking",
					"text": "第一段思考",
				},
				{
					"type": "thinking",
					"text": "第二段思考",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal assistant envelope: %v", err)
	}

	runner.readClaudeStreamJSON(context.Background(), strings.NewReader(string(envelope)+"\n"), "s-thinking", sink)

	var thinking []protocol.ThinkingEvent
	for _, event := range events {
		if v, ok := event.(protocol.ThinkingEvent); ok {
			thinking = append(thinking, v)
		}
	}
	if len(thinking) != 2 {
		t.Fatalf("expected two thinking events, got %#v", thinking)
	}
	firstID := strings.TrimSpace(thinking[0].ContextID)
	secondID := strings.TrimSpace(thinking[1].ContextID)
	if firstID == "" || secondID == "" {
		t.Fatalf("expected context ids on thinking events: %#v", thinking)
	}
	if firstID == secondID {
		t.Fatalf("expected distinct context ids, got %q", firstID)
	}
	if !strings.HasPrefix(firstID, "claude-thinking:claude-session-1:") ||
		!strings.HasPrefix(secondID, "claude-thinking:claude-session-1:") {
		t.Fatalf("unexpected context ids: %q %q", firstID, secondID)
	}
}

func TestPtyRunnerEmitsReadyPromptAfterResult(t *testing.T) {
	runner := NewPtyRunner()
	var events []any
	sink := func(event any) { events = append(events, event) }

	resultEnvelope, err := json.Marshal(map[string]any{
		"type":       "result",
		"session_id": "resume-ready-1",
		"result":     "Fallback result text",
	})
	if err != nil {
		t.Fatalf("marshal result envelope: %v", err)
	}

	runner.readClaudeStreamJSON(context.Background(), strings.NewReader(string(resultEnvelope)+"\n"), "s-result-only", sink)

	var logs []protocol.LogEvent
	var prompts []protocol.PromptRequestEvent
	for _, event := range events {
		switch v := event.(type) {
		case protocol.LogEvent:
			logs = append(logs, v)
		case protocol.PromptRequestEvent:
			prompts = append(prompts, v)
		}
	}
	if len(logs) != 1 || logs[0].Message != "Fallback result text" {
		t.Fatalf("expected fallback result log, got %#v", logs)
	}
	if len(prompts) != 1 {
		t.Fatalf("expected one ready prompt event, got %#v", prompts)
	}
	if prompts[0].Message != "等待输入" {
		t.Fatalf("unexpected ready prompt message: %#v", prompts[0])
	}
	if prompts[0].RuntimeMeta.ResumeSessionID != "resume-ready-1" {
		t.Fatalf("expected resume session id on ready prompt, got %#v", prompts[0].RuntimeMeta)
	}
	if prompts[0].RuntimeMeta.BlockingKind != "ready" {
		t.Fatalf("expected ready blocking kind on ready prompt, got %#v", prompts[0].RuntimeMeta)
	}
}

func TestPtyRunnerEmitsResultWhenAssistantTextMissing(t *testing.T) {
	runner := NewPtyRunner()
	var events []any
	sink := func(event any) { events = append(events, event) }

	resultEnvelope, err := json.Marshal(map[string]any{
		"type":       "result",
		"session_id": "resume-dedup-2",
		"result":     "Fallback result text",
	})
	if err != nil {
		t.Fatalf("marshal result envelope: %v", err)
	}

	runner.readClaudeStreamJSON(context.Background(), strings.NewReader(string(resultEnvelope)+"\n"), "s-result-only", sink)

	var logs []protocol.LogEvent
	var prompts []protocol.PromptRequestEvent
	for _, event := range events {
		switch v := event.(type) {
		case protocol.LogEvent:
			logs = append(logs, v)
		case protocol.PromptRequestEvent:
			prompts = append(prompts, v)
		}
	}
	if len(logs) != 1 || logs[0].Message != "Fallback result text" {
		t.Fatalf("expected fallback result log, got %#v", logs)
	}
	if len(prompts) != 1 {
		t.Fatalf("expected one ready prompt event, got %#v", prompts)
	}
	if prompts[0].Message != "等待输入" {
		t.Fatalf("unexpected ready prompt message: %#v", prompts[0])
	}
	if prompts[0].RuntimeMeta.ResumeSessionID != "resume-dedup-2" {
		t.Fatalf("expected resume session id on ready prompt, got %#v", prompts[0].RuntimeMeta)
	}
	if prompts[0].RuntimeMeta.BlockingKind != "ready" {
		t.Fatalf("expected ready blocking kind on ready prompt, got %#v", prompts[0].RuntimeMeta)
	}
}

func TestPtyRunnerCloseSuppressesExitNoise(t *testing.T) {
	runner := NewPtyRunner()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventsCh := make(chan any, 32)
	errCh := make(chan error, 1)
	go func() {
		errCh <- runner.Run(ctx, ExecRequest{
			SessionID: "s-close",
			Command: shellTestCommand(
				"sleep 10",
				"Start-Sleep -Seconds 10",
				"ping -n 11 127.0.0.1 >nul",
			),
			Mode: ModePTY,
		}, func(event any) {
			eventsCh <- event
		})
	}()

	time.Sleep(100 * time.Millisecond)
	if err := runner.Close(); err != nil {
		t.Fatalf("close runner: %v", err)
	}
	cancel()

	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for runner shutdown")
	}
	close(eventsCh)

	for event := range eventsCh {
		errEvent, ok := event.(protocol.ErrorEvent)
		if !ok {
			continue
		}
		message := strings.ToLower(errEvent.Message)
		if strings.Contains(message, "command exited") || strings.Contains(message, "finished with error") || strings.Contains(message, "signal") || strings.Contains(message, "killed") {
			t.Fatalf("expected exit noise to be suppressed, got %#v", errEvent)
		}
	}
}

func TestParseCatalogAuthoringPayloadRequiresSentinel(t *testing.T) {
	text := `{"kind":"skill","skill":{"name":"review","description":"desc","prompt":"prompt","targetType":"diff","resultView":"review-card"}}`
	if _, ok := parseCatalogAuthoringPayload(text); ok {
		t.Fatal("expected payload without sentinel to be rejected")
	}
}

func TestParseCatalogAuthoringPayloadAcceptsSkill(t *testing.T) {
	text := `{"mobilevcCatalogAuthoring":true,"kind":"skill","skill":{"name":"review","description":"desc","prompt":"prompt","targetType":"diff","resultView":"review-card"}}`
	payload, ok := parseCatalogAuthoringPayload(text)
	if !ok {
		t.Fatal("expected skill payload to parse")
	}
	if payload.Kind != "skill" || payload.Skill.Name != "review" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestParseCatalogAuthoringPayloadAcceptsMemory(t *testing.T) {
	text := `{"mobilevcCatalogAuthoring":true,"kind":"memory","memory":{"id":"mem-1","title":"偏好","content":"用户偏爱深色模式"}}`
	payload, ok := parseCatalogAuthoringPayload(text)
	if !ok {
		t.Fatal("expected memory payload to parse")
	}
	if payload.Kind != "memory" || payload.Memory.ID != "mem-1" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestPtyRunnerCatalogAuthoringOnlyTriggersForCatalogSource(t *testing.T) {
	runner := NewPtyRunner()
	runner.pendingReq = ExecRequest{RuntimeMeta: protocol.RuntimeMeta{Source: "command"}}
	var events []any
	runner.tryEmitCatalogAuthoringResult("s1", `{"mobilevcCatalogAuthoring":true,"kind":"memory","memory":{"id":"mem-1","title":"偏好","content":"内容"}}`, func(event any) {
		events = append(events, event)
	})
	if len(events) != 0 {
		t.Fatalf("expected no events, got %#v", events)
	}
}

func TestPtyRunnerCatalogAuthoringEmitsStructuredEvent(t *testing.T) {
	runner := NewPtyRunner()
	runner.pendingReq = ExecRequest{RuntimeMeta: protocol.RuntimeMeta{Source: "catalog-authoring", TargetType: "skill", ResultView: "skill-catalog", SkillName: "review"}}
	var events []any
	runner.tryEmitCatalogAuthoringResult("s1", `{"mobilevcCatalogAuthoring":true,"kind":"skill","skill":{"name":"review","description":"desc","prompt":"prompt","targetType":"diff","resultView":"review-card"}}`, func(event any) {
		events = append(events, event)
	})
	if len(events) != 1 {
		t.Fatalf("expected one event, got %#v", events)
	}
	result, ok := events[0].(protocol.CatalogAuthoringResultEvent)
	if !ok {
		t.Fatalf("expected CatalogAuthoringResultEvent, got %#v", events[0])
	}
	if result.Domain != "skill" || result.Skill == nil || result.Skill.Name != "review" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Source != "catalog-authoring" || result.ResultView != "skill-catalog" {
		t.Fatalf("expected runtime meta to be preserved, got %#v", result.RuntimeMeta)
	}
}

func TestPtyRunnerClaudeStreamSuppressesExitNoiseAfterClose(t *testing.T) {
	installBlockingFakeClaude(t)
	runner := NewPtyRunner()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventsCh := make(chan any, 32)
	errCh := make(chan error, 1)
	go func() {
		errCh <- runner.runClaudeStream(ctx, ExecRequest{
			SessionID: "s-close-stream",
			Command: shellTestCommand(
				"sh -c 'while IFS= read -r _line; do sleep 1; done' sh",
				"while ($null -ne ($line = [Console]::In.ReadLine())) { Start-Sleep -Seconds 1 }",
				"cmd /C more >nul",
			),
			Mode: ModePTY,
		}, ".", func(event any) {
			eventsCh <- event
		})
	}()

	time.Sleep(150 * time.Millisecond)
	if err := runner.Close(); err != nil {
		t.Fatalf("close runner: %v", err)
	}
	cancel()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("run claude stream: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for stream runner shutdown")
	}
	close(eventsCh)

	for event := range eventsCh {
		switch v := event.(type) {
		case protocol.ErrorEvent:
			message := strings.ToLower(v.Message)
			if strings.Contains(message, "signal") || strings.Contains(message, "killed") || strings.Contains(message, "command exited") {
				t.Fatalf("expected stream close noise to be suppressed, got %#v", v)
			}
		case protocol.SessionStateEvent:
			if strings.Contains(strings.ToLower(v.Message), "finished with error") {
				t.Fatalf("expected stream close noise to be suppressed, got %#v", v)
			}
		}
	}
}

func TestPtyRunnerClaudeResumeUsesInteractiveWriter(t *testing.T) {
	runner := NewPtyRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	eventsCh := make(chan any, 32)
	errCh := make(chan error, 1)
	go func() {
		errCh <- runner.runClaudeResumeInteractive(ctx, ExecRequest{
			SessionID: "s-resume",
			Command: shellTestCommand(
				"printf 'resume ready> '; IFS= read -r line; printf 'got:%s\n' \"$line\"",
				"Write-Host -NoNewline 'resume ready> '; $line = Read-Host; Write-Output ('got:' + $line)",
				"<nul set /p =resume ready^>  & set /p line= & echo got:%line%",
			),
			Mode: ModePTY,
		}, ".", func(event any) {
			eventsCh <- event
		})
	}()

	var sawPrompt bool
	deadline := time.After(5 * time.Second)
	for !sawPrompt {
		select {
		case event := <-eventsCh:
			switch v := event.(type) {
			case protocol.PromptRequestEvent:
				if strings.Contains(v.Message, "resume ready>") || strings.Contains(v.Message, "Claude 会话已恢复") {
					sawPrompt = true
				}
			case protocol.LogEvent:
				if strings.Contains(v.Message, "resume ready>") {
					sawPrompt = true
				}
			}
		case err := <-errCh:
			if err != nil {
				t.Fatalf("resume runner failed before prompt: %v", err)
			}
		case <-deadline:
			t.Fatal("did not receive resume prompt")
		}
	}

	runner.mu.Lock()
	_, isStreamWriter := runner.writer.(*claudeStreamWriter)
	interactive := runner.interactive
	runner.mu.Unlock()
	if isStreamWriter {
		t.Fatal("expected resume runner to avoid claudeStreamWriter and use interactive writer")
	}
	if !interactive {
		t.Fatal("expected resume runner to be interactive")
	}

	if err := runner.Write(context.Background(), []byte("y\n")); err != nil {
		t.Fatalf("write resume input: %v", err)
	}

	var sawOutput bool
	deadline = time.After(5 * time.Second)
	for !sawOutput {
		select {
		case event := <-eventsCh:
			if v, ok := event.(protocol.LogEvent); ok && strings.Contains(v.Message, "got:y") {
				sawOutput = true
			}
		case err := <-errCh:
			if err != nil {
				t.Fatalf("resume runner failed: %v", err)
			}
		case <-deadline:
			t.Fatal("did not receive echoed resume input")
		}
	}
}

func TestPtyRunnerClaudeLazyStartExposesSessionStateBeforeInput(t *testing.T) {
	runner := NewPtyRunner()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventsCh := make(chan any, 8)
	errCh := make(chan error, 1)
	go func() {
		errCh <- runner.Run(ctx, ExecRequest{SessionID: "s3", Command: "claude", Mode: ModePTY}, func(event any) {
			eventsCh <- event
		})
	}()

	// With eager start, the runner should emit an AgentStateEvent immediately
	// instead of deferring process launch until first input.
	deadline := time.After(3 * time.Second)
	gotStartupEvent := false
	for !gotStartupEvent {
		select {
		case event := <-eventsCh:
			if e, ok := event.(protocol.AgentStateEvent); ok &&
				strings.Contains(e.Message, "检查环境") {
				gotStartupEvent = true
			}
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("runner failed: %v", err)
			}
			return
		case <-deadline:
			t.Fatal("did not receive startup AgentStateEvent from eager start")
		}
	}

	// Verify the runner is NOT in lazy-start mode (process starts immediately)
	runner.mu.Lock()
	lazyStart := runner.lazyStart
	runner.mu.Unlock()
	if lazyStart {
		t.Fatal("expected runner to start eagerly (not lazyStart) after exec")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("runner did not exit cleanly: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runner did not exit after cancel")
	}
}

func TestPtyRunnerCanAcceptInteractiveInputReflectsState(t *testing.T) {
	runner := NewPtyRunner()
	if runner.CanAcceptInteractiveInput() {
		t.Fatal("expected empty runner to reject interactive input")
	}
	runner.mu.Lock()
	runner.interactive = true
	runner.closed = false
	runner.writer = &claudeStreamWriter{writer: &strings.Builder{}}
	runner.mu.Unlock()
	if !runner.CanAcceptInteractiveInput() {
		t.Fatal("expected interactive runner to accept direct input")
	}
}

func TestPtyRunnerStructuredRuntimePhaseTextBecomesEvent(t *testing.T) {
	runner := NewPtyRunner()
	var events []any
	sink := func(event any) { events = append(events, event) }

	envelope, err := json.Marshal(map[string]any{
		"type":       "assistant",
		"session_id": "resume-1",
		"message": map[string]any{
			"content": []map[string]any{{
				"type": "text",
				"text": `{"mobilevcRuntimePhase":true,"phase":"permission_blocked","kind":"permission","message":"awaiting approval"}`,
			}},
		},
	})
	if err != nil {
		t.Fatalf("marshal assistant envelope: %v", err)
	}

	reader := strings.NewReader(string(envelope) + "\n")
	runner.readClaudeStreamJSON(context.Background(), reader, "s-runtime-phase", sink)

	var phaseEvent *protocol.RuntimePhaseEvent
	for _, event := range events {
		if v, ok := event.(protocol.RuntimePhaseEvent); ok {
			if v.Phase == "permission_blocked" {
				phaseEvent = &v
				break
			}
		}
	}
	if phaseEvent == nil {
		t.Fatalf("expected runtime phase event, got %#v", events)
	}
	if phaseEvent.Phase != "permission_blocked" || phaseEvent.Kind != "permission" || phaseEvent.Message != "awaiting approval" {
		t.Fatalf("unexpected runtime phase event: %#v", phaseEvent)
	}
}

func TestPtyRunnerStructuredRuntimePhaseToolResultBecomesEvent(t *testing.T) {
	runner := NewPtyRunner()
	var events []any
	sink := func(event any) { events = append(events, event) }

	envelope, err := json.Marshal(map[string]any{
		"type":       "user",
		"session_id": "resume-2",
		"message": map[string]any{
			"content": []map[string]any{{
				"type":     "tool_result",
				"is_error": true,
				"content":  `{"mobilevcRuntimePhase":true,"phase":"plan_active","kind":"plan","message":"planning"}`,
			}},
		},
	})
	if err != nil {
		t.Fatalf("marshal tool_result envelope: %v", err)
	}

	reader := strings.NewReader(string(envelope) + "\n")
	runner.readClaudeStreamJSON(context.Background(), reader, "s-runtime-phase-tool", sink)

	var phaseEvent *protocol.RuntimePhaseEvent
	for _, event := range events {
		if v, ok := event.(protocol.RuntimePhaseEvent); ok {
			if v.Phase == "plan_active" {
				phaseEvent = &v
				break
			}
		}
	}
	if phaseEvent == nil {
		t.Fatalf("expected runtime phase event, got %#v", events)
	}
	if phaseEvent.Phase != "plan_active" || phaseEvent.Kind != "plan" || phaseEvent.Message != "planning" {
		t.Fatalf("unexpected runtime phase event: %#v", phaseEvent)
	}
}

func TestPtyRunnerCloseSuppressesResumeExitError(t *testing.T) {
	runner := NewPtyRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	eventsCh := make(chan any, 32)
	errCh := make(chan error, 1)
	go func() {
		errCh <- runner.runClaudeResumeInteractive(ctx, ExecRequest{
			SessionID: "s-resume-close",
			Command: shellTestCommand(
				"printf 'resume ready> '; sleep 10",
				"Write-Host -NoNewline 'resume ready> '; Start-Sleep -Seconds 10",
				"<nul set /p =resume ready^>  & ping -n 11 127.0.0.1 >nul",
			),
			Mode: ModePTY,
		}, ".", func(event any) {
			eventsCh <- event
		})
	}()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-eventsCh:
			switch v := event.(type) {
			case protocol.PromptRequestEvent:
				if strings.Contains(v.Message, "resume ready>") || strings.Contains(v.Message, "Claude 会话已恢复") {
					goto ready
				}
			case protocol.LogEvent:
				if strings.Contains(v.Message, "resume ready>") {
					goto ready
				}
			}
		case err := <-errCh:
			if err != nil {
				t.Fatalf("resume runner failed before close: %v", err)
			}
			t.Fatal("resume runner exited before close")
		case <-deadline:
			t.Fatal("did not receive resume prompt before close")
		}
	}

ready:
	if err := runner.Close(); err != nil {
		t.Fatalf("close runner: %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected nil after intentional close, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not exit after close")
	}

	for {
		select {
		case event := <-eventsCh:
			if _, ok := event.(protocol.ErrorEvent); ok {
				t.Fatalf("unexpected error event after intentional close: %#v", event)
			}
		default:
			return
		}
	}
}

func TestPtyRunnerClaudeSessionIDPrefersManagedSessionIDAndFallsBackToStreamSession(t *testing.T) {
	runner := NewPtyRunner()
	runner.pendingReq = ExecRequest{Command: "claude --session-id managed-session-123 --resume managed-session-123"}
	if got := runner.ClaudeSessionID(); got != "managed-session-123" {
		t.Fatalf("expected managed session id, got %q", got)
	}
	runner.claudeSessionID = "stream-session-456"
	if got := runner.ClaudeSessionID(); got != "managed-session-123" {
		t.Fatalf("expected managed session id to win, got %q", got)
	}
	runner.pendingReq = ExecRequest{Command: "claude"}
	if got := runner.ClaudeSessionID(); got != "stream-session-456" {
		t.Fatalf("expected stream session fallback, got %q", got)
	}
}

func TestPtyRunnerResumeCommandAddsPermissionMode(t *testing.T) {
	cmd := appendPermissionModeToCommand("claude --resume resume-xyz", "auto")
	if !strings.Contains(cmd, "--permission-mode auto") {
		t.Fatalf("expected auto permission mode in resume command, got %q", cmd)
	}
	if strings.Count(cmd, "--permission-mode") != 1 {
		t.Fatalf("expected single permission mode flag, got %q", cmd)
	}
	unchanged := appendPermissionModeToCommand(cmd, "default")
	if unchanged != cmd {
		t.Fatalf("expected existing permission mode to remain unchanged, got %q", unchanged)
	}
}

func TestCodexContextWindowUsageParsesCamelCasePayload(t *testing.T) {
	usage, ok := codexContextWindowUsage(map[string]any{
		"modelContextWindow": 200000,
		"last": map[string]any{
			"totalTokens": 50000,
		},
	})
	if !ok {
		t.Fatal("expected usage to parse")
	}
	if usage.TokenLimit != 200000 || usage.TokensUsed != 50000 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}

func TestCodexContextWindowUsageParsesSnakeCasePayload(t *testing.T) {
	usage, ok := codexContextWindowUsage(map[string]any{
		"model_context_window": 128000,
		"total": map[string]any{
			"total_tokens": 64000,
		},
	})
	if !ok {
		t.Fatal("expected usage to parse")
	}
	if usage.TokenLimit != 128000 || usage.TokensUsed != 64000 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}

func TestClaudeUsageHelpers(t *testing.T) {
	maxTokens := claudeContextWindowMaxTokens(json.RawMessage(`{
		"primary": {"contextWindow": 200000},
		"fallback": {"contextWindow": 100000}
	}`))
	if maxTokens != 200000 {
		t.Fatalf("unexpected max tokens: %d", maxTokens)
	}

	totalTokens := claudeUsageTotalTokens(json.RawMessage(`{"total_tokens":12345}`))
	if totalTokens != 12345 {
		t.Fatalf("unexpected total tokens: %d", totalTokens)
	}

	derived := claudeDerivedResultUsage(json.RawMessage(`{
		"input_tokens":1000,
		"cache_creation_input_tokens":200,
		"cache_read_input_tokens":300,
		"output_tokens":400
	}`))
	if derived != 1900 {
		t.Fatalf("unexpected derived usage: %d", derived)
	}

	effective := claudeEffectiveUsageTokens(json.RawMessage(`{
		"total_tokens":0,
		"input_tokens":1000,
		"cache_creation_input_tokens":200,
		"cache_read_input_tokens":300,
		"output_tokens":400
	}`), "result")
	if effective != 1900 {
		t.Fatalf("unexpected effective usage: %d", effective)
	}
}

func TestPtyRunnerClaudeContextWindowUsageStabilizesLimitAndZero(t *testing.T) {
	runner := NewPtyRunner()
	events := []any{}
	sink := func(event any) {
		events = append(events, event)
	}

	runner.emitClaudeContextWindowUsage("s1", claudeStreamEnvelope{
		Type:       "result",
		SessionID:  "claude-session",
		ModelUsage: json.RawMessage(`{"primary":{"contextWindow":200000}}`),
		Usage: json.RawMessage(`{
			"total_tokens":0,
			"input_tokens":5000,
			"cache_read_input_tokens":1000,
			"output_tokens":500
		}`),
	}, sink)

	if len(events) != 1 {
		t.Fatalf("expected first usage event, got %d", len(events))
	}
	first, ok := events[0].(protocol.ContextWindowUsageEvent)
	if !ok {
		t.Fatalf("expected ContextWindowUsageEvent, got %T", events[0])
	}
	if first.Usage.TokenLimit != 200000 || first.Usage.TokensUsed != 6500 {
		t.Fatalf("unexpected first usage: %+v", first.Usage)
	}

	runner.emitClaudeContextWindowUsage("s1", claudeStreamEnvelope{
		Type:       "result",
		SessionID:  "claude-session",
		ModelUsage: json.RawMessage(`{"secondary":{"contextWindow":100000}}`),
		Usage:      json.RawMessage(`{"total_tokens":7000}`),
	}, sink)

	if len(events) != 2 {
		t.Fatalf("expected second usage event, got %d", len(events))
	}
	second := events[1].(protocol.ContextWindowUsageEvent)
	if second.Usage.TokenLimit != 200000 || second.Usage.TokensUsed != 7000 {
		t.Fatalf("unexpected second usage: %+v", second.Usage)
	}

	runner.emitClaudeContextWindowUsage("s1", claudeStreamEnvelope{
		Type:      "result",
		SessionID: "claude-session",
		Usage:     json.RawMessage(`{"total_tokens":0}`),
	}, sink)

	if len(events) != 2 {
		t.Fatalf("zero usage should not emit after a real value, got %d", len(events))
	}
}

func TestNewClaudeStreamCommandPreservesResumeAndPermissionMode(t *testing.T) {
	cmd := newClaudeStreamCommand(context.Background(), "claude --resume resume-xyz", "resume-xyz", "auto")
	joined := strings.Join(cmd.Args, " ")
	if strings.Count(joined, "--resume") != 1 {
		t.Fatalf("expected single --resume, got %q", joined)
	}
	if strings.Contains(joined, "--session-id") {
		t.Fatalf("did not expect --session-id on resume command, got %q", joined)
	}
	if !strings.Contains(joined, "resume-xyz") {
		t.Fatalf("expected resume id value, got %q", joined)
	}
	if !strings.Contains(joined, "--permission-mode auto") {
		t.Fatalf("expected auto permission mode, got %q", joined)
	}
}

func TestNewClaudePromptCommandPreservesResumeAndPermissionMode(t *testing.T) {
	cmd := newClaudePromptCommand(context.Background(), "claude --resume resume-xyz", "hello", "resume-xyz", "default")
	joined := strings.Join(cmd.Args, " ")
	if strings.Count(joined, "--resume") != 1 {
		t.Fatalf("expected single --resume, got %q", joined)
	}
	if strings.Contains(joined, "--session-id") {
		t.Fatalf("did not expect --session-id on resume command, got %q", joined)
	}
	if !strings.Contains(joined, "resume-xyz") {
		t.Fatalf("expected resume id value, got %q", joined)
	}
	if !strings.Contains(joined, "--permission-mode default") {
		t.Fatalf("expected default permission mode, got %q", joined)
	}
}

func TestPtyRunnerLazyStartUsesRuntimeMetaResumeSessionID(t *testing.T) {
	runner := NewPtyRunner()
	runner.mu.Lock()
	runner.lazyStart = true
	runner.permissionMode = "auto"
	runner.pendingReq = ExecRequest{
		SessionID:      "s-resume-meta",
		Command:        "claude",
		Mode:           ModePTY,
		PermissionMode: "auto",
		RuntimeMeta:    protocol.RuntimeMeta{ResumeSessionID: "resume-meta-xyz"},
	}
	runner.pendingCWD = "/tmp"
	runner.closed = false
	runner.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := runner.startClaudeStreamOnFirstInput(ctx, runner.pendingReq, "/tmp", func(any) {}, []byte("继续\n")); err != nil {
		t.Fatalf("start lazy prompt command: %v", err)
	}

	runner.mu.Lock()
	cmd := runner.cmd
	runner.mu.Unlock()
	if cmd == nil {
		t.Fatal("expected lazy prompt command to start")
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "--resume resume-meta-xyz") {
		t.Fatalf("expected resume id from runtime meta, got %q", joined)
	}
	_ = runner.Close()
}

func TestPtyRunnerLazyStartDoesNotTreatManagedSessionIDAsResume(t *testing.T) {
	runner := NewPtyRunner()
	runner.mu.Lock()
	runner.lazyStart = true
	runner.permissionMode = "default"
	runner.pendingReq = ExecRequest{
		SessionID:      "s-managed-session",
		Command:        "claude --session-id managed-xyz",
		Mode:           ModePTY,
		PermissionMode: "default",
		RuntimeMeta:    protocol.RuntimeMeta{ResumeSessionID: "managed-xyz"},
	}
	runner.pendingCWD = "/tmp"
	runner.closed = false
	runner.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := runner.startClaudeStreamOnFirstInput(ctx, runner.pendingReq, "/tmp", func(any) {}, []byte("hello\n")); err != nil {
		t.Fatalf("start lazy prompt command: %v", err)
	}

	runner.mu.Lock()
	cmd := runner.cmd
	runner.mu.Unlock()
	if cmd == nil {
		t.Fatal("expected lazy prompt command to start")
	}
	joined := strings.Join(cmd.Args, " ")
	if strings.Contains(joined, "--resume managed-xyz") {
		t.Fatalf("did not expect managed session id to be reused as resume, got %q", joined)
	}
	_ = runner.Close()
}

func TestShouldDeferLiveTailEmissionForTinyScreenRedrawChunks(t *testing.T) {
	now := time.Now()
	if !shouldDeferLiveTailEmission("Tip", "", "\x1b[39;49m\x1b[KT", time.Time{}, now) {
		t.Fatal("expected first tiny redraw chunk to be deferred")
	}
	if shouldDeferLiveTailEmission("Tip: ", "Tip", "\x1b[39;49m\x1b[K ", now.Add(-300*time.Millisecond), now) {
		t.Fatal("expected boundary-terminated live tail to emit")
	}
}

func TestNormalizeScreenRedrawChunkCollapsesSingleCharacterRedraw(t *testing.T) {
	raw := "\x1b[39;49m\x1b[KT\x1b[39m\x1b[49m\x1b[0m\r\n\x1b[39;49m\x1b[K\x1b[39m\x1b[49m\x1b[0m\r\n\x1b[39;49m\x1b[Ki\x1b[39m\x1b[49m\x1b[0m\r\n\x1b[39;49m\x1b[K\x1b[39m\x1b[49m\x1b[0m\r\n\x1b[39;49m\x1b[Kp\x1b[39m\x1b[49m\x1b[0m\r\n\x1b[39;49m\x1b[K \x1b[39m\x1b[49m\x1b[0m\r\n"
	chunk := "T\r\n\r\ni\r\n\r\np\r\n \r\n"
	if got := normalizeScreenRedrawChunk(raw, chunk); got != "Tip " {
		t.Fatalf("unexpected normalized chunk: %q", got)
	}
}

func TestExtractResumeArgSupportsCodexResumeSubcommand(t *testing.T) {
	if got := extractResumeArg("codex resume thread-123 -m gpt-5"); got != "thread-123" {
		t.Fatalf("expected codex resume session id, got %q", got)
	}
}

func TestExtractCodexInitialPromptStripsResumeAndModelFlags(t *testing.T) {
	if got := extractCodexInitialPrompt("codex resume thread-123 -m gpt-5 /plan"); got != "/plan" {
		t.Fatalf("unexpected codex initial prompt: %q", got)
	}
}

func TestExtractCodexInitialPromptStripsConfigFlags(t *testing.T) {
	if got := extractCodexInitialPrompt("codex -m gpt-5.5 --config model_reasoning_effort=medium"); got != "" {
		t.Fatalf("unexpected codex initial prompt: %q", got)
	}
	if got := extractCodexInitialPrompt("codex -m gpt-5.5 --config model_reasoning_effort=medium 你好"); got != "你好" {
		t.Fatalf("unexpected codex initial prompt: %q", got)
	}
}

func TestCodexRunUsesInitialInputAsInitialPromptWhenCommandHasNoInlinePrompt(t *testing.T) {
	req := ExecRequest{
		SessionID:      "s-codex-initial-input",
		Command:        "codex -m gpt-5.5",
		Mode:           ModePTY,
		PermissionMode: "default",
		InitialInput:   "hello from initial input\n",
	}

	if got := extractCodexInitialPrompt(req.Command); got != "" {
		t.Fatalf("expected no inline codex initial prompt, got %q", got)
	}

	initialPrompt := extractCodexInitialPrompt(req.Command)
	if strings.TrimSpace(initialPrompt) == "" && strings.TrimSpace(req.InitialInput) != "" {
		initialPrompt = strings.TrimSpace(req.InitialInput)
	}

	if initialPrompt != "hello from initial input" {
		t.Fatalf("expected initial input fallback to become initial prompt, got %q", initialPrompt)
	}
}

func TestCodexResumeCommandStartsImmediatelyWithoutLazyPrompt(t *testing.T) {
	req := ExecRequest{
		SessionID: "s-codex-resume",
		Command:   "codex resume thread-123",
		Mode:      ModePTY,
		RuntimeMeta: protocol.RuntimeMeta{
			Engine:          "codex",
			ResumeSessionID: "thread-123",
		},
	}

	initialPrompt := extractCodexInitialPrompt(req.Command)
	if initialPrompt != "" {
		t.Fatalf("expected no inline prompt for codex resume command, got %q", initialPrompt)
	}
	if strings.TrimSpace(extractResumeArg(req.Command)) == "" {
		t.Fatalf("expected codex resume command to carry a resume id")
	}
}

func TestExtractCodexReasoningEffortFlagSupportsConfigOverride(t *testing.T) {
	got := extractCodexReasoningEffortFlag(
		`codex -m gpt-5.4 --config model_reasoning_effort="xhigh"`,
	)
	if got != "xhigh" {
		t.Fatalf("expected xhigh reasoning effort, got %q", got)
	}
}

func resolveNextPendingRPC(t *testing.T, app *codexAppSession, result any) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		var id string
		for key := range app.pending {
			id = key
			break
		}
		app.mu.Unlock()
		if id != "" {
			resultRaw, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("marshal pending rpc result: %v", err)
			}
			app.resolvePending(codexRPCMessage{
				ID:     json.RawMessage(id),
				Result: resultRaw,
			})
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for pending codex rpc call")
}

func resolveNextPendingRPCError(t *testing.T, app *codexAppSession, rpcError codexRPCError) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		var id string
		for key := range app.pending {
			id = key
			break
		}
		app.mu.Unlock()
		if id != "" {
			app.resolvePending(codexRPCMessage{
				ID:    json.RawMessage(id),
				Error: &rpcError,
			})
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for pending codex rpc call")
}

func TestCodexContextWindowUsageUnsupportedReadIsOptional(t *testing.T) {
	buf := &nopWriteCloser{}
	app := &codexAppSession{
		runner:    NewPtyRunner(),
		stdin:     buf,
		sessionID: "s-context-window-unsupported",
	}
	app.setThreadID("thread-context-1")

	errCh := make(chan error, 1)
	okCh := make(chan bool, 1)
	go func() {
		_, ok, err := app.ContextWindowUsage(context.Background())
		okCh <- ok
		errCh <- err
	}()

	resolveNextPendingRPCError(t, app, codexRPCError{
		Code:    -32600,
		Message: "Invalid request: unknown variant `thread/contextWindow/read`, expected one of `thread/compact/start`",
	})

	if err := <-errCh; err != nil {
		t.Fatalf("ContextWindowUsage returned err: %v", err)
	}
	if ok := <-okCh; ok {
		t.Fatal("ContextWindowUsage returned ok=true for unsupported optional method")
	}
	firstOutput := buf.String()
	if !strings.Contains(firstOutput, `"method":"thread/contextWindow/read"`) {
		t.Fatalf("expected first call to request context window usage, got %q", firstOutput)
	}

	buf.Reset()
	_, ok, err := app.ContextWindowUsage(context.Background())
	if err != nil {
		t.Fatalf("second ContextWindowUsage returned err: %v", err)
	}
	if ok {
		t.Fatal("second ContextWindowUsage returned ok=true after unsupported method")
	}
	if got := buf.String(); got != "" {
		t.Fatalf("expected unsupported context window read to be cached, got rpc output %q", got)
	}
}

func TestCodexAppSessionResumePassesSandboxAndApprovalPolicy(t *testing.T) {
	buf := &nopWriteCloser{}
	runner := NewPtyRunner()
	runner.SetPermissionMode("bypassPermissions")
	app := &codexAppSession{
		runner: runner,
		req: ExecRequest{
			Command: "codex -m gpt-5.4",
			RuntimeMeta: protocol.RuntimeMeta{
				CodexSandboxMode: "danger-full-access",
			},
		},
		cwd:   "/tmp/project",
		stdin: buf,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.startOrResumeThread(context.Background(), "thread-123")
	}()

	resolveNextPendingRPC(t, app, map[string]any{
		"thread": map[string]any{
			"id": "thread-123",
		},
	})

	if err := <-errCh; err != nil {
		t.Fatalf("resume thread: %v", err)
	}
	output := buf.String()
	for _, want := range []string{
		`"method":"thread/resume"`,
		`"threadId":"thread-123"`,
		`"cwd":"/tmp/project"`,
		`"approvalPolicy":"never"`,
		`"sandbox":"danger-full-access"`,
		`"model":"gpt-5.4"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %s in thread/resume payload, got %q", want, output)
		}
	}
	if strings.Contains(output, "approvalsReviewer") || strings.Contains(output, "serviceName") {
		t.Fatalf("did not expect start-only fields in thread/resume payload, got %q", output)
	}
}

func TestCodexAppSessionTurnStartPassesReasoningEffort(t *testing.T) {
	buf := &nopWriteCloser{}
	app := &codexAppSession{
		runner: NewPtyRunner(),
		req: ExecRequest{
			Command: "codex -m gpt-5.4 --config model_reasoning_effort=xhigh",
		},
		stdin: buf,
	}
	app.setThreadID("thread-123")

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.SendUserInput(context.Background(), []byte("hello\n"))
	}()

	resolveNextPendingRPC(t, app, map[string]any{
		"turn": map[string]any{
			"id": "turn-1",
		},
	})

	if err := <-errCh; err != nil {
		t.Fatalf("send user input: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"method":"turn/start"`) {
		t.Fatalf("expected turn/start rpc call, got %q", output)
	}
	if !strings.Contains(output, `"model":"gpt-5.4"`) {
		t.Fatalf("expected model override in turn/start payload, got %q", output)
	}
	if !strings.Contains(output, `"effort":"xhigh"`) {
		t.Fatalf("expected reasoning effort override in turn/start payload, got %q", output)
	}
}

func TestCodexAppSessionTurnStartPassesRuntimeSandbox(t *testing.T) {
	buf := &nopWriteCloser{}
	runner := NewPtyRunner()
	runner.SetPermissionMode("bypassPermissions")
	app := &codexAppSession{
		runner: runner,
		req: ExecRequest{
			Command: "codex -m gpt-5.4",
			RuntimeMeta: protocol.RuntimeMeta{
				CodexSandboxMode: "danger-full-access",
			},
		},
		stdin: buf,
	}
	app.setThreadID("thread-123")

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.SendUserInput(context.Background(), []byte("hello\n"))
	}()

	resolveNextPendingRPC(t, app, map[string]any{
		"turn": map[string]any{
			"id": "turn-1",
		},
	})

	if err := <-errCh; err != nil {
		t.Fatalf("send user input: %v", err)
	}

	output := buf.String()
	for _, want := range []string{
		`"method":"turn/start"`,
		`"threadId":"thread-123"`,
		`"approvalPolicy":"never"`,
		`"sandbox":"danger-full-access"`,
		`"model":"gpt-5.4"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %s in turn/start payload, got %q", want, output)
		}
	}
}

func TestCodexAppSessionTurnStartUsesCodexConfigDefaults(t *testing.T) {
	buf := &nopWriteCloser{}
	app := &codexAppSession{
		runner: NewPtyRunner(),
		req: ExecRequest{
			Command: "codex",
		},
		defaults: codexConfigDefaults{
			model:           "gpt-5.5",
			reasoningEffort: "xhigh",
		},
		stdin: buf,
	}
	app.setThreadID("thread-123")

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.SendUserInput(context.Background(), []byte("hello\n"))
	}()

	resolveNextPendingRPC(t, app, map[string]any{
		"turn": map[string]any{
			"id": "turn-1",
		},
	})

	if err := <-errCh; err != nil {
		t.Fatalf("send user input: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"model":"gpt-5.5"`) {
		t.Fatalf("expected model default in turn/start payload, got %q", output)
	}
	if !strings.Contains(output, `"effort":"xhigh"`) {
		t.Fatalf("expected reasoning effort default in turn/start payload, got %q", output)
	}
}

func TestCodexAppSessionTurnStartCommandOverridesCodexConfigDefaults(t *testing.T) {
	buf := &nopWriteCloser{}
	app := &codexAppSession{
		runner: NewPtyRunner(),
		req: ExecRequest{
			Command: "codex -m gpt-5.4 --config model_reasoning_effort=high",
		},
		defaults: codexConfigDefaults{
			model:           "gpt-5.5",
			reasoningEffort: "xhigh",
		},
		stdin: buf,
	}
	app.setThreadID("thread-123")

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.SendUserInput(context.Background(), []byte("hello\n"))
	}()

	resolveNextPendingRPC(t, app, map[string]any{
		"turn": map[string]any{
			"id": "turn-1",
		},
	})

	if err := <-errCh; err != nil {
		t.Fatalf("send user input: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"model":"gpt-5.4"`) {
		t.Fatalf("expected command model override in turn/start payload, got %q", output)
	}
	if !strings.Contains(output, `"effort":"high"`) {
		t.Fatalf("expected command reasoning effort override in turn/start payload, got %q", output)
	}
	if strings.Contains(output, `"gpt-5.5"`) || strings.Contains(output, `"xhigh"`) {
		t.Fatalf("did not expect config defaults to override command flags, got %q", output)
	}
}

func TestCodexAppSessionCompactUsesThreadCompactStart(t *testing.T) {
	buf := &nopWriteCloser{}
	var compactionEvents []protocol.CompactionEvent
	app := &codexAppSession{
		runner: NewPtyRunner(),
		req: ExecRequest{
			Command: "codex -m gpt-5.4",
		},
		stdin:     buf,
		sessionID: "s-compact",
		sink: func(event any) {
			if compaction, ok := event.(protocol.CompactionEvent); ok {
				compactionEvents = append(compactionEvents, compaction)
			}
		},
	}
	app.setThreadID("thread-compact-1")

	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Compact(context.Background())
	}()

	resolveNextPendingRPC(t, app, map[string]any{
		"turn": map[string]any{
			"id": "turn-compact-1",
		},
	})

	if err := <-errCh; err != nil {
		t.Fatalf("compact: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"method":"thread/compact/start"`) {
		t.Fatalf("expected thread/compact/start rpc call, got %q", output)
	}
	if !strings.Contains(output, `"threadId":"thread-compact-1"`) {
		t.Fatalf("expected thread id in compact payload, got %q", output)
	}
	if got := app.activeTurn(); got != "turn-compact-1" {
		t.Fatalf("expected active turn to update, got %q", got)
	}
	if len(compactionEvents) != 1 {
		t.Fatalf("expected loading compaction event, got %#v", compactionEvents)
	}
	if compactionEvents[0].Status != "loading" || compactionEvents[0].Trigger != "manual" {
		t.Fatalf("unexpected compaction loading event: %#v", compactionEvents[0])
	}
	if compactionEvents[0].ContextID == "" {
		t.Fatalf("expected stable compaction context id at start, got %#v", compactionEvents[0])
	}
}

func TestCodexAppSessionCompactionItemCompletionEmitsCompletedEvent(t *testing.T) {
	var compactionEvents []protocol.CompactionEvent
	app := &codexAppSession{
		runner:    NewPtyRunner(),
		sessionID: "s-compaction-complete",
		sink: func(event any) {
			if compaction, ok := event.(protocol.CompactionEvent); ok {
				compactionEvents = append(compactionEvents, compaction)
			}
		},
	}
	app.setCompactionTurnID("turn-compact-1")
	app.setCompactionID("cmp-1")

	params, err := json.Marshal(map[string]any{
		"threadId": "thread-123",
		"turnId":   "turn-compact-1",
		"item": map[string]any{
			"type": "contextCompaction",
			"id":   "item-compact-1",
		},
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	app.handleItemEvent(params, "done")

	if len(compactionEvents) != 1 {
		t.Fatalf("expected one completed compaction event, got %#v", compactionEvents)
	}
	if compactionEvents[0].Status != "completed" || compactionEvents[0].Trigger != "manual" {
		t.Fatalf("unexpected compaction completed event: %#v", compactionEvents[0])
	}
	if compactionEvents[0].ContextID != "cmp-1" {
		t.Fatalf("expected completed event to retain compaction lifecycle id, got %#v", compactionEvents[0])
	}
}

func TestCodexAppSessionCompactionTurnErrorEmitsFailedEvent(t *testing.T) {
	var compactionEvents []protocol.CompactionEvent
	app := &codexAppSession{
		runner:    NewPtyRunner(),
		sessionID: "s-compaction-failed",
		sink: func(event any) {
			if compaction, ok := event.(protocol.CompactionEvent); ok {
				compactionEvents = append(compactionEvents, compaction)
			}
		},
	}
	app.setCompactionTurnID("turn-compact-1")
	app.setCompactionID("cmp-err")

	params, err := json.Marshal(map[string]any{
		"threadId": "thread-123",
		"turn": map[string]any{
			"id": "turn-compact-1",
			"error": map[string]any{
				"message": "compaction exploded",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	app.handleTurnCompleted(params)

	if len(compactionEvents) != 1 {
		t.Fatalf("expected one failed compaction event, got %#v", compactionEvents)
	}
	if compactionEvents[0].Status != "failed" {
		t.Fatalf("unexpected compaction failed event: %#v", compactionEvents[0])
	}
	if compactionEvents[0].Message != "compaction exploded" {
		t.Fatalf("unexpected compaction error message: %#v", compactionEvents[0])
	}
	if compactionEvents[0].ContextID != "cmp-err" {
		t.Fatalf("expected failed event to retain compaction lifecycle id, got %#v", compactionEvents[0])
	}
}

func TestCodexAppSessionCompactionLoadingIsNotDuplicatedByItemStarted(t *testing.T) {
	var compactionEvents []protocol.CompactionEvent
	app := &codexAppSession{
		runner:    NewPtyRunner(),
		sessionID: "s-compaction-loading-dedup",
		sink: func(event any) {
			if compaction, ok := event.(protocol.CompactionEvent); ok {
				compactionEvents = append(compactionEvents, compaction)
			}
		},
	}
	app.setThreadID("thread-compact-1")
	app.resetCompactionLifecycle()
	app.setCompactionID("cmp-load")
	app.emitCompactionEvent("loading", "manual", "")

	params, err := json.Marshal(map[string]any{
		"threadId": "thread-123",
		"turnId":   "turn-compact-1",
		"item": map[string]any{
			"type": "contextCompaction",
			"id":   "item-compact-1",
		},
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	app.handleItemEvent(params, "running")

	if len(compactionEvents) != 1 {
		t.Fatalf("expected one loading compaction event, got %#v", compactionEvents)
	}
	if compactionEvents[0].ContextID != "cmp-load" {
		t.Fatalf("expected deduped loading event to retain original lifecycle id, got %#v", compactionEvents[0])
	}
}

func TestCodexAppSessionStreamsAssistantDeltasBeforePromptWithoutDuplicateFinalText(t *testing.T) {
	runner := NewPtyRunner()
	eventsCh := make(chan any, 8)
	app := &codexAppSession{
		runner:    runner,
		sessionID: "s-codex-delta",
		sink:      func(event any) { eventsCh <- event },
	}
	app.setThreadID("thread-123")

	started, err := json.Marshal(map[string]any{
		"threadId": "thread-123",
		"turn": map[string]any{
			"id":     "turn-1",
			"status": "in_progress",
		},
	})
	if err != nil {
		t.Fatalf("marshal started: %v", err)
	}
	app.handleNotification(codexRPCMessage{Method: "turn/started", Params: started})

	for _, delta := range []string{"T", "ip", " : ", "hello", " world"} {
		params, err := json.Marshal(map[string]any{
			"threadId": "thread-123",
			"turnId":   "turn-1",
			"itemId":   "item-1",
			"delta":    delta,
		})
		if err != nil {
			t.Fatalf("marshal delta: %v", err)
		}
		app.handleNotification(codexRPCMessage{Method: "item/agentMessage/delta", Params: params})
	}

	completed, err := json.Marshal(map[string]any{
		"threadId": "thread-123",
		"turn": map[string]any{
			"id":     "turn-1",
			"status": "completed",
		},
	})
	if err != nil {
		t.Fatalf("marshal completed: %v", err)
	}
	app.handleNotification(codexRPCMessage{Method: "turn/completed", Params: completed})

	var logs []protocol.LogEvent
	var prompts []protocol.PromptRequestEvent
	deadline := time.After(800 * time.Millisecond)
collect:
	for len(prompts) == 0 {
		select {
		case event := <-eventsCh:
			switch v := event.(type) {
			case protocol.LogEvent:
				logs = append(logs, v)
			case protocol.PromptRequestEvent:
				prompts = append(prompts, v)
			}
		case <-deadline:
			break collect
		}
	}
	for {
		select {
		case event := <-eventsCh:
			switch v := event.(type) {
			case protocol.LogEvent:
				logs = append(logs, v)
			case protocol.PromptRequestEvent:
				prompts = append(prompts, v)
			}
		default:
			goto done
		}
	}
done:
	if len(logs) < 1 {
		t.Fatalf("expected streamed assistant logs, got %#v", logs)
	}
	if logs[0].Message != "Tip : " {
		t.Fatalf("expected first assistant chunk before completion, got %#v", logs)
	}
	combined := strings.Join(logMessages(logs), "")
	if combined != "Tip : hello world" {
		t.Fatalf("unexpected combined streamed log message: %q from %#v", combined, logs)
	}
	countFinal := 0
	for _, log := range logs {
		if log.Message == "Tip : hello world" {
			countFinal++
		}
		if log.Source != "codex/assistant" {
			t.Fatalf("expected codex assistant source on streamed log, got %#v", log.RuntimeMeta)
		}
		if log.ExecutionID != "turn-1" {
			t.Fatalf("expected turn id execution key on streamed log, got %#v", log.RuntimeMeta)
		}
	}
	if countFinal != 0 {
		t.Fatalf("did not expect completed full text after streamed chunks, got %d occurrences in %#v", countFinal, logs)
	}
	if len(prompts) != 1 || prompts[0].ResumeSessionID != "thread-123" {
		t.Fatalf("expected invisible continue prompt with thread id, got %#v", prompts)
	}
}

func TestCodexAppSessionKeepsAssistantTurnIDForLateCompletedText(t *testing.T) {
	runner := NewPtyRunner()
	eventsCh := make(chan any, 8)
	app := &codexAppSession{
		runner:    runner,
		sessionID: "s-codex-late-completed",
		sink:      func(event any) { eventsCh <- event },
	}
	app.setThreadID("thread-123")

	started, err := json.Marshal(map[string]any{
		"threadId": "thread-123",
		"turn": map[string]any{
			"id":     "turn-1",
			"status": "in_progress",
		},
	})
	if err != nil {
		t.Fatalf("marshal started: %v", err)
	}
	app.handleNotification(codexRPCMessage{Method: "turn/started", Params: started})

	delta, err := json.Marshal(map[string]any{
		"threadId": "thread-123",
		"turnId":   "turn-1",
		"itemId":   "item-1",
		"delta":    "Tip :",
	})
	if err != nil {
		t.Fatalf("marshal delta: %v", err)
	}
	app.handleNotification(codexRPCMessage{Method: "item/agentMessage/delta", Params: delta})

	completedTurn, err := json.Marshal(map[string]any{
		"threadId": "thread-123",
		"turn": map[string]any{
			"id":     "turn-1",
			"status": "completed",
		},
	})
	if err != nil {
		t.Fatalf("marshal completed turn: %v", err)
	}
	app.handleNotification(codexRPCMessage{Method: "turn/completed", Params: completedTurn})

	completedItem, err := json.Marshal(map[string]any{
		"threadId": "thread-123",
		"turnId":   "turn-1",
		"item": map[string]any{
			"type": "agentMessage",
			"text": "Tip : hello world",
		},
	})
	if err != nil {
		t.Fatalf("marshal completed item: %v", err)
	}
	app.handleNotification(codexRPCMessage{Method: "item/completed", Params: completedItem})

	var logs []protocol.LogEvent
	for {
		select {
		case event := <-eventsCh:
			if log, ok := event.(protocol.LogEvent); ok {
				logs = append(logs, log)
			}
		default:
			if len(logs) != 2 {
				t.Fatalf("expected streamed chunk plus completed suffix, got %#v", logs)
			}
			if logs[0].Message != "Tip :" || logs[1].Message != " hello world" {
				t.Fatalf("unexpected late completed stream logs: %#v", logMessages(logs))
			}
			for _, log := range logs {
				if log.ExecutionID != "turn-1" {
					t.Fatalf("expected late completed log to keep turn execution id, got %#v", log.RuntimeMeta)
				}
				if log.Source != "codex/assistant" {
					t.Fatalf("expected codex assistant source, got %#v", log.RuntimeMeta)
				}
			}
			return
		}
	}
}

func TestCodexAppSessionDoesNotDropRepeatedAssistantDeltaChunk(t *testing.T) {
	runner := NewPtyRunner()
	eventsCh := make(chan any, 8)
	app := &codexAppSession{
		runner:    runner,
		sessionID: "s-codex-repeated-delta",
		sink:      func(event any) { eventsCh <- event },
	}
	app.setThreadID("thread-123")

	started, err := json.Marshal(map[string]any{
		"threadId": "thread-123",
		"turn": map[string]any{
			"id":     "turn-1",
			"status": "in_progress",
		},
	})
	if err != nil {
		t.Fatalf("marshal started: %v", err)
	}
	app.handleNotification(codexRPCMessage{Method: "turn/started", Params: started})

	for _, delta := range []string{"hello world", " hello world"} {
		params, err := json.Marshal(map[string]any{
			"threadId": "thread-123",
			"turnId":   "turn-1",
			"itemId":   "item-1",
			"delta":    delta,
		})
		if err != nil {
			t.Fatalf("marshal delta: %v", err)
		}
		app.handleNotification(codexRPCMessage{Method: "item/agentMessage/delta", Params: params})
	}

	var logs []protocol.LogEvent
	for {
		select {
		case event := <-eventsCh:
			if log, ok := event.(protocol.LogEvent); ok {
				logs = append(logs, log)
			}
		default:
			if len(logs) != 2 {
				t.Fatalf("expected both repeated assistant chunks, got %#v", logMessages(logs))
			}
			if got := strings.Join(logMessages(logs), ""); got != "hello world hello world" {
				t.Fatalf("unexpected repeated assistant chunks: %q from %#v", got, logMessages(logs))
			}
			return
		}
	}
}

func TestCodexAppSessionPreservesAssistantChunkNewlineBoundary(t *testing.T) {
	runner := NewPtyRunner()
	eventsCh := make(chan any, 8)
	app := &codexAppSession{
		runner:    runner,
		sessionID: "s-codex-newline-delta",
		sink:      func(event any) { eventsCh <- event },
	}
	app.setThreadID("thread-123")

	started, err := json.Marshal(map[string]any{
		"threadId": "thread-123",
		"turn": map[string]any{
			"id":     "turn-1",
			"status": "in_progress",
		},
	})
	if err != nil {
		t.Fatalf("marshal started: %v", err)
	}
	app.handleNotification(codexRPCMessage{Method: "turn/started", Params: started})

	for _, delta := range []string{"hello\n", "world"} {
		params, err := json.Marshal(map[string]any{
			"threadId": "thread-123",
			"turnId":   "turn-1",
			"itemId":   "item-1",
			"delta":    delta,
		})
		if err != nil {
			t.Fatalf("marshal delta: %v", err)
		}
		app.handleNotification(codexRPCMessage{Method: "item/agentMessage/delta", Params: params})
	}

	completed, err := json.Marshal(map[string]any{
		"threadId": "thread-123",
		"turn": map[string]any{
			"id":     "turn-1",
			"status": "completed",
		},
	})
	if err != nil {
		t.Fatalf("marshal completed turn: %v", err)
	}
	app.handleNotification(codexRPCMessage{Method: "turn/completed", Params: completed})

	var logs []protocol.LogEvent
	for {
		select {
		case event := <-eventsCh:
			if log, ok := event.(protocol.LogEvent); ok {
				logs = append(logs, log)
			}
		default:
			if len(logs) != 2 {
				t.Fatalf("expected both newline assistant chunks, got %#v", logMessages(logs))
			}
			if got := strings.Join(logMessages(logs), ""); got != "hello\nworld" {
				t.Fatalf("unexpected newline assistant chunks: %q from %#v", got, logMessages(logs))
			}
			return
		}
	}
}

func TestCodexAppSessionEmitsReasoningSummaryThinkingEvents(t *testing.T) {
	eventsCh := make(chan any, 8)
	app := &codexAppSession{
		runner:    NewPtyRunner(),
		sessionID: "s-codex-reasoning-summary",
		sink:      func(event any) { eventsCh <- event },
	}
	app.setThreadID("thread-123")
	app.setActiveTurnID("turn-1")

	for _, step := range []struct {
		method string
		params map[string]any
	}{
		{
			method: "item/reasoning/summaryTextDelta",
			params: map[string]any{
				"threadId":     "thread-123",
				"turnId":       "turn-1",
				"itemId":       "reasoning-1",
				"summaryIndex": 0,
				"delta":        "checking",
			},
		},
		{
			method: "item/reasoning/summaryTextDelta",
			params: map[string]any{
				"threadId":     "thread-123",
				"turnId":       "turn-1",
				"itemId":       "reasoning-1",
				"summaryIndex": 0,
				"delta":        " ",
			},
		},
		{
			method: "item/reasoning/summaryTextDelta",
			params: map[string]any{
				"threadId":     "thread-123",
				"turnId":       "turn-1",
				"itemId":       "reasoning-1",
				"summaryIndex": 0,
				"delta":        "context",
			},
		},
		{
			method: "item/reasoning/summaryPartAdded",
			params: map[string]any{
				"threadId":     "thread-123",
				"turnId":       "turn-1",
				"itemId":       "reasoning-1",
				"summaryIndex": 1,
			},
		},
		{
			method: "item/reasoning/summaryTextDelta",
			params: map[string]any{
				"threadId":     "thread-123",
				"turnId":       "turn-1",
				"itemId":       "reasoning-1",
				"summaryIndex": 1,
				"delta":        "准备修复",
			},
		},
	} {
		raw, err := json.Marshal(step.params)
		if err != nil {
			t.Fatalf("marshal reasoning event: %v", err)
		}
		app.handleNotification(codexRPCMessage{Method: step.method, Params: raw})
	}

	var thinking []protocol.ThinkingEvent
	for {
		select {
		case event := <-eventsCh:
			if item, ok := event.(protocol.ThinkingEvent); ok {
				thinking = append(thinking, item)
			}
		default:
			if len(thinking) != 3 {
				t.Fatalf("expected three cumulative thinking events, got %#v", thinking)
			}
			last := thinking[len(thinking)-1]
			if last.Content != "checking context\n\n准备修复" {
				t.Fatalf("unexpected reasoning summary content: %q", last.Content)
			}
			if last.Engine != "codex" || last.Source != "codex/reasoning-summary" {
				t.Fatalf("expected codex reasoning metadata, got %#v", last.RuntimeMeta)
			}
			if last.ExecutionID != "turn-1" {
				t.Fatalf("expected turn execution id, got %#v", last.RuntimeMeta)
			}
			if last.ContextID != "codex-reasoning:turn-1:reasoning-1" {
				t.Fatalf("expected stable reasoning context id, got %#v", last.RuntimeMeta)
			}
			return
		}
	}
}

func TestCodexAppSessionCompletedReasoningRecoversAndDedupesSummary(t *testing.T) {
	eventsCh := make(chan any, 8)
	app := &codexAppSession{
		runner:    NewPtyRunner(),
		sessionID: "s-codex-reasoning-completed",
		sink:      func(event any) { eventsCh <- event },
	}
	app.setThreadID("thread-123")
	app.setActiveTurnID("turn-1")

	streaming, err := json.Marshal(map[string]any{
		"threadId":     "thread-123",
		"turnId":       "turn-1",
		"itemId":       "reasoning-1",
		"summaryIndex": 0,
		"delta":        "正在定位",
	})
	if err != nil {
		t.Fatalf("marshal streaming: %v", err)
	}
	app.handleNotification(codexRPCMessage{Method: "item/reasoning/summaryTextDelta", Params: streaming})

	completedSame, err := json.Marshal(map[string]any{
		"threadId": "thread-123",
		"turnId":   "turn-1",
		"item": map[string]any{
			"type": "reasoning",
			"id":   "reasoning-1",
			"summary": []map[string]any{
				{"text": "正在定位"},
			},
			"content": []map[string]any{
				{"text": "raw hidden chain"},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal completed same: %v", err)
	}
	app.handleNotification(codexRPCMessage{Method: "item/completed", Params: completedSame})

	completedRicher, err := json.Marshal(map[string]any{
		"threadId": "thread-123",
		"turnId":   "turn-1",
		"item": map[string]any{
			"type": "reasoning",
			"id":   "reasoning-1",
			"summary": []map[string]any{
				{"text": "正在定位"},
				{"text": "准备修复"},
			},
			"content": []map[string]any{
				{"text": "raw hidden chain"},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal completed richer: %v", err)
	}
	app.handleNotification(codexRPCMessage{Method: "item/completed", Params: completedRicher})

	rawDelta, err := json.Marshal(map[string]any{
		"threadId":     "thread-123",
		"turnId":       "turn-1",
		"itemId":       "reasoning-1",
		"contentIndex": 0,
		"delta":        "raw hidden chain",
	})
	if err != nil {
		t.Fatalf("marshal raw delta: %v", err)
	}
	app.handleNotification(codexRPCMessage{Method: "item/reasoning/textDelta", Params: rawDelta})

	var thinking []protocol.ThinkingEvent
	for {
		select {
		case event := <-eventsCh:
			if item, ok := event.(protocol.ThinkingEvent); ok {
				thinking = append(thinking, item)
			}
		default:
			if len(thinking) != 2 {
				t.Fatalf("expected streaming plus richer completed thinking, got %#v", thinking)
			}
			if thinking[0].Content != "正在定位" {
				t.Fatalf("unexpected streaming content: %#v", thinking)
			}
			if thinking[1].Content != "正在定位\n\n准备修复" {
				t.Fatalf("unexpected completed content: %#v", thinking)
			}
			for _, item := range thinking {
				if strings.Contains(item.Content, "raw hidden") {
					t.Fatalf("raw reasoning content leaked into thinking event: %#v", item)
				}
			}
			return
		}
	}
}

func logMessages(logs []protocol.LogEvent) []string {
	messages := make([]string, 0, len(logs))
	for _, log := range logs {
		messages = append(messages, log.Message)
	}
	return messages
}

func TestCodexAppSessionEmitsHookAIStatus(t *testing.T) {
	eventsCh := make(chan any, 4)
	app := &codexAppSession{
		runner:    NewPtyRunner(),
		sessionID: "s-codex-hook",
		sink:      func(event any) { eventsCh <- event },
	}
	params, err := json.Marshal(map[string]any{
		"threadId": "thread-123",
		"turnId":   "turn-1",
		"run": map[string]any{
			"id":        "user-prompt-submit:0:/workspace/.codex/hooks.json",
			"eventName": "userPromptSubmit",
		},
	})
	if err != nil {
		t.Fatalf("marshal hook params: %v", err)
	}
	app.handleNotification(codexRPCMessage{Method: "hook/started", Params: params})

	select {
	case event := <-eventsCh:
		status, ok := event.(protocol.AIStatusEvent)
		if !ok {
			t.Fatalf("expected ai status event, got %#v", event)
		}
		if !status.Visible || status.Label != "Running hook: userPromptSubmit" || status.Phase != "running_hook" {
			t.Fatalf("unexpected hook status: %#v", status)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for hook ai status")
	}
}

func TestCodexAppSessionWritePermissionResponseEncodesJSONRPCResult(t *testing.T) {
	buf := &nopWriteCloser{}
	runner := NewPtyRunner()
	runner.permissionMode = "auto"
	app := &codexAppSession{
		runner:    runner,
		sessionID: "s-codex-permission",
		stdin:     buf,
	}
	app.cachePendingApproval(&codexPendingApproval{
		id:     json.RawMessage("42"),
		method: "item/fileChange/requestApproval",
	})

	if err := app.WritePermissionResponse(context.Background(), "approve"); err != nil {
		t.Fatalf("write permission response: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"id":42`) {
		t.Fatalf("expected numeric request id in response, got %q", output)
	}
	if !strings.Contains(output, `"decision":"acceptForSession"`) {
		t.Fatalf("expected acceptForSession decision, got %q", output)
	}
	if app.HasPendingPermissionRequest() {
		t.Fatal("expected pending approval to be cleared")
	}
}

func TestCodexShouldIgnoreStderrFiltersStructuredInternalCodexLogs(t *testing.T) {
	text := "2026-03-31T17:40:33.818322Z ERROR codex_core::tools::router: error=Exit code: 1"
	if !codexShouldIgnoreStderr(text) {
		t.Fatalf("expected structured internal codex log to be ignored: %q", text)
	}
}

func TestCodexShouldIgnoreStderrKeepsPlainUserFacingErrors(t *testing.T) {
	text := "tool execution failed: exit status 1"
	if codexShouldIgnoreStderr(text) {
		t.Fatalf("expected plain stderr to remain visible: %q", text)
	}
}

func TestCodexAppSessionReadLoopHandlesLongJSONLines(t *testing.T) {
	hugeDelta := strings.Repeat("x", 2*1024*1024)
	deltaParams, err := json.Marshal(map[string]any{
		"threadId": "thread-123",
		"turnId":   "turn-1",
		"itemId":   "item-1",
		"delta":    hugeDelta,
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	deltaMessage, err := json.Marshal(codexRPCMessage{
		JSONRPC: "2.0",
		Method:  "item/agentMessage/delta",
		Params:  deltaParams,
	})
	if err != nil {
		t.Fatalf("marshal rpc message: %v", err)
	}
	completedParams, err := json.Marshal(map[string]any{
		"threadId": "thread-123",
		"turnId":   "turn-1",
		"item": map[string]any{
			"type": "agentMessage",
			"id":   "item-1",
			"text": hugeDelta,
		},
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	completedMessage, err := json.Marshal(codexRPCMessage{
		JSONRPC: "2.0",
		Method:  "item/completed",
		Params:  completedParams,
	})
	if err != nil {
		t.Fatalf("marshal rpc message: %v", err)
	}

	var logs []protocol.LogEvent
	app := &codexAppSession{
		runner:    NewPtyRunner(),
		sessionID: "s-codex-long-line",
		sink: func(event any) {
			if log, ok := event.(protocol.LogEvent); ok {
				logs = append(logs, log)
			}
		},
		pending: make(map[string]chan codexRPCResponse),
	}

	app.readLoop(context.Background(), strings.NewReader(string(deltaMessage)+"\n"+string(completedMessage)+"\n"))

	if len(logs) != 1 {
		t.Fatalf("expected a single log event, got %d", len(logs))
	}
	if logs[0].Message != hugeDelta {
		t.Fatalf("unexpected log size=%d want=%d", len(logs[0].Message), len(hugeDelta))
	}
}

func TestCodexAppSessionCompletedCommandDoesNotEmitStepUpdate(t *testing.T) {
	params, err := json.Marshal(map[string]any{
		"threadId": "thread-123",
		"turnId":   "turn-1",
		"item": map[string]any{
			"type":    "commandExecution",
			"command": "go test ./internal/session",
		},
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	var steps []protocol.StepUpdateEvent
	app := &codexAppSession{
		sessionID: "s-codex-completed-command",
		sink: func(event any) {
			if step, ok := event.(protocol.StepUpdateEvent); ok {
				steps = append(steps, step)
			}
		},
	}

	app.handleItemEvent(params, "running")
	app.handleItemEvent(params, "done")

	if len(steps) != 1 {
		t.Fatalf("expected only running step, got %#v", steps)
	}
	if steps[0].Message == "Completed command" || steps[0].Status == "done" {
		t.Fatalf("completed command step should not be emitted, got %#v", steps[0])
	}
}

func TestReadOutputDoesNotDuplicateChunkedStderrLiveTail(t *testing.T) {
	runner := NewPtyRunner()
	reader := &chunkReader{
		chunks: []string{
			"zsh:1: command ",
			"not found: cl",
			"auxe\n",
		},
	}
	var logs []protocol.LogEvent
	runner.readOutput(context.Background(), reader, "s-stderr", "stderr", false, func(event any) {
		if log, ok := event.(protocol.LogEvent); ok {
			logs = append(logs, log)
		}
	})

	if len(logs) != 1 {
		t.Fatalf("expected a single stderr log, got %#v", logs)
	}
	if logs[0].Message != "zsh:1: command not found: clauxe" {
		t.Fatalf("unexpected stderr log message: %q", logs[0].Message)
	}
}
