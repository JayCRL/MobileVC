package session

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mobilevc/internal/data"
	"mobilevc/internal/protocol"
)

func TestToProtocolSummary(t *testing.T) {
	created := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	in := data.SessionSummary{
		ID:                "s1",
		Title:             "title",
		CreatedAt:         created,
		UpdatedAt:         created,
		LastPreview:       "hi",
		EntryCount:        3,
		Source:            "mobilevc",
		External:          true,
		Ownership:         "user",
		ExecutionActive:   true,
		ClaudeSessionUUID: "claude-uuid-1",
		Runtime: data.SessionRuntime{
			ResumeSessionID: "r1",
			Command:         "claude",
			Engine:          "claude",
			CWD:             "/p",
			PermissionMode:  "auto",
			ClaudeLifecycle: "active",
			Source:          "mobilevc",
		},
	}
	got := ToProtocolSummary(in)
	if got.ID != "s1" || got.Title != "title" || got.LastPreview != "hi" || got.EntryCount != 3 {
		t.Errorf("basic fields: %+v", got)
	}
	if got.External != true || got.Ownership != "user" || got.ExecutionActive != true {
		t.Errorf("flags: %+v", got)
	}
	if got.Runtime.ResumeSessionID != "r1" || got.Runtime.Command != "claude" || got.Runtime.Engine != "claude" {
		t.Errorf("runtime: %+v", got.Runtime)
	}
	if got.Runtime.ClaudeSessionUUID != "claude-uuid-1" {
		t.Errorf("claude session uuid: %+v", got.Runtime)
	}
	if !strings.HasPrefix(got.CreatedAt, "2026-01-01") {
		t.Errorf("created at format: %q", got.CreatedAt)
	}
}

func TestToProtocolSessionContext(t *testing.T) {
	in := data.SessionContext{
		EnabledSkillNames: []string{"a", "b"},
		EnabledMemoryIDs:  []string{"m1"},
		Configured:        true,
	}
	got := ToProtocolSessionContext(in)
	if len(got.EnabledSkillNames) != 2 || got.EnabledSkillNames[0] != "a" {
		t.Errorf("skills: %+v", got.EnabledSkillNames)
	}
	if len(got.EnabledMemoryIDs) != 1 || got.EnabledMemoryIDs[0] != "m1" {
		t.Errorf("memory: %+v", got.EnabledMemoryIDs)
	}
	// 验证拷贝独立: 修改原 slice 不影响 got
	in.EnabledSkillNames[0] = "x"
	if got.EnabledSkillNames[0] == "x" {
		t.Errorf("expected independent copy")
	}
}

func TestToProtocolContextWindowUsage(t *testing.T) {
	got := ToProtocolContextWindowUsage(data.ContextWindowUsage{
		TokensUsed: 250,
		TokenLimit: 200,
	})
	if got.TokenLimit != 200 {
		t.Fatalf("expected token limit 200, got %d", got.TokenLimit)
	}
	if got.TokensUsed != 200 {
		t.Fatalf("expected clamped tokens used 200, got %d", got.TokensUsed)
	}
}

func TestToProtocolCatalogMetadata(t *testing.T) {
	t.Run("zero time omitted", func(t *testing.T) {
		got := ToProtocolCatalogMetadata(data.CatalogMetadata{})
		if got.LastSyncedAt != "" {
			t.Errorf("expected empty, got %q", got.LastSyncedAt)
		}
	})
	t.Run("synced at formatted", func(t *testing.T) {
		ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
		got := ToProtocolCatalogMetadata(data.CatalogMetadata{LastSyncedAt: ts, VersionToken: "v1", LastError: "err"})
		if !strings.HasPrefix(got.LastSyncedAt, "2026-01-01") {
			t.Errorf("formatted: %q", got.LastSyncedAt)
		}
		if got.VersionToken != "v1" || got.LastError != "err" {
			t.Errorf("misc: %+v", got)
		}
	})
}

func TestHistoryContextFromSnapshot(t *testing.T) {
	if got := HistoryContextFromSnapshot(nil); got != nil {
		t.Errorf("expected nil for nil input")
	}
	in := &data.SnapshotContext{
		ID: "id", Type: "step", Message: "msg", Path: "p",
		PendingReview: true, GroupID: "g1",
	}
	got := HistoryContextFromSnapshot(in)
	if got.ID != "id" || got.Path != "p" || !got.PendingReview || got.GroupID != "g1" {
		t.Errorf("got %+v", got)
	}
}

func TestProtocolReviewFile(t *testing.T) {
	in := ReviewFile{ContextID: "f1", Path: "p", Title: "T", Diff: "d", PendingReview: true}
	got := ProtocolReviewFile(in)
	if got.ID != "f1" || got.Path != "p" || got.Title != "T" || !got.PendingReview {
		t.Errorf("got %+v", got)
	}
}

func TestProtocolReviewGroup(t *testing.T) {
	if got := ProtocolReviewGroup(nil); got != nil {
		t.Errorf("expected nil for nil")
	}
	in := &ReviewGroup{
		ID:    "g1",
		Title: "T",
		Files: []ReviewFile{{ContextID: "f1"}, {ContextID: "f2"}},
	}
	got := ProtocolReviewGroup(in)
	if got.ID != "g1" {
		t.Errorf("id: %q", got.ID)
	}
	if len(got.Files) != 2 {
		t.Errorf("files: %+v", got.Files)
	}
}

