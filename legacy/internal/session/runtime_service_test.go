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

type resumeStubRunner struct {
	mu            sync.Mutex
	interactive   bool
	claudeSession string
	writeErr      error
	writeDelay    time.Duration
	started       chan struct{}
	closed        chan struct{}
	lastReq       engine.ExecRequest
	writes        [][]byte
	runFn         func(ctx context.Context, req engine.ExecRequest, sink engine.EventSink) error
}

func newResumeStubRunner(sessionID string, interactive bool) *resumeStubRunner {
	return &resumeStubRunner{
		interactive:   interactive,
		claudeSession: sessionID,
		started:       make(chan struct{}, 1),
		closed:        make(chan struct{}, 1),
	}
}

func (s *resumeStubRunner) Run(ctx context.Context, req engine.ExecRequest, sink engine.EventSink) error {
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

func (s *resumeStubRunner) Write(ctx context.Context, data []byte) error {
	s.mu.Lock()
	interactive := s.interactive
	s.mu.Unlock()
	if !interactive {
		return ErrRunnerNotInteractive
	}
	if s.writeDelay > 0 {
		timer := time.NewTimer(s.writeDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	if s.writeErr != nil {
		return s.writeErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes = append(s.writes, append([]byte(nil), data...))
	return nil
}

func (s *resumeStubRunner) Close() error {
	select {
	case s.closed <- struct{}{}:
	default:
	}
	return nil
}

func (s *resumeStubRunner) CanAcceptInteractiveInput() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.interactive
}

func (s *resumeStubRunner) setInteractive(value bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interactive = value
}

func (s *resumeStubRunner) ClaudeSessionID() string {
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
	first := newResumeStubRunner("resume-old", true)
	second := newResumeStubRunner("resume-new", true)
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
	first := newResumeStubRunner("resume-old", true)
	second := newResumeStubRunner("resume-new", true)
	firstDone := make(chan struct{})
	secondDone := make(chan struct{})
	firstRunEntered := make(chan struct{}, 1)
	secondRunEntered := make(chan struct{}, 1)

	first.runFn = func(ctx context.Context, req engine.ExecRequest, sink engine.EventSink) error {
		select {
		case firstRunEntered <- struct{}{}:
		default:
		}
		<-firstDone
		return nil
	}
	second.runFn = func(ctx context.Context, req engine.ExecRequest, sink engine.EventSink) error {
		select {
		case secondRunEntered <- struct{}{}:
		default:
		}
		<-secondDone
		return nil
	}

	call := 0
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() engine.Runner { return newResumeStubRunner("", true) },
		NewPtyRunner: func() engine.Runner {
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
		Mode:           engine.ModePTY,
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
		Mode:           engine.ModePTY,
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

type overridableResumeStubRunner struct {
	resumeStubRunner
	runFn func(ctx context.Context, req engine.ExecRequest, sink engine.EventSink) error
}

func (s *overridableResumeStubRunner) Run(ctx context.Context, req engine.ExecRequest, sink engine.EventSink) error {
	if s.runFn != nil {
		return s.runFn(ctx, req, sink)
	}
	return s.resumeStubRunner.Run(ctx, req, sink)
}

func TestExecuteInjectsManagedSessionIDForFreshClaudeExec(t *testing.T) {
	pty := newResumeStubRunner("", true)
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() engine.Runner { return newResumeStubRunner("", true) },
		NewPtyRunner:  func() engine.Runner { return pty },
	})
	if err := svc.Execute(context.Background(), "s1", ExecuteRequest{
		Command:        "claude",
		CWD:            "/tmp",
		Mode:           engine.ModePTY,
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
	pty := newResumeStubRunner("resume-xyz", false)
	pty.runFn = func(ctx context.Context, req engine.ExecRequest, sink engine.EventSink) error {
		sink(protocol.NewSessionStateEvent("s1", "active", "command started"))
		sink(protocol.NewStepUpdateEvent("s1", "Running TodoWrite", "running", "TodoWrite", "TodoWrite", "Running TodoWrite"))
		pty.setInteractive(true)
		sink(protocol.NewPromptRequestEvent("s1", "继续输入", nil))
		return nil
	}
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() engine.Runner { return newResumeStubRunner("", true) },
		NewPtyRunner:  func() engine.Runner { return pty },
	})
	var events []any
	if err := svc.Execute(context.Background(), "s1", ExecuteRequest{
		Command:        "claude",
		CWD:            "/tmp",
		Mode:           engine.ModePTY,
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

func TestSendInputOrResumeWritesActiveRunnerWithoutRestart(t *testing.T) {
	active := newResumeStubRunner("resume-active", true)
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() engine.Runner { return newResumeStubRunner("", true) },
		NewPtyRunner:  func() engine.Runner { t.Fatal("did not expect resume runner to start"); return nil },
	})
	if err := svc.manager.start("s1", active, nil, protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default", ResumeSessionID: "resume-active"}); err != nil {
		t.Fatalf("start manager: %v", err)
	}

	if err := svc.SendInputOrResume(context.Background(), "s1", ExecuteRequest{
		Command:        "claude",
		CWD:            "/tmp",
		Mode:           engine.ModePTY,
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
	resumed := newResumeStubRunner("resume-detached", true)
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() engine.Runner { return newResumeStubRunner("", true) },
		NewPtyRunner:  func() engine.Runner { return resumed },
	})
	svc.manager.updateResumeSessionID("resume-detached")

	if err := svc.SendInputOrResume(context.Background(), "s1", ExecuteRequest{
		Command:        "claude",
		CWD:            "/tmp",
		Mode:           engine.ModePTY,
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

func TestSendInputOrResumePreservesCodexYoloRuntimeMeta(t *testing.T) {
	resumed := newResumeStubRunner("thread-yolo", true)
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() engine.Runner { return newResumeStubRunner("", true) },
		NewPtyRunner:  func() engine.Runner { return resumed },
	})
	svc.manager.updateResumeSessionID("thread-yolo")

	err := svc.SendInputOrResume(context.Background(), "s1", ExecuteRequest{
		Command:        "codex",
		CWD:            "/tmp/project",
		Mode:           engine.ModePTY,
		PermissionMode: "bypassPermissions",
		RuntimeMeta: protocol.RuntimeMeta{
			Command:          "codex",
			Engine:           "codex",
			CWD:              "/tmp/project",
			CodexSandboxMode: "danger-full-access",
			PermissionMode:   "bypassPermissions",
			ResumeSessionID:  "thread-yolo",
		},
	}, InputRequest{
		Data: "check github\n",
		RuntimeMeta: protocol.RuntimeMeta{
			Source:           "input",
			CodexSandboxMode: "danger-full-access",
			PermissionMode:   "bypassPermissions",
		},
	}, func(any) {})
	if err != nil {
		t.Fatalf("send input or resume: %v", err)
	}

	waitSignal(t, resumed.started, "detached codex resume runner start")
	if !strings.HasPrefix(strings.ToLower(resumed.lastReq.Command), "codex resume thread-yolo") {
		t.Fatalf("expected codex resume command, got %q", resumed.lastReq.Command)
	}
	if resumed.lastReq.PermissionMode != "bypassPermissions" {
		t.Fatalf("expected bypass permission mode, got %q", resumed.lastReq.PermissionMode)
	}
	if resumed.lastReq.RuntimeMeta.CodexSandboxMode != "danger-full-access" {
		t.Fatalf("expected danger-full-access sandbox, got %q", resumed.lastReq.RuntimeMeta.CodexSandboxMode)
	}
	if len(resumed.writes) != 1 || string(resumed.writes[0]) != "check github\n" {
		t.Fatalf("unexpected resumed runner writes: %#v", resumed.writes)
	}
}

func TestSendInputOrResumeAllowsSlowDetachedResumeStartup(t *testing.T) {
	resumed := newResumeStubRunner("resume-detached", true)
	resumed.writeDelay = 4500 * time.Millisecond
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() engine.Runner { return newResumeStubRunner("", true) },
		NewPtyRunner:  func() engine.Runner { return resumed },
	})
	svc.manager.updateResumeSessionID("resume-detached")

	err := svc.SendInputOrResume(context.Background(), "s1", ExecuteRequest{
		Command:        "claude",
		CWD:            "/tmp",
		Mode:           engine.ModePTY,
		PermissionMode: "default",
		RuntimeMeta:    protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default", ResumeSessionID: "resume-detached"},
	}, InputRequest{Data: "slow hello\n", RuntimeMeta: protocol.RuntimeMeta{Source: "input"}}, func(any) {})
	if err != nil {
		t.Fatalf("send input or resume: %v", err)
	}
	if len(resumed.writes) != 1 || string(resumed.writes[0]) != "slow hello\n" {
		t.Fatalf("unexpected resumed runner writes: %#v", resumed.writes)
	}
}

func TestSendInputOrResumeWaitsForInteractiveBeforeSingleWrite(t *testing.T) {
	resumed := newResumeStubRunner("resume-detached", false)
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() engine.Runner { return newResumeStubRunner("", true) },
		NewPtyRunner:  func() engine.Runner { return resumed },
	})
	svc.manager.updateResumeSessionID("resume-detached")

	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.SendInputOrResume(context.Background(), "s1", ExecuteRequest{
			Command:        "claude",
			CWD:            "/tmp",
			Mode:           engine.ModePTY,
			PermissionMode: "default",
			RuntimeMeta:    protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default", ResumeSessionID: "resume-detached"},
		}, InputRequest{Data: "one write only\n", RuntimeMeta: protocol.RuntimeMeta{Source: "input"}}, func(any) {})
	}()

	waitSignal(t, resumed.started, "detached resume runner start")
	time.Sleep(100 * time.Millisecond)
	resumed.mu.Lock()
	writesBeforeReady := len(resumed.writes)
	resumed.mu.Unlock()
	if writesBeforeReady != 0 {
		t.Fatalf("expected no writes before interactive readiness, got %d", writesBeforeReady)
	}

	resumed.setInteractive(true)
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("send input or resume: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for resumed input send")
	}

	resumed.mu.Lock()
	defer resumed.mu.Unlock()
	if len(resumed.writes) != 1 || string(resumed.writes[0]) != "one write only\n" {
		t.Fatalf("unexpected resumed runner writes: %#v", resumed.writes)
	}
}

