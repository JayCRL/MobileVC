package codexsync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mobilevc/internal/data"

	_ "modernc.org/sqlite"
)

func TestFindNativeThreadLoadsOnlyRequestedRollout(t *testing.T) {
	codexDir, db := setupCodexThreadsStore(t)
	targetID, _, _ := seedTwoCodexThreads(t, codexDir, db)
	writeHistoryFixture(t, filepath.Join(codexDir, "history.jsonl"), targetID, "target prompt")

	thread, err := FindNativeThread(context.Background(), targetID)
	if err != nil {
		t.Fatalf("find target thread: %v", err)
	}
	if thread.ThreadID != targetID {
		t.Fatalf("expected target thread, got %#v", thread)
	}
	if len(thread.HistoryPrompts) != 1 || thread.HistoryPrompts[0].Text != "target prompt" {
		t.Fatalf("expected target history only, got %#v", thread.HistoryPrompts)
	}
	if len(thread.LogEntries) != 1 || thread.LogEntries[0].Message != "target reply" {
		t.Fatalf("expected target rollout only, got %#v", thread.LogEntries)
	}
}

func TestFindNativeThreadIncludesCodexNativeToolAndPatchEvents(t *testing.T) {
	codexDir, db := setupCodexThreadsStore(t)
	threadID := "native-codex-thread"
	rolloutPath := filepath.Join(codexDir, "sessions", "native.jsonl")
	writeNativeCodexRolloutFixture(t, rolloutPath)
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC).Unix()
	insertThread(t, db, threadID, "/workspace", "Native Codex", rolloutPath, now)

	thread, err := FindNativeThread(context.Background(), threadID)
	if err != nil {
		t.Fatalf("find native thread: %v", err)
	}

	assertLogEntryContains(t, thread.LogEntries, "user", "请检查历史")
	assertLogEntryContains(t, thread.LogEntries, "markdown", "已经检查完成")
	assertLogEntryContains(t, thread.LogEntries, "system", "Codex 调用工具：exec_command")
	assertLogEntryContains(t, thread.LogEntries, "system", "Codex 工具输出")
	assertLogEntryContains(t, thread.LogEntries, "system", "Codex 调用工具：apply_patch")
	assertLogEntryContains(t, thread.LogEntries, "system", "Codex 应用补丁：success")
	output := findLogEntryContains(t, thread.LogEntries, "system", "Codex 工具输出")
	if output.Context == nil || output.Context.Tool != "exec_command" {
		t.Fatalf("expected tool output to inherit call tool, got %#v", output.Context)
	}
	if got := countLogEntryContains(thread.LogEntries, "markdown", "已经检查完成"); got != 1 {
		t.Fatalf("expected one assistant final answer, got %d entries: %#v", got, thread.LogEntries)
	}
}

func TestFindNativeThreadFoldsLargeCodexNativeToolOutput(t *testing.T) {
	codexDir, db := setupCodexThreadsStore(t)
	threadID := "native-codex-large-output"
	rolloutPath := filepath.Join(codexDir, "sessions", "large-output.jsonl")
	writeNativeCodexLargeOutputFixture(t, rolloutPath)
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC).Unix()
	insertThread(t, db, threadID, "/workspace", "Native Codex", rolloutPath, now)

	thread, err := FindNativeThread(context.Background(), threadID)
	if err != nil {
		t.Fatalf("find native thread: %v", err)
	}

	output := findLogEntryContains(t, thread.LogEntries, "system", "Codex 工具输出已折叠")
	if len(output.Message) > maxNativeToolOutputMessageBytes+256 {
		t.Fatalf("folded tool output still too large: %d bytes", len(output.Message))
	}
	if !strings.Contains(output.Message, "原始输出") {
		t.Fatalf("folded output should expose truncation details: %q", output.Message)
	}
	if output.Context == nil || output.Context.Status != "truncated" {
		t.Fatalf("expected truncated status, got %#v", output.Context)
	}
}

