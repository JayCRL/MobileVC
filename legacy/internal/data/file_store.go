package data

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"mobilevc/internal/protocol"
)

type FileStore struct {
	mu                  sync.Mutex
	baseDir             string
	indexPath           string
	skillCatalogPath    string
	memoryCatalogPath   string
	permissionRulesPath string
	pushTokensPath      string
	sessionMetaCache    map[string]sessionRecordMeta
}

type fileIndex struct {
	Sessions            []SessionSummary `json:"sessions"`
	DeletedNativeClaude []string         `json:"deletedNativeClaude,omitempty"`
	DeletedNativeCodex  []string         `json:"deletedNativeCodex,omitempty"`
}

type sessionRecordMeta struct {
	Summary           SessionSummary
	ClientActions     []ClientActionRecord
	SessionContext    SessionContext
	SessionContextSet bool
}

type sessionProjectionSidecars struct {
	RuntimeMeta       sessionRuntimeMetaSidecar
	Context           sessionContextSidecar
	Permission        sessionPermissionSidecar
	Diff              sessionDiffSidecar
	Terminal          sessionTerminalSidecar
	TerminalExecution sessionTerminalExecutionSidecar
}

type sessionLogEntriesSidecar struct {
	SessionID  string             `json:"sessionId"`
	EntryCount int                `json:"entryCount"`
	LogEntries []SnapshotLogEntry `json:"logEntries"`
}

const sessionLogEntriesSidecarVersion = 1

type sessionLogEntriesSidecarHeader struct {
	Version    int    `json:"version"`
	SessionID  string `json:"sessionId"`
	EntryCount int    `json:"entryCount"`
}

type sessionLogEntriesIndex struct {
	Version    int     `json:"version"`
	SessionID  string  `json:"sessionId"`
	EntryCount int     `json:"entryCount"`
	Offsets    []int64 `json:"offsets"`
}

const sessionSideDomainVersion = 1

type sessionRuntimeMetaSidecar struct {
	Version            int                     `json:"version"`
	SessionID          string                  `json:"sessionId"`
	Controller         ControllerSnapshot      `json:"controller,omitempty"`
	Runtime            SessionRuntime          `json:"runtime,omitempty"`
	ContextWindowUsage ContextWindowUsage      `json:"contextWindowUsage,omitempty"`
	CurrentStep        *SnapshotContext        `json:"currentStep,omitempty"`
	LatestError        *SnapshotContext        `json:"latestError,omitempty"`
	SkillCatalogMeta   CatalogMetadata         `json:"skillCatalogMeta,omitempty"`
	MemoryCatalogMeta  CatalogMetadata         `json:"memoryCatalogMeta,omitempty"`
	Counts             SessionProjectionCounts `json:"counts"`
}

type sessionContextSidecar struct {
	Version           int            `json:"version"`
	SessionID         string         `json:"sessionId"`
	SessionContext    SessionContext `json:"sessionContext,omitempty"`
	SessionContextSet bool           `json:"sessionContextSet,omitempty"`
}

type sessionPermissionSidecar struct {
	Version   int              `json:"version"`
	SessionID string           `json:"sessionId"`
	Enabled   bool             `json:"enabled"`
	Items     []PermissionRule `json:"items,omitempty"`
}

type sessionDiffSidecar struct {
	Version           int           `json:"version"`
	SessionID         string        `json:"sessionId"`
	Diffs             []DiffContext `json:"diffs,omitempty"`
	CurrentDiff       *DiffContext  `json:"currentDiff,omitempty"`
	ReviewGroups      []ReviewGroup `json:"reviewGroups,omitempty"`
	ActiveReviewGroup *ReviewGroup  `json:"activeReviewGroup,omitempty"`
}

type sessionTerminalSidecar struct {
	Version             int               `json:"version"`
	SessionID           string            `json:"sessionId"`
	RawTerminalByStream map[string]string `json:"rawTerminalByStream,omitempty"`
}

type sessionTerminalExecutionSidecar struct {
	Version            int                 `json:"version"`
	SessionID          string              `json:"sessionId"`
	TerminalExecutions []TerminalExecution `json:"terminalExecutions,omitempty"`
}

type sessionRecordLightweight struct {
	Summary       SessionSummary            `json:"summary"`
	Projection    projectionLightweightJSON `json:"projection"`
	ClientActions []ClientActionRecord      `json:"clientActions,omitempty"`
}

type projectionLightweightJSON struct {
	Diffs                  []DiffContext       `json:"diffs,omitempty"`
	CurrentDiff            *DiffContext        `json:"currentDiff,omitempty"`
	ReviewGroups           []ReviewGroup       `json:"reviewGroups,omitempty"`
	ActiveReviewGroup      *ReviewGroup        `json:"activeReviewGroup,omitempty"`
	CurrentStep            *SnapshotContext    `json:"currentStep,omitempty"`
	LatestError            *SnapshotContext    `json:"latestError,omitempty"`
	RawTerminalByStream    map[string]string   `json:"rawTerminalByStream,omitempty"`
	TerminalExecutions     []TerminalExecution `json:"terminalExecutions,omitempty"`
	Controller             ControllerSnapshot  `json:"controller,omitempty"`
	Runtime                SessionRuntime      `json:"runtime,omitempty"`
	ContextWindowUsage     ContextWindowUsage  `json:"contextWindowUsage,omitempty"`
	SessionContext         SessionContext      `json:"sessionContext,omitempty"`
	SessionContextSet      bool                `json:"sessionContextSet,omitempty"`
	PermissionRulesEnabled bool                `json:"permissionRulesEnabled,omitempty"`
	PermissionRules        []PermissionRule    `json:"permissionRules,omitempty"`
	SkillCatalogMeta       CatalogMetadata     `json:"skillCatalogMeta,omitempty"`
	MemoryCatalogMeta      CatalogMetadata     `json:"memoryCatalogMeta,omitempty"`
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
		baseDir:             baseDir,
		indexPath:           filepath.Join(baseDir, "index.json"),
		skillCatalogPath:    filepath.Join(baseDir, "skills.catalog.json"),
		memoryCatalogPath:   filepath.Join(baseDir, "memory.catalog.json"),
		permissionRulesPath: filepath.Join(baseDir, "permissions.rules.json"),
		pushTokensPath:      filepath.Join(baseDir, "push_tokens.json"),
		sessionMetaCache:    make(map[string]sessionRecordMeta),
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
		ID:                fmt.Sprintf("session-%d", now.UnixNano()),
		Title:             fallbackTitle(title, now),
		CreatedAt:         now,
		UpdatedAt:         now,
		Source:            "mobilevc",
		Ownership:         "mobilevc",
		Runtime:           SessionRuntime{Source: "mobilevc"},
		ClaudeSessionUUID: uuid.NewString(),
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
	s.cacheSessionMetaLocked(record)
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
	if existing, err := s.readSessionWithoutLogEntriesLocked(record.Summary.ID); err == nil {
		if record.Summary.Title == "" {
			record.Summary.Title = existing.Summary.Title
		}
		if record.Summary.LastPreview == "" {
			record.Summary.LastPreview = existing.Summary.LastPreview
		}
		if record.Summary.CreatedAt.IsZero() {
			record.Summary.CreatedAt = existing.Summary.CreatedAt
		}
		if record.Summary.ClaudeSessionUUID == "" {
			record.Summary.ClaudeSessionUUID = existing.Summary.ClaudeSessionUUID
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return SessionSummary{}, err
	}
	record.Summary = deriveProjectionSummary(record.Summary, record.Projection)
	if err := s.writeSessionLocked(record); err != nil {
		return SessionSummary{}, err
	}
	s.cacheSessionMetaLocked(record)
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
	return s.SaveProjectionWithOptions(ctx, sessionID, projection)
}

func (s *FileStore) SaveProjectionWithOptions(ctx context.Context, sessionID string, projection ProjectionSnapshot, opts ...ProjectionSaveOption) (SessionSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SessionSummary{}, ctx.Err()
	default:
	}
	meta, ok := s.sessionMetaLocked(sessionID)
	if !ok {
		record, err := s.readSessionWithoutLogEntriesLocked(sessionID)
		if err != nil {
			return SessionSummary{}, err
		}
		meta = sessionMetaFromLightweightRecord(record)
	}
	now := time.Now().UTC()
	if !projection.SessionContextSet {
		projection.SessionContextSet = projection.SessionContext.Configured ||
			len(projection.SessionContext.EnabledSkillNames) > 0 ||
			len(projection.SessionContext.EnabledMemoryIDs) > 0
	}
	if !projection.SessionContextSet {
		projection.SessionContext = meta.SessionContext
		projection.SessionContextSet = meta.SessionContextSet
	}
	preserveExistingLogEntries := len(projection.LogEntries) == 0 && meta.Summary.EntryCount > 0
	preservedLogEntryCount := meta.Summary.EntryCount
	if preserveExistingLogEntries {
		index, err := s.readSessionLogEntriesIndexLocked(sessionID)
		if err != nil {
			return SessionSummary{}, err
		}
		preservedLogEntryCount = index.EntryCount
	}
	record := SessionRecord{
		Summary:       meta.Summary,
		Projection:    projection,
		ClientActions: append([]ClientActionRecord(nil), meta.ClientActions...),
	}
	record.Projection = normalizeProjection(projection)
	record = normalizeSessionRecord(record)
	record.Summary.UpdatedAt = now
	saveOpts := ProjectionSaveOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&saveOpts)
		}
	}
	if saveOpts.JSONLSyncEntryCount != nil {
		record.Summary.JSONLSyncEntryCount = *saveOpts.JSONLSyncEntryCount
	}
	if preserveExistingLogEntries {
		record.Summary.EntryCount = preservedLogEntryCount
		record.Summary.Title = meta.Summary.Title
		record.Summary.LastPreview = meta.Summary.LastPreview
		sidecars := sidecarsFromRecord(record)
		if err := s.writeSessionRecordOnlyLocked(record); err != nil {
			return SessionSummary{}, err
		}
		if err := s.writeProjectionSidecarsLocked(record.Summary.ID, sidecars); err != nil {
			return SessionSummary{}, err
		}
	} else if err := s.writeSessionLocked(record); err != nil {
		return SessionSummary{}, err
	}
	s.cacheSessionMetaLocked(record)
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

func (s *FileStore) AppendSessionLogEntries(ctx context.Context, sessionID string, entries []SnapshotLogEntry, opts ...ProjectionSaveOption) (SessionSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SessionSummary{}, ctx.Err()
	default:
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SessionSummary{}, fmt.Errorf("session id is required")
	}
	record, err := s.readSessionWithoutLogEntriesLocked(sessionID)
	if err != nil {
		return SessionSummary{}, err
	}
	index, err := s.readSessionLogEntriesIndexLocked(sessionID)
	if err != nil {
		return SessionSummary{}, err
	}
	appendEntries := dedupeAppendLogEntries(s.readLastSessionLogEntryLocked(sessionID, index), entries)
	saveOpts := ProjectionSaveOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&saveOpts)
		}
	}
	if len(appendEntries) == 0 {
		if saveOpts.JSONLSyncEntryCount == nil {
			return record.Summary, nil
		}
		record.Summary.JSONLSyncEntryCount = *saveOpts.JSONLSyncEntryCount
		if err := s.writeSessionRecordOnlyLocked(record); err != nil {
			return SessionSummary{}, err
		}
		s.cacheSessionMetaLocked(record)
		if err := s.updateSessionIndexLocked(record.Summary); err != nil {
			return SessionSummary{}, err
		}
		return record.Summary, nil
	}
	if err := s.appendSessionLogEntriesLocked(sessionID, index, appendEntries); err != nil {
		return SessionSummary{}, err
	}
	totalEntryCount := index.EntryCount + len(appendEntries)
	record.Summary.EntryCount = totalEntryCount
	record.Summary.UpdatedAt = time.Now().UTC()
	projection := record.Projection
	projection.LogEntries = appendEntries
	record.Summary = deriveProjectionSummary(record.Summary, projection)
	record.Summary.EntryCount = totalEntryCount
	if saveOpts.JSONLSyncEntryCount != nil {
		record.Summary.JSONLSyncEntryCount = *saveOpts.JSONLSyncEntryCount
	}
	if err := s.updateRuntimeMetaCountLocked(sessionID, record.Summary.EntryCount); err != nil {
		return SessionSummary{}, err
	}
	if err := s.writeSessionRecordOnlyLocked(record); err != nil {
		return SessionSummary{}, err
	}
	s.cacheSessionMetaLocked(record)
	if err := s.updateSessionIndexLocked(record.Summary); err != nil {
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
	record, err := s.readSessionLocked(sessionID)
	if err != nil {
		return SessionRecord{}, err
	}
	s.cacheSessionMetaLocked(record)
	return record, nil
}