func TestSendInputWhenRunnerReadyMapsInternalDeadlineToRunnerTimeout(t *testing.T) {
	active := newResumeStubRunner("resume-active", true)
	active.writeDelay = 50 * time.Millisecond
	svc := NewService("s1", Dependencies{})
	if err := svc.manager.start("s1", active, nil, protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", ResumeSessionID: "resume-active"}); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	previousTimeout := resumedInputReadyTimeout
	resumedInputReadyTimeout = 10 * time.Millisecond
	defer func() {
		resumedInputReadyTimeout = previousTimeout
	}()

	err := svc.sendInputWhenRunnerReady(context.Background(), "s1", InputRequest{Data: "hello\n"}, func(any) {})
	if !errors.Is(err, ErrRunnerStartTimeout) {
		t.Fatalf("expected ErrRunnerStartTimeout, got %v", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("did not expect raw context deadline error, got %v", err)
	}
}

func TestSendInputOrResumeReturnsNoActiveRunnerWithoutResumeSession(t *testing.T) {
	svc := NewService("s1", Dependencies{})
	err := svc.SendInputOrResume(context.Background(), "s1", ExecuteRequest{
		Command:        "claude",
		CWD:            "/tmp",
		Mode:           engine.ModePTY,
		PermissionMode: "default",
		RuntimeMeta:    protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default"},
	}, InputRequest{Data: "hello\n"}, func(any) {})
	if !errors.Is(err, ErrNoActiveRunner) {
		t.Fatalf("expected ErrNoActiveRunner, got %v", err)
	}
}

func TestExecuteDefaultsToCodexWhenEngineIsCodex(t *testing.T) {
	pty := newResumeStubRunner("", true)
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() engine.Runner { return newResumeStubRunner("", true) },
		NewPtyRunner:  func() engine.Runner { return pty },
	})
	if err := svc.Execute(context.Background(), "s1", ExecuteRequest{
		Command:        "",
		CWD:            "/tmp",
		Mode:           engine.ModePTY,
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

func TestBuildDetachedResumeRequestForCodexDoesNotAppendClaudeFlags(t *testing.T) {
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() engine.Runner { return newResumeStubRunner("", true) },
		NewPtyRunner:  func() engine.Runner { return newResumeStubRunner("", true) },
	})
	svc.manager.updateResumeSessionID("resume-codex-123")
	req, err := svc.buildDetachedResumeRequest(ExecuteRequest{
		Command:        "codex -m gpt-5",
		CWD:            "/tmp",
		Mode:           engine.ModePTY,
		PermissionMode: "default",
		RuntimeMeta: protocol.RuntimeMeta{
			Command:          "codex -m gpt-5",
			Engine:           "codex",
			CodexSandboxMode: "danger-full-access",
			CWD:              "/tmp",
			ResumeSessionID:  "resume-codex-123",
			PermissionMode:   "default",
		},
	}, "config")
	if err != nil {
		t.Fatalf("buildDetachedResumeRequest: %v", err)
	}
	lower := strings.ToLower(req.Command)
	if !strings.HasPrefix(lower, "codex resume resume-codex-123") {
		t.Fatalf("expected codex resume command, got %q", req.Command)
	}
	if strings.Contains(lower, "--print") || strings.Contains(lower, "--input-format") || strings.Contains(lower, "--output-format") || strings.Contains(lower, "--permission-prompt-tool") {
		t.Fatalf("did not expect claude stream flags on codex command, got %q", req.Command)
	}
	if req.PermissionMode != "config" || req.RuntimeMeta.PermissionMode != "config" {
		t.Fatalf("expected codex config permission mode to be preserved, got req=%q meta=%q", req.PermissionMode, req.RuntimeMeta.PermissionMode)
	}
	if req.RuntimeMeta.CodexSandboxMode != "danger-full-access" {
		t.Fatalf("expected codex sandbox mode to be preserved, got %q", req.RuntimeMeta.CodexSandboxMode)
	}
}

func TestCompactRestartsDetachedCodexResumeSession(t *testing.T) {
	resumed := newResumeStubRunner("resume-codex-123", true)
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() engine.Runner { return newResumeStubRunner("", true) },
		NewPtyRunner:  func() engine.Runner { return resumed },
	})
	svc.manager.updateResumeSessionID("resume-codex-123")
	svc.manager.updateMeta(func(m *protocol.RuntimeMeta) {
		m.Command = "codex -m gpt-5"
		m.Engine = "codex"
		m.CWD = "/tmp"
		m.PermissionMode = "default"
		m.CodexSandboxMode = "danger-full-access"
		m.ResumeSessionID = "resume-codex-123"
	})

	err := svc.Compact(context.Background(), "s1", protocol.RuntimeMeta{}, func(any) {})
	if !errors.Is(err, engine.ErrInputNotSupported) {
		t.Fatalf("expected detached codex resume runner to restart into compact path and fail on missing compactor, got %v", err)
	}

	waitSignal(t, resumed.started, "detached codex compact resume runner start")
	lower := strings.ToLower(strings.TrimSpace(resumed.lastReq.Command))
	if !strings.HasPrefix(lower, "codex resume resume-codex-123") {
		t.Fatalf("expected codex resume command before compact, got %q", resumed.lastReq.Command)
	}
	if got := resumed.lastReq.RuntimeMeta.CodexSandboxMode; got != "danger-full-access" {
		t.Fatalf("expected stored codex sandbox mode on detached compact resume, got %q", got)
	}
}

