package unit

import (
	"context"
	"testing"

	"mobilevc/internal/data"
	"mobilevc/internal/engine"
	"mobilevc/internal/protocol"
	"mobilevc/internal/session"
)

func TestController_InitialEvent(t *testing.T) {
	ctrl := session.NewController("s1")
	ev := ctrl.InitialEvent()
	if ev.SessionID != "s1" {
		t.Errorf("SessionID: got %q", ev.SessionID)
	}
	if ev.State != "IDLE" {
		t.Errorf("State: got %q, want IDLE", ev.State)
	}
}

func TestController_OnExecStart(t *testing.T) {
	ctrl := session.NewController("s1")
	events := ctrl.OnExecStart("claude --print", protocol.RuntimeMeta{SkillName: "test-skill"})
	if len(events) == 0 {
		t.Fatal("expected at least 1 event")
	}
	agentEv, ok := events[0].(protocol.AgentStateEvent)
	if !ok {
		t.Fatalf("expected AgentStateEvent, got %T", events[0])
	}
	if agentEv.State != "THINKING" {
		t.Errorf("State: got %q, want THINKING", agentEv.State)
	}
}

func TestController_OnExecStart_Transition(t *testing.T) {
	ctrl := session.NewController("s1")
	ev := ctrl.InitialEvent()
	if ev.State != "IDLE" {
		t.Fatalf("expected IDLE, got %q", ev.State)
	}

	events := ctrl.OnExecStart("claude", protocol.RuntimeMeta{})
	if len(events) == 0 {
		t.Fatal("expected events")
	}
	agentEv := events[0].(protocol.AgentStateEvent)
	if agentEv.State != "THINKING" {
		t.Errorf("expected THINKING, got %q", agentEv.State)
	}
}

func TestController_OnInputSent(t *testing.T) {
	ctrl := session.NewController("s1")
	ctrl.OnExecStart("claude", protocol.RuntimeMeta{})

	events := ctrl.OnInputSent(protocol.RuntimeMeta{Source: "user-input"})
	if len(events) == 0 {
		t.Fatal("expected events")
	}
	agentEv := events[0].(protocol.AgentStateEvent)
	if agentEv.State != "THINKING" {
		t.Errorf("expected THINKING, got %q", agentEv.State)
	}
}

func TestController_OnCommandFinished_Idle(t *testing.T) {
	ctrl := session.NewController("s1")
	ctrl.OnExecStart("ls -la", protocol.RuntimeMeta{})

	events := ctrl.OnCommandFinished(protocol.RuntimeMeta{})
	if len(events) == 0 {
		t.Fatal("expected events")
	}
	agentEv := events[0].(protocol.AgentStateEvent)
	if agentEv.State != "IDLE" {
		t.Errorf("expected IDLE, got %q", agentEv.State)
	}
}

func TestController_OnRunnerEvent_PromptRequest(t *testing.T) {
	ctrl := session.NewController("s1")
	ctrl.OnExecStart("claude", protocol.RuntimeMeta{})

	events := ctrl.OnRunnerEvent(protocol.PromptRequestEvent{
		Message: "Allow tool?",
		Event: protocol.Event{
			RuntimeMeta: protocol.RuntimeMeta{
				BlockingKind:       "permission",
				PermissionRequestID: "pr-1",
			},
		},
	})
	if len(events) == 0 {
		t.Fatal("expected events")
	}
	agentEv := events[0].(protocol.AgentStateEvent)
	if agentEv.State != "WAIT_INPUT" {
		t.Errorf("expected WAIT_INPUT, got %q", agentEv.State)
	}
}

func TestController_OnRunnerEvent_StepUpdate(t *testing.T) {
	ctrl := session.NewController("s1")
	ctrl.OnExecStart("claude", protocol.RuntimeMeta{})

	events := ctrl.OnRunnerEvent(protocol.StepUpdateEvent{
		Message: "Editing file.go",
		Status:  "in_progress",
	})
	if len(events) == 0 {
		t.Fatal("expected events")
	}
	agentEv := events[0].(protocol.AgentStateEvent)
	if agentEv.State != "RUNNING_TOOL" {
		t.Errorf("expected RUNNING_TOOL, got %q", agentEv.State)
	}
}

func TestController_Snapshot_Restore(t *testing.T) {
	ctrl := session.NewController("s1")
	ctrl.OnExecStart("claude --print", protocol.RuntimeMeta{})

	snap := ctrl.Snapshot()
	if snap.State != "THINKING" {
		t.Errorf("snapshot state: got %q", snap.State)
	}
	if snap.CurrentCommand != "claude --print" {
		t.Errorf("snapshot command: got %q", snap.CurrentCommand)
	}

	ctrl2 := session.NewController("s2")
	ctrl2.Restore(snap)
	snap2 := ctrl2.Snapshot()
	if snap2.State != snap.State {
		t.Errorf("restored state: got %q, want %q", snap2.State, snap.State)
	}
}

