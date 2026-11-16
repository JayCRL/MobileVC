package data

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"mobilevc/internal/protocol"
)

func TestFileStoreDeleteSessionRemovesRecordAndIndex(t *testing.T) {
	baseDir := t.TempDir()
	fs, err := NewFileStore(baseDir)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}

	created, err := fs.CreateSession(context.Background(), "delete-me")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := fs.DeleteSession(context.Background(), created.ID); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	if _, err := fs.GetSession(context.Background(), created.ID); err == nil {
		t.Fatal("expected deleted session lookup to fail")
	}
	if _, err := os.Stat(fs.sessionPath(created.ID)); !os.IsNotExist(err) {
		t.Fatalf("expected session file removed, got err=%v", err)
	}
	if _, err := os.Stat(fs.sessionLogEntriesPath(created.ID)); !os.IsNotExist(err) {
		t.Fatalf("expected session log sidecar removed, got err=%v", err)
	}
	if _, err := os.Stat(fs.sessionLogEntriesIndexPath(created.ID)); !os.IsNotExist(err) {
		t.Fatalf("expected session log index removed, got err=%v", err)
	}

	items, err := fs.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	for _, item := range items {
		if item.ID == created.ID {
			t.Fatalf("expected deleted session absent from index, got %#v", items)
		}
	}
}

func TestFileStoreDeleteSessionRecordsNativeClaudeTombstone(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "delete claude")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	record, err := fs.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	record.Summary.Runtime = SessionRuntime{
		ResumeSessionID: record.Summary.ClaudeSessionUUID,
		Command:         "claude --resume " + record.Summary.ClaudeSessionUUID,
		Engine:          "claude",
		Source:          "mobilevc",
	}
	record.Projection.Runtime = record.Summary.Runtime
	if _, err := fs.UpsertSession(context.Background(), record); err != nil {
		t.Fatalf("upsert claude session: %v", err)
	}

	if err := fs.DeleteSession(context.Background(), created.ID); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	deleted, err := fs.DeletedNativeSessionIDs(context.Background())
	if err != nil {
		t.Fatalf("deleted native session ids: %v", err)
	}
	if _, ok := deleted.ClaudeSessionIDs[record.Summary.ClaudeSessionUUID]; !ok {
		t.Fatalf("expected deleted Claude UUID %q, got %#v", record.Summary.ClaudeSessionUUID, deleted.ClaudeSessionIDs)
	}
}

func TestFileStoreDeleteSessionDoesNotDecodeHistoryRows(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "delete without history decode")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	entries := make([]SnapshotLogEntry, 0, 5)
	for i := 0; i < 5; i++ {
		entries = append(entries, SnapshotLogEntry{Kind: "user", Message: fmt.Sprintf("entry-%d", i)})
	}
	if _, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		LogEntries:          entries,
	}); err != nil {
		t.Fatalf("save projection: %v", err)
	}
	corruptLogEntrySidecarRow(t, fs, created.ID, 1)
	if _, err := fs.GetSessionHistoryWindow(context.Background(), SessionHistoryWindowRequest{
		SessionID: created.ID,
		Before:    1,
		Limit:     1,
	}); err == nil {
		t.Fatal("expected history page covering corrupted row to fail")
	}
	if err := fs.DeleteSession(context.Background(), created.ID); err != nil {
		t.Fatalf("delete should not decode history rows: %v", err)
	}
	if _, err := os.Stat(fs.sessionPath(created.ID)); !os.IsNotExist(err) {
		t.Fatalf("expected session file removed, got err=%v", err)
	}
	if _, err := os.Stat(fs.sessionLogEntriesPath(created.ID)); !os.IsNotExist(err) {
		t.Fatalf("expected session log sidecar removed, got err=%v", err)
	}
}

func TestFileStoreListSessionsDoesNotDecodeHistoryRows(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "list without history decode")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	entries := make([]SnapshotLogEntry, 0, 5)
	for i := 0; i < 5; i++ {
		entries = append(entries, SnapshotLogEntry{Kind: "user", Message: fmt.Sprintf("entry-%d", i)})
	}
	if _, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		LogEntries:          entries,
	}); err != nil {
		t.Fatalf("save projection: %v", err)
	}
	corruptLogEntrySidecarRow(t, fs, created.ID, 1)
	items, err := fs.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list should not decode history rows: %v", err)
	}
	found := false
	for _, item := range items {
		if item.ID == created.ID {
			found = true
			if item.EntryCount != len(entries) {
				t.Fatalf("expected entry count %d from lightweight summary, got %#v", len(entries), item)
			}
		}
	}
	if !found {
		t.Fatalf("expected session in list, got %#v", items)
	}
	if _, err := fs.GetSessionHistoryWindow(context.Background(), SessionHistoryWindowRequest{
		SessionID: created.ID,
		Before:    1,
		Limit:     1,
	}); err == nil {
		t.Fatal("expected history page covering corrupted row to still fail")
	}
}

func TestFileStoreListSessionsRepairsEmbeddedLegacySummaryWithoutSidecar(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "legacy embedded")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	now := time.Date(2026, 4, 1, 12, 15, 0, 0, time.UTC)
	legacyRecord := SessionRecord{
		Summary: SessionSummary{
			ID:        created.ID,
			Title:     "2026-04-01 20:15",
			CreatedAt: now,
			UpdatedAt: now,
			Runtime:   SessionRuntime{Source: "mobilevc"},
			Source:    "mobilevc",
			Ownership: "mobilevc",
		},
		Projection: ProjectionSnapshot{
			RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
			Runtime:             SessionRuntime{Source: "mobilevc"},
			LogEntries: []SnapshotLogEntry{
				{Kind: "user", Message: "修复 legacy 标题"},
				{Kind: "user", Message: "修复 legacy preview"},
			},
		},
	}
	data, err := json.MarshalIndent(legacyRecord, "", "  ")
	if err != nil {
		t.Fatalf("marshal legacy record: %v", err)
	}
	if err := os.WriteFile(fs.sessionPath(created.ID), data, 0o644); err != nil {
		t.Fatalf("write legacy record: %v", err)
	}
	if err := os.WriteFile(fs.sessionLogEntriesPath(created.ID), []byte("{bad sidecar"), 0o644); err != nil {
		t.Fatalf("write corrupt sidecar: %v", err)
	}
	indexData, err := json.MarshalIndent(fileIndex{Sessions: []SessionSummary{legacyRecord.Summary}}, "", "  ")
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	if err := os.WriteFile(fs.indexPath, indexData, 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	items, err := fs.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list should repair embedded legacy summary without reading sidecar: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one item, got %#v", items)
	}
	if items[0].Title != "修复 legacy 标题" || items[0].LastPreview != "修复 legacy preview" {
		t.Fatalf("expected repaired summary from embedded log entries, got %#v", items[0])
	}
}

func TestFileStoreGetSessionHistoryWindowReturnsTailWithoutRecordLogEntries(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "window")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	entries := make([]SnapshotLogEntry, 0, 250)
	for i := 0; i < 250; i++ {
		entries = append(entries, SnapshotLogEntry{
			Kind:      "user",
			Message:   fmt.Sprintf("entry-%03d", i),
			Timestamp: time.Unix(int64(i), 0).UTC().Format(time.RFC3339),
		})
	}
	if _, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "terminal", "stderr": ""},
		LogEntries:          entries,
		Runtime:             SessionRuntime{Command: "codex", Engine: "codex"},
	}); err != nil {
		t.Fatalf("save projection: %v", err)
	}

	window, err := fs.GetSessionHistoryWindow(context.Background(), SessionHistoryWindowRequest{
		SessionID: created.ID,
		Limit:     120,
	})
	if err != nil {
		t.Fatalf("get history window: %v", err)
	}
	if window.LogEntryStart != 130 || window.LogEntryTotal != 250 {
		t.Fatalf("unexpected window metadata: start=%d total=%d", window.LogEntryStart, window.LogEntryTotal)
	}
	if got := len(window.LogEntries); got != 120 {
		t.Fatalf("expected 120 entries, got %d", got)
	}
	if window.LogEntries[0].Timestamp != entries[130].Timestamp || window.LogEntries[119].Timestamp != entries[249].Timestamp {
		t.Fatalf("unexpected tail entries: first=%#v last=%#v", window.LogEntries[0], window.LogEntries[119])
	}
	if got := len(window.Record.Projection.LogEntries); got != 0 {
		t.Fatalf("expected lightweight record to omit log entries, got %d", got)
	}
	if window.Record.Summary.EntryCount != 250 {
		t.Fatalf("expected summary entry count to remain total, got %#v", window.Record.Summary)
	}
	if window.Record.Projection.Runtime.Command != "codex" {
		t.Fatalf("expected lightweight runtime metadata, got %#v", window.Record.Projection.Runtime)
	}
	terminalRange, err := fs.GetSessionTerminalRange(context.Background(), SessionTerminalRangeRequest{
		SessionID: created.ID,
		Stream:    "stdout",
	})
	if err != nil {
		t.Fatalf("get terminal range: %v", err)
	}
	if terminalRange.Content != "terminal" {
		t.Fatalf("expected terminal sidecar output, got %#v", terminalRange)
	}
	recordBytes, err := os.ReadFile(fs.sessionPath(created.ID))
	if err != nil {
		t.Fatalf("read session record: %v", err)
	}
	var stored map[string]any
	if err := json.Unmarshal(recordBytes, &stored); err != nil {
		t.Fatalf("decode stored lightweight record: %v", err)
	}
	projection, ok := stored["projection"].(map[string]any)
	if !ok {
		t.Fatalf("expected projection object in stored record, got %#v", stored["projection"])
	}
	if raw, exists := projection["logEntries"]; exists {
		if items, ok := raw.([]any); !ok || len(items) != 0 {
			t.Fatalf("expected canonical record to omit log entries, got %#v", raw)
		}
	}
	fullRecord, err := fs.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get full session: %v", err)
	}
	if got := len(fullRecord.Projection.LogEntries); got != 250 {
		t.Fatalf("expected GetSession to hydrate all entries from sidecar, got %d", got)
	}
}

