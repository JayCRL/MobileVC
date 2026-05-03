package session

import (
	"testing"

	"mobilevc/internal/protocol"
)

func TestControllerPromptEventForcesWaitingInputLifecycle(t *testing.T) {
	controller := NewController("s1")
	controller.OnExecStart("claude", protocol.RuntimeMeta{Command: "claude", ClaudeLifecycle: "starting"})
	events := controller.OnRunnerEvent(protocol.ApplyRuntimeMeta(
		protocol.NewPromptRequestEvent("s1", "继续输入", nil),
		protocol.RuntimeMeta{ClaudeLifecycle: "starting", ResumeSessionID: "resume-1"},
	))
	if len(events) != 1 {
		t.Fatalf("expected one agent state event, got %#v", events)
	}
	agent, ok := events[0].(protocol.AgentStateEvent)
	if !ok {
		t.Fatalf("expected agent state event, got %#v", events[0])
	}
	if agent.RuntimeMeta.ClaudeLifecycle != "waiting_input" {
		t.Fatalf("expected waiting_input lifecycle, got %#v", agent.RuntimeMeta)
	}
}

func TestControllerPermissionPromptReplacesPreviousPermissionMeta(t *testing.T) {
	controller := NewController("s1")
	controller.OnInputSent(protocol.RuntimeMeta{
		Source:              "permission-decision",
		PermissionRequestID: "old-perm",
		TargetPath:          "/tmp/old.txt",
	})
	events := controller.OnRunnerEvent(protocol.ApplyRuntimeMeta(
		protocol.NewPromptRequestEvent("s1", "Claude requested permissions to use Edit on /tmp/new.txt", []string{"y", "n"}),
		protocol.RuntimeMeta{
			BlockingKind:        "permission",
			PermissionRequestID: "new-perm",
			TargetPath:          "/tmp/new.txt",
		},
	))
	if len(events) != 1 {
		t.Fatalf("expected one agent event, got %#v", events)
	}
	snapshot := controller.Snapshot()
	if snapshot.ActiveMeta.PermissionRequestID != "new-perm" {
		t.Fatalf("expected new permission id, got %#v", snapshot.ActiveMeta)
	}
	if snapshot.ActiveMeta.TargetPath != "/tmp/new.txt" {
		t.Fatalf("expected new target path, got %#v", snapshot.ActiveMeta)
	}
}

func TestControllerDoneStepDoesNotOverwriteRunningToolState(t *testing.T) {
	controller := NewController("s1")
	controller.OnExecStart("claude", protocol.RuntimeMeta{Command: "claude"})
	events := controller.OnRunnerEvent(protocol.ApplyRuntimeMeta(
		protocol.NewStepUpdateEvent("s1", "正在修改 main.dart", "running", "/repo/main.dart", "Edit", "claude"),
		protocol.RuntimeMeta{Command: "claude", Engine: "claude"},
	))
	if len(events) != 1 {
		t.Fatalf("expected running step agent event, got %#v", events)
	}
	snapshot := controller.Snapshot()
	if snapshot.State != ControllerStateRunningTool {
		t.Fatalf("expected running tool state, got %q", snapshot.State)
	}
	if snapshot.LastStep != "正在修改 main.dart" {
		t.Fatalf("expected running step to be kept, got %q", snapshot.LastStep)
	}

	events = controller.OnRunnerEvent(protocol.ApplyRuntimeMeta(
		protocol.NewStepUpdateEvent("s1", "tool completed", "done", "/repo/main.dart", "Edit", "claude"),
		protocol.RuntimeMeta{Command: "claude", Engine: "claude"},
	))
	if len(events) != 0 {
		t.Fatalf("expected done step not to emit agent event, got %#v", events)
	}
	snapshot = controller.Snapshot()
	if snapshot.State != ControllerStateRunningTool {
		t.Fatalf("expected running tool state to remain, got %q", snapshot.State)
	}
	if snapshot.LastStep != "正在修改 main.dart" {
		t.Fatalf("done step should not become status label, got %q", snapshot.LastStep)
	}
}

