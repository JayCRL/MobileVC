package store

import (
	"context"
	"mobilevc/internal/session"
	"time"
)

type SkillSource string

type CatalogDomain string

type CatalogSyncState string

type CatalogSourceOfTruth string

const (
	SkillSourceBuiltin  SkillSource = "builtin"
	SkillSourceLocal    SkillSource = "local"
	SkillSourceExternal SkillSource = "external"
)

const (
	CatalogDomainSkill  CatalogDomain = "skill"
	CatalogDomainMemory CatalogDomain = "memory"
)

const (
	CatalogSyncStateIdle     CatalogSyncState = "idle"
	CatalogSyncStateSyncing  CatalogSyncState = "syncing"
	CatalogSyncStateSynced   CatalogSyncState = "synced"
	CatalogSyncStateDrifted  CatalogSyncState = "drifted"
	CatalogSyncStateDraft    CatalogSyncState = "draft"
	CatalogSyncStateFailed   CatalogSyncState = "failed"
)

const (
	CatalogSourceTruthLocalMirror CatalogSourceOfTruth = "mobilevc-mirror"
	CatalogSourceTruthClaude      CatalogSourceOfTruth = "claude"
)

type CatalogMetadata struct {
	Domain        CatalogDomain        `json:"domain,omitempty"`
	SourceOfTruth CatalogSourceOfTruth `json:"sourceOfTruth,omitempty"`
	SyncState     CatalogSyncState     `json:"syncState,omitempty"`
	DriftDetected bool                 `json:"driftDetected,omitempty"`
	LastSyncedAt  time.Time            `json:"lastSyncedAt,omitempty"`
	VersionToken  string               `json:"versionToken,omitempty"`
	LastError     string               `json:"lastError,omitempty"`
}

type SkillDefinition struct {
	Name          string               `json:"name"`
	Description   string               `json:"description,omitempty"`
	Prompt        string               `json:"prompt,omitempty"`
	ResultView    string               `json:"resultView,omitempty"`
	TargetType    string               `json:"targetType,omitempty"`
	Source        SkillSource          `json:"source,omitempty"`
	SourceOfTruth CatalogSourceOfTruth `json:"sourceOfTruth,omitempty"`
	SyncState     CatalogSyncState     `json:"syncState,omitempty"`
	Editable      bool                 `json:"editable,omitempty"`
	DriftDetected bool                 `json:"driftDetected,omitempty"`
	UpdatedAt     time.Time            `json:"updatedAt,omitempty"`
	LastSyncedAt  time.Time            `json:"lastSyncedAt,omitempty"`
}

type MemoryItem struct {
	ID            string               `json:"id"`
	Title         string               `json:"title"`
	Content       string               `json:"content"`
	Source        string               `json:"source,omitempty"`
	SourceOfTruth CatalogSourceOfTruth `json:"sourceOfTruth,omitempty"`
	SyncState     CatalogSyncState     `json:"syncState,omitempty"`
	Editable      bool                 `json:"editable,omitempty"`
	DriftDetected bool                 `json:"driftDetected,omitempty"`
	UpdatedAt     time.Time            `json:"updatedAt,omitempty"`
	LastSyncedAt  time.Time            `json:"lastSyncedAt,omitempty"`
}

type SkillCatalogSnapshot struct {
	Meta  CatalogMetadata   `json:"meta,omitempty"`
	Items []SkillDefinition `json:"items,omitempty"`
}

type MemoryCatalogSnapshot struct {
	Meta  CatalogMetadata `json:"meta,omitempty"`
	Items []MemoryItem    `json:"items,omitempty"`
}

type SessionContext struct {
	EnabledSkillNames []string `json:"enabledSkillNames,omitempty"`
	EnabledMemoryIDs  []string `json:"enabledMemoryIds,omitempty"`
}

type SessionRuntime struct {
	ResumeSessionID string `json:"resumeSessionId,omitempty"`
	Command         string `json:"command,omitempty"`
	Engine          string `json:"engine,omitempty"`
	PermissionMode  string `json:"permissionMode,omitempty"`
	CWD             string `json:"cwd,omitempty"`
}

type SessionSummary struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	LastPreview string         `json:"lastPreview,omitempty"`
	EntryCount  int            `json:"entryCount,omitempty"`
	Runtime     SessionRuntime `json:"runtime,omitempty"`
}

