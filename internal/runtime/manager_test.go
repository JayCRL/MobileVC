package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"mobilevc/internal/protocol"
	"mobilevc/internal/runner"
)

type hotSwapStubRunner struct {
	interactive   bool
	claudeSession string
	writeErr      error
	started       chan struct{}
	closed        chan struct{}
	lastReq       runner.ExecRequest
	writes        [][]byte
	runFn         func(ctx context.Context, req runner.ExecRequest, sink runner.EventSink) error
}

func newHotSwapStubRunner(sessionID string, interactive bool) *hotSwapStubRunner {
	return &hotSwapStubRunner{
		interactive:   interactive,
		claudeSession: sessionID,
		started:       make(chan struct{}, 1),
		closed:        make(chan struct{}, 1),
	}
}

func (s *hotSwapStubRunner) Run(ctx context.Context, req runner.ExecRequest, sink runner.EventSink) error {
	s.lastReq = req
	select {
	case s.started <- struct{}{}:
	default:
	}
	if s.runFn != nil {
		return s.runFn(ctx, req, sink)
	}
	<-ctx.Done()
	return nil
}

func (s *hotSwapStubRunner) Write(ctx context.Context, data []byte) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	s.writes = append(s.writes, append([]byte(nil), data...))
	return nil
}

func (s *hotSwapStubRunner) Close() error {
	select {
	case s.closed <- struct{}{}:
	default:
	}
	return nil
}

func (s *hotSwapStubRunner) CanAcceptInteractiveInput() bool {
	return s.interactive
}

func (s *hotSwapStubRunner) ClaudeSessionID() string {
	return s.claudeSession
}

func waitSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func TestManagerFinishIfCurrentIgnoresSupersededRunner(t *testing.T) {
	first := newHotSwapStubRunner("resume-old", true)
	second := newHotSwapStubRunner("resume-new", true)
	mgr := newManager()
	if err := mgr.start("s1", first, nil, protocol.RuntimeMeta{Command: "claude", PermissionMode: "default"}); err != nil {
		t.Fatalf("start first runner: %v", err)
	}
	mgr.closeActive()
	if err := mgr.start("s1", second, nil, protocol.RuntimeMeta{Command: "claude --resume resume-new", PermissionMode: "auto", ResumeSessionID: "resume-new"}); err != nil {
		t.Fatalf("start second runner: %v", err)
	}

	if mgr.finishIfCurrent(first) {
		t.Fatal("expected superseded runner finish to be ignored")
	}
	if !mgr.isRunning() {
		t.Fatal("expected current runner to remain active after superseded finish")
	}
	currentRunner, meta, sessionID := mgr.current()
	if currentRunner != second {
		t.Fatal("expected second runner to remain active")
	}
	if sessionID != "s1" || meta.ResumeSessionID != "resume-new" {
		t.Fatalf("unexpected active meta after ignored finish: session=%q meta=%#v", sessionID, meta)
	}

	if !mgr.finishIfCurrent(second) {
		t.Fatal("expected active runner finish to succeed")
	}
	if mgr.isRunning() {
		t.Fatal("expected manager to be idle after finishing current runner")
	}
}