func corruptLogEntrySidecarRow(t *testing.T, fs *FileStore, sessionID string, row int) {
	t.Helper()
	raw, err := os.ReadFile(fs.sessionLogEntriesPath(sessionID))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	lines := bytes.Split(raw, []byte("\n"))
	if row <= 0 || row >= len(lines) {
		t.Fatalf("sidecar row %d out of range for %q", row, string(raw))
	}
	lines[row] = bytes.Repeat([]byte(" "), len(lines[row]))
	copy(lines[row], []byte("{bad json}"))
	var rebuilt bytes.Buffer
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		rebuilt.Write(line)
		rebuilt.WriteByte('\n')
	}
	if err := os.WriteFile(fs.sessionLogEntriesPath(sessionID), rebuilt.Bytes(), 0o644); err != nil {
		t.Fatalf("write corrupted sidecar row: %v", err)
	}
}

func TestFileStoreGetSessionHistoryWindowReturnsEarlierPage(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "window-page")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	entries := make([]SnapshotLogEntry, 0, 10)
	for i := 0; i < 10; i++ {
		entries = append(entries, SnapshotLogEntry{Kind: "user", Message: fmt.Sprintf("%d", i)})
	}
	if _, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		LogEntries:          entries,
	}); err != nil {
		t.Fatalf("save projection: %v", err)
	}

	window, err := fs.GetSessionHistoryWindow(context.Background(), SessionHistoryWindowRequest{
		SessionID: created.ID,
		Before:    6,
		Limit:     3,
	})
	if err != nil {
		t.Fatalf("get page window: %v", err)
	}
	if window.LogEntryStart != 3 || window.LogEntryTotal != 10 {
		t.Fatalf("unexpected page metadata: start=%d total=%d", window.LogEntryStart, window.LogEntryTotal)
	}
	got := []string{window.LogEntries[0].Message, window.LogEntries[1].Message, window.LogEntries[2].Message}
	want := []string{"3", "4", "5"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected page %v, got %v", want, got)
		}
	}
}

func TestFileStoreGetSessionHistoryWindowRebuildsMissingSidecar(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "legacy-window")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	legacy := SessionRecord{
		Summary: SessionSummary{
			ID:        created.ID,
			Title:     "legacy-window",
			CreatedAt: created.CreatedAt,
			UpdatedAt: created.UpdatedAt,
			Runtime:   SessionRuntime{Source: "mobilevc"},
		},
		Projection: ProjectionSnapshot{
			RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
			LogEntries: []SnapshotLogEntry{
				{Kind: "user", Message: "a"},
				{Kind: "markdown", Message: "b"},
			},
			Runtime: SessionRuntime{Source: "mobilevc"},
		},
	}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatalf("marshal legacy record: %v", err)
	}
	if err := os.WriteFile(fs.sessionPath(created.ID), data, 0o644); err != nil {
		t.Fatalf("write legacy record: %v", err)
	}
	if err := os.Remove(fs.sessionLogEntriesPath(created.ID)); err != nil {
		t.Fatalf("remove sidecar: %v", err)
	}
	if err := os.Remove(fs.sessionLogEntriesIndexPath(created.ID)); err != nil {
		t.Fatalf("remove sidecar index: %v", err)
	}

	window, err := fs.GetSessionHistoryWindow(context.Background(), SessionHistoryWindowRequest{
		SessionID: created.ID,
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("get history window: %v", err)
	}
	if window.LogEntryTotal != 2 || len(window.LogEntries) != 1 || window.LogEntries[0].Message != "b" {
		t.Fatalf("unexpected rebuilt window: %#v", window)
	}
	if _, err := os.Stat(fs.sessionLogEntriesPath(created.ID)); err != nil {
		t.Fatalf("expected sidecar rebuilt, got err=%v", err)
	}
	if _, err := os.Stat(fs.sessionLogEntriesIndexPath(created.ID)); err != nil {
		t.Fatalf("expected sidecar index rebuilt, got err=%v", err)
	}
}

func TestFileStoreGetSessionHistoryWindowMigratesLegacySidecar(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "legacy-sidecar")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		LogEntries: []SnapshotLogEntry{
			{Kind: "user", Message: "a"},
			{Kind: "markdown", Message: "b"},
			{Kind: "user", Message: "c"},
		},
	}); err != nil {
		t.Fatalf("save projection: %v", err)
	}
	if err := os.Remove(fs.sessionLogEntriesIndexPath(created.ID)); err != nil {
		t.Fatalf("remove sidecar index: %v", err)
	}
	legacy := sessionLogEntriesSidecar{
		SessionID:  created.ID,
		EntryCount: 3,
		LogEntries: []SnapshotLogEntry{
			{Kind: "user", Message: "a"},
			{Kind: "markdown", Message: "b"},
			{Kind: "user", Message: "c"},
		},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy sidecar: %v", err)
	}
	if err := os.WriteFile(fs.sessionLogEntriesPath(created.ID), data, 0o644); err != nil {
		t.Fatalf("write legacy sidecar: %v", err)
	}

	window, err := fs.GetSessionHistoryWindow(context.Background(), SessionHistoryWindowRequest{
		SessionID: created.ID,
		Limit:     2,
	})
	if err != nil {
		t.Fatalf("get history window: %v", err)
	}
	if window.LogEntryTotal != 3 || window.LogEntryStart != 1 || len(window.LogEntries) != 2 || window.LogEntries[0].Message != "b" {
		t.Fatalf("unexpected migrated window: %#v", window)
	}
	if _, err := os.Stat(fs.sessionLogEntriesIndexPath(created.ID)); err != nil {
		t.Fatalf("expected sidecar index created, got err=%v", err)
	}
	raw, err := os.ReadFile(fs.sessionLogEntriesPath(created.ID))
	if err != nil {
		t.Fatalf("read migrated sidecar: %v", err)
	}
	if len(raw) == 0 || raw[0] != '{' || json.Valid(raw) {
		t.Fatalf("expected migrated JSONL sidecar, got %q", string(raw))
	}
}

func TestFileStoreGetSessionHistoryWindowFailsOnSidecarMismatch(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "bad-sidecar")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		LogEntries:          []SnapshotLogEntry{{Kind: "user", Message: "a"}},
	}); err != nil {
		t.Fatalf("save projection: %v", err)
	}
	raw, err := os.ReadFile(fs.sessionLogEntriesPath(created.ID))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	lines := bytes.SplitN(raw, []byte("\n"), 2)
	if len(lines) != 2 {
		t.Fatalf("expected JSONL sidecar, got %q", string(raw))
	}
	badHeader := sessionLogEntriesSidecarHeader{
		Version:    sessionLogEntriesSidecarVersion,
		SessionID:  "other-session",
		EntryCount: 1,
	}
	headerData, err := json.Marshal(badHeader)
	if err != nil {
		t.Fatalf("marshal bad header: %v", err)
	}
	raw = append(append(headerData, '\n'), lines[1]...)
	if err := os.WriteFile(fs.sessionLogEntriesPath(created.ID), raw, 0o644); err != nil {
		t.Fatalf("write bad sidecar: %v", err)
	}
	if _, err := fs.GetSessionHistoryWindow(context.Background(), SessionHistoryWindowRequest{SessionID: created.ID, Limit: 1}); err == nil {
		t.Fatal("expected sidecar mismatch to fail")
	}
}

func TestFileStoreGetSessionHistoryWindowTailDoesNotDecodeEarlierRows(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "tail-window")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	entries := make([]SnapshotLogEntry, 0, 5)
	for i := 0; i < 5; i++ {
		entries = append(entries, SnapshotLogEntry{Kind: "user", Message: fmt.Sprintf("entry-%d", i)})
	}
	if _, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		LogEntries:          entries,
	}); err != nil {
		t.Fatalf("save projection: %v", err)
	}
	raw, err := os.ReadFile(fs.sessionLogEntriesPath(created.ID))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	lines := bytes.Split(raw, []byte("\n"))
	if len(lines) < 7 {
		t.Fatalf("expected header plus entries, got %q", string(raw))
	}
	lines[1] = bytes.Repeat([]byte(" "), len(lines[1]))
	copy(lines[1], []byte("{bad json}"))
	var rebuilt bytes.Buffer
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		rebuilt.Write(line)
		rebuilt.WriteByte('\n')
	}
	if err := os.WriteFile(fs.sessionLogEntriesPath(created.ID), rebuilt.Bytes(), 0o644); err != nil {
		t.Fatalf("write corrupted earlier row: %v", err)
	}

	window, err := fs.GetSessionHistoryWindow(context.Background(), SessionHistoryWindowRequest{
		SessionID: created.ID,
		Limit:     2,
	})
	if err != nil {
		t.Fatalf("tail window should not decode earlier rows: %v", err)
	}
	if got := []string{window.LogEntries[0].Message, window.LogEntries[1].Message}; got[0] != "entry-3" || got[1] != "entry-4" {
		t.Fatalf("unexpected tail entries: %v", got)
	}
	if _, err := fs.GetSessionHistoryWindow(context.Background(), SessionHistoryWindowRequest{
		SessionID: created.ID,
		Before:    2,
		Limit:     2,
	}); err == nil {
		t.Fatal("expected page covering corrupted earlier row to fail")
	}
}

