package runner

import (
	"context"
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