func TestCompactDetachedCodexResumePrefersRequestRuntimeMeta(t *testing.T) {
	resumed := newResumeStubRunner("resume-request", true)
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() engine.Runner { return newResumeStubRunner("", true) },
		NewPtyRunner:  func() engine.Runner { return resumed },
	})
	svc.manager.updateResumeSessionID("resume-stale")
	svc.manager.updateMeta(func(m *protocol.RuntimeMeta) {
		m.Command = "codex resume resume-stale"
		m.Engine = "codex"
		m.CWD = "/tmp/stale"
		m.PermissionMode = "default"
		m.CodexSandboxMode = "workspace-write"
		m.ResumeSessionID = "resume-stale"
	})

	err := svc.Compact(context.Background(), "s1", protocol.RuntimeMeta{
		Command:          "codex -m gpt-5.5",
		Engine:           "codex",
		CWD:              "/tmp/request",
		PermissionMode:   "bypassPermissions",
		CodexSandboxMode: "danger-full-access",
		ResumeSessionID:  "resume-request",
	}, func(any) {})
	if !errors.Is(err, engine.ErrInputNotSupported) {
		t.Fatalf("expected detached codex resume runner to restart into compact path and fail on missing compactor, got %v", err)
	}

	waitSignal(t, resumed.started, "detached codex compact request-meta resume runner start")
	lower := strings.ToLower(strings.TrimSpace(resumed.lastReq.Command))
	if !strings.HasPrefix(lower, "codex resume resume-request") {
		t.Fatalf("expected request resume id to win over stale cache, got %q", resumed.lastReq.Command)
	}
	if strings.Contains(lower, "resume-stale") {
		t.Fatalf("did not expect stale resume id in command, got %q", resumed.lastReq.Command)
	}
	if resumed.lastReq.CWD != "/tmp/request" {
		t.Fatalf("expected request cwd on detached compact resume, got %q", resumed.lastReq.CWD)
	}
	if resumed.lastReq.PermissionMode != "bypassPermissions" {
		t.Fatalf("expected request permission mode on detached compact resume, got %q", resumed.lastReq.PermissionMode)
	}
	if got := resumed.lastReq.RuntimeMeta.CodexSandboxMode; got != "danger-full-access" {
		t.Fatalf("expected request codex sandbox mode on detached compact resume, got %q", got)
	}
}

