//go:build manual
// +build manual

package unit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mobilevc/internal/engine"
	"mobilevc/internal/protocol"
	"mobilevc/internal/session"
)

func TestClaudeSessionInProjectRoot(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not found")
	}

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir = dir + "/../.."
	t.Logf("project root: %s", dir)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	sessionID := "mobilevc-test-session"
	svc := session.NewService(sessionID, session.Dependencies{})
	col := &eventCollector{}
	svc.SetSink(col.emit)

	t.Log("starting claude...")
	if err := svc.Execute(ctx, sessionID, session.ExecuteRequest{
		Command:        "claude",
		CWD:            dir,
		Mode:           engine.ModePTY,
		PermissionMode: "default",
		RuntimeMeta:    protocol.RuntimeMeta{ExecutionID: "exec-manual"},
	}, col.emit); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && !svc.CanAcceptInteractiveInput() {
		time.Sleep(200 * time.Millisecond)
	}
	if !svc.CanAcceptInteractiveInput() {
		t.Fatal("claude not interactive")
	}

	sendAndWait := func(msg string) {
		t.Helper()
		t.Logf(">>> %s", msg)
		if err := svc.SendInput(ctx, sessionID, session.InputRequest{Data: msg}, col.emit); err != nil {
			t.Fatalf("SendInput: %v", err)
		}
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			for _, p := range col.promptRequests() {
				if p.PermissionRequestID == "" {
					continue
				}
				col.mu.Lock()
				if col.approvedPermIDs == nil {
					col.approvedPermIDs = make(map[string]bool)
				}
				done := col.approvedPermIDs[p.PermissionRequestID]
				if !done {
					col.approvedPermIDs[p.PermissionRequestID] = true
				}
				col.mu.Unlock()
				if done {
					continue
				}
				t.Logf("  approve: %s", p.Message)
				svc.SendPermissionDecision(ctx, sessionID, "approve",
					protocol.RuntimeMeta{Source: "permission-decision", TargetText: "approve", PermissionRequestID: p.PermissionRequestID},
					col.emit)
			}
			state := col.lastState()
			if state == "WAIT_INPUT" || state == "IDLE" {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	sendAndWait("你好，这是一条来自 MobileVC 自动化测试的消息，请回复：收到")
	sendAndWait("请用一句话介绍一下 Go 语言的优点")
	sendAndWait("谢谢，请总结一下我们刚才聊了什么")

	t.Logf("agent states: %d", len(col.agentStates()))
	for _, s := range col.agentStates() {
		t.Logf("  state=%s msg=%s", s.State, s.Message)
	}

	snap := svc.ControllerSnapshot()
	t.Logf("claude session ID: %s", snap.ResumeSession)

	svc.StopActive(sessionID, col.emit)
	svc.Cleanup()

	normalizeSessionJSONL(t, dir, snap.ResumeSession)
}

func normalizeSessionJSONL(t *testing.T, cwd, claudeID string) {
	t.Helper()
	if cwd == "" || claudeID == "" {
		return
	}
	resolved := cwd
	if abs, err := filepath.Abs(cwd); err == nil {
		resolved = abs
	}
	if eval, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = eval
	}
	home, _ := os.UserHomeDir()
	encoded := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, resolved)
	filePath := filepath.Join(home, ".claude", "projects", encoded, claudeID+".jsonl")

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Logf("normalize: cannot read %s: %v", filePath, err)
		return
	}
	lines := strings.Split(string(data), "\n")
	var newLines []string
	headerWritten := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !headerWritten && strings.Contains(line, `"type":"queue-operation"`) {
			continue
		}
		if !headerWritten {
			ts := time.Now().UTC().Format(time.RFC3339Nano)
			newLines = append(newLines,
				`{"type":"permission-mode","permissionMode":"default","sessionId":"`+claudeID+`","cwd":"`+resolved+`","timestamp":"`+ts+`","version":"2.1.119","entrypoint":"cli"}`,
				`{"type":"file-history-snapshot","sessionId":"`+claudeID+`","cwd":"`+resolved+`","timestamp":"`+ts+`","version":"2.1.119","entrypoint":"cli"}`,
			)
			headerWritten = true
		}
		newLines = append(newLines, line)
	}
	if len(newLines) > 0 {
		if err := os.WriteFile(filePath, []byte(strings.Join(newLines, "\n")+"\n"), 0o644); err != nil {
			t.Fatalf("normalize: write failed: %v", err)
		}
		t.Logf("normalize: session JSONL updated")
	}
}