func TestFileStoreProjectionSideReadersAvoidLogEntrySidecarRows(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "side readers")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	entries := make([]SnapshotLogEntry, 0, 5)
	for i := 0; i < 5; i++ {
		entries = append(entries, SnapshotLogEntry{Kind: "user", Message: fmt.Sprintf("entry-%d", i)})
	}
	if _, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		Diffs: []DiffContext{
			{ContextID: "diff-1", Path: "a.go", Diff: "+a"},
			{ContextID: "diff-2", Path: "b.go", Diff: "+b"},
		},
		RawTerminalByStream: map[string]string{"stdout": "你好abc", "stderr": "err"},
		TerminalExecutions: []TerminalExecution{
			{ExecutionID: "exec-1", Command: "go test", Stdout: "hidden"},
			{ExecutionID: "exec-2", Command: "go test ./...", Stdout: "hidden-2"},
		},
		LogEntries:             entries,
		SessionContext:         SessionContext{EnabledSkillNames: []string{"review"}, Configured: true},
		SessionContextSet:      true,
		PermissionRulesEnabled: true,
		PermissionRules: []PermissionRule{{
			ID:      "rule-1",
			Scope:   PermissionScopeSession,
			Enabled: true,
			Kind:    PermissionKindShell,
		}},
	}); err != nil {
		t.Fatalf("save projection: %v", err)
	}
	raw, err := os.ReadFile(fs.sessionLogEntriesPath(created.ID))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	lines := bytes.Split(raw, []byte("\n"))
	if len(lines) < 7 {
		t.Fatalf("expected header plus entries, got %q", string(raw))
	}
	lines[1] = bytes.Repeat([]byte(" "), len(lines[1]))
	copy(lines[1], []byte("{bad json}"))
	var rebuilt bytes.Buffer
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		rebuilt.Write(line)
		rebuilt.WriteByte('\n')
	}
	if err := os.WriteFile(fs.sessionLogEntriesPath(created.ID), rebuilt.Bytes(), 0o644); err != nil {
		t.Fatalf("write corrupted earlier row: %v", err)
	}

	contextSnapshot, err := fs.GetSessionContext(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get session context: %v", err)
	}
	if got := contextSnapshot.SessionContext.EnabledSkillNames; len(got) != 1 || got[0] != "review" {
		t.Fatalf("unexpected context: %#v", contextSnapshot)
	}
	rules, err := fs.GetSessionPermissionRuleSnapshot(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get permission rules: %v", err)
	}
	if !rules.Enabled || len(rules.Items) != 1 || rules.Items[0].ID != "rule-1" {
		t.Fatalf("unexpected permission rules: %#v", rules)
	}
	diffPage, err := fs.GetSessionDiffPage(context.Background(), SessionDiffPageRequest{SessionID: created.ID, Before: 2, Limit: 1})
	if err != nil {
		t.Fatalf("get diff page: %v", err)
	}
	if diffPage.DiffStart != 1 || diffPage.DiffTotal != 2 || len(diffPage.Diffs) != 1 || diffPage.Diffs[0].ContextID != "diff-2" {
		t.Fatalf("unexpected diff page: %#v", diffPage)
	}
	terminalRange, err := fs.GetSessionTerminalRange(context.Background(), SessionTerminalRangeRequest{
		SessionID: created.ID,
		Stream:    "stdout",
		Start:     len("你"),
		Limit:     4,
	})
	if err != nil {
		t.Fatalf("get terminal range: %v", err)
	}
	if terminalRange.Start != len("你") || terminalRange.Content != "好a" {
		t.Fatalf("unexpected UTF-8 terminal range: %#v", terminalRange)
	}
	execPage, err := fs.GetSessionTerminalExecutionPage(context.Background(), SessionTerminalExecutionPageRequest{
		SessionID: created.ID,
		Before:    2,
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("get terminal execution page: %v", err)
	}
	if execPage.ExecutionStart != 1 || execPage.ExecutionTotal != 2 || len(execPage.TerminalExecutions) != 1 {
		t.Fatalf("unexpected execution page: %#v", execPage)
	}
	if execPage.TerminalExecutions[0].ExecutionID != "exec-2" || execPage.TerminalExecutions[0].Stdout != "" {
		t.Fatalf("expected output-stripped execution page, got %#v", execPage.TerminalExecutions)
	}
	execSnapshot, err := fs.GetSessionTerminalExecution(context.Background(), SessionTerminalExecutionRequest{
		SessionID:     created.ID,
		ExecutionID:   "exec-2",
		IncludeOutput: true,
	})
	if err != nil {
		t.Fatalf("get terminal execution: %v", err)
	}
	if execSnapshot.TerminalExecution.ExecutionID != "exec-2" || execSnapshot.TerminalExecution.Stdout != "hidden-2" {
		t.Fatalf("unexpected terminal execution: %#v", execSnapshot)
	}
	if _, err := fs.GetSessionTerminalExecution(context.Background(), SessionTerminalExecutionRequest{
		SessionID:     created.ID,
		ExecutionID:   "missing",
		IncludeOutput: true,
	}); err == nil {
		t.Fatal("expected missing execution error")
	}
	if _, err := fs.GetSessionHistoryWindow(context.Background(), SessionHistoryWindowRequest{
		SessionID: created.ID,
		Before:    2,
		Limit:     2,
	}); err == nil {
		t.Fatal("expected history page covering corrupted row to still fail")
	}
}

func TestFileStoreDeleteSessionRejectsMissingSession(t *testing.T) {
	baseDir := t.TempDir()
	fs, err := NewFileStore(baseDir)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}

	created, err := fs.CreateSession(context.Background(), "delete-me")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := fs.DeleteSession(context.Background(), created.ID); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if err := fs.DeleteSession(context.Background(), created.ID); err == nil {
		t.Fatal("expected repeated delete to fail")
	}
}

func TestFileStoreSavePushTokenUsesOwnerOnlyPermissions(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}

	if err := fs.SavePushToken(context.Background(), "session-1", "tok", "ios"); err != nil {
		t.Fatalf("SavePushToken failed: %v", err)
	}

	info, err := os.Stat(fs.pushTokensPath)
	if err != nil {
		t.Fatalf("stat push tokens: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("push token file mode: got %v, want 0600", got)
	}
}

func TestFileStorePersistsSessionContext(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "ctx")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	_, err = fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		SessionContext: SessionContext{
			EnabledSkillNames: []string{"review", "analyze"},
			EnabledMemoryIDs:  []string{"m1", "m2"},
		},
		SkillCatalogMeta: CatalogMetadata{
			SourceOfTruth: CatalogSourceTruthClaude,
			SyncState:     CatalogSyncStateSynced,
		},
	})
	if err != nil {
		t.Fatalf("save projection: %v", err)
	}
	record, err := fs.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if len(record.Projection.SessionContext.EnabledSkillNames) != 2 {
		t.Fatalf("unexpected enabled skills length: %#v", record.Projection.SessionContext)
	}
	seenSkills := map[string]bool{}
	for _, item := range record.Projection.SessionContext.EnabledSkillNames {
		seenSkills[item] = true
	}
	if !seenSkills["review"] || !seenSkills["analyze"] {
		t.Fatalf("unexpected enabled skills: %#v", record.Projection.SessionContext)
	}
	if len(record.Projection.SessionContext.EnabledMemoryIDs) != 2 || record.Projection.SessionContext.EnabledMemoryIDs[1] != "m2" {
		t.Fatalf("unexpected enabled memories: %#v", record.Projection.SessionContext)
	}
	if !record.Projection.SessionContext.Configured {
		t.Fatalf("expected configured session context, got %#v", record.Projection.SessionContext)
	}
	if record.Projection.SkillCatalogMeta.SyncState != CatalogSyncStateSynced {
		t.Fatalf("expected skill catalog meta persisted, got %#v", record.Projection.SkillCatalogMeta)
	}
}

func TestFileStoreGetSessionContextMigratesMissingSidecar(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "legacy ctx")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	_, err = fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		SessionContext: SessionContext{
			EnabledSkillNames: []string{"review"},
			EnabledMemoryIDs:  []string{"memory-1"},
			Configured:        true,
		},
		SessionContextSet: true,
	})
	if err != nil {
		t.Fatalf("save projection: %v", err)
	}
	if err := os.Remove(fs.sessionContextPath(created.ID)); err != nil {
		t.Fatalf("remove context sidecar: %v", err)
	}

	snapshot, err := fs.GetSessionContext(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get missing context sidecar: %v", err)
	}
	if snapshot.SessionID != created.ID {
		t.Fatalf("unexpected snapshot session id: %#v", snapshot)
	}
	if got := snapshot.SessionContext.EnabledSkillNames; len(got) != 1 || got[0] != "review" {
		t.Fatalf("unexpected skills from legacy context: %#v", snapshot.SessionContext)
	}
	if got := snapshot.SessionContext.EnabledMemoryIDs; len(got) != 1 || got[0] != "memory-1" {
		t.Fatalf("unexpected memories from legacy context: %#v", snapshot.SessionContext)
	}
	if _, err := os.Stat(fs.sessionContextPath(created.ID)); err != nil {
		t.Fatalf("missing context sidecar was not migrated: %v", err)
	}
}

func TestFileStoreGetSessionContextFailsForCorruptSidecar(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "corrupt ctx")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := os.WriteFile(fs.sessionContextPath(created.ID), []byte("{bad json}"), 0o644); err != nil {
		t.Fatalf("write corrupt context sidecar: %v", err)
	}

	if _, err := fs.GetSessionContext(context.Background(), created.ID); err == nil {
		t.Fatal("expected corrupt context sidecar to fail")
	}
}

