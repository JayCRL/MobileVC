package runner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"mobilevc/internal/adapter"
	"mobilevc/internal/protocol"
	"mobilevc/internal/session"
)

const scannerMaxTokenSize = 1024 * 1024

type ExecRunner struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	executionID string
	command     string
	cwd         string
}

func NewExecRunner() *ExecRunner {
	return &ExecRunner{}
}

func (r *ExecRunner) Run(ctx context.Context, req ExecRequest, sink EventSink) error {
	if req.SessionID == "" {
		return errors.New("session id is required")
	}
	if req.Command == "" {
		return errors.New("command is required")
	}

	cwd := req.CWD
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
	}

	executionID := fmt.Sprintf("exec-%d", time.Now().UTC().UnixNano())
	meta := protocol.RuntimeMeta{ExecutionID: executionID, Command: req.Command, CWD: cwd}

	cmd := newShellCommand(ctx, req.Command, req.Mode)
	cmd.Dir = cwd

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe: %w", err)
	}

	sendEvent(sink, protocol.ApplyRuntimeMeta(protocol.NewSessionStateEvent(req.SessionID, string(session.StateActive), "command started"), meta))

	if err := cmd.Start(); err != nil {
		exitCode := -1
		sendEvent(sink, protocol.NewExecutionLogEvent(req.SessionID, executionID, req.Command, "", "started", nil))
		sendEvent(sink, protocol.NewExecutionLogEvent(req.SessionID, executionID, fmt.Sprintf("start command: %v", err), "stderr", "stderr", nil))
		sendEvent(sink, protocol.NewExecutionLogEvent(req.SessionID, executionID, "", "", "finished", &exitCode))
		sendEvent(sink, protocol.ApplyRuntimeMeta(protocol.NewErrorEvent(req.SessionID, fmt.Sprintf("start command: %v", err), ""), meta))
		return fmt.Errorf("start command: %w", err)
	}

	sendEvent(sink, protocol.NewExecutionLogEvent(req.SessionID, executionID, req.Command, "", "started", nil))
	r.mu.Lock()
	r.cmd = cmd
	r.executionID = executionID
	r.command = req.Command
	r.cwd = cwd
	r.mu.Unlock()
	defer r.clear()

	var wg sync.WaitGroup
	wg.Add(2)
	go r.streamOutput(&wg, stdoutPipe, req.SessionID, executionID, "stdout", sink)
	go r.streamOutput(&wg, stderrPipe, req.SessionID, executionID, "stderr", sink)

	waitErr := cmd.Wait()
	wg.Wait()

	exitCode := 0
	if waitErr != nil {
		message := waitErr.Error()
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
			message = fmt.Sprintf("command exited with code %d", exitCode)
		} else {
			exitCode = -1
		}
		sendEvent(sink, protocol.NewExecutionLogEvent(req.SessionID, executionID, "", "", "finished", &exitCode))
		sendEvent(sink, protocol.ApplyRuntimeMeta(protocol.NewErrorEvent(req.SessionID, message, ""), meta))
		sendEvent(sink, protocol.ApplyRuntimeMeta(protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished with error"), meta))
		return waitErr
	}

	sendEvent(sink, protocol.NewExecutionLogEvent(req.SessionID, executionID, "", "", "finished", &exitCode))
	sendEvent(sink, protocol.ApplyRuntimeMeta(protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished"), meta))
	return nil
}

func (r *ExecRunner) Write(ctx context.Context, data []byte) error {
	return ErrInputNotSupported
}

func (r *ExecRunner) Close() error {
	r.mu.Lock()
	cmd := r.cmd
	r.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	return nil
}

func (r *ExecRunner) ProcessRef() ProcessRef {
	r.mu.Lock()
	defer r.mu.Unlock()
	ref := ProcessRef{
		ExecutionID: r.executionID,
		Command:     r.command,
		CWD:         r.cwd,
		Source:      "exec",
	}
	if r.cmd != nil && r.cmd.Process != nil {
		ref.RootPID = r.cmd.Process.Pid
	}
	return ref
}

func (r *ExecRunner) streamOutput(wg *sync.WaitGroup, reader io.Reader, sessionID string, executionID string, stream string, sink EventSink) {
	defer wg.Done()

	parser := adapter.NewGenericParser()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), scannerMaxTokenSize)

	for scanner.Scan() {
		for _, event := range parser.ParseLine(scanner.Text(), sessionID, stream) {
			sendEvent(sink, attachExecutionMeta(event, executionID, stream))
		}
	}

	for _, event := range parser.Flush(sessionID, stream) {
		sendEvent(sink, attachExecutionMeta(event, executionID, stream))
	}

	if err := scanner.Err(); err != nil {
		sendEvent(sink, protocol.ApplyRuntimeMeta(protocol.NewErrorEvent(sessionID, fmt.Sprintf("read %s: %v", stream, err), ""), protocol.RuntimeMeta{ExecutionID: executionID}))
	}
}

func attachExecutionMeta(event any, executionID string, stream string) any {
	event = protocol.ApplyRuntimeMeta(event, protocol.RuntimeMeta{ExecutionID: executionID})
	if logEvent, ok := event.(protocol.LogEvent); ok {
		if logEvent.Phase == "" {
			logEvent.Phase = stream
		}
		return logEvent
	}
	return event
}

func sendEvent(sink EventSink, event any) {
	if sink != nil {
		sink(event)
	}
}

func (r *ExecRunner) clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cmd = nil
	r.executionID = ""
	r.command = ""
	r.cwd = ""
}