func TestProtocolReviewGroups(t *testing.T) {
	if got := ProtocolReviewGroups(nil); got != nil {
		t.Errorf("expected nil")
	}
	got := ProtocolReviewGroups([]ReviewGroup{{ID: "g1"}, {ID: "g2"}})
	if len(got) != 2 {
		t.Errorf("expected 2, got %+v", got)
	}
}

func TestProtocolDiffContextAndContexts(t *testing.T) {
	if got := ProtocolDiffContext(nil); got != nil {
		t.Errorf("expected nil")
	}
	d := &DiffContext{ContextID: "c1", Path: "p", Diff: "+ a", PendingReview: true, GroupID: "g1"}
	got := ProtocolDiffContext(d)
	if got.ID != "c1" || got.Type != "diff" || got.Path != "p" {
		t.Errorf("got %+v", got)
	}
	gotMany := ProtocolDiffContexts([]DiffContext{*d})
	if len(gotMany) != 1 {
		t.Errorf("expected 1 context, got %d", len(gotMany))
	}
	if got2 := ProtocolDiffContexts(nil); got2 != nil {
		t.Errorf("expected nil for nil input")
	}
}

func TestReviewStateEventFromProjection(t *testing.T) {
	projection := data.ProjectionSnapshot{
		Diffs: []DiffContext{{ContextID: "f1", PendingReview: true, Path: "p"}},
	}
	got := ReviewStateEventFromProjection("s1", projection)
	if got.SessionID != "s1" {
		t.Errorf("session id: %q", got.SessionID)
	}
	if len(got.Groups) == 0 {
		t.Errorf("expected review groups built")
	}
	if got.ActiveGroup == nil {
		t.Errorf("expected active review group")
	}
}

func TestReviewStatusFromDecision(t *testing.T) {
	cases := []struct{ in, want string }{
		{"accept", "accepted"},
		{"  ACCEPT  ", "accepted"},
		{"revert", "reverted"},
		{"revise", "revised"},
		{"yolo", "pending"},
		{"", "pending"},
	}
	for _, tc := range cases {
		if got := reviewStatusFromDecision(tc.in); got != tc.want {
			t.Errorf("(%q) -> %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSnapshotDiffMatches(t *testing.T) {
	t.Run("by context id", func(t *testing.T) {
		if !snapshotDiffMatches(DiffContext{ContextID: "c1"}, "c1", "") {
			t.Errorf("expected match by ctx id")
		}
	})
	t.Run("by path", func(t *testing.T) {
		if !snapshotDiffMatches(DiffContext{Path: "p"}, "", "p") {
			t.Errorf("expected match by path")
		}
	})
	t.Run("no match", func(t *testing.T) {
		if snapshotDiffMatches(DiffContext{ContextID: "x", Path: "y"}, "c1", "p") {
			t.Errorf("expected no match")
		}
	})
	t.Run("trim whitespace", func(t *testing.T) {
		if !snapshotDiffMatches(DiffContext{ContextID: "  c1  "}, "c1", "") {
			t.Errorf("expected match with trimmed whitespace")
		}
	})
}

func TestApplyReviewDecisionToProjection_Accept(t *testing.T) {
	snapshot := data.ProjectionSnapshot{
		Diffs: []DiffContext{{ContextID: "f1", Path: "p", PendingReview: true}},
	}
	current := DiffContext{ContextID: "f1", Path: "p"}
	got := ApplyReviewDecisionToProjection(snapshot,
		protocol.ReviewDecisionRequestEvent{ContextID: "f1"}, "accept", current)
	if got.Diffs[0].PendingReview {
		t.Errorf("accept should clear pendingReview")
	}
	if got.Diffs[0].ReviewStatus != "accepted" {
		t.Errorf("status: %q", got.Diffs[0].ReviewStatus)
	}
}

func TestApplyReviewDecisionToProjection_RevisePreservesPending(t *testing.T) {
	snapshot := data.ProjectionSnapshot{
		Diffs: []DiffContext{{ContextID: "f1", Path: "p", PendingReview: false}},
	}
	current := DiffContext{ContextID: "f1"}
	got := ApplyReviewDecisionToProjection(snapshot,
		protocol.ReviewDecisionRequestEvent{ContextID: "f1"}, "revise", current)
	if !got.Diffs[0].PendingReview {
		t.Errorf("revise should set pendingReview true")
	}
	if got.Diffs[0].ReviewStatus != "revised" {
		t.Errorf("status: %q", got.Diffs[0].ReviewStatus)
	}
}

func TestApplyReviewDecisionToProjection_FillsGroupID(t *testing.T) {
	snapshot := data.ProjectionSnapshot{
		Diffs: []DiffContext{{ContextID: "f1", Path: "p"}}, // 没有 GroupID
	}
	current := DiffContext{ContextID: "f1"}
	got := ApplyReviewDecisionToProjection(snapshot,
		protocol.ReviewDecisionRequestEvent{ContextID: "f1", GroupID: "g1"}, "revert", current)
	if got.Diffs[0].GroupID != "g1" {
		t.Errorf("expected group id filled, got %q", got.Diffs[0].GroupID)
	}
}

func TestSessionHistoryEventFromRecord(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1", Title: "T"},
		Projection: data.ProjectionSnapshot{
			LogEntries: []data.SnapshotLogEntry{{Kind: "markdown", Message: "hi"}},
			Diffs:      []DiffContext{{ContextID: "f1", PendingReview: true}},
			ContextWindowUsage: data.ContextWindowUsage{
				TokensUsed: 1200,
				TokenLimit: 2000,
			},
			Runtime: data.SessionRuntime{
				ResumeSessionID: "r1",
				Command:         "claude",
				Engine:          "claude",
			},
		},
	}
	got := SessionHistoryEventFromRecord(record, true)
	if got.SessionID != "s1" {
		t.Errorf("session id: %q", got.SessionID)
	}
	if got.Summary.ID != "s1" {
		t.Errorf("summary id: %q", got.Summary.ID)
	}
	if len(got.LogEntries) != 1 {
		t.Errorf("expected 1 log entry, got %d", len(got.LogEntries))
	}
	if got.ContextWindowUsage.TokensUsed != 1200 ||
		got.ContextWindowUsage.TokenLimit != 2000 {
		t.Fatalf("unexpected context window usage: %+v", got.ContextWindowUsage)
	}
	if !got.CanResume {
		t.Errorf("expected CanResume=true")
	}
	if !got.RuntimeAlive {
		t.Errorf("expected RuntimeAlive=true")
	}
}

func TestSessionHistoryWindowEventFromRecordReturnsTailWindow(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			LogEntries: []data.SnapshotLogEntry{
				{Kind: "markdown", Message: "a"},
				{Kind: "markdown", Message: "b"},
				{Kind: "markdown", Message: "c"},
			},
		},
	}

	got := SessionHistoryWindowEventFromRecord(record, false, 2)

	if got.LogEntryStart != 1 || got.LogEntryTotal != 3 || !got.HasMoreBefore {
		t.Fatalf("unexpected window metadata: start=%d total=%d hasMore=%v", got.LogEntryStart, got.LogEntryTotal, got.HasMoreBefore)
	}
	if got.Latest.LogEntryCount != 3 {
		t.Fatalf("latest log entry count should use total history count, got %d", got.Latest.LogEntryCount)
	}
	if len(got.LogEntries) != 2 || got.LogEntries[0].Message != "b" || got.LogEntries[1].Message != "c" {
		t.Fatalf("unexpected window entries: %+v", got.LogEntries)
	}
}