func (s *FileStore) GetSessionSummary(ctx context.Context, sessionID string) (SessionSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SessionSummary{}, ctx.Err()
	default:
	}
	record, err := s.readSessionWithoutLogEntriesLocked(sessionID)
	if err != nil {
		return SessionSummary{}, err
	}
	s.cacheSessionMetaLocked(record)
	return record.Summary, nil
}

func (s *FileStore) GetSessionHistoryWindow(ctx context.Context, req SessionHistoryWindowRequest) (SessionHistoryWindow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SessionHistoryWindow{}, ctx.Err()
	default:
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return SessionHistoryWindow{}, fmt.Errorf("session ID is required")
	}
	record, err := s.readSessionWithoutLogEntriesLocked(sessionID)
	if err != nil {
		return SessionHistoryWindow{}, err
	}
	if _, err := s.readOrRebuildSessionContextSidecarLocked(sessionID, record.Projection); err != nil {
		return SessionHistoryWindow{}, err
	}
	entries, start, total, err := s.readSessionLogEntryWindowLocked(sessionID, req.Before, req.Limit)
	if err != nil {
		return SessionHistoryWindow{}, err
	}
	record.Projection.LogEntries = nil
	record.Summary.EntryCount = total
	return SessionHistoryWindow{
		Record:        record,
		LogEntries:    entries,
		LogEntryStart: start,
		LogEntryTotal: total,
	}, nil
}

func (s *FileStore) GetSessionRuntimeMetadata(ctx context.Context, sessionID string) (SessionRuntimeMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SessionRuntimeMetadata{}, ctx.Err()
	default:
	}
	record, err := s.readSessionWithoutLogEntriesLocked(sessionID)
	if err != nil {
		return SessionRuntimeMetadata{}, err
	}
	runtimeMeta, err := s.readOrRebuildSessionRuntimeMetaSidecarLocked(sessionID, record)
	if err != nil {
		return SessionRuntimeMetadata{}, err
	}
	applyRuntimeMetaSidecarToProjection(&record.Projection, runtimeMeta)
	record.Summary.EntryCount = runtimeMeta.Counts.LogEntryCount
	return SessionRuntimeMetadata{Record: record, Latest: runtimeMeta.Counts}, nil
}

func (s *FileStore) GetSessionContext(ctx context.Context, sessionID string) (SessionContextSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SessionContextSnapshot{}, ctx.Err()
	default:
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SessionContextSnapshot{}, fmt.Errorf("session id is required")
	}
	record, err := s.readSessionWithoutLogEntriesLocked(sessionID)
	if err != nil {
		return SessionContextSnapshot{}, err
	}
	sidecar, err := s.readOrRebuildSessionContextSidecarLocked(sessionID, record.Projection)
	if err != nil {
		return SessionContextSnapshot{}, err
	}
	return SessionContextSnapshot{
		SessionID:      sidecar.SessionID,
		SessionContext: normalizeSessionContext(sidecar.SessionContext),
	}, nil
}

func (s *FileStore) SaveSessionContext(ctx context.Context, snapshot SessionContextSnapshot) (SessionSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SessionSummary{}, ctx.Err()
	default:
	}
	sessionID := strings.TrimSpace(snapshot.SessionID)
	if sessionID == "" {
		return SessionSummary{}, fmt.Errorf("session id is required")
	}
	record, err := s.readSessionWithoutLogEntriesLocked(sessionID)
	if err != nil {
		return SessionSummary{}, err
	}
	record.Projection.SessionContext = normalizeSessionContext(snapshot.SessionContext)
	record.Projection.SessionContextSet = record.Projection.SessionContext.Configured
	record.Summary.UpdatedAt = time.Now().UTC()
	sidecar := sessionContextSidecar{
		Version:           sessionSideDomainVersion,
		SessionID:         sessionID,
		SessionContext:    record.Projection.SessionContext,
		SessionContextSet: record.Projection.SessionContextSet,
	}
	if err := s.writeSessionRecordOnlyLocked(record); err != nil {
		return SessionSummary{}, err
	}
	if err := s.writeSessionContextSidecarLocked(sessionID, sidecar); err != nil {
		return SessionSummary{}, err
	}
	s.cacheSessionMetaLocked(record)
	if err := s.updateSessionIndexLocked(record.Summary); err != nil {
		return SessionSummary{}, err
	}
	return record.Summary, nil
}

func (s *FileStore) GetSessionPermissionRuleSnapshot(ctx context.Context, sessionID string) (SessionPermissionRuleSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SessionPermissionRuleSnapshot{}, ctx.Err()
	default:
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SessionPermissionRuleSnapshot{}, fmt.Errorf("session id is required")
	}
	record, err := s.readSessionWithoutLogEntriesLocked(sessionID)
	if err != nil {
		return SessionPermissionRuleSnapshot{}, err
	}
	sidecar, err := s.readOrRebuildSessionPermissionSidecarLocked(sessionID, record.Projection)
	if err != nil {
		return SessionPermissionRuleSnapshot{}, err
	}
	return SessionPermissionRuleSnapshot{
		SessionID: sidecar.SessionID,
		Enabled:   sidecar.Enabled,
		Items:     append([]PermissionRule(nil), sidecar.Items...),
	}, nil
}

func (s *FileStore) SaveSessionPermissionRuleSnapshot(ctx context.Context, snapshot SessionPermissionRuleSnapshot) (SessionSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SessionSummary{}, ctx.Err()
	default:
	}
	sessionID := strings.TrimSpace(snapshot.SessionID)
	if sessionID == "" {
		return SessionSummary{}, fmt.Errorf("session id is required")
	}
	record, err := s.readSessionWithoutLogEntriesLocked(sessionID)
	if err != nil {
		return SessionSummary{}, err
	}
	record.Projection.PermissionRulesEnabled = snapshot.Enabled
	record.Projection.PermissionRules = normalizePermissionRules(snapshot.Items)
	record.Summary.UpdatedAt = time.Now().UTC()
	sidecar := sessionPermissionSidecar{
		Version:   sessionSideDomainVersion,
		SessionID: sessionID,
		Enabled:   record.Projection.PermissionRulesEnabled,
		Items:     append([]PermissionRule(nil), record.Projection.PermissionRules...),
	}
	if err := s.writeSessionRecordOnlyLocked(record); err != nil {
		return SessionSummary{}, err
	}
	if err := s.writeSessionPermissionSidecarLocked(sessionID, sidecar); err != nil {
		return SessionSummary{}, err
	}
	s.cacheSessionMetaLocked(record)
	if err := s.updateSessionIndexLocked(record.Summary); err != nil {
		return SessionSummary{}, err
	}
	return record.Summary, nil
}

func (s *FileStore) updateSessionIndexLocked(summary SessionSummary) error {
	sessionID := strings.TrimSpace(summary.ID)
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	indexFile, err := s.readIndexLocked()
	if err != nil {
		return err
	}
	updated := false
	for i := range indexFile.Sessions {
		if strings.TrimSpace(indexFile.Sessions[i].ID) == sessionID {
			indexFile.Sessions[i] = summary
			updated = true
			break
		}
	}
	if !updated {
		indexFile.Sessions = append(indexFile.Sessions, summary)
	}
	sort.Slice(indexFile.Sessions, func(i, j int) bool {
		return indexFile.Sessions[i].UpdatedAt.After(indexFile.Sessions[j].UpdatedAt)
	})
	return s.writeIndexLocked(indexFile)
}

func (s *FileStore) GetSessionDiffPage(ctx context.Context, req SessionDiffPageRequest) (SessionDiffPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SessionDiffPage{}, ctx.Err()
	default:
	}
	record, latest, err := s.readProjectionSideDomainRecordLocked(req.SessionID)
	if err != nil {
		return SessionDiffPage{}, err
	}
	diffSidecar, err := s.readOrRebuildSessionDiffSidecarLocked(req.SessionID, sidecarsFromRecord(record).Diff)
	if err != nil {
		return SessionDiffPage{}, err
	}
	applyDiffSidecarToProjection(&record.Projection, diffSidecar)
	diffs, start, total := diffWindow(diffSidecar.Diffs, req.Before, req.Limit)
	return SessionDiffPage{
		Record:    record,
		Latest:    latest,
		Diffs:     diffs,
		DiffStart: start,
		DiffTotal: total,
	}, nil
}

func (s *FileStore) GetSessionTerminalRange(ctx context.Context, req SessionTerminalRangeRequest) (SessionTerminalRange, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SessionTerminalRange{}, ctx.Err()
	default:
	}
	record, latest, err := s.readProjectionSideDomainRecordLocked(req.SessionID)
	if err != nil {
		return SessionTerminalRange{}, err
	}
	terminalSidecar, err := s.readOrRebuildSessionTerminalSidecarLocked(req.SessionID, sidecarsFromRecord(record).Terminal)
	if err != nil {
		return SessionTerminalRange{}, err
	}
	applyTerminalSidecarToProjection(&record.Projection, terminalSidecar)
	stream, err := normalizeTerminalStream(req.Stream)
	if err != nil {
		return SessionTerminalRange{}, err
	}
	content := terminalSidecar.RawTerminalByStream[stream]
	start, end := terminalRangeBounds(content, req.Start, req.Limit)
	return SessionTerminalRange{
		Record:  record,
		Latest:  latest,
		Stream:  stream,
		Start:   start,
		End:     end,
		Total:   len(content),
		Content: content[start:end],
	}, nil
}

func (s *FileStore) GetSessionTerminalExecutionPage(ctx context.Context, req SessionTerminalExecutionPageRequest) (SessionTerminalExecutionPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SessionTerminalExecutionPage{}, ctx.Err()
	default:
	}
	record, latest, err := s.readProjectionSideDomainRecordLocked(req.SessionID)
	if err != nil {
		return SessionTerminalExecutionPage{}, err
	}
	executionSidecar, err := s.readOrRebuildSessionTerminalExecutionSidecarLocked(req.SessionID, sidecarsFromRecord(record).TerminalExecution)
	if err != nil {
		return SessionTerminalExecutionPage{}, err
	}
	applyTerminalExecutionSidecarToProjection(&record.Projection, executionSidecar)
	executions, start, total := terminalExecutionWindow(executionSidecar.TerminalExecutions, req.Before, req.Limit)
	if !req.IncludeOutput {
		executions = terminalExecutionsWithoutOutput(executions)
	}
	return SessionTerminalExecutionPage{
		Record:             record,
		Latest:             latest,
		TerminalExecutions: executions,
		ExecutionStart:     start,
		ExecutionTotal:     total,
		IncludeOutput:      req.IncludeOutput,
	}, nil
}

