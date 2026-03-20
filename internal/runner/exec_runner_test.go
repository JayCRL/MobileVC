package runner

import (
	"context"
	"errors"
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
