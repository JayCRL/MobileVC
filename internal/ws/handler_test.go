package ws

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"mobilevc/internal/protocol"
	"mobilevc/internal/runner"
	"mobilevc/internal/store"
)

type stubRunner struct {
	mu                 sync.Mutex
	events             []any
	writeCh            chan []byte
	writeErr           error
	holdOpen           bool
	lastPermissionMode string
	permissionModes    []string
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

func (s *stubRunner) SetPermissionMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastPermissionMode = mode
	s.permissionModes = append(s.permissionModes, mode)
}

func newTestHandler() *Handler {
	return NewHandler("test", nil)
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

type switchableStubRunner struct {
	mu       sync.Mutex
	writeCh  chan []byte
	sink     runner.EventSink
	req      runner.ExecRequest
	started  chan struct{}
	closed   chan struct{}
	closeErr error
}

func newSwitchableStubRunner() *switchableStubRunner {
	return &switchableStubRunner{
		writeCh: make(chan []byte, 8),
		started: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (s *switchableStubRunner) Run(ctx context.Context, req runner.ExecRequest, sink runner.EventSink) error {
	s.mu.Lock()
	s.req = req
	s.sink = sink
	s.mu.Unlock()
	close(s.started)
	<-ctx.Done()
	return s.closeErr
}

func (s *switchableStubRunner) Write(ctx context.Context, data []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.writeCh <- append([]byte(nil), data...):
		return nil
	}
}

func (s *switchableStubRunner) Close() error {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return nil
}

func (s *switchableStubRunner) Emit(event any) {
	s.mu.Lock()
	sink := s.sink
	s.mu.Unlock()
	if sink != nil {
		sink(event)
	}
}

func (s *switchableStubRunner) WaitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-s.started:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not start")
	}
}

func (s *switchableStubRunner) WaitClosed(t *testing.T) {
	t.Helper()
	select {
	case <-s.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("runner was not closed")
	}
}

func readInitialSessionID(t *testing.T, conn *websocket.Conn) string {
	t.Helper()
	first, second := readInitialEvents(t, conn)
	requireEventType(t, first, protocol.EventTypeSessionState)
	requireAgentState(t, second, "IDLE", false)
	sessionID, _ := first["sessionId"].(string)
	if sessionID == "" {
		t.Fatalf("expected initial session id, got %#v", first)
	}
	return sessionID
}

func readUntilType(t *testing.T, conn *websocket.Conn, want string) map[string]any {
	t.Helper()
	for i := 0; i < 20; i++ {
		event := readEventMap(t, conn)
		if event["type"] == want {
			return event
		}
	}
	t.Fatalf("did not receive %s event", want)
	return nil
}

func readUntilSessionHistory(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	return readUntilType(t, conn, protocol.EventTypeSessionHistory)
}

func readUntilSessionCreated(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	return readUntilType(t, conn, protocol.EventTypeSessionCreated)
}

func sessionLogTexts(record store.SessionRecord) []string {
	out := make([]string, 0, len(record.Projection.LogEntries))
	for _, entry := range record.Projection.LogEntries {
		switch entry.Kind {
		case "markdown", "system", "user":
			if strings.TrimSpace(entry.Message) != "" {
				out = append(out, entry.Message)
			}
		case "terminal":
			if strings.TrimSpace(entry.Text) != "" {
				out = append(out, entry.Text)
			}
		case "error":
			if entry.Context != nil && strings.TrimSpace(entry.Context.Message) != "" {
				out = append(out, entry.Context.Message)
			}
		case "step":
			if entry.Context != nil && strings.TrimSpace(entry.Context.Message) != "" {
				out = append(out, entry.Context.Message)
			}
		case "diff":
			if entry.Context != nil && strings.TrimSpace(entry.Context.Title) != "" {
				out = append(out, entry.Context.Title)
			}
		}
	}
	return out
}

func containsText(items []string, want string) bool {
	for _, item := range items {
		if strings.Contains(item, want) {
			return true
		}
	}
	return false
}

func TestHandlerExecFlow(t *testing.T) {
	execRunner := newStubRunner(
		protocol.NewLogEvent("ignored", "hello from runner", "stdout"),
		protocol.NewSessionStateEvent("ignored", "closed", "command finished"),
	)

	h := newTestHandler()
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

	h := newTestHandler()
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

	h := newTestHandler()
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
		protocol.NewPromptRequestEvent("ignored", "Claude 会话已就绪，可继续输入", nil),
	)

	h := newTestHandler()
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
	_ = readUntilType(t, conn, protocol.EventTypePromptRequest)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "WAIT_INPUT", true)
}