func TestExecuteSupersededRunnerDoesNotEmitFinishedState(t *testing.T) {
	first := newHotSwapStubRunner("resume-old", true)
	second := newHotSwapStubRunner("resume-new", true)
	firstDone := make(chan struct{})
	secondDone := make(chan struct{})
	firstRunEntered := make(chan struct{}, 1)
	secondRunEntered := make(chan struct{}, 1)

	first.runFn = func(ctx context.Context, req runner.ExecRequest, sink runner.EventSink) error {
		select {
		case firstRunEntered <- struct{}{}:
		default:
		}
		<-firstDone
		return nil
	}
	second.runFn = func(ctx context.Context, req runner.ExecRequest, sink runner.EventSink) error {
		select {
		case secondRunEntered <- struct{}{}:
		default:
		}
		<-secondDone
		return nil
	}

	call := 0
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() runner.Runner { return newHotSwapStubRunner("", true) },
		NewPtyRunner: func() runner.Runner {
			call++
			if call == 1 {
				return first
			}
			return second
		},
	})

	var mu sync.Mutex
	var events []any
	emit := func(event any) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	}

	if err := svc.Execute(context.Background(), "s1", ExecuteRequest{
		Command:        "claude",
		CWD:            "/tmp",
		Mode:           runner.ModePTY,
		PermissionMode: "default",
		RuntimeMeta:    protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default"},
	}, emit); err != nil {
		t.Fatalf("execute first: %v", err)
	}
	waitSignal(t, firstRunEntered, "first run start")

	svc.manager.closeActive()
	if err := svc.Execute(context.Background(), "s1", ExecuteRequest{
		Command:        "claude --resume resume-new --print",
		CWD:            "/tmp",
		Mode:           runner.ModePTY,
		PermissionMode: "auto",
		RuntimeMeta:    protocol.RuntimeMeta{Command: "claude --resume resume-new --print", CWD: "/tmp", PermissionMode: "auto", ResumeSessionID: "resume-new"},
	}, emit); err != nil {
		t.Fatalf("execute second: %v", err)
	}
	waitSignal(t, secondRunEntered, "second run start")

	close(firstDone)
	time.Sleep(50 * time.Millisecond)
	if !svc.IsRunning() {
		t.Fatal("expected second runner to remain active after first runner exits")
	}

	close(secondDone)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	idleCount := 0
	for _, event := range events {
		agent, ok := event.(protocol.AgentStateEvent)
		if !ok {
			continue
		}
		if agent.State == "IDLE" {
			idleCount++
		}
	}
	if idleCount != 1 {
		t.Fatalf("expected exactly one idle transition from current runner, got %d events=%#v", idleCount, events)
	}
}

type overridableHotSwapStubRunner struct {
	hotSwapStubRunner
	runFn func(ctx context.Context, req runner.ExecRequest, sink runner.EventSink) error
}

func (s *overridableHotSwapStubRunner) Run(ctx context.Context, req runner.ExecRequest, sink runner.EventSink) error {
	if s.runFn != nil {
		return s.runFn(ctx, req, sink)
	}
	return s.hotSwapStubRunner.Run(ctx, req, sink)
}

func TestExecuteInjectsManagedSessionIDForFreshClaudeExec(t *testing.T) {
	pty := newHotSwapStubRunner("", true)
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() runner.Runner { return newHotSwapStubRunner("", true) },
		NewPtyRunner:  func() runner.Runner { return pty },
	})
	if err := svc.Execute(context.Background(), "s1", ExecuteRequest{
		Command:        "claude",
		CWD:            "/tmp",
		Mode:           runner.ModePTY,
		PermissionMode: "default",
		RuntimeMeta:    protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default"},
	}, func(any) {}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	waitSignal(t, pty.started, "fresh runner start")
	if !strings.Contains(pty.lastReq.Command, "--session-id ") {
		t.Fatalf("expected managed session id on fresh command, got %q", pty.lastReq.Command)
	}
	if strings.Contains(pty.lastReq.Command, "--resume") {
		t.Fatalf("did not expect resume flag on fresh command, got %q", pty.lastReq.Command)
	}
	resumeID := svc.RuntimeSnapshot().ResumeSessionID
	if resumeID == "" {
		t.Fatal("expected runtime snapshot to persist managed session id")
	}
	if !strings.Contains(pty.lastReq.Command, resumeID) {
		t.Fatalf("expected command to contain managed session id %q, got %q", resumeID, pty.lastReq.Command)
	}
}

func TestExecuteClaudeLifecycleTransitionsFromStarting(t *testing.T) {
	pty := newHotSwapStubRunner("resume-xyz", false)
	pty.runFn = func(ctx context.Context, req runner.ExecRequest, sink runner.EventSink) error {
		sink(protocol.NewSessionStateEvent("s1", "active", "command started"))
		sink(protocol.NewStepUpdateEvent("s1", "Running TodoWrite", "running", "TodoWrite", "TodoWrite", "Running TodoWrite"))
		pty.interactive = true
		sink(protocol.NewPromptRequestEvent("s1", "继续输入", nil))
		return nil
	}
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() runner.Runner { return newHotSwapStubRunner("", true) },
		NewPtyRunner:  func() runner.Runner { return pty },
	})
	var events []any
	if err := svc.Execute(context.Background(), "s1", ExecuteRequest{
		Command:        "claude",
		CWD:            "/tmp",
		Mode:           runner.ModePTY,
		PermissionMode: "default",
		RuntimeMeta:    protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default"},
	}, func(event any) {
		events = append(events, event)
	}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	waitSignal(t, pty.started, "runner start")
	time.Sleep(50 * time.Millisecond)

	seenStarting := false
	seenActive := false
	seenWaiting := false
	for _, event := range events {
		switch e := event.(type) {
		case protocol.AgentStateEvent:
			switch e.State {
			case "THINKING":
				if e.RuntimeMeta.ClaudeLifecycle == "starting" {
					seenStarting = true
				}
			case "RUNNING_TOOL":
				if e.RuntimeMeta.ClaudeLifecycle == "active" {
					seenActive = true
				}
			case "WAIT_INPUT":
				if e.RuntimeMeta.ClaudeLifecycle == "waiting_input" {
					seenWaiting = true
				}
			}
		case protocol.StepUpdateEvent:
			if e.RuntimeMeta.ClaudeLifecycle != "active" {
				t.Fatalf("expected step update lifecycle active, got %#v", e.RuntimeMeta)
			}
		case protocol.PromptRequestEvent:
			if e.RuntimeMeta.ClaudeLifecycle != "waiting_input" {
				t.Fatalf("expected prompt lifecycle waiting_input, got %#v", e.RuntimeMeta)
			}
		}
	}
	if !seenStarting {
		t.Fatal("expected initial thinking state to remain starting")
	}
	if !seenActive {
		t.Fatal("expected running tool state to become active")
	}
	if !seenWaiting {
		t.Fatal("expected wait input state to become waiting_input")
	}
}