func (s *FileStore) GetSessionTerminalExecution(ctx context.Context, req SessionTerminalExecutionRequest) (SessionTerminalExecutionSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return SessionTerminalExecutionSnapshot{}, ctx.Err()
	default:
	}
	sessionID := strings.TrimSpace(req.SessionID)
	executionID := strings.TrimSpace(req.ExecutionID)
	if sessionID == "" {
		return SessionTerminalExecutionSnapshot{}, fmt.Errorf("session id is required")
	}
	if executionID == "" {
		return SessionTerminalExecutionSnapshot{}, fmt.Errorf("execution id is required")
	}
	record, latest, err := s.readProjectionSideDomainRecordLocked(sessionID)
	if err != nil {
		return SessionTerminalExecutionSnapshot{}, err
	}
	executionSidecar, err := s.readOrRebuildSessionTerminalExecutionSidecarLocked(sessionID, sidecarsFromRecord(record).TerminalExecution)
	if err != nil {
		return SessionTerminalExecutionSnapshot{}, err
	}
	for _, execution := range executionSidecar.TerminalExecutions {
		if strings.TrimSpace(execution.ExecutionID) != executionID {
			continue
		}
		if !req.IncludeOutput {
			execution.Stdout = ""
			execution.Stderr = ""
		}
		return SessionTerminalExecutionSnapshot{
			SessionID:         sessionID,
			Latest:            latest,
			TerminalExecution: execution,
		}, nil
	}
	return SessionTerminalExecutionSnapshot{}, fmt.Errorf("terminal execution %s not found for session %s", executionID, sessionID)
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
	record, err := s.readSessionWithoutLogEntriesLocked(sessionID)
	if err != nil {
		return err
	}
	if err := os.Remove(s.sessionPath(sessionID)); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("session not found: %s: %w", sessionID, os.ErrNotExist)
		}
		return fmt.Errorf("delete session record: %w", err)
	}
	if err := os.Remove(s.sessionLogEntriesPath(sessionID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete session log entries sidecar: %w", err)
	}
	if err := os.Remove(s.sessionLogEntriesIndexPath(sessionID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete session log entries index: %w", err)
	}
	if err := os.Remove(s.sessionRuntimeMetaPath(sessionID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete session runtime metadata sidecar: %w", err)
	}
	if err := os.Remove(s.sessionContextPath(sessionID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete session context sidecar: %w", err)
	}
	if err := os.Remove(s.sessionPermissionPath(sessionID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete session permission sidecar: %w", err)
	}
	if err := os.Remove(s.sessionDiffsPath(sessionID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete session diff sidecar: %w", err)
	}
	if err := os.Remove(s.sessionTerminalPath(sessionID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete session terminal sidecar: %w", err)
	}
	if err := os.Remove(s.sessionTerminalExecutionsPath(sessionID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete session terminal execution sidecar: %w", err)
	}
	index, err := s.readIndexLocked()
	if err != nil {
		return err
	}
	index.Sessions = filterOut(index.Sessions, sessionID)
	index = addDeletedNativeSessionMarkers(index, nativeDeletionMarkersForSummary(record.Summary))
	if err := s.writeIndexLocked(index); err != nil {
		return err
	}
	delete(s.sessionMetaCache, sessionID)
	return nil
}

func (s *FileStore) DeletedNativeSessionIDs(ctx context.Context) (NativeSessionDeletionSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return NativeSessionDeletionSet{}, ctx.Err()
	default:
	}
	index, err := s.readIndexLocked()
	if err != nil {
		return NativeSessionDeletionSet{}, err
	}
	return nativeDeletionSetFromIndex(index), nil
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
	data, err := json.Marshal(index)
	if err != nil {
		return fmt.Errorf("encode session index: %w", err)
	}
	return writeJSONFileAtomic(s.indexPath, data, 0o644)
}

func (s *FileStore) readSessionLocked(sessionID string) (SessionRecord, error) {
	record, err := s.readSessionFileLocked(sessionID)
	if err != nil {
		return SessionRecord{}, err
	}
	if err := s.hydrateProjectionSideDomainsLocked(&record); err != nil {
		return SessionRecord{}, err
	}
	embeddedEntries := append([]SnapshotLogEntry(nil), record.Projection.LogEntries...)
	entries, err := s.readAllSessionLogEntriesIfAvailableLocked(sessionID)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return SessionRecord{}, err
		}
		if len(embeddedEntries) == 0 && record.Summary.EntryCount > 0 {
			return SessionRecord{}, fmt.Errorf("session log entries sidecar missing for %s: %w", sessionID, os.ErrNotExist)
		}
		entries = dedupeAdjacentLogEntries(embeddedEntries)
		if writeErr := s.writeSessionLocked(SessionRecord{
			Summary:       record.Summary,
			Projection:    projectionWithLogEntries(record.Projection, entries),
			ClientActions: record.ClientActions,
		}); writeErr != nil {
			return SessionRecord{}, writeErr
		}
	} else if len(embeddedEntries) > 0 && len(entries) != len(dedupeAdjacentLogEntries(embeddedEntries)) {
		entries = dedupeAdjacentLogEntries(embeddedEntries)
		if writeErr := s.writeSessionLocked(SessionRecord{
			Summary:       record.Summary,
			Projection:    projectionWithLogEntries(record.Projection, entries),
			ClientActions: record.ClientActions,
		}); writeErr != nil {
			return SessionRecord{}, writeErr
		}
	}
	record.Projection.LogEntries = entries
	return normalizeSessionRecord(record), nil
}

func (s *FileStore) readSessionFileLocked(sessionID string) (SessionRecord, error) {
	data, err := os.ReadFile(s.sessionPath(sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return SessionRecord{}, fmt.Errorf("session not found: %s: %w", sessionID, os.ErrNotExist)
		}
		return SessionRecord{}, fmt.Errorf("read session record: %w", err)
	}
	var record SessionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return SessionRecord{}, fmt.Errorf("decode session record: %w", err)
	}
	return record, nil
}

func (s *FileStore) readSessionWithoutLogEntriesLocked(sessionID string) (SessionRecord, error) {
	file, err := os.Open(s.sessionPath(sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return SessionRecord{}, fmt.Errorf("session not found: %s: %w", sessionID, os.ErrNotExist)
		}
		return SessionRecord{}, fmt.Errorf("read session record: %w", err)
	}
	defer file.Close()
	var record sessionRecordLightweight
	if err := json.NewDecoder(file).Decode(&record); err != nil {
		return SessionRecord{}, fmt.Errorf("decode session record: %w", err)
	}
	return normalizeSessionRecordLightweight(SessionRecord{
		Summary:       record.Summary,
		Projection:    record.Projection.toProjectionSnapshot(),
		ClientActions: record.ClientActions,
	}), nil
}

func (s *FileStore) readSessionIndexRecordLocked(sessionID string) (SessionRecord, error) {
	record, err := s.readSessionFileLocked(sessionID)
	if err != nil {
		return SessionRecord{}, err
	}
	if len(record.Projection.LogEntries) > 0 {
		return normalizeSessionRecord(record), nil
	}
	return normalizeSessionRecordLightweight(record), nil
}

func (s *FileStore) writeSessionLocked(record SessionRecord) error {
	record = normalizeSessionRecord(record)
	entries := append([]SnapshotLogEntry(nil), record.Projection.LogEntries...)
	record.Summary = deriveProjectionSummary(record.Summary, record.Projection)
	sidecars := sidecarsFromRecord(record)
	record.Projection = lightweightProjectionForRecord(record.Projection)
	record.Projection.LogEntries = nil
	if err := s.writeSessionRecordOnlyLocked(record); err != nil {
		return err
	}
	if err := s.writeSessionLogEntriesLocked(record.Summary.ID, entries); err != nil {
		return err
	}
	return s.writeProjectionSidecarsLocked(record.Summary.ID, sidecars)
}

func (s *FileStore) writeSessionRecordOnlyLocked(record SessionRecord) error {
	record.Projection = lightweightProjectionForRecord(record.Projection)
	record.Projection.LogEntries = nil
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode session record: %w", err)
	}
	return writeJSONFileAtomic(s.sessionPath(record.Summary.ID), data, 0o644)
}

func (s *FileStore) readSessionLogEntryWindowLocked(sessionID string, before, limit int) ([]SnapshotLogEntry, int, int, error) {
	requestBefore := before
	index, err := s.readSessionLogEntriesIndexLocked(sessionID)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, 0, 0, err
		}
		record, rebuildErr := s.readSessionFileLocked(sessionID)
		if rebuildErr != nil {
			return nil, 0, 0, rebuildErr
		}
		record = normalizeSessionRecord(record)
		if len(record.Projection.LogEntries) == 0 && record.Summary.EntryCount > 0 {
			return nil, 0, 0, fmt.Errorf("session log entries sidecar missing for %s: %w", sessionID, os.ErrNotExist)
		}
		if writeErr := s.writeSessionLocked(record); writeErr != nil {
			return nil, 0, 0, writeErr
		}
		index, err = s.readSessionLogEntriesIndexLocked(sessionID)
		if err != nil {
			return nil, 0, 0, err
		}
	}
	total := index.EntryCount
	if total < 0 {
		return nil, 0, 0, fmt.Errorf("session log entries index has negative count: %d", total)
	}
	if len(index.Offsets) != total {
		return nil, 0, 0, fmt.Errorf("session log entries index count mismatch: got %d offsets, index count %d", len(index.Offsets), total)
	}
	before = normalizeWindowBefore(before, total)
	start := normalizeWindowStart(before, limit)
	entries, err := s.readSessionLogEntriesRangeLocked(sessionID, index.Offsets, start, before)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			record, rebuildErr := s.readSessionLocked(sessionID)
			if rebuildErr != nil {
				return nil, 0, 0, rebuildErr
			}
			if writeErr := s.writeSessionLogEntriesLocked(sessionID, record.Projection.LogEntries); writeErr != nil {
				return nil, 0, 0, writeErr
			}
			return s.readSessionLogEntryWindowLocked(sessionID, requestBefore, limit)
		}
		return nil, 0, 0, err
	}
	return entries, start, total, nil
}

func (s *FileStore) readAllSessionLogEntriesIfAvailableLocked(sessionID string) ([]SnapshotLogEntry, error) {
	index, err := s.readSessionLogEntriesIndexLocked(sessionID)
	if err != nil {
		return nil, err
	}
	return s.readSessionLogEntriesRangeLocked(sessionID, index.Offsets, 0, index.EntryCount)
}

func (s *FileStore) readSessionLogEntriesIndexLocked(sessionID string) (sessionLogEntriesIndex, error) {
	data, err := os.ReadFile(s.sessionLogEntriesIndexPath(sessionID))
	if err == nil {
		var index sessionLogEntriesIndex
		if decodeErr := json.Unmarshal(data, &index); decodeErr != nil {
			return sessionLogEntriesIndex{}, fmt.Errorf("decode session log entries index: %w", decodeErr)
		}
		if err := validateSessionLogEntriesIndex(sessionID, index); err != nil {
			return sessionLogEntriesIndex{}, err
		}
		if err := s.validateSessionLogEntriesHeaderCountLocked(sessionID, index.EntryCount); err != nil {
			return sessionLogEntriesIndex{}, err
		}
		return index, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return sessionLogEntriesIndex{}, fmt.Errorf("read session log entries index: %w", err)
	}
	entries, legacyErr := s.readLegacySessionLogEntriesSidecarLocked(sessionID)
	if legacyErr != nil {
		if errors.Is(legacyErr, os.ErrNotExist) {
			return sessionLogEntriesIndex{}, os.ErrNotExist
		}
		return sessionLogEntriesIndex{}, legacyErr
	}
	if writeErr := s.writeSessionLogEntriesLocked(sessionID, entries); writeErr != nil {
		return sessionLogEntriesIndex{}, writeErr
	}
	data, err = os.ReadFile(s.sessionLogEntriesIndexPath(sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return sessionLogEntriesIndex{}, os.ErrNotExist
		}
		return sessionLogEntriesIndex{}, fmt.Errorf("read rebuilt session log entries index: %w", err)
	}
	var index sessionLogEntriesIndex
	if decodeErr := json.Unmarshal(data, &index); decodeErr != nil {
		return sessionLogEntriesIndex{}, fmt.Errorf("decode rebuilt session log entries index: %w", decodeErr)
	}
	if err := validateSessionLogEntriesIndex(sessionID, index); err != nil {
		return sessionLogEntriesIndex{}, err
	}
	return index, nil
}