func TestFileStoreGetSessionHistoryWindowMigratesMissingContextSidecar(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "window context")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		LogEntries: []SnapshotLogEntry{{Kind: "user", Message: "hello"}},
		SessionContext: SessionContext{
			EnabledSkillNames: []string{"review"},
			Configured:        true,
		},
		SessionContextSet: true,
	}); err != nil {
		t.Fatalf("save projection: %v", err)
	}
	if err := os.Remove(fs.sessionContextPath(created.ID)); err != nil {
		t.Fatalf("remove context sidecar: %v", err)
	}

	window, err := fs.GetSessionHistoryWindow(context.Background(), SessionHistoryWindowRequest{
		SessionID: created.ID,
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("history window should migrate missing context sidecar: %v", err)
	}
	if window.LogEntryTotal != 1 || len(window.LogEntries) != 1 {
		t.Fatalf("unexpected history window: %#v", window)
	}
	snapshot, err := fs.GetSessionContext(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get migrated context: %v", err)
	}
	if got := snapshot.SessionContext.EnabledSkillNames; len(got) != 1 || got[0] != "review" {
		t.Fatalf("unexpected migrated context: %#v", snapshot.SessionContext)
	}
}

func TestFileStoreGetSessionHistoryWindowFailsForCorruptContextSidecar(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "window corrupt context")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		LogEntries: []SnapshotLogEntry{{Kind: "user", Message: "hello"}},
	}); err != nil {
		t.Fatalf("save projection: %v", err)
	}
	if err := os.WriteFile(fs.sessionContextPath(created.ID), []byte("{bad json}"), 0o644); err != nil {
		t.Fatalf("write corrupt context sidecar: %v", err)
	}

	if _, err := fs.GetSessionHistoryWindow(context.Background(), SessionHistoryWindowRequest{
		SessionID: created.ID,
		Limit:     1,
	}); err == nil {
		t.Fatal("expected corrupt context sidecar to fail history window")
	}
}

func TestFileStoreMarkClientActionPersistsDuplicateMetadata(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "dedupe")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	duplicate, err := fs.MarkClientAction(context.Background(), created.ID, ClientActionRecord{
		ClientActionID: " action-1 ",
		Action:         " input ",
	}, time.Hour, 10)
	if err != nil {
		t.Fatalf("mark client action: %v", err)
	}
	if duplicate {
		t.Fatal("first client action should not be duplicate")
	}

	duplicate, err = fs.MarkClientAction(context.Background(), created.ID, ClientActionRecord{
		ClientActionID: "action-1",
		Action:         "input",
	}, time.Hour, 10)
	if err != nil {
		t.Fatalf("mark duplicate client action: %v", err)
	}
	if !duplicate {
		t.Fatal("second client action should be duplicate")
	}

	record, err := fs.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if len(record.ClientActions) != 1 {
		t.Fatalf("expected one client action record, got %#v", record.ClientActions)
	}
	got := record.ClientActions[0]
	if got.ClientActionID != "action-1" || got.Action != "input" || got.Status != "accepted" {
		t.Fatalf("unexpected client action record: %#v", got)
	}
	if got.AckedAt.IsZero() {
		t.Fatal("expected ack timestamp to be stored")
	}
}

func TestFileStoreMarkClientActionAppliesTTLAndLimit(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "dedupe")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	stale := time.Now().UTC().Add(-2 * time.Hour)
	record, err := fs.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	record.ClientActions = []ClientActionRecord{
		{ClientActionID: "old", Action: "input", Status: "accepted", AckedAt: stale},
	}
	if _, err := fs.UpsertSession(context.Background(), record); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	for _, id := range []string{"new-1", "new-2", "new-3"} {
		if _, err := fs.MarkClientAction(context.Background(), created.ID, ClientActionRecord{
			ClientActionID: id,
			Action:         "input",
		}, time.Hour, 2); err != nil {
			t.Fatalf("mark client action %s: %v", id, err)
		}
	}

	record, err = fs.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get updated session: %v", err)
	}
	ids := make([]string, 0, len(record.ClientActions))
	for _, item := range record.ClientActions {
		ids = append(ids, item.ClientActionID)
	}
	want := []string{"new-2", "new-3"}
	if len(ids) != len(want) {
		t.Fatalf("expected ids %v, got %v", want, ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("expected ids %v, got %v", want, ids)
		}
	}
}

func TestFileStoreMarkClientActionDoesNotDecodeHistoryRows(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "dedupe without history decode")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	entries := make([]SnapshotLogEntry, 0, 5)
	for i := 0; i < 5; i++ {
		entries = append(entries, SnapshotLogEntry{Kind: "user", Message: fmt.Sprintf("entry-%d", i)})
	}
	if _, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		LogEntries:          entries,
	}); err != nil {
		t.Fatalf("save projection: %v", err)
	}
	raw, err := os.ReadFile(fs.sessionLogEntriesPath(created.ID))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	lines := bytes.Split(raw, []byte("\n"))
	if len(lines) < 7 {
		t.Fatalf("expected header plus entries, got %q", string(raw))
	}
	lines[1] = bytes.Repeat([]byte(" "), len(lines[1]))
	copy(lines[1], []byte("{bad json}"))
	var rebuilt bytes.Buffer
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		rebuilt.Write(line)
		rebuilt.WriteByte('\n')
	}
	if err := os.WriteFile(fs.sessionLogEntriesPath(created.ID), rebuilt.Bytes(), 0o644); err != nil {
		t.Fatalf("write corrupted earlier row: %v", err)
	}

	duplicate, err := fs.MarkClientAction(context.Background(), created.ID, ClientActionRecord{
		ClientActionID: "action-1",
		Action:         "input",
	}, time.Hour, 10)
	if err != nil {
		t.Fatalf("mark client action should not decode history rows: %v", err)
	}
	if duplicate {
		t.Fatal("first client action should not be duplicate")
	}
	duplicate, err = fs.MarkClientAction(context.Background(), created.ID, ClientActionRecord{
		ClientActionID: "action-1",
		Action:         "input",
	}, time.Hour, 10)
	if err != nil {
		t.Fatalf("mark duplicate should not decode history rows: %v", err)
	}
	if !duplicate {
		t.Fatal("second client action should be duplicate")
	}
	if _, err := fs.GetSessionHistoryWindow(context.Background(), SessionHistoryWindowRequest{
		SessionID: created.ID,
		Before:    2,
		Limit:     2,
	}); err == nil {
		t.Fatal("expected history page covering corrupted row to still fail")
	}
}

func TestFileStoreSaveProjectionPersistsExternalCodexSessionState(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	createdAt := mustTime("2026-04-04T02:00:00Z")
	record := SessionRecord{
		Summary: SessionSummary{
			ID:        "codex-thread:thread-1",
			Title:     "Codex 会话",
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
			Runtime: SessionRuntime{
				ResumeSessionID: "thread-1",
				Command:         "codex",
				Engine:          "codex",
				CWD:             "/tmp/project",
				ClaudeLifecycle: "resumable",
				Source:          "codex-native",
			},
			Source:   "codex-native",
			External: true,
		},
		Projection: ProjectionSnapshot{
			RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
			Runtime: SessionRuntime{
				ResumeSessionID: "thread-1",
				Command:         "codex",
				Engine:          "codex",
				CWD:             "/tmp/project",
				ClaudeLifecycle: "resumable",
				Source:          "codex-native",
			},
			Controller: ControllerSnapshot{
				SessionID:      "codex-thread:thread-1",
				ResumeSession:  "thread-1",
				CurrentCommand: "codex",
			},
		},
	}
	if _, err := fs.UpsertSession(context.Background(), record); err != nil {
		t.Fatalf("upsert external session: %v", err)
	}

	summary, err := fs.SaveProjection(context.Background(), record.Summary.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "assistant output", "stderr": ""},
		LogEntries: []SnapshotLogEntry{
			{Kind: "user", Message: "继续这个会话", Timestamp: "2026-04-04T02:01:00Z"},
			{Kind: "markdown", Message: "等待你的确认", Timestamp: "2026-04-04T02:01:01Z"},
		},
		Runtime: SessionRuntime{
			ResumeSessionID: "thread-1",
			Command:         "codex resume thread-1",
			Engine:          "codex",
			CWD:             "/tmp/project",
			PermissionMode:  "default",
			ClaudeLifecycle: "waiting_input",
		},
		Controller: ControllerSnapshot{
			SessionID:       "codex-thread:thread-1",
			State:           ControllerStateWaitInput,
			CurrentCommand:  "codex resume thread-1",
			ResumeSession:   "thread-1",
			ClaudeLifecycle: "waiting_input",
			ActiveMeta: protocol.RuntimeMeta{
				ResumeSessionID: "thread-1",
				Command:         "codex resume thread-1",
				Engine:          "codex",
				CWD:             "/tmp/project",
				PermissionMode:  "default",
				ClaudeLifecycle: "waiting_input",
			},
		},
	})
	if err != nil {
		t.Fatalf("save external projection: %v", err)
	}

	if summary.EntryCount != 2 {
		t.Fatalf("expected external entry count to update, got %#v", summary)
	}
	if summary.Runtime.ClaudeLifecycle != "waiting_input" {
		t.Fatalf("expected external runtime lifecycle to persist, got %#v", summary.Runtime)
	}
	record, err = fs.GetSession(context.Background(), record.Summary.ID)
	if err != nil {
		t.Fatalf("get external session: %v", err)
	}
	if len(record.Projection.LogEntries) != 2 {
		t.Fatalf("expected external log entries persisted, got %#v", record.Projection.LogEntries)
	}
	if record.Projection.Controller.State != ControllerStateWaitInput {
		t.Fatalf("expected external controller state persisted, got %#v", record.Projection.Controller)
	}
	if record.Projection.Runtime.Command != "codex resume thread-1" {
		t.Fatalf("expected external runtime command persisted, got %#v", record.Projection.Runtime)
	}
	if record.Projection.RawTerminalByStream["stdout"] != "assistant output" {
		t.Fatalf("expected external raw terminal output persisted, got %#v", record.Projection.RawTerminalByStream)
	}
}

