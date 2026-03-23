package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type FileStore struct {
	mu        sync.Mutex
	baseDir   string
	indexPath string
}

type fileIndex struct {
	Sessions []SessionSummary `json:"sessions"`
}

func NewFileStore(baseDir string) (*FileStore, error) {
	if strings.TrimSpace(baseDir) == "" {
		baseDir = defaultBaseDir()
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}
	return &FileStore{
		baseDir:   baseDir,
		indexPath: filepath.Join(baseDir, "index.json"),
	}, nil
}

func defaultBaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".", ".mobilevc", "sessions")
	}
	return filepath.Join(home, ".mobilevc", "sessions")
}

func (s *FileStore) CreateSession(ctx context.Context, title string) (SessionSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SessionSummary{}, ctx.Err()
	default:
	}
	now := time.Now().UTC()
	summary := SessionSummary{
		ID:        fmt.Sprintf("session-%d", now.UnixNano()),
		Title:     fallbackTitle(title, now),
		CreatedAt: now,
		UpdatedAt: now,
	}
	record := SessionRecord{Summary: summary, Projection: ProjectionSnapshot{RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""}}}
	index, err := s.readIndexLocked()
	if err != nil {
		return SessionSummary{}, err
	}
	index.Sessions = append([]SessionSummary{summary}, filterOut(index.Sessions, summary.ID)...)
	if err := s.writeSessionLocked(record); err != nil {
		return SessionSummary{}, err
	}
	if err := s.writeIndexLocked(index); err != nil {
		return SessionSummary{}, err
	}
	return summary, nil
}

func (s *FileStore) SaveProjection(ctx context.Context, sessionID string, projection ProjectionSnapshot) (SessionSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SessionSummary{}, ctx.Err()
	default:
	}
	record, err := s.readSessionLocked(sessionID)
	if err != nil {
		return SessionSummary{}, err
	}
	now := time.Now().UTC()
	record.Projection = normalizeProjection(projection)
	record.Summary.Runtime = record.Projection.Runtime
	record.Summary.UpdatedAt = now
	record.Summary.EntryCount = len(record.Projection.LogEntries)
	record.Summary.LastPreview = buildPreview(record.Projection)
	if err := s.writeSessionLocked(record); err != nil {
		return SessionSummary{}, err
	}
	index, err := s.readIndexLocked()
	if err != nil {
		return SessionSummary{}, err
	}
	updated := false
	for i := range index.Sessions {
		if index.Sessions[i].ID == sessionID {
			index.Sessions[i] = record.Summary
			updated = true
			break
		}
	}
	if !updated {
		index.Sessions = append(index.Sessions, record.Summary)
	}
	sort.Slice(index.Sessions, func(i, j int) bool {
		return index.Sessions[i].UpdatedAt.After(index.Sessions[j].UpdatedAt)
	})
	if err := s.writeIndexLocked(index); err != nil {
		return SessionSummary{}, err
	}
	return record.Summary, nil
}

func (s *FileStore) GetSession(ctx context.Context, sessionID string) (SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SessionRecord{}, ctx.Err()
	default:
	}
	return s.readSessionLocked(sessionID)
}

func (s *FileStore) ListSessions(ctx context.Context) ([]SessionSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	index, err := s.readIndexLocked()
	if err != nil {
		return nil, err
	}
	items := append([]SessionSummary(nil), index.Sessions...)
	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items, nil
}

func (s *FileStore) DeleteSession(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if _, err := s.readSessionLocked(sessionID); err != nil {
		return err
	}
	if err := os.Remove(s.sessionPath(sessionID)); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("session not found: %s", sessionID)
		}
		return fmt.Errorf("delete session record: %w", err)
	}
	index, err := s.readIndexLocked()
	if err != nil {
		return err
	}
	index.Sessions = filterOut(index.Sessions, sessionID)
	if err := s.writeIndexLocked(index); err != nil {
		return err
	}
	return nil
}

func (s *FileStore) readIndexLocked() (fileIndex, error) {
	var index fileIndex
	data, err := os.ReadFile(s.indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fileIndex{}, nil
		}
		return fileIndex{}, fmt.Errorf("read session index: %w", err)
	}
	if len(data) == 0 {
		return fileIndex{}, nil
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return fileIndex{}, fmt.Errorf("decode session index: %w", err)
	}
	return index, nil
}

func (s *FileStore) writeIndexLocked(index fileIndex) error {
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session index: %w", err)
	}
	return os.WriteFile(s.indexPath, data, 0o644)
}

func (s *FileStore) readSessionLocked(sessionID string) (SessionRecord, error) {
	data, err := os.ReadFile(s.sessionPath(sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return SessionRecord{}, fmt.Errorf("session not found: %s", sessionID)
		}
		return SessionRecord{}, fmt.Errorf("read session record: %w", err)
	}
	var record SessionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return SessionRecord{}, fmt.Errorf("decode session record: %w", err)
	}
	record.Projection = normalizeProjection(record.Projection)
	return record, nil
}

func (s *FileStore) writeSessionLocked(record SessionRecord) error {
	record.Projection = normalizeProjection(record.Projection)
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session record: %w", err)
	}
	return os.WriteFile(s.sessionPath(record.Summary.ID), data, 0o644)
}

func (s *FileStore) sessionPath(sessionID string) string {
	return filepath.Join(s.baseDir, sessionID+".json")
}

func (s *FileStore) BaseDir() string {
	if s == nil {
		return ""
	}
	return s.baseDir
}

func filterOut(items []SessionSummary, id string) []SessionSummary {
	out := make([]SessionSummary, 0, len(items))
	for _, item := range items {
		if item.ID != id {
			out = append(out, item)
		}
	}
	return out
}

func normalizeProjection(projection ProjectionSnapshot) ProjectionSnapshot {
	if projection.RawTerminalByStream == nil {
		projection.RawTerminalByStream = map[string]string{"stdout": "", "stderr": ""}
	}
	if _, ok := projection.RawTerminalByStream["stdout"]; !ok {
		projection.RawTerminalByStream["stdout"] = ""
	}
	if _, ok := projection.RawTerminalByStream["stderr"]; !ok {
		projection.RawTerminalByStream["stderr"] = ""
	}
	if projection.LogEntries == nil {
		projection.LogEntries = []SnapshotLogEntry{}
	}
	return projection
}

func buildPreview(projection ProjectionSnapshot) string {
	for i := len(projection.LogEntries) - 1; i >= 0; i-- {
		entry := projection.LogEntries[i]
		switch entry.Kind {
		case "markdown", "system", "user":
			text := strings.TrimSpace(firstNonEmptyString(entry.Message, entry.Text))
			if text != "" {
				return truncatePreview(text)
			}
		case "error":
			if entry.Context != nil {
				text := strings.TrimSpace(entry.Context.Message)
				if text != "" {
					return truncatePreview(text)
				}
			}
		}
	}
	return ""
}

func truncatePreview(text string) string {
	runes := []rune(text)
	if len(runes) <= 80 {
		return text
	}
	return string(runes[:80]) + "…"
}

func fallbackTitle(title string, now time.Time) string {
	title = strings.TrimSpace(title)
	if title != "" {
		return title
	}
	return now.Local().Format("2006-01-02 15:04")
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