func (s *FileStore) readLegacySessionLogEntriesSidecarLocked(sessionID string) ([]SnapshotLogEntry, error) {
	data, err := os.ReadFile(s.sessionLogEntriesPath(sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("read session log entries sidecar: %w", err)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("session log entries sidecar is empty")
	}
	if trimmed[0] != '{' {
		return nil, os.ErrNotExist
	}
	var sidecar sessionLogEntriesSidecar
	if err := json.Unmarshal(trimmed, &sidecar); err != nil {
		return nil, fmt.Errorf("decode legacy session log entries sidecar: %w", err)
	}
	if strings.TrimSpace(sidecar.SessionID) != "" && strings.TrimSpace(sidecar.SessionID) != strings.TrimSpace(sessionID) {
		return nil, fmt.Errorf("session log entries sidecar belongs to %s, not %s", sidecar.SessionID, sessionID)
	}
	entries := dedupeAdjacentLogEntries(sidecar.LogEntries)
	if sidecar.EntryCount != 0 && sidecar.EntryCount != len(entries) {
		return nil, fmt.Errorf("session log entries sidecar count mismatch: got %d entries, sidecar count %d", len(entries), sidecar.EntryCount)
	}
	return append([]SnapshotLogEntry(nil), entries...), nil
}

func (s *FileStore) readSessionLogEntriesRangeLocked(sessionID string, offsets []int64, start, before int) ([]SnapshotLogEntry, error) {
	if start < 0 || before < start || before > len(offsets) {
		return nil, fmt.Errorf("invalid session log entries window: start=%d before=%d total=%d", start, before, len(offsets))
	}
	if start == before {
		return []SnapshotLogEntry{}, nil
	}
	file, err := os.Open(s.sessionLogEntriesPath(sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("session log entries sidecar missing for %s: %w", sessionID, os.ErrNotExist)
		}
		return nil, fmt.Errorf("read session log entries sidecar: %w", err)
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	headerLine, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read session log entries sidecar header: %w", err)
	}
	var header sessionLogEntriesSidecarHeader
	if err := json.Unmarshal(bytes.TrimSpace(headerLine), &header); err != nil {
		return nil, fmt.Errorf("decode session log entries sidecar header: %w", err)
	}
	if err := validateSessionLogEntriesHeader(sessionID, header); err != nil {
		return nil, err
	}
	if _, err := file.Seek(offsets[start], io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek session log entries sidecar: %w", err)
	}
	reader.Reset(file)
	entries := make([]SnapshotLogEntry, 0, before-start)
	for row := start; row < before; row++ {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("session log entries sidecar ended at row %d before expected %d", row, before)
			}
			return nil, fmt.Errorf("read session log entries sidecar row %d: %w", row, err)
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			return nil, fmt.Errorf("session log entries sidecar ended at row %d before expected %d", row, before)
		}
		var entry SnapshotLogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil, fmt.Errorf("decode session log entry row %d: %w", row, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *FileStore) writeSessionLogEntriesLocked(sessionID string, entries []SnapshotLogEntry) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	entries = dedupeAdjacentLogEntries(entries)
	var buf bytes.Buffer
	header := sessionLogEntriesSidecarHeader{
		Version:    sessionLogEntriesSidecarVersion,
		SessionID:  sessionID,
		EntryCount: len(entries),
	}
	headerData, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("encode session log entries sidecar header: %w", err)
	}
	buf.Write(headerData)
	buf.WriteByte('\n')
	offsets := make([]int64, 0, len(entries))
	for i, entry := range entries {
		offsets = append(offsets, int64(buf.Len()))
		row, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("encode session log entry row %d: %w", i, err)
		}
		buf.Write(row)
		buf.WriteByte('\n')
	}
	if err := writeJSONFileAtomic(s.sessionLogEntriesPath(sessionID), buf.Bytes(), 0o644); err != nil {
		return err
	}
	index := sessionLogEntriesIndex{
		Version:    sessionLogEntriesSidecarVersion,
		SessionID:  sessionID,
		EntryCount: len(entries),
		Offsets:    offsets,
	}
	indexData, err := json.Marshal(index)
	if err != nil {
		return fmt.Errorf("encode session log entries index: %w", err)
	}
	return writeJSONFileAtomic(s.sessionLogEntriesIndexPath(sessionID), indexData, 0o644)
}

func (s *FileStore) readLastSessionLogEntryLocked(sessionID string, index sessionLogEntriesIndex) *SnapshotLogEntry {
	if index.EntryCount <= 0 || len(index.Offsets) != index.EntryCount {
		return nil
	}
	entries, err := s.readSessionLogEntriesRangeLocked(sessionID, index.Offsets, index.EntryCount-1, index.EntryCount)
	if err != nil || len(entries) == 0 {
		return nil
	}
	entry := entries[0]
	return &entry
}

func dedupeAppendLogEntries(previous *SnapshotLogEntry, entries []SnapshotLogEntry) []SnapshotLogEntry {
	if len(entries) == 0 {
		return nil
	}
	normalized := dedupeAdjacentLogEntries(entries)
	out := make([]SnapshotLogEntry, 0, len(normalized))
	for _, entry := range normalized {
		if previous != nil && equivalentAdjacentLogEntry(*previous, entry) {
			continue
		}
		out = append(out, entry)
		previous = &out[len(out)-1]
	}
	return out
}

func (s *FileStore) appendSessionLogEntriesLocked(sessionID string, index sessionLogEntriesIndex, entries []SnapshotLogEntry) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if len(entries) == 0 {
		return nil
	}
	file, err := os.OpenFile(s.sessionLogEntriesPath(sessionID), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("session log entries sidecar missing for %s: %w", sessionID, os.ErrNotExist)
		}
		return fmt.Errorf("open session log entries sidecar for append: %w", err)
	}
	defer file.Close()
	offset, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("seek session log entries sidecar for append: %w", err)
	}
	offsets := append([]int64(nil), index.Offsets...)
	for i, entry := range entries {
		row, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("encode appended session log entry row %d: %w", i, err)
		}
		offsets = append(offsets, offset)
		written, err := file.Write(append(row, '\n'))
		if err != nil {
			return fmt.Errorf("append session log entry row %d: %w", i, err)
		}
		offset += int64(written)
	}
	nextIndex := sessionLogEntriesIndex{
		Version:    sessionLogEntriesSidecarVersion,
		SessionID:  sessionID,
		EntryCount: index.EntryCount + len(entries),
		Offsets:    offsets,
	}
	indexData, err := json.Marshal(nextIndex)
	if err != nil {
		return fmt.Errorf("encode session log entries index: %w", err)
	}
	if err := s.rewriteSessionLogEntriesHeaderLocked(sessionID, nextIndex.EntryCount); err != nil {
		return err
	}
	return writeJSONFileAtomic(s.sessionLogEntriesIndexPath(sessionID), indexData, 0o644)
}

func (s *FileStore) updateRuntimeMetaCountLocked(sessionID string, logEntryCount int) error {
	sidecar, err := s.readSessionRuntimeMetaSidecarLocked(sessionID)
	if err != nil {
		return err
	}
	sidecar.Counts.LogEntryCount = logEntryCount
	return s.writeJSONFileLocked(s.sessionRuntimeMetaPath(sessionID), sidecar, "encode session runtime metadata sidecar")
}

func (s *FileStore) rewriteSessionLogEntriesHeaderLocked(sessionID string, entryCount int) error {
	path := s.sessionLogEntriesPath(sessionID)
	src, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("session log entries sidecar missing for %s: %w", sessionID, os.ErrNotExist)
		}
		return fmt.Errorf("open session log entries sidecar for header rewrite: %w", err)
	}
	defer src.Close()
	reader := bufio.NewReader(src)
	oldHeader, err := reader.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("read session log entries sidecar header: %w", err)
	}
	var existing sessionLogEntriesSidecarHeader
	if err := json.Unmarshal(bytes.TrimSpace(oldHeader), &existing); err != nil {
		return fmt.Errorf("decode session log entries sidecar header: %w", err)
	}
	if err := validateSessionLogEntriesHeader(sessionID, existing); err != nil {
		return err
	}
	header := sessionLogEntriesSidecarHeader{
		Version:    sessionLogEntriesSidecarVersion,
		SessionID:  strings.TrimSpace(sessionID),
		EntryCount: entryCount,
	}
	headerData, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("encode session log entries sidecar header: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp log entries sidecar: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(headerData); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write session log entries sidecar header: %w", err)
	}
	if _, err := tmp.Write([]byte("\n")); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write session log entries sidecar newline: %w", err)
	}
	if _, err := io.Copy(tmp, reader); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("copy session log entries sidecar body: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp log entries sidecar: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp log entries sidecar: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace session log entries sidecar: %w", err)
	}
	cleanup = false
	return nil
}

func writeJSONFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}
	cleanup = false
	return nil
}

func validateSessionLogEntriesIndex(sessionID string, index sessionLogEntriesIndex) error {
	if index.Version != sessionLogEntriesSidecarVersion {
		return fmt.Errorf("unsupported session log entries index version: %d", index.Version)
	}
	if strings.TrimSpace(index.SessionID) != strings.TrimSpace(sessionID) {
		return fmt.Errorf("session log entries index belongs to %s, not %s", index.SessionID, sessionID)
	}
	if index.EntryCount < 0 {
		return fmt.Errorf("session log entries index has negative count: %d", index.EntryCount)
	}
	if len(index.Offsets) != index.EntryCount {
		return fmt.Errorf("session log entries index count mismatch: got %d offsets, index count %d", len(index.Offsets), index.EntryCount)
	}
	var previous int64 = -1
	for i, offset := range index.Offsets {
		if offset < 0 {
			return fmt.Errorf("session log entries index offset %d is negative: %d", i, offset)
		}
		if offset <= previous {
			return fmt.Errorf("session log entries index offset %d is not increasing: %d <= %d", i, offset, previous)
		}
		previous = offset
	}
	return nil
}

func validateSessionLogEntriesHeader(sessionID string, header sessionLogEntriesSidecarHeader) error {
	if header.Version != sessionLogEntriesSidecarVersion {
		return fmt.Errorf("unsupported session log entries sidecar version: %d", header.Version)
	}
	if strings.TrimSpace(header.SessionID) != strings.TrimSpace(sessionID) {
		return fmt.Errorf("session log entries sidecar belongs to %s, not %s", header.SessionID, sessionID)
	}
	return nil
}

func (s *FileStore) validateSessionLogEntriesHeaderCountLocked(sessionID string, expectedCount int) error {
	header, err := s.readSessionLogEntriesHeaderLocked(sessionID)
	if err != nil {
		return err
	}
	if header.EntryCount != expectedCount {
		return fmt.Errorf("session log entries sidecar count mismatch: header count %d, index count %d", header.EntryCount, expectedCount)
	}
	return nil
}

func (s *FileStore) readSessionLogEntriesHeaderLocked(sessionID string) (sessionLogEntriesSidecarHeader, error) {
	file, err := os.Open(s.sessionLogEntriesPath(sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return sessionLogEntriesSidecarHeader{}, fmt.Errorf("session log entries sidecar missing for %s: %w", sessionID, os.ErrNotExist)
		}
		return sessionLogEntriesSidecarHeader{}, fmt.Errorf("read session log entries sidecar: %w", err)
	}
	defer file.Close()
	line, err := bufio.NewReader(file).ReadBytes('\n')
	if err != nil {
		return sessionLogEntriesSidecarHeader{}, fmt.Errorf("read session log entries sidecar header: %w", err)
	}
	var header sessionLogEntriesSidecarHeader
	if err := json.Unmarshal(bytes.TrimSpace(line), &header); err != nil {
		return sessionLogEntriesSidecarHeader{}, fmt.Errorf("decode session log entries sidecar header: %w", err)
	}
	if err := validateSessionLogEntriesHeader(sessionID, header); err != nil {
		return sessionLogEntriesSidecarHeader{}, err
	}
	return header, nil
}

func (s *FileStore) hydrateProjectionSideDomainsLocked(record *SessionRecord) error {
	sidecars, err := s.readSessionProjectionSidecarsLocked(record.Summary.ID)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		record.Projection = normalizeProjection(record.Projection)
		if rebuildErr := s.writeProjectionSidecarsLocked(record.Summary.ID, sidecarsFromRecord(*record)); rebuildErr != nil {
			return rebuildErr
		}
		if rewriteErr := s.writeSessionRecordOnlyLocked(*record); rewriteErr != nil {
			return rewriteErr
		}
		return nil
	}
	applySidecarsToProjection(&record.Projection, sidecars)
	return nil
}

func (s *FileStore) readProjectionSideDomainRecordLocked(sessionID string) (SessionRecord, SessionProjectionCounts, error) {
	record, err := s.readSessionWithoutLogEntriesLocked(sessionID)
	if err != nil {
		return SessionRecord{}, SessionProjectionCounts{}, err
	}
	runtimeMeta, err := s.readOrRebuildSessionRuntimeMetaSidecarLocked(sessionID, record)
	if err != nil {
		return SessionRecord{}, SessionProjectionCounts{}, err
	}
	applyRuntimeMetaSidecarToProjection(&record.Projection, runtimeMeta)
	record.Summary.EntryCount = runtimeMeta.Counts.LogEntryCount
	return record, runtimeMeta.Counts, nil
}

func (s *FileStore) readSessionProjectionSidecarsLocked(sessionID string) (sessionProjectionSidecars, error) {
	record, err := s.readSessionWithoutLogEntriesLocked(sessionID)
	if err != nil {
		return sessionProjectionSidecars{}, err
	}
	sidecars := sidecarsFromRecord(record)
	runtimeMeta, err := s.readOrRebuildSessionRuntimeMetaSidecarLocked(sessionID, record)
	if err != nil {
		return sessionProjectionSidecars{}, err
	}
	contextSidecar, err := s.readOrRebuildSessionContextSidecarLocked(sessionID, record.Projection)
	if err != nil {
		return sessionProjectionSidecars{}, err
	}
	permissionSidecar, err := s.readOrRebuildSessionPermissionSidecarLocked(sessionID, record.Projection)
	if err != nil {
		return sessionProjectionSidecars{}, err
	}
	diffSidecar, err := s.readOrRebuildSessionDiffSidecarLocked(sessionID, sidecars.Diff)
	if err != nil {
		return sessionProjectionSidecars{}, err
	}
	terminalSidecar, err := s.readOrRebuildSessionTerminalSidecarLocked(sessionID, sidecars.Terminal)
	if err != nil {
		return sessionProjectionSidecars{}, err
	}
	executionSidecar, err := s.readOrRebuildSessionTerminalExecutionSidecarLocked(sessionID, sidecars.TerminalExecution)
	if err != nil {
		return sessionProjectionSidecars{}, err
	}
	return sessionProjectionSidecars{
		RuntimeMeta:       runtimeMeta,
		Context:           contextSidecar,
		Permission:        permissionSidecar,
		Diff:              diffSidecar,
		Terminal:          terminalSidecar,
		TerminalExecution: executionSidecar,
	}, nil
}