func TestSessionHistoryPageEventFromRecordReturnsEarlierWindow(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			LogEntries: []data.SnapshotLogEntry{
				{Kind: "markdown", Message: "a"},
				{Kind: "markdown", Message: "b"},
				{Kind: "markdown", Message: "c"},
				{Kind: "markdown", Message: "d"},
			},
		},
	}

	got := SessionHistoryPageEventFromRecord(record, 2, 2)

	if got.LogEntryStart != 0 || got.LogEntryTotal != 4 || got.HasMoreBefore {
		t.Fatalf("unexpected page metadata: start=%d total=%d hasMore=%v", got.LogEntryStart, got.LogEntryTotal, got.HasMoreBefore)
	}
	if got.Latest.LogEntryCount != 4 {
		t.Fatalf("latest log entry count should use total history count, got %d", got.Latest.LogEntryCount)
	}
	if len(got.LogEntries) != 2 || got.LogEntries[0].Message != "a" || got.LogEntries[1].Message != "b" {
		t.Fatalf("unexpected page entries: %+v", got.LogEntries)
	}
}

func TestSessionDeltaEventFromRecord_LightweightWhenKnownEmpty(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			LogEntries: []data.SnapshotLogEntry{
				{Kind: "markdown", Message: "a"},
				{Kind: "markdown", Message: "b"},
			},
			ContextWindowUsage: data.ContextWindowUsage{
				TokensUsed: 300,
				TokenLimit: 1000,
			},
			RawTerminalByStream: map[string]string{"stdout": "stdout-content"},
			Runtime:             data.SessionRuntime{Command: "claude"},
		},
	}
	got := SessionDeltaEventFromRecord(record, protocol.SessionDeltaKnown{}, DeltaCursorSnapshot{}, false)
	if got.SessionID != "s1" {
		t.Errorf("session id: %q", got.SessionID)
	}
	if len(got.AppendLogEntries) != 2 {
		t.Errorf("expected 2 log entries, got %d", len(got.AppendLogEntries))
	}
	if len(got.RawTerminalByStream) != 0 {
		t.Errorf("expected terminal content to stay lazy-loaded, got %+v", got.RawTerminalByStream)
	}
	if got.ContextWindowUsage.TokensUsed != 300 ||
		got.ContextWindowUsage.TokenLimit != 1000 {
		t.Fatalf("unexpected context window usage: %+v", got.ContextWindowUsage)
	}
	if got.Latest.LogEntryCount != 2 {
		t.Errorf("latest log entry count: %d", got.Latest.LogEntryCount)
	}
	if got.Latest.TerminalStdoutLength != len("stdout-content") {
		t.Errorf("latest terminal stdout length: %d", got.Latest.TerminalStdoutLength)
	}
}

func TestSessionDeltaEventFromRecord_RespectsKnownLog(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			LogEntries: []data.SnapshotLogEntry{
				{Kind: "markdown", Message: "a"},
				{Kind: "markdown", Message: "b"},
				{Kind: "markdown", Message: "c"},
			},
			Runtime: data.SessionRuntime{Command: "claude"},
		},
	}
	known := protocol.SessionDeltaKnown{LogEntryCount: 2}
	got := SessionDeltaEventFromRecord(record, known, DeltaCursorSnapshot{}, false)
	if len(got.AppendLogEntries) != 1 {
		t.Errorf("expected 1 new entry, got %d", len(got.AppendLogEntries))
	}
}

