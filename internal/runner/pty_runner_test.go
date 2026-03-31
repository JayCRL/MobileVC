package runner

import (
	"context"
	"encoding/json"
	"errors"
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
	if !strings.Contains(output, `"behavior":"allow"`) {
		t.Fatalf("expected allow behavior, got %q", output)
	}
	if runner.HasPendingPermissionRequest() {
		t.Fatal("expected pending control request to be cleared after successful write")
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
	for _, event := range events {
		if v, ok := event.(protocol.LogEvent); ok {
			logs = append(logs, v)
		}
	}
	if len(logs) != 1 || logs[0].Message != "Fallback result text" {
		t.Fatalf("expected fallback result log, got %#v", logs)
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

	deadline := time.After(3 * time.Second)
	for {
		runner.mu.Lock()
		sawWriter := runner.writer != nil && runner.lazyStart && !runner.closed
		interactive := runner.interactive
		runner.mu.Unlock()
		if sawWriter {
			if interactive {
				t.Fatal("expected runner to remain non-interactive before first input")
			}
			break
		}
		select {
		case <-eventsCh:
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("lazy-start runner failed: %v", err)
			}
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			t.Fatal("did not expose lazy-start input handle")
		}
	}

	runner.mu.Lock()
	lazyStart := runner.lazyStart
	interactive := runner.interactive
	runner.mu.Unlock()
	if !lazyStart {
		t.Fatal("expected runner to remain in lazy-start mode before first input")
	}
	if interactive {
		t.Fatal("expected runner to remain non-interactive before first input")
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
			phaseEvent = &v
			break
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
			phaseEvent = &v
			break
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
	cmd := appendPermissionModeToCommand("claude --resume resume-xyz", "acceptEdits")
	if !strings.Contains(cmd, "--permission-mode acceptEdits") {
		t.Fatalf("expected acceptEdits permission mode in resume command, got %q", cmd)
	}
	if strings.Count(cmd, "--permission-mode") != 1 {
		t.Fatalf("expected single permission mode flag, got %q", cmd)
	}
	unchanged := appendPermissionModeToCommand(cmd, "default")
	if unchanged != cmd {
		t.Fatalf("expected existing permission mode to remain unchanged, got %q", unchanged)
	}
}

func TestNewClaudeStreamCommandPreservesResumeAndPermissionMode(t *testing.T) {
	cmd := newClaudeStreamCommand(context.Background(), "claude --resume resume-xyz", "resume-xyz", "acceptEdits")
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
	if !strings.Contains(joined, "--permission-mode acceptEdits") {
		t.Fatalf("expected acceptEdits permission mode, got %q", joined)
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
	runner.permissionMode = "acceptEdits"
	runner.pendingReq = ExecRequest{
		SessionID:      "s-resume-meta",
		Command:        "claude",
		Mode:           ModePTY,
		PermissionMode: "acceptEdits",
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

func TestCodexAppSessionCoalescesAssistantDeltasBeforePrompt(t *testing.T) {
	runner := NewPtyRunner()
	var events []any
	app := &codexAppSession{
		runner:    runner,
		sessionID: "s-codex-delta",
		sink:      func(event any) { events = append(events, event) },
	}
	app.setThreadID("thread-123")

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
	for _, event := range events {
		switch v := event.(type) {
		case protocol.LogEvent:
			logs = append(logs, v)
		case protocol.PromptRequestEvent:
			prompts = append(prompts, v)
		}
	}

	if len(logs) != 1 {
		t.Fatalf("expected a single coalesced log, got %#v", logs)
	}
	if logs[0].Message != "Tip : hello world" {
		t.Fatalf("unexpected coalesced log message: %q", logs[0].Message)
	}
	if len(prompts) != 1 || prompts[0].ResumeSessionID != "thread-123" {
		t.Fatalf("expected invisible continue prompt with thread id, got %#v", prompts)
	}
}

func TestCodexAppSessionWritePermissionResponseEncodesJSONRPCResult(t *testing.T) {
	buf := &nopWriteCloser{}
	runner := NewPtyRunner()
	runner.permissionMode = "acceptEdits"
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
