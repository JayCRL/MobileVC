package session

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"mobilevc/internal/engine"
	"mobilevc/internal/protocol"
)

// permissionStubRunner 同时实现 engine.Runner / PermissionResponseWriter / ClaudeSessionProvider
type permissionStubRunner struct {
	mu                sync.Mutex
	interactive       bool
	hasPending        bool
	currentRequestID  string
	writeErr          error
	writeDecisions    []string
	permissionMode    string
	permissionModeSet int

	runStarted chan struct{}
	runDone    chan struct{}
	runErr     error
}

func newPermissionStubRunner() *permissionStubRunner {
	return &permissionStubRunner{
		runStarted: make(chan struct{}, 1),
		runDone:    make(chan struct{}),
	}
}

func (r *permissionStubRunner) Run(ctx context.Context, req engine.ExecRequest, sink engine.EventSink) error {
	select {
	case r.runStarted <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
	case <-r.runDone:
	}
	return r.runErr
}

func (r *permissionStubRunner) Write(ctx context.Context, data []byte) error { return nil }

func (r *permissionStubRunner) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case <-r.runDone:
	default:
		close(r.runDone)
	}
	return nil
}

func (r *permissionStubRunner) CanAcceptInteractiveInput() bool { return r.interactive }
func (r *permissionStubRunner) ClaudeSessionID() string         { return "" }

func (r *permissionStubRunner) WritePermissionResponse(ctx context.Context, decision string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeErr != nil {
		return r.writeErr
	}
	r.writeDecisions = append(r.writeDecisions, decision)
	r.hasPending = false
	return nil
}

func (r *permissionStubRunner) HasPendingPermissionRequest() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hasPending
}

func (r *permissionStubRunner) CurrentPermissionRequestID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.currentRequestID
}

func (r *permissionStubRunner) SetPermissionMode(mode string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.permissionMode = mode
	r.permissionModeSet++
}

func (r *permissionStubRunner) decisions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.writeDecisions))
	copy(out, r.writeDecisions)
	return out
}

// startPermissionRunner 启动 service 的 PTY runner（用 stub），等到 runner 真的进入 Run。
func startPermissionRunner(t *testing.T, svc *Service, sessionID string, runner *permissionStubRunner) []any {
	t.Helper()
	emit := []any{}
	mu := sync.Mutex{}
	if err := svc.Execute(context.Background(), sessionID, ExecuteRequest{
		Command:     "claude",
		Mode:        engine.ModePTY,
		RuntimeMeta: protocol.RuntimeMeta{Command: "claude"},
	}, func(event any) {
		mu.Lock()
		emit = append(emit, event)
		mu.Unlock()
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	select {
	case <-runner.runStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not start")
	}
	mu.Lock()
	defer mu.Unlock()
	out := make([]any, len(emit))
	copy(out, emit)
	return out
}

// makeServiceWithPermissionRunner 构造一个带 permissionStubRunner 的 Service。
func makeServiceWithPermissionRunner(t *testing.T, runner *permissionStubRunner) *Service {
	t.Helper()
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() engine.Runner { return newPermissionStubRunner() },
		NewPtyRunner:  func() engine.Runner { return runner },
	})
	t.Cleanup(svc.Cleanup)
	return svc
}

// ---- 不需要 active runner 的方法 ----

func TestService_InitialEvent(t *testing.T) {
	svc := NewService("s1", Dependencies{})
	t.Cleanup(svc.Cleanup)
	ev := svc.InitialEvent()
	if ev.SessionID != "s1" {
		t.Errorf("session id: %q", ev.SessionID)
	}
}

func TestService_SetSinkAndClearSink(t *testing.T) {
	svc := NewService("s1", Dependencies{})
	t.Cleanup(svc.Cleanup)
	if got := svc.getSink(); got != nil {
		t.Fatal("expected nil sink initially")
	}
	called := 0
	svc.SetSink(func(any) { called++ })
	if got := svc.getSink(); got == nil {
		t.Fatal("expected sink set")
	}
	svc.ClearSink()
	if got := svc.getSink(); got != nil {
		t.Fatal("expected sink cleared")
	}
}

