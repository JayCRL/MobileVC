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

	"mobilevc/internal/session"
)

type FileStore struct {
	mu                sync.Mutex
	baseDir           string
	indexPath         string
	skillCatalogPath  string
	memoryCatalogPath string
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
		baseDir:           baseDir,
		indexPath:         filepath.Join(baseDir, "index.json"),
		skillCatalogPath:  filepath.Join(baseDir, "skills.catalog.json"),
		memoryCatalogPath: filepath.Join(baseDir, "memory.catalog.json"),
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
	record := SessionRecord{Summary: summary, Projection: normalizeProjection(ProjectionSnapshot{RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""}})}
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

func (s *FileStore) ListSkillCatalog(ctx context.Context) ([]SkillDefinition, error) {
	snapshot, err := s.GetSkillCatalogSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.Items, nil
}

func (s *FileStore) SaveSkillCatalog(ctx context.Context, items []SkillDefinition) error {
	snapshot, err := s.GetSkillCatalogSnapshot(ctx)
	if err != nil {
		return err
	}
	snapshot.Items = items
	if snapshot.Meta.Domain == "" {
		snapshot.Meta.Domain = CatalogDomainSkill
	}
	return s.SaveSkillCatalogSnapshot(ctx, snapshot)
}

func (s *FileStore) GetSkillCatalogSnapshot(ctx context.Context) (SkillCatalogSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SkillCatalogSnapshot{}, ctx.Err()
	default:
	}
	return s.readSkillCatalogSnapshotLocked()
}

func (s *FileStore) SaveSkillCatalogSnapshot(ctx context.Context, snapshot SkillCatalogSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	snapshot = normalizeSkillCatalogSnapshot(snapshot)
	return s.writeJSONFileLocked(s.skillCatalogPath, snapshot, "encode skill catalog")
}

func (s *FileStore) ListMemoryCatalog(ctx context.Context) ([]MemoryItem, error) {
	snapshot, err := s.GetMemoryCatalogSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.Items, nil
}

func (s *FileStore) SaveMemoryCatalog(ctx context.Context, items []MemoryItem) error {
	snapshot, err := s.GetMemoryCatalogSnapshot(ctx)
	if err != nil {
		return err
	}
	snapshot.Items = items
	if snapshot.Meta.Domain == "" {
		snapshot.Meta.Domain = CatalogDomainMemory
	}
	return s.SaveMemoryCatalogSnapshot(ctx, snapshot)
}

func (s *FileStore) GetMemoryCatalogSnapshot(ctx context.Context) (MemoryCatalogSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return MemoryCatalogSnapshot{}, ctx.Err()
	default:
	}
	return s.readMemoryCatalogSnapshotLocked()
}

func (s *FileStore) SaveMemoryCatalogSnapshot(ctx context.Context, snapshot MemoryCatalogSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	snapshot = normalizeMemoryCatalogSnapshot(snapshot)
	return s.writeJSONFileLocked(s.memoryCatalogPath, snapshot, "encode memory catalog")
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

func (s *FileStore) readSkillCatalogSnapshotLocked() (SkillCatalogSnapshot, error) {
	data, err := os.ReadFile(s.skillCatalogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return normalizeSkillCatalogSnapshot(SkillCatalogSnapshot{}), nil
		}
		return SkillCatalogSnapshot{}, fmt.Errorf("read skill catalog: %w", err)
	}
	if len(data) == 0 {
		return normalizeSkillCatalogSnapshot(SkillCatalogSnapshot{}), nil
	}

	var snapshot SkillCatalogSnapshot
	if err := json.Unmarshal(data, &snapshot); err == nil {
		return normalizeSkillCatalogSnapshot(snapshot), nil
	}

	var items []SkillDefinition
	if err := json.Unmarshal(data, &items); err == nil {
		return normalizeSkillCatalogSnapshot(SkillCatalogSnapshot{Items: items}), nil
	}

	if err := json.Unmarshal(data, &snapshot); err != nil {
		return SkillCatalogSnapshot{}, fmt.Errorf("decode skill catalog: %w", err)
	}
	return normalizeSkillCatalogSnapshot(snapshot), nil
}

func (s *FileStore) readMemoryCatalogSnapshotLocked() (MemoryCatalogSnapshot, error) {
	var snapshot MemoryCatalogSnapshot
	if err := s.readJSONFileLocked(s.memoryCatalogPath, &snapshot, "read memory catalog", "decode memory catalog"); err != nil {
		return MemoryCatalogSnapshot{}, err
	}
	return normalizeMemoryCatalogSnapshot(snapshot), nil
}