func TestController_UpdatePermissionMode(t *testing.T) {
	ctrl := session.NewController("s1")
	ctrl.UpdatePermissionMode("auto")
	snap := ctrl.Snapshot()
	if snap.ActiveMeta.PermissionMode != "auto" {
		t.Errorf("PermissionMode: got %q, want auto", snap.ActiveMeta.PermissionMode)
	}
}

func TestController_RecordUserInput(t *testing.T) {
	ctrl := session.NewController("s1")
	ctrl.RecordUserInput("hello claude")
	snap := ctrl.Snapshot()
	if snap.LastUserInput != "hello claude" {
		t.Errorf("LastUserInput: got %q", snap.LastUserInput)
	}
}

func TestController_RecordUserInput_Empty(t *testing.T) {
	ctrl := session.NewController("s1")
	ctrl.RecordUserInput("  ")
	snap := ctrl.Snapshot()
	if snap.LastUserInput != "" {
		t.Errorf("empty input should be ignored: %q", snap.LastUserInput)
	}
}

func TestController_RecentDiff(t *testing.T) {
	ctrl := session.NewController("s1")
	ctrl.OnExecStart("claude", protocol.RuntimeMeta{})
	ctrl.OnRunnerEvent(protocol.FileDiffEvent{
		Path:  "/tmp/test.go",
		Title: "test diff",
		Diff:  "+func Test() {}",
		Lang:  "go",
	})

	diff := ctrl.RecentDiff()
	if diff.Path != "/tmp/test.go" {
		t.Errorf("RecentDiff path: got %q", diff.Path)
	}
	if !diff.PendingReview {
		t.Error("diff should be pending review")
	}

	diffs := ctrl.RecentDiffs()
	if len(diffs) == 0 {
		t.Fatal("RecentDiffs should not be empty")
	}
}

func TestController_Dedup_PromptRequest(t *testing.T) {
	ctrl := session.NewController("s1")
	ctrl.OnExecStart("claude", protocol.RuntimeMeta{})

	req := protocol.PromptRequestEvent{
		Message: "Allow?",
		Event: protocol.Event{
			RuntimeMeta: protocol.RuntimeMeta{
				PermissionRequestID: "pr-1",
				BlockingKind:        "permission",
			},
		},
	}
	events1 := ctrl.OnRunnerEvent(req)
	if len(events1) == 0 {
		t.Fatal("first event should not be deduped")
	}
	events2 := ctrl.OnRunnerEvent(req)
	if len(events2) != 0 {
		t.Fatal("duplicate prompt should be deduped")
	}
}

func TestController_Dedup_StepUpdate(t *testing.T) {
	ctrl := session.NewController("s1")
	ctrl.OnExecStart("claude", protocol.RuntimeMeta{})

	step := protocol.StepUpdateEvent{
		Message: "Running tool",
		Status:  "in_progress",
	}
	e1 := ctrl.OnRunnerEvent(step)
	if len(e1) == 0 {
		t.Fatal("first step should not be deduped")
	}
	e2 := ctrl.OnRunnerEvent(step)
	if len(e2) != 0 {
		t.Fatal("duplicate step should be deduped")
	}
}

