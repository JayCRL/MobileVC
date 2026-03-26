package runtime

import (
	"context"
	"errors"
	"strings"
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
	if err := svc.manager.start("s1", first, protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default"}); err != nil {
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
	if second.lastReq.PermissionMode != "acceptEdits" {
		t.Fatalf("expected acceptEdits, got %#v", second.lastReq)
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
	snapshot := svc.RuntimeSnapshot()
	if !snapshot.TemporaryElevated {
		t.Fatal("expected temporary elevated snapshot")
	}
	if snapshot.SafePermissionMode != "default" {
		t.Fatalf("expected safe permission mode default, got %#v", snapshot)
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
	if err := svc.manager.start("s1", first, protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "acceptEdits", ResumeSessionID: "resume-234"}); err != nil {
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

func TestHotSwapApproveWithTemporaryElevationRequiresResumeSession(t *testing.T) {
	first := newHotSwapStubRunner("", true)
	svc := NewService("s1", Dependencies{})
	if err := svc.manager.start("s1", first, protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default"}); err != nil {
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
	if err := svc.manager.start("s1", first, protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "default", ResumeSessionID: "resume-345"}); err != nil {
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

func TestRestoreSafePermissionModeBeforeInputPropagatesWriteFailure(t *testing.T) {
	first := newHotSwapStubRunner("resume-456", true)
	second := newHotSwapStubRunner("resume-456", true)
	second.writeErr = errors.New("write failed")
	svc := NewService("s1", Dependencies{
		NewExecRunner: func() runner.Runner { return newHotSwapStubRunner("", true) },
		NewPtyRunner:  func() runner.Runner { return second },
	})
	if err := svc.manager.start("s1", first, protocol.RuntimeMeta{Command: "claude", CWD: "/tmp", PermissionMode: "acceptEdits", ResumeSessionID: "resume-456"}); err != nil {
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