func TestHandlerInputWithoutRunner(t *testing.T) {
	h := newTestHandler()
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

	h := newTestHandler()
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

func TestHandlerRecoversRunnerPanicAndReturnsErrorEvent(t *testing.T) {
	var logs bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	defer log.SetOutput(originalWriter)
	defer log.SetFlags(originalFlags)

	h := newTestHandler()
	h.Upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	h.NewExecRunner = func() runner.Runner {
		return &panicRunner{}
	}

	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "panic please",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}

	event := readUntilType(t, conn, protocol.EventTypeError)
	if event["msg"] != "internal server error" {
		t.Fatalf("unexpected error event: %#v", event)
	}
	stack, _ := event["stack"].(string)
	if stack == "" {
		t.Fatalf("expected panic stack in error event, got %#v", event)
	}
	if !strings.Contains(logs.String(), "runner panic recovered") {
		t.Fatalf("expected runtime panic log, got %q", logs.String())
	}
}

type panicRunner struct{}

func (p *panicRunner) Run(ctx context.Context, req runner.ExecRequest, sink runner.EventSink) error {
	panic("boom")
}

func (p *panicRunner) Write(ctx context.Context, data []byte) error {
	return nil
}

func (p *panicRunner) Close() error {
	return nil
}

func (p *panicRunner) SetPermissionMode(mode string) {}

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
	h := newTestHandler()
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
	h := newTestHandler()
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
	h := newTestHandler()
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

func TestHandlerReviewDecisionSendsPromptToRunner(t *testing.T) {
	ptyRunner := newHoldingStubRunner(protocol.NewPromptRequestEvent("ignored", "等待输入", nil))
	h := newTestHandler()
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.ExecRequestEvent{ClientEvent: protocol.ClientEvent{Action: "exec"}, Command: "claude", Mode: "pty"}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)
	_ = readUntilType(t, conn, protocol.EventTypePromptRequest)
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)

	if err := conn.WriteJSON(protocol.ReviewDecisionRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "review_decision"},
		Decision:       "accept",
		ContextID:      "diff:1",
		ContextTitle:   "最近 Diff",
		TargetPath:     "internal/ws/handler.go",
		PermissionMode: "acceptEdits",
	}); err != nil {
		t.Fatalf("write review decision request: %v", err)
	}

	select {
	case payload := <-ptyRunner.writeCh:
		got := string(payload)
		if !strings.Contains(got, "请接受刚刚展示的 diff 变更") {
			t.Fatalf("unexpected review decision payload: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive review decision payload")
	}

	thinking := readUntilType(t, conn, protocol.EventTypeAgentState)
	if thinking["state"] != "THINKING" {
		t.Fatalf("expected THINKING state, got %#v", thinking)
	}
	if thinking["source"] != "review-decision" {
		t.Fatalf("expected review-decision source, got %#v", thinking)
	}
	if thinking["permissionMode"] != "acceptEdits" {
		t.Fatalf("expected acceptEdits permission mode, got %#v", thinking)
	}
}

func TestHandlerSetPermissionModeUpdatesRunner(t *testing.T) {
	ptyRunner := newHoldingStubRunner(protocol.NewPromptRequestEvent("ignored", "等待输入", nil))
	h := newTestHandler()
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.ExecRequestEvent{ClientEvent: protocol.ClientEvent{Action: "exec"}, Command: "claude", Mode: "pty", PermissionMode: "acceptEdits"}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)
	_ = readUntilType(t, conn, protocol.EventTypePromptRequest)
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)

	if err := conn.WriteJSON(protocol.PermissionModeUpdateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "set_permission_mode"}, PermissionMode: "default"}); err != nil {
		t.Fatalf("write permission mode request: %v", err)
	}

	state := readUntilType(t, conn, protocol.EventTypeAgentState)
	if state["permissionMode"] != "default" {
		t.Fatalf("expected updated permission mode, got %#v", state)
	}
	if ptyRunner.lastPermissionMode != "default" {
		t.Fatalf("expected runner permission mode to update, got %q", ptyRunner.lastPermissionMode)
	}
}