func TestFileStoreSaveProjectionDerivesTitleAndPreviewFromMeaningfulUserInput(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	summary, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		LogEntries: []SnapshotLogEntry{
			{Kind: "user", Message: "codex -m gpt-5-codex --config model_reasoning_effort=high"},
			{Kind: "system", Message: "command started"},
			{Kind: "user", Message: "帮我查看这个项目的会话回复逻辑"},
			{Kind: "markdown", Message: "我先看下项目结构。"},
			{Kind: "user", Message: "再看下恢复逻辑"},
		},
	})
	if err != nil {
		t.Fatalf("save projection: %v", err)
	}

	if summary.Title != "帮我查看这个项目的会话回复逻辑" {
		t.Fatalf("expected derived title, got %q", summary.Title)
	}
	if summary.LastPreview != "再看下恢复逻辑" {
		t.Fatalf("expected latest user preview, got %q", summary.LastPreview)
	}
}

func TestFileStoreSaveProjectionWithoutLogEntriesPreservesExistingHistory(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "preserve history")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	entries := []SnapshotLogEntry{
		{Kind: "user", Message: "a"},
		{Kind: "markdown", Message: "b"},
		{Kind: "user", Message: "c"},
	}
	if _, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		LogEntries:          entries,
		Runtime:             SessionRuntime{Command: "claude", CWD: "/tmp/project"},
	}); err != nil {
		t.Fatalf("save projection with history: %v", err)
	}
	summary, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "new output", "stderr": ""},
		Runtime:             SessionRuntime{Command: "claude", CWD: "/tmp/project", PermissionMode: "auto"},
	})
	if err != nil {
		t.Fatalf("save lightweight projection: %v", err)
	}
	if summary.EntryCount != len(entries) {
		t.Fatalf("expected entry count preserved, got %#v", summary)
	}
	window, err := fs.GetSessionHistoryWindow(context.Background(), SessionHistoryWindowRequest{
		SessionID: created.ID,
		Limit:     2,
	})
	if err != nil {
		t.Fatalf("get history window: %v", err)
	}
	if window.LogEntryTotal != len(entries) || len(window.LogEntries) != 2 || window.LogEntries[0].Message != "b" {
		t.Fatalf("expected existing history preserved in sidecar, got %#v", window)
	}
	record, err := fs.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if len(record.Projection.LogEntries) != len(entries) {
		t.Fatalf("expected hydrated history preserved, got %#v", record.Projection.LogEntries)
	}
	if record.Projection.Runtime.PermissionMode != "auto" {
		t.Fatalf("expected runtime update persisted, got %#v", record.Projection.Runtime)
	}
}

func TestFileStoreSaveLightweightProjectionDoesNotDecodeExistingHistoryRows(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "preserve history without decode")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	entries := make([]SnapshotLogEntry, 0, 5)
	for i := 0; i < 5; i++ {
		entries = append(entries, SnapshotLogEntry{Kind: "user", Message: fmt.Sprintf("entry-%d", i)})
	}
	if _, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		LogEntries:          entries,
	}); err != nil {
		t.Fatalf("save projection with history: %v", err)
	}
	raw, err := os.ReadFile(fs.sessionLogEntriesPath(created.ID))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	lines := bytes.Split(raw, []byte("\n"))
	if len(lines) < 7 {
		t.Fatalf("expected header plus entries, got %q", string(raw))
	}
	lines[1] = bytes.Repeat([]byte(" "), len(lines[1]))
	copy(lines[1], []byte("{bad json}"))
	var rebuilt bytes.Buffer
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		rebuilt.Write(line)
		rebuilt.WriteByte('\n')
	}
	if err := os.WriteFile(fs.sessionLogEntriesPath(created.ID), rebuilt.Bytes(), 0o644); err != nil {
		t.Fatalf("write corrupted earlier row: %v", err)
	}

	coldStore, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("new cold file store: %v", err)
	}
	summary, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "runtime-only update", "stderr": ""},
		Runtime:             SessionRuntime{Command: "codex", CWD: "/tmp/project"},
	})
	if err != nil {
		t.Fatalf("warm save lightweight projection should not decode history rows: %v", err)
	}
	if summary.EntryCount != len(entries) {
		t.Fatalf("expected entry count preserved, got %#v", summary)
	}
	summary, err = coldStore.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "cold runtime-only update", "stderr": ""},
		Runtime:             SessionRuntime{Command: "codex", CWD: "/tmp/project", PermissionMode: "auto"},
	})
	if err != nil {
		t.Fatalf("cold save lightweight projection should not decode history rows: %v", err)
	}
	if summary.EntryCount != len(entries) {
		t.Fatalf("expected entry count preserved, got %#v", summary)
	}
	window, err := coldStore.GetSessionHistoryWindow(context.Background(), SessionHistoryWindowRequest{
		SessionID: created.ID,
		Limit:     2,
	})
	if err != nil {
		t.Fatalf("tail window should still avoid corrupted earlier row: %v", err)
	}
	if got := []string{window.LogEntries[0].Message, window.LogEntries[1].Message}; got[0] != "entry-3" || got[1] != "entry-4" {
		t.Fatalf("unexpected tail entries: %v", got)
	}
	if _, err := coldStore.GetSessionHistoryWindow(context.Background(), SessionHistoryWindowRequest{
		SessionID: created.ID,
		Before:    2,
		Limit:     2,
	}); err == nil {
		t.Fatal("expected page covering corrupted earlier row to fail")
	}
}

func TestFileStoreAppendSessionLogEntriesUpdatesHeaderCount(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "append header")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		LogEntries:          []SnapshotLogEntry{{Kind: "user", Message: "initial"}},
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
	}); err != nil {
		t.Fatalf("save projection: %v", err)
	}
	summary, err := fs.AppendSessionLogEntries(context.Background(), created.ID, []SnapshotLogEntry{{Kind: "markdown", Message: "appended"}})
	if err != nil {
		t.Fatalf("append log entries: %v", err)
	}
	if summary.EntryCount != 2 {
		t.Fatalf("expected appended summary count 2, got %#v", summary)
	}
	window, err := fs.GetSessionHistoryWindow(context.Background(), SessionHistoryWindowRequest{
		SessionID: created.ID,
		Limit:     2,
	})
	if err != nil {
		t.Fatalf("get appended history window: %v", err)
	}
	if window.LogEntryTotal != 2 || len(window.LogEntries) != 2 || window.LogEntries[1].Message != "appended" {
		t.Fatalf("unexpected appended history window: %#v", window)
	}
}

func TestFileStoreSaveSessionPermissionRulesDoesNotDecodeExistingHistoryRows(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "permission sidecar update")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	entries := make([]SnapshotLogEntry, 0, 5)
	for i := 0; i < 5; i++ {
		entries = append(entries, SnapshotLogEntry{Kind: "user", Message: fmt.Sprintf("entry-%d", i)})
	}
	if _, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		LogEntries:             entries,
		RawTerminalByStream:    map[string]string{"stdout": "existing stdout", "stderr": ""},
		Runtime:                SessionRuntime{Command: "codex", CWD: "/tmp/project"},
		SessionContext:         SessionContext{EnabledMemoryIDs: []string{"memory-1"}, Configured: true},
		SessionContextSet:      true,
		Diffs:                  []DiffContext{{ContextID: "diff-1", Path: "a.go", Diff: "+a"}},
		TerminalExecutions:     []TerminalExecution{{ExecutionID: "exec-1", Command: "go test", Stdout: "ok"}},
		PermissionRulesEnabled: true,
		PermissionRules: []PermissionRule{{
			ID:      "rule-1",
			Scope:   PermissionScopeSession,
			Enabled: true,
		}},
	}); err != nil {
		t.Fatalf("save projection with history: %v", err)
	}
	corruptLogEntrySidecarRow(t, fs, created.ID, 1)
	coldStore, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("new cold file store: %v", err)
	}
	summary, err := coldStore.SaveSessionPermissionRuleSnapshot(context.Background(), SessionPermissionRuleSnapshot{
		SessionID: created.ID,
		Enabled:   true,
		Items: []PermissionRule{{
			ID:         "rule-1",
			Scope:      PermissionScopeSession,
			Enabled:    true,
			MatchCount: 1,
		}},
	})
	if err != nil {
		t.Fatalf("save permission sidecar should not decode history rows: %v", err)
	}
	if summary.EntryCount != len(entries) {
		t.Fatalf("expected entry count preserved, got %#v", summary)
	}
	rules, err := coldStore.GetSessionPermissionRuleSnapshot(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get permission sidecar: %v", err)
	}
	runtimeMeta, err := coldStore.GetSessionRuntimeMetadata(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get runtime metadata: %v", err)
	}
	contextSnapshot, err := coldStore.GetSessionContext(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get session context: %v", err)
	}
	diffPage, err := coldStore.GetSessionDiffPage(context.Background(), SessionDiffPageRequest{
		SessionID: created.ID,
		Before:    1,
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("get diff page: %v", err)
	}
	terminalRange, err := coldStore.GetSessionTerminalRange(context.Background(), SessionTerminalRangeRequest{
		SessionID: created.ID,
		Stream:    "stdout",
		Limit:     len("existing stdout"),
	})
	if err != nil {
		t.Fatalf("get terminal range: %v", err)
	}
	execPage, err := coldStore.GetSessionTerminalExecutionPage(context.Background(), SessionTerminalExecutionPageRequest{
		SessionID: created.ID,
		Before:    1,
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("get terminal execution page: %v", err)
	}
	if len(rules.Items) != 1 || rules.Items[0].MatchCount != 1 ||
		runtimeMeta.Record.Projection.Runtime.CWD != "/tmp/project" ||
		!contextSnapshot.SessionContext.Configured ||
		len(diffPage.Diffs) != 1 ||
		terminalRange.Content != "existing stdout" ||
		len(execPage.TerminalExecutions) != 1 {
		t.Fatalf("permission sidecar update should preserve other domains, rules=%#v runtime=%#v context=%#v diff=%#v terminal=%#v exec=%#v", rules, runtimeMeta, contextSnapshot, diffPage, terminalRange, execPage)
	}
}

func TestFileStoreSaveSessionContextDoesNotDecodeExistingHistoryRows(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "context sidecar update")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	entries := make([]SnapshotLogEntry, 0, 5)
	for i := 0; i < 5; i++ {
		entries = append(entries, SnapshotLogEntry{Kind: "user", Message: fmt.Sprintf("entry-%d", i)})
	}
	if _, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		LogEntries:          entries,
		RawTerminalByStream: map[string]string{"stdout": "existing stdout", "stderr": ""},
		Runtime:             SessionRuntime{Command: "codex", CWD: "/tmp/project"},
		SessionContext:      SessionContext{EnabledMemoryIDs: []string{"old-memory"}, Configured: true},
		SessionContextSet:   true,
		Diffs:               []DiffContext{{ContextID: "diff-1", Path: "a.go", Diff: "+a"}},
	}); err != nil {
		t.Fatalf("save projection with history: %v", err)
	}
	corruptLogEntrySidecarRow(t, fs, created.ID, 1)
	coldStore, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("new cold file store: %v", err)
	}
	summary, err := coldStore.SaveSessionContext(context.Background(), SessionContextSnapshot{
		SessionID: created.ID,
		SessionContext: SessionContext{
			EnabledSkillNames: []string{"review"},
			EnabledMemoryIDs:  []string{"memory-1"},
			Configured:        true,
		},
	})
	if err != nil {
		t.Fatalf("save context sidecar should not decode history rows: %v", err)
	}
	if summary.EntryCount != len(entries) {
		t.Fatalf("expected entry count preserved, got %#v", summary)
	}
	contextSnapshot, err := coldStore.GetSessionContext(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get session context: %v", err)
	}
	if got := contextSnapshot.SessionContext.EnabledSkillNames; len(got) != 1 || got[0] != "review" {
		t.Fatalf("unexpected context skills: %#v", contextSnapshot.SessionContext)
	}
	runtimeMeta, err := coldStore.GetSessionRuntimeMetadata(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get runtime metadata: %v", err)
	}
	diffPage, err := coldStore.GetSessionDiffPage(context.Background(), SessionDiffPageRequest{
		SessionID: created.ID,
		Before:    1,
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("get diff page: %v", err)
	}
	if runtimeMeta.Record.Projection.Runtime.CWD != "/tmp/project" || len(diffPage.Diffs) != 1 {
		t.Fatalf("context sidecar update should preserve other domains, runtime=%#v diff=%#v", runtimeMeta, diffPage)
	}
}