func TestSessionDeltaEventFromRecord_PreservesLogEntryAttachments(t *testing.T) {
	attachment := protocol.TimelineAttachment{
		ID:            "att-1",
		Kind:          "image",
		Name:          "screen.png",
		MIMEType:      "image/png",
		Size:          9,
		Path:          "/tmp/screen.png",
		PreviewStatus: "available",
		Source:        "user_upload",
	}
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			LogEntries: []data.SnapshotLogEntry{
				{
					Kind:        "user",
					Message:     "看图",
					Attachments: []protocol.TimelineAttachment{attachment},
				},
			},
			Runtime: data.SessionRuntime{Command: "claude"},
		},
	}

	got := SessionDeltaEventFromRecord(record, protocol.SessionDeltaKnown{}, DeltaCursorSnapshot{}, false)
	if len(got.AppendLogEntries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(got.AppendLogEntries))
	}
	attachments := got.AppendLogEntries[0].Attachments
	if len(attachments) != 1 {
		t.Fatalf("expected attachment metadata, got %+v", got.AppendLogEntries[0])
	}
	if attachments[0].ID != attachment.ID || attachments[0].Path != attachment.Path {
		t.Fatalf("attachment metadata not preserved: %+v", attachments[0])
	}
}

func TestSessionDeltaEventFromRecord_KnownExceedsLatestResetsToZero(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			LogEntries: []data.SnapshotLogEntry{{Kind: "markdown", Message: "a"}},
			Runtime:    data.SessionRuntime{Command: "claude"},
		},
	}
	known := protocol.SessionDeltaKnown{LogEntryCount: 100}
	got := SessionDeltaEventFromRecord(record, known, DeltaCursorSnapshot{}, false)
	if !got.RequiresFullSync {
		t.Fatalf("expected full sync when known log count is invalid")
	}
	if len(got.AppendLogEntries) != 0 {
		t.Errorf("expected invalid delta base to avoid full log delivery, got %d", len(got.AppendLogEntries))
	}
}

func TestSessionHistoryWindowEventFromRecordWithPayloadLimitShrinksEntries(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			LogEntries: []data.SnapshotLogEntry{
				{Kind: "markdown", Message: strings.Repeat("a", 32)},
				{Kind: "markdown", Message: strings.Repeat("b", 32)},
				{Kind: "markdown", Message: strings.Repeat("c", 32)},
			},
			Runtime: data.SessionRuntime{Command: "claude"},
		},
	}
	lightweight := SessionHistoryWindowEventFromRecordWithPayloadLimit(record, false, 3, 0)
	lightweightBytes, err := json.Marshal(lightweight)
	if err != nil {
		t.Fatalf("marshal lightweight history: %v", err)
	}

	got := SessionHistoryWindowEventFromRecordWithPayloadLimit(record, false, 3, len(lightweightBytes)-10)

	if len(got.LogEntries) >= len(lightweight.LogEntries) {
		t.Fatalf("expected shrunken history window, got %d entries", len(got.LogEntries))
	}
	if got.LogEntryTotal != 3 || !got.HasMoreBefore {
		t.Fatalf("unexpected window metadata: start=%d total=%d hasMore=%v", got.LogEntryStart, got.LogEntryTotal, got.HasMoreBefore)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal shrunken history: %v", err)
	}
	if len(encoded) > len(lightweightBytes)-10 {
		t.Fatalf("history payload still exceeds budget: got %d budget %d", len(encoded), len(lightweightBytes)-10)
	}
}

func TestSessionHistoryWindowEventFromRecordOmitsLazyTerminalPayload(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			LogEntries: []data.SnapshotLogEntry{
				{Kind: "markdown", Message: "visible"},
			},
			RawTerminalByStream: map[string]string{
				"stdout": strings.Repeat("x", 4096),
				"stderr": "",
			},
			TerminalExecutions: []data.TerminalExecution{
				{
					ExecutionID: "exec-1",
					Command:     strings.Repeat("cmd", 256),
					Stdout:      strings.Repeat("y", 4096),
				},
			},
			Runtime: data.SessionRuntime{Command: "claude"},
		},
	}
	budget := 1400

	got := SessionHistoryWindowEventFromRecordWithCursorAndPayloadLimit(record, false, 1, DeltaCursorSnapshot{LatestCursor: 9}, budget)

	if got.PayloadLimited {
		t.Fatalf("expected lazy terminal omission without payload limiting")
	}
	if len(got.RawTerminalByStream) != 0 {
		t.Fatalf("expected raw terminal payload to be dropped, got %+v", got.RawTerminalByStream)
	}
	if len(got.TerminalExecutions) != 0 {
		t.Fatalf("expected terminal executions to be dropped, got %+v", got.TerminalExecutions)
	}
	if got.Latest.EventCursor != 9 || got.Latest.LogEntryCount != 1 || got.Latest.TerminalExecutionCount != 1 {
		t.Fatalf("latest known metadata was not preserved: %+v", got.Latest)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal history: %v", err)
	}
	if len(encoded) > budget {
		t.Fatalf("history payload exceeds budget: got %d budget %d", len(encoded), budget)
	}
}

