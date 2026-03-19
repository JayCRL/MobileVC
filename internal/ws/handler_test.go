package ws

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"mobilevc/internal/protocol"
	"mobilevc/internal/runner"
)

type stubRunner struct {
	mu       sync.Mutex
	events   []any
	writeCh  chan []byte
	writeErr error
}

func newStubRunner(events ...any) *stubRunner {
	return &stubRunner{
		events:  events,
		writeCh: make(chan []byte, 8),
	}
}

func (s *stubRunner) Run(ctx context.Context, req runner.ExecRequest, sink runner.EventSink) error {
	for _, event := range s.events {
		sink(event)
	}
	<-ctx.Done()
	return nil
}

func (s *stubRunner) Write(ctx context.Context, data []byte) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.writeCh <- append([]byte(nil), data...):
		return nil
	}
}

func (s *stubRunner) Close() error {
	return nil
}

func TestHandlerExecFlow(t *testing.T) {
	execRunner := newStubRunner(
		protocol.NewLogEvent("ignored", "hello from runner", "stdout"),
		protocol.NewSessionStateEvent("ignored", "closed", "command finished"),
	)

	h := NewHandler("test")
	h.NewExecRunner = func() runner.Runner { return execRunner }

	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/?token=test"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var initial protocol.SessionStateEvent
	if err := conn.ReadJSON(&initial); err != nil {
		t.Fatalf("read initial event: %v", err)
	}
	if initial.Type != protocol.EventTypeSessionState {
		t.Fatalf("expected session state event, got %#v", initial)
	}

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "printf 'ignored'",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}

	for i := 0; i < 4; i++ {
		var event map[string]any
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatalf("read event: %v", err)
		}
		if event["type"] == protocol.EventTypeLog && event["msg"] == "hello from runner" {
			if event["stream"] != "stdout" {
				t.Fatalf("expected stdout stream, got %#v", event)
			}
			return
		}
	}

	t.Fatal("did not receive expected log event")
}

func TestHandlerPtyInputFlow(t *testing.T) {
	ptyRunner := newStubRunner(
		protocol.NewPromptRequestEvent("ignored", "Proceed? [y/N]", []string{"y", "n"}),
	)

	h := NewHandler("test")
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }

	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/?token=test"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var initial protocol.SessionStateEvent
	if err := conn.ReadJSON(&initial); err != nil {
		t.Fatalf("read initial event: %v", err)
	}

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "printf 'ignored'",
		Mode:        "pty",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}

	for {
		var event map[string]any
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatalf("read event: %v", err)
		}
		if event["type"] == protocol.EventTypePromptRequest {
			break
		}
	}

	if err := conn.WriteJSON(protocol.InputRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "input"},
		Data:        "y\n",
	}); err != nil {
		t.Fatalf("write input request: %v", err)
	}

	select {
	case payload := <-ptyRunner.writeCh:
		if string(payload) != "y\n" {
			t.Fatalf("expected y\\n payload, got %q", string(payload))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive input payload")
	}
}

func TestHandlerInputWithoutRunner(t *testing.T) {
	h := NewHandler("test")
	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/?token=test"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var initial protocol.SessionStateEvent
	if err := conn.ReadJSON(&initial); err != nil {
		t.Fatalf("read initial event: %v", err)
	}

	if err := conn.WriteJSON(protocol.InputRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "input"},
		Data:        "x\n",
	}); err != nil {
		t.Fatalf("write input request: %v", err)
	}

	for {
		var event map[string]any
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatalf("read event: %v", err)
		}
		if event["type"] == protocol.EventTypeError {
			if event["msg"] != "no active runner" {
				t.Fatalf("unexpected error event: %#v", event)
			}
			return
		}
	}
}

func TestHandlerInputRejectedForExecRunner(t *testing.T) {
	execRunner := newStubRunner()
	execRunner.writeErr = runner.ErrInputNotSupported

	h := NewHandler("test")
	h.NewExecRunner = func() runner.Runner { return execRunner }

	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/?token=test"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var initial protocol.SessionStateEvent
	if err := conn.ReadJSON(&initial); err != nil {
		t.Fatalf("read initial event: %v", err)
	}

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "printf 'ignored'",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if err := conn.WriteJSON(protocol.InputRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "input"},
		Data:        "x\n",
	}); err != nil {
		t.Fatalf("write input request: %v", err)
	}

	for {
		var event map[string]any
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatalf("read event: %v", err)
		}
		if event["type"] == protocol.EventTypeError {
			if event["msg"] != "input is only supported for pty sessions" {
				t.Fatalf("unexpected error event: %#v", event)
			}
			return
		}
	}
}

func TestParseMode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    runner.Mode
		wantErr error
	}{
		{name: "default", input: "", want: runner.ModeExec},
		{name: "exec", input: "exec", want: runner.ModeExec},
		{name: "pty", input: "pty", want: runner.ModePTY},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMode(tt.input)
			if err != nil {
				t.Fatalf("parse mode returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}

	if _, err := parseMode("weird"); err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestHandlerRejectsEmptyInput(t *testing.T) {
	h := NewHandler("test")
	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/?token=test"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var initial protocol.SessionStateEvent
	if err := conn.ReadJSON(&initial); err != nil {
		t.Fatalf("read initial event: %v", err)
	}

	if err := conn.WriteJSON(protocol.InputRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "input"},
		Data:        "",
	}); err != nil {
		t.Fatalf("write input request: %v", err)
	}

	for {
		var event map[string]any
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatalf("read event: %v", err)
		}
		if event["type"] == protocol.EventTypeError {
			if event["msg"] != "input data is required" {
				t.Fatalf("unexpected error event: %#v", event)
			}
			return
		}
	}
}

func TestHandlerUnknownAction(t *testing.T) {
	h := NewHandler("test")
	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/?token=test"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var initial protocol.SessionStateEvent
	if err := conn.ReadJSON(&initial); err != nil {
		t.Fatalf("read initial event: %v", err)
	}

	if err := conn.WriteJSON(map[string]any{"action": "nope"}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	for {
		var event map[string]any
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatalf("read event: %v", err)
		}
		if event["type"] == protocol.EventTypeError {
			if event["msg"] != "unknown action: nope" {
				t.Fatalf("unexpected error event: %#v", event)
			}
			return
		}
	}
}

func TestHandlerUnknownMode(t *testing.T) {
	h := NewHandler("test")
	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/?token=test"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var initial protocol.SessionStateEvent
	if err := conn.ReadJSON(&initial); err != nil {
		t.Fatalf("read initial event: %v", err)
	}

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "printf 'ignored'",
		Mode:        "weird",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}

	for {
		var event map[string]any
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatalf("read event: %v", err)
		}
		if event["type"] == protocol.EventTypeError {
			if event["msg"] != "unknown mode: weird" {
				t.Fatalf("unexpected error event: %#v", event)
			}
			return
		}
	}
}

func TestHandlerMapsInputNotSupportedError(t *testing.T) {
	if !errors.Is(runner.ErrInputNotSupported, runner.ErrInputNotSupported) {
		t.Fatal("expected sentinel error")
	}
}