func TestListNativeThreadsDoesNotLoadRolloutHistory(t *testing.T) {
	codexDir, db := setupCodexThreadsStore(t)
	targetID, targetRollout, otherRollout := seedTwoCodexThreads(t, codexDir, db)
	writeHistoryFixture(t, filepath.Join(codexDir, "history.jsonl"), targetID, "target prompt")

	threads, err := ListNativeThreads(context.Background(), "/workspace")
	if err != nil {
		t.Fatalf("list native threads: %v", err)
	}
	if len(threads) != 2 {
		t.Fatalf("expected both workspace threads, got %#v", threads)
	}
	for _, thread := range threads {
		if len(thread.LogEntries) != 0 {
			t.Fatalf("session list should not load rollout entries, got %#v", thread)
		}
		if len(thread.HistoryPrompts) != 0 {
			t.Fatalf("session list should not scan history prompts, got %#v", thread)
		}
	}

	if err := os.Remove(targetRollout); err != nil {
		t.Fatalf("remove target rollout: %v", err)
	}
	if err := os.Remove(otherRollout); err != nil {
		t.Fatalf("remove other rollout: %v", err)
	}
	threads, err = ListNativeThreads(context.Background(), "/workspace")
	if err != nil {
		t.Fatalf("list native threads after rollout removal: %v", err)
	}
	if len(threads) != 2 {
		t.Fatalf("expected list to survive missing rollout files, got %#v", threads)
	}
}

func TestListNativeThreadsExcludesSubagentThreads(t *testing.T) {
	_, db := setupCodexThreadsStore(t)
	now := time.Date(2026, 5, 24, 1, 0, 0, 0, time.UTC).Unix()
	insertThreadWithSources(t, db, "main-thread", "/workspace", "Main", "", now, "cli", "")
	insertThreadWithSources(t, db, "subagent-thread", "/workspace", "Worker", "", now+1, "cli", "subagent")

	threads, err := ListNativeThreads(context.Background(), "/workspace")
	if err != nil {
		t.Fatalf("list native threads: %v", err)
	}
	if len(threads) != 1 || threads[0].ThreadID != "main-thread" {
		t.Fatalf("expected only main thread, got %#v", threads)
	}

	subagents, err := ListNativeSubagentThreadIDs(context.Background(), "/workspace")
	if err != nil {
		t.Fatalf("list subagent thread ids: %v", err)
	}
	if _, ok := subagents["subagent-thread"]; !ok || len(subagents) != 1 {
		t.Fatalf("expected subagent thread id, got %#v", subagents)
	}
}

func TestListNativeThreadsMatchesCodexTUIVisibleSources(t *testing.T) {
	_, db := setupCodexThreadsStore(t)
	now := time.Date(2026, 5, 24, 1, 0, 0, 0, time.UTC).Unix()
	insertThreadWithSources(t, db, "legacy-cli", "/workspace", "Legacy CLI", "", now, "cli", "")
	insertThreadWithSources(t, db, "legacy-vscode", "/workspace", "Legacy VSCode", "", now+1, "vscode", "")
	insertThreadWithSources(t, db, "new-user", "/workspace", "New user", "", now+2, "cli", "user")
	insertThreadWithSources(t, db, "exec-thread", "/workspace", "Exec", "", now+3, "exec", "")
	insertThreadWithSources(t, db, "subagent-thread", "/workspace", "Worker", "", now+4, "vscode", "subagent")
	insertThreadWithSources(t, db, "metadata-thread", "/workspace", "Metadata", "", now+5, "cli", "metadata")

	threads, err := ListNativeThreads(context.Background(), "/workspace")
	if err != nil {
		t.Fatalf("list native threads: %v", err)
	}
	got := make(map[string]bool)
	for _, thread := range threads {
		got[thread.ThreadID] = true
	}
	for _, id := range []string{"legacy-cli", "legacy-vscode", "new-user"} {
		if !got[id] {
			t.Fatalf("expected visible codex thread %q, got %#v", id, threads)
		}
	}
	for _, id := range []string{"exec-thread", "subagent-thread", "metadata-thread"} {
		if got[id] {
			t.Fatalf("did not expect hidden codex thread %q, got %#v", id, threads)
		}
	}

	hidden, err := ListNativeHiddenThreadIDs(context.Background(), "/workspace")
	if err != nil {
		t.Fatalf("list hidden native threads: %v", err)
	}
	for _, id := range []string{"exec-thread", "subagent-thread", "metadata-thread"} {
		if _, ok := hidden[id]; !ok {
			t.Fatalf("expected hidden thread id %q, got %#v", id, hidden)
		}
	}
	for _, id := range []string{"legacy-cli", "legacy-vscode", "new-user"} {
		if _, ok := hidden[id]; ok {
			t.Fatalf("did not expect visible thread id %q in hidden set %#v", id, hidden)
		}
	}
}

