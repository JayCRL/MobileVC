package session

import (
	"strings"
	"testing"

	"mobilevc/internal/data"
	"mobilevc/internal/protocol"
)

func TestIsClaudeCommandLike(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    bool
	}{
		{"empty", "", false},
		{"whitespace", "   ", false},
		{"plain claude", "claude", true},
		{"claude with args", "claude --resume xyz", true},
		{"absolute path", "/usr/local/bin/claude", true},
		{"absolute path with args", "/usr/local/bin/claude -m sonnet", true},
		{"windows path slash", `C:/tools/claude.exe`, false}, // 仅 head==claude.exe 通过, 路径前缀失败
		{"claude.exe head", "claude.exe -h", true},
		{"uppercase Claude", "Claude --version", true},
		{"codex not claude", "codex run", false},
		{"random shell", "bash -c hi", false},
		{"path containing claude in name only", "/opt/claudette", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsClaudeCommandLike(tc.command)
			if got != tc.want {
				t.Fatalf("IsClaudeCommandLike(%q) = %v, want %v", tc.command, got, tc.want)
			}
		})
	}
}

func TestPermissionDecisionIntent(t *testing.T) {
	cases := []struct {
		name string
		req  protocol.PermissionDecisionRequestEvent
		want data.PermissionKind
	}{
		{
			name: "english read",
			req:  protocol.PermissionDecisionRequestEvent{PromptMessage: "Allow Claude to Read this file?"},
			want: "read",
		},
		{
			name: "english view",
			req:  protocol.PermissionDecisionRequestEvent{PromptMessage: "Permission to view directory"},
			want: "read",
		},
		{
			name: "chinese read",
			req:  protocol.PermissionDecisionRequestEvent{PromptMessage: "需要读取 README"},
			want: "read",
		},
		{
			name: "chinese view",
			req:  protocol.PermissionDecisionRequestEvent{PromptMessage: "需要查看目录"},
			want: "read",
		},
		{
			name: "english bash",
			req:  protocol.PermissionDecisionRequestEvent{PromptMessage: "Run Bash command?"},
			want: data.PermissionKindShell,
		},
		{
			name: "fallback target type bash",
			req:  protocol.PermissionDecisionRequestEvent{FallbackTargetType: "Bash"},
			want: data.PermissionKindShell,
		},
		{
			name: "fallback command says command",
			req:  protocol.PermissionDecisionRequestEvent{FallbackCommand: "execute command"},
			want: data.PermissionKindShell,
		},
		{
			name: "chinese command",
			req:  protocol.PermissionDecisionRequestEvent{PromptMessage: "执行命令吗？"},
			want: data.PermissionKindShell,
		},
		{
			name: "default to write",
			req:  protocol.PermissionDecisionRequestEvent{PromptMessage: "Apply edit"},
			want: data.PermissionKindWrite,
		},
		{
			name: "empty defaults to write",
			req:  protocol.PermissionDecisionRequestEvent{},
			want: data.PermissionKindWrite,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PermissionDecisionIntent(tc.req)
			if got != tc.want {
				t.Fatalf("PermissionDecisionIntent = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildPermissionDecisionPrompt(t *testing.T) {
	t.Run("empty decision rejected", func(t *testing.T) {
		_, err := BuildPermissionDecisionPrompt("", protocol.PermissionDecisionRequestEvent{})
		if err == nil {
			t.Fatal("expected error for empty decision")
		}
	})
	t.Run("unknown decision rejected", func(t *testing.T) {
		_, err := BuildPermissionDecisionPrompt("maybe", protocol.PermissionDecisionRequestEvent{})
		if err == nil {
			t.Fatal("expected error for unknown decision")
		}
	})
	t.Run("approve write default copy", func(t *testing.T) {
		got, err := BuildPermissionDecisionPrompt("approve", protocol.PermissionDecisionRequestEvent{
			TargetPath:          "/tmp/x.md",
			PermissionRequestID: "req-1",
			ResumeSessionID:     "sess-2",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "/tmp/x.md") {
			t.Errorf("expected target path in prompt: %q", got)
		}
		if !strings.Contains(got, "PermissionRequestID: req-1") {
			t.Errorf("expected request id metadata: %q", got)
		}
		if !strings.Contains(got, "ResumeSessionID: sess-2") {
			t.Errorf("expected resume id metadata: %q", got)
		}
		if !strings.Contains(got, "文件修改") {
			t.Errorf("expected default write copy: %q", got)
		}
	})
	t.Run("approve read variant", func(t *testing.T) {
		got, err := BuildPermissionDecisionPrompt("approve", protocol.PermissionDecisionRequestEvent{
			PromptMessage: "Allow read?",
			TargetPath:    "/etc/hosts",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "只读") {
			t.Errorf("expected read copy: %q", got)
		}
	})
	t.Run("approve shell variant", func(t *testing.T) {
		got, err := BuildPermissionDecisionPrompt("approve", protocol.PermissionDecisionRequestEvent{
			PromptMessage:      "Run Bash",
			FallbackTargetType: "Bash",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "命令执行") {
			t.Errorf("expected shell copy: %q", got)
		}
	})
	t.Run("deny copy", func(t *testing.T) {
		got, err := BuildPermissionDecisionPrompt("deny", protocol.PermissionDecisionRequestEvent{
			TargetPath: "/tmp/danger",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "拒绝") {
			t.Errorf("expected denial copy: %q", got)
		}
		if !strings.Contains(got, "/tmp/danger") {
			t.Errorf("expected target in deny: %q", got)
		}
	})
	t.Run("subject fallback chain", func(t *testing.T) {
		// no targetPath; fallback to ContextTitle
		got, err := BuildPermissionDecisionPrompt("approve", protocol.PermissionDecisionRequestEvent{
			ContextTitle: "feature-branch",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "feature-branch") {
			t.Errorf("expected ContextTitle as subject: %q", got)
		}
	})
	t.Run("subject empty fallback to placeholder", func(t *testing.T) {
		got, err := BuildPermissionDecisionPrompt("approve", protocol.PermissionDecisionRequestEvent{})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "刚才请求的操作") {
			t.Errorf("expected placeholder subject: %q", got)
		}
	})
	t.Run("includes original prompt", func(t *testing.T) {
		got, err := BuildPermissionDecisionPrompt("approve", protocol.PermissionDecisionRequestEvent{
			PromptMessage: "  Apply diff?  ",
			TargetPath:    "/x",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "OriginalPrompt: Apply diff?") {
			t.Errorf("expected trimmed original prompt: %q", got)
		}
	})
}

func TestBuildPermissionDecisionPlan_NonClaudeGoesDirect(t *testing.T) {
	plan, err := BuildPermissionDecisionPlan(
		protocol.PermissionDecisionRequestEvent{
			Decision:        "approve",
			FallbackCommand: "bash run.sh",
		},
		data.ProjectionSnapshot{},
		ControllerSnapshot{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != PermissionDecisionActionDirect {
		t.Errorf("expected Direct for non-claude, got %v", plan.Action)
	}
	if plan.Decision != "approve" {
		t.Errorf("decision: %q", plan.Decision)
	}
	if plan.Meta.Source != "permission-decision" {
		t.Errorf("meta source: %q", plan.Meta.Source)
	}
}

func TestBuildPermissionDecisionPlan_ClaudeApproveAutoElevates(t *testing.T) {
	plan, err := BuildPermissionDecisionPlan(
		protocol.PermissionDecisionRequestEvent{
			Decision:        "approve",
			FallbackCommand: "claude",
			PermissionMode:  "default",
		},
		data.ProjectionSnapshot{},
		ControllerSnapshot{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != PermissionDecisionActionAutoThenDirect {
		t.Errorf("expected AutoThenDirect, got %v", plan.Action)
	}
	if plan.Meta.PermissionMode != "auto" {
		t.Errorf("expected auto upgrade, got %q", plan.Meta.PermissionMode)
	}
}

func TestBuildPermissionDecisionPlan_ClaudeApproveWhenAlreadyAutoIsDirect(t *testing.T) {
	plan, err := BuildPermissionDecisionPlan(
		protocol.PermissionDecisionRequestEvent{
			Decision:        "approve",
			FallbackCommand: "claude",
			PermissionMode:  "auto",
		},
		data.ProjectionSnapshot{},
		ControllerSnapshot{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != PermissionDecisionActionDirect {
		t.Errorf("expected Direct (already auto), got %v", plan.Action)
	}
	if plan.Meta.PermissionMode != "auto" {
		t.Errorf("expected auto preserved, got %q", plan.Meta.PermissionMode)
	}
}

func TestBuildPermissionDecisionPlan_ClaudeDenyEmitsPrompt(t *testing.T) {
	plan, err := BuildPermissionDecisionPlan(
		protocol.PermissionDecisionRequestEvent{
			Decision:        "deny",
			FallbackCommand: "claude",
			TargetPath:      "/tmp/secret",
		},
		data.ProjectionSnapshot{},
		ControllerSnapshot{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != PermissionDecisionActionDenyThenInput {
		t.Errorf("expected DenyThenInput, got %v", plan.Action)
	}
	if plan.Decision != "deny" {
		t.Errorf("decision: %q", plan.Decision)
	}
	if !strings.Contains(plan.Prompt, "/tmp/secret") {
		t.Errorf("expected target in prompt: %q", plan.Prompt)
	}
}

func TestBuildPermissionDecisionPlan_FallbackChain(t *testing.T) {
	// req 没填 ResumeSessionID/CWD/Engine, 应当从 controller 和 projection 兜底
	projection := data.ProjectionSnapshot{
		Runtime: data.SessionRuntime{
			ResumeSessionID: "proj-resume",
			Command:         "claude",
			CWD:             "/proj/cwd",
			PermissionMode:  "auto",
		},
	}
	controller := ControllerSnapshot{
		ResumeSession:  "ctrl-resume",
		CurrentCommand: "ctrl-cmd",
		ActiveMeta: protocol.RuntimeMeta{
			ContextID: "ctx-controller",
			CWD:       "/ctrl/cwd",
		},
	}
	plan, err := BuildPermissionDecisionPlan(
		protocol.PermissionDecisionRequestEvent{
			Decision:        "approve",
			FallbackCommand: "claude",
		},
		projection,
		controller,
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Meta.ResumeSessionID != "ctrl-resume" {
		t.Errorf("expected controller resume to win, got %q", plan.Meta.ResumeSessionID)
	}
	if plan.Meta.ContextID != "ctx-controller" {
		t.Errorf("expected controller context, got %q", plan.Meta.ContextID)
	}
	if plan.Meta.CWD != "/ctrl/cwd" {
		t.Errorf("expected controller cwd, got %q", plan.Meta.CWD)
	}
	if plan.Action != PermissionDecisionActionDirect {
		t.Errorf("auto already, expected Direct, got %v", plan.Action)
	}
}
