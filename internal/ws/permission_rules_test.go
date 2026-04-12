package ws

import (
	"strings"
	"testing"
	"time"

	"mobilevc/internal/protocol"
	"mobilevc/internal/runner"
	runtimepkg "mobilevc/internal/runtime"
	"mobilevc/internal/session"
	"mobilevc/internal/store"
)

func TestBuildPermissionRuleCarriesScopeAndContext(t *testing.T) {
	req := protocol.PermissionDecisionRequestEvent{
		Decision:        "approve",
		TargetPath:      "/workspace/lib/main.dart",
		PromptMessage:   "Allow write to lib/main.dart?",
		FallbackCommand: "bash run.sh",
		FallbackEngine:  "codex",
	}

	rule := buildPermissionRule(req, "persistent", store.ProjectionSnapshot{}, session.ControllerSnapshot{})

	if rule.Scope != store.PermissionScopePersistent {
		t.Fatalf("expected persistent scope, got %q", rule.Scope)
	}
	if !rule.Enabled {
		t.Fatal("expected rule enabled")
	}
	if rule.Engine != "codex" {
		t.Fatalf("expected codex engine, got %q", rule.Engine)
	}
	if rule.Kind != store.PermissionKindWrite {
		t.Fatalf("expected write kind, got %q", rule.Kind)
	}
	if rule.CommandHead != "bash" {
		t.Fatalf("expected bash command head, got %q", rule.CommandHead)
	}
	if rule.TargetPathPrefix != "/workspace/lib/main.dart" {
		t.Fatalf("unexpected target path prefix %q", rule.TargetPathPrefix)
	}
	if rule.ID == "" {
		t.Fatal("expected generated rule id")
	}
}

func TestMaybeAutoApplyPermissionEventIgnoresReadyPrompt(t *testing.T) {
	sessionStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}

	sessionID := "session-ready-prompt"
	created, err := sessionStore.CreateSession(t.Context(), sessionID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sessionID = created.ID
	projection := store.ProjectionSnapshot{
		PermissionRulesEnabled: true,
		PermissionRules: []store.PermissionRule{{
			ID:               "session|claude|write|claude|/workspace/lib",
			Scope:            store.PermissionScopeSession,
			Enabled:          true,
			Engine:           "claude",
			Kind:             store.PermissionKindWrite,
			CommandHead:      "claude",
			TargetPathPrefix: "/workspace/lib",
		}},
	}
	if _, err := sessionStore.SaveProjection(t.Context(), sessionID, projection); err != nil {
		t.Fatalf("save projection: %v", err)
	}

	event := protocol.ApplyRuntimeMeta(
		protocol.NewPromptRequestEvent(sessionID, "等待输入", nil),
		protocol.RuntimeMeta{
			Engine:     "claude",
			Command:    "claude --resume resume-123",
			TargetPath: "/workspace/lib/main.dart",
		},
	)

	service := runtimepkg.NewService(sessionID, runtimepkg.Dependencies{})
	applied, err := maybeAutoApplyPermissionEvent(t.Context(), sessionStore, sessionID, event, service, func(any) {}, func(any) {})
	if err != nil {
		t.Fatalf("maybe auto apply permission event: %v", err)
	}
	if applied {
		t.Fatal("expected ready prompt not to trigger permission auto-apply")
	}
}

