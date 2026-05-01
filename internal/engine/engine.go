package engine

import (
	"context"
	"errors"

	"mobilevc/internal/protocol"
)

// Mode specifies the execution mode.
type Mode string

const (
	ModeExec Mode = "exec"
	ModePTY  Mode = "pty"
)

var ErrInputNotSupported = errors.New("input not supported")
var ErrNoPendingControlRequest = errors.New("no pending control request")

// ExecRequest is the request to run a command via the engine.
type ExecRequest struct {
	Command        string
	CWD            string
	SessionID      string
	Mode           Mode
	PermissionMode string
	InitialInput   string
	protocol.RuntimeMeta
}

// EventSink is the callback for engine events.
type EventSink func(event any)

// Engine is the low-level AI CLI interaction interface.
// It encapsulates PTY/exec management and outputs standardized event streams.
type Engine interface {
	Run(ctx context.Context, req ExecRequest, sink EventSink) error
	Write(ctx context.Context, data []byte) error
	Close() error
}

// Runner is a type alias for backward compatibility.
type Runner = Engine

// ProcessRef identifies a running process.
type ProcessRef struct {
	RootPID     int
	ExecutionID string
	Command     string
	CWD         string
	Source      string
}

// ProcessProvider exposes the process reference.
type ProcessProvider interface {
	ProcessRef() ProcessRef
}

// InteractiveStateProvider reports whether the engine accepts interactive input.
type InteractiveStateProvider interface {
	CanAcceptInteractiveInput() bool
}

// PermissionResponseWriter handles permission decisions via stdio.
type PermissionResponseWriter interface {
	WritePermissionResponse(ctx context.Context, decision string) error
	HasPendingPermissionRequest() bool
	CurrentPermissionRequestID() string
}

// ClaudeSessionProvider exposes the underlying Claude session ID.
type ClaudeSessionProvider interface {
	ClaudeSessionID() string
}
