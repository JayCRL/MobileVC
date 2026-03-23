package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mobilevc/internal/protocol"
)

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

func TestPtyRunnerClaudeLazyStartExposesPromptBeforeInput(t *testing.T) {
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

	var sawPrompt bool
	deadline := time.After(3 * time.Second)
	for !sawPrompt {
		select {
		case event := <-eventsCh:
			if prompt, ok := event.(protocol.PromptRequestEvent); ok && strings.Contains(prompt.Message, "Claude 会话已就绪") {
				sawPrompt = true
			}
		case <-deadline:
			t.Fatal("did not receive lazy-start prompt")
		}
	}

	runner.mu.Lock()
	lazyStart := runner.lazyStart
	runner.mu.Unlock()
	if !lazyStart {
		t.Fatal("expected runner to remain in lazy-start mode before first input")
	}

	cancel()
	select {
	case <-errCh:
	case <-time.After(3 * time.Second):
		t.Fatal("runner did not exit after cancel")
	}
}

func TestPtyRunnerSyntheticFileDiffAfterEditToolResult(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "hello.txt")
	if err := os.WriteFile(filePath, []byte("alpha\n"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}

	runner := NewPtyRunner()
	runner.currentDir = tempDir

	toolInput, err := json.Marshal(map[string]any{"file_path": filePath})
	if err != nil {
		t.Fatalf("marshal tool input: %v", err)
	}

	var events []any
	sink := func(event any) { events = append(events, event) }

	assistantEnvelope, err := json.Marshal(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": []map[string]any{{
				"type":  "tool_use",
				"name":  "Edit",
				"input": json.RawMessage(toolInput),
			}},
		},
	})
	if err != nil {
		t.Fatalf("marshal assistant envelope: %v", err)
	}
	userEnvelope, err := json.Marshal(map[string]any{
		"type": "user",
		"tool_use_result": map[string]any{
			"type":     "tool completed",
			"filePath": filePath,
		},
	})
	if err != nil {
		t.Fatalf("marshal user envelope: %v", err)
	}

	reader := strings.NewReader(string(assistantEnvelope) + "\n")
	runner.readClaudeStreamJSON(context.Background(), reader, "s4", sink)

	if err := os.WriteFile(filePath, []byte("beta\n"), 0o644); err != nil {
		t.Fatalf("write updated file: %v", err)
	}
	reader = strings.NewReader(string(userEnvelope) + "\n")
	runner.readClaudeStreamJSON(context.Background(), reader, "s4", sink)

	var diffEvent *protocol.FileDiffEvent
	for _, event := range events {
		if v, ok := event.(protocol.FileDiffEvent); ok {
			diffEvent = &v
			break
		}
	}
	if diffEvent == nil {
		t.Fatalf("expected synthetic file diff event, got %#v", events)
	}
	if diffEvent.Path != "hello.txt" {
		t.Fatalf("expected relative path hello.txt, got %q", diffEvent.Path)
	}
	if !strings.Contains(diffEvent.Diff, "-alpha") || !strings.Contains(diffEvent.Diff, "+beta") {
		t.Fatalf("expected diff to contain old/new content, got %q", diffEvent.Diff)
	}
}