func TestSessionHistoryWindowEventFromRecordWithPayloadLimitDropsLargeDiffPayload(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			LogEntries: []data.SnapshotLogEntry{
				{Kind: "markdown", Message: "visible"},
			},
			Diffs: []DiffContext{
				{ContextID: "diff-1", Path: "file.go", Diff: strings.Repeat("+", 4096)},
			},
			CurrentDiff: &DiffContext{
				ContextID: "diff-1",
				Path:      "file.go",
				Diff:      strings.Repeat("+", 4096),
			},
			ReviewGroups: []ReviewGroup{
				{ID: "group-1", Title: strings.Repeat("review", 512)},
			},
			ActiveReviewGroup: &ReviewGroup{ID: "group-1"},
			CurrentStep:       &data.SnapshotContext{Message: strings.Repeat("step", 512)},
			LatestError:       &data.SnapshotContext{Message: strings.Repeat("error", 512)},
			Runtime:           data.SessionRuntime{Command: "claude"},
		},
	}
	budget := 1500

	got := SessionHistoryWindowEventFromRecordWithPayloadLimit(record, false, 1, budget)

	if !got.PayloadLimited {
		t.Fatalf("expected payloadLimited history")
	}
	if len(got.Diffs) != 0 || got.CurrentDiff != nil || len(got.ReviewGroups) != 0 || got.ActiveReviewGroup != nil {
		t.Fatalf("expected diff/review payload to be dropped: diffs=%d current=%v reviews=%d active=%v", len(got.Diffs), got.CurrentDiff, len(got.ReviewGroups), got.ActiveReviewGroup)
	}
	if got.CurrentStep != nil || got.LatestError != nil {
		t.Fatalf("expected step/error payload to be dropped")
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal history: %v", err)
	}
	if len(encoded) > budget {
		t.Fatalf("history payload exceeds budget: got %d budget %d", len(encoded), budget)
	}
}

func TestSessionHistoryPageEventFromRecordWithPayloadLimitShrinksEntries(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			LogEntries: []data.SnapshotLogEntry{
				{Kind: "markdown", Message: strings.Repeat("a", 32)},
				{Kind: "markdown", Message: strings.Repeat("b", 32)},
				{Kind: "markdown", Message: strings.Repeat("c", 32)},
			},
			Runtime: data.SessionRuntime{Command: "claude"},
		},
	}
	full := SessionHistoryPageEventFromRecord(record, 3, 3)
	fullBytes, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal full page: %v", err)
	}

	got := SessionHistoryPageEventFromRecordWithPayloadLimit(record, 3, 3, len(fullBytes)-10)

	if len(got.LogEntries) >= len(full.LogEntries) {
		t.Fatalf("expected shrunken history page, got %d entries", len(got.LogEntries))
	}
	if got.LogEntryTotal != 3 || !got.HasMoreBefore {
		t.Fatalf("unexpected page metadata: start=%d total=%d hasMore=%v", got.LogEntryStart, got.LogEntryTotal, got.HasMoreBefore)
	}
	if got.Latest.LogEntryCount != 3 {
		t.Fatalf("latest log entry count should survive payload shrinking, got %d", got.Latest.LogEntryCount)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal shrunken page: %v", err)
	}
	if len(encoded) > len(fullBytes)-10 {
		t.Fatalf("page payload still exceeds budget: got %d budget %d", len(encoded), len(fullBytes)-10)
	}
}

func TestSessionHistoryPageEventFromWindowWithPayloadLimitShrinksBeforeFullMarshal(t *testing.T) {
	entries := []data.SnapshotLogEntry{
		{Kind: "markdown", Message: strings.Repeat("a", 1024)},
		{Kind: "markdown", Message: strings.Repeat("b", 1024)},
		{Kind: "markdown", Message: strings.Repeat("c", 1024)},
	}
	window := data.SessionHistoryWindow{
		Record: data.SessionRecord{
			Summary:    data.SessionSummary{ID: "s1"},
			Projection: data.ProjectionSnapshot{Runtime: data.SessionRuntime{Command: "claude"}},
		},
		LogEntries:    entries,
		LogEntryStart: 0,
		LogEntryTotal: len(entries),
	}
	budget := 2300

	got := SessionHistoryPageEventFromWindowWithPayloadLimit(window, budget)

	if !got.PayloadLimited {
		t.Fatalf("expected payloadLimited history page")
	}
	if len(got.LogEntries) >= len(entries) {
		t.Fatalf("expected shrunken window history page, got %d entries", len(got.LogEntries))
	}
	if got.LogEntryTotal != len(entries) || got.Latest.LogEntryCount != len(entries) {
		t.Fatalf("window metadata should survive shrinking: total=%d latest=%d", got.LogEntryTotal, got.Latest.LogEntryCount)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal shrunken window page: %v", err)
	}
	if len(encoded) > budget {
		t.Fatalf("window page payload still exceeds budget: got %d budget %d", len(encoded), budget)
	}
}

func TestSessionDeltaEventFromRecordWithPayloadLimitRequiresFullSyncForLargePayload(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			LogEntries: []data.SnapshotLogEntry{
				{Kind: "markdown", Message: strings.Repeat("a", 64)},
				{Kind: "markdown", Message: strings.Repeat("b", 64)},
				{Kind: "markdown", Message: strings.Repeat("c", 64)},
			},
			Runtime: data.SessionRuntime{Command: "claude"},
		},
	}
	full := SessionDeltaEventFromRecord(record, protocol.SessionDeltaKnown{}, DeltaCursorSnapshot{}, false)
	fullBytes, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal full delta: %v", err)
	}

	got := SessionDeltaEventFromRecordWithPayloadLimit(record, protocol.SessionDeltaKnown{}, DeltaCursorSnapshot{}, false, len(fullBytes)-10, 0)

	if !got.RequiresFullSync {
		t.Fatalf("expected oversized delta to require full sync")
	}
	if !got.PayloadLimited {
		t.Fatalf("expected oversized delta to be marked payloadLimited")
	}
	if got.PayloadLimitReason == "" {
		t.Fatalf("expected payload limit reason")
	}
	if len(got.AppendLogEntries) != 0 {
		t.Fatalf("expected oversized delta to omit append log entries, got %d", len(got.AppendLogEntries))
	}
	if got.Latest.LogEntryCount != 3 {
		t.Fatalf("latest log entry count: %d", got.Latest.LogEntryCount)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal full-sync delta: %v", err)
	}
	if len(encoded) >= len(fullBytes) {
		t.Fatalf("expected full-sync marker to be smaller than full delta: marker=%d full=%d", len(encoded), len(fullBytes))
	}
}

