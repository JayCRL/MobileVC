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

	"mobilevc/internal/adapter"
	"mobilevc/internal/protocol"
	"mobilevc/internal/session"
)

const scannerMaxTokenSize = 1024 * 1024

type ExecRunner struct{}

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

	sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateActive), "command started"))

	if err := cmd.Start(); err != nil {
		sendEvent(sink, protocol.NewErrorEvent(req.SessionID, fmt.Sprintf("start command: %v", err), ""))
		return fmt.Errorf("start command: %w", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go r.streamOutput(&wg, stdoutPipe, req.SessionID, "stdout", sink)
	go r.streamOutput(&wg, stderrPipe, req.SessionID, "stderr", sink)

	waitErr := cmd.Wait()
	wg.Wait()

	if waitErr != nil {
		message := waitErr.Error()
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			message = fmt.Sprintf("command exited with code %d", exitErr.ExitCode())
		}
		sendEvent(sink, protocol.NewErrorEvent(req.SessionID, message, ""))
		sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished with error"))
		return waitErr
	}

	sendEvent(sink, protocol.NewSessionStateEvent(req.SessionID, string(session.StateClosed), "command finished"))
	return nil
}

func (r *ExecRunner) Write(ctx context.Context, data []byte) error {
	return ErrInputNotSupported
}

func (r *ExecRunner) Close() error {
	return nil
}

func (r *ExecRunner) streamOutput(wg *sync.WaitGroup, reader io.Reader, sessionID string, stream string, sink EventSink) {
	defer wg.Done()

	parser := adapter.NewGenericParser()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), scannerMaxTokenSize)

	for scanner.Scan() {
		for _, event := range parser.ParseLine(scanner.Text(), sessionID, stream) {
			sendEvent(sink, event)
		}
	}

	for _, event := range parser.Flush(sessionID, stream) {
		sendEvent(sink, event)
	}

	if err := scanner.Err(); err != nil {
		sendEvent(sink, protocol.NewErrorEvent(sessionID, fmt.Sprintf("read %s: %v", stream, err), ""))
	}
}

func sendEvent(sink EventSink, event any) {
	if sink != nil {
		sink(event)
	}
}
