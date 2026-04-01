package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	permissionRulesPath string
}

type fileIndex struct {
	Sessions []SessionSummary `json:"sessions"`
}

var (
	sessionPlaceholderPattern = regexp.MustCompile(`^session(?:[-_\s][a-z0-9]+)?$`)
	sessionTimestampPattern   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(?:[ t]\d{2}:\d{2}(?::\d{2})?)?$`)
)

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
		permissionRulesPath: filepath.Join(baseDir, "permissions.rules.json"),
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
		Runtime:   SessionRuntime{Source: "mobilevc"},
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

func (s *FileStore) UpsertSession(ctx context.Context, record SessionRecord) (SessionSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SessionSummary{}, ctx.Err()
	default:
	}
	record = normalizeSessionRecord(record)
	if strings.TrimSpace(record.Summary.ID) == "" {
		return SessionSummary{}, fmt.Errorf("session id is required")
	}
	if record.Summary.CreatedAt.IsZero() {
		record.Summary.CreatedAt = time.Now().UTC()
	}
	if record.Summary.UpdatedAt.IsZero() {
		record.Summary.UpdatedAt = record.Summary.CreatedAt
	}
	index, err := s.readIndexLocked()
	if err != nil {
		return SessionSummary{}, err
	}
	if existing, err := s.readSessionLocked(record.Summary.ID); err == nil {
		if record.Summary.Title == "" {
			record.Summary.Title = existing.Summary.Title
		}
		if record.Summary.LastPreview == "" {
			record.Summary.LastPreview = existing.Summary.LastPreview
		}
		if record.Summary.CreatedAt.IsZero() {
			record.Summary.CreatedAt = existing.Summary.CreatedAt
		}
	}
	record.Summary = deriveProjectionSummary(record.Summary, record.Projection)
	if err := s.writeSessionLocked(record); err != nil {
		return SessionSummary{}, err
	}
	updated := false
	for i := range index.Sessions {
		if index.Sessions[i].ID == record.Summary.ID {
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
	if record.Summary.External || strings.EqualFold(strings.TrimSpace(record.Summary.Source), "codex-native") {
		record.Projection.SessionContext = normalizeSessionContext(projection.SessionContext)
		record.Projection.SkillCatalogMeta = normalizeCatalogMetadata(
			projection.SkillCatalogMeta,
			CatalogDomainSkill,
		)
		record.Projection.MemoryCatalogMeta = normalizeCatalogMetadata(
			projection.MemoryCatalogMeta,
			CatalogDomainMemory,
		)
		if err := s.writeSessionLocked(record); err != nil {
			return SessionSummary{}, err
		}
		return record.Summary, nil
	}
	now := time.Now().UTC()
	record.Projection = normalizeProjection(projection)
	record.Summary.Runtime = record.Projection.Runtime
	if record.Summary.Runtime.Source == "" {
		record.Summary.Runtime.Source = "mobilevc"
	}
	record.Summary.UpdatedAt = now
	record.Summary = deriveProjectionSummary(record.Summary, record.Projection)
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
	index, err = s.reconcileIndexLocked(index)
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

func (s *FileStore) GetPermissionRuleSnapshot(ctx context.Context) (PermissionRuleSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return PermissionRuleSnapshot{}, ctx.Err()
	default:
	}
	return s.readPermissionRuleSnapshotLocked()
}

func (s *FileStore) SavePermissionRuleSnapshot(ctx context.Context, snapshot PermissionRuleSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	snapshot = normalizePermissionRuleSnapshot(snapshot)
	return s.writeJSONFileLocked(s.permissionRulesPath, snapshot, "encode permission rules")
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
	return normalizeSessionRecord(record), nil
}

func (s *FileStore) writeSessionLocked(record SessionRecord) error {
	record = normalizeSessionRecord(record)
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

func (s *FileStore) readPermissionRuleSnapshotLocked() (PermissionRuleSnapshot, error) {
	var snapshot PermissionRuleSnapshot
	if err := s.readJSONFileLocked(s.permissionRulesPath, &snapshot, "read permission rules", "decode permission rules"); err != nil {
		return PermissionRuleSnapshot{}, err
	}
	return normalizePermissionRuleSnapshot(snapshot), nil
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
	if projection.PermissionRules == nil {
		projection.PermissionRules = []PermissionRule{}
	}
	projection.PermissionRules = normalizePermissionRules(projection.PermissionRules)
	projection.SessionContext = normalizeSessionContext(projection.SessionContext)
	projection.SkillCatalogMeta = normalizeCatalogMetadata(projection.SkillCatalogMeta, CatalogDomainSkill)
	projection.MemoryCatalogMeta = normalizeCatalogMetadata(projection.MemoryCatalogMeta, CatalogDomainMemory)
	return projection
}

func normalizeSessionRecord(record SessionRecord) SessionRecord {
	record.Projection = normalizeProjection(record.Projection)
	if record.Summary.External || strings.EqualFold(strings.TrimSpace(record.Summary.Source), "codex-native") {
		if record.Summary.Runtime.Source == "" {
			record.Summary.Runtime.Source = "codex-native"
		}
		record.Projection.Runtime = record.Summary.Runtime
		return record
	}
	if record.Projection.Runtime.Source == "" {
		record.Projection.Runtime.Source = record.Summary.Runtime.Source
	}
	record.Summary.Runtime = record.Projection.Runtime
	if record.Summary.Runtime.Source == "" {
		record.Summary.Runtime.Source = "mobilevc"
	}
	record.Summary = deriveProjectionSummary(record.Summary, record.Projection)
	return record
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

func normalizePermissionRuleSnapshot(snapshot PermissionRuleSnapshot) PermissionRuleSnapshot {
	snapshot.Items = normalizePermissionRules(snapshot.Items)
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

func normalizePermissionRules(items []PermissionRule) []PermissionRule {
	if len(items) == 0 {
		return []PermissionRule{}
	}
	out := make([]PermissionRule, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" {
			continue
		}
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		item.Engine = strings.TrimSpace(strings.ToLower(item.Engine))
		item.CommandHead = strings.TrimSpace(strings.ToLower(item.CommandHead))
		item.TargetPathPrefix = strings.TrimSpace(item.TargetPathPrefix)
		item.Summary = strings.TrimSpace(item.Summary)
		if item.Scope == "" {
			item.Scope = PermissionScopeSession
		}
		if item.Kind == "" {
			item.Kind = PermissionKindGeneric
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func buildPreview(projection ProjectionSnapshot) string {
	if text := latestMeaningfulProjectionText(projection, true); text != "" {
		return truncatePreview(text)
	}
	if text := latestMeaningfulProjectionText(projection, false); text != "" {
		return truncatePreview(text)
	}
	return ""
}

func deriveProjectionSummary(summary SessionSummary, projection ProjectionSnapshot) SessionSummary {
	summary.EntryCount = len(projection.LogEntries)
	if title := buildSummaryTitle(summary.Title, projection, summary.CreatedAt); title != "" {
		summary.Title = title
	}
	summary.LastPreview = buildPreview(projection)
	return summary
}

func buildSummaryTitle(current string, projection ProjectionSnapshot, createdAt time.Time) string {
	normalizedCurrent := normalizeSummaryText(current)
	if isMeaningfulSummaryTitle(normalizedCurrent) {
		return normalizedCurrent
	}
	if text := firstMeaningfulProjectionText(projection, true); text != "" {
		return truncatePreview(text)
	}
	if text := firstMeaningfulProjectionText(projection, false); text != "" {
		return truncatePreview(text)
	}
	if normalizedCurrent != "" {
		if looksLikeSummaryNoise(normalizedCurrent) || looksLikeBootstrapCommand(normalizedCurrent) {
			return fallbackTitle("", nonZeroTime(createdAt, time.Now().UTC()))
		}
		return normalizedCurrent
	}
	return fallbackTitle("", nonZeroTime(createdAt, time.Now().UTC()))
}

func firstMeaningfulProjectionText(projection ProjectionSnapshot, userOnly bool) string {
	for _, entry := range projection.LogEntries {
		if text := meaningfulProjectionEntryText(entry, userOnly); text != "" {
			return text
		}
	}
	return ""
}

func latestMeaningfulProjectionText(projection ProjectionSnapshot, userOnly bool) string {
	for i := len(projection.LogEntries) - 1; i >= 0; i-- {
		if text := meaningfulProjectionEntryText(projection.LogEntries[i], userOnly); text != "" {
			return text
		}
	}
	return ""
}

func meaningfulProjectionEntryText(entry SnapshotLogEntry, userOnly bool) string {
	if userOnly && entry.Kind != "user" {
		return ""
	}
	var text string
	switch entry.Kind {
	case "markdown", "system", "user":
		text = firstNonEmptyString(entry.Message, entry.Text)
	case "error":
		if entry.Context != nil {
			text = entry.Context.Message
		}
	default:
		return ""
	}
	text = normalizeSummaryText(text)
	if !isMeaningfulSummaryText(text) {
		return ""
	}
	return text
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

func normalizeSummaryText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func isMeaningfulSummaryText(text string) bool {
	if text == "" {
		return false
	}
	return !looksLikeSummaryNoise(text) && !looksLikeBootstrapCommand(text)
}

func isMeaningfulSummaryTitle(text string) bool {
	if !isMeaningfulSummaryText(text) {
		return false
	}
	return !looksLikePlaceholderTitle(text)
}

func looksLikeSummaryNoise(text string) bool {
	lower := strings.ToLower(normalizeSummaryText(text))
	if lower == "" {
		return true
	}
	switch lower {
	case "ok", "done", "running", "thinking", "processing", "active", "ready", "idle", "is ready", "已就绪",
		"session active", "session ready", "command started", "command finished", "status: active", "status: ready", "status: idle":
		return true
	}
	return strings.HasPrefix(lower, "command started ") ||
		strings.HasPrefix(lower, "command finished ") ||
		strings.HasPrefix(lower, "active:") ||
		strings.HasPrefix(lower, "ready:") ||
		strings.HasPrefix(lower, "idle:") ||
		strings.HasPrefix(lower, "--config model_reasoning_effort=") ||
		strings.HasPrefix(lower, "model_reasoning_effort=") ||
		looksLikeTimestampText(lower) ||
		looksLikeModelSummary(lower)
}

func looksLikePlaceholderTitle(text string) bool {
	lower := strings.ToLower(normalizeSummaryText(text))
	if lower == "" {
		return true
	}
	return lower == "session" ||
		lower == "new session" ||
		lower == "history" ||
		sessionPlaceholderPattern.MatchString(lower)
}

func looksLikeTimestampText(text string) bool {
	return sessionTimestampPattern.MatchString(strings.ToLower(normalizeSummaryText(text)))
}

func looksLikeModelSummary(text string) bool {
	fields := strings.Fields(strings.ToLower(normalizeSummaryText(text)))
	if len(fields) < 2 {
		return false
	}
	switch fields[0] {
	case "claude", "codex", "gemini":
	default:
		return false
	}
	for _, field := range fields[1:] {
		if strings.Contains(field, "gpt-") ||
			strings.Contains(field, "sonnet") ||
			strings.Contains(field, "opus") ||
			field == "-low" ||
			field == "-medium" ||
			field == "-high" ||
			field == "low" ||
			field == "medium" ||
			field == "high" {
			return true
		}
	}
	return false
}

func looksLikeBootstrapCommand(text string) bool {
	normalized := normalizeSummaryText(text)
	if normalized == "" {
		return false
	}
	lower := strings.ToLower(normalized)
	if strings.HasPrefix(lower, "--config ") ||
		strings.HasPrefix(lower, "--model ") ||
		strings.HasPrefix(lower, "-m ") {
		return true
	}
	startsWithAICommand := lower == "claude" ||
		strings.HasPrefix(lower, "claude ") ||
		lower == "codex" ||
		strings.HasPrefix(lower, "codex ") ||
		lower == "gemini" ||
		strings.HasPrefix(lower, "gemini ")
	if !startsWithAICommand {
		return false
	}
	if !strings.Contains(normalized, " ") {
		return true
	}
	return strings.Contains(lower, " --model ") ||
		strings.Contains(lower, " -m ") ||
		strings.Contains(lower, " --config ") ||
		strings.Contains(lower, " --permission-mode ") ||
		strings.Contains(lower, " --approval-mode ") ||
		strings.Contains(lower, " --dangerously-skip-permissions") ||
		looksLikeModelSummary(lower)
}

func nonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func (s *FileStore) reconcileIndexLocked(index fileIndex) (fileIndex, error) {
	updated := false
	for i := range index.Sessions {
		record, err := s.readSessionLocked(index.Sessions[i].ID)
		if err != nil {
			continue
		}
		if index.Sessions[i] != record.Summary {
			index.Sessions[i] = record.Summary
			updated = true
		}
	}
	if updated {
		if err := s.writeIndexLocked(index); err != nil {
			return fileIndex{}, err
		}
	}
	return index, nil
}
