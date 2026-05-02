package claudesync

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mobilevc/internal/data"
)

func TestMirrorSessionIDRoundTrip(t *testing.T) {
	in := "abc-123"
	mirror := MirrorSessionID(in)
	if !strings.HasPrefix(mirror, "claude-session:") {
		t.Errorf("expected mirror prefix, got %q", mirror)
	}
	if !IsMirrorSessionID(mirror) {
		t.Errorf("IsMirrorSessionID should return true for %q", mirror)
	}
	if IsMirrorSessionID(in) {
		t.Errorf("IsMirrorSessionID should return false for raw id %q", in)
	}
	if got := SessionIDFromMirror(mirror); got != in {
		t.Errorf("round trip lost: got %q, want %q", got, in)
	}
	if got := SessionIDFromMirror(in); got != in {
		t.Errorf("non-mirror id should pass through, got %q", got)
	}
}

func TestEncodeCWDToProjectDir(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"  ", ""},
		{"/Users/wust_lh/MobileVC", "-Users-wust-lh-MobileVC"},
		{"a/b.c", "a-b-c"},
		{"abc123", "abc123"},
	}
	for _, tc := range cases {
		if got := EncodeCWDToProjectDir(tc.in); got != tc.want {
			t.Errorf("EncodeCWDToProjectDir(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestClaudeProjectsDir_HonorsHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	got, err := ClaudeProjectsDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(tmp, ".claude", "projects")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractJSONLEvents(t *testing.T) {
	t.Run("startIndex past end", func(t *testing.T) {
		entries := []data.SnapshotLogEntry{{Kind: "user", Message: "x"}}
		got, count := ExtractJSONLEvents(entries, 5)
		if got != nil {
			t.Errorf("expected nil events, got %+v", got)
		}
		if count != 1 {
			t.Errorf("expected count=1, got %d", count)
		}
	})
	t.Run("user and markdown picked", func(t *testing.T) {
		entries := []data.SnapshotLogEntry{
			{Kind: "user", Message: "hi", Timestamp: "t1"},
			{Kind: "markdown", Message: "reply", Timestamp: "t2"},
			{Kind: "system", Message: "ignored"},
			{Kind: "user", Text: "from text"},
			{Kind: "markdown", Message: "  "},
		}
		got, count := ExtractJSONLEvents(entries, 0)
		if count != 5 {
			t.Errorf("count: %d", count)
		}
		if len(got) != 3 {
			t.Fatalf("expected 3 events, got %d: %+v", len(got), got)
		}
		if got[0].Type != "user" || got[0].Text != "hi" || got[0].Timestamp != "t1" {
			t.Errorf("event 0: %+v", got[0])
		}
		if got[1].Type != "assistant" || got[1].Text != "reply" {
			t.Errorf("event 1: %+v", got[1])
		}
		if got[2].Type != "user" || got[2].Text != "from text" {
			t.Errorf("event 2 (text fallback): %+v", got[2])
		}
	})
	t.Run("startIndex skips earlier", func(t *testing.T) {
		entries := []data.SnapshotLogEntry{
			{Kind: "user", Message: "a"},
			{Kind: "user", Message: "b"},
		}
		got, _ := ExtractJSONLEvents(entries, 1)
		if len(got) != 1 || got[0].Text != "b" {
			t.Errorf("got %+v", got)
		}
	})
}

func TestEntryDedupKey(t *testing.T) {
	cases := []struct {
		entry data.SnapshotLogEntry
		want  string
	}{
		{data.SnapshotLogEntry{Kind: "user", Message: "  hi   world  "}, "user:hi world"},
		{data.SnapshotLogEntry{Kind: "markdown", Text: "fallback"}, "markdown:fallback"},
		{data.SnapshotLogEntry{Kind: "user", Message: ""}, ""},
		{data.SnapshotLogEntry{Kind: "user", Message: "  "}, ""},
	}
	for i, tc := range cases {
		if got := entryDedupKey(tc.entry); got != tc.want {
			t.Errorf("[%d] got %q, want %q", i, got, tc.want)
		}
	}
}

// 写一个 user + assistant 的 JSONL 文件，再用 parseSessionFromFile 解析回来。
func writeFixtureJSONL(t *testing.T, dir, sessionUUID, cwd string, events []map[string]any) string {
	t.Helper()
	path := filepath.Join(dir, sessionUUID+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for _, ev := range events {
		if _, ok := ev["sessionId"]; !ok {
			ev["sessionId"] = sessionUUID
		}
		if _, ok := ev["cwd"]; !ok {
			ev["cwd"] = cwd
		}
		if err := enc.Encode(ev); err != nil {
			f.Close()
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseSessionFromFile(t *testing.T) {
	tmp := t.TempDir()
	cwd := "/Users/x"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	events := []map[string]any{
		{
			"type":      "user",
			"timestamp": now,
			"message":   map[string]any{"role": "user", "content": "你好"},
		},
		{
			"type":      "assistant",
			"timestamp": now,
			"message": map[string]any{
				"content": []map[string]any{{"type": "text", "text": "你好，张三"}},
			},
		},
	}
	path := writeFixtureJSONL(t, tmp, "uuid-1", cwd, events)
	sess, err := parseSessionFromFile(path, "uuid-1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.SessionID != "uuid-1" {
		t.Errorf("session id: %q", sess.SessionID)
	}
	if sess.CWD != cwd {
		t.Errorf("cwd: %q", sess.CWD)
	}
	if sess.Title != "你好" {
		t.Errorf("title: %q", sess.Title)
	}
	if sess.FirstUserMessage != "你好" {
		t.Errorf("first user: %q", sess.FirstUserMessage)
	}
	if sess.LastAssistantText != "你好，张三" {
		t.Errorf("last assistant: %q", sess.LastAssistantText)
	}
	if len(sess.LogEntries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(sess.LogEntries))
	}
	if sess.LogEntries[0].Kind != "user" || sess.LogEntries[1].Kind != "markdown" {
		t.Errorf("entries kinds: %+v", sess.LogEntries)
	}
}

func TestParseSessionFromFile_BadJSONLinesSkipped(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "bad.jsonl")
	if err := os.WriteFile(path, []byte("not json\n{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":\"hi\"}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess, err := parseSessionFromFile(path, "uuid")
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.LogEntries) != 1 {
		t.Errorf("expected 1 valid entry, got %d", len(sess.LogEntries))
	}
}

func TestExtractUserMessageText(t *testing.T) {
	t.Run("string content", func(t *testing.T) {
		raw := json.RawMessage(`{"role":"user","content":"hello"}`)
		if got := extractUserMessageText(raw); got != "hello" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("block content", func(t *testing.T) {
		raw := json.RawMessage(`{"role":"user","content":[{"type":"text","text":"  hi  "}]}`)
		if got := extractUserMessageText(raw); got != "hi" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("empty raw", func(t *testing.T) {
		if got := extractUserMessageText(nil); got != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("malformed", func(t *testing.T) {
		raw := json.RawMessage(`{"bad`)
		if got := extractUserMessageText(raw); got != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("non-text block returns empty", func(t *testing.T) {
		raw := json.RawMessage(`{"content":[{"type":"image","text":""}]}`)
		if got := extractUserMessageText(raw); got != "" {
			t.Errorf("got %q", got)
		}
	})
}

func TestExtractAssistantText(t *testing.T) {
	raw := json.RawMessage(`{"content":[{"type":"text","text":"reply"}]}`)
	if got := extractAssistantText(raw); got != "reply" {
		t.Errorf("got %q", got)
	}
	if got := extractAssistantText(nil); got != "" {
		t.Errorf("expected empty for nil, got %q", got)
	}
	if got := extractAssistantText(json.RawMessage(`{"content":"no blocks"}`)); got != "" {
		t.Errorf("expected empty when content is not block array, got %q", got)
	}
}

func TestNormalizeJSONLTimestamp(t *testing.T) {
	t.Run("empty -> now", func(t *testing.T) {
		got := normalizeJSONLTimestamp("")
		if _, err := time.Parse(time.RFC3339, got); err != nil {
			t.Errorf("expected RFC3339, got %q", got)
		}
	})
	t.Run("RFC3339Nano normalized to RFC3339", func(t *testing.T) {
		in := "2026-01-01T12:34:56.789Z"
		got := normalizeJSONLTimestamp(in)
		if _, err := time.Parse(time.RFC3339, got); err != nil {
			t.Errorf("expected parseable RFC3339, got %q", got)
		}
	})
	t.Run("malformed kept as-is", func(t *testing.T) {
		in := "not-a-timestamp"
		if got := normalizeJSONLTimestamp(in); got != in {
			t.Errorf("got %q", got)
		}
	})
}

func TestIsMeaningfulPromptText(t *testing.T) {
	yes := []string{"hello", "  实际内容  ", "session start things"}
	no := []string{"", "  ", "session", "Session", "new session", "command started", "command finished", "command finished foo"}
	for _, s := range yes {
		if !isMeaningfulPromptText(s) {
			t.Errorf("expected meaningful for %q", s)
		}
	}
	for _, s := range no {
		if isMeaningfulPromptText(s) {
			t.Errorf("expected NOT meaningful for %q", s)
		}
	}
}

func TestNonZeroTime(t *testing.T) {
	zero := time.Time{}
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if got := nonZeroTime(zero, t1, t2); !got.Equal(t1) {
		t.Errorf("expected first non-zero, got %v", got)
	}
	// 没有任何 non-zero 时返回当前时间(非零)
	got := nonZeroTime()
	if got.IsZero() {
		t.Errorf("expected fallback to non-zero now(), got zero")
	}
}

func TestReadFirstSessionID(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "x.jsonl")
	body := `{"type":"user","sessionId":"sid-from-line","message":{"role":"user","content":"hi"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readFirstSessionID(path); got != "sid-from-line" {
		t.Errorf("got %q", got)
	}
	if got := readFirstSessionID(filepath.Join(tmp, "missing")); got != "" {
		t.Errorf("expected empty for missing file, got %q", got)
	}
}

// --- 集成: WriteSessionToJSONL → parseSessionFromFile 来回 ---

func TestWriteSessionToJSONL_AndReadBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := "/Users/test/Project"
	uuid := "uuid-x"

	events := []JSONLEvent{
		{Type: "user", Text: "你好"},
		{Type: "assistant", Text: "你好回答"},
	}
	if err := WriteSessionToJSONL(cwd, uuid, events); err != nil {
		t.Fatal(err)
	}

	encoded := EncodeCWDToProjectDir(cwd)
	expectedPath := filepath.Join(home, ".claude", "projects", encoded, uuid+".jsonl")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("file not found: %v", err)
	}
	sess, err := parseSessionFromFile(expectedPath, uuid)
	if err != nil {
		t.Fatal(err)
	}
	if sess.FirstUserMessage != "你好" {
		t.Errorf("first user: %q", sess.FirstUserMessage)
	}
	if sess.LastAssistantText != "你好回答" {
		t.Errorf("last assistant: %q", sess.LastAssistantText)
	}
	if len(sess.LogEntries) != 2 {
		t.Errorf("entries: %+v", sess.LogEntries)
	}
}

func TestWriteSessionToJSONL_AppendsAndChainsParent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := "/Users/test/Project"
	uuid := "uuid-y"

	if err := WriteSessionToJSONL(cwd, uuid, []JSONLEvent{{Type: "user", Text: "first"}}); err != nil {
		t.Fatal(err)
	}
	if err := WriteSessionToJSONL(cwd, uuid, []JSONLEvent{{Type: "assistant", Text: "second"}}); err != nil {
		t.Fatal(err)
	}

	// 读取文件的所有行，验证行数 == 2
	encoded := EncodeCWDToProjectDir(cwd)
	path := filepath.Join(home, ".claude", "projects", encoded, uuid+".jsonl")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	// 第一行应该 parentUuid=null, 第二行 parentUuid 应该等于第一行的 uuid
	var l1, l2 map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &l1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &l2); err != nil {
		t.Fatal(err)
	}
	if _, ok := l1["parentUuid"].(string); ok {
		t.Errorf("expected first parentUuid null, got string: %v", l1["parentUuid"])
	}
	parent2, _ := l2["parentUuid"].(string)
	uuid1, _ := l1["uuid"].(string)
	if parent2 == "" || parent2 != uuid1 {
		t.Errorf("expected second.parentUuid (%q) == first.uuid (%q)", parent2, uuid1)
	}
}

func TestWriteSessionToJSONL_NoOpInputs(t *testing.T) {
	if err := WriteSessionToJSONL("", "u", []JSONLEvent{{Type: "user", Text: "x"}}); err != nil {
		t.Errorf("expected nil err for empty cwd, got %v", err)
	}
	if err := WriteSessionToJSONL("/x", "", []JSONLEvent{{Type: "user", Text: "x"}}); err != nil {
		t.Errorf("expected nil err for empty uuid, got %v", err)
	}
	if err := WriteSessionToJSONL("/x", "u", nil); err != nil {
		t.Errorf("expected nil err for nil events, got %v", err)
	}
}

// --- ListNativeSessions / FindNativeSession 集成 ---

func TestListNativeSessionsAndFind(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := home // 用 tmp dir 作 cwd, 这样 normalizePath 不会走偏到非测试目录
	uuid := "uuid-list"
	if err := WriteSessionToJSONL(cwd, uuid, []JSONLEvent{
		{Type: "user", Text: "你好"},
		{Type: "assistant", Text: "你好答复"},
	}); err != nil {
		t.Fatal(err)
	}
	sessions, err := ListNativeSessions(context.Background(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) == 0 {
		t.Fatal("expected at least one session")
	}
	found := false
	for _, s := range sessions {
		if s.SessionID == uuid {
			found = true
			if s.MirrorSessionID != MirrorSessionID(uuid) {
				t.Errorf("mirror id: %q", s.MirrorSessionID)
			}
		}
	}
	if !found {
		t.Errorf("expected to find session %q in %+v", uuid, sessions)
	}

	// FindNativeSession by raw id
	got, err := FindNativeSession(context.Background(), uuid)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != uuid {
		t.Errorf("FindNativeSession id mismatch: %q vs %q", got.SessionID, uuid)
	}

	// FindNativeSession by mirror id
	got2, err := FindNativeSession(context.Background(), MirrorSessionID(uuid))
	if err != nil {
		t.Fatal(err)
	}
	if got2.SessionID != uuid {
		t.Errorf("FindNativeSession (mirror) id mismatch: %q vs %q", got2.SessionID, uuid)
	}

	// FindNativeSession with empty id returns error
	if _, err := FindNativeSession(context.Background(), ""); err == nil {
		t.Errorf("expected error for empty id")
	}

	// FindNativeSession with unknown id
	if _, err := FindNativeSession(context.Background(), "missing-uuid"); err == nil {
		t.Errorf("expected error for missing id")
	}
}

func TestListNativeSessions_EmptyCWDReturnsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, err := ListNativeSessions(context.Background(), "  ")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}

// --- MirrorRecord ---

func TestMirrorRecord(t *testing.T) {
	sess := NativeSession{
		SessionID:        "uuid",
		Title:            "  greeting  ",
		FirstUserMessage: "hi",
		CWD:              "/x",
		LogEntries:       []data.SnapshotLogEntry{{Kind: "user", Message: "hi"}},
	}
	rec := MirrorRecord(sess)
	if rec.Summary.ID != MirrorSessionID("uuid") {
		t.Errorf("summary id: %q", rec.Summary.ID)
	}
	if rec.Summary.Title != "greeting" {
		t.Errorf("title trimmed: %q", rec.Summary.Title)
	}
	if rec.Projection.Runtime.ResumeSessionID != "uuid" {
		t.Errorf("resume id: %q", rec.Projection.Runtime.ResumeSessionID)
	}
	if rec.Projection.Controller.SessionID != MirrorSessionID("uuid") {
		t.Errorf("controller id: %q", rec.Projection.Controller.SessionID)
	}
}

func TestMirrorRecord_FallbacksWhenTitleEmpty(t *testing.T) {
	sess := NativeSession{
		SessionID:        "uuid",
		FirstUserMessage: "原始",
	}
	rec := MirrorRecord(sess)
	if rec.Summary.Title != "原始" {
		t.Errorf("expected first user as title, got %q", rec.Summary.Title)
	}
}

func TestMirrorRecord_DefaultTitle(t *testing.T) {
	sess := NativeSession{SessionID: "uuid"}
	rec := MirrorRecord(sess)
	if rec.Summary.Title != "Claude 会话" {
		t.Errorf("expected default title, got %q", rec.Summary.Title)
	}
}

// --- MergeJSONLToSession ---

func TestMergeJSONLToSession_Empty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, count, err := MergeJSONLToSession("", "u", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil || count != 0 {
		t.Errorf("expected empty: got=%+v count=%d", got, count)
	}
}

func TestMergeJSONLToSession_FileMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, count, err := MergeJSONLToSession("/some/cwd", "missing-uuid", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil || count != 0 {
		t.Errorf("expected nil for missing file: got=%+v count=%d", got, count)
	}
}

func TestMergeJSONLToSession_MergesNewEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := "/Users/test"
	uuid := "uuid-merge"

	// 先写两条到 jsonl
	if err := WriteSessionToJSONL(cwd, uuid, []JSONLEvent{
		{Type: "user", Text: "first"},
		{Type: "assistant", Text: "second"},
	}); err != nil {
		t.Fatal(err)
	}

	// 已知的 entries 只包含第一条 user
	existing := []data.SnapshotLogEntry{{Kind: "user", Message: "first"}}
	merged, count, err := MergeJSONLToSession(cwd, uuid, existing)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 1 || merged[0].Kind != "markdown" {
		t.Errorf("expected 1 markdown to be merged, got %+v", merged)
	}
	if count != 2 {
		t.Errorf("expected count=2 (existing + new), got %d", count)
	}
}

func TestMergeJSONLToSession_NothingToMergeReturnsNil(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := "/Users/test"
	uuid := "uuid-allknown"

	if err := WriteSessionToJSONL(cwd, uuid, []JSONLEvent{{Type: "user", Text: "only"}}); err != nil {
		t.Fatal(err)
	}
	existing := []data.SnapshotLogEntry{{Kind: "user", Message: "only"}}
	merged, count, err := MergeJSONLToSession(cwd, uuid, existing)
	if err != nil {
		t.Fatal(err)
	}
	if merged != nil {
		t.Errorf("expected nil merged, got %+v", merged)
	}
	if count != 0 {
		t.Errorf("expected count=0 when no merge, got %d", count)
	}
}

func TestNormalizePath(t *testing.T) {
	if got := normalizePath(""); got != "" {
		t.Errorf("empty: %q", got)
	}
	tmp := t.TempDir()
	if got := normalizePath(tmp); got == "" {
		t.Errorf("expected non-empty, got %q for %q", got, tmp)
	}
}
