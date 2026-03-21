package ws

import (
	"context"
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
	holdOpen bool
}

func newStubRunner(events ...any) *stubRunner {
	return &stubRunner{
		events:  events,
		writeCh: make(chan []byte, 8),
	}
}

func newHoldingStubRunner(events ...any) *stubRunner {
	runner := newStubRunner(events...)
	runner.holdOpen = true
	return runner
}

func (s *stubRunner) Run(ctx context.Context, req runner.ExecRequest, sink runner.EventSink) error {
	for _, event := range s.events {
		sink(event)
	}
	if !s.holdOpen {
		return nil
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

func newTestConn(t *testing.T, h *Handler) *websocket.Conn {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/?token=test"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	return conn
}

func readEventMap(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	var event map[string]any
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatalf("read event: %v", err)
	}
	return event
}

func readInitialEvents(t *testing.T, conn *websocket.Conn) (map[string]any, map[string]any) {
	t.Helper()
	first := readEventMap(t, conn)
	second := readEventMap(t, conn)
	return first, second
}

func requireEventType(t *testing.T, event map[string]any, want string) {
	t.Helper()
	if event["type"] != want {
		t.Fatalf("expected %s event, got %#v", want, event)
	}
}

func requireAgentState(t *testing.T, event map[string]any, wantState string, wantAwait bool) {
	t.Helper()
	requireEventType(t, event, protocol.EventTypeAgentState)
	if event["state"] != wantState {
		t.Fatalf("expected agent state %q, got %#v", wantState, event)
	}
	await, _ := event["awaitInput"].(bool)
	if await != wantAwait {
		t.Fatalf("expected awaitInput=%v, got %#v", wantAwait, event)
	}
}

func readUntilType(t *testing.T, conn *websocket.Conn, want string) map[string]any {
	t.Helper()
	for i := 0; i < 12; i++ {
		event := readEventMap(t, conn)
		if event["type"] == want {
			return event
		}
	}
	t.Fatalf("did not receive %s event", want)
	return nil
}

func TestHandlerExecFlow(t *testing.T) {
	execRunner := newStubRunner(
		protocol.NewLogEvent("ignored", "hello from runner", "stdout"),
		protocol.NewSessionStateEvent("ignored", "closed", "command finished"),
	)

	h := NewHandler("test")
	h.NewExecRunner = func() runner.Runner { return execRunner }

	conn := newTestConn(t, h)
	first, second := readInitialEvents(t, conn)
	requireEventType(t, first, protocol.EventTypeSessionState)
	requireAgentState(t, second, "IDLE", false)

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "printf 'ignored'",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}

	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "THINKING", false)
	if event := readUntilType(t, conn, protocol.EventTypeLog); event["msg"] != "hello from runner" || event["stream"] != "stdout" {
		t.Fatalf("expected stdout log event, got %#v", event)
	}
	if event := readUntilType(t, conn, protocol.EventTypeSessionState); event["state"] != "closed" {
		t.Fatalf("expected closed session event, got %#v", event)
	}
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "IDLE", false)
}

func TestHandlerPtyInputFlow(t *testing.T) {
	ptyRunner := newHoldingStubRunner(
		protocol.NewPromptRequestEvent("ignored", "Proceed? [y/N]", []string{"y", "n"}),
	)

	h := NewHandler("test")
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }

	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "printf 'ignored'",
		Mode:        "pty",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}

	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "THINKING", false)
	_ = readUntilType(t, conn, protocol.EventTypePromptRequest)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "WAIT_INPUT", true)

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

	requireAgentState(t, readEventMap(t, conn), "THINKING", false)
}

func TestHandlerEmitsAgentStateForToolEventsAndFinish(t *testing.T) {
	ptyRunner := newStubRunner(
		protocol.NewStepUpdateEvent("ignored", "Reading internal/ws/handler.go", "running", "internal/ws/handler.go", "reading", "Reading internal/ws/handler.go"),
		protocol.NewFileDiffEvent("ignored", "internal/ws/handler.go", "Updating internal/ws/handler.go", "diff --git a/internal/ws/handler.go b/internal/ws/handler.go", "go"),
	)

	h := NewHandler("test")
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }

	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "claude",
		Mode:        "pty",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}

	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "THINKING", false)
	_ = readUntilType(t, conn, protocol.EventTypeStepUpdate)
	toolEvent := readUntilType(t, conn, protocol.EventTypeAgentState)
	requireAgentState(t, toolEvent, "RUNNING_TOOL", false)
	if toolEvent["step"] != "Reading internal/ws/handler.go" {
		t.Fatalf("expected step in agent state, got %#v", toolEvent)
	}
	_ = readUntilType(t, conn, protocol.EventTypeFileDiff)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "RUNNING_TOOL", false)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "IDLE", false)
}

func TestHandlerClaudeSessionStartsInWaitInput(t *testing.T) {
	ptyRunner := newHoldingStubRunner(
		protocol.NewLogEvent("ignored", "Welcome back!", "stdout"),
	)

	h := NewHandler("test")
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }

	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "claude",
		Mode:        "pty",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}

	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "THINKING", false)
	_ = readUntilType(t, conn, protocol.EventTypeLog)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "WAIT_INPUT", true)
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
	var initialAgent map[string]any
	if err := conn.ReadJSON(&initialAgent); err != nil {
		t.Fatalf("read initial agent event: %v", err)
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
	execRunner := newHoldingStubRunner()
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

func TestHandlerSkillExecUsesUnifiedRuntimeFlow(t *testing.T) {
	execRunner := newStubRunner(
		protocol.NewLogEvent("ignored", "skill review output", "stdout"),
		protocol.NewSessionStateEvent("ignored", "closed", "command finished"),
	)

	h := NewHandler("test")
	h.NewExecRunner = func() runner.Runner { return execRunner }

	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.SkillRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "skill_exec"},
		Name:        "review",
		CWD:         ".",
		TargetType:  "current-diff",
		TargetPath:  "internal/ws/handler.go",
		TargetTitle: "当前 Diff",
		TargetDiff:  "diff --git a/internal/ws/handler.go b/internal/ws/handler.go",
	}); err != nil {
		t.Fatalf("write skill request: %v", err)
	}

	thinking := readUntilType(t, conn, protocol.EventTypeAgentState)
	requireAgentState(t, thinking, "THINKING", false)
	if thinking["skillName"] != "review" {
		t.Fatalf("expected skillName in agent state, got %#v", thinking)
	}
	if thinking["source"] != "skill-center" {
		t.Fatalf("expected source in agent state, got %#v", thinking)
	}
	logEvent := readUntilType(t, conn, protocol.EventTypeLog)
	if logEvent["skillName"] != "review" || logEvent["source"] != "skill-center" {
		t.Fatalf("expected runtime meta on log event, got %#v", logEvent)
	}
	finalState := readUntilType(t, conn, protocol.EventTypeAgentState)
	if finalState["state"] == "WAIT_INPUT" {
		finalState = readUntilType(t, conn, protocol.EventTypeAgentState)
	}
	requireAgentState(t, finalState, "IDLE", false)
}
