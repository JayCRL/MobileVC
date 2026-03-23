package ws

import (
	"context"
	"strings"
	"testing"

	"mobilevc/internal/protocol"
	"mobilevc/internal/runner"
	runtimepkg "mobilevc/internal/runtime"
)

type stubSkillLauncher struct {
	buildReq    runtimepkg.ExecuteRequest
	buildErr    error
	extractText string
	calls       int
}

func (s *stubSkillLauncher) BuildRequest(name, engine, cwd, targetType, targetPath, targetTitle, targetDiff, contextID, contextTitle, targetText, targetStack string) (runtimepkg.ExecuteRequest, error) {
	s.calls++
	if s.buildErr != nil {
		return runtimepkg.ExecuteRequest{}, s.buildErr
	}
	if s.buildReq.Command == "" {
		s.buildReq = runtimepkg.ExecuteRequest{Command: "claude \"prompt\"", CWD: cwd, Mode: runner.ModeExec}
	}
	return s.buildReq, nil
}

func (s *stubSkillLauncher) ExtractPrompt(command string) string {
	if s.extractText != "" {
		return s.extractText
	}
	return "prompt"
}

func collectEmit(events *[]any) func(any) {
	return func(event any) {
		*events = append(*events, event)
	}
}

func TestParseSlashCommandErrors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "empty", input: "", wantErr: "slash command is required"},
		{name: "missing slash", input: "help", wantErr: "slash command must start with /"},
		{name: "unsupported", input: "/unknown", wantErr: "unsupported slash command: /unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseSlashCommand(tt.input)
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("expected error %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestParseSlashCommandLongestPrefixWins(t *testing.T) {
	parsed, err := parseSlashCommand("/git commit hello")
	if err != nil {
		t.Fatalf("parse slash command: %v", err)
	}
	if parsed.spec.key != "/git commit" {
		t.Fatalf("expected /git commit, got %s", parsed.spec.key)
	}
	if parsed.args != "hello" {
		t.Fatalf("expected args hello, got %q", parsed.args)
	}
}

func TestBuildExecRequestFromSlash(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		cwd         string
		wantCommand string
		wantErr     string
	}{
		{name: "init", command: "/init", cwd: "/tmp/demo", wantCommand: "claude /init"},
		{name: "compact", command: "/compact", cwd: "/tmp/demo", wantCommand: "claude /compact"},
		{name: "run", command: "/run echo hi", cwd: "/tmp/demo", wantCommand: "echo hi"},
		{name: "add dir missing args", command: "/add-dir", wantErr: "/add-dir requires arguments"},
		{name: "add dir", command: "/add-dir /tmp/demo", cwd: "/tmp/demo", wantCommand: "claude /add-dir /tmp/demo"},
		{name: "git commit quotes arg", command: "/git commit hello", wantCommand: "git commit -m \"hello\""},
		{name: "git commit keeps quoted arg", command: "/git commit \"hello\"", wantCommand: "git commit -m \"hello\""},
		{name: "test with args still fallback", command: "/test path/to/file", wantCommand: "go test ./..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := parseSlashCommand(tt.command)
			if err != nil {
				t.Fatalf("parse slash command: %v", err)
			}
			req, err := buildExecRequestFromSlash(parsed, protocol.SlashCommandRequestEvent{CWD: tt.cwd, PermissionMode: "acceptEdits"})
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("expected error %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("build exec request: %v", err)
			}
			if req.Command != tt.wantCommand {
				t.Fatalf("expected command %q, got %q", tt.wantCommand, req.Command)
			}
			if req.CWD != fallback(tt.cwd, ".") || req.PermissionMode != "acceptEdits" {
				t.Fatalf("unexpected request metadata: %#v", req)
			}
			if req.RuntimeMeta.Source != "slash-command" {
				t.Fatalf("expected slash-command source, got %#v", req.RuntimeMeta)
			}
		})
	}
}