func TestHandlerSetPermissionModeUpdatesActiveRunner(t *testing.T) {
	ptyRunner := newHoldingStubRunner(protocol.NewPromptRequestEvent("ignored", "等待输入", nil))
	h := newTestHandler()
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "exec"},
		Command:        "claude",
		Mode:           "pty",
		PermissionMode: "acceptEdits",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)
	_ = readUntilType(t, conn, protocol.EventTypePromptRequest)
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)

	if err := conn.WriteJSON(protocol.PermissionModeUpdateRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "set_permission_mode"},
		PermissionMode: "default",
	}); err != nil {
		t.Fatalf("write set_permission_mode request: %v", err)
	}

	state := readUntilType(t, conn, protocol.EventTypeAgentState)
	if state["permissionMode"] != "default" {
		t.Fatalf("expected permissionMode to be default, got %#v", state)
	}
	if ptyRunner.lastPermissionMode != "default" {
		t.Fatalf("expected runner permission mode to update, got %q", ptyRunner.lastPermissionMode)
	}
}

func TestHandlerReviewDecisionWithoutRunner(t *testing.T) {
	h := newTestHandler()
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.ReviewDecisionRequestEvent{ClientEvent: protocol.ClientEvent{Action: "review_decision"}, Decision: "revert"}); err != nil {
		t.Fatalf("write review decision request: %v", err)
	}

	event := readUntilType(t, conn, protocol.EventTypeError)
	if event["msg"] != "当前没有可交互会话，请先恢复会话后再审核 diff" {
		t.Fatalf("unexpected error event: %#v", event)
	}
}

func TestHandlerReviewDecisionRejectsAcceptOutsideAcceptEdits(t *testing.T) {
	ptyRunner := newHoldingStubRunner(protocol.NewPromptRequestEvent("ignored", "等待输入", nil))
	h := newTestHandler()
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.ExecRequestEvent{ClientEvent: protocol.ClientEvent{Action: "exec"}, Command: "claude", Mode: "pty", PermissionMode: "default"}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)
	_ = readUntilType(t, conn, protocol.EventTypePromptRequest)
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)

	if err := conn.WriteJSON(protocol.ReviewDecisionRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "review_decision"},
		Decision:       "accept",
		ContextID:      "diff:1",
		ContextTitle:   "最近 Diff",
		TargetPath:     "internal/ws/handler.go",
		PermissionMode: "default",
	}); err != nil {
		t.Fatalf("write review decision request: %v", err)
	}

	event := readUntilType(t, conn, protocol.EventTypeError)
	if event["msg"] != "当前 permission mode 不是 acceptEdits，不能直接 accept diff" {
		t.Fatalf("unexpected error event: %#v", event)
	}

	select {
	case payload := <-ptyRunner.writeCh:
		t.Fatalf("expected no review payload to runner, got %q", string(payload))
	case <-time.After(200 * time.Millisecond):
	}
}

func TestHandlerReviewDecisionRejectsUnknownDecision(t *testing.T) {
	h := newTestHandler()
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.ReviewDecisionRequestEvent{ClientEvent: protocol.ClientEvent{Action: "review_decision"}, Decision: "shipit"}); err != nil {
		t.Fatalf("write review decision request: %v", err)
	}

	event := readUntilType(t, conn, protocol.EventTypeError)
	if event["msg"] != "review decision must be one of: accept, revert, revise" {
		t.Fatalf("unexpected error event: %#v", event)
	}
}

func TestHandlerRuntimeInfoReturnsContextSnapshot(t *testing.T) {
	h := newTestHandler()
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.RuntimeInfoRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "runtime_info"},
		Query:       "context",
		CWD:         ".",
	}); err != nil {
		t.Fatalf("write runtime_info request: %v", err)
	}

	event := readUntilType(t, conn, protocol.EventTypeRuntimeInfoResult)
	if event["query"] != "context" {
		t.Fatalf("expected context query, got %#v", event)
	}
	items, ok := event["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("expected runtime info items, got %#v", event)
	}
}

func TestHandlerRuntimeInfoRejectsUnknownQuery(t *testing.T) {
	h := newTestHandler()
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.RuntimeInfoRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "runtime_info"},
		Query:       "mystery",
		CWD:         ".",
	}); err != nil {
		t.Fatalf("write runtime_info request: %v", err)
	}

	event := readUntilType(t, conn, protocol.EventTypeError)
	if event["msg"] != "unsupported runtime_info query: mystery" {
		t.Fatalf("unexpected error event: %#v", event)
	}
}

