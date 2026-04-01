package codexsync

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"mobilevc/internal/store"
)

const mirrorPrefix = "codex-thread:"

type NativeThread struct {
	ThreadID         string
	MirrorSessionID  string
	Title            string
	CWD              string
	Model            string
	Source           string
	ModelProvider    string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	FirstUserMessage string
	HistoryPrompts   []NativePrompt
}

type NativePrompt struct {
	Text      string
	Timestamp time.Time
}

type historyLine struct {
	SessionID string `json:"session_id"`
	TS        int64  `json:"ts"`
	Text      string `json:"text"`
}

func MirrorSessionID(threadID string) string {
	return mirrorPrefix + strings.TrimSpace(threadID)
}

func IsMirrorSessionID(sessionID string) bool {
	return strings.HasPrefix(strings.TrimSpace(sessionID), mirrorPrefix)
}

func ThreadIDFromMirror(sessionID string) string {
	return strings.TrimPrefix(strings.TrimSpace(sessionID), mirrorPrefix)
}

func ListNativeThreads(ctx context.Context, cwdFilter string) ([]NativeThread, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir failed: %w", err)
	}
	dbPath := filepath.Join(home, ".codex", "state_5.sqlite")
	historyPath := filepath.Join(home, ".codex", "history.jsonl")
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return []NativeThread{}, nil
		}
		return nil, fmt.Errorf("stat codex sqlite failed: %w", err)
	}

	threads, err := queryThreads(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	if len(threads) == 0 {
		return []NativeThread{}, nil
	}
	prompts, err := loadHistory(historyPath)
	if err != nil {
		return nil, err
	}

	normalizedFilter := normalizePath(cwdFilter)
	result := make([]NativeThread, 0, len(threads))
	for _, thread := range threads {
		if normalizedFilter != "" && normalizePath(thread.CWD) != normalizedFilter {
			continue
		}
		thread.MirrorSessionID = MirrorSessionID(thread.ThreadID)
		if items, ok := prompts[thread.ThreadID]; ok {
			thread.HistoryPrompts = items
		}
		if !isMeaningfulPromptText(thread.Title) {
			thread.Title = latestMeaningfulPrompt(thread.HistoryPrompts)
		}
		if !isMeaningfulPromptText(thread.Title) {
			thread.Title = strings.TrimSpace(thread.FirstUserMessage)
		}
		if !isMeaningfulPromptText(thread.Title) {
			thread.Title = "Codex 会话"
		}
		result = append(result, thread)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result, nil
}

func FindNativeThread(ctx context.Context, sessionID string) (NativeThread, error) {
	threadID := strings.TrimSpace(sessionID)
	if IsMirrorSessionID(threadID) {
		threadID = ThreadIDFromMirror(threadID)
	}
	if threadID == "" {
		return NativeThread{}, fmt.Errorf("empty codex thread id")
	}
	threads, err := ListNativeThreads(ctx, "")
	if err != nil {
		return NativeThread{}, err
	}
	for _, thread := range threads {
		if thread.ThreadID == threadID {
			return thread, nil
		}
	}
	return NativeThread{}, fmt.Errorf("codex thread not found: %s", threadID)
}

func MirrorRecord(thread NativeThread) store.SessionRecord {
	title := strings.TrimSpace(thread.Title)
	if !isMeaningfulPromptText(title) {
		title = latestMeaningfulPrompt(thread.HistoryPrompts)
	}
	if !isMeaningfulPromptText(title) {
		title = strings.TrimSpace(thread.FirstUserMessage)
	}
	if !isMeaningfulPromptText(title) {
		title = "Codex 会话"
	}
	preview := latestMeaningfulPrompt(thread.HistoryPrompts)
	if !isMeaningfulPromptText(preview) {
		preview = strings.TrimSpace(thread.FirstUserMessage)
	}
	if !isMeaningfulPromptText(preview) {
		preview = title
	}
	entries := make([]store.SnapshotLogEntry, 0, len(thread.HistoryPrompts))
	for _, item := range thread.HistoryPrompts {
		entries = append(entries, store.SnapshotLogEntry{
			Kind:      "user",
			Label:     "历史输入",
			Message:   item.Text,
			Text:      item.Text,
			Timestamp: item.Timestamp.UTC().Format(time.RFC3339),
		})
	}
	runtime := store.SessionRuntime{
		ResumeSessionID: thread.ThreadID,
		Command:         "codex",
		Engine:          "codex",
		CWD:             thread.CWD,
		ClaudeLifecycle: "resumable",
		Source:          "codex-native",
	}
	projection := store.ProjectionSnapshot{
		LogEntries:          entries,
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		Runtime:             runtime,
	}
	return store.SessionRecord{
		Summary: store.SessionSummary{
			ID:          MirrorSessionID(thread.ThreadID),
			Title:       title,
			CreatedAt:   nonZeroTime(thread.CreatedAt, thread.UpdatedAt, time.Now().UTC()),
			UpdatedAt:   nonZeroTime(thread.UpdatedAt, thread.CreatedAt, time.Now().UTC()),
			LastPreview: preview,
			EntryCount:  len(entries),
			Runtime:     runtime,
			Source:      "codex-native",
			External:    true,
		},
		Projection: projection,
	}
}

func queryThreads(ctx context.Context, dbPath string) ([]NativeThread, error) {
	query := "select id, cwd, title, coalesce(model,''), coalesce(source,''), coalesce(model_provider,''), created_at, updated_at, coalesce(first_user_message,'') from threads where archived = 0 order by updated_at desc;"
	cmd := exec.CommandContext(ctx, "sqlite3", "-separator", "\t", dbPath, query)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("query codex threads failed: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	items := make([]NativeThread, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 9 {
			continue
		}
		items = append(items, NativeThread{
			ThreadID:         strings.TrimSpace(parts[0]),
			CWD:              strings.TrimSpace(parts[1]),
			Title:            strings.TrimSpace(parts[2]),
			Model:            strings.TrimSpace(parts[3]),
			Source:           strings.TrimSpace(parts[4]),
			ModelProvider:    strings.TrimSpace(parts[5]),
			CreatedAt:        unixTime(parts[6]),
			UpdatedAt:        unixTime(parts[7]),
			FirstUserMessage: strings.TrimSpace(parts[8]),
		})
	}
	return items, nil
}