func TestControllerKeepsRecentDiffContext(t *testing.T) {
	controller := NewController("s1")
	controller.OnRunnerEvent(protocol.FileDiffEvent{
		Event: protocol.NewBaseEvent(protocol.EventTypeFileDiff, "s1"),
		Path:  "internal/ws/handler.go",
		Title: "Updating internal/ws/handler.go",
		Diff:  "diff --git a/internal/ws/handler.go b/internal/ws/handler.go",
		Lang:  "go",
	})
	diff := controller.RecentDiff()
	if diff.Path != "internal/ws/handler.go" {
		t.Fatalf("unexpected diff path: %q", diff.Path)
	}
	if diff.Title != "Updating internal/ws/handler.go" {
		t.Fatalf("unexpected diff title: %q", diff.Title)
	}
	if !diff.PendingReview {
		t.Fatal("expected pending review to be true")
	}
}

func TestControllerReviewDecisionClearsPendingReview(t *testing.T) {
	controller := NewController("s1")
	controller.OnRunnerEvent(protocol.FileDiffEvent{
		Event: protocol.NewBaseEvent(protocol.EventTypeFileDiff, "s1"),
		Path:  "internal/ws/handler.go",
		Title: "Updating internal/ws/handler.go",
		Diff:  "diff --git a/internal/ws/handler.go b/internal/ws/handler.go",
		Lang:  "go",
	})
	controller.OnInputSent(protocol.RuntimeMeta{Source: "review-decision", TargetText: "accept"})
	if controller.RecentDiff().PendingReview {
		t.Fatal("expected pending review to be false after accept")
	}
}

func TestControllerReviewDecisionReviseKeepsPendingReview(t *testing.T) {
	controller := NewController("s1")
	controller.OnRunnerEvent(protocol.FileDiffEvent{
		Event: protocol.NewBaseEvent(protocol.EventTypeFileDiff, "s1"),
		Path:  "internal/ws/handler.go",
		Title: "Updating internal/ws/handler.go",
		Diff:  "diff --git a/internal/ws/handler.go b/internal/ws/handler.go",
		Lang:  "go",
	})
	controller.OnInputSent(protocol.RuntimeMeta{Source: "review-decision", TargetText: "revise", PermissionMode: "default"})
	diff := controller.RecentDiff()
	if !diff.PendingReview {
		t.Fatal("expected pending review to remain true after revise")
	}
	snapshot := controller.Snapshot()
	if snapshot.ActiveMeta.PermissionMode != "default" {
		t.Fatalf("expected permission mode to update, got %q", snapshot.ActiveMeta.PermissionMode)
	}
}

func TestControllerPermissionDecisionDoesNotAffectPendingReview(t *testing.T) {
	controller := NewController("s1")
	controller.OnRunnerEvent(protocol.FileDiffEvent{
		Event: protocol.NewBaseEvent(protocol.EventTypeFileDiff, "s1"),
		Path:  "internal/ws/handler.go",
		Title: "Updating internal/ws/handler.go",
		Diff:  "diff --git a/internal/ws/handler.go b/internal/ws/handler.go",
		Lang:  "go",
	})
	controller.OnInputSent(protocol.RuntimeMeta{Source: "permission-decision", TargetText: "approve", PermissionMode: "default"})
	diff := controller.RecentDiff()
	if !diff.PendingReview {
		t.Fatal("expected pending review to remain true after permission decision")
	}
	snapshot := controller.Snapshot()
	if snapshot.ActiveMeta.PermissionMode != "default" {
		t.Fatalf("expected permission mode to update, got %q", snapshot.ActiveMeta.PermissionMode)
	}
}

func TestControllerUpdatePermissionModePersistsToSnapshot(t *testing.T) {
	controller := NewController("s1")
	controller.UpdatePermissionMode("default")

	snapshot := controller.Snapshot()
	if snapshot.ActiveMeta.PermissionMode != "default" {
		t.Fatalf("expected permission mode to persist in snapshot, got %q", snapshot.ActiveMeta.PermissionMode)
	}
}

func TestCommandDetectionSupportsCodex(t *testing.T) {
	if !isAICommand("codex") {
		t.Fatal("expected codex to be treated as AI command")
	}
	if !isClaudeCommand("codex") {
		t.Fatal("expected codex to be treated as interactive AI command for lifecycle tracking")
	}
}
