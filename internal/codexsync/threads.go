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

	"mobilevc/internal/protocol"
	"mobilevc/internal/session"
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
	RolloutPath      string
	HistoryPrompts   []NativePrompt
	LogEntries       []store.SnapshotLogEntry
	ControllerState  session.ControllerState
	ClaudeLifecycle  string
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

type rolloutEnvelope struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type rolloutEventPayload struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type nativeRolloutSnapshot struct {
	LogEntries      []store.SnapshotLogEntry
	ControllerState session.ControllerState
	ClaudeLifecycle string
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
		if rollout, err := loadRollout(thread.RolloutPath); err == nil {
			thread.LogEntries = rollout.LogEntries
			thread.ControllerState = rollout.ControllerState
			thread.ClaudeLifecycle = rollout.ClaudeLifecycle
		}
		if !isMeaningfulPromptText(thread.Title) {
			thread.Title = latestMeaningfulPrompt(thread.HistoryPrompts)
		}
		if !isMeaningfulPromptText(thread.Title) {
			thread.Title = latestMeaningfulNativeLogText(thread.LogEntries)
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
		title = latestMeaningfulNativeLogText(thread.LogEntries)
	}
	if !isMeaningfulPromptText(title) {
		title = strings.TrimSpace(thread.FirstUserMessage)
	}
	if !isMeaningfulPromptText(title) {
		title = "Codex 会话"
	}
	preview := latestMeaningfulNativeLogText(thread.LogEntries)
	if !isMeaningfulPromptText(preview) {
		preview = latestMeaningfulPrompt(thread.HistoryPrompts)
	}
	if !isMeaningfulPromptText(preview) {
		preview = strings.TrimSpace(thread.FirstUserMessage)
	}
	if !isMeaningfulPromptText(preview) {
		preview = title
	}
	entries := append([]store.SnapshotLogEntry(nil), thread.LogEntries...)
	if len(entries) == 0 {
		entries = buildPromptLogEntries(thread.HistoryPrompts)
	}
	lifecycle := strings.TrimSpace(thread.ClaudeLifecycle)
	if lifecycle == "" {
		lifecycle = "resumable"
	}
	runtime := store.SessionRuntime{
		ResumeSessionID: thread.ThreadID,
		Command:         "codex",
		Engine:          "codex",
		CWD:             thread.CWD,
		ClaudeLifecycle: lifecycle,
		Source:          "codex-native",
	}
	controllerState := thread.ControllerState
	if controllerState == "" {
		controllerState = controllerStateFromLifecycle(lifecycle)
	}
	controller := session.ControllerSnapshot{
		SessionID:       MirrorSessionID(thread.ThreadID),
		State:           controllerState,
		CurrentCommand:  "codex",
		ResumeSession:   thread.ThreadID,
		ClaudeLifecycle: lifecycle,
		ActiveMeta: protocol.RuntimeMeta{
			ResumeSessionID: thread.ThreadID,
			Command:         "codex",
			Engine:          "codex",
			Model:           thread.Model,
			CWD:             thread.CWD,
			ClaudeLifecycle: lifecycle,
		},
	}
	projection := store.ProjectionSnapshot{
		LogEntries:          entries,
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		Controller:          controller,
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
	queries := []string{
		"select id, cwd, title, coalesce(model,''), coalesce(source,''), coalesce(model_provider,''), created_at, updated_at, coalesce(first_user_message,''), coalesce(rollout_path,'') from threads where archived = 0 order by updated_at desc;",
		"select id, cwd, title, coalesce(model,''), coalesce(source,''), coalesce(model_provider,''), created_at, updated_at, coalesce(first_user_message,'') from threads where archived = 0 order by updated_at desc;",
	}
	var (
		output []byte
		err    error
	)
	for idx, query := range queries {
		cmd := exec.CommandContext(ctx, "sqlite3", "-separator", "\t", dbPath, query)
		output, err = cmd.CombinedOutput()
		if err == nil {
			break
		}
		if idx == 0 && strings.Contains(string(output), "no such column: rollout_path") {
			continue
		}
		return nil, fmt.Errorf("query codex threads failed: %w (%s)", err, strings.TrimSpace(string(output)))
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
		rolloutPath := ""
		if len(parts) > 9 {
			rolloutPath = strings.TrimSpace(parts[9])
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
			RolloutPath:      rolloutPath,
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

func loadRollout(path string) (nativeRolloutSnapshot, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nativeRolloutSnapshot{}, nil
	}
	file, err := os.Open(trimmed)
	if err != nil {
		if os.IsNotExist(err) {
			return nativeRolloutSnapshot{}, nil
		}
		return nativeRolloutSnapshot{}, fmt.Errorf("open codex rollout failed: %w", err)
	}
	defer file.Close()

	snapshot := nativeRolloutSnapshot{
		ControllerState: session.ControllerStateIdle,
		ClaudeLifecycle: "resumable",
	}
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)
	taskOpen := false
	for scanner.Scan() {
		var line rolloutEnvelope
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if strings.TrimSpace(line.Type) != "event_msg" {
			continue
		}
		var payload rolloutEventPayload
		if err := json.Unmarshal(line.Payload, &payload); err != nil {
			continue
		}
		timestamp := normalizeRolloutTimestamp(line.Timestamp)
		switch strings.TrimSpace(payload.Type) {
		case "task_started":
			taskOpen = true
			snapshot.ControllerState = session.ControllerStateThinking
			snapshot.ClaudeLifecycle = "active"
		case "task_complete", "turn_aborted":
			taskOpen = false
			snapshot.ControllerState = session.ControllerStateIdle
			snapshot.ClaudeLifecycle = "resumable"
		case "user_message":
			message := strings.TrimSpace(payload.Message)
			if !isMeaningfulPromptText(message) {
				continue
			}
			snapshot.LogEntries = append(snapshot.LogEntries, store.SnapshotLogEntry{
				Kind:      "user",
				Label:     "历史输入",
				Message:   message,
				Text:      message,
				Timestamp: timestamp,
			})
		case "agent_message":
			message := strings.TrimSpace(payload.Message)
			if message == "" {
				continue
			}
			snapshot.LogEntries = append(snapshot.LogEntries, store.SnapshotLogEntry{
				Kind:      "markdown",
				Message:   message,
				Text:      message,
				Timestamp: timestamp,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nativeRolloutSnapshot{}, fmt.Errorf("scan codex rollout failed: %w", err)
	}
	if taskOpen {
		snapshot.ControllerState = session.ControllerStateThinking
		snapshot.ClaudeLifecycle = "active"
	}
	return snapshot, nil
}

func normalizeRolloutTimestamp(value string) string {
	parsed := strings.TrimSpace(value)
	if parsed == "" {
		return time.Now().UTC().Format(time.RFC3339)
	}
	if ts, err := time.Parse(time.RFC3339Nano, parsed); err == nil {
		return ts.UTC().Format(time.RFC3339)
	}
	return parsed
}

func normalizePath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	absPath, err := filepath.Abs(trimmed)
	if err == nil {
		trimmed = absPath
	}
	if resolved, err := filepath.EvalSymlinks(trimmed); err == nil && strings.TrimSpace(resolved) != "" {
		trimmed = resolved
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

func latestMeaningfulNativeLogText(entries []store.SnapshotLogEntry) string {
	for i := len(entries) - 1; i >= 0; i-- {
		text := strings.TrimSpace(firstNonEmpty(entries[i].Text, entries[i].Message))
		if text == "" {
			continue
		}
		if entries[i].Kind == "user" && !isMeaningfulPromptText(text) {
			continue
		}
		return text
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

func buildPromptLogEntries(items []NativePrompt) []store.SnapshotLogEntry {
	entries := make([]store.SnapshotLogEntry, 0, len(items))
	for _, item := range items {
		entries = append(entries, store.SnapshotLogEntry{
			Kind:      "user",
			Label:     "历史输入",
			Message:   item.Text,
			Text:      item.Text,
			Timestamp: item.Timestamp.UTC().Format(time.RFC3339),
		})
	}
	return entries
}

func controllerStateFromLifecycle(lifecycle string) session.ControllerState {
	switch strings.TrimSpace(lifecycle) {
	case "waiting_input":
		return session.ControllerStateWaitInput
	case "starting", "active":
		return session.ControllerStateThinking
	case "resumable":
		return session.ControllerStateIdle
	default:
		return session.ControllerStateIdle
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Now().UTC()
}