func (s *FileStore) writeProjectionSidecarsLocked(sessionID string, sidecars sessionProjectionSidecars) error {
	if err := s.writeJSONFileLocked(s.sessionRuntimeMetaPath(sessionID), sidecars.RuntimeMeta, "encode session runtime metadata sidecar"); err != nil {
		return err
	}
	if err := s.writeSessionContextSidecarLocked(sessionID, sidecars.Context); err != nil {
		return err
	}
	if err := s.writeSessionPermissionSidecarLocked(sessionID, sidecars.Permission); err != nil {
		return err
	}
	if err := s.writeJSONFileLocked(s.sessionDiffsPath(sessionID), sidecars.Diff, "encode session diff sidecar"); err != nil {
		return err
	}
	if err := s.writeJSONFileLocked(s.sessionTerminalPath(sessionID), sidecars.Terminal, "encode session terminal sidecar"); err != nil {
		return err
	}
	return s.writeJSONFileLocked(s.sessionTerminalExecutionsPath(sessionID), sidecars.TerminalExecution, "encode session terminal execution sidecar")
}

func (s *FileStore) writeSessionContextSidecarLocked(sessionID string, sidecar sessionContextSidecar) error {
	sidecar.SessionID = strings.TrimSpace(sessionID)
	sidecar.Version = sessionSideDomainVersion
	sidecar.SessionContext = normalizeSessionContext(sidecar.SessionContext)
	sidecar.SessionContextSet = sidecar.SessionContextSet || sidecar.SessionContext.Configured
	return s.writeJSONFileLocked(s.sessionContextPath(sessionID), sidecar, "encode session context sidecar")
}

func (s *FileStore) readOrRebuildSessionRuntimeMetaSidecarLocked(sessionID string, record SessionRecord) (sessionRuntimeMetaSidecar, error) {
	sidecar, err := s.readSessionRuntimeMetaSidecarLocked(sessionID)
	if err == nil {
		return sidecar, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return sessionRuntimeMetaSidecar{}, err
	}
	sidecars, legacyErr := s.sidecarsFromSessionFileLocked(sessionID)
	if legacyErr == nil {
		sidecar = sidecars.RuntimeMeta
	} else {
		sidecar = sidecarsFromRecord(record).RuntimeMeta
	}
	if writeErr := s.writeJSONFileLocked(s.sessionRuntimeMetaPath(sessionID), sidecar, "encode session runtime metadata sidecar"); writeErr != nil {
		return sessionRuntimeMetaSidecar{}, writeErr
	}
	return sidecar, nil
}

func (s *FileStore) readOrRebuildSessionContextSidecarLocked(sessionID string, projection ProjectionSnapshot) (sessionContextSidecar, error) {
	sidecar, err := s.readSessionContextSidecarLocked(sessionID)
	if err == nil {
		return sidecar, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return sessionContextSidecar{}, err
	}
	sidecar = sessionContextSidecarFromProjection(sessionID, projection)
	if writeErr := s.writeSessionContextSidecarLocked(sessionID, sidecar); writeErr != nil {
		return sessionContextSidecar{}, writeErr
	}
	return sidecar, nil
}

func (s *FileStore) readOrRebuildSessionPermissionSidecarLocked(sessionID string, projection ProjectionSnapshot) (sessionPermissionSidecar, error) {
	sidecar, err := s.readSessionPermissionSidecarLocked(sessionID)
	if err == nil {
		return sidecar, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return sessionPermissionSidecar{}, err
	}
	sidecar = sidecarsFromRecord(SessionRecord{
		Summary:    SessionSummary{ID: strings.TrimSpace(sessionID)},
		Projection: projection,
	}).Permission
	if writeErr := s.writeSessionPermissionSidecarLocked(sessionID, sidecar); writeErr != nil {
		return sessionPermissionSidecar{}, writeErr
	}
	return sidecar, nil
}

func (s *FileStore) readOrRebuildSessionDiffSidecarLocked(sessionID string, fallback sessionDiffSidecar) (sessionDiffSidecar, error) {
	sidecar, err := s.readSessionDiffSidecarLocked(sessionID)
	if err == nil {
		return sidecar, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return sessionDiffSidecar{}, err
	}
	if sidecars, legacyErr := s.sidecarsFromSessionFileLocked(sessionID); legacyErr == nil {
		fallback = sidecars.Diff
	}
	fallback.SessionID = strings.TrimSpace(sessionID)
	fallback.Version = sessionSideDomainVersion
	if writeErr := s.writeJSONFileLocked(s.sessionDiffsPath(sessionID), fallback, "encode session diff sidecar"); writeErr != nil {
		return sessionDiffSidecar{}, writeErr
	}
	return fallback, nil
}

func (s *FileStore) readOrRebuildSessionTerminalSidecarLocked(sessionID string, fallback sessionTerminalSidecar) (sessionTerminalSidecar, error) {
	sidecar, err := s.readSessionTerminalSidecarLocked(sessionID)
	if err == nil {
		return sidecar, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return sessionTerminalSidecar{}, err
	}
	if sidecars, legacyErr := s.sidecarsFromSessionFileLocked(sessionID); legacyErr == nil {
		fallback = sidecars.Terminal
	}
	fallback.SessionID = strings.TrimSpace(sessionID)
	fallback.Version = sessionSideDomainVersion
	if fallback.RawTerminalByStream == nil {
		fallback.RawTerminalByStream = map[string]string{"stdout": "", "stderr": ""}
	}
	if writeErr := s.writeJSONFileLocked(s.sessionTerminalPath(sessionID), fallback, "encode session terminal sidecar"); writeErr != nil {
		return sessionTerminalSidecar{}, writeErr
	}
	return fallback, nil
}

func (s *FileStore) readOrRebuildSessionTerminalExecutionSidecarLocked(sessionID string, fallback sessionTerminalExecutionSidecar) (sessionTerminalExecutionSidecar, error) {
	sidecar, err := s.readSessionTerminalExecutionSidecarLocked(sessionID)
	if err == nil {
		return sidecar, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return sessionTerminalExecutionSidecar{}, err
	}
	if sidecars, legacyErr := s.sidecarsFromSessionFileLocked(sessionID); legacyErr == nil {
		fallback = sidecars.TerminalExecution
	}
	fallback.SessionID = strings.TrimSpace(sessionID)
	fallback.Version = sessionSideDomainVersion
	if writeErr := s.writeJSONFileLocked(s.sessionTerminalExecutionsPath(sessionID), fallback, "encode session terminal execution sidecar"); writeErr != nil {
		return sessionTerminalExecutionSidecar{}, writeErr
	}
	return fallback, nil
}

func (s *FileStore) sidecarsFromSessionFileLocked(sessionID string) (sessionProjectionSidecars, error) {
	record, err := s.readSessionFileLocked(sessionID)
	if err != nil {
		return sessionProjectionSidecars{}, err
	}
	record = normalizeSessionRecord(record)
	return sidecarsFromRecord(record), nil
}

func sessionContextSidecarFromProjection(sessionID string, projection ProjectionSnapshot) sessionContextSidecar {
	context := normalizeSessionContext(projection.SessionContext)
	return sessionContextSidecar{
		Version:           sessionSideDomainVersion,
		SessionID:         strings.TrimSpace(sessionID),
		SessionContext:    context,
		SessionContextSet: projection.SessionContextSet || context.Configured,
	}
}

func (s *FileStore) writeSessionPermissionSidecarLocked(sessionID string, sidecar sessionPermissionSidecar) error {
	sidecar.SessionID = strings.TrimSpace(sessionID)
	sidecar.Version = sessionSideDomainVersion
	sidecar.Items = normalizePermissionRules(sidecar.Items)
	return s.writeJSONFileLocked(s.sessionPermissionPath(sessionID), sidecar, "encode session permission sidecar")
}

func (s *FileStore) readSessionRuntimeMetaSidecarLocked(sessionID string) (sessionRuntimeMetaSidecar, error) {
	var sidecar sessionRuntimeMetaSidecar
	if err := s.readStrictJSONFileLocked(s.sessionRuntimeMetaPath(sessionID), &sidecar, "read session runtime metadata sidecar", "decode session runtime metadata sidecar"); err != nil {
		return sessionRuntimeMetaSidecar{}, err
	}
	if err := validateSessionSidecar(sessionID, sidecar.Version, sidecar.SessionID, "runtime metadata"); err != nil {
		return sessionRuntimeMetaSidecar{}, err
	}
	sidecar.Counts.LogEntryCount = latestLogEntryCountFromIndexFallback(sidecar.Counts.LogEntryCount)
	return sidecar, nil
}

func (s *FileStore) readSessionContextSidecarLocked(sessionID string) (sessionContextSidecar, error) {
	var sidecar sessionContextSidecar
	if err := s.readStrictJSONFileLocked(s.sessionContextPath(sessionID), &sidecar, "read session context sidecar", "decode session context sidecar"); err != nil {
		return sessionContextSidecar{}, err
	}
	if err := validateSessionSidecar(sessionID, sidecar.Version, sidecar.SessionID, "context"); err != nil {
		return sessionContextSidecar{}, err
	}
	sidecar.SessionContext = normalizeSessionContext(sidecar.SessionContext)
	return sidecar, nil
}

func (s *FileStore) readSessionPermissionSidecarLocked(sessionID string) (sessionPermissionSidecar, error) {
	var sidecar sessionPermissionSidecar
	if err := s.readStrictJSONFileLocked(s.sessionPermissionPath(sessionID), &sidecar, "read session permission sidecar", "decode session permission sidecar"); err != nil {
		return sessionPermissionSidecar{}, err
	}
	if err := validateSessionSidecar(sessionID, sidecar.Version, sidecar.SessionID, "permission"); err != nil {
		return sessionPermissionSidecar{}, err
	}
	sidecar.Items = normalizePermissionRules(sidecar.Items)
	return sidecar, nil
}

func (s *FileStore) readSessionDiffSidecarLocked(sessionID string) (sessionDiffSidecar, error) {
	var sidecar sessionDiffSidecar
	if err := s.readStrictJSONFileLocked(s.sessionDiffsPath(sessionID), &sidecar, "read session diff sidecar", "decode session diff sidecar"); err != nil {
		return sessionDiffSidecar{}, err
	}
	if err := validateSessionSidecar(sessionID, sidecar.Version, sidecar.SessionID, "diff"); err != nil {
		return sessionDiffSidecar{}, err
	}
	return sidecar, nil
}

func (s *FileStore) readSessionTerminalSidecarLocked(sessionID string) (sessionTerminalSidecar, error) {
	var sidecar sessionTerminalSidecar
	if err := s.readStrictJSONFileLocked(s.sessionTerminalPath(sessionID), &sidecar, "read session terminal sidecar", "decode session terminal sidecar"); err != nil {
		return sessionTerminalSidecar{}, err
	}
	if err := validateSessionSidecar(sessionID, sidecar.Version, sidecar.SessionID, "terminal"); err != nil {
		return sessionTerminalSidecar{}, err
	}
	if sidecar.RawTerminalByStream == nil {
		sidecar.RawTerminalByStream = map[string]string{"stdout": "", "stderr": ""}
	}
	if _, ok := sidecar.RawTerminalByStream["stdout"]; !ok {
		sidecar.RawTerminalByStream["stdout"] = ""
	}
	if _, ok := sidecar.RawTerminalByStream["stderr"]; !ok {
		sidecar.RawTerminalByStream["stderr"] = ""
	}
	return sidecar, nil
}

func (s *FileStore) readSessionTerminalExecutionSidecarLocked(sessionID string) (sessionTerminalExecutionSidecar, error) {
	var sidecar sessionTerminalExecutionSidecar
	if err := s.readStrictJSONFileLocked(s.sessionTerminalExecutionsPath(sessionID), &sidecar, "read session terminal execution sidecar", "decode session terminal execution sidecar"); err != nil {
		return sessionTerminalExecutionSidecar{}, err
	}
	if err := validateSessionSidecar(sessionID, sidecar.Version, sidecar.SessionID, "terminal execution"); err != nil {
		return sessionTerminalExecutionSidecar{}, err
	}
	if sidecar.TerminalExecutions == nil {
		sidecar.TerminalExecutions = []TerminalExecution{}
	}
	return sidecar, nil
}