func TestHandlerSlashCommandRuntimeInfoQueries(t *testing.T) {
	tests := []struct {
		name    string
		command string
		query   string
	}{
		{name: "help", command: "/help", query: "help"},
		{name: "context", command: "/context", query: "context"},
		{name: "model", command: "/model", query: "model"},
		{name: "cost", command: "/cost", query: "cost"},
		{name: "doctor", command: "/doctor", query: "doctor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler()
			conn := newTestConn(t, h)
			_, _ = readInitialEvents(t, conn)

			if err := conn.WriteJSON(protocol.SlashCommandRequestEvent{
				ClientEvent: protocol.ClientEvent{Action: "slash_command"},
				Command:     tt.command,
				CWD:         ".",
			}); err != nil {
				t.Fatalf("write slash command request: %v", err)
			}

			event := readUntilType(t, conn, protocol.EventTypeRuntimeInfoResult)
			if event["query"] != tt.query {
				t.Fatalf("expected query %q, got %#v", tt.query, event)
			}
		})
	}
}

func TestHandlerSlashCommandLocalOnlyCommands(t *testing.T) {
	tests := []string{"/clear", "/exit", "/quit", "/fast"}
	for _, command := range tests {
		t.Run(command, func(t *testing.T) {
			h := newTestHandler()
			conn := newTestConn(t, h)
			_, _ = readInitialEvents(t, conn)

			if err := conn.WriteJSON(protocol.SlashCommandRequestEvent{
				ClientEvent: protocol.ClientEvent{Action: "slash_command"},
				Command:     command,
			}); err != nil {
				t.Fatalf("write slash command request: %v", err)
			}

			event := readUntilType(t, conn, protocol.EventTypeRuntimeInfoResult)
			items, ok := event["items"].([]any)
			if !ok || len(items) == 0 {
				t.Fatalf("expected runtime info items, got %#v", event)
			}
			first, ok := items[0].(map[string]any)
			if !ok || first["status"] != "local-only" {
				t.Fatalf("expected local-only status, got %#v", event)
			}
		})
	}
}

func TestHandlerSlashCommandDiffRequiresContext(t *testing.T) {
	h := newTestHandler()
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.SlashCommandRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "slash_command"},
		Command:     "/diff",
	}); err != nil {
		t.Fatalf("write slash command request: %v", err)
	}

	event := readUntilType(t, conn, protocol.EventTypeError)
	if event["msg"] != "/diff requires targetDiff context" {
		t.Fatalf("unexpected error event: %#v", event)
	}
}

func TestHandlerSlashCommandExecMappings(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{name: "init", command: "/init", want: "claude /init"},
		{name: "compact", command: "/compact", want: "claude /compact"},
		{name: "run", command: "/run echo hi", want: "echo hi"},
		{name: "add-dir", command: "/add-dir /tmp/demo", want: "claude /add-dir /tmp/demo"},
		{name: "git commit quote", command: "/git commit hello", want: "git commit -m \"hello\""},
		{name: "test fallback", command: "/test path/to/file", want: "go test ./..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runnerStub := newHoldingStubRunner()
			h := newTestHandler()
			h.NewPtyRunner = func() runner.Runner { return runnerStub }
			conn := newTestConn(t, h)
			_, _ = readInitialEvents(t, conn)

			if err := conn.WriteJSON(protocol.SlashCommandRequestEvent{
				ClientEvent: protocol.ClientEvent{Action: "slash_command"},
				Command:     tt.command,
				CWD:         ".",
			}); err != nil {
				t.Fatalf("write slash command request: %v", err)
			}

			thinking := readUntilType(t, conn, protocol.EventTypeAgentState)
			if thinking["state"] != "THINKING" {
				t.Fatalf("expected THINKING state, got %#v", thinking)
			}
			select {
			case <-runnerStub.writeCh:
				// ignore stray writes
			default:
			}
		})
	}
}