func TestHotSwapApproveWithTemporaryElevationRestartsWithResumeAndContinuation(t *testing.T) {
	first := newHotSwapStubRunner("resume-123", true)
	second := newHotSwapStubRunner("resume-123", true)
	call := 0
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() runner.Runner { return newHotSwapStubRunner("", true) },
		NewPtyRunner: func() runner.Runner {
			call++
			if call == 1 {
				return second
			}
			return newHotSwapStubRunner("resume-123", true)
		},
	})
	if err := svc.manager.start("s1", first, nil, protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default"}); err != nil {
		t.Fatalf("start manager: %v", err)
	}

	continuation := hotSwapContinuationInput("README.md", "写 README 需要你的授权")
	err := svc.HotSwapApproveWithTemporaryElevation(context.Background(), "s1", ExecuteRequest{
		Command:        "claude",
		CWD:            "/tmp",
		Mode:           runner.ModePTY,
		PermissionMode: "default",
		RuntimeMeta: protocol.RuntimeMeta{
			Source:         "permission-decision",
			Command:        "claude",
			CWD:            "/tmp",
			PermissionMode: "default",
		},
	}, continuation, func(any) {})
	if err != nil {
		t.Fatalf("hot swap approve: %v", err)
	}
	waitSignal(t, first.closed, "old runner close")
	waitSignal(t, second.started, "new runner start")
	if second.lastReq.PermissionMode != "auto" {
		t.Fatalf("expected auto, got %#v", second.lastReq)
	}
	if !strings.Contains(second.lastReq.Command, "--resume resume-123") {
		t.Fatalf("expected resume command, got %q", second.lastReq.Command)
	}
	if !strings.Contains(second.lastReq.Command, "--print") {
		t.Fatalf("expected print mode on hot swap command, got %q", second.lastReq.Command)
	}
	if strings.Contains(second.lastReq.Command, "--session-id") {
		t.Fatalf("did not expect managed session id on hot swap command, got %q", second.lastReq.Command)
	}
	if len(second.writes) != 1 || string(second.writes[0]) != continuation {
		t.Fatalf("unexpected continuation writes: %#v", second.writes)
	}
	if !strings.Contains(continuation, "先使用 Read 读取目标文件的当前内容") {
		t.Fatalf("expected continuation to require Read before Edit, got %q", continuation)
	}
	snapshot := svc.RuntimeSnapshot()
	if !snapshot.TemporaryElevated {
		t.Fatal("expected temporary elevated snapshot")
	}
	if snapshot.SafePermissionMode != "default" {
		t.Fatalf("expected safe permission mode default, got %#v", snapshot)
	}
	if snapshot.ActiveMeta.PermissionMode != "default" {
		t.Fatalf("expected exposed runtime permission mode to stay default during temporary elevation, got %#v", snapshot.ActiveMeta)
	}
}