func (s *FileStore) readJSONFileLocked(path string, target any, readErrLabel, decodeErrLabel string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("%s: %w", readErrLabel, err)
	}
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("%s: %w", decodeErrLabel, err)
	}
	return nil
}

func (s *FileStore) writeJSONFileLocked(path string, value any, encodeErrLabel string) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("%s: %w", encodeErrLabel, err)
	}
	return os.WriteFile(path, data, 0o644)
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
	if projection.TerminalExecutions == nil {
		projection.TerminalExecutions = []TerminalExecution{}
	}
	if projection.ReviewGroups == nil {
		projection.ReviewGroups = []session.ReviewGroup{}
	}
	projection.SessionContext = normalizeSessionContext(projection.SessionContext)
	projection.SkillCatalogMeta = normalizeCatalogMetadata(projection.SkillCatalogMeta, CatalogDomainSkill)
	projection.MemoryCatalogMeta = normalizeCatalogMetadata(projection.MemoryCatalogMeta, CatalogDomainMemory)
	return projection
}

func normalizeSessionContext(ctx SessionContext) SessionContext {
	ctx.EnabledSkillNames = normalizeStringSlice(ctx.EnabledSkillNames)
	ctx.EnabledMemoryIDs = normalizeStringSlice(ctx.EnabledMemoryIDs)
	return ctx
}

func normalizeSkillCatalogSnapshot(snapshot SkillCatalogSnapshot) SkillCatalogSnapshot {
	snapshot.Meta = normalizeCatalogMetadata(snapshot.Meta, CatalogDomainSkill)
	snapshot.Items = normalizeSkillCatalog(snapshot.Items)
	return snapshot
}

func normalizeMemoryCatalogSnapshot(snapshot MemoryCatalogSnapshot) MemoryCatalogSnapshot {
	snapshot.Meta = normalizeCatalogMetadata(snapshot.Meta, CatalogDomainMemory)
	snapshot.Items = normalizeMemoryCatalog(snapshot.Items)
	return snapshot
}

func normalizeCatalogMetadata(meta CatalogMetadata, domain CatalogDomain) CatalogMetadata {
	if meta.Domain == "" {
		meta.Domain = domain
	}
	if meta.SourceOfTruth == "" {
		meta.SourceOfTruth = CatalogSourceTruthClaude
	}
	if meta.SyncState == "" {
		meta.SyncState = CatalogSyncStateIdle
	}
	return meta
}

func normalizeSkillCatalog(items []SkillDefinition) []SkillDefinition {
	if len(items) == 0 {
		return []SkillDefinition{}
	}
	out := make([]SkillDefinition, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		item.Name = name
		item.Description = strings.TrimSpace(item.Description)
		item.Prompt = strings.TrimSpace(item.Prompt)
		item.ResultView = strings.TrimSpace(item.ResultView)
		item.TargetType = strings.TrimSpace(item.TargetType)
		if item.Source == "" {
			item.Source = SkillSourceLocal
		}
		if item.SourceOfTruth == "" {
			item.SourceOfTruth = CatalogSourceTruthClaude
		}
		if item.SyncState == "" {
			if item.Source == SkillSourceLocal {
				item.SyncState = CatalogSyncStateDraft
			} else {
				item.SyncState = CatalogSyncStateIdle
			}
		}
		if item.Source == SkillSourceBuiltin {
			item.Editable = false
		} else if !item.Editable {
			item.Editable = item.Source == SkillSourceLocal
		}
		if _, ok := seen[item.Name]; ok {
			continue
		}
		seen[item.Name] = struct{}{}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func normalizeMemoryCatalog(items []MemoryItem) []MemoryItem {
	if len(items) == 0 {
		return []MemoryItem{}
	}
	out := make([]MemoryItem, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		item.ID = id
		item.Title = strings.TrimSpace(item.Title)
		item.Content = strings.TrimSpace(item.Content)
		item.Source = strings.TrimSpace(item.Source)
		if item.Source == "" {
			item.Source = "local"
		}
		if item.SourceOfTruth == "" {
			item.SourceOfTruth = CatalogSourceTruthClaude
		}
		if item.SyncState == "" {
			if item.Source == "local" {
				item.SyncState = CatalogSyncStateDraft
			} else {
				item.SyncState = CatalogSyncStateIdle
			}
		}
		if item.Source == "builtin" {
			item.Editable = false
		} else if !item.Editable {
			item.Editable = item.Source == "local"
		}
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func normalizeStringSlice(items []string) []string {
	if len(items) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
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
