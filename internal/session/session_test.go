package session

import (
	"testing"

	"mobilevc/internal/protocol"
)

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

func TestControllerUpdatePermissionModePersistsToSnapshot(t *testing.T) {
	controller := NewController("s1")
	controller.UpdatePermissionMode("default")

	snapshot := controller.Snapshot()
	if snapshot.ActiveMeta.PermissionMode != "default" {
		t.Fatalf("expected permission mode to persist in snapshot, got %q", snapshot.ActiveMeta.PermissionMode)
	}
}
