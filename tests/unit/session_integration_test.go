package unit

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"mobilevc/internal/engine"
	"mobilevc/internal/protocol"
	"mobilevc/internal/session"
)

// hasClaude checks if the claude CLI is available and authenticated.
func hasClaude(t *testing.T) bool {
	t.Helper()
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not found in PATH")
		return false
	}
	// Quick check that Claude can actually run (has API key configured)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "--version")
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("claude CLI not functional: %v (output: %s)", err, string(out))
		return false
	}
	t.Logf("claude version: %s", strings.TrimSpace(string(out)))
	return true
}

// eventCollector captures events from a session into a slice.
type eventCollector struct {
	mu     sync.Mutex
	events []any
}

func (c *eventCollector) emit(event any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

func (c *eventCollector) collect() []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]any, len(c.events))
	copy(out, c.events)
	return out
}

func (c *eventCollector) agentStates() []protocol.AgentStateEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []protocol.AgentStateEvent
	for _, e := range c.events {
		if ev, ok := e.(protocol.AgentStateEvent); ok {
			out = append(out, ev)
		}
	}
	return out
}

func (c *eventCollector) logMessages() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, e := range c.events {
		if ev, ok := e.(protocol.LogEvent); ok {
			out = append(out, ev.Message)
		}
	}
	return out
}

func (c *eventCollector) lastState() string {
	states := c.agentStates()
	if len(states) == 0 {
		return ""
	}
	return states[len(states)-1].State
}

func (c *eventCollector) promptRequests() []protocol.PromptRequestEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []protocol.PromptRequestEvent
	for _, e := range c.events {
		if ev, ok := e.(protocol.PromptRequestEvent); ok {
			out = append(out, ev)
		}
	}
	return out
}

func (c *eventCollector) interactionRequests() []protocol.InteractionRequestEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []protocol.InteractionRequestEvent
	for _, e := range c.events {
		if ev, ok := e.(protocol.InteractionRequestEvent); ok {
			out = append(out, ev)
		}
	}
	return out
}