func TestRestoreSafePermissionModeBeforeInputRestartsAndSendsUserInput(t *testing.T) {
	first := newHotSwapStubRunner("resume-234", true)
	second := newHotSwapStubRunner("resume-234", true)
	call := 0
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() runner.Runner { return newHotSwapStubRunner("", true) },
		NewPtyRunner: func() runner.Runner {
			call++
			if call == 1 {
				return second
			}
			return newHotSwapStubRunner("resume-234", true)
		},
	})
	if err := svc.manager.start("s1", first, nil, protocol.RuntimeMeta{Command: "claude --resume resume-234", CWD: "/tmp", PermissionMode: "auto", ResumeSessionID: "resume-234"}); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	svc.manager.updateResumeSessionID("resume-234")
	svc.manager.setTemporaryElevation(true, "default")

	err := svc.RestoreSafePermissionModeBeforeInput(context.Background(), "s1", ExecuteRequest{
		Command:        "claude --resume resume-234",
		CWD:            "/tmp",
		Mode:           runner.ModePTY,
		PermissionMode: "default",
		RuntimeMeta: protocol.RuntimeMeta{
			Command:         "claude --resume resume-234",
			CWD:             "/tmp",
			ResumeSessionID: "resume-234",
			PermissionMode:  "default",
		},
	}, "hello\n", func(any) {})
	if err != nil {
		t.Fatalf("restore safe mode: %v", err)
	}
	waitSignal(t, first.closed, "elevated runner close")
	waitSignal(t, second.started, "safe runner start")
	if second.lastReq.PermissionMode != "default" {
		t.Fatalf("expected default mode, got %#v", second.lastReq)
	}
	if !strings.Contains(second.lastReq.Command, "--resume resume-234") {
		t.Fatalf("expected resume command during restore, got %q", second.lastReq.Command)
	}
	if !strings.Contains(second.lastReq.Command, "--print") {
		t.Fatalf("expected print mode during restore, got %q", second.lastReq.Command)
	}
	if len(second.writes) != 1 || string(second.writes[0]) != "hello\n" {
		t.Fatalf("unexpected user input writes: %#v", second.writes)
	}
	if svc.RuntimeSnapshot().TemporaryElevated {
		t.Fatal("expected temporary elevation to be cleared")
	}
}

func TestRestoreSafePermissionModeBeforeInputCanRestartFromDetachedResumeSession(t *testing.T) {
	second := newHotSwapStubRunner("resume-detached", true)
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() runner.Runner { return newHotSwapStubRunner("", true) },
		NewPtyRunner:  func() runner.Runner { return second },
	})
	svc.manager.updateResumeSessionID("resume-detached")
	svc.manager.setTemporaryElevation(true, "default")
	if snapshot := svc.RuntimeSnapshot(); snapshot.ActiveSession != "" || snapshot.Running {
		t.Fatalf("expected detached snapshot before restore, got %#v", snapshot)
	}

	err := svc.RestoreSafePermissionModeBeforeInput(context.Background(), "s1", ExecuteRequest{
		Command:        "claude",
		CWD:            "/tmp",
		Mode:           runner.ModePTY,
		PermissionMode: "default",
		RuntimeMeta: protocol.RuntimeMeta{
			Command:         "claude",
			CWD:             "/tmp",
			ResumeSessionID: "resume-detached",
			PermissionMode:  "default",
		},
	}, "hello\n", func(any) {})
	if err != nil {
		t.Fatalf("restore safe mode from detached resume: %v", err)
	}
	waitSignal(t, second.started, "safe runner start")
	if second.lastReq.PermissionMode != "default" {
		t.Fatalf("expected default mode, got %#v", second.lastReq)
	}
	if !strings.Contains(second.lastReq.Command, "--resume resume-detached") {
		t.Fatalf("expected detached resume command, got %q", second.lastReq.Command)
	}
	if len(second.writes) != 1 || string(second.writes[0]) != "hello\n" {
		t.Fatalf("unexpected user input writes: %#v", second.writes)
	}
	if svc.RuntimeSnapshot().TemporaryElevated {
		t.Fatal("expected temporary elevation to be cleared")
	}
}

func TestSendInputOrResumeWritesActiveRunnerWithoutRestart(t *testing.T) {
	active := newHotSwapStubRunner("resume-active", true)
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() runner.Runner { return newHotSwapStubRunner("", true) },
		NewPtyRunner:  func() runner.Runner { t.Fatal("did not expect resume runner to start"); return nil },
	})
	if err := svc.manager.start("s1", active, nil, protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default", ResumeSessionID: "resume-active"}); err != nil {
		t.Fatalf("start manager: %v", err)
	}

	if err := svc.SendInputOrResume(context.Background(), "s1", ExecuteRequest{
		Command:        "claude",
		CWD:            "/tmp",
		Mode:           runner.ModePTY,
		PermissionMode: "default",
		RuntimeMeta:    protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default", ResumeSessionID: "resume-active"},
	}, InputRequest{Data: "hello\n"}, func(any) {}); err != nil {
		t.Fatalf("send input or resume: %v", err)
	}

	if len(active.writes) != 1 || string(active.writes[0]) != "hello\n" {
		t.Fatalf("unexpected active runner writes: %#v", active.writes)
	}
}