func TestFileStoreRebuildsMissingProjectionSidecarsFromLegacyRecord(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	now := mustTime("2026-06-07T07:48:16Z")
	record := SessionRecord{
		Summary: SessionSummary{
			ID:         "session-legacy-sidecars",
			Title:      "legacy sidecars",
			CreatedAt:  now,
			UpdatedAt:  now,
			EntryCount: 2,
			Runtime: SessionRuntime{
				Command: "claude --session-id legacy",
				Engine:  "claude",
				CWD:     "/tmp/project",
				Source:  "mobilevc",
			},
			Source:    "mobilevc",
			Ownership: "mobilevc",
		},
		Projection: ProjectionSnapshot{
			Runtime: SessionRuntime{
				Command: "claude --session-id legacy",
				Engine:  "claude",
				CWD:     "/tmp/project",
				Source:  "mobilevc",
			},
			Controller: ControllerSnapshot{
				SessionID:      "session-legacy-sidecars",
				State:          ControllerStateWaitInput,
				CurrentCommand: "claude --session-id legacy",
			},
			SessionContext: SessionContext{
				EnabledSkillNames: []string{"review"},
				Configured:        true,
			},
			SessionContextSet:      true,
			PermissionRulesEnabled: true,
			PermissionRules: []PermissionRule{{
				ID:          "rule-1",
				Kind:        PermissionKindShell,
				CommandHead: "go",
				Enabled:     true,
			}},
			Diffs:               []DiffContext{{ContextID: "diff-1", Path: "main.go", Diff: "+ok"}},
			RawTerminalByStream: map[string]string{"stdout": "legacy stdout", "stderr": ""},
			TerminalExecutions:  []TerminalExecution{{ExecutionID: "exec-1", Command: "go test", Stdout: "ok"}},
			LogEntries: []SnapshotLogEntry{
				{Kind: "user", Message: "one", Timestamp: now.Format(time.RFC3339)},
				{Kind: "markdown", Message: "two", Timestamp: now.Add(time.Second).Format(time.RFC3339)},
			},
		},
	}
	sidecars := sidecarsFromRecord(record)
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal legacy record: %v", err)
	}
	if err := os.WriteFile(fs.sessionPath(record.Summary.ID), data, 0o644); err != nil {
		t.Fatalf("write legacy record: %v", err)
	}
	if err := fs.writeSessionLogEntriesLocked(record.Summary.ID, record.Projection.LogEntries); err != nil {
		t.Fatalf("write log sidecar: %v", err)
	}
	if err := fs.writeJSONFileLocked(fs.sessionRuntimeMetaPath(record.Summary.ID), sidecars.RuntimeMeta, "encode session runtime metadata sidecar"); err != nil {
		t.Fatalf("write runtime sidecar: %v", err)
	}
	indexData, err := json.Marshal(fileIndex{Sessions: []SessionSummary{record.Summary}})
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	if err := os.WriteFile(fs.indexPath, indexData, 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	for _, path := range []string{
		fs.sessionContextPath(record.Summary.ID),
		fs.sessionPermissionPath(record.Summary.ID),
		fs.sessionDiffsPath(record.Summary.ID),
		fs.sessionTerminalPath(record.Summary.ID),
		fs.sessionTerminalExecutionsPath(record.Summary.ID),
	} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("remove projection sidecar %s: %v", path, err)
		}
	}

	runtimeMeta, err := fs.GetSessionRuntimeMetadata(context.Background(), record.Summary.ID)
	if err != nil {
		t.Fatalf("get rebuilt runtime metadata: %v", err)
	}
	if runtimeMeta.Record.Projection.Runtime.CWD != "/tmp/project" || runtimeMeta.Latest.LogEntryCount != 2 {
		t.Fatalf("unexpected rebuilt runtime metadata: %#v", runtimeMeta)
	}
	permission, err := fs.GetSessionPermissionRuleSnapshot(context.Background(), record.Summary.ID)
	if err != nil {
		t.Fatalf("get rebuilt permission rules: %v", err)
	}
	if !permission.Enabled || len(permission.Items) != 1 {
		t.Fatalf("unexpected rebuilt permission rules: %#v", permission)
	}
	diffPage, err := fs.GetSessionDiffPage(context.Background(), SessionDiffPageRequest{
		SessionID: record.Summary.ID,
		Before:    1,
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("get rebuilt diff page: %v", err)
	}
	terminalRange, err := fs.GetSessionTerminalRange(context.Background(), SessionTerminalRangeRequest{
		SessionID: record.Summary.ID,
		Stream:    "stdout",
		Limit:     len("legacy stdout"),
	})
	if err != nil {
		t.Fatalf("get rebuilt terminal range: %v", err)
	}
	execPage, err := fs.GetSessionTerminalExecutionPage(context.Background(), SessionTerminalExecutionPageRequest{
		SessionID:     record.Summary.ID,
		Before:        1,
		Limit:         1,
		IncludeOutput: true,
	})
	if err != nil {
		t.Fatalf("get rebuilt terminal execution page: %v", err)
	}
	if len(diffPage.Diffs) != 1 || terminalRange.Content != "legacy stdout" || len(execPage.TerminalExecutions) != 1 {
		t.Fatalf("unexpected rebuilt side domains: diff=%#v terminal=%#v exec=%#v", diffPage, terminalRange, execPage)
	}
}

func TestFileStoreUpsertSessionFailsWhenExistingRecordIsCorrupt(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "corrupt existing")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := os.WriteFile(fs.sessionPath(created.ID), []byte("{bad json"), 0o644); err != nil {
		t.Fatalf("corrupt session record: %v", err)
	}
	_, err = fs.UpsertSession(context.Background(), SessionRecord{
		Summary: SessionSummary{
			ID:        created.ID,
			Title:     "replacement",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
		Projection: ProjectionSnapshot{
			LogEntries: []SnapshotLogEntry{{Kind: "user", Message: "replacement"}},
		},
	})
	if err == nil {
		t.Fatal("expected corrupt existing record to fail")
	}
	if !strings.Contains(err.Error(), "decode session record") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestFileStoreSaveProjectionCollapsesExactAdjacentDuplicateLogEntries(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "dedupe logs")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	summary, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		LogEntries: []SnapshotLogEntry{
			{Kind: "user", Message: "a", Timestamp: "2026-05-27T08:01:59Z"},
			{Kind: "user", Message: "a", Label: "回复", Timestamp: "2026-05-27T08:01:59Z"},
			{Kind: "markdown", Message: "同一回复", Timestamp: "2026-05-27T08:02:16Z"},
			{
				Kind:        "markdown",
				Message:     "同一回复",
				Timestamp:   "2026-05-27T08:02:16Z",
				Stream:      "stdout",
				ExecutionID: "exec-1",
				Phase:       "stdout",
				Context: &SnapshotContext{
					ID:          "hook-1",
					Title:       "同一回复",
					Timestamp:   "2026-05-27T08:02:16Z",
					ExecutionID: "exec-1",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("save projection: %v", err)
	}
	if summary.EntryCount != 2 {
		t.Fatalf("expected deduped entry count, got %#v", summary)
	}
	record, err := fs.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got := len(record.Projection.LogEntries); got != 2 {
		t.Fatalf("expected two deduped entries, got %d: %#v", got, record.Projection.LogEntries)
	}
	if record.Projection.LogEntries[0].Kind != "user" || record.Projection.LogEntries[1].Kind != "markdown" {
		t.Fatalf("unexpected deduped entries: %#v", record.Projection.LogEntries)
	}
}

func TestFileStoreSaveProjectionKeepsLegitimateRepeatedLogEntries(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "keep repeats")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if _, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		LogEntries: []SnapshotLogEntry{
			{Kind: "user", Message: "a", Timestamp: "2026-05-27T08:01:59Z"},
			{Kind: "user", Message: "a", Timestamp: "2026-05-27T08:02:00Z"},
			{Kind: "markdown", Message: "分隔回复", Timestamp: "2026-05-27T08:02:01Z"},
			{Kind: "user", Message: "a", Timestamp: "2026-05-27T08:01:59Z"},
		},
	}); err != nil {
		t.Fatalf("save projection: %v", err)
	}
	record, err := fs.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got := len(record.Projection.LogEntries); got != 4 {
		t.Fatalf("expected repeated but distinct entries to remain, got %d: %#v", got, record.Projection.LogEntries)
	}
}

