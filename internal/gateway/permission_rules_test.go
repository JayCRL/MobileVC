package gateway

import (
	"testing"
	"time"

	"mobilevc/internal/data"
	"mobilevc/internal/engine"
	"mobilevc/internal/protocol"
	"mobilevc/internal/session"
)

func TestBuildPermissionRuleCarriesScopeAndContext(t *testing.T) {
	req := protocol.PermissionDecisionRequestEvent{
		Decision:        "approve",
		TargetPath:      "/workspace/lib/main.dart",
		PromptMessage:   "Allow write to lib/main.dart?",
		FallbackCommand: "bash run.sh",
		FallbackEngine:  "codex",
	}

	rule := buildPermissionRule(req, "persistent", data.ProjectionSnapshot{}, session.ControllerSnapshot{})

	if rule.Scope != data.PermissionScopePersistent {
		t.Fatalf("expected persistent scope, got %q", rule.Scope)
	}
	if !rule.Enabled {
		t.Fatal("expected rule enabled")
	}
	if rule.Engine != "codex" {
		t.Fatalf("expected codex engine, got %q", rule.Engine)
	}
	if rule.Kind != data.PermissionKindWrite {
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

func TestBuildPermissionDecisionFromEventDoesNotInheritStaleTarget(t *testing.T) {
	meta := protocol.RuntimeMeta{
		PermissionRequestID: "perm-new",
		BlockingKind:        "permission",
		Command:             "claude",
	}
	controller := session.ControllerSnapshot{
		ActiveMeta: protocol.RuntimeMeta{
			PermissionRequestID: "perm-old",
			TargetPath:          "/workspace/old.txt",
			ContextID:           "old-context",
		},
	}

	req := session.BuildPermissionDecisionFromEvent("s1", "Claude requested permissions to use Bash", meta, data.ProjectionSnapshot{}, controller)

	if req.PermissionRequestID != "perm-new" {
		t.Fatalf("expected current permission id, got %#v", req)
	}
	if req.TargetPath != "" {
		t.Fatalf("expected no stale target path, got %#v", req)
	}
	if req.ContextID != "" {
		t.Fatalf("expected no stale context id, got %#v", req)
	}
}

func TestMaybeAutoApplyPermissionEventIgnoresReadyPrompt(t *testing.T) {
	sessionStore, err := data.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}

	sessionID := "session-ready-prompt"
	created, err := sessionStore.CreateSession(t.Context(), sessionID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sessionID = created.ID
	projection := data.ProjectionSnapshot{
		PermissionRulesEnabled: true,
		PermissionRules: []data.PermissionRule{{
			ID:               "session|claude|write|claude|/workspace/lib",
			Scope:            data.PermissionScopeSession,
			Enabled:          true,
			Engine:           "claude",
			Kind:             data.PermissionKindWrite,
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

	service := session.NewService(sessionID, session.Dependencies{})
	applied, err := maybeAutoApplyPermissionEvent(t.Context(), sessionStore, sessionID, event, service, func(any) {}, func(any) {})
	if err != nil {
		t.Fatalf("maybe auto apply permission event: %v", err)
	}
	if applied {
		t.Fatal("expected ready prompt not to trigger permission auto-apply")
	}
}

func TestMaybeAutoApplyPermissionEventUsesDirectApproveForSessionRule(t *testing.T) {
	sessionStore, err := data.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}

	sessionID := "session-auto-approve"
	created, err := sessionStore.CreateSession(t.Context(), sessionID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sessionID = created.ID
	projection := data.ProjectionSnapshot{
		PermissionRulesEnabled: true,
		PermissionRules: []data.PermissionRule{{
			ID:               "session|claude|write|claude|/workspace/lib/main.dart",
			Scope:            data.PermissionScopeSession,
			Enabled:          true,
			Engine:           "claude",
			Kind:             data.PermissionKindWrite,
			CommandHead:      "claude",
			TargetPathPrefix: "/workspace/lib/main.dart",
			Summary:          "allow main.dart edits",
		}},
	}
	if _, err := sessionStore.SaveProjection(t.Context(), sessionID, projection); err != nil {
		t.Fatalf("save projection: %v", err)
	}

	firstRunner := newHoldingStubRunner()
	firstRunner.currentPermissionRequestID = "perm-main"
	runnerIndex := 0
	service := session.NewService(sessionID, session.Dependencies{
		NewPtyRunner: func() engine.Runner {
			runnerIndex++
			return firstRunner
		},
		NewExecRunner: func() engine.Runner { return newHoldingStubRunner() },
	})
	if err := service.Execute(t.Context(), sessionID, session.ExecuteRequest{
		Command:        "claude",
		CWD:            "/workspace",
		Mode:           engine.ModePTY,
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
			Engine:              "claude",
			Command:             "claude --resume resume-123",
			CWD:                 "/workspace",
			PermissionMode:      "default",
			ResumeSessionID:     "resume-123",
			BlockingKind:        "permission",
			TargetPath:          "/workspace/lib/main.dart",
			PermissionRequestID: "perm-main",
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
	select {
	case decision := <-firstRunner.permissionResponseWriteCh:
		if decision != "approve" {
			t.Fatalf("unexpected direct permission decision: %q", decision)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive direct permission decision")
	}
	if runnerIndex != 1 {
		t.Fatalf("expected no runner restart, got runner count=%d", runnerIndex)
	}
	select {
	case payload := <-firstRunner.writeCh:
		t.Fatalf("unexpected continuation payload: %q", string(payload))
	case <-time.After(200 * time.Millisecond):
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
	items := []data.PermissionRule{
		{
			ID:               "session|codex|write|bash|/workspace/lib",
			Scope:            data.PermissionScopeSession,
			Enabled:          true,
			Engine:           "codex",
			Kind:             data.PermissionKindWrite,
			CommandHead:      "bash",
			TargetPathPrefix: "/workspace/lib",
		},
		{
			ID:          "persistent|codex|shell|python|",
			Scope:       data.PermissionScopePersistent,
			Enabled:     true,
			Engine:      "codex",
			Kind:        data.PermissionKindShell,
			CommandHead: "python",
		},
	}

	match, ok := session.MatchPermissionRule(items, session.PermissionMatchContext{
		Engine:      "codex",
		Kind:        data.PermissionKindWrite,
		CommandHead: "bash",
		TargetPath:  "/workspace/lib/main.dart",
	})
	if !ok {
		t.Fatal("expected a matching rule")
	}
	if match.ID != "session|codex|write|bash|/workspace/lib" {
		t.Fatalf("unexpected match id %q", match.ID)
	}

	if _, ok := session.MatchPermissionRule(items, session.PermissionMatchContext{
		Engine:      "codex",
		Kind:        data.PermissionKindWrite,
		CommandHead: "bash",
		TargetPath:  "/workspace/test/main.dart",
	}); ok {
		t.Fatal("expected prefix mismatch to fail")
	}

	if _, ok := session.MatchPermissionRule(items, session.PermissionMatchContext{
		Engine:      "codex",
		Kind:        data.PermissionKindNetwork,
		CommandHead: "python",
		TargetPath:  "",
	}); ok {
		t.Fatal("expected kind mismatch to fail")
	}
}