func TestCompactWaitsForDetachedCodexResumeRunnerToBecomeInteractive(t *testing.T) {
	resumed := newResumeStubRunner("resume-codex-wait", false)
	resumed.runFn = func(ctx context.Context, req engine.ExecRequest, sink engine.EventSink) error {
		select {
		case resumed.started <- struct{}{}:
		default:
		}
		time.AfterFunc(120*time.Millisecond, func() {
			resumed.setInteractive(true)
		})
		<-ctx.Done()
		return nil
	}

	svc := NewService("s1", Dependencies{
		NewExecRunner: func() engine.Runner { return newResumeStubRunner("", true) },
		NewPtyRunner:  func() engine.Runner { return resumed },
	})
	svc.manager.updateResumeSessionID("resume-codex-wait")
	svc.manager.updateMeta(func(m *protocol.RuntimeMeta) {
		m.Command = "codex -m gpt-5"
		m.Engine = "codex"
		m.CWD = "/tmp"
		m.PermissionMode = "default"
		m.ResumeSessionID = "resume-codex-wait"
	})

	start := time.Now()
	err := svc.Compact(context.Background(), "s1", protocol.RuntimeMeta{}, func(any) {})
	if !errors.Is(err, engine.ErrInputNotSupported) {
		t.Fatalf("expected detached codex resume runner to reach compactor check after interactive wait, got %v", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("expected compact to wait for interactive resume readiness, elapsed=%v", elapsed)
	}
}