func TestSessionDeltaEventFromRecordWithPayloadLimitUsesMaxAppendEntriesForEmptyKnown(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			LogEntries: []data.SnapshotLogEntry{
				{Kind: "markdown", Message: "a"},
				{Kind: "markdown", Message: "b"},
				{Kind: "markdown", Message: "c"},
			},
			Runtime: data.SessionRuntime{Command: "claude"},
		},
	}

	got := SessionDeltaEventFromRecordWithPayloadLimit(record, protocol.SessionDeltaKnown{}, DeltaCursorSnapshot{}, false, 0, 2)

	if !got.RequiresFullSync {
		t.Fatalf("expected empty-known delta exceeding append limit to require full sync")
	}
	if len(got.AppendLogEntries) != 0 {
		t.Fatalf("expected full-sync marker to omit append entries, got %d", len(got.AppendLogEntries))
	}
	if got.Latest.LogEntryCount != 3 {
		t.Fatalf("latest log entry count: %d", got.Latest.LogEntryCount)
	}
}

func TestSessionDeltaEventFromRecord_LeavesTerminalExecutionsLazyLoaded(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			TerminalExecutions: []data.TerminalExecution{
				{ExecutionID: "exec-1", Command: "first"},
				{ExecutionID: "exec-2", Command: "second"},
			},
			Runtime: data.SessionRuntime{Command: "claude"},
		},
	}
	known := protocol.SessionDeltaKnown{TerminalExecutionCount: 1}
	got := SessionDeltaEventFromRecord(record, known, DeltaCursorSnapshot{}, false)
	if got.Latest.TerminalExecutionCount != 2 {
		t.Fatalf("latest terminal execution count: %d", got.Latest.TerminalExecutionCount)
	}
	if len(got.TerminalExecutions) != 0 {
		t.Fatalf("expected terminal executions to stay lazy-loaded, got %+v", got.TerminalExecutions)
	}
}

func TestSessionDeltaEventFromRecord_LeavesCurrentDiffLazyLoaded(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			Diffs: []DiffContext{{
				ContextID: "diff-1",
				Path:      "large.go",
				Diff:      strings.Repeat("+changed\n", 1024),
			}},
			CurrentDiff: &DiffContext{
				ContextID: "diff-1",
				Path:      "large.go",
				Diff:      strings.Repeat("+changed\n", 1024),
			},
			Runtime: data.SessionRuntime{Command: "claude"},
		},
	}

	got := SessionDeltaEventFromRecord(record, protocol.SessionDeltaKnown{}, DeltaCursorSnapshot{}, false)
	if got.Latest.DiffCount != 1 {
		t.Fatalf("latest diff count: %d", got.Latest.DiffCount)
	}
	if got.CurrentDiff != nil {
		t.Fatalf("expected current diff body to stay lazy-loaded, got %+v", got.CurrentDiff)
	}
	if len(got.UpsertDiffs) != 0 || len(got.ReviewGroups) != 0 || got.ActiveReviewGroup != nil {
		t.Fatalf("expected diff/review payloads to stay lazy-loaded, got upserts=%d reviews=%d active=%v", len(got.UpsertDiffs), len(got.ReviewGroups), got.ActiveReviewGroup)
	}
}

func TestSessionDeltaEventFromRecord_IncludesUpdatedExecutionForAppendedLog(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			LogEntries: []data.SnapshotLogEntry{
				{Kind: "terminal", Message: "started", ExecutionID: "exec-1", Phase: "started"},
				{Kind: "terminal", Message: "done", ExecutionID: "exec-1", Phase: "finished"},
			},
			TerminalExecutions: []data.TerminalExecution{
				{ExecutionID: "exec-1", Command: "go test", FinishedAt: "2026-01-01T00:00:00Z"},
			},
			Runtime: data.SessionRuntime{Command: "claude"},
		},
	}
	known := protocol.SessionDeltaKnown{
		LogEntryCount:          1,
		TerminalExecutionCount: 1,
	}
	got := SessionDeltaEventFromRecord(record, known, DeltaCursorSnapshot{}, false)
	if len(got.AppendLogEntries) != 1 || got.AppendLogEntries[0].Phase != "finished" {
		t.Fatalf("expected appended finished log, got %+v", got.AppendLogEntries)
	}
	if len(got.TerminalExecutions) != 0 {
		t.Fatalf("expected terminal execution details to stay lazy-loaded, got %+v", got.TerminalExecutions)
	}
}