func (c *eventCollector) waitForPermissionPrompt(timeout time.Duration) (*protocol.PromptRequestEvent, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		prompts := c.promptRequests()
		for _, p := range prompts {
			if p.BlockingKind == "permission" && p.PermissionRequestID != "" {
				return &p, true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, false
}

func (c *eventCollector) waitForState(state string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if c.lastState() == state {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// TestClaudeSessionFullFlow runs a complete Claude session lifecycle:
// connect → start claude → send "你好" → receive response → finish
func TestClaudeSessionFullFlow(t *testing.T) {
	if !hasClaude(t) {
		return
	}

	// 1. Setup: create service with real runners
	dir := t.TempDir()
	sessionID := "test-session-claude"
	svc := session.NewService(sessionID, session.Dependencies{})

	collector := &eventCollector{}
	svc.SetSink(collector.emit)

	initial := svc.InitialEvent()
	t.Logf("initial state: %s", initial.State)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 2. Execute Claude in PTY mode
	execReq := session.ExecuteRequest{
		Command:        "claude",
		CWD:            dir,
		Mode:           engine.ModePTY,
		PermissionMode: "default",
		RuntimeMeta: protocol.RuntimeMeta{
			ExecutionID: "exec-1",
		},
	}

	t.Log("starting claude...")
	err := svc.Execute(ctx, sessionID, execReq, collector.emit)
	if err != nil {
		t.Fatalf("Execute claude: %v", err)
	}

	// 3. Wait for Claude to be ready (interactive or awaiting input)
	t.Log("waiting for claude to become interactive...")
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if svc.CanAcceptInteractiveInput() {
			t.Log("claude is interactive")
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !svc.CanAcceptInteractiveInput() {
		states := collector.agentStates()
		t.Logf("agent states during wait: %d events", len(states))
		for i, s := range states {
			t.Logf("  [%d] state=%s msg=%s", i, s.State, s.Message)
		}
		t.Fatal("claude did not become interactive within timeout")
	}

	// 4. Send "你好" as input
	t.Log("sending input: 你好")
	inputReq := session.InputRequest{Data: "你好"}
	err = svc.SendInput(ctx, sessionID, inputReq, collector.emit)
	if err != nil {
		t.Fatalf("SendInput: %v", err)
	}

	// 5. Wait for Claude to process and respond — watch for assistant reply
	t.Log("waiting for claude response...")
	deadline = time.Now().Add(30 * time.Second)
	hadReply := false
	for time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		msgs := collector.logMessages()
		for _, msg := range msgs {
			if strings.Contains(msg, "你好") || strings.Contains(msg, "hello") || strings.Contains(msg, "Hello") {
				hadReply = true
				t.Logf("claude echoed/responded: %s", msg)
				break
			}
		}
		if hadReply {
			break
		}
		// Also check if claude went back to idle (finished processing)
		if !svc.IsRunning() {
			t.Log("claude process finished")
			break
		}
	}

	// 6. Stop Claude and verify we got through the full lifecycle
	t.Log("stopping claude...")
	if err := svc.StopActive(sessionID, collector.emit); err != nil {
		t.Logf("StopActive: %v (may be normal if already finished)", err)
	}
	svc.Cleanup()

	finalState := collector.lastState()
	t.Logf("final state: %s", finalState)
	t.Logf("total events captured: %d", len(collector.collect()))
	t.Logf("agent state transitions: %d", len(collector.agentStates()))

	// Verify we went through the expected state transitions
	states := collector.agentStates()
	if len(states) == 0 {
		t.Fatal("no agent state events received")
	}

	// Should have seen THINKING at some point
	sawThinking := false
	sawReply := false
	for _, s := range states {
		t.Logf("  state=%s msg=%s", s.State, s.Message)
		if s.State == "THINKING" {
			sawThinking = true
		}
	}
	for _, msg := range collector.logMessages() {
		if strings.TrimSpace(msg) != "" {
			sawReply = true
			break
		}
	}

	if !sawThinking {
		t.Error("never entered THINKING state")
	}
	if !sawReply {
		t.Log("note: no assistant reply captured (may have been stopped before response)")
	}
}

// TestClaudeSessionFileWritePermission runs:
// connect → claude → "create smoketest.txt with 111" → approve permission → verify file
func TestClaudeSessionFileWritePermission(t *testing.T) {
	if !hasClaude(t) {
		return
	}

	dir := t.TempDir()
	sessionID := "test-session-filewrite"
	svc := session.NewService(sessionID, session.Dependencies{})

	collector := &eventCollector{}
	svc.SetSink(collector.emit)

	t.Logf("initial state: %s", svc.InitialEvent().State)
	t.Logf("working dir: %s", dir)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 1. Start Claude in PTY mode
	execReq := session.ExecuteRequest{
		Command:        "claude",
		CWD:            dir,
		Mode:           engine.ModePTY,
		PermissionMode: "default",
		RuntimeMeta:    protocol.RuntimeMeta{ExecutionID: "exec-fw"},
	}

	t.Log("starting claude...")
	if err := svc.Execute(ctx, sessionID, execReq, collector.emit); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// 2. Wait for interactive
	t.Log("waiting for interactive...")
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && !svc.CanAcceptInteractiveInput() {
		time.Sleep(200 * time.Millisecond)
	}
	if !svc.CanAcceptInteractiveInput() {
		t.Fatal("claude did not become interactive")
	}

	// 3. Send file creation request
	t.Log("sending: create smoketest.txt with 111")
	inputReq := session.InputRequest{Data: "在当前文件夹下新建文件 smoketest.txt，内容写入 111"}
	if err := svc.SendInput(ctx, sessionID, inputReq, collector.emit); err != nil {
		t.Fatalf("SendInput: %v", err)
	}

	// 4. Wait for permission prompt and approve it
	t.Log("waiting for permission prompt...")
	prompt, ok := collector.waitForPermissionPrompt(60 * time.Second)
	if ok {
		t.Logf("got permission prompt: id=%s msg=%s", prompt.PermissionRequestID, prompt.Message)
		t.Log("approving permission...")
		meta := protocol.RuntimeMeta{
			Source:              "permission-decision",
			TargetText:          "approve",
			PermissionRequestID: prompt.PermissionRequestID,
			PermissionMode:      "default",
		}
		if err := svc.SendPermissionDecision(ctx, sessionID, "approve", meta, collector.emit); err != nil {
			t.Fatalf("SendPermissionDecision: %v", err)
		}
		t.Log("permission approved, waiting for claude to finish...")
	} else {
		t.Log("no permission prompt — Claude may have auto-approved or is still processing")
	}

	// 5. Wait for Claude to finish (go back to WAIT_INPUT or idle)
	t.Log("waiting for claude to finish file operation...")
	deadline = time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		state := collector.lastState()
		if state == "WAIT_INPUT" || state == "IDLE" {
			t.Logf("claude finished, state=%s", state)
			break
		}
		if !svc.IsRunning() {
			t.Log("claude process stopped")
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// 6. Stop Claude
	t.Log("stopping claude...")
	svc.StopActive(sessionID, collector.emit)
	svc.Cleanup()

	// 7. Verify the file was created with expected content
	t.Log("verifying smoketest.txt...")
	content, err := os.ReadFile(dir + "/smoketest.txt")
	if err != nil {
		// Check all collected logs for clues
		t.Logf("agent states during test:")
		for _, s := range collector.agentStates() {
			t.Logf("  state=%s msg=%s", s.State, s.Message)
		}
		t.Logf("prompt requests:")
		for _, p := range collector.promptRequests() {
			t.Logf("  id=%s kind=%s msg=%s", p.PermissionRequestID, p.BlockingKind, p.Message)
		}
		t.Fatalf("smoketest.txt not found: %v", err)
	}
	got := strings.TrimSpace(string(content))
	t.Logf("file content: %q", got)
	if !strings.Contains(got, "111") {
		t.Errorf("expected file to contain '111', got %q", got)
	}

	// Log state transitions
	t.Log("state transitions:")
	for _, s := range collector.agentStates() {
		t.Logf("  state=%s msg=%s", s.State, s.Message)
	}

	t.Logf("total events: %d", len(collector.collect()))
}