func validateSessionSidecar(sessionID string, version int, sidecarSessionID, domain string) error {
	if version == 0 && strings.TrimSpace(sidecarSessionID) == "" {
		return fmt.Errorf("session %s sidecar missing for %s: %w", domain, sessionID, os.ErrNotExist)
	}
	if version != sessionSideDomainVersion {
		return fmt.Errorf("unsupported session %s sidecar version: %d", domain, version)
	}
	if strings.TrimSpace(sidecarSessionID) != strings.TrimSpace(sessionID) {
		return fmt.Errorf("session %s sidecar belongs to %s, not %s", domain, sidecarSessionID, sessionID)
	}
	return nil
}

func latestLogEntryCountFromIndexFallback(count int) int {
	if count < 0 {
		return 0
	}
	return count
}

func sidecarsFromRecord(record SessionRecord) sessionProjectionSidecars {
	sessionID := record.Summary.ID
	latest := projectionCounts(record.Projection, record.Summary.EntryCount)
	return sessionProjectionSidecars{
		RuntimeMeta: sessionRuntimeMetaSidecar{
			Version:            sessionSideDomainVersion,
			SessionID:          sessionID,
			Controller:         record.Projection.Controller,
			Runtime:            record.Projection.Runtime,
			ContextWindowUsage: record.Projection.ContextWindowUsage,
			CurrentStep:        cloneSnapshotContext(record.Projection.CurrentStep),
			LatestError:        cloneSnapshotContext(record.Projection.LatestError),
			SkillCatalogMeta:   record.Projection.SkillCatalogMeta,
			MemoryCatalogMeta:  record.Projection.MemoryCatalogMeta,
			Counts:             latest,
		},
		Context: sessionContextSidecar{
			Version:           sessionSideDomainVersion,
			SessionID:         sessionID,
			SessionContext:    record.Projection.SessionContext,
			SessionContextSet: record.Projection.SessionContextSet,
		},
		Permission: sessionPermissionSidecar{
			Version:   sessionSideDomainVersion,
			SessionID: sessionID,
			Enabled:   record.Projection.PermissionRulesEnabled,
			Items:     append([]PermissionRule(nil), record.Projection.PermissionRules...),
		},
		Diff: sessionDiffSidecar{
			Version:           sessionSideDomainVersion,
			SessionID:         sessionID,
			Diffs:             append([]DiffContext(nil), record.Projection.Diffs...),
			CurrentDiff:       cloneDiffContext(record.Projection.CurrentDiff),
			ReviewGroups:      append([]ReviewGroup(nil), record.Projection.ReviewGroups...),
			ActiveReviewGroup: cloneReviewGroup(record.Projection.ActiveReviewGroup),
		},
		Terminal: sessionTerminalSidecar{
			Version:             sessionSideDomainVersion,
			SessionID:           sessionID,
			RawTerminalByStream: cloneStringMap(record.Projection.RawTerminalByStream),
		},
		TerminalExecution: sessionTerminalExecutionSidecar{
			Version:            sessionSideDomainVersion,
			SessionID:          sessionID,
			TerminalExecutions: append([]TerminalExecution(nil), record.Projection.TerminalExecutions...),
		},
	}
}

func lightweightProjectionForRecord(projection ProjectionSnapshot) ProjectionSnapshot {
	projection.Diffs = nil
	projection.CurrentDiff = nil
	projection.ReviewGroups = nil
	projection.ActiveReviewGroup = nil
	projection.LogEntries = nil
	projection.RawTerminalByStream = nil
	projection.TerminalExecutions = nil
	return projection
}

func applySidecarsToProjection(projection *ProjectionSnapshot, sidecars sessionProjectionSidecars) {
	applyRuntimeMetaSidecarToProjection(projection, sidecars.RuntimeMeta)
	applyContextSidecarToProjection(projection, sidecars.Context)
	applyPermissionSidecarToProjection(projection, sidecars.Permission)
	applyDiffSidecarToProjection(projection, sidecars.Diff)
	applyTerminalSidecarToProjection(projection, sidecars.Terminal)
	applyTerminalExecutionSidecarToProjection(projection, sidecars.TerminalExecution)
	*projection = normalizeProjection(*projection)
}

func applyRuntimeMetaSidecarToProjection(projection *ProjectionSnapshot, sidecar sessionRuntimeMetaSidecar) {
	projection.Controller = sidecar.Controller
	projection.Runtime = sidecar.Runtime
	projection.ContextWindowUsage = normalizeContextWindowUsage(sidecar.ContextWindowUsage)
	projection.CurrentStep = cloneSnapshotContext(sidecar.CurrentStep)
	projection.LatestError = cloneSnapshotContext(sidecar.LatestError)
	projection.SkillCatalogMeta = normalizeCatalogMetadata(sidecar.SkillCatalogMeta, CatalogDomainSkill)
	projection.MemoryCatalogMeta = normalizeCatalogMetadata(sidecar.MemoryCatalogMeta, CatalogDomainMemory)
}

func applyContextSidecarToProjection(projection *ProjectionSnapshot, sidecar sessionContextSidecar) {
	projection.SessionContext = normalizeSessionContext(sidecar.SessionContext)
	projection.SessionContextSet = sidecar.SessionContextSet
}

func applyPermissionSidecarToProjection(projection *ProjectionSnapshot, sidecar sessionPermissionSidecar) {
	projection.PermissionRulesEnabled = sidecar.Enabled
	projection.PermissionRules = normalizePermissionRules(sidecar.Items)
}

func applyDiffSidecarToProjection(projection *ProjectionSnapshot, sidecar sessionDiffSidecar) {
	projection.Diffs = append([]DiffContext(nil), sidecar.Diffs...)
	projection.CurrentDiff = cloneDiffContext(sidecar.CurrentDiff)
	projection.ReviewGroups = append([]ReviewGroup(nil), sidecar.ReviewGroups...)
	projection.ActiveReviewGroup = cloneReviewGroup(sidecar.ActiveReviewGroup)
}

func applyTerminalSidecarToProjection(projection *ProjectionSnapshot, sidecar sessionTerminalSidecar) {
	projection.RawTerminalByStream = cloneStringMap(sidecar.RawTerminalByStream)
}

func applyTerminalExecutionSidecarToProjection(projection *ProjectionSnapshot, sidecar sessionTerminalExecutionSidecar) {
	projection.TerminalExecutions = append([]TerminalExecution(nil), sidecar.TerminalExecutions...)
}

func cloneSnapshotContext(input *SnapshotContext) *SnapshotContext {
	if input == nil {
		return nil
	}
	out := *input
	return &out
}

func cloneDiffContext(input *DiffContext) *DiffContext {
	if input == nil {
		return nil
	}
	out := *input
	return &out
}

func cloneReviewGroup(input *ReviewGroup) *ReviewGroup {
	if input == nil {
		return nil
	}
	out := *input
	out.Files = append([]ReviewFile(nil), input.Files...)
	return &out
}

func cloneStringMap(input map[string]string) map[string]string {
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	if out == nil {
		out = map[string]string{}
	}
	return out
}

func normalizeWindowBefore(before, total int) int {
	if before <= 0 || before > total {
		return total
	}
	return before
}

func normalizeWindowStart(before, limit int) int {
	if before <= 0 {
		return 0
	}
	if limit <= 0 || limit >= before {
		return 0
	}
	return before - limit
}

func projectionCounts(projection ProjectionSnapshot, logEntryCount int) SessionProjectionCounts {
	if logEntryCount < 0 {
		logEntryCount = 0
	}
	projection = normalizeProjection(projection)
	return SessionProjectionCounts{
		LogEntryCount:          logEntryCount,
		DiffCount:              len(projection.Diffs),
		TerminalExecutionCount: len(projection.TerminalExecutions),
		TerminalStdoutLength:   len(projection.RawTerminalByStream["stdout"]),
		TerminalStderrLength:   len(projection.RawTerminalByStream["stderr"]),
	}
}

func diffWindow(entries []DiffContext, before, limit int) ([]DiffContext, int, int) {
	total := len(entries)
	before = normalizeWindowBefore(before, total)
	start := normalizeWindowStart(before, limit)
	return append([]DiffContext(nil), entries[start:before]...), start, total
}

func terminalExecutionWindow(entries []TerminalExecution, before, limit int) ([]TerminalExecution, int, int) {
	total := len(entries)
	before = normalizeWindowBefore(before, total)
	start := normalizeWindowStart(before, limit)
	return append([]TerminalExecution(nil), entries[start:before]...), start, total
}

func terminalExecutionsWithoutOutput(entries []TerminalExecution) []TerminalExecution {
	if len(entries) == 0 {
		return entries
	}
	out := make([]TerminalExecution, 0, len(entries))
	for _, entry := range entries {
		entry.Stdout = ""
		entry.Stderr = ""
		out = append(out, entry)
	}
	return out
}

func normalizeTerminalStream(stream string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(stream)) {
	case "", "stdout":
		return "stdout", nil
	case "stderr":
		return "stderr", nil
	default:
		return "", fmt.Errorf("unsupported terminal stream %q", stream)
	}
}

func terminalRangeBounds(content string, start, limit int) (int, int) {
	total := len(content)
	if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}
	if limit <= 0 {
		limit = 64 * 1024
	}
	if limit > 512*1024 {
		limit = 512 * 1024
	}
	end := start + limit
	if end > total {
		end = total
	}
	for start < total && !utf8.RuneStart(content[start]) {
		start++
	}
	for end > start && end < total && !utf8.RuneStart(content[end]) {
		end--
	}
	if end < start {
		end = start
	}
	if end == start && start < total {
		_, size := utf8.DecodeRuneInString(content[start:])
		end = start + size
	}
	return start, end
}

func projectionWithLogEntries(projection ProjectionSnapshot, entries []SnapshotLogEntry) ProjectionSnapshot {
	projection.LogEntries = append([]SnapshotLogEntry(nil), entries...)
	return projection
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

func (s *FileStore) readStrictJSONFileLocked(path string, target any, readErrLabel, decodeErrLabel string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s: %w", readErrLabel, os.ErrNotExist)
		}
		return fmt.Errorf("%s: %w", readErrLabel, err)
	}
	if len(data) == 0 {
		return fmt.Errorf("%s: empty file", decodeErrLabel)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("%s: %w", decodeErrLabel, err)
	}
	return nil
}

func (s *FileStore) writeJSONFileLocked(path string, value any, encodeErrLabel string) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("%s: %w", encodeErrLabel, err)
	}
	return writeJSONFileAtomic(path, data, 0o644)
}

func (projection projectionLightweightJSON) toProjectionSnapshot() ProjectionSnapshot {
	return ProjectionSnapshot{
		CurrentStep:            projection.CurrentStep,
		LatestError:            projection.LatestError,
		Controller:             projection.Controller,
		Runtime:                projection.Runtime,
		ContextWindowUsage:     projection.ContextWindowUsage,
		SessionContext:         projection.SessionContext,
		SessionContextSet:      projection.SessionContextSet,
		PermissionRulesEnabled: projection.PermissionRulesEnabled,
		PermissionRules:        projection.PermissionRules,
		SkillCatalogMeta:       projection.SkillCatalogMeta,
		MemoryCatalogMeta:      projection.MemoryCatalogMeta,
	}
}

func (s *FileStore) sessionPath(sessionID string) string {
	return filepath.Join(s.baseDir, sessionID+".json")
}

func (s *FileStore) sessionLogEntriesPath(sessionID string) string {
	return filepath.Join(s.baseDir, sessionID+".log_entries.json")
}

func (s *FileStore) sessionLogEntriesIndexPath(sessionID string) string {
	return filepath.Join(s.baseDir, sessionID+".log_entries.idx.json")
}

func (s *FileStore) sessionRuntimeMetaPath(sessionID string) string {
	return filepath.Join(s.baseDir, sessionID+".runtime_meta.json")
}

func (s *FileStore) sessionContextPath(sessionID string) string {
	return filepath.Join(s.baseDir, sessionID+".context.json")
}

func (s *FileStore) sessionPermissionPath(sessionID string) string {
	return filepath.Join(s.baseDir, sessionID+".permission.json")
}

func (s *FileStore) sessionDiffsPath(sessionID string) string {
	return filepath.Join(s.baseDir, sessionID+".diffs.json")
}

func (s *FileStore) sessionTerminalPath(sessionID string) string {
	return filepath.Join(s.baseDir, sessionID+".terminal.json")
}

func (s *FileStore) sessionTerminalExecutionsPath(sessionID string) string {
	return filepath.Join(s.baseDir, sessionID+".terminal_executions.json")
}

func (s *FileStore) BaseDir() string {
	if s == nil {
		return ""
	}
	return s.baseDir
}