func TestSessionDeltaEventFromRecord_IncludesRunningExecutionsWhenTerminalOutputChanges(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			RawTerminalByStream: map[string]string{"stdout": "new output"},
			TerminalExecutions: []data.TerminalExecution{
				{ExecutionID: "exec-1", Command: "first"},
				{ExecutionID: "exec-2", Command: "second"},
			},
			Runtime: data.SessionRuntime{Command: "claude"},
		},
	}
	known := protocol.SessionDeltaKnown{
		TerminalExecutionCount: 2,
		TerminalStdoutLength:   3,
	}
	got := SessionDeltaEventFromRecord(record, known, DeltaCursorSnapshot{}, false)
	if got.RequiresFullSync {
		t.Fatalf("expected incremental terminal output delta, got full sync")
	}
	if len(got.TerminalExecutions) != 0 || len(got.RawTerminalByStream) != 0 {
		t.Fatalf("expected terminal payloads to stay lazy-loaded, executions=%+v raw=%+v", got.TerminalExecutions, got.RawTerminalByStream)
	}
}

func TestSessionDeltaEventFromRecord_KnownTerminalExecutionExceedsLatestDoesNotFullSync(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			TerminalExecutions: []data.TerminalExecution{
				{ExecutionID: "exec-1", Command: "first"},
			},
			Runtime: data.SessionRuntime{Command: "claude"},
		},
	}
	known := protocol.SessionDeltaKnown{TerminalExecutionCount: 100}
	got := SessionDeltaEventFromRecord(record, known, DeltaCursorSnapshot{}, false)
	if got.RequiresFullSync {
		t.Fatalf("terminal execution mismatches should be resolved through lazy execution page hydration")
	}
	if len(got.TerminalExecutions) != 0 {
		t.Fatalf("expected invalid delta base to avoid full terminal execution delivery, got %+v", got.TerminalExecutions)
	}
}

func TestSessionTerminalRangeEventFromRecordWithPayloadLimitReturnsRange(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			RawTerminalByStream: map[string]string{"stdout": "0123456789"},
			Runtime:             data.SessionRuntime{Command: "claude"},
		},
	}

	got, err := SessionTerminalRangeEventFromRecordWithPayloadLimit(record, "stdout", 2, 4, DeltaCursorSnapshot{LatestCursor: 7}, 0)
	if err != nil {
		t.Fatalf("terminal range: %v", err)
	}
	if got.Stream != "stdout" || got.Start != 2 || got.End != 6 || got.Total != 10 || got.Content != "2345" {
		t.Fatalf("unexpected terminal range: %+v", got)
	}
	if got.Latest.EventCursor != 7 || got.Latest.TerminalStdoutLength != 10 {
		t.Fatalf("latest metadata not preserved: %+v", got.Latest)
	}
}

func TestSessionTerminalRangeEventFromRecordWithPayloadLimitKeepsUTF8Boundary(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			RawTerminalByStream: map[string]string{"stdout": "a你好b"},
			Runtime:             data.SessionRuntime{Command: "claude"},
		},
	}

	got, err := SessionTerminalRangeEventFromRecordWithPayloadLimit(record, "stdout", 1, 4, DeltaCursorSnapshot{}, 0)
	if err != nil {
		t.Fatalf("terminal range: %v", err)
	}
	if got.Start != 1 || got.End != 4 || got.Total != len("a你好b") || got.Content != "你" {
		t.Fatalf("unexpected utf8-safe terminal range: %+v", got)
	}
	got, err = SessionTerminalRangeEventFromRecordWithPayloadLimit(record, "stdout", 1, 1, DeltaCursorSnapshot{}, 0)
	if err != nil {
		t.Fatalf("terminal range with short limit: %v", err)
	}
	if got.Start != 1 || got.End != 4 || got.Content != "你" {
		t.Fatalf("expected short limit to expand to one utf8 rune, got %+v", got)
	}
}

func TestSessionTerminalRangeEventFromRecordWithPayloadLimitRejectsInvalidStream(t *testing.T) {
	record := data.SessionRecord{Summary: data.SessionSummary{ID: "s1"}}
	if _, err := SessionTerminalRangeEventFromRecordWithPayloadLimit(record, "stdin", 0, 1, DeltaCursorSnapshot{}, 0); err == nil {
		t.Fatal("expected invalid stream error")
	}
}

func TestSessionDiffPageEventFromRecordWithPayloadLimitReturnsBoundedPage(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			Diffs: []DiffContext{
				{ContextID: "diff-1", Path: "a.go", Diff: "a"},
				{ContextID: "diff-2", Path: "b.go", Diff: "b", GroupID: "g"},
				{ContextID: "diff-3", Path: "c.go", Diff: "c", GroupID: "g"},
			},
			CurrentDiff: &DiffContext{ContextID: "diff-3", Path: "c.go", Diff: "c", GroupID: "g"},
			Runtime:     data.SessionRuntime{Command: "claude"},
		},
	}

	got := SessionDiffPageEventFromRecordWithPayloadLimit(record, 3, 2, DeltaCursorSnapshot{LatestCursor: 8}, 0)
	if got.DiffStart != 1 || got.DiffTotal != 3 || !got.HasMoreBefore {
		t.Fatalf("unexpected diff page metadata: %+v", got)
	}
	if len(got.Diffs) != 2 || got.Diffs[0].ID != "diff-2" || got.Diffs[1].ID != "diff-3" {
		t.Fatalf("unexpected diff page entries: %+v", got.Diffs)
	}
	if got.CurrentDiff == nil || got.CurrentDiff.ID != "diff-3" {
		t.Fatalf("expected current diff on page, got %+v", got.CurrentDiff)
	}
	if got.Latest.EventCursor != 8 || got.Latest.DiffCount != 3 {
		t.Fatalf("latest metadata not preserved: %+v", got.Latest)
	}
}