func TestSendInputOrResumeRestartsDetachedResumeSession(t *testing.T) {
	resumed := newHotSwapStubRunner("resume-detached", true)
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() runner.Runner { return newHotSwapStubRunner("", true) },
		NewPtyRunner:  func() runner.Runner { return resumed },
	})
	svc.manager.updateResumeSessionID("resume-detached")

	if err := svc.SendInputOrResume(context.Background(), "s1", ExecuteRequest{
		Command:        "claude",
		CWD:            "/tmp",
		Mode:           runner.ModePTY,
		PermissionMode: "default",
		RuntimeMeta:    protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default", ResumeSessionID: "resume-detached"},
	}, InputRequest{Data: "hello again\n", RuntimeMeta: protocol.RuntimeMeta{Source: "input"}}, func(any) {}); err != nil {
		t.Fatalf("send input or resume: %v", err)
	}

	waitSignal(t, resumed.started, "detached resume runner start")
	if !strings.Contains(resumed.lastReq.Command, "--resume resume-detached") {
		t.Fatalf("expected detached resume command, got %q", resumed.lastReq.Command)
	}
	if len(resumed.writes) != 1 || string(resumed.writes[0]) != "hello again\n" {
		t.Fatalf("unexpected resumed runner writes: %#v", resumed.writes)
	}
}

func TestSendInputOrResumeReturnsNoActiveRunnerWithoutResumeSession(t *testing.T) {
	svc := NewService("s1", Dependencies{})
	err := svc.SendInputOrResume(context.Background(), "s1", ExecuteRequest{
		Command:        "claude",
		CWD:            "/tmp",
		Mode:           runner.ModePTY,
		PermissionMode: "default",
		RuntimeMeta:    protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default"},
	}, InputRequest{Data: "hello\n"}, func(any) {})
	if !errors.Is(err, ErrNoActiveRunner) {
		t.Fatalf("expected ErrNoActiveRunner, got %v", err)
	}
}

func TestHotSwapApproveWithTemporaryElevationRequiresResumeSession(t *testing.T) {
	first := newHotSwapStubRunner("", true)
	svc := NewService("s1", Dependencies{})
	if err := svc.manager.start("s1", first, nil, protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default"}); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	continuation := hotSwapContinuationInput("README.md", "写 README 需要你的授权")
	err := svc.HotSwapApproveWithTemporaryElevation(context.Background(), "s1", ExecuteRequest{
		Command:        "claude",
		CWD:            "/tmp",
		Mode:           runner.ModePTY,
		PermissionMode: "default",
		RuntimeMeta:    protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default"},
	}, continuation, func(any) {})
	if !errors.Is(err, ErrResumeSessionUnavailable) {
		t.Fatalf("expected ErrResumeSessionUnavailable, got %v", err)
	}
}

func TestHotSwapApproveWithTemporaryElevationDoesNotRequireInteractiveRunner(t *testing.T) {
	first := newHotSwapStubRunner("resume-345", true)
	second := newHotSwapStubRunner("resume-345", false)
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() runner.Runner { return newHotSwapStubRunner("", true) },
		NewPtyRunner:  func() runner.Runner { return second },
	})
	if err := svc.manager.start("s1", first, nil, protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default", ResumeSessionID: "resume-345"}); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	continuation := hotSwapContinuationInput("README.md", "写 README 需要你的授权")
	err := svc.HotSwapApproveWithTemporaryElevation(context.Background(), "s1", ExecuteRequest{
		Command:        "claude",
		CWD:            "/tmp",
		Mode:           runner.ModePTY,
		PermissionMode: "default",
		RuntimeMeta:    protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default", ResumeSessionID: "resume-345"},
	}, continuation, func(any) {})
	if err != nil {
		t.Fatalf("expected non-interactive hot swap to succeed, got %v", err)
	}
	waitSignal(t, first.closed, "old runner close")
	waitSignal(t, second.started, "new runner start")
	if len(second.writes) != 1 || string(second.writes[0]) != continuation {
		t.Fatalf("unexpected continuation writes: %#v", second.writes)
	}
}

