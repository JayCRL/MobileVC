package runner

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"mobilevc/internal/protocol"
)

func TestExecRunnerEmitsLogEvent(t *testing.T) {
	runner := NewExecRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var events []any
	err := runner.Run(ctx, ExecRequest{
		SessionID: "s1",
		Command:   shellTestCommand("printf 'hello\\nworld\\n'", "Write-Output 'hello'; Write-Output 'world'", "echo hello && echo world"),
	}, func(event any) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatalf("run command: %v", err)
	}

	var foundHello bool
	for _, event := range events {
		logEvent, ok := event.(protocol.LogEvent)
		if !ok {
			continue
		}
		if strings.Contains(logEvent.Message, "hello") && logEvent.Stream == "stdout" {
			foundHello = true
		}
	}

	if !foundHello {
		t.Fatalf("expected stdout log event, got %#v", events)
	}
}

func TestExecRunnerEmitsErrorEvent(t *testing.T) {
	runner := NewExecRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var events []any
	err := runner.Run(ctx, ExecRequest{
		SessionID: "s1",
		Command:   shellTestCommand("printf 'boom\\n' >&2; exit 7", "[Console]::Error.WriteLine('boom'); exit 7", "echo boom 1>&2 && exit /b 7"),
	}, func(event any) {
		events = append(events, event)
	})
	if err == nil {
		t.Fatal("expected command failure")
	}

	var foundStderrLog bool
	var foundError bool
	for _, event := range events {
		switch v := event.(type) {
		case protocol.LogEvent:
			if strings.Contains(v.Message, "boom") && v.Stream == "stderr" {
				foundStderrLog = true
			}
		case protocol.ErrorEvent:
			if v.Message == "command exited with code 7" {
				foundError = true
			}
		}
	}

	if !foundStderrLog {
		t.Fatalf("expected stderr log event, got %#v", events)
	}
	if !foundError {
		t.Fatalf("expected error event, got %#v", events)
	}
}

func TestExecRunnerEmitsExecutionLifecycle(t *testing.T) {
	runner := NewExecRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var events []any
	err := runner.Run(ctx, ExecRequest{
		SessionID: "s1",
		Command:   shellTestCommand("printf 'hello\n'", "Write-Output 'hello'", "echo hello"),
	}, func(event any) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatalf("run command: %v", err)
	}

	var started, stdout, finished *protocol.LogEvent
	for _, event := range events {
		logEvent, ok := event.(protocol.LogEvent)
		if !ok {
			continue
		}
		switch logEvent.Phase {
		case "started":
			copy := logEvent
			started = &copy
		case "stdout":
			if strings.Contains(logEvent.Message, "hello") {
				copy := logEvent
				stdout = &copy
			}
		case "finished":
			copy := logEvent
			finished = &copy
		}
	}

	if started == nil {
		t.Fatalf("expected started log event, got %#v", events)
	}
	if strings.TrimSpace(started.ExecutionID) == "" {
		t.Fatalf("expected execution id on started event, got %#v", started)
	}
	if stdout == nil {
		t.Fatalf("expected stdout log event, got %#v", events)
	}
	if stdout.ExecutionID != started.ExecutionID {
		t.Fatalf("expected stdout execution id %q, got %#v", started.ExecutionID, stdout)
	}
	if finished == nil {
		t.Fatalf("expected finished log event, got %#v", events)
	}
	if finished.ExecutionID != started.ExecutionID {
		t.Fatalf("expected finished execution id %q, got %#v", started.ExecutionID, finished)
	}
	if finished.ExitCode == nil || *finished.ExitCode != 0 {
		t.Fatalf("expected exitCode=0, got %#v", finished)
	}
}