func TestSessionTerminalExecutionPageEventFromRecordWithPayloadLimitStripsOutputByDefault(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			TerminalExecutions: []data.TerminalExecution{
				{ExecutionID: "exec-1", Command: "first", Stdout: "old"},
				{ExecutionID: "exec-2", Command: "second", Stdout: "new"},
			},
			Runtime: data.SessionRuntime{Command: "claude"},
		},
	}

	got := SessionTerminalExecutionPageEventFromRecordWithPayloadLimit(record, 2, 1, false, DeltaCursorSnapshot{LatestCursor: 9}, 0)
	if got.ExecutionStart != 1 || got.ExecutionTotal != 2 || !got.HasMoreBefore {
		t.Fatalf("unexpected execution page metadata: %+v", got)
	}
	if len(got.TerminalExecutions) != 1 || got.TerminalExecutions[0].ExecutionID != "exec-2" {
		t.Fatalf("unexpected execution page entries: %+v", got.TerminalExecutions)
	}
	if got.TerminalExecutions[0].Stdout != "" {
		t.Fatalf("expected output stripped by default, got %+v", got.TerminalExecutions[0])
	}
	if got.Latest.EventCursor != 9 || got.Latest.TerminalExecutionCount != 2 {
		t.Fatalf("latest metadata not preserved: %+v", got.Latest)
	}
}

func TestRestoredAgentStateEventFromRecord_Idle_NoResume(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			Controller: data.ControllerSnapshot{State: ControllerStateIdle},
			Runtime:    data.SessionRuntime{},
		},
	}
	got := RestoredAgentStateEventFromRecord(record, false, false)
	// Idle + no resume + no lifecycle -> nil
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestRestoredAgentStateEventFromRecord_IdleWithResume(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			Controller: data.ControllerSnapshot{State: ControllerStateIdle},
			Runtime:    data.SessionRuntime{ResumeSessionID: "r1"},
		},
	}
	got := RestoredAgentStateEventFromRecord(record, false, false)
	if got == nil {
		t.Fatal("expected event")
	}
	if got.State != string(ControllerStateIdle) {
		t.Errorf("state: %q", got.State)
	}
	// 注入 lifecycle
	if got.RuntimeMeta.ClaudeLifecycle != "waiting_input" {
		t.Errorf("expected lifecycle filled to waiting_input, got %q", got.RuntimeMeta.ClaudeLifecycle)
	}
}

func TestRestoredAgentStateEventFromRecord_DowngradeStaleRunning(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			Controller: data.ControllerSnapshot{State: ControllerStateThinking},
			Runtime:    data.SessionRuntime{}, // 没有 resume -> downgrade to Idle (return nil because Idle without resume)
		},
	}
	got := RestoredAgentStateEventFromRecord(record, false, false)
	if got != nil {
		t.Errorf("expected nil after downgrade with no resume, got %+v", got)
	}
}

func TestRestoredAgentStateEventFromRecord_ThinkingWithResumeBecomesWaitInput(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			Controller: data.ControllerSnapshot{State: ControllerStateThinking, LastStep: "step"},
			Runtime:    data.SessionRuntime{ResumeSessionID: "r1"},
		},
	}
	got := RestoredAgentStateEventFromRecord(record, false, false)
	if got == nil {
		t.Fatal("expected event")
	}
	if got.State != string(ControllerStateWaitInput) {
		t.Errorf("expected WAIT_INPUT downgrade, got %q", got.State)
	}
	if !got.AwaitInput {
		t.Errorf("expected awaitInput true")
	}
}

func TestRestoredAgentStateEventFromRecord_HasActiveRunnerKeepsThinking(t *testing.T) {
	record := data.SessionRecord{
		Summary: data.SessionSummary{ID: "s1"},
		Projection: data.ProjectionSnapshot{
			Controller: data.ControllerSnapshot{State: ControllerStateThinking, LastStep: "step"},
		},
	}
	got := RestoredAgentStateEventFromRecord(record, true, false)
	if got == nil {
		t.Fatal("expected event")
	}
	if got.State != string(ControllerStateThinking) {
		t.Errorf("expected THINKING preserved when runner active, got %q", got.State)
	}
}

func TestRestoredAgentStateEventFromRecord_DerivesStateFromLifecycle(t *testing.T) {
	cases := []struct {
		lifecycle string
		resume    string
		want      ControllerState
	}{
		// "starting" + resume id 会被 NormalizeProjectionLifecycle 规整成 "resumable"
		{"waiting_input", "r1", ControllerStateWaitInput},
		{"active", "r1", ControllerStateThinking},
		{"resumable", "r1", ControllerStateIdle},
		{"starting", "", ControllerStateThinking}, // 无 resume 才保持 starting
	}
	for _, tc := range cases {
		t.Run(tc.lifecycle, func(t *testing.T) {
			record := data.SessionRecord{
				Summary: data.SessionSummary{ID: "s1"},
				Projection: data.ProjectionSnapshot{
					Runtime: data.SessionRuntime{ClaudeLifecycle: tc.lifecycle, ResumeSessionID: tc.resume},
				},
			}
			got := RestoredAgentStateEventFromRecord(record, true, false)
			if got == nil {
				t.Fatal("expected event")
			}
			if got.State != string(tc.want) {
				t.Errorf("expected %q, got %q", tc.want, got.State)
			}
		})
	}
}