func TestListNativeThreadsSupportsLegacySchemaWithoutThreadSource(t *testing.T) {
	codexDir, db := setupCodexThreadsStoreWithoutThreadSource(t)
	now := time.Date(2026, 5, 24, 1, 0, 0, 0, time.UTC).Unix()
	rolloutPath := filepath.Join(codexDir, "sessions", "legacy.jsonl")
	writeRolloutFixture(t, rolloutPath, "legacy reply")
	if _, err := db.Exec(
		`insert into threads (id, cwd, title, model, source, model_provider, created_at, updated_at, first_user_message, rollout_path, archived)
		values ('legacy-thread', '/workspace', 'Legacy', 'gpt-5.5', 'cli', 'openai', ?, ?, 'Legacy', ?, 0)`,
		now,
		now,
		rolloutPath,
	); err != nil {
		t.Fatalf("insert legacy thread: %v", err)
	}
	if _, err := db.Exec(
		`insert into threads (id, cwd, title, model, source, model_provider, created_at, updated_at, first_user_message, rollout_path, archived)
		values ('legacy-exec-thread', '/workspace', 'Legacy exec', 'gpt-5.5', 'exec', 'openai', ?, ?, 'Legacy exec', ?, 0)`,
		now+1,
		now+1,
		rolloutPath,
	); err != nil {
		t.Fatalf("insert legacy exec thread: %v", err)
	}

	threads, err := ListNativeThreads(context.Background(), "/workspace")
	if err != nil {
		t.Fatalf("list native threads: %v", err)
	}
	if len(threads) != 1 || threads[0].ThreadID != "legacy-thread" {
		t.Fatalf("expected legacy thread, got %#v", threads)
	}
	hidden, err := ListNativeHiddenThreadIDs(context.Background(), "/workspace")
	if err != nil {
		t.Fatalf("list hidden legacy threads: %v", err)
	}
	if _, ok := hidden["legacy-exec-thread"]; !ok || len(hidden) != 1 {
		t.Fatalf("expected legacy exec thread hidden, got %#v", hidden)
	}

	subagents, err := ListNativeSubagentThreadIDs(context.Background(), "/workspace")
	if err != nil {
		t.Fatalf("list subagent thread ids: %v", err)
	}
	if len(subagents) != 0 {
		t.Fatalf("legacy schema should not infer subagents, got %#v", subagents)
	}
}

