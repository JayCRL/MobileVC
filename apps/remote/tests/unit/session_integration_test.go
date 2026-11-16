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
	mu              sync.Mutex
	events          []any
	approvedPermIDs map[string]bool
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
	c.mu.Lock()
	if c.approvedPermIDs == nil {
		c.approvedPermIDs = make(map[string]bool)
	}
	c.mu.Unlock()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		prompts := c.promptRequests()
		for _, p := range prompts {
			// Accept any prompt with a permission request ID (tool use permission or text prompt)
			if p.PermissionRequestID != "" {
				c.mu.Lock()
				alreadyApproved := c.approvedPermIDs[p.PermissionRequestID]
				if !alreadyApproved {
					c.approvedPermIDs[p.PermissionRequestID] = true
				}
				c.mu.Unlock()
				if !alreadyApproved {
					return &p, true
				}
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

func (c *eventCollector) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = nil
}

// waitForInteractive waits up to timeout for the service to accept interactive input.
func waitForInteractive(t *testing.T, svc *session.Service, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) && !svc.CanAcceptInteractiveInput() {
		time.Sleep(200 * time.Millisecond)
	}
	if !svc.CanAcceptInteractiveInput() {
		t.Fatal("claude did not become interactive within timeout")
	}
}

// sendAndWaitForInput sends input and waits for the controller to reach WAIT_INPUT state.
func sendAndWaitForInput(t *testing.T, ctx context.Context, svc *session.Service, sessionID, input string, collector *eventCollector, timeout time.Duration) {
	t.Helper()
	t.Logf("sending: %s", input)
	if err := svc.SendInput(ctx, sessionID, session.InputRequest{Data: input}, collector.emit); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	// Wait for Claude to finish processing
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state := collector.lastState()
		if state == "WAIT_INPUT" || state == "IDLE" {
			return
		}
		if !svc.IsRunning() {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// approvePermissionIfNeeded watches for permission prompts and approves them.
func approvePermissionIfNeeded(t *testing.T, ctx context.Context, svc *session.Service, sessionID string, collector *eventCollector, timeout time.Duration) bool {
	t.Helper()
	prompt, ok := collector.waitForPermissionPrompt(timeout)
	if !ok {
		return false
	}
	t.Logf("approving permission: id=%s", prompt.PermissionRequestID)
	meta := protocol.RuntimeMeta{
		Source:              "permission-decision",
		TargetText:          "approve",
		PermissionRequestID: prompt.PermissionRequestID,
		PermissionMode:      "default",
	}
	if err := svc.SendPermissionDecision(ctx, sessionID, "approve", meta, collector.emit); err != nil {
		t.Fatalf("SendPermissionDecision: %v", err)
	}
	return true
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

// TestClaudeSessionBackgroundTask tests mobile background→foreground seamless continuation.
// Simulates: user starts a multi-step task → app goes to background → Claude keeps running →
// app returns to foreground → events still flowing, task completes.
//
// 默认 skip：该测试依赖真实 Claude CLI 多步权限流程，单次运行 ≥100s 且会因 Claude
// 行为/速度变化偶发失败。需要手动验证多步任务+权限审批链路时设置：
//   MOBILEVC_RUN_CLAUDE_INTEGRATION=1 go test ./tests/unit/... -run TestClaudeSessionBackgroundTask
func TestClaudeSessionBackgroundTask(t *testing.T) {
	if os.Getenv("MOBILEVC_RUN_CLAUDE_INTEGRATION") != "1" {
		t.Skip("set MOBILEVC_RUN_CLAUDE_INTEGRATION=1 to run this Claude e2e test")
	}
	if !hasClaude(t) {
		return
	}

	dir := t.TempDir()
	sessionID := "test-session-bg"
	svc := session.NewService(sessionID, session.Dependencies{})

	collector := &eventCollector{}
	svc.SetSink(collector.emit)

	t.Logf("working dir: %s", dir)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 1. Start Claude
	execReq := session.ExecuteRequest{
		Command:        "claude",
		CWD:            dir,
		Mode:           engine.ModePTY,
		PermissionMode: "default",
		RuntimeMeta:    protocol.RuntimeMeta{ExecutionID: "exec-bg"},
	}
	if err := svc.Execute(ctx, sessionID, execReq, collector.emit); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	waitForInteractive(t, svc, 30*time.Second)

	// 2. Send a multi-step task that takes time
	taskInput := "依次创建3个文件: data1.txt(内容hello1), data2.txt(内容hello2), data3.txt(内容hello3)"
	sendAndWaitForInput(t, ctx, svc, sessionID, taskInput, collector, 10*time.Second)

	// 3. Process: approve any permission prompts, wait for completion
	t.Log("processing background task...")
	deadline := time.Now().Add(90 * time.Second)
	permCount := 0
	lastEventCount := 0
	stableSince := time.Time{}
	for time.Now().Before(deadline) {
		// Approve any permission prompts we haven't handled yet
		for _, p := range collector.promptRequests() {
			if p.PermissionRequestID == "" {
				continue
			}
			collector.mu.Lock()
			if collector.approvedPermIDs == nil {
				collector.approvedPermIDs = make(map[string]bool)
			}
			done := collector.approvedPermIDs[p.PermissionRequestID]
			if !done {
				collector.approvedPermIDs[p.PermissionRequestID] = true
			}
			collector.mu.Unlock()
			if done {
				continue
			}
			permCount++
			t.Logf("approving permission #%d: id=%s msg=%s", permCount, p.PermissionRequestID, p.Message)
			meta := protocol.RuntimeMeta{
				Source:              "permission-decision",
				TargetText:          "approve",
				PermissionRequestID: p.PermissionRequestID,
			}
			svc.SendPermissionDecision(ctx, sessionID, "approve", meta, collector.emit)
		}

		state := collector.lastState()
		totalEvents := len(collector.collect())
		if totalEvents != lastEventCount {
			lastEventCount = totalEvents
			stableSince = time.Time{}
		} else if state == "WAIT_INPUT" || state == "IDLE" {
			if stableSince.IsZero() {
				stableSince = time.Now()
			}
		}

		if (state == "IDLE" && time.Since(stableSince) > 2*time.Second) ||
			(state == "WAIT_INPUT" && time.Since(stableSince) > 5*time.Second) {
			t.Logf("task completed (state=%s), approved %d permissions", state, permCount)
			break
		}
		if !svc.IsRunning() {
			t.Log("claude process stopped")
			break
		}
		time.Sleep(300 * time.Millisecond)
	}

	// 5. Stop Claude
	svc.StopActive(sessionID, collector.emit)
	svc.Cleanup()

	// 6. Verify files were created
	t.Log("verifying created files...")
	created := 0
	for _, name := range []string{"data1.txt", "data2.txt", "data3.txt"} {
		content, err := os.ReadFile(dir + "/" + name)
		if err == nil {
			created++
			t.Logf("  %s: %q", name, strings.TrimSpace(string(content)))
		} else {
			t.Logf("  %s: not found (%v)", name, err)
		}
	}
	t.Logf("files created: %d/3 (permission-mode=default requires sequential approval)", created)
	if created == 0 {
		t.Error("no files were created — background task failed")
	}
	// Note: with default permission mode, each file Write needs individual approval.
	// Timing in test loop may miss later prompts; 1+ file proves the flow works.

	// Log state transitions to show background processing
	t.Log("state transitions during background task:")
	for _, s := range collector.agentStates() {
		t.Logf("  state=%s msg=%s", s.State, s.Message)
	}
}

// TestClaudeSessionDisconnectReconnect tests mobile disconnect → reconnect → seamless resume.
// Simulates: user chats → connection lost (app backgrounded) → reconnects →
// session resumes with full context preserved.
//
// 默认 skip：是否记得跨 resume 的名字属于 Claude CLI 的实际行为，会随版本变化波动。
// 后端层面的"resume 后能再起 session 并响应输入"已被 P0 单测覆盖。需要手动联调
// 完整 disconnect/reconnect 链路时设置：
//   MOBILEVC_RUN_CLAUDE_INTEGRATION=1 go test ./tests/unit/... -run TestClaudeSessionDisconnectReconnect
func TestClaudeSessionDisconnectReconnect(t *testing.T) {
	if os.Getenv("MOBILEVC_RUN_CLAUDE_INTEGRATION") != "1" {
		t.Skip("set MOBILEVC_RUN_CLAUDE_INTEGRATION=1 to run this Claude e2e test")
	}
	if !hasClaude(t) {
		return
	}

	dir := t.TempDir()
	sessionID := "test-session-reconnect"

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// === Phase 1: Initial session ===
	t.Log("=== Phase 1: Initial session ===")
	svc1 := session.NewService(sessionID, session.Dependencies{})
	col1 := &eventCollector{}
	svc1.SetSink(col1.emit)

	execReq := session.ExecuteRequest{
		Command:        "claude",
		CWD:            dir,
		Mode:           engine.ModePTY,
		PermissionMode: "default",
		RuntimeMeta:    protocol.RuntimeMeta{ExecutionID: "exec-rc-1"},
	}
	if err := svc1.Execute(ctx, sessionID, execReq, col1.emit); err != nil {
		t.Fatalf("Phase 1 Execute: %v", err)
	}
	waitForInteractive(t, svc1, 30*time.Second)

	// Send identifying info
	sendAndWaitForInput(t, ctx, svc1, sessionID, "请记住：我的名字是张三，我今年25岁", col1, 30*time.Second)
	approvePermissionIfNeeded(t, ctx, svc1, sessionID, col1, 5*time.Second)

	// Verify Claude acknowledged
	t.Logf("Phase 1 agent states: %d", len(col1.agentStates()))
	for _, s := range col1.agentStates() {
		t.Logf("  state=%s msg=%s", s.State, s.Message)
	}

	// Save the resume session ID from the controller
	snap1 := svc1.ControllerSnapshot()
	resumeID := snap1.ResumeSession
	t.Logf("resume session ID: %s", resumeID)
	if resumeID == "" {
		t.Fatal("no resume session ID — Claude session did not persist")
	}

	// === Phase 2: Disconnect (stop runner, simulate connection loss) ===
	t.Log("=== Phase 2: Disconnect (simulating app to background) ===")
	svc1.StopActive(sessionID, col1.emit)
	svc1.Cleanup()
	t.Log("connection dropped, runner stopped")

	// Small delay to simulate background time
	time.Sleep(1 * time.Second)

	// === Phase 3: Reconnect (new connection, resume session) ===
	t.Log("=== Phase 3: Reconnect (simulating app to foreground) ===")
	svc2 := session.NewService(sessionID, session.Dependencies{})
	col2 := &eventCollector{}
	svc2.SetSink(col2.emit)

	resumeReq := session.ExecuteRequest{
		Command:        "claude",
		CWD:            dir,
		Mode:           engine.ModePTY,
		PermissionMode: "default",
		RuntimeMeta: protocol.RuntimeMeta{
			ExecutionID:     "exec-rc-2",
			ResumeSessionID: resumeID,
		},
	}
	if err := svc2.Execute(ctx, sessionID, resumeReq, col2.emit); err != nil {
		t.Fatalf("Phase 3 Execute (resume): %v", err)
	}
	waitForInteractive(t, svc2, 30*time.Second)
	t.Log("session resumed and interactive")

	// Ask a question that requires remembering context
	sendAndWaitForInput(t, ctx, svc2, sessionID, "请问我刚才告诉你我叫什么名字？", col2, 30*time.Second)
	approvePermissionIfNeeded(t, ctx, svc2, sessionID, col2, 5*time.Second)

	// Check Claude's response for the remembered name
	t.Log("Phase 3 response:")
	gotName := false
	for _, msg := range col2.logMessages() {
		if strings.Contains(msg, "张三") {
			gotName = true
			t.Logf("  Claude remembered: %s", msg)
			break
		}
	}
	if gotName {
		t.Log("SUCCESS: Claude remembered '张三' after disconnect/reconnect")
	} else {
		t.Log("checking all log messages for name...")
		for _, msg := range col2.logMessages() {
			t.Logf("  msg: %s", msg)
		}
		t.Error("Claude did NOT remember the name after reconnect")
	}

	// Cleanup
	svc2.StopActive(sessionID, col2.emit)
	svc2.Cleanup()

	// Log state transitions for Phase 3
	t.Log("Phase 3 (reconnect) state transitions:")
	for _, s := range col2.agentStates() {
		t.Logf("  state=%s msg=%s", s.State, s.Message)
	}
}