type SnapshotContext struct {
	ID            string `json:"id,omitempty"`
	Type          string `json:"type,omitempty"`
	Message       string `json:"message,omitempty"`
	Status        string `json:"status,omitempty"`
	Target        string `json:"target,omitempty"`
	TargetPath    string `json:"targetPath,omitempty"`
	Tool          string `json:"tool,omitempty"`
	Command       string `json:"command,omitempty"`
	Timestamp     string `json:"timestamp,omitempty"`
	Title         string `json:"title,omitempty"`
	Stack         string `json:"stack,omitempty"`
	Code          string `json:"code,omitempty"`
	RelatedStep   string `json:"relatedStep,omitempty"`
	Path          string `json:"path,omitempty"`
	Diff          string `json:"diff,omitempty"`
	Lang          string `json:"lang,omitempty"`
	PendingReview bool   `json:"pendingReview,omitempty"`
	Source        string `json:"source,omitempty"`
	SkillName     string `json:"skillName,omitempty"`
	ExecutionID   string `json:"executionId,omitempty"`
	GroupID       string `json:"groupId,omitempty"`
	GroupTitle    string `json:"groupTitle,omitempty"`
	ReviewStatus  string `json:"reviewStatus,omitempty"`
}

type SnapshotLogEntry struct {
	Kind        string           `json:"kind"`
	Message     string           `json:"message,omitempty"`
	Label       string           `json:"label,omitempty"`
	Timestamp   string           `json:"timestamp,omitempty"`
	Stream      string           `json:"stream,omitempty"`
	Text        string           `json:"text,omitempty"`
	ExecutionID string           `json:"executionId,omitempty"`
	Phase       string           `json:"phase,omitempty"`
	ExitCode    *int             `json:"exitCode,omitempty"`
	Context     *SnapshotContext `json:"context,omitempty"`
}

type TerminalExecution struct {
	ExecutionID string `json:"executionId"`
	Command     string `json:"command,omitempty"`
	CWD         string `json:"cwd,omitempty"`
	StartedAt   string `json:"startedAt,omitempty"`
	FinishedAt  string `json:"finishedAt,omitempty"`
	ExitCode    *int   `json:"exitCode,omitempty"`
	Stdout      string `json:"stdout,omitempty"`
	Stderr      string `json:"stderr,omitempty"`
}

type ProjectionSnapshot struct {
	Diffs               []session.DiffContext      `json:"diffs,omitempty"`
	CurrentDiff         *session.DiffContext       `json:"currentDiff,omitempty"`
	ReviewGroups        []session.ReviewGroup      `json:"reviewGroups,omitempty"`
	ActiveReviewGroup   *session.ReviewGroup       `json:"activeReviewGroup,omitempty"`
	CurrentStep         *SnapshotContext           `json:"currentStep,omitempty"`
	LatestError         *SnapshotContext           `json:"latestError,omitempty"`
	LogEntries          []SnapshotLogEntry         `json:"logEntries,omitempty"`
	RawTerminalByStream map[string]string          `json:"rawTerminalByStream,omitempty"`
	TerminalExecutions  []TerminalExecution        `json:"terminalExecutions,omitempty"`
	Controller          session.ControllerSnapshot `json:"controller,omitempty"`
	Runtime             SessionRuntime             `json:"runtime,omitempty"`
	SessionContext      SessionContext             `json:"sessionContext,omitempty"`
	SkillCatalogMeta    CatalogMetadata            `json:"skillCatalogMeta,omitempty"`
	MemoryCatalogMeta   CatalogMetadata            `json:"memoryCatalogMeta,omitempty"`
}

type SessionRecord struct {
	Summary    SessionSummary     `json:"summary"`
	Projection ProjectionSnapshot `json:"projection"`
}

type Store interface {
	CreateSession(ctx context.Context, title string) (SessionSummary, error)
	SaveProjection(ctx context.Context, sessionID string, projection ProjectionSnapshot) (SessionSummary, error)
	GetSession(ctx context.Context, sessionID string) (SessionRecord, error)
	ListSessions(ctx context.Context) ([]SessionSummary, error)
	DeleteSession(ctx context.Context, sessionID string) error
	ListSkillCatalog(ctx context.Context) ([]SkillDefinition, error)
	SaveSkillCatalog(ctx context.Context, items []SkillDefinition) error
	GetSkillCatalogSnapshot(ctx context.Context) (SkillCatalogSnapshot, error)
	SaveSkillCatalogSnapshot(ctx context.Context, snapshot SkillCatalogSnapshot) error
	ListMemoryCatalog(ctx context.Context) ([]MemoryItem, error)
	SaveMemoryCatalog(ctx context.Context, items []MemoryItem) error
	GetMemoryCatalogSnapshot(ctx context.Context) (MemoryCatalogSnapshot, error)
	SaveMemoryCatalogSnapshot(ctx context.Context, snapshot MemoryCatalogSnapshot) error
}