func loadHistory(path string) (map[string][]NativePrompt, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]NativePrompt{}, nil
		}
		return nil, fmt.Errorf("open codex history failed: %w", err)
	}
	defer file.Close()

	items := map[string][]NativePrompt{}
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		var line historyLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		sessionID := strings.TrimSpace(line.SessionID)
		text := strings.TrimSpace(line.Text)
		if sessionID == "" || text == "" {
			continue
		}
		items[sessionID] = append(items[sessionID], NativePrompt{Text: text, Timestamp: time.Unix(line.TS, 0).UTC()})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan codex history failed: %w", err)
	}
	return items, nil
}

func unixTime(value string) time.Time {
	parsed := strings.TrimSpace(value)
	if parsed == "" {
		return time.Time{}
	}
	seconds, err := strconv.ParseInt(parsed, 10, 64)
	if err == nil {
		return time.Unix(seconds, 0).UTC()
	}
	return time.Time{}
}

func normalizePath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	cleaned := filepath.Clean(trimmed)
	return strings.TrimSuffix(cleaned, string(filepath.Separator))
}

func latestMeaningfulPrompt(items []NativePrompt) string {
	for i := len(items) - 1; i >= 0; i-- {
		text := strings.TrimSpace(items[i].Text)
		if isMeaningfulPromptText(text) {
			return text
		}
	}
	return ""
}

func isMeaningfulPromptText(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if lower == "session" ||
		lower == "new session" ||
		lower == "command started" ||
		lower == "command finished" ||
		strings.HasPrefix(lower, "command finished ") ||
		strings.HasPrefix(lower, "--config ") ||
		strings.HasPrefix(lower, "model_reasoning_effort=") {
		return false
	}
	if strings.HasPrefix(lower, "codex ") || lower == "codex" {
		if strings.Contains(lower, "gpt-") ||
			strings.Contains(lower, "sonnet") ||
			strings.Contains(lower, "opus") ||
			strings.HasSuffix(lower, "-low") ||
			strings.HasSuffix(lower, "-medium") ||
			strings.HasSuffix(lower, "-high") {
			return false
		}
	}
	return true
}

func nonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Now().UTC()
}