func TestHandlerSessionDeleteRemovesHistorySessionFromList(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: "session-a"}); err != nil {
		t.Fatalf("write session create request: %v", err)
	}
	createdA := readUntilSessionCreated(t, conn)
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)
	summaryA, ok := createdA["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary payload, got %#v", createdA)
	}
	sessionA, _ := summaryA["id"].(string)
	if sessionA == "" {
		t.Fatalf("expected session A id, got %#v", createdA)
	}

	if err := conn.WriteJSON(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: "session-b"}); err != nil {
		t.Fatalf("write session create request: %v", err)
	}
	createdB := readUntilSessionCreated(t, conn)
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)
	summaryB, ok := createdB["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary payload, got %#v", createdB)
	}
	sessionB, _ := summaryB["id"].(string)
	if sessionB == "" || sessionB == sessionA {
		t.Fatalf("expected distinct session B id, got %q", sessionB)
	}

	if err := conn.WriteJSON(protocol.SessionDeleteRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_delete"}, SessionID: sessionA}); err != nil {
		t.Fatalf("write session delete request: %v", err)
	}

	listEvent := readUntilType(t, conn, protocol.EventTypeSessionListResult)
	items, ok := listEvent["items"].([]any)
	if !ok {
		t.Fatalf("expected session list items, got %#v", listEvent)
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["id"] == sessionA {
			t.Fatalf("expected deleted session removed from list, got %#v", items)
		}
	}
	if _, err := h.SessionStore.GetSession(context.Background(), sessionA); err == nil {
		t.Fatal("expected deleted history session lookup to fail")
	}
	if _, err := h.SessionStore.GetSession(context.Background(), sessionB); err != nil {
		t.Fatalf("expected current session to remain, got %v", err)
	}
}

func TestHandlerSessionDeleteCurrentSessionCleansRuntimeAndFallsBack(t *testing.T) {
	runnerA := newSwitchableStubRunner()
	firstRunner := runnerA
	runnerB := newSwitchableStubRunner()
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner {
		if runnerA != nil {
			r := runnerA
			runnerA = nil
			return r
		}
		return runnerB
	}
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: "session-a"}); err != nil {
		t.Fatalf("write initial session create request: %v", err)
	}
	createdA := readUntilSessionCreated(t, conn)
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)
	summaryA, ok := createdA["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary payload, got %#v", createdA)
	}
	sessionA, _ := summaryA["id"].(string)
	if sessionA == "" {
		t.Fatalf("expected session A id, got %#v", createdA)
	}

	if err := conn.WriteJSON(protocol.ExecRequestEvent{ClientEvent: protocol.ClientEvent{Action: "exec"}, Command: "claude", Mode: "pty"}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	firstRunner.WaitStarted(t)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "THINKING", false)

	if err := conn.WriteJSON(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: "session-b"}); err != nil {
		t.Fatalf("write session create request: %v", err)
	}
	createdB := readUntilSessionCreated(t, conn)
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)
	summaryB, ok := createdB["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary payload, got %#v", createdB)
	}
	sessionB, _ := summaryB["id"].(string)
	if sessionB == "" || sessionB == sessionA {
		t.Fatalf("expected distinct session B id, got %q", sessionB)
	}
	firstRunner.WaitClosed(t)

	if err := conn.WriteJSON(protocol.ExecRequestEvent{ClientEvent: protocol.ClientEvent{Action: "exec"}, Command: "claude", Mode: "pty"}); err != nil {
		t.Fatalf("write exec request for session B: %v", err)
	}
	runnerB.WaitStarted(t)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "THINKING", false)

	if err := conn.WriteJSON(protocol.SessionDeleteRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_delete"}, SessionID: sessionB}); err != nil {
		t.Fatalf("write session delete request: %v", err)
	}

	listEvent := readUntilType(t, conn, protocol.EventTypeSessionListResult)
	items, ok := listEvent["items"].([]any)
	if !ok {
		t.Fatalf("expected session list items, got %#v", listEvent)
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["id"] == sessionB {
			t.Fatalf("expected deleted current session removed from list, got %#v", items)
		}
	}
	history := readUntilSessionHistory(t, conn)
	if history["sessionId"] != sessionA {
		t.Fatalf("expected fallback history for session A, got %#v", history)
	}
	runnerB.WaitClosed(t)
	if _, err := h.SessionStore.GetSession(context.Background(), sessionB); err == nil {
		t.Fatal("expected deleted current session lookup to fail")
	}

	runnerB.Emit(protocol.NewLogEvent("ignored", "late output from deleted session B", "stdout"))
	runnerB.Emit(protocol.NewStepUpdateEvent("ignored", "late step from deleted session B", "running", "internal/ws/handler.go", "reading", "claude"))

	recordA, err := h.SessionStore.GetSession(context.Background(), sessionA)
	if err != nil {
		t.Fatalf("get session A: %v", err)
	}
	textsA := sessionLogTexts(recordA)
	if containsText(textsA, "late output from deleted session B") || containsText(textsA, "late step from deleted session B") {
		t.Fatalf("did not expect deleted session events to leak into fallback session, got %#v", textsA)
	}
}