func TestService_RecordUserInputAndPermissionMode(t *testing.T) {
	svc := NewService("s1", Dependencies{})
	t.Cleanup(svc.Cleanup)
	svc.RecordUserInput("hello")
	snap := svc.ControllerSnapshot()
	if snap.LastUserInput != "hello" {
		t.Errorf("LastUserInput: %q", snap.LastUserInput)
	}

	// 空白会被 normalize 成 auto
	svc.UpdatePermissionMode("")
	if got := svc.ControllerSnapshot().ActiveMeta.PermissionMode; got != "auto" {
		t.Errorf("empty -> %q, want auto", got)
	}
	svc.UpdatePermissionMode("bypassPermissions")
	if got := svc.ControllerSnapshot().ActiveMeta.PermissionMode; got != "bypassPermissions" {
		t.Errorf("bypass -> %q", got)
	}
	// 非法值会被映射成 auto
	svc.UpdatePermissionMode("acceptEdits")
	if got := svc.ControllerSnapshot().ActiveMeta.PermissionMode; got != "auto" {
		t.Errorf("invalid -> %q, want auto", got)
	}
}

func TestService_CleanupIdempotent(t *testing.T) {
	svc := NewService("s1", Dependencies{})
	svc.Cleanup()
	svc.Cleanup() // double cleanup must not panic
}

func TestService_CurrentPermissionRequestID_NoActive(t *testing.T) {
	svc := NewService("s1", Dependencies{})
	t.Cleanup(svc.Cleanup)
	if got := svc.CurrentPermissionRequestID(""); got != "" {
		t.Errorf("expected empty when no runner, got %q", got)
	}
}

func TestService_CurrentRunner_NoActive(t *testing.T) {
	svc := NewService("s1", Dependencies{})
	t.Cleanup(svc.Cleanup)
	if got := svc.CurrentRunner(); got != nil {
		t.Errorf("expected nil runner, got %T", got)
	}
}

func TestService_CanAcceptInteractiveInput_NoActive(t *testing.T) {
	svc := NewService("s1", Dependencies{})
	t.Cleanup(svc.Cleanup)
	if got := svc.CanAcceptInteractiveInput(); got {
		t.Errorf("expected false when no runner")
	}
}

// ---- StopActive ----

func TestService_StopActive_NoRunner(t *testing.T) {
	svc := NewService("s1", Dependencies{})
	t.Cleanup(svc.Cleanup)
	err := svc.StopActive("s1", func(any) {})
	if !errors.Is(err, ErrNoActiveRunner) {
		t.Errorf("expected ErrNoActiveRunner, got %v", err)
	}
}

func TestService_StopActive_WithRunner(t *testing.T) {
	runner := newPermissionStubRunner()
	runner.interactive = true
	svc := makeServiceWithPermissionRunner(t, runner)
	startPermissionRunner(t, svc, "s1", runner)

	emitted := []any{}
	if err := svc.StopActive("s1", func(e any) { emitted = append(emitted, e) }); err != nil {
		t.Fatalf("StopActive: %v", err)
	}
	if !runner.HasPendingPermissionRequest() {
		// runner.Close() 已被调用
	}
	// 应该至少 emit 一个 stopped session_state
	foundStopped := false
	for _, e := range emitted {
		if state, ok := e.(protocol.SessionStateEvent); ok && state.State == "stopped" {
			foundStopped = true
		}
	}
	if !foundStopped {
		t.Errorf("expected session_state stopped, got %d events", len(emitted))
	}
	// 等 service 完全清理（execWG）
	doneWait := make(chan struct{})
	go func() {
		svc.Cleanup()
		close(doneWait)
	}()
	select {
	case <-doneWait:
	case <-time.After(2 * time.Second):
		t.Fatal("Cleanup hung")
	}
}