func TestMaybeAutoApplyPermissionEventUsesHotSwapApproveForSessionRule(t *testing.T) {
	sessionStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}

	sessionID := "session-auto-approve"
	created, err := sessionStore.CreateSession(t.Context(), sessionID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sessionID = created.ID
	projection := store.ProjectionSnapshot{
		PermissionRulesEnabled: true,
		PermissionRules: []store.PermissionRule{{
			ID:               "session|claude|write|claude|/workspace/lib/main.dart",
			Scope:            store.PermissionScopeSession,
			Enabled:          true,
			Engine:           "claude",
			Kind:             store.PermissionKindWrite,
			CommandHead:      "claude",
			TargetPathPrefix: "/workspace/lib/main.dart",
			Summary:          "allow main.dart edits",
		}},
	}
	if _, err := sessionStore.SaveProjection(t.Context(), sessionID, projection); err != nil {
		t.Fatalf("save projection: %v", err)
	}

	firstRunner := newHoldingStubRunner()
	secondRunner := newHoldingStubRunner()
	runnerIndex := 0
	service := runtimepkg.NewService(sessionID, runtimepkg.Dependencies{
		NewPtyRunner: func() runner.Runner {
			runnerIndex++
			if runnerIndex == 1 {
				return firstRunner
			}
			return secondRunner
		},
		NewExecRunner: func() runner.Runner { return newHoldingStubRunner() },
	})
	if err := service.Execute(t.Context(), sessionID, runtimepkg.ExecuteRequest{
		Command:        "claude",
		CWD:            "/workspace",
		Mode:           runner.ModePTY,
		PermissionMode: "default",
		RuntimeMeta: protocol.RuntimeMeta{
			Command:         "claude",
			CWD:             "/workspace",
			ResumeSessionID: "resume-123",
			PermissionMode:  "default",
			TargetPath:      "/workspace/lib/main.dart",
		},
	}, func(any) {}); err != nil {
		t.Fatalf("execute service: %v", err)
	}
	firstRunner.WaitStarted(t)

	event := protocol.ApplyRuntimeMeta(
		protocol.NewPromptRequestEvent(sessionID, "Claude requested permissions to use Edit on /workspace/lib/main.dart", []string{"y", "n"}),
		protocol.RuntimeMeta{
			Engine:          "claude",
			Command:         "claude --resume resume-123",
			CWD:             "/workspace",
			PermissionMode:  "default",
			ResumeSessionID: "resume-123",
			BlockingKind:    "permission",
			TargetPath:      "/workspace/lib/main.dart",
		},
	)

	var emitted []any
	applied, err := maybeAutoApplyPermissionEvent(t.Context(), sessionStore, sessionID, event, service, func(evt any) {
		emitted = append(emitted, evt)
	}, func(evt any) {
		emitted = append(emitted, evt)
	})
	if err != nil {
		t.Fatalf("maybe auto apply permission event: %v", err)
	}
	if !applied {
		t.Fatal("expected session rule to auto-apply")
	}
	firstRunner.WaitClosed(t)
	secondRunner.WaitStarted(t)
	select {
	case payload := <-secondRunner.writeCh:
		if got := string(payload); !strings.Contains(got, "先使用 Read 读取目标文件的当前内容") {
			t.Fatalf("expected hot-swap continuation payload, got %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive hot-swap continuation payload")
	}

	record, err := sessionStore.GetSession(t.Context(), sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if len(record.Projection.PermissionRules) != 1 {
		t.Fatalf("expected one session rule, got %#v", record.Projection.PermissionRules)
	}
	if record.Projection.PermissionRules[0].MatchCount != 1 {
		t.Fatalf("expected match count increment, got %#v", record.Projection.PermissionRules[0])
	}
}

func TestMatchPermissionRuleHonorsPrefixAndKind(t *testing.T) {
	items := []store.PermissionRule{
		{
			ID:               "session|codex|write|bash|/workspace/lib",
			Scope:            store.PermissionScopeSession,
			Enabled:          true,
			Engine:           "codex",
			Kind:             store.PermissionKindWrite,
			CommandHead:      "bash",
			TargetPathPrefix: "/workspace/lib",
		},
		{
			ID:          "persistent|codex|shell|python|",
			Scope:       store.PermissionScopePersistent,
			Enabled:     true,
			Engine:      "codex",
			Kind:        store.PermissionKindShell,
			CommandHead: "python",
		},
	}

	match, ok := matchPermissionRule(items, permissionMatchContext{
		Engine:      "codex",
		Kind:        store.PermissionKindWrite,
		CommandHead: "bash",
		TargetPath:  "/workspace/lib/main.dart",
	})
	if !ok {
		t.Fatal("expected a matching rule")
	}
	if match.ID != "session|codex|write|bash|/workspace/lib" {
		t.Fatalf("unexpected match id %q", match.ID)
	}

	if _, ok := matchPermissionRule(items, permissionMatchContext{
		Engine:      "codex",
		Kind:        store.PermissionKindWrite,
		CommandHead: "bash",
		TargetPath:  "/workspace/test/main.dart",
	}); ok {
		t.Fatal("expected prefix mismatch to fail")
	}

	if _, ok := matchPermissionRule(items, permissionMatchContext{
		Engine:      "codex",
		Kind:        store.PermissionKindNetwork,
		CommandHead: "python",
		TargetPath:  "",
	}); ok {
		t.Fatal("expected kind mismatch to fail")
	}
}