func TestHandlerSessionLoadKeepsOldRunnerEventsInOriginalSessionProjection(t *testing.T) {
	runnerA := newSwitchableStubRunner()
	firstRunner := runnerA
	runnerB := newSwitchableStubRunner()

	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner {
		if runnerA != nil {
			r := runnerA
			runnerA = nil
			return r
		}
		return runnerB
	}

	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: "session-a"}); err != nil {
		t.Fatalf("write initial session create request: %v", err)
	}
	createdA := readUntilSessionCreated(t, conn)
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)
	summaryA, ok := createdA["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary payload, got %#v", createdA)
	}
	sessionA, _ := summaryA["id"].(string)
	if sessionA == "" {
		t.Fatalf("expected initial session id, got %#v", createdA)
	}

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "claude",
		Mode:        "pty",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	firstRunner.WaitStarted(t)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "THINKING", false)

	if err := conn.WriteJSON(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: "session-b"}); err != nil {
		t.Fatalf("write session create request: %v", err)
	}
	created := readUntilSessionCreated(t, conn)
	summary, ok := created["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary payload, got %#v", created)
	}
	sessionB, _ := summary["id"].(string)
	if sessionB == "" || sessionB == sessionA {
		t.Fatalf("expected new session id, got %q", sessionB)
	}
	firstRunner.WaitClosed(t)

	firstRunner.Emit(protocol.NewLogEvent("ignored", "late output from session A", "stdout"))
	firstRunner.Emit(protocol.NewStepUpdateEvent("ignored", "late step from session A", "running", "internal/ws/handler.go", "reading", "claude"))

	recordA, err := h.SessionStore.GetSession(context.Background(), sessionA)
	if err != nil {
		t.Fatalf("get session A: %v", err)
	}
	recordB, err := h.SessionStore.GetSession(context.Background(), sessionB)
	if err != nil {
		t.Fatalf("get session B: %v", err)
	}

	textsA := sessionLogTexts(recordA)
	textsB := sessionLogTexts(recordB)
	if !containsText(textsA, "late output from session A") {
		t.Fatalf("expected late output in session A projection, got %#v", textsA)
	}
	if !containsText(textsA, "late step from session A") {
		t.Fatalf("expected late step in session A projection, got %#v", textsA)
	}
	if containsText(textsB, "late output from session A") || containsText(textsB, "late step from session A") {
		t.Fatalf("did not expect session A events in session B projection, got %#v", textsB)
	}
}

func TestHandlerSessionLoadCleansUpPreviousRuntime(t *testing.T) {
	runnerA := newSwitchableStubRunner()
	firstRunner := runnerA
	runnerB := newSwitchableStubRunner()

	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner {
		if runnerA != nil {
			r := runnerA
			runnerA = nil
			return r
		}
		return runnerB
	}

	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: "session-a"}); err != nil {
		t.Fatalf("write initial session create request: %v", err)
	}
	createdA := readUntilSessionCreated(t, conn)
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)
	summaryA, ok := createdA["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary payload, got %#v", createdA)
	}
	sessionA, _ := summaryA["id"].(string)
	if sessionA == "" {
		t.Fatalf("expected initial session id, got %#v", createdA)
	}

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "claude",
		Mode:        "pty",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	firstRunner.WaitStarted(t)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "THINKING", false)

	if err := conn.WriteJSON(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: "session-b"}); err != nil {
		t.Fatalf("write session create request: %v", err)
	}
	created := readUntilSessionCreated(t, conn)
	summary, ok := created["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary payload, got %#v", created)
	}
	sessionB, _ := summary["id"].(string)
	if sessionB == "" || sessionB == sessionA {
		t.Fatalf("expected new session id, got %q", sessionB)
	}
	firstRunner.WaitClosed(t)

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "claude",
		Mode:        "pty",
	}); err != nil {
		t.Fatalf("write exec request for session B: %v", err)
	}
	runnerB.WaitStarted(t)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "THINKING", false)

	runnerB.Emit(protocol.NewLogEvent("ignored", "live output from session B", "stdout"))
	logEvent := readUntilType(t, conn, protocol.EventTypeLog)
	if logEvent["msg"] != "live output from session B" {
		t.Fatalf("unexpected log event payload: %#v", logEvent)
	}

	if err := conn.WriteJSON(protocol.SessionLoadRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_load"}, SessionID: sessionA}); err != nil {
		t.Fatalf("write session load request: %v", err)
	}
	history := readUntilSessionHistory(t, conn)
	if history["sessionId"] != sessionA {
		t.Fatalf("expected session history for session A, got %#v", history)
	}
	runnerB.WaitClosed(t)
}