func TestFileStoreGetSessionNormalizesLegacyAdjacentDuplicateLogEntries(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "legacy dupes")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	legacy := SessionRecord{
		Summary: SessionSummary{
			ID:        created.ID,
			Title:     "legacy dupes",
			CreatedAt: created.CreatedAt,
			UpdatedAt: created.UpdatedAt,
			Runtime:   SessionRuntime{Source: "mobilevc"},
		},
		Projection: ProjectionSnapshot{
			RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
			LogEntries: []SnapshotLogEntry{
				{Kind: "markdown", Message: "重复回复", Timestamp: "2026-05-27T08:02:16Z"},
				{Kind: "markdown", Message: "重复回复", Timestamp: "2026-05-27T08:02:16Z"},
			},
			Runtime: SessionRuntime{Source: "mobilevc"},
		},
	}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatalf("marshal legacy record: %v", err)
	}
	if err := os.WriteFile(fs.sessionPath(created.ID), data, 0o644); err != nil {
		t.Fatalf("write legacy record: %v", err)
	}

	record, err := fs.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got := len(record.Projection.LogEntries); got != 1 {
		t.Fatalf("expected legacy adjacent duplicate to collapse, got %d: %#v", got, record.Projection.LogEntries)
	}
}

func TestFileStoreListSessionsRepairsLegacySummaryFromProjection(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	staleRecord := SessionRecord{
		Summary: SessionSummary{
			ID:        created.ID,
			Title:     "2026-04-01 20:15",
			CreatedAt: created.CreatedAt,
			UpdatedAt: created.UpdatedAt,
			Runtime:   SessionRuntime{Source: "mobilevc"},
		},
		Projection: ProjectionSnapshot{
			RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
			LogEntries: []SnapshotLogEntry{
				{Kind: "user", Message: "看下这个项目的会话恢复逻辑"},
				{Kind: "user", Message: "顺便检查一下 resume"},
			},
			Runtime: SessionRuntime{Source: "mobilevc"},
		},
	}
	data, err := json.MarshalIndent(staleRecord, "", "  ")
	if err != nil {
		t.Fatalf("marshal stale record: %v", err)
	}
	if err := os.WriteFile(fs.sessionPath(created.ID), data, 0o644); err != nil {
		t.Fatalf("write stale record: %v", err)
	}
	indexData, err := json.MarshalIndent(fileIndex{Sessions: []SessionSummary{staleRecord.Summary}}, "", "  ")
	if err != nil {
		t.Fatalf("marshal stale index: %v", err)
	}
	if err := os.WriteFile(fs.indexPath, indexData, 0o644); err != nil {
		t.Fatalf("write stale index: %v", err)
	}

	items, err := fs.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one session, got %#v", items)
	}
	if items[0].Title != "看下这个项目的会话恢复逻辑" {
		t.Fatalf("expected repaired title, got %q", items[0].Title)
	}
	if items[0].LastPreview != "顺便检查一下 resume" {
		t.Fatalf("expected repaired preview, got %q", items[0].LastPreview)
	}
}

func TestFileStoreReadsLegacySkillCatalogArray(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	legacy := `[
	  {
	    "name": "legacy-review",
	    "description": "legacy",
	    "prompt": "review it",
	    "resultView": "review-card",
	    "targetType": "diff"
	  }
	]`
	if err := os.WriteFile(fs.skillCatalogPath, []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy skill catalog: %v", err)
	}
	snapshot, err := fs.GetSkillCatalogSnapshot(context.Background())
	if err != nil {
		t.Fatalf("get skill snapshot: %v", err)
	}
	if snapshot.Meta.Domain != CatalogDomainSkill {
		t.Fatalf("expected skill domain metadata, got %#v", snapshot.Meta)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].Name != "legacy-review" {
		t.Fatalf("unexpected legacy skill catalog items: %#v", snapshot.Items)
	}
}

func TestFileStoreSkillAndMemoryCatalogRoundTrip(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	skillSyncedAt := mustTime("2026-03-25T10:00:00Z")
	memorySyncedAt := mustTime("2026-03-25T11:00:00Z")
	err = fs.SaveSkillCatalogSnapshot(context.Background(), SkillCatalogSnapshot{
		Meta: CatalogMetadata{
			Domain:        CatalogDomainSkill,
			SourceOfTruth: CatalogSourceTruthClaude,
			SyncState:     CatalogSyncStateSynced,
			DriftDetected: false,
			LastSyncedAt:  skillSyncedAt,
			VersionToken:  "skill-v1",
		},
		Items: []SkillDefinition{{
			Name:          "local-review",
			Description:   "desc",
			Prompt:        "prompt",
			ResultView:    "review-card",
			TargetType:    "diff",
			Source:        SkillSourceLocal,
			SourceOfTruth: CatalogSourceTruthClaude,
			SyncState:     CatalogSyncStateDraft,
			Editable:      true,
			DriftDetected: true,
			LastSyncedAt:  skillSyncedAt,
		}},
	})
	if err != nil {
		t.Fatalf("save skill catalog: %v", err)
	}
	err = fs.SaveMemoryCatalogSnapshot(context.Background(), MemoryCatalogSnapshot{
		Meta: CatalogMetadata{
			Domain:        CatalogDomainMemory,
			SourceOfTruth: CatalogSourceTruthClaude,
			SyncState:     CatalogSyncStateDraft,
			DriftDetected: true,
			LastSyncedAt:  memorySyncedAt,
			VersionToken:  "memory-v1",
		},
		Items: []MemoryItem{{
			ID:            "mem-1",
			Title:         "Memory 1",
			Content:       "content",
			Source:        "local",
			SourceOfTruth: CatalogSourceTruthClaude,
			SyncState:     CatalogSyncStateSynced,
			Editable:      true,
			DriftDetected: false,
			LastSyncedAt:  memorySyncedAt,
		}},
	})
	if err != nil {
		t.Fatalf("save memory catalog: %v", err)
	}
	skillSnapshot, err := fs.GetSkillCatalogSnapshot(context.Background())
	if err != nil {
		t.Fatalf("get skill snapshot: %v", err)
	}
	if skillSnapshot.Meta.SyncState != CatalogSyncStateSynced || skillSnapshot.Meta.VersionToken != "skill-v1" {
		t.Fatalf("unexpected skill snapshot meta: %#v", skillSnapshot.Meta)
	}
	if len(skillSnapshot.Items) != 1 || skillSnapshot.Items[0].Name != "local-review" || skillSnapshot.Items[0].LastSyncedAt.IsZero() {
		t.Fatalf("unexpected skill catalog: %#v", skillSnapshot.Items)
	}
	memorySnapshot, err := fs.GetMemoryCatalogSnapshot(context.Background())
	if err != nil {
		t.Fatalf("get memory snapshot: %v", err)
	}
	if memorySnapshot.Meta.SyncState != CatalogSyncStateDraft || !memorySnapshot.Meta.DriftDetected {
		t.Fatalf("unexpected memory snapshot meta: %#v", memorySnapshot.Meta)
	}
	if len(memorySnapshot.Items) != 1 || memorySnapshot.Items[0].ID != "mem-1" || memorySnapshot.Items[0].SyncState != CatalogSyncStateSynced {
		t.Fatalf("unexpected memory catalog: %#v", memorySnapshot.Items)
	}
}

func TestFileStoreMemoryCatalogUpsertReadBackIncludesNewItem(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	updatedAt := mustTime("2026-03-25T12:00:00Z")
	if err := fs.SaveMemoryCatalogSnapshot(context.Background(), MemoryCatalogSnapshot{
		Meta: CatalogMetadata{Domain: CatalogDomainMemory},
		Items: []MemoryItem{{
			ID:        "mem-new",
			Title:     "Remember",
			Content:   "remember this",
			Source:    "local",
			Editable:  true,
			UpdatedAt: updatedAt,
		}},
	}); err != nil {
		t.Fatalf("save memory catalog snapshot: %v", err)
	}
	items, err := fs.ListMemoryCatalog(context.Background())
	if err != nil {
		t.Fatalf("list memory catalog: %v", err)
	}
	if len(items) != 1 || items[0].ID != "mem-new" || items[0].Content != "remember this" {
		t.Fatalf("unexpected memory items: %#v", items)
	}
}