// ---- SendPermissionDecision ----

func TestService_SendPermissionDecision_NoRunner(t *testing.T) {
	svc := NewService("s1", Dependencies{})
	t.Cleanup(svc.Cleanup)
	err := svc.SendPermissionDecision(context.Background(), "s1", "approve", protocol.RuntimeMeta{}, func(any) {})
	if !errors.Is(err, ErrNoActiveRunner) {
		t.Errorf("expected ErrNoActiveRunner, got %v", err)
	}
}

func TestService_SendPermissionDecision_NoPending(t *testing.T) {
	runner := newPermissionStubRunner()
	runner.hasPending = false
	svc := makeServiceWithPermissionRunner(t, runner)
	startPermissionRunner(t, svc, "s1", runner)

	err := svc.SendPermissionDecision(context.Background(), "s1", "approve", protocol.RuntimeMeta{}, func(any) {})
	if !errors.Is(err, engine.ErrNoPendingControlRequest) {
		t.Errorf("expected ErrNoPendingControlRequest, got %v", err)
	}
}

func TestService_SendPermissionDecision_RequestIDMismatch(t *testing.T) {
	runner := newPermissionStubRunner()
	runner.hasPending = true
	runner.currentRequestID = "perm-1"
	svc := makeServiceWithPermissionRunner(t, runner)
	startPermissionRunner(t, svc, "s1", runner)

	err := svc.SendPermissionDecision(context.Background(), "s1", "approve",
		protocol.RuntimeMeta{PermissionRequestID: "perm-9"}, func(any) {})
	if !errors.Is(err, engine.ErrNoPendingControlRequest) {
		t.Errorf("expected ErrNoPendingControlRequest on id mismatch, got %v", err)
	}
}

func TestService_SendPermissionDecision_HappyPathWritesDecision(t *testing.T) {
	runner := newPermissionStubRunner()
	runner.hasPending = true
	runner.currentRequestID = "perm-1"
	svc := makeServiceWithPermissionRunner(t, runner)
	startPermissionRunner(t, svc, "s1", runner)

	emitted := []any{}
	err := svc.SendPermissionDecision(context.Background(), "s1", "approve",
		protocol.RuntimeMeta{
			PermissionRequestID: "perm-1",
			PermissionMode:      "bypassPermissions",
			Source:              "permission-decision",
		},
		func(e any) { emitted = append(emitted, e) },
	)
	if err != nil {
		t.Fatalf("SendPermissionDecision: %v", err)
	}
	got := runner.decisions()
	if len(got) != 1 || got[0] != "approve" {
		t.Errorf("decision writes: %v", got)
	}
	if runner.permissionModeSet == 0 {
		t.Errorf("expected SetPermissionMode called when meta.PermissionMode set")
	}
	// runner 注入了 permission mode 设置
	if runner.permissionMode != "bypassPermissions" {
		t.Errorf("runner permission mode: %q", runner.permissionMode)
	}
	// activeMeta 已合入：permission mode 应被记录
	snap := svc.RuntimeSnapshot()
	if snap.ActiveMeta.PermissionMode != "bypassPermissions" {
		t.Errorf("active meta permissionMode: %q", snap.ActiveMeta.PermissionMode)
	}
	if len(emitted) == 0 {
		t.Errorf("expected at least one input-sent event")
	}
}

func TestService_SendPermissionDecision_WriteErrPropagates(t *testing.T) {
	runner := newPermissionStubRunner()
	runner.hasPending = true
	runner.writeErr = engine.ErrNoPendingControlRequest
	svc := makeServiceWithPermissionRunner(t, runner)
	startPermissionRunner(t, svc, "s1", runner)

	err := svc.SendPermissionDecision(context.Background(), "s1", "approve",
		protocol.RuntimeMeta{}, func(any) {})
	if !errors.Is(err, engine.ErrNoPendingControlRequest) {
		t.Errorf("expected ErrNoPendingControlRequest mapped, got %v", err)
	}
}

