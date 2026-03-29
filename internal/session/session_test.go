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