func (s *FileStore) MarkClientAction(ctx context.Context, sessionID string, record ClientActionRecord, ttl time.Duration, limit int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}
	sessionID = strings.TrimSpace(sessionID)
	record.ClientActionID = strings.TrimSpace(record.ClientActionID)
	record.Action = strings.TrimSpace(record.Action)
	record.Status = strings.TrimSpace(record.Status)
	if sessionID == "" {
		return false, fmt.Errorf("session id is required")
	}
	if record.ClientActionID == "" {
		return false, fmt.Errorf("client action id is required")
	}
	if record.Status == "" {
		record.Status = "accepted"
	}
	now := time.Now().UTC()
	if record.AckedAt.IsZero() {
		record.AckedAt = now
	}
	if limit <= 0 {
		limit = 500
	}
	sessionRecord, err := s.readSessionWithoutLogEntriesLocked(sessionID)
	if err != nil {
		return false, err
	}
	sessionRecord.ClientActions = normalizeClientActionRecords(sessionRecord.ClientActions, now, ttl, limit)
	for _, item := range sessionRecord.ClientActions {
		if item.ClientActionID == record.ClientActionID {
			return true, nil
		}
	}
	sessionRecord.ClientActions = append(sessionRecord.ClientActions, record)
	sessionRecord.ClientActions = normalizeClientActionRecords(sessionRecord.ClientActions, now, ttl, limit)
	if err := s.writeSessionRecordOnlyLocked(sessionRecord); err != nil {
		return false, err
	}
	s.cacheSessionMetaLocked(sessionRecord)
	return false, nil
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

type nativeDeletionMarkers struct {
	ClaudeSessionIDs []string
	CodexThreadIDs   []string
}

func nativeDeletionMarkersForSummary(summary SessionSummary) nativeDeletionMarkers {
	if isExternalNativeSummary(summary) {
		return nativeDeletionMarkers{}
	}
	runtime := mergeSessionRuntime(summary.Runtime, SessionRuntime{})
	markers := nativeDeletionMarkers{}
	if isClaudeRuntimeSummary(summary, runtime) {
		markers.ClaudeSessionIDs = appendIfNonEmpty(markers.ClaudeSessionIDs, summary.ClaudeSessionUUID)
		markers.ClaudeSessionIDs = appendIfNonEmpty(markers.ClaudeSessionIDs, runtime.ResumeSessionID)
	}
	if isCodexRuntimeSummary(summary, runtime) {
		markers.CodexThreadIDs = appendIfNonEmpty(markers.CodexThreadIDs, runtime.ResumeSessionID)
	}
	return markers
}

func isExternalNativeSummary(summary SessionSummary) bool {
	source := strings.TrimSpace(strings.ToLower(summary.Source))
	ownership := strings.TrimSpace(strings.ToLower(summary.Ownership))
	runtimeSource := strings.TrimSpace(strings.ToLower(summary.Runtime.Source))
	return summary.External ||
		source == "claude-native" ||
		source == "codex-native" ||
		ownership == "claude-native" ||
		ownership == "codex-native" ||
		runtimeSource == "claude-native" ||
		runtimeSource == "codex-native"
}

func isClaudeRuntimeSummary(summary SessionSummary, runtime SessionRuntime) bool {
	if strings.TrimSpace(summary.ClaudeSessionUUID) != "" {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(runtime.Engine), "claude") ||
		strings.EqualFold(strings.TrimSpace(runtime.Source), "claude-native") {
		return true
	}
	return commandHeadIs(runtime.Command, "claude")
}

func isCodexRuntimeSummary(summary SessionSummary, runtime SessionRuntime) bool {
	if strings.EqualFold(strings.TrimSpace(runtime.Engine), "codex") ||
		strings.EqualFold(strings.TrimSpace(runtime.Source), "codex-native") {
		return true
	}
	return commandHeadIs(runtime.Command, "codex")
}

func commandHeadIs(command, want string) bool {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return false
	}
	head := strings.ToLower(filepath.Base(fields[0]))
	return head == want || head == want+".exe"
}

func appendIfNonEmpty(items []string, value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return items
	}
	return append(items, trimmed)
}

func addDeletedNativeSessionMarkers(index fileIndex, markers nativeDeletionMarkers) fileIndex {
	index.DeletedNativeClaude = mergeStringSet(index.DeletedNativeClaude, markers.ClaudeSessionIDs)
	index.DeletedNativeCodex = mergeStringSet(index.DeletedNativeCodex, markers.CodexThreadIDs)
	return index
}

func mergeStringSet(existing, added []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(added))
	out := make([]string, 0, len(existing)+len(added))
	for _, value := range append(append([]string{}, existing...), added...) {
		trimmed := strings.TrimSpace(value)
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

func nativeDeletionSetFromIndex(index fileIndex) NativeSessionDeletionSet {
	return NativeSessionDeletionSet{
		ClaudeSessionIDs: stringSet(index.DeletedNativeClaude),
		CodexThreadIDs:   stringSet(index.DeletedNativeCodex),
	}
}

func stringSet(items []string) map[string]struct{} {
	result := make(map[string]struct{}, len(items))
	for _, value := range items {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result[trimmed] = struct{}{}
		}
	}
	return result
}

func sessionMetaFromRecord(record SessionRecord) sessionRecordMeta {
	record = normalizeSessionRecord(record)
	return sessionRecordMeta{
		Summary:           record.Summary,
		ClientActions:     append([]ClientActionRecord(nil), record.ClientActions...),
		SessionContext:    record.Projection.SessionContext,
		SessionContextSet: record.Projection.SessionContextSet || record.Projection.SessionContext.Configured,
	}
}

func sessionMetaFromLightweightRecord(record SessionRecord) sessionRecordMeta {
	record = normalizeSessionRecordLightweight(record)
	return sessionRecordMeta{
		Summary:           record.Summary,
		ClientActions:     append([]ClientActionRecord(nil), record.ClientActions...),
		SessionContext:    record.Projection.SessionContext,
		SessionContextSet: record.Projection.SessionContextSet || record.Projection.SessionContext.Configured,
	}
}

func (s *FileStore) cacheSessionMetaLocked(record SessionRecord) {
	if s.sessionMetaCache == nil {
		s.sessionMetaCache = make(map[string]sessionRecordMeta)
	}
	meta := sessionMetaFromRecord(record)
	if len(record.Projection.LogEntries) == 0 && record.Summary.EntryCount > 0 {
		meta = sessionMetaFromLightweightRecord(record)
	}
	sessionID := strings.TrimSpace(meta.Summary.ID)
	if sessionID == "" {
		return
	}
	s.sessionMetaCache[sessionID] = meta
}

func (s *FileStore) sessionMetaLocked(sessionID string) (sessionRecordMeta, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || s.sessionMetaCache == nil {
		return sessionRecordMeta{}, false
	}
	meta, ok := s.sessionMetaCache[sessionID]
	if !ok {
		return sessionRecordMeta{}, false
	}
	meta.ClientActions = append([]ClientActionRecord(nil), meta.ClientActions...)
	return meta, true
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
	projection.LogEntries = dedupeAdjacentLogEntries(projection.LogEntries)
	if projection.TerminalExecutions == nil {
		projection.TerminalExecutions = []TerminalExecution{}
	}
	if projection.ReviewGroups == nil {
		projection.ReviewGroups = []ReviewGroup{}
	}
	if projection.PermissionRules == nil {
		projection.PermissionRules = []PermissionRule{}
	}
	projection.PermissionRules = normalizePermissionRules(projection.PermissionRules)
	projection.ContextWindowUsage = normalizeContextWindowUsage(projection.ContextWindowUsage)
	projection.SessionContext = normalizeSessionContext(projection.SessionContext)
	projection.SkillCatalogMeta = normalizeCatalogMetadata(projection.SkillCatalogMeta, CatalogDomainSkill)
	projection.MemoryCatalogMeta = normalizeCatalogMetadata(projection.MemoryCatalogMeta, CatalogDomainMemory)
	return projection
}

func dedupeAdjacentLogEntries(entries []SnapshotLogEntry) []SnapshotLogEntry {
	if len(entries) < 2 {
		return entries
	}
	out := make([]SnapshotLogEntry, 0, len(entries))
	for _, entry := range entries {
		if len(out) > 0 && equivalentAdjacentLogEntry(out[len(out)-1], entry) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func equivalentAdjacentLogEntry(left SnapshotLogEntry, right SnapshotLogEntry) bool {
	if left.Kind != right.Kind {
		return false
	}
	switch left.Kind {
	case "markdown", "system", "user":
		return displayLogEntryDedupeKey(left) == displayLogEntryDedupeKey(right)
	default:
		return snapshotLogEntryDedupeKey(left) == snapshotLogEntryDedupeKey(right)
	}
}

func displayLogEntryDedupeKey(entry SnapshotLogEntry) string {
	return strings.Join([]string{
		entry.Kind,
		logEntryDisplayTimestamp(entry),
		normalizeLogEntryText(logEntryDisplayText(entry)),
	}, "\x1f")
}

func snapshotLogEntryDedupeKey(entry SnapshotLogEntry) string {
	exitCode := ""
	if entry.ExitCode != nil {
		exitCode = fmt.Sprintf("%d", *entry.ExitCode)
	}
	contextFields := []string{"", "", "", "", "", "", "", "", "", "", ""}
	if entry.Context != nil {
		contextFields = []string{
			entry.Context.ID,
			entry.Context.Type,
			entry.Context.Path,
			entry.Context.Title,
			entry.Context.Message,
			entry.Context.Target,
			entry.Context.TargetPath,
			entry.Context.Tool,
			entry.Context.Command,
			entry.Context.Source,
			entry.Context.ExecutionID,
		}
	}
	fields := []string{
		entry.Kind,
		logEntryDisplayTimestamp(entry),
		normalizeLogEntryText(logEntryDisplayText(entry)),
		entry.Label,
		entry.Stream,
		entry.ExecutionID,
		entry.Phase,
		exitCode,
		snapshotLogEntryAttachmentDedupeKey(entry.Attachments),
	}
	fields = append(fields, contextFields...)
	return strings.Join(fields, "\x1f")
}

func snapshotLogEntryAttachmentDedupeKey(attachments []protocol.TimelineAttachment) string {
	if len(attachments) == 0 {
		return ""
	}
	fields := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		fields = append(fields, strings.Join([]string{
			attachment.ID,
			attachment.Kind,
			attachment.Name,
			attachment.MIMEType,
			fmt.Sprintf("%d", attachment.Size),
			attachment.Path,
			attachment.PreviewStatus,
			attachment.Source,
		}, "\x1e"))
	}
	return strings.Join(fields, "\x1d")
}

func logEntryDisplayTimestamp(entry SnapshotLogEntry) string {
	contextTimestamp := ""
	if entry.Context != nil {
		contextTimestamp = strings.TrimSpace(entry.Context.Timestamp)
	}
	return firstNonEmptyString(strings.TrimSpace(entry.Timestamp), contextTimestamp)
}

func logEntryDisplayText(entry SnapshotLogEntry) string {
	text := firstNonEmptyString(entry.Message, entry.Text)
	if text == "" && entry.Context != nil {
		text = entry.Context.Message
	}
	return text
}

func normalizeLogEntryText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func normalizeSessionRecord(record SessionRecord) SessionRecord {
	record.Projection = normalizeProjection(record.Projection)
	record.ClientActions = normalizeClientActionRecords(record.ClientActions, time.Time{}, 0, 0)
	nativeSource := inferNativeSource(record)
	defaultSource := "mobilevc"
	if nativeSource != "" {
		record.Summary.External = true
		defaultSource = nativeSource
	}
	if record.Summary.Ownership == "" {
		if nativeSource != "" {
			record.Summary.Ownership = nativeSource
		} else if !record.Summary.External {
			record.Summary.Ownership = "mobilevc"
		}
	} else if nativeSource != "" && nativeSourceFromValue(record.Summary.Ownership) == "" {
		if strings.EqualFold(strings.TrimSpace(record.Summary.Ownership), "claude-native") {
			record.Summary.Ownership = nativeSource
		}
	}
	record.Projection.Runtime = mergeSessionRuntime(record.Summary.Runtime, record.Projection.Runtime)
	if record.Projection.Runtime.Source == "" {
		record.Projection.Runtime.Source = defaultSource
	}
	record.Summary.Runtime = mergeSessionRuntime(record.Summary.Runtime, record.Projection.Runtime)
	if record.Summary.Runtime.Source == "" {
		record.Summary.Runtime.Source = defaultSource
	}
	if record.Summary.Source == "" {
		record.Summary.Source = defaultSource
	}
	// ExecutionActive latches: true when controller state is non-IDLE
	// (THINKING, RUNNING_TOOL, WAIT_INPUT), false only when IDLE/empty.
	controllerState := strings.TrimSpace(string(record.Projection.Controller.State))
	if controllerState == "" || controllerState == string(ControllerStateIdle) {
		record.Summary.ExecutionActive = false
	} else {
		record.Summary.ExecutionActive = true
	}
	record.Summary = deriveProjectionSummary(record.Summary, record.Projection)
	return record
}

func normalizeSessionRecordLightweight(record SessionRecord) SessionRecord {
	record.Projection = normalizeProjection(record.Projection)
	record.Projection.LogEntries = nil
	record.ClientActions = normalizeClientActionRecords(record.ClientActions, time.Time{}, 0, 0)
	nativeSource := inferNativeSource(record)
	defaultSource := "mobilevc"
	if nativeSource != "" {
		record.Summary.External = true
		defaultSource = nativeSource
	}
	if record.Summary.Ownership == "" {
		if nativeSource != "" {
			record.Summary.Ownership = nativeSource
		} else if !record.Summary.External {
			record.Summary.Ownership = "mobilevc"
		}
	} else if nativeSource != "" && nativeSourceFromValue(record.Summary.Ownership) == "" {
		if strings.EqualFold(strings.TrimSpace(record.Summary.Ownership), "claude-native") {
			record.Summary.Ownership = nativeSource
		}
	}
	record.Projection.Runtime = mergeSessionRuntime(record.Summary.Runtime, record.Projection.Runtime)
	if record.Projection.Runtime.Source == "" {
		record.Projection.Runtime.Source = defaultSource
	}
	record.Summary.Runtime = mergeSessionRuntime(record.Summary.Runtime, record.Projection.Runtime)
	if record.Summary.Runtime.Source == "" {
		record.Summary.Runtime.Source = defaultSource
	}
	if record.Summary.Source == "" {
		record.Summary.Source = defaultSource
	}
	controllerState := strings.TrimSpace(string(record.Projection.Controller.State))
	if controllerState == "" || controllerState == string(ControllerStateIdle) {
		record.Summary.ExecutionActive = false
	} else {
		record.Summary.ExecutionActive = true
	}
	return record
}

func inferNativeSource(record SessionRecord) string {
	if isMobileVCOwnedSummary(record.Summary) {
		return ""
	}
	if source := nativeSourceFromValue(record.Summary.Source); source != "" {
		return source
	}
	if source := nativeSourceFromValue(record.Summary.Runtime.Source); source != "" {
		return source
	}
	if source := nativeSourceFromValue(record.Projection.Runtime.Source); source != "" {
		return source
	}
	if source := nativeSourceFromValue(record.Projection.Controller.ActiveMeta.Source); source != "" {
		return source
	}
	if source := nativeSourceFromValue(record.Summary.Ownership); source != "" {
		return source
	}

	sessionID := strings.TrimSpace(record.Summary.ID)
	if strings.HasPrefix(sessionID, "codex-thread:") {
		return "codex-native"
	}
	if strings.HasPrefix(sessionID, "claude-session:") {
		return "claude-native"
	}

	if !record.Summary.External {
		return ""
	}
	runtime := mergeSessionRuntime(record.Summary.Runtime, record.Projection.Runtime)
	engine := strings.ToLower(strings.TrimSpace(runtime.Engine))
	command := strings.ToLower(strings.TrimSpace(firstNonEmptyString(
		runtime.Command,
		record.Projection.Controller.CurrentCommand,
		record.Projection.Controller.ActiveMeta.Command,
	)))
	if engine == "codex" || command == "codex" || strings.HasPrefix(command, "codex ") {
		return "codex-native"
	}
	if engine == "claude" || command == "claude" || strings.HasPrefix(command, "claude ") {
		return "claude-native"
	}
	return ""
}

func nativeSourceFromValue(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "codex-native":
		return "codex-native"
	case "claude-native":
		return "claude-native"
	default:
		return ""
	}
}