func TestBuildSkillRequestFromSlash(t *testing.T) {
	parsed, err := parseSlashCommand("/review")
	if err != nil {
		t.Fatalf("parse slash command: %v", err)
	}

	_, err = buildSkillRequestFromSlash(parsed, protocol.SlashCommandRequestEvent{})
	if err == nil || err.Error() != "/review requires targetType context" {
		t.Fatalf("expected targetType error, got %v", err)
	}

	req, err := buildSkillRequestFromSlash(parsed, protocol.SlashCommandRequestEvent{
		Engine:       "claude",
		CWD:          "/tmp/demo",
		TargetType:   "diff",
		TargetPath:   "internal/ws/handler.go",
		TargetDiff:   "diff --git a b",
		TargetTitle:  "最近 Diff",
		ContextID:    "diff:1",
		ContextTitle: "最近 Diff",
	})
	if err != nil {
		t.Fatalf("build skill request: %v", err)
	}
	if req.Name != "review" || req.TargetType != "diff" || req.TargetDiff == "" {
		t.Fatalf("unexpected skill request: %#v", req)
	}
}

func TestHandleLocalSlashCommand(t *testing.T) {
	t.Run("clear local only", func(t *testing.T) {
		parsed, err := parseSlashCommand("/clear")
		if err != nil {
			t.Fatalf("parse slash command: %v", err)
		}
		var events []any
		if err := handleLocalSlashCommand("s1", parsed, protocol.SlashCommandRequestEvent{}, collectEmit(&events)); err != nil {
			t.Fatalf("handle local slash command: %v", err)
		}
		result, ok := events[0].(protocol.RuntimeInfoResultEvent)
		if !ok {
			t.Fatalf("expected runtime info result, got %#v", events)
		}
		if len(result.Items) == 0 || result.Items[0].Status != "local-only" {
			t.Fatalf("expected local-only item, got %#v", result)
		}
	})

	t.Run("diff requires target diff", func(t *testing.T) {
		parsed, err := parseSlashCommand("/diff")
		if err != nil {
			t.Fatalf("parse slash command: %v", err)
		}
		var events []any
		err = handleLocalSlashCommand("s1", parsed, protocol.SlashCommandRequestEvent{}, collectEmit(&events))
		if err == nil || err.Error() != "/diff requires targetDiff context" {
			t.Fatalf("expected diff context error, got %v", err)
		}
	})

	t.Run("diff emits file diff", func(t *testing.T) {
		parsed, err := parseSlashCommand("/diff")
		if err != nil {
			t.Fatalf("parse slash command: %v", err)
		}
		var events []any
		err = handleLocalSlashCommand("s1", parsed, protocol.SlashCommandRequestEvent{
			TargetPath:   "internal/ws/handler.go",
			TargetTitle:  "最近 Diff",
			TargetDiff:   "diff --git a/internal/ws/handler.go b/internal/ws/handler.go",
			ContextID:    "diff:1",
			ContextTitle: "最近 Diff",
		}, collectEmit(&events))
		if err != nil {
			t.Fatalf("handle local slash command: %v", err)
		}
		diff, ok := events[0].(protocol.FileDiffEvent)
		if !ok {
			t.Fatalf("expected file diff event, got %#v", events)
		}
		if diff.RuntimeMeta.Source != "slash-command" {
			t.Fatalf("expected slash-command source, got %#v", diff.RuntimeMeta)
		}
	})
}

func TestExecuteSkillRequestUsesSendInputWhenRunnerActive(t *testing.T) {
	ptyRunner := newHoldingStubRunner(protocol.NewPromptRequestEvent("ignored", "等待输入", nil))
	runtimeSvc := runtimepkg.NewService("s1", runtimepkg.Dependencies{
		NewPtyRunner:  func() runner.Runner { return ptyRunner },
		NewExecRunner: func() runner.Runner { return newStubRunner() },
	})
	if err := runtimeSvc.Execute(context.Background(), "s1", runtimepkg.ExecuteRequest{Command: "claude", Mode: runner.ModePTY}, func(any) {}); err != nil {
		t.Fatalf("start runtime service runner: %v", err)
	}
	launcher := &stubSkillLauncher{buildReq: runtimepkg.ExecuteRequest{Command: "claude \"review prompt\"", Mode: runner.ModeExec}, extractText: "review prompt"}
	if err := executeSkillRequest(context.Background(), "s1", protocol.SkillRequestEvent{Name: "review", CWD: ".", TargetType: "diff", TargetDiff: "diff --git a b"}, runtimeSvc, launcher, func(any) {}); err != nil {
		t.Fatalf("execute skill request: %v", err)
	}
	select {
	case payload := <-ptyRunner.writeCh:
		if string(payload) != "review prompt\n" {
			t.Fatalf("expected send input payload, got %q", string(payload))
		}
	default:
		t.Fatal("expected prompt to be sent to active runner")
	}
}