func setupCodexThreadsStore(t *testing.T) (string, *sql.DB) {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	codexDir := filepath.Join(homeDir, ".codex")
	if err := os.MkdirAll(filepath.Join(codexDir, "sessions"), 0o755); err != nil {
		t.Fatalf("mkdir codex sessions: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(codexDir, "state_5.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`create table threads (
		id text primary key,
		cwd text,
		title text,
		model text,
		source text,
		model_provider text,
		thread_source text,
		created_at integer,
		updated_at integer,
		first_user_message text,
		rollout_path text,
		archived integer default 0
	)`); err != nil {
		t.Fatalf("create threads table: %v", err)
	}
	return codexDir, db
}

func setupCodexThreadsStoreWithoutThreadSource(t *testing.T) (string, *sql.DB) {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	codexDir := filepath.Join(homeDir, ".codex")
	if err := os.MkdirAll(filepath.Join(codexDir, "sessions"), 0o755); err != nil {
		t.Fatalf("mkdir codex sessions: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(codexDir, "state_5.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`create table threads (
		id text primary key,
		cwd text,
		title text,
		model text,
		source text,
		model_provider text,
		created_at integer,
		updated_at integer,
		first_user_message text,
		rollout_path text,
		archived integer default 0
	)`); err != nil {
		t.Fatalf("create legacy threads table: %v", err)
	}
	return codexDir, db
}

func seedTwoCodexThreads(t *testing.T, codexDir string, db *sql.DB) (string, string, string) {
	t.Helper()
	targetID := "target-thread"
	targetRollout := filepath.Join(codexDir, "sessions", "target.jsonl")
	otherRollout := filepath.Join(codexDir, "sessions", "other.jsonl")
	writeRolloutFixture(t, targetRollout, "target reply")
	writeRolloutFixture(t, otherRollout, "other reply")
	now := time.Date(2026, 5, 24, 1, 0, 0, 0, time.UTC).Unix()
	insertThread(t, db, targetID, "/workspace", "Target", targetRollout, now)
	insertThread(t, db, "other-thread", "/workspace", "Other", otherRollout, now+1)
	return targetID, targetRollout, otherRollout
}

func insertThread(t *testing.T, db *sql.DB, id, cwd, title, rolloutPath string, updatedAt int64) {
	t.Helper()
	insertThreadWithSource(t, db, id, cwd, title, rolloutPath, updatedAt, "")
}

func insertThreadWithSource(t *testing.T, db *sql.DB, id, cwd, title, rolloutPath string, updatedAt int64, threadSource string) {
	t.Helper()
	insertThreadWithSources(t, db, id, cwd, title, rolloutPath, updatedAt, "cli", threadSource)
}

func insertThreadWithSources(t *testing.T, db *sql.DB, id, cwd, title, rolloutPath string, updatedAt int64, source, threadSource string) {
	t.Helper()
	if _, err := db.Exec(
		`insert into threads (id, cwd, title, model, source, model_provider, thread_source, created_at, updated_at, first_user_message, rollout_path, archived)
		values (?, ?, ?, 'gpt-5.5', ?, 'openai', ?, ?, ?, ?, ?, 0)`,
		id,
		cwd,
		title,
		source,
		threadSource,
		updatedAt,
		updatedAt,
		title,
		rolloutPath,
	); err != nil {
		t.Fatalf("insert thread %s: %v", id, err)
	}
}

func writeHistoryFixture(t *testing.T, path, sessionID, text string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create history fixture: %v", err)
	}
	defer file.Close()
	line := historyLine{SessionID: sessionID, TS: time.Now().Unix(), Text: text}
	if err := json.NewEncoder(file).Encode(line); err != nil {
		t.Fatalf("write history fixture: %v", err)
	}
}

func writeRolloutFixture(t *testing.T, path, message string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir rollout dir: %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create rollout fixture: %v", err)
	}
	defer file.Close()
	line := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"type":      "event_msg",
		"payload": map[string]any{
			"type":    "agent_message",
			"message": message,
		},
	}
	if err := json.NewEncoder(file).Encode(line); err != nil {
		t.Fatalf("write rollout fixture: %v", err)
	}
	if _, err := fmt.Fprintln(file); err != nil {
		t.Fatalf("terminate rollout fixture: %v", err)
	}
}

func writeNativeCodexRolloutFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir rollout dir: %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create native rollout fixture: %v", err)
	}
	defer file.Close()
	lines := []map[string]any{
		{
			"timestamp": "2026-05-28T12:00:00.000Z",
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "task_started",
				"turn_id": "turn-1",
			},
		},
		{
			"timestamp": "2026-05-28T12:00:01.000Z",
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "user_message",
				"message": "请检查历史",
			},
		},
		{
			"timestamp": "2026-05-28T12:00:02.000Z",
			"type":      "response_item",
			"payload": map[string]any{
				"type":      "function_call",
				"name":      "exec_command",
				"arguments": `{"cmd":"rg history"}`,
				"call_id":   "call-1",
			},
		},
		{
			"timestamp": "2026-05-28T12:00:03.000Z",
			"type":      "response_item",
			"payload": map[string]any{
				"type":    "function_call_output",
				"call_id": "call-1",
				"output":  "history parser result",
			},
		},
		{
			"timestamp": "2026-05-28T12:00:04.000Z",
			"type":      "response_item",
			"payload": map[string]any{
				"type":    "custom_tool_call",
				"name":    "apply_patch",
				"input":   "*** Begin Patch\n*** End Patch\n",
				"call_id": "call-2",
				"status":  "completed",
			},
		},
		{
			"timestamp": "2026-05-28T12:00:05.000Z",
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "patch_apply_end",
				"call_id": "call-2",
				"success": true,
				"stdout":  "Success. Updated the following files:\nM internal/data/codexsync/threads.go\n",
				"changes": map[string]any{
					"/workspace/internal/data/codexsync/threads.go": map[string]any{
						"type": "update",
					},
				},
			},
		},
		{
			"timestamp": "2026-05-28T12:00:06.000Z",
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "agent_message",
				"message": "已经检查完成",
			},
		},
		{
			"timestamp": "2026-05-28T12:00:06.000Z",
			"type":      "response_item",
			"payload": map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{
					{"text": "已经检查完成"},
				},
			},
		},
		{
			"timestamp": "2026-05-28T12:00:07.000Z",
			"type":      "event_msg",
			"payload": map[string]any{
				"type":               "task_complete",
				"turn_id":            "turn-1",
				"last_agent_message": "已经检查完成",
			},
		},
	}
	for _, line := range lines {
		if err := json.NewEncoder(file).Encode(line); err != nil {
			t.Fatalf("write native rollout fixture: %v", err)
		}
	}
}

func writeNativeCodexLargeOutputFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir rollout dir: %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create native rollout fixture: %v", err)
	}
	defer file.Close()
	lines := []map[string]any{
		{
			"timestamp": "2026-05-28T12:00:02.000Z",
			"type":      "response_item",
			"payload": map[string]any{
				"type":    "function_call",
				"name":    "exec_command",
				"call_id": "call-large",
			},
		},
		{
			"timestamp": "2026-05-28T12:00:03.000Z",
			"type":      "response_item",
			"payload": map[string]any{
				"type":    "function_call_output",
				"call_id": "call-large",
				"output":  strings.Repeat("0123456789abcdef\n", 2048),
			},
		},
	}
	for _, line := range lines {
		if err := json.NewEncoder(file).Encode(line); err != nil {
			t.Fatalf("write native rollout fixture: %v", err)
		}
	}
}

func assertLogEntryContains(t *testing.T, entries []data.SnapshotLogEntry, kind, text string) {
	t.Helper()
	if countLogEntryContains(entries, kind, text) == 0 {
		t.Fatalf("expected %s entry containing %q, got %#v", kind, text, entries)
	}
}

func findLogEntryContains(t *testing.T, entries []data.SnapshotLogEntry, kind, text string) data.SnapshotLogEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.Kind == kind && strings.Contains(entry.Message, text) {
			return entry
		}
	}
	t.Fatalf("expected %s entry containing %q, got %#v", kind, text, entries)
	return data.SnapshotLogEntry{}
}

func countLogEntryContains(entries []data.SnapshotLogEntry, kind, text string) int {
	count := 0
	for _, entry := range entries {
		if entry.Kind == kind && strings.Contains(entry.Message, text) {
			count++
		}
	}
	return count
}