func TestService_SendPermissionDecision_ResumeNotFoundErr(t *testing.T) {
	runner := newPermissionStubRunner()
	runner.hasPending = true
	runner.writeErr = errors.New("no conversation found with session id xx")
	svc := makeServiceWithPermissionRunner(t, runner)
	startPermissionRunner(t, svc, "s1", runner)

	err := svc.SendPermissionDecision(context.Background(), "s1", "approve",
		protocol.RuntimeMeta{}, func(any) {})
	if !errors.Is(err, ErrResumeConversationNotFound) {
		t.Errorf("expected ErrResumeConversationNotFound, got %v", err)
	}
}

// ---- CurrentPermissionRequestID with active runner ----

func TestService_CurrentPermissionRequestID_WithActive(t *testing.T) {
	runner := newPermissionStubRunner()
	runner.hasPending = true
	runner.currentRequestID = "perm-77"
	svc := makeServiceWithPermissionRunner(t, runner)
	startPermissionRunner(t, svc, "active-session", runner)

	if got := svc.CurrentPermissionRequestID(""); got != "perm-77" {
		t.Errorf("expected perm-77, got %q", got)
	}
	// 错误 sessionID -> 空
	if got := svc.CurrentPermissionRequestID("other-session"); got != "" {
		t.Errorf("expected empty for mismatched sessionID, got %q", got)
	}
	// 正确 sessionID
	if got := svc.CurrentPermissionRequestID("active-session"); got != "perm-77" {
		t.Errorf("expected perm-77 for active session, got %q", got)
	}
}

// ---- ReviewDecision ----

func TestService_ReviewDecision_EmptyRejected(t *testing.T) {
	svc := NewService("s1", Dependencies{})
	t.Cleanup(svc.Cleanup)
	err := svc.ReviewDecision(context.Background(), "s1", ReviewDecisionRequest{}, func(any) {})
	if err == nil || !strings.Contains(err.Error(), "review decision is required") {
		t.Errorf("expected required error, got %v", err)
	}
}

func TestService_ReviewDecision_UnknownRejectedAfterRunner(t *testing.T) {
	// reviewDecisionPayload 不识别时返回 "review decision must be one of"
	runner := newPermissionStubRunner()
	runner.interactive = true
	svc := makeServiceWithPermissionRunner(t, runner)
	startPermissionRunner(t, svc, "s1", runner)

	err := svc.ReviewDecision(context.Background(), "s1", ReviewDecisionRequest{Decision: "yolo"}, func(any) {})
	if err == nil || !strings.Contains(err.Error(), "review decision must be one of") {
		t.Errorf("expected unknown decision error, got %v", err)
	}
}

func TestService_ReviewDecision_ReviewOnlyAcceptShortCircuits(t *testing.T) {
	// IsReviewOnly + decision != revert 时只 emit OnInputSent，不写 runner
	svc := NewService("s1", Dependencies{})
	t.Cleanup(svc.Cleanup)
	emitted := []any{}
	err := svc.ReviewDecision(context.Background(), "s1", ReviewDecisionRequest{
		Decision:     "accept",
		IsReviewOnly: true,
	}, func(e any) { emitted = append(emitted, e) })
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	// 没 runner 也能成功 — 说明是短路路径
	if len(emitted) == 0 {
		t.Errorf("expected emit OnInputSent events")
	}
}

func TestService_ReviewDecision_AcceptRoutesThroughSendInput(t *testing.T) {
	runner := newPermissionStubRunner()
	runner.interactive = true
	svc := makeServiceWithPermissionRunner(t, runner)
	startPermissionRunner(t, svc, "s1", runner)
	err := svc.ReviewDecision(context.Background(), "s1", ReviewDecisionRequest{Decision: "accept"}, func(any) {})
	if err != nil {
		t.Fatalf("ReviewDecision accept: %v", err)
	}
}