func TestParseMode(t *testing.T) {
	tests := []struct {
		raw     string
		want    engine.Mode
		wantErr bool
	}{
		{"pty", engine.ModePTY, false},
		{"exec", engine.ModeExec, false},
		{"", engine.ModeExec, false},
		{"  pty  ", engine.ModePTY, false},
		{"unknown", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := session.ParseMode(tt.raw)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for %q", tt.raw)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnqueue(t *testing.T) {
	ch := make(chan any, 1)
	ctx := context.Background()
	session.Enqueue(ctx, ch, "hello")
	select {
	case v := <-ch:
		if v != "hello" {
			t.Errorf("got %q", v)
		}
	default:
		t.Fatal("expected value on channel")
	}
}

func TestEnqueue_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch := make(chan any, 1)
	session.Enqueue(ctx, ch, "hello")
	select {
	case <-ch:
		t.Fatal("should not receive on cancelled context")
	default:
		// expected
	}
}

func TestNewService(t *testing.T) {
	deps := session.Dependencies{}
	svc := session.NewService("s1", deps)
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
}

func TestService_IsRunning_Initially(t *testing.T) {
	svc := session.NewService("s1", session.Dependencies{})
	if svc.IsRunning() {
		t.Error("service should not be running initially")
	}
}

func TestService_CanAcceptInteractiveInput_Initially(t *testing.T) {
	svc := session.NewService("s1", session.Dependencies{})
	if svc.CanAcceptInteractiveInput() {
		t.Error("should not accept input initially")
	}
}

func TestService_ControllerSnapshot_Initial(t *testing.T) {
	svc := session.NewService("s1", session.Dependencies{})
	snap := svc.ControllerSnapshot()
	if snap.State != "IDLE" {
		t.Errorf("initial state: got %q, want IDLE", snap.State)
	}
}

func TestClassifyPermissionKind(t *testing.T) {
	// Test that the function returns a valid kind for common inputs
	// Actual classification logic depends on keyword matching in the implementation
	tests := []struct {
		msg, path, cmd string
	}{
		{"Write file.go", "/src/file.go", "claude"},
		{"Run shell command", "", "claude"},
		{"Read file", "", "claude"},
		{"", "", ""},
	}
	for _, tt := range tests {
		got := session.ClassifyPermissionKind(tt.msg, tt.path, tt.cmd)
		if got == "" {
			t.Errorf("ClassifyPermissionKind(%q,%q,%q) returned empty", tt.msg, tt.path, tt.cmd)
		}
	}
}

func TestPermissionCommandHead(t *testing.T) {
	if got := session.PermissionCommandHead("claude --print"); got != "claude" {
		t.Errorf("got %q, want claude", got)
	}
	if got := session.PermissionCommandHead(""); got != "" {
		t.Errorf("empty: got %q", got)
	}
	if got := session.PermissionCommandHead("codex app-server"); got != "codex" {
		t.Errorf("got %q, want codex", got)
	}
}

func TestPermissionRuleID(t *testing.T) {
	id := session.PermissionRuleID(data.PermissionRule{
		Kind: data.PermissionKindWrite, TargetPathPrefix: "/src", Engine: "claude",
	})
	if id == "" {
		t.Error("rule ID should not be empty")
	}
	// Same inputs = same ID
	id2 := session.PermissionRuleID(data.PermissionRule{
		Kind: data.PermissionKindWrite, TargetPathPrefix: "/src", Engine: "claude",
	})
	if id != id2 {
		t.Errorf("same inputs should produce same ID: %q vs %q", id, id2)
	}
}

func TestMatchPermissionRule(t *testing.T) {
	rule := data.PermissionRule{
		ID: "r1", Engine: "claude", Kind: data.PermissionKindWrite,
		TargetPathPrefix: "/src", Enabled: true,
	}
	ctx := session.PermissionMatchContext{
		Engine: "claude", Kind: data.PermissionKindWrite, TargetPath: "/src/main.go",
	}
	matched, ok := session.MatchPermissionRule([]data.PermissionRule{rule}, ctx)
	if !ok {
		t.Fatal("expected match")
	}
	if matched.ID != "r1" {
		t.Errorf("matched ID: got %q", matched.ID)
	}
}

func TestMatchPermissionRule_NoMatch(t *testing.T) {
	rule := data.PermissionRule{
		ID: "r1", Engine: "claude", Kind: data.PermissionKindWrite,
		Enabled: true,
	}
	ctx := session.PermissionMatchContext{
		Engine: "codex", Kind: data.PermissionKindWrite,
	}
	_, ok := session.MatchPermissionRule([]data.PermissionRule{rule}, ctx)
	if ok {
		t.Fatal("should not match different engine")
	}
}

func TestMatchPermissionRule_Disabled(t *testing.T) {
	rule := data.PermissionRule{
		ID: "r1", Engine: "claude", Kind: data.PermissionKindWrite, Enabled: false,
	}
	ctx := session.PermissionMatchContext{
		Engine: "claude", Kind: data.PermissionKindWrite,
	}
	_, ok := session.MatchPermissionRule([]data.PermissionRule{rule}, ctx)
	if ok {
		t.Fatal("should not match disabled rule")
	}
}

func TestMarkPermissionRuleMatched(t *testing.T) {
	rules := []data.PermissionRule{
		{ID: "r1", Enabled: true},
		{ID: "r2", Enabled: true},
	}
	updated := session.MarkPermissionRuleMatched(rules, "r1")
	if updated[0].MatchCount != 1 {
		t.Errorf("MatchCount: got %d, want 1", updated[0].MatchCount)
	}
	if updated[0].LastMatchedAt.IsZero() {
		t.Error("LastMatchedAt should not be zero")
	}
	if updated[1].MatchCount != 0 {
		t.Error("r2 should not be marked")
	}
}

func TestBuildPermissionDecisionPrompt(t *testing.T) {
	prompt, err := session.BuildPermissionDecisionPrompt("approve", protocol.PermissionDecisionRequestEvent{
		PermissionRequestID: "pr-1",
		ContextID:           "ctx-1",
	})
	if err != nil {
		t.Fatalf("BuildPermissionDecisionPrompt: %v", err)
	}
	if prompt == "" {
		t.Error("prompt should not be empty")
	}
}

func TestPermissionDecisionIntent(t *testing.T) {
	kind := session.PermissionDecisionIntent(protocol.PermissionDecisionRequestEvent{
		TargetPath: "/src/file.go",
	})
	if kind == "" {
		t.Error("decision intent should not be empty")
	}
}

func TestIsClaudeCommandLike(t *testing.T) {
	if !session.IsClaudeCommandLike("claude") {
		t.Error("'claude' should be claude-like")
	}
	if !session.IsClaudeCommandLike("claude --print") {
		t.Error("'claude --print' should be claude-like")
	}
	if session.IsClaudeCommandLike("codex") {
		t.Error("'codex' should not be claude-like")
	}
	if session.IsClaudeCommandLike("ls") {
		t.Error("'ls' should not be claude-like")
	}
	if session.IsClaudeCommandLike("") {
		t.Error("empty should not be claude-like")
	}
}

func TestNormalizeProjectionSnapshot(t *testing.T) {
	snap := session.NormalizeProjectionSnapshot(data.ProjectionSnapshot{})
	if snap.RawTerminalByStream == nil {
		t.Error("RawTerminalByStream should be initialized")
	}
	if snap.LogEntries == nil {
		t.Error("LogEntries should be initialized")
	}
	if snap.TerminalExecutions == nil {
		t.Error("TerminalExecutions should be initialized")
	}
}

func TestNormalizeProjectionLifecycle(t *testing.T) {
	// Pass-through when lifecycle is already set
	lc := session.NormalizeProjectionLifecycle("active", "resume-123")
	if lc != "active" {
		t.Errorf("got %q, want active", lc)
	}
	// "starting" + resume → "resumable"
	lc = session.NormalizeProjectionLifecycle("starting", "resume-456")
	if lc != "resumable" {
		t.Errorf("got %q, want resumable", lc)
	}
	// "starting" without resume → stays "starting"
	lc = session.NormalizeProjectionLifecycle("starting", "")
	if lc != "starting" {
		t.Errorf("got %q, want starting", lc)
	}
	// empty → empty
	lc = session.NormalizeProjectionLifecycle("", "")
	if lc != "" {
		t.Errorf("got %q, want empty", lc)
	}
}

func TestIsBusyRuntimeState(t *testing.T) {
	if !session.IsBusyRuntimeState("THINKING") {
		t.Error("THINKING should be busy")
	}
	if !session.IsBusyRuntimeState("RUNNING_TOOL") {
		t.Error("RUNNING_TOOL should be busy")
	}
	if !session.IsBusyRuntimeState("RUNNING") {
		t.Error("RUNNING should be busy")
	}
	if session.IsBusyRuntimeState("IDLE") {
		t.Error("IDLE should not be busy")
	}
	if session.IsBusyRuntimeState("WAIT_INPUT") {
		t.Error("WAIT_INPUT should not be busy")
	}
	if session.IsBusyRuntimeState("") {
		t.Error("empty should not be busy")
	}
}

func TestBuildPermissionDecisionPlan(t *testing.T) {
	plan, err := session.BuildPermissionDecisionPlan(
		protocol.PermissionDecisionRequestEvent{
			Decision:            "allow",
			PermissionRequestID: "pr-1",
			TargetPath:          "/src/test.go",
		},
		data.ProjectionSnapshot{},
		session.ControllerSnapshot{State: "WAIT_INPUT"},
	)
	if err != nil {
		t.Fatalf("BuildPermissionDecisionPlan: %v", err)
	}
	if plan.Action == "" {
		t.Error("plan action should not be empty")
	}
}

func TestPermissionContextFromDecision(t *testing.T) {
	ctx := session.PermissionContextFromDecision(
		protocol.PermissionDecisionRequestEvent{
			TargetPath:     "/src/file.go",
			FallbackEngine: "claude",
		},
		data.ProjectionSnapshot{},
		session.ControllerSnapshot{CurrentCommand: "claude"},
	)
	if ctx.TargetPath != "/src/file.go" {
		t.Errorf("TargetPath: got %q", ctx.TargetPath)
	}
	if ctx.Engine != "claude" {
		t.Errorf("Engine: got %q, want claude", ctx.Engine)
	}
}

func TestBuildPermissionRule(t *testing.T) {
	rule := session.BuildPermissionRule(
		protocol.PermissionDecisionRequestEvent{
			TargetPath: "/src/file.go",
		},
		"session",
		data.ProjectionSnapshot{},
		session.ControllerSnapshot{CurrentCommand: "claude"},
	)
	if rule.Scope != "session" {
		t.Errorf("Scope: got %q", rule.Scope)
	}
	if rule.ID == "" {
		t.Error("rule ID should not be empty")
	}
}