func TestExecuteDefaultsToCodexWhenEngineIsCodex(t *testing.T) {
	pty := newHotSwapStubRunner("", true)
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() runner.Runner { return newHotSwapStubRunner("", true) },
		NewPtyRunner:  func() runner.Runner { return pty },
	})
	if err := svc.Execute(context.Background(), "s1", ExecuteRequest{
		Command:        "",
		CWD:            "/tmp",
		Mode:           runner.ModePTY,
		PermissionMode: "default",
		RuntimeMeta: protocol.RuntimeMeta{
			Engine:         "codex",
			CWD:            "/tmp",
			PermissionMode: "default",
		},
	}, func(any) {}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	waitSignal(t, pty.started, "codex runner start")
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(pty.lastReq.Command)), "codex") {
		t.Fatalf("expected codex command, got %q", pty.lastReq.Command)
	}
	if strings.Contains(pty.lastReq.Command, "--session-id") {
		t.Fatalf("did not expect claude managed session id on codex command, got %q", pty.lastReq.Command)
	}
}

func TestRunnerIsClaudeSessionSupportsCodexCommand(t *testing.T) {
	if !runnerIsClaudeSession(nil, "codex --help") {
		t.Fatal("expected codex command to be treated as AI session command")
	}
}

func TestEnsureResumeCommandUsesCodexResumeSubcommand(t *testing.T) {
	got := ensureResumeCommand("codex -m gpt-5", "session-xyz")
	if got != "codex resume session-xyz -m gpt-5" {
		t.Fatalf("unexpected codex resume command: %q", got)
	}
	if strings.Contains(strings.ToLower(got), "--resume") {
		t.Fatalf("did not expect claude-style --resume flag in codex command: %q", got)
	}
}

func TestBuildDetachedHotSwapStreamRequestForCodexDoesNotAppendClaudeFlags(t *testing.T) {
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() runner.Runner { return newHotSwapStubRunner("", true) },
		NewPtyRunner:  func() runner.Runner { return newHotSwapStubRunner("", true) },
	})
	svc.manager.updateResumeSessionID("resume-codex-123")
	req, safeMode, err := svc.buildDetachedHotSwapStreamRequest(ExecuteRequest{
		Command:        "codex -m gpt-5",
		CWD:            "/tmp",
		Mode:           runner.ModePTY,
		PermissionMode: "default",
		RuntimeMeta: protocol.RuntimeMeta{
			Command:         "codex -m gpt-5",
			Engine:          "codex",
			CWD:             "/tmp",
			ResumeSessionID: "resume-codex-123",
			PermissionMode:  "default",
		},
	}, "auto")
	if err != nil {
		t.Fatalf("buildDetachedHotSwapStreamRequest: %v", err)
	}
	if safeMode != "default" {
		t.Fatalf("expected default safe permission mode, got %q", safeMode)
	}
	lower := strings.ToLower(req.Command)
	if !strings.HasPrefix(lower, "codex resume resume-codex-123") {
		t.Fatalf("expected codex resume command, got %q", req.Command)
	}
	if strings.Contains(lower, "--print") || strings.Contains(lower, "--input-format") || strings.Contains(lower, "--output-format") || strings.Contains(lower, "--permission-prompt-tool") {
		t.Fatalf("did not expect claude stream flags on codex command, got %q", req.Command)
	}
}

func TestRestoreSafePermissionModeBeforeInputPropagatesWriteFailure(t *testing.T) {
	first := newHotSwapStubRunner("resume-456", true)
	second := newHotSwapStubRunner("resume-456", true)
	second.writeErr = errors.New("write failed")
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() runner.Runner { return newHotSwapStubRunner("", true) },
		NewPtyRunner:  func() runner.Runner { return second },
	})
	if err := svc.manager.start("s1", first, nil, protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "auto", ResumeSessionID: "resume-456"}); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	svc.manager.updateResumeSessionID("resume-456")
	svc.manager.setTemporaryElevation(true, "default")
	err := svc.RestoreSafePermissionModeBeforeInput(context.Background(), "s1", ExecuteRequest{
		Command:        "claude",
		CWD:            "/tmp",
		Mode:           runner.ModePTY,
		PermissionMode: "default",
		RuntimeMeta:    protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default", ResumeSessionID: "resume-456"},
	}, "hello\n", func(any) {})
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected write failure, got %v", err)
	}
}