// ---- PlanDecision ----

func TestService_PlanDecision_EmptyRejected(t *testing.T) {
	svc := NewService("s1", Dependencies{})
	t.Cleanup(svc.Cleanup)
	err := svc.PlanDecision(context.Background(), "s1", PlanDecisionRequest{}, func(any) {})
	if err == nil || !strings.Contains(err.Error(), "plan decision is required") {
		t.Errorf("expected required error, got %v", err)
	}
}

func TestService_PlanDecision_NoRunner(t *testing.T) {
	svc := NewService("s1", Dependencies{})
	t.Cleanup(svc.Cleanup)
	err := svc.PlanDecision(context.Background(), "s1", PlanDecisionRequest{Decision: "approve"}, func(any) {})
	if !errors.Is(err, ErrNoActiveRunner) {
		t.Errorf("expected ErrNoActiveRunner, got %v", err)
	}
}

// ---- reviewDecisionPayload (unexported helper) ----

func TestReviewDecisionPayload(t *testing.T) {
	cases := []struct {
		decision string
		meta     protocol.RuntimeMeta
		want     string // 关键词
	}{
		{"accept", protocol.RuntimeMeta{TargetPath: "/p"}, "ACCEPT"},
		{"revert", protocol.RuntimeMeta{}, "REVERT"},
		{"revise", protocol.RuntimeMeta{ContextTitle: "title"}, "REVISE"},
		{"yolo", protocol.RuntimeMeta{}, ""},
		{"", protocol.RuntimeMeta{}, ""},
		{"  Accept  ", protocol.RuntimeMeta{}, "ACCEPT"}, // case insensitive + trim
	}
	for _, tc := range cases {
		got := reviewDecisionPayload(tc.decision, tc.meta)
		if tc.want == "" {
			if got != "" {
				t.Errorf("decision=%q: expected empty, got %q", tc.decision, got)
			}
			continue
		}
		if !strings.Contains(got, tc.want) {
			t.Errorf("decision=%q: expected %q in %q", tc.decision, tc.want, got)
		}
	}
}

func TestReviewDecisionPayload_TargetSubjectLine(t *testing.T) {
	got := reviewDecisionPayload("accept", protocol.RuntimeMeta{TargetPath: "/p", ContextTitle: "ignored", Target: "ignored"})
	if !strings.Contains(got, "Target: /p") {
		t.Errorf("expected Target line with TargetPath, got %q", got)
	}
}

// ---- defaultAICommandFromCommandOrEngine ----

func TestDefaultAICommandFromCommandOrEngine(t *testing.T) {
	cases := []struct {
		command, engine, want string
	}{
		{"codex", "", "codex"},
		{"/usr/bin/codex --resume x", "", "codex"},
		{"codex.exe", "", "codex"},
		{"gemini -m flash", "", "gemini"},
		{"", "codex", "codex"},
		{"", "Gemini", "gemini"},
		{"", "", "claude"},
		{"unknown-bin", "", "claude"},
	}
	for _, tc := range cases {
		if got := defaultAICommandFromCommandOrEngine(tc.command, tc.engine); got != tc.want {
			t.Errorf("(%q, %q) -> %q, want %q", tc.command, tc.engine, got, tc.want)
		}
	}
}

// ---- stripResumeArg ----

func TestStripResumeArg(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"   ", ""},
		{"claude --resume abc", "claude"},
		{"claude --resume abc -m sonnet", "claude -m sonnet"},
		{"codex resume sess-1", "codex"},
		{"codex resume sess-1 --foo", "codex --foo"},
		{"codex resume --foo", "codex --foo"}, // 第二个不是 sess id, 不消费
		{"claude", "claude"},
		{"claude -m sonnet", "claude -m sonnet"}, // no --resume to strip
	}
	for _, tc := range cases {
		if got := stripResumeArg(tc.in); got != tc.want {
			t.Errorf("stripResumeArg(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