func TestFileStoreNormalizesExternalCodexOwnershipFromRuntime(t *testing.T) {
	record := normalizeSessionRecord(SessionRecord{
		Summary: SessionSummary{
			ID:        "codex-thread:thread-ownership",
			Title:     "Codex 会话",
			CreatedAt: mustTime("2026-04-04T02:00:00Z"),
			UpdatedAt: mustTime("2026-04-04T02:00:00Z"),
			External:  true,
			Runtime: SessionRuntime{
				ResumeSessionID: "thread-ownership",
				Command:         "codex",
				Engine:          "codex",
				CWD:             "/tmp/project",
				Source:          "codex-native",
			},
		},
		Projection: ProjectionSnapshot{
			Runtime: SessionRuntime{
				ResumeSessionID: "thread-ownership",
				Command:         "codex",
				Engine:          "codex",
				CWD:             "/tmp/project",
				Source:          "codex-native",
			},
		},
	})

	if record.Summary.Source != "codex-native" ||
		record.Summary.Runtime.Source != "codex-native" ||
		record.Summary.Ownership != "codex-native" {
		t.Fatalf("expected codex-native ownership/source, got %#v", record.Summary)
	}
}

func TestFileStoreDoesNotInferClaudeOwnershipForUnknownExternalSession(t *testing.T) {
	record := normalizeSessionRecord(SessionRecord{
		Summary: SessionSummary{
			ID:        "external-session",
			Title:     "External session",
			CreatedAt: mustTime("2026-04-04T02:00:00Z"),
			UpdatedAt: mustTime("2026-04-04T02:00:00Z"),
			External:  true,
		},
	})

	if record.Summary.Ownership == "claude-native" ||
		record.Summary.Source == "claude-native" ||
		record.Summary.Runtime.Source == "claude-native" {
		t.Fatalf("unknown external session must not default to claude-native: %#v", record.Summary)
	}
}

func TestFileStoreMemoryCatalogNormalizationDefaultsDomainAndSyncState(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	if err := fs.SaveMemoryCatalogSnapshot(context.Background(), MemoryCatalogSnapshot{
		Meta:  CatalogMetadata{},
		Items: []MemoryItem{{ID: "mem-1", Title: "Memory 1", Content: "hello"}},
	}); err != nil {
		t.Fatalf("save memory catalog snapshot: %v", err)
	}
	snapshot, err := fs.GetMemoryCatalogSnapshot(context.Background())
	if err != nil {
		t.Fatalf("get memory snapshot: %v", err)
	}
	if snapshot.Meta.Domain != CatalogDomainMemory {
		t.Fatalf("expected memory domain, got %#v", snapshot.Meta)
	}
	if snapshot.Meta.SyncState != CatalogSyncStateIdle {
		t.Fatalf("expected idle sync state, got %#v", snapshot.Meta)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].ID != "mem-1" {
		t.Fatalf("unexpected memory snapshot items: %#v", snapshot.Items)
	}
}

func TestFileStoreListSessionsHidesUntouchedAutoSessionsWhenRealSessionExists(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}

	meaningful := normalizeSessionRecord(SessionRecord{
		Summary: SessionSummary{
			ID:        "session-real",
			Title:     "修复 flutter 会话列表",
			CreatedAt: mustTime("2026-04-01T10:00:00Z"),
			UpdatedAt: mustTime("2026-04-01T10:05:00Z"),
			Runtime:   SessionRuntime{Source: "mobilevc"},
		},
		Projection: ProjectionSnapshot{
			RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
			LogEntries: []SnapshotLogEntry{
				{Kind: "user", Message: "修复 flutter 会话列表"},
				{Kind: "markdown", Message: "先查空会话来源"},
			},
			Runtime: SessionRuntime{Source: "mobilevc"},
		},
	})
	autoOlder := normalizeSessionRecord(SessionRecord{
		Summary: SessionSummary{
			ID:        "session-auto-old",
			Title:     "2026-04-01 17:59",
			CreatedAt: mustTime("2026-04-01T09:59:49Z"),
			UpdatedAt: mustTime("2026-04-01T09:59:49Z"),
			Runtime:   SessionRuntime{Source: "mobilevc", CWD: "/Users/wust_lh/MobileVC"},
		},
		Projection: ProjectionSnapshot{
			RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
			Runtime:             SessionRuntime{Source: "mobilevc", CWD: "/Users/wust_lh/MobileVC"},
		},
	})
	autoNewer := normalizeSessionRecord(SessionRecord{
		Summary: SessionSummary{
			ID:        "session-auto-new",
			Title:     "2026-04-01 18:01",
			CreatedAt: mustTime("2026-04-01T10:01:00Z"),
			UpdatedAt: mustTime("2026-04-01T10:01:00Z"),
			Runtime:   SessionRuntime{Source: "mobilevc", CWD: "/Users/wust_lh/MobileVC"},
		},
		Projection: ProjectionSnapshot{
			RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
			Runtime:             SessionRuntime{Source: "mobilevc", CWD: "/Users/wust_lh/MobileVC"},
		},
	})

	writeSessionRecordFixture(t, fs, meaningful)
	writeSessionRecordFixture(t, fs, autoOlder)
	writeSessionRecordFixture(t, fs, autoNewer)
	writeSessionIndexFixture(t, fs, []SessionSummary{
		autoNewer.Summary,
		meaningful.Summary,
		autoOlder.Summary,
	})

	items, err := fs.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected only meaningful session, got %#v", items)
	}
	if items[0].ID != meaningful.Summary.ID {
		t.Fatalf("expected meaningful session to remain, got %#v", items)
	}
}

func TestFileStoreListSessionsKeepsNewestUntouchedAutoSessionWhenOnlyPlaceholdersExist(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}

	autoOlder := normalizeSessionRecord(SessionRecord{
		Summary: SessionSummary{
			ID:        "session-auto-old",
			Title:     "2026-04-01 17:59",
			CreatedAt: mustTime("2026-04-01T09:59:49Z"),
			UpdatedAt: mustTime("2026-04-01T09:59:49Z"),
			Runtime:   SessionRuntime{Source: "mobilevc"},
		},
		Projection: ProjectionSnapshot{
			RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
			Runtime:             SessionRuntime{Source: "mobilevc"},
		},
	})
	autoNewer := normalizeSessionRecord(SessionRecord{
		Summary: SessionSummary{
			ID:        "session-auto-new",
			Title:     "2026-04-01 18:01",
			CreatedAt: mustTime("2026-04-01T10:01:00Z"),
			UpdatedAt: mustTime("2026-04-01T10:01:00Z"),
			Runtime:   SessionRuntime{Source: "mobilevc"},
		},
		Projection: ProjectionSnapshot{
			RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
			Runtime:             SessionRuntime{Source: "mobilevc"},
		},
	})

	writeSessionRecordFixture(t, fs, autoOlder)
	writeSessionRecordFixture(t, fs, autoNewer)
	writeSessionIndexFixture(t, fs, []SessionSummary{
		autoOlder.Summary,
		autoNewer.Summary,
	})

	items, err := fs.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected only newest placeholder, got %#v", items)
	}
	if items[0].ID != autoNewer.Summary.ID {
		t.Fatalf("expected newest placeholder session, got %#v", items)
	}
}

func TestFileStoreAppendSessionLogEntriesUpdatesWindowIndexAndJSONLCursor(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	created, err := fs.CreateSession(context.Background(), "append window")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := fs.SaveProjection(context.Background(), created.ID, ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		LogEntries: []SnapshotLogEntry{{
			Kind:      "user",
			Message:   "first",
			Timestamp: "2026-06-07T01:00:00Z",
		}},
	}); err != nil {
		t.Fatalf("save initial projection: %v", err)
	}

	summary, err := fs.AppendSessionLogEntries(context.Background(), created.ID, []SnapshotLogEntry{{
		Kind:      "markdown",
		Message:   "second",
		Timestamp: "2026-06-07T01:01:00Z",
	}}, WithJSONLSyncEntryCount(2))
	if err != nil {
		t.Fatalf("append log entries: %v", err)
	}
	if summary.EntryCount != 2 {
		t.Fatalf("expected appended entry count, got %#v", summary)
	}
	if summary.JSONLSyncEntryCount != 2 {
		t.Fatalf("expected jsonl sync count to update, got %#v", summary)
	}

	window, err := fs.GetSessionHistoryWindow(context.Background(), SessionHistoryWindowRequest{SessionID: created.ID, Limit: 2})
	if err != nil {
		t.Fatalf("read appended window: %v", err)
	}
	if window.LogEntryTotal != 2 || len(window.LogEntries) != 2 {
		t.Fatalf("unexpected appended window: %#v", window)
	}
	if window.LogEntries[1].Message != "second" {
		t.Fatalf("expected appended entry in window, got %#v", window.LogEntries)
	}
	record, err := fs.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get appended session: %v", err)
	}
	if record.Summary.JSONLSyncEntryCount != 2 {
		t.Fatalf("expected jsonl sync count to survive full read, got %#v", record.Summary)
	}
}

func writeSessionRecordFixture(t *testing.T, fs *FileStore, record SessionRecord) {
	t.Helper()
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatalf("marshal session record: %v", err)
	}
	if err := os.WriteFile(fs.sessionPath(record.Summary.ID), data, 0o644); err != nil {
		t.Fatalf("write session record: %v", err)
	}
}

func writeSessionIndexFixture(t *testing.T, fs *FileStore, items []SessionSummary) {
	t.Helper()
	data, err := json.MarshalIndent(fileIndex{Sessions: items}, "", "  ")
	if err != nil {
		t.Fatalf("marshal session index: %v", err)
	}
	if err := os.WriteFile(fs.indexPath, data, 0o644); err != nil {
		t.Fatalf("write session index: %v", err)
	}
}

func mustTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}
