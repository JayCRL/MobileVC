package ws

import (
	"testing"

	"mobilevc/internal/protocol"
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