func TestExecuteSkillRequestWithoutRunnerExecutesNormally(t *testing.T) {
	execRunner := newHoldingStubRunner(protocol.NewPromptRequestEvent("ignored", "done", nil))
	runtimeSvc := runtimepkg.NewService("s1", runtimepkg.Dependencies{
		NewExecRunner: func() runner.Runner { return execRunner },
		NewPtyRunner:  func() runner.Runner { return newStubRunner() },
	})
	launcher := &stubSkillLauncher{buildReq: runtimepkg.ExecuteRequest{Command: "claude \"review prompt\"", Mode: runner.ModeExec}}
	var events []any
	if err := executeSkillRequest(context.Background(), "s1", protocol.SkillRequestEvent{Name: "review", CWD: ".", TargetType: "diff", TargetDiff: "diff --git a b"}, runtimeSvc, launcher, collectEmit(&events)); err != nil {
		t.Fatalf("execute skill request: %v", err)
	}
	if !runtimeSvc.IsRunning() {
		t.Fatal("expected runtime service to start runner for skill execution")
	}
}

func TestHandleSlashCommandSkillLauncherNil(t *testing.T) {
	runtimeSvc := runtimepkg.NewService("s1", runtimepkg.Dependencies{})
	err := handleSlashCommand(context.Background(), "s1", protocol.SlashCommandRequestEvent{Command: "/review", TargetType: "diff"}, runtimeSvc, nil, func(any) {})
	if err == nil || err.Error() != "skill launcher is unavailable" {
		t.Fatalf("expected launcher unavailable error, got %v", err)
	}
}

func TestHandleSlashCommandRuntimeInfoAndLocalOnly(t *testing.T) {
	runtimeSvc := runtimepkg.NewService("s1", runtimepkg.Dependencies{})
	launcher := &stubSkillLauncher{}

	t.Run("help", func(t *testing.T) {
		var events []any
		err := handleSlashCommand(context.Background(), "s1", protocol.SlashCommandRequestEvent{Command: "/help", CWD: "."}, runtimeSvc, launcher, collectEmit(&events))
		if err != nil {
			t.Fatalf("handle slash command: %v", err)
		}
		if _, ok := events[0].(protocol.RuntimeInfoResultEvent); !ok {
			t.Fatalf("expected runtime info result, got %#v", events)
		}
	})

	t.Run("clear", func(t *testing.T) {
		var events []any
		err := handleSlashCommand(context.Background(), "s1", protocol.SlashCommandRequestEvent{Command: "/clear"}, runtimeSvc, launcher, collectEmit(&events))
		if err != nil {
			t.Fatalf("handle slash command: %v", err)
		}
		result := events[0].(protocol.RuntimeInfoResultEvent)
		if len(result.Items) == 0 || result.Items[0].Status != "local-only" {
			t.Fatalf("unexpected runtime info result: %#v", result)
		}
	})
}

func TestGuessLangFromPath(t *testing.T) {
	tests := map[string]string{
		"a.go":  "go",
		"a.ts":  "javascript",
		"a.py":  "python",
		"a.txt": "text",
	}
	for input, want := range tests {
		if got := guessLangFromPath(input); got != want {
			t.Fatalf("guessLangFromPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsQuotedArgument(t *testing.T) {
	if !isQuotedArgument("\"hello\"") {
		t.Fatal("expected double quoted argument to be detected")
	}
	if !isQuotedArgument("'hello'") {
		t.Fatal("expected single quoted argument to be detected")
	}
	if isQuotedArgument("hello") {
		t.Fatal("did not expect unquoted argument to be detected")
	}
}

func TestSlashCommandHelpContainsUnifiedProtocolHint(t *testing.T) {
	result, err := runtimepkg.BuildRuntimeInfoResult("s1", "help", ".", nil)
	if err != nil {
		t.Fatalf("build runtime info result: %v", err)
	}
	found := false
	for _, item := range result.Items {
		if item.Label == "slash_commands" && strings.Contains(item.Detail, "slash_command action") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected slash_commands item to mention slash_command action, got %#v", result.Items)
	}
}
