package session

import (
	"context"
	"strings"
	"testing"

	"mobilevc/internal/data"
	"mobilevc/internal/protocol"
)

// stubResponder 实现 engine.PermissionResponseWriter
type stubResponder struct {
	pending   bool
	requestID string
	writeErr  error
}

func (s *stubResponder) WritePermissionResponse(ctx context.Context, decision string) error {
	return s.writeErr
}
func (s *stubResponder) HasPendingPermissionRequest() bool { return s.pending }
func (s *stubResponder) CurrentPermissionRequestID() string {
	return s.requestID
}

// stubPendingLookup 实现 PendingPermissionPromptLookup
type stubPendingLookup struct {
	byID    map[string]*protocol.PromptRequestEvent
	latest  *protocol.PromptRequestEvent
	calls   int
	lastReq string
}

func (s *stubPendingLookup) LatestPendingPermissionPrompt(requestID string) *protocol.PromptRequestEvent {
	s.calls++
	s.lastReq = requestID
	if s.byID == nil {
		return nil
	}
	return s.byID[requestID]
}

func (s *stubPendingLookup) LatestPendingPrompt() *protocol.PromptRequestEvent {
	return s.latest
}

func TestNormalizeClaudePermissionMode(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"bypassPermissions", "bypassPermissions"},
		{"  bypassPermissions  ", "bypassPermissions"},
		{"auto", "auto"},
		{"", "auto"},
		{"default", "auto"},
		{"acceptEdits", "auto"},
		{"random", "auto"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := NormalizeClaudePermissionMode(tc.in); got != tc.want {
				t.Errorf("NormalizeClaudePermissionMode(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestPromptHasExplicitPermissionIntent(t *testing.T) {
	yes := []string{
		"Permission required",
		"Allow once?",
		"Allow this time?",
		"Always allow",
		"Always deny",
		"Authorize this action",
		"Authorization needed",
		"Approval required",
		"需要授权吗？",
		"请确认权限",
		"是否允许该操作",
	}
	no := []string{
		"",
		"Continue?",
		"Pick a model",
		"已完成",
	}
	for _, m := range yes {
		t.Run("yes/"+m, func(t *testing.T) {
			p := protocol.PromptRequestEvent{Message: m}
			if !PromptHasExplicitPermissionIntent(p) {
				t.Errorf("expected true for %q", m)
			}
		})
	}
	for _, m := range no {
		t.Run("no/"+m, func(t *testing.T) {
			p := protocol.PromptRequestEvent{Message: m}
			if PromptHasExplicitPermissionIntent(p) {
				t.Errorf("expected false for %q", m)
			}
		})
	}
}

func TestBuildReviewDecisionPrompt(t *testing.T) {
	t.Run("empty rejected", func(t *testing.T) {
		_, err := BuildReviewDecisionPrompt("", protocol.ReviewDecisionRequestEvent{})
		if err == nil {
			t.Fatal("expected error for empty decision")
		}
	})
	t.Run("unknown rejected", func(t *testing.T) {
		_, err := BuildReviewDecisionPrompt("yolo", protocol.ReviewDecisionRequestEvent{})
		if err == nil {
			t.Fatal("expected error for unknown decision")
		}
	})
	t.Run("accept with target", func(t *testing.T) {
		got, err := BuildReviewDecisionPrompt("accept", protocol.ReviewDecisionRequestEvent{TargetPath: "main.go"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "main.go") || !strings.Contains(got, "接受") {
			t.Errorf("expected accept copy with target, got %q", got)
		}
	})
	t.Run("revert", func(t *testing.T) {
		got, err := BuildReviewDecisionPrompt("revert", protocol.ReviewDecisionRequestEvent{TargetPath: "x"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "撤回") {
			t.Errorf("expected revert copy: %q", got)
		}
	})
	t.Run("revise", func(t *testing.T) {
		got, err := BuildReviewDecisionPrompt("revise", protocol.ReviewDecisionRequestEvent{TargetPath: "x"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "继续调整") {
			t.Errorf("expected revise copy: %q", got)
		}
	})
	t.Run("subject fallback to context title", func(t *testing.T) {
		got, err := BuildReviewDecisionPrompt("accept", protocol.ReviewDecisionRequestEvent{ContextTitle: "feature-x"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "feature-x") {
			t.Errorf("expected ContextTitle in subject: %q", got)
		}
	})
	t.Run("subject empty falls back to placeholder", func(t *testing.T) {
		got, err := BuildReviewDecisionPrompt("accept", protocol.ReviewDecisionRequestEvent{})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "当前 diff") {
			t.Errorf("expected placeholder subject: %q", got)
		}
	})
}

func TestRefreshedPermissionPromptEvent_NilService(t *testing.T) {
	p := RefreshedPermissionPromptEvent("sess-1", protocol.PermissionDecisionRequestEvent{}, nil)
	if p != nil {
		t.Errorf("expected nil for nil service, got %+v", p)
	}
}

func TestRefreshedPermissionPromptEventWithID_EmptyRequestID(t *testing.T) {
	svc := NewService("sess-1", Dependencies{})
	p := RefreshedPermissionPromptEventWithID("sess-1", protocol.PermissionDecisionRequestEvent{}, svc, "  ")
	if p != nil {
		t.Errorf("expected nil for blank requestID, got %+v", p)
	}
}

func TestRefreshedPermissionPromptEventWithID_BuildsPrompt(t *testing.T) {
	svc := NewService("sess-1", Dependencies{})
	req := protocol.PermissionDecisionRequestEvent{
		PromptMessage:   "Allow write?",
		ResumeSessionID: "resume-x",
		FallbackCommand: "claude",
		FallbackEngine:  "claude",
		FallbackCWD:     "/tmp",
		PermissionMode:  "bypassPermissions",
		ContextID:       "ctx-1",
		ContextTitle:    "feat",
		TargetPath:      "/tmp/x.go",
	}
	p := RefreshedPermissionPromptEventWithID("sess-1", req, svc, "perm-id-7")
	if p == nil {
		t.Fatal("expected prompt event")
	}
	if p.SessionID != "sess-1" {
		t.Errorf("session id: %q", p.SessionID)
	}
	if p.Message != "Allow write?" {
		t.Errorf("message: %q", p.Message)
	}
	if len(p.Options) != 2 || p.Options[0] != "y" || p.Options[1] != "n" {
		t.Errorf("options: %v", p.Options)
	}
	meta := p.RuntimeMeta
	if meta.Source != "permission-refresh" {
		t.Errorf("source: %q", meta.Source)
	}
	if meta.PermissionRequestID != "perm-id-7" {
		t.Errorf("permission request id: %q", meta.PermissionRequestID)
	}
	if meta.BlockingKind != "permission" {
		t.Errorf("blocking kind: %q", meta.BlockingKind)
	}
	if meta.PermissionMode != "bypassPermissions" {
		t.Errorf("permission mode: %q", meta.PermissionMode)
	}
	if meta.TargetPath != "/tmp/x.go" {
		t.Errorf("target path: %q", meta.TargetPath)
	}
	if meta.ResumeSessionID != "resume-x" {
		t.Errorf("resume session: %q", meta.ResumeSessionID)
	}
}

func TestRefreshedPermissionPromptEventWithID_DefaultMessage(t *testing.T) {
	svc := NewService("sess-1", Dependencies{})
	p := RefreshedPermissionPromptEventWithID("sess-1", protocol.PermissionDecisionRequestEvent{}, svc, "id")
	if p == nil {
		t.Fatal("expected prompt event")
	}
	if !strings.Contains(p.Message, "需要你的授权") {
		t.Errorf("expected default message, got %q", p.Message)
	}
}

func TestShouldBlockInputForPendingPermission_NilResponder(t *testing.T) {
	if got := ShouldBlockInputForPendingPermission(nil, nil, data.ProjectionSnapshot{}, nil); got {
		t.Fatal("expected false for nil responder")
	}
}

func TestShouldBlockInputForPendingPermission_NoPending(t *testing.T) {
	r := &stubResponder{pending: false}
	if got := ShouldBlockInputForPendingPermission(r, nil, data.ProjectionSnapshot{}, nil); got {
		t.Fatal("expected false when responder has no pending")
	}
}

func TestShouldBlockInputForPendingPermission_BlockingMetaInActiveController(t *testing.T) {
	r := &stubResponder{pending: true, requestID: ""}
	svc := NewService("s", Dependencies{})
	// 通过 controller 注入 BlockingKind=permission
	svc.controller.activeMeta.BlockingKind = "permission"
	got := ShouldBlockInputForPendingPermission(r, svc, data.ProjectionSnapshot{}, nil)
	if !got {
		t.Fatal("expected block due to controller BlockingKind=permission")
	}
}

func TestShouldBlockInputForPendingPermission_BlockingKindFromProjection(t *testing.T) {
	r := &stubResponder{pending: true, requestID: ""}
	projection := data.ProjectionSnapshot{
		Controller: data.ControllerSnapshot{
			ActiveMeta: protocol.RuntimeMeta{BlockingKind: "permission"},
		},
	}
	if got := ShouldBlockInputForPendingPermission(r, nil, projection, nil); !got {
		t.Fatal("expected block from projection.Controller meta")
	}
}

func TestShouldBlockInputForPendingPermission_RequestIDPresentBlocks(t *testing.T) {
	// 即便没有 BlockingKind, 只要有非空、非 sentinel 的 requestID 也阻塞
	r := &stubResponder{pending: true, requestID: "perm-9"}
	if got := ShouldBlockInputForPendingPermission(r, nil, data.ProjectionSnapshot{}, nil); !got {
		t.Fatal("expected block when responder reports a real requestID")
	}
}

func TestShouldBlockInputForPendingPermission_LookupByIDHits(t *testing.T) {
	// requestID 是 sentinel 时不会因为 requestID 直接阻塞, 但 lookup 命中时阻塞
	r := &stubResponder{pending: true, requestID: "__text_permission_prompt__"}
	prompt := &protocol.PromptRequestEvent{Message: "permission?"}
	pending := &stubPendingLookup{
		byID: map[string]*protocol.PromptRequestEvent{
			"__text_permission_prompt__": prompt,
		},
	}
	if got := ShouldBlockInputForPendingPermission(r, nil, data.ProjectionSnapshot{}, pending); !got {
		t.Fatal("expected block when pending lookup hits sentinel id")
	}
	if pending.calls == 0 {
		t.Errorf("lookup not called")
	}
}

func TestShouldBlockInputForPendingPermission_LookupMissReturnsFalse(t *testing.T) {
	r := &stubResponder{pending: true, requestID: "__text_permission_prompt__"}
	pending := &stubPendingLookup{} // 全部未命中
	got := ShouldBlockInputForPendingPermission(r, nil, data.ProjectionSnapshot{}, pending)
	if got {
		t.Fatal("expected false when no permission signal anywhere")
	}
}

func TestShouldBlockInputForPendingPermission_LatestPromptMetaBlocks(t *testing.T) {
	r := &stubResponder{pending: true, requestID: "__text_permission_prompt__"}
	pending := &stubPendingLookup{
		latest: &protocol.PromptRequestEvent{
			Message: "anything",
			Event: protocol.Event{
				RuntimeMeta: protocol.RuntimeMeta{BlockingKind: "permission"},
			},
		},
	}
	if got := ShouldBlockInputForPendingPermission(r, nil, data.ProjectionSnapshot{}, pending); !got {
		t.Fatal("expected block when latest prompt meta is permission")
	}
}