func isMobileVCOwnedSummary(summary SessionSummary) bool {
	return strings.EqualFold(strings.TrimSpace(summary.Ownership), "mobilevc") ||
		strings.EqualFold(strings.TrimSpace(summary.Source), "mobilevc") ||
		strings.EqualFold(strings.TrimSpace(summary.Runtime.Source), "mobilevc")
}

func normalizeClientActionRecords(items []ClientActionRecord, now time.Time, ttl time.Duration, limit int) []ClientActionRecord {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	cutoff := time.Time{}
	if ttl > 0 && !now.IsZero() {
		cutoff = now.Add(-ttl)
	}
	normalized := make([]ClientActionRecord, 0, len(items))
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		item.ClientActionID = strings.TrimSpace(item.ClientActionID)
		item.Action = strings.TrimSpace(item.Action)
		item.Status = strings.TrimSpace(item.Status)
		if item.ClientActionID == "" {
			continue
		}
		if !cutoff.IsZero() && !item.AckedAt.IsZero() && item.AckedAt.Before(cutoff) {
			continue
		}
		if _, exists := seen[item.ClientActionID]; exists {
			continue
		}
		seen[item.ClientActionID] = struct{}{}
		normalized = append(normalized, item)
	}
	for i, j := 0, len(normalized)-1; i < j; i, j = i+1, j-1 {
		normalized[i], normalized[j] = normalized[j], normalized[i]
	}
	if limit > 0 && len(normalized) > limit {
		normalized = append([]ClientActionRecord(nil), normalized[len(normalized)-limit:]...)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func mergeSessionRuntime(base SessionRuntime, overlay SessionRuntime) SessionRuntime {
	return SessionRuntime{
		ResumeSessionID:  firstNonEmptyString(overlay.ResumeSessionID, base.ResumeSessionID),
		Command:          firstNonEmptyString(overlay.Command, base.Command),
		Engine:           firstNonEmptyString(overlay.Engine, base.Engine),
		PermissionMode:   firstNonEmptyString(overlay.PermissionMode, base.PermissionMode),
		CodexSandboxMode: firstNonEmptyString(overlay.CodexSandboxMode, base.CodexSandboxMode),
		CWD:              firstNonEmptyString(overlay.CWD, base.CWD),
		ClaudeLifecycle:  firstNonEmptyString(overlay.ClaudeLifecycle, base.ClaudeLifecycle),
		Source:           firstNonEmptyString(overlay.Source, base.Source),
		SourcePath:       firstNonEmptyString(overlay.SourcePath, base.SourcePath),
		SourceSize:       firstNonZeroInt64(overlay.SourceSize, base.SourceSize),
		SourceModUnixNS:  firstNonZeroInt64(overlay.SourceModUnixNS, base.SourceModUnixNS),
		SourceEntryCount: firstNonZeroInt(overlay.SourceEntryCount, base.SourceEntryCount),
	}
}

func firstNonZeroInt(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func firstNonZeroInt64(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func normalizeContextWindowUsage(usage ContextWindowUsage) ContextWindowUsage {
	if usage.TokenLimit < 0 {
		usage.TokenLimit = 0
	}
	if usage.TokensUsed < 0 {
		usage.TokensUsed = 0
	}
	if usage.TokenLimit > 0 && usage.TokensUsed > usage.TokenLimit {
		usage.TokensUsed = usage.TokenLimit
	}
	if usage.TokenLimit == 0 {
		usage.TokensUsed = 0
	}
	return usage
}

func normalizeSessionContext(ctx SessionContext) SessionContext {
	ctx.EnabledSkillNames = normalizeStringSlice(ctx.EnabledSkillNames)
	ctx.EnabledMemoryIDs = normalizeStringSlice(ctx.EnabledMemoryIDs)
	if len(ctx.EnabledSkillNames) > 0 || len(ctx.EnabledMemoryIDs) > 0 {
		ctx.Configured = true
	}
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
			item.Editable = true
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
			item.Editable = true
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

func buildPreview(summary SessionSummary, projection ProjectionSnapshot) string {
	if summary.External ||
		strings.EqualFold(strings.TrimSpace(summary.Source), "codex-native") ||
		strings.EqualFold(strings.TrimSpace(summary.Runtime.Source), "codex-native") {
		if text := latestMeaningfulProjectionText(projection, false); text != "" {
			return truncatePreview(text)
		}
	}
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
	summary.LastPreview = buildPreview(summary, projection)
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
	if isOperationalNativeCodexSummaryEntry(entry) {
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

func isOperationalNativeCodexSummaryEntry(entry SnapshotLogEntry) bool {
	if entry.Kind != "system" || entry.Context == nil {
		return false
	}
	if strings.TrimSpace(entry.Context.Source) != "codex-native" {
		return false
	}
	switch strings.TrimSpace(entry.Context.Type) {
	case "codex_task", "codex_tool_call", "codex_tool_output", "codex_patch":
		return true
	default:
		return false
	}
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

func isUntouchedAutoSessionRecord(record SessionRecord) bool {
	summary := record.Summary
	if summary.External || strings.EqualFold(strings.TrimSpace(summary.Source), "codex-native") {
		return false
	}
	runtimeSource := strings.ToLower(strings.TrimSpace(firstNonEmptyString(
		summary.Runtime.Source,
		record.Projection.Runtime.Source,
		summary.Source,
	)))
	if runtimeSource != "" && runtimeSource != "mobilevc" {
		return false
	}
	if summary.EntryCount > 0 || len(record.Projection.LogEntries) > 0 {
		return false
	}
	if strings.TrimSpace(summary.LastPreview) != "" {
		return false
	}
	if strings.TrimSpace(firstNonEmptyString(summary.Runtime.ResumeSessionID, record.Projection.Runtime.ResumeSessionID)) != "" {
		return false
	}
	if strings.TrimSpace(firstNonEmptyString(summary.Runtime.Command, record.Projection.Runtime.Command)) != "" {
		return false
	}
	title := normalizeSummaryText(summary.Title)
	if title == "" {
		return true
	}
	return looksLikePlaceholderTitle(title) || looksLikeTimestampText(title)
}

func selectVisibleSessions(items []SessionSummary, untouched map[string]bool) []SessionSummary {
	if len(items) == 0 {
		return nil
	}
	visible := make([]SessionSummary, 0, len(items))
	placeholderCount := 0
	for _, item := range items {
		if untouched[item.ID] {
			placeholderCount++
			continue
		}
		visible = append(visible, item)
	}
	if len(visible) > 0 {
		return visible
	}
	if placeholderCount <= 1 {
		return append([]SessionSummary(nil), items...)
	}
	newest := items[0]
	for _, item := range items[1:] {
		if item.UpdatedAt.After(newest.UpdatedAt) {
			newest = item
			continue
		}
		if item.UpdatedAt.Equal(newest.UpdatedAt) && item.CreatedAt.After(newest.CreatedAt) {
			newest = item
		}
	}
	return []SessionSummary{newest}
}

func sameSessionSummaryList(a, b []SessionSummary) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *FileStore) reconcileIndexLocked(index fileIndex) (fileIndex, error) {
	updated := false
	reconciled := make([]SessionSummary, 0, len(index.Sessions))
	untouched := make(map[string]bool, len(index.Sessions))
	for i := range index.Sessions {
		record, err := s.readSessionIndexRecordLocked(index.Sessions[i].ID)
		if err != nil {
			updated = true
			continue
		}
		if index.Sessions[i] != record.Summary {
			updated = true
		}
		reconciled = append(reconciled, record.Summary)
		if isUntouchedAutoSessionRecord(record) {
			untouched[record.Summary.ID] = true
		}
	}
	visible := selectVisibleSessions(reconciled, untouched)
	if !sameSessionSummaryList(index.Sessions, visible) {
		index.Sessions = visible
		updated = true
	}
	if updated {
		if err := s.writeIndexLocked(index); err != nil {
			return fileIndex{}, err
		}
	}
	return index, nil
}

func (s *FileStore) SavePushToken(ctx context.Context, sessionID, token, platform string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tokens := make(map[string]map[string]string)
	if data, err := os.ReadFile(s.pushTokensPath); err == nil {
		_ = json.Unmarshal(data, &tokens)
	}

	if tokens[sessionID] == nil {
		tokens[sessionID] = make(map[string]string)
	}
	tokens[sessionID]["token"] = token
	tokens[sessionID]["platform"] = platform

	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal push tokens: %w", err)
	}

	return os.WriteFile(s.pushTokensPath, data, 0600)
}

func (s *FileStore) GetPushToken(ctx context.Context, sessionID string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.pushTokensPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", nil
		}
		return "", "", err
	}

	tokens := make(map[string]map[string]string)
	if err := json.Unmarshal(data, &tokens); err != nil {
		return "", "", err
	}

	if info, ok := tokens[sessionID]; ok {
		return info["token"], info["platform"], nil
	}

	return "", "", nil
}
