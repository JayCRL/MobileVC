package claudesync

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mobilevc/internal/protocol"
	"mobilevc/internal/session"
	"mobilevc/internal/store"
)

const mirrorPrefix = "claude-session:"

// NativeSession 描述一条来自 Claude CLI 原生 jsonl 存档的会话。
type NativeSession struct {
	SessionID         string
	MirrorSessionID   string
	Title             string
	CWD               string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	FirstUserMessage  string
	LastAssistantText string
	LogEntries        []store.SnapshotLogEntry
	JSONLFiles        []string
}

// jsonlLine 只取本次关心的字段，其余忽略。
type jsonlLine struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	CWD       string          `json:"cwd"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
}

type userMessagePayload struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type assistantContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// MirrorSessionID 在原生 sessionID 外面套上前缀，得到 mobilevc 可感知的 mirror session ID。
func MirrorSessionID(sessionID string) string {
	return mirrorPrefix + strings.TrimSpace(sessionID)
}

// IsMirrorSessionID 判断给定的 sessionID 是否由 Claude 原生会话镜像而来。
func IsMirrorSessionID(sessionID string) bool {
	return strings.HasPrefix(strings.TrimSpace(sessionID), mirrorPrefix)
}

// SessionIDFromMirror 从 mirror sessionID 中剥出原生 UUID。
func SessionIDFromMirror(sessionID string) string {
	return strings.TrimPrefix(strings.TrimSpace(sessionID), mirrorPrefix)
}

// ClaudeProjectsDir 返回 ~/.claude/projects 的绝对路径。
func ClaudeProjectsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir failed: %w", err)
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// EncodeCWDToProjectDir 复刻 Claude CLI 的目录命名规则：把 cwd 里所有非字母数字字符替换成 '-'。
// 例：/Users/wust_lh/MobileVC → -Users-wust-lh-MobileVC。
func EncodeCWDToProjectDir(cwd string) string {
	trimmed := strings.TrimSpace(cwd)
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	return b.String()
}

// ListNativeSessions 列出 cwdFilter 对应目录下所有原生 Claude 会话。
// cwdFilter 为空时不扫（遵循 codexsync 语义），避免跨项目扫描耗时。
//
// Claude CLI 自己不 resolve symlink（用 Node process.cwd() 原样编码），
// 而这里的 normalizePath 会 filepath.EvalSymlinks。在 Windows 下 EvalSymlinks
// 走 GetFinalPathNameByHandle 可能做 canonical 化（盘符大小写、\\?\ 前缀、
// junction 展开），得到的编码目录名会跟 Claude CLI 实际存的不一致。
// 这里对多个候选路径都各试一次，任一目录命中即聚合结果。
func ListNativeSessions(ctx context.Context, cwdFilter string) ([]NativeSession, error) {
	candidates := candidateProjectCWDs(cwdFilter)
	if len(candidates) == 0 {
		return []NativeSession{}, nil
	}
	projectsDir, err := ClaudeProjectsDir()
	if err != nil {
		return nil, err
	}
	seenDir := map[string]struct{}{}
	seenSession := map[string]struct{}{}
	var aggregated []NativeSession
	for _, cwd := range candidates {
		encoded := EncodeCWDToProjectDir(cwd)
		if encoded == "" {
			continue
		}
		dir := filepath.Join(projectsDir, encoded)
		if _, ok := seenDir[dir]; ok {
			continue
		}
		seenDir[dir] = struct{}{}
		info, err := os.Stat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat claude projects dir failed: %w", err)
		}
		if !info.IsDir() {
			continue
		}
		sessions, scanErr := scanDir(ctx, dir, cwd)
		if scanErr != nil {
			return nil, scanErr
		}
		for _, s := range sessions {
			if _, ok := seenSession[s.SessionID]; ok {
				continue
			}
			seenSession[s.SessionID] = struct{}{}
			aggregated = append(aggregated, s)
		}
	}
	if aggregated == nil {
		return []NativeSession{}, nil
	}
	sort.Slice(aggregated, func(i, j int) bool {
		return aggregated[i].UpdatedAt.After(aggregated[j].UpdatedAt)
	})
	return aggregated, nil
}

// candidateProjectCWDs 对同一个 cwd 产出可能的目录候选。
// Claude CLI 存储目录名基于 Node process.cwd() 原样编码，不解析 symlink，
// 但我们过去只用 normalizePath 的结果（EvalSymlinks 后）去匹配。
// 在 Windows（或存在 symlink 的场景下）两边容易不一致，这里把原样输入、
// filepath.Abs、filepath.EvalSymlinks 的结果都纳入候选。
func candidateProjectCWDs(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	out := make([]string, 0, 4)
	seen := map[string]struct{}{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		cleaned := strings.TrimSuffix(filepath.Clean(v), string(filepath.Separator))
		if cleaned == "" {
			return
		}
		if _, ok := seen[cleaned]; ok {
			return
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	add(trimmed)
	if abs, err := filepath.Abs(trimmed); err == nil {
		add(abs)
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			add(resolved)
		}
	}
	if resolved, err := filepath.EvalSymlinks(trimmed); err == nil {
		add(resolved)
	}
	return out
}

// FindNativeSession 在所有项目目录中按 sessionID 查找。
// 用于点击 mirror 会话恢复：此时调用方没有上下文 cwd，但 jsonl 里会自带 cwd 字段。
func FindNativeSession(ctx context.Context, sessionID string) (NativeSession, error) {
	targetID := strings.TrimSpace(sessionID)
	if IsMirrorSessionID(targetID) {
		targetID = SessionIDFromMirror(targetID)
	}
	if targetID == "" {
		return NativeSession{}, fmt.Errorf("empty claude session id")
	}
	projectsDir, err := ClaudeProjectsDir()
	if err != nil {
		return NativeSession{}, err
	}
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return NativeSession{}, fmt.Errorf("claude session not found: %s", targetID)
		}
		return NativeSession{}, fmt.Errorf("read claude projects dir failed: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		select {
		case <-ctx.Done():
			return NativeSession{}, ctx.Err()
		default:
		}
		sessions, scanErr := scanDir(ctx, filepath.Join(projectsDir, entry.Name()), "")
		if scanErr != nil {
			continue
		}
		for _, s := range sessions {
			if s.SessionID == targetID {
				return s, nil
			}
		}
	}
	return NativeSession{}, fmt.Errorf("claude session not found: %s", targetID)
}

// MirrorRecord 把原生 Claude 会话铺成 mobilevc 认识的 SessionRecord。
func MirrorRecord(sess NativeSession) store.SessionRecord {
	title := strings.TrimSpace(sess.Title)
	if !isMeaningfulPromptText(title) {
		title = strings.TrimSpace(sess.FirstUserMessage)
	}
	if !isMeaningfulPromptText(title) {
		title = "Claude 会话"
	}
	preview := strings.TrimSpace(sess.LastAssistantText)
	if preview == "" {
		preview = strings.TrimSpace(sess.FirstUserMessage)
	}
	if preview == "" {
		preview = title
	}
	entries := append([]store.SnapshotLogEntry(nil), sess.LogEntries...)
	runtime := store.SessionRuntime{
		ResumeSessionID: sess.SessionID,
		Command:         "claude",
		Engine:          "claude",
		CWD:             sess.CWD,
		ClaudeLifecycle: "resumable",
		Source:          "claude-native",
	}
	controller := session.ControllerSnapshot{
		SessionID:       MirrorSessionID(sess.SessionID),
		State:           session.ControllerStateIdle,
		CurrentCommand:  "claude",
		ResumeSession:   sess.SessionID,
		ClaudeLifecycle: "resumable",
		ActiveMeta: protocol.RuntimeMeta{
			ResumeSessionID: sess.SessionID,
			Command:         "claude",
			Engine:          "claude",
			CWD:             sess.CWD,
			ClaudeLifecycle: "resumable",
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
			ID:          MirrorSessionID(sess.SessionID),
			Title:       title,
			CreatedAt:   nonZeroTime(sess.CreatedAt, sess.UpdatedAt, time.Now().UTC()),
			UpdatedAt:   nonZeroTime(sess.UpdatedAt, sess.CreatedAt, time.Now().UTC()),
			LastPreview: preview,
			EntryCount:  len(entries),
			Runtime:     runtime,
			Source:      "claude-native",
			External:    true,
		},
		Projection: projection,
	}
}

// scanDir 在单个项目目录下把所有 jsonl 按 sessionId 聚合成 NativeSession。
// 每组只对 mtime 最新的那份 jsonl 做完整扫描拿 Title / preview / LogEntries，
// 其余 jsonl 只用 mtime 参与 UpdatedAt；这是在 413 unique sid / 503 files 场景下的性能权衡。
func scanDir(ctx context.Context, dir string, fallbackCWD string) ([]NativeSession, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []NativeSession{}, nil
		}
		return nil, fmt.Errorf("read claude session dir failed: %w", err)
	}
	type fileMeta struct {
		path  string
		mtime time.Time
		sid   string
	}
	byID := map[string][]fileMeta{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		full := filepath.Join(dir, name)
		info, statErr := entry.Info()
		if statErr != nil {
			continue
		}
		sid := strings.TrimSuffix(name, ".jsonl")
		// 若 jsonl 首行携带 sessionId 且与文件名不同，以首行的 sessionId 为准。
		if firstSID := readFirstSessionID(full); firstSID != "" {
			sid = firstSID
		}
		if strings.TrimSpace(sid) == "" {
			continue
		}
		byID[sid] = append(byID[sid], fileMeta{path: full, mtime: info.ModTime(), sid: sid})
	}
	sessions := make([]NativeSession, 0, len(byID))
	for sid, files := range byID {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		sort.Slice(files, func(i, j int) bool {
			return files[i].mtime.After(files[j].mtime)
		})
		authoritative := files[0]
		sess, parseErr := parseSessionFromFile(authoritative.path, sid)
		if parseErr != nil {
			continue
		}
		if strings.TrimSpace(sess.CWD) == "" {
			sess.CWD = fallbackCWD
		}
		paths := make([]string, 0, len(files))
		for _, f := range files {
			paths = append(paths, f.path)
			if f.mtime.After(sess.UpdatedAt) {
				sess.UpdatedAt = f.mtime
			}
			if sess.CreatedAt.IsZero() || f.mtime.Before(sess.CreatedAt) {
				sess.CreatedAt = f.mtime
			}
		}
		sess.JSONLFiles = paths
		sess.MirrorSessionID = MirrorSessionID(sid)
		sessions = append(sessions, sess)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions, nil
}

// parseSessionFromFile 扫描一条 jsonl 拿 Title/FirstUserMessage/LastAssistantText/LogEntries 等。
// 对未知 type 宽松跳过，保持向前兼容。
func parseSessionFromFile(path string, fallbackSessionID string) (NativeSession, error) {
	file, err := os.Open(path)
	if err != nil {
		return NativeSession{}, fmt.Errorf("open claude jsonl failed: %w", err)
	}
	defer file.Close()

	sess := NativeSession{SessionID: fallbackSessionID}
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)
	for scanner.Scan() {
		var line jsonlLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if sess.SessionID == "" && strings.TrimSpace(line.SessionID) != "" {
			sess.SessionID = strings.TrimSpace(line.SessionID)
		}
		if sess.CWD == "" && strings.TrimSpace(line.CWD) != "" {
			sess.CWD = strings.TrimSpace(line.CWD)
		}
		ts := normalizeJSONLTimestamp(line.Timestamp)
		switch strings.TrimSpace(line.Type) {
		case "user":
			text := extractUserMessageText(line.Message)
			if !isMeaningfulPromptText(text) {
				continue
			}
			if sess.FirstUserMessage == "" {
				sess.FirstUserMessage = text
			}
			if sess.Title == "" {
				sess.Title = text
			}
			sess.LogEntries = append(sess.LogEntries, store.SnapshotLogEntry{
				Kind:      "user",
				Label:     "历史输入",
				Message:   text,
				Text:      text,
				Timestamp: ts,
			})
		case "assistant":
			text := extractAssistantText(line.Message)
			if text == "" {
				continue
			}
			sess.LastAssistantText = text
			sess.LogEntries = append(sess.LogEntries, store.SnapshotLogEntry{
				Kind:      "markdown",
				Message:   text,
				Text:      text,
				Timestamp: ts,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return NativeSession{}, fmt.Errorf("scan claude jsonl failed: %w", err)
	}
	return sess, nil
}

// readFirstSessionID 只读首行拿 sessionId，用于目录聚合时的快速校验。
func readFirstSessionID(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		var line jsonlLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if sid := strings.TrimSpace(line.SessionID); sid != "" {
			return sid
		}
	}
	return ""
}

func extractUserMessageText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload userMessagePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	content := payload.Content
	if len(content) == 0 {
		return ""
	}
	// user.message.content 可能是纯字符串，也可能是 assistant 风格的 block 数组。
	if len(content) > 0 && content[0] == '"' {
		var text string
		if err := json.Unmarshal(content, &text); err == nil {
			return strings.TrimSpace(text)
		}
	}
	var blocks []assistantContentBlock
	if err := json.Unmarshal(content, &blocks); err == nil {
		for _, block := range blocks {
			if strings.TrimSpace(block.Type) == "text" {
				if trimmed := strings.TrimSpace(block.Text); trimmed != "" {
					return trimmed
				}
			}
		}
	}
	return ""
}

func extractAssistantText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	if len(payload.Content) == 0 {
		return ""
	}
	var blocks []assistantContentBlock
	if err := json.Unmarshal(payload.Content, &blocks); err != nil {
		return ""
	}
	for _, block := range blocks {
		if strings.TrimSpace(block.Type) == "text" {
			if trimmed := strings.TrimSpace(block.Text); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func normalizeJSONLTimestamp(value string) string {
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
		strings.HasPrefix(lower, "command finished ") {
		return false
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