func TestExecRunnerEmitsExecutionLifecycleOnFailure(t *testing.T) {
	runner := NewExecRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var events []any
	err := runner.Run(ctx, ExecRequest{
		SessionID: "s1",
		Command:   shellTestCommand("printf 'boom\n' >&2; exit 7", "[Console]::Error.WriteLine('boom'); exit 7", "echo boom 1>&2 && exit /b 7"),
	}, func(event any) {
		events = append(events, event)
	})
	if err == nil {
		t.Fatal("expected command failure")
	}

	var started, stderr, finished *protocol.LogEvent
	for _, event := range events {
		logEvent, ok := event.(protocol.LogEvent)
		if !ok {
			continue
		}
		switch logEvent.Phase {
		case "started":
			copy := logEvent
			started = &copy
		case "stderr":
			if strings.Contains(logEvent.Message, "boom") {
				copy := logEvent
				stderr = &copy
			}
		case "finished":
			copy := logEvent
			finished = &copy
		}
	}

	if started == nil || stderr == nil || finished == nil {
		t.Fatalf("expected started/stderr/finished events, got %#v", events)
	}
	if stderr.ExecutionID != started.ExecutionID || finished.ExecutionID != started.ExecutionID {
		t.Fatalf("expected same execution id across lifecycle, got started=%#v stderr=%#v finished=%#v", started, stderr, finished)
	}
	if finished.ExitCode == nil || *finished.ExitCode != 7 {
		t.Fatalf("expected exitCode=7, got %#v", finished)
	}
}

func TestExecRunnerWriteNotSupported(t *testing.T) {
	runner := NewExecRunner()
	if err := runner.Write(context.Background(), []byte("y\\n")); !errors.Is(err, ErrInputNotSupported) {
		t.Fatalf("expected ErrInputNotSupported, got %v", err)
	}
}

func TestNewShellCommandUsesPowerShellForClaudeOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}

	cmd := newShellCommand(context.Background(), "claude", ModePTY)
	path := strings.ToLower(cmd.Path)
	if !strings.HasSuffix(path, "bash.exe") {
		t.Fatalf("expected bash entry for interactive claude, got %q", cmd.Path)
	}
	args := strings.Join(cmd.Args, " ")
	lowerArgs := strings.ToLower(args)
	if !strings.Contains(lowerArgs, "-lc") || !strings.Contains(lowerArgs, "winpty") || !strings.Contains(lowerArgs, "claude") {
		t.Fatalf("expected bash wrapped winpty claude invocation, got %q", args)
	}
}

func TestBuildClaudePromptCommandIncludesResume(t *testing.T) {
	got := buildClaudePromptCommand("claude", "hello", "session-123")
	lower := strings.ToLower(got)
	if !strings.Contains(lower, "--resume session-123") {
		t.Fatalf("expected resume flag in %q", got)
	}
}

func TestBuildClaudeStreamJSONCommandIncludesStructuredIOFlags(t *testing.T) {
	got := buildClaudeStreamJSONCommand("claude")
	lower := strings.ToLower(got)
	for _, expected := range []string{
		"--output-format stream-json",
		"--input-format stream-json",
		"--permission-prompt-tool stdio",
	} {
		if !strings.Contains(lower, expected) {
			t.Fatalf("expected %q in %q", expected, got)
		}
	}
}

func TestBuildClaudeStreamJSONCommandDoesNotDuplicatePermissionPromptTool(t *testing.T) {
	got := buildClaudeStreamJSONCommand("claude --permission-prompt-tool stdio")
	if strings.Count(strings.ToLower(got), "--permission-prompt-tool") != 1 {
		t.Fatalf("expected single --permission-prompt-tool in %q", got)
	}
}

func TestShellEnvironmentRemovesClaudeCodeForClaudeCommand(t *testing.T) {
	const key = "CLAUDECODE"
	old := os.Getenv(key)
	_ = os.Setenv(key, "1")
	defer func() {
		if old == "" {
			_ = os.Unsetenv(key)
		} else {
			_ = os.Setenv(key, old)
		}
	}()

	env := shellEnvironment(getShellSpec(), "claude")
	for _, item := range env {
		if strings.HasPrefix(strings.ToUpper(item), key+"=") {
			t.Fatalf("expected %s to be removed for claude command, got %q", key, item)
		}
	}
}

func shellTestCommand(posix, powershell, cmd string) string {
	spec := getShellSpec()
	if len(spec.args) > 0 {
		switch spec.args[0] {
		case "-NoLogo":
			return powershell
		case "/C":
			return cmd
		}
	}
	return posix
}
