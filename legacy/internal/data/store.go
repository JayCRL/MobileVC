package data

import (
	"context"
	"mobilevc/internal/protocol"
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
	CatalogSyncStateIdle    CatalogSyncState = "idle"
	CatalogSyncStateSyncing CatalogSyncState = "syncing"
	CatalogSyncStateSynced  CatalogSyncState = "synced"
	CatalogSyncStateDrifted CatalogSyncState = "drifted"
	CatalogSyncStateDraft   CatalogSyncState = "draft"
	CatalogSyncStateFailed  CatalogSyncState = "failed"
)

const (
	CatalogSourceTruthLocalMirror CatalogSourceOfTruth = "mobilevc-mirror"
	CatalogSourceTruthClaude      CatalogSourceOfTruth = "claude"
	CatalogSourceTruthCodex       CatalogSourceOfTruth = "codex"
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

type PermissionScope string

type PermissionKind string

const (
	PermissionScopeSession    PermissionScope = "session"
	PermissionScopePersistent PermissionScope = "persistent"
)

const (
	PermissionKindWrite   PermissionKind = "write"
	PermissionKindShell   PermissionKind = "shell"
	PermissionKindNetwork PermissionKind = "network"
	PermissionKindGeneric PermissionKind = "generic"
)

type PermissionRule struct {
	ID               string          `json:"id"`
	Scope            PermissionScope `json:"scope,omitempty"`
	Enabled          bool            `json:"enabled"`
	Engine           string          `json:"engine,omitempty"`
	Kind             PermissionKind  `json:"kind,omitempty"`
	CommandHead      string          `json:"commandHead,omitempty"`
	TargetPathPrefix string          `json:"targetPathPrefix,omitempty"`
	Summary          string          `json:"summary,omitempty"`
	CreatedAt        time.Time       `json:"createdAt,omitempty"`
	LastMatchedAt    time.Time       `json:"lastMatchedAt,omitempty"`
	MatchCount       int             `json:"matchCount,omitempty"`
}

type PermissionRuleSnapshot struct {
	Enabled bool             `json:"enabled"`
	Items   []PermissionRule `json:"items,omitempty"`
}

type SessionContext struct {
	EnabledSkillNames []string `json:"enabledSkillNames,omitempty"`
	EnabledMemoryIDs  []string `json:"enabledMemoryIds,omitempty"`
	Configured        bool     `json:"configured,omitempty"`
}

type SessionRuntime struct {
	ResumeSessionID  string `json:"resumeSessionId,omitempty"`
	Command          string `json:"command,omitempty"`
	Engine           string `json:"engine,omitempty"`
	PermissionMode   string `json:"permissionMode,omitempty"`
	CodexSandboxMode string `json:"codexSandboxMode,omitempty"`
	CWD              string `json:"cwd,omitempty"`
	ClaudeLifecycle  string `json:"claudeLifecycle,omitempty"`
	Source           string `json:"source,omitempty"`
	SourcePath       string `json:"sourcePath,omitempty"`
	SourceSize       int64  `json:"sourceSize,omitempty"`
	SourceModUnixNS  int64  `json:"sourceModUnixNs,omitempty"`
	SourceEntryCount int    `json:"sourceEntryCount,omitempty"`
}

type ContextWindowUsage struct {
	TokensUsed int `json:"tokensUsed,omitempty"`
	TokenLimit int `json:"tokenLimit,omitempty"`
}

type ControllerState string

const (
	ControllerStateIdle        ControllerState = "IDLE"
	ControllerStateThinking    ControllerState = "THINKING"
	ControllerStateWaitInput   ControllerState = "WAIT_INPUT"
	ControllerStateRunningTool ControllerState = "RUNNING_TOOL"
)

type SessionSummary struct {
	ID                  string         `json:"id"`
	Title               string         `json:"title"`
	CreatedAt           time.Time      `json:"createdAt"`
	UpdatedAt           time.Time      `json:"updatedAt"`
	LastPreview         string         `json:"lastPreview,omitempty"`
	EntryCount          int            `json:"entryCount,omitempty"`
	Runtime             SessionRuntime `json:"runtime,omitempty"`
	Source              string         `json:"source,omitempty"`
	External            bool           `json:"external,omitempty"`
	Ownership           string         `json:"ownership,omitempty"`
	ExecutionActive     bool           `json:"executionActive,omitempty"`
	ClaudeSessionUUID   string         `json:"claudeSessionUUID,omitempty"`
	JSONLSyncEntryCount int            `json:"jsonlSyncEntryCount,omitempty"`
}

type SnapshotContext struct {
	ID            string `json:"id,omitempty"`
	Type          string `json:"type,omitempty"`
	Message       string `json:"message,omitempty"`
	Status        string `json:"status,omitempty"`
	Trigger       string `json:"trigger,omitempty"`
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

type DiffContext struct {
	ContextID     string `json:"contextId,omitempty"`
	Title         string `json:"title,omitempty"`
	Path          string `json:"path,omitempty"`
	Diff          string `json:"diff,omitempty"`
	Lang          string `json:"lang,omitempty"`
	PendingReview bool   `json:"pendingReview,omitempty"`
	ExecutionID   string `json:"executionId,omitempty"`
	GroupID       string `json:"groupId,omitempty"`
	GroupTitle    string `json:"groupTitle,omitempty"`
	ReviewStatus  string `json:"reviewStatus,omitempty"`
}

type ReviewFile struct {
	ContextID     string `json:"contextId,omitempty"`
	Title         string `json:"title,omitempty"`
	Path          string `json:"path,omitempty"`
	Diff          string `json:"diff,omitempty"`
	Lang          string `json:"lang,omitempty"`
	PendingReview bool   `json:"pendingReview,omitempty"`
	ExecutionID   string `json:"executionId,omitempty"`
	ReviewStatus  string `json:"reviewStatus,omitempty"`
}

type ReviewGroup struct {
	ID            string       `json:"id,omitempty"`
	Title         string       `json:"title,omitempty"`
	ExecutionID   string       `json:"executionId,omitempty"`
	PendingReview bool         `json:"pendingReview,omitempty"`
	ReviewStatus  string       `json:"reviewStatus,omitempty"`
	CurrentFileID string       `json:"currentFileId,omitempty"`
	CurrentPath   string       `json:"currentPath,omitempty"`
	PendingCount  int          `json:"pendingCount,omitempty"`
	AcceptedCount int          `json:"acceptedCount,omitempty"`
	RevertedCount int          `json:"revertedCount,omitempty"`
	RevisedCount  int          `json:"revisedCount,omitempty"`
	Files         []ReviewFile `json:"files,omitempty"`
}

type ControllerSnapshot struct {
	SessionID             string               `json:"sessionId"`
	State                 ControllerState      `json:"state"`
	CurrentCommand        string               `json:"currentCommand,omitempty"`
	LastStep              string               `json:"lastStep,omitempty"`
	LastTool              string               `json:"lastTool,omitempty"`
	ResumeSession         string               `json:"resumeSession,omitempty"`
	ClaudeLifecycle       string               `json:"claudeLifecycle,omitempty"`
	LastUserInput         string               `json:"lastUserInput,omitempty"`
	ActiveMeta            protocol.RuntimeMeta `json:"activeMeta,omitempty"`
	RecentDiffs           []DiffContext        `json:"recentDiffs,omitempty"`
	RecentDiff            DiffContext          `json:"recentDiff,omitempty"`
	ReviewGroups          []ReviewGroup        `json:"reviewGroups,omitempty"`
	ActiveReviewID        string               `json:"activeReviewId,omitempty"`
	ResolvedPermissionIDs []string             `json:"resolvedPermissionIds,omitempty"`
}

type SnapshotLogEntry struct {
	Kind        string                        `json:"kind"`
	Message     string                        `json:"message,omitempty"`
	Label       string                        `json:"label,omitempty"`
	Timestamp   string                        `json:"timestamp,omitempty"`
	Stream      string                        `json:"stream,omitempty"`
	Text        string                        `json:"text,omitempty"`
	ExecutionID string                        `json:"executionId,omitempty"`
	Phase       string                        `json:"phase,omitempty"`
	ExitCode    *int                          `json:"exitCode,omitempty"`
	Context     *SnapshotContext              `json:"context,omitempty"`
	Attachments []protocol.TimelineAttachment `json:"attachments,omitempty"`
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
	Diffs                  []DiffContext       `json:"diffs,omitempty"`
	CurrentDiff            *DiffContext        `json:"currentDiff,omitempty"`
	ReviewGroups           []ReviewGroup       `json:"reviewGroups,omitempty"`
	ActiveReviewGroup      *ReviewGroup        `json:"activeReviewGroup,omitempty"`
	CurrentStep            *SnapshotContext    `json:"currentStep,omitempty"`
	LatestError            *SnapshotContext    `json:"latestError,omitempty"`
	LogEntries             []SnapshotLogEntry  `json:"logEntries,omitempty"`
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

type SessionRecord struct {
	Summary       SessionSummary       `json:"summary"`
	Projection    ProjectionSnapshot   `json:"projection"`
	ClientActions []ClientActionRecord `json:"clientActions,omitempty"`
}

type SessionHistoryWindowRequest struct {
	SessionID string
	Before    int
	Limit     int
}

type SessionHistoryWindow struct {
	Record        SessionRecord
	LogEntries    []SnapshotLogEntry
	LogEntryStart int
	LogEntryTotal int
}

type SessionProjectionCounts struct {
	LogEntryCount          int
	DiffCount              int
	TerminalExecutionCount int
	TerminalStdoutLength   int
	TerminalStderrLength   int
}

type SessionRuntimeMetadata struct {
	Record SessionRecord
	Latest SessionProjectionCounts
}

type SessionContextSnapshot struct {
	SessionID      string
	SessionContext SessionContext
}

type SessionPermissionRuleSnapshot struct {
	SessionID string
	Enabled   bool
	Items     []PermissionRule
}

type SessionDiffPageRequest struct {
	SessionID string
	Before    int
	Limit     int
}

type SessionDiffPage struct {
	Record    SessionRecord
	Latest    SessionProjectionCounts
	Diffs     []DiffContext
	DiffStart int
	DiffTotal int
}

type SessionTerminalRangeRequest struct {
	SessionID string
	Stream    string
	Start     int
	Limit     int
}

type SessionTerminalRange struct {
	Record  SessionRecord
	Latest  SessionProjectionCounts
	Stream  string
	Start   int
	End     int
	Total   int
	Content string
}

type SessionTerminalExecutionPageRequest struct {
	SessionID     string
	Before        int
	Limit         int
	IncludeOutput bool
}

type SessionTerminalExecutionPage struct {
	Record             SessionRecord
	Latest             SessionProjectionCounts
	TerminalExecutions []TerminalExecution
	ExecutionStart     int
	ExecutionTotal     int
	IncludeOutput      bool
}

type SessionTerminalExecutionRequest struct {
	SessionID     string
	ExecutionID   string
	IncludeOutput bool
}

type SessionTerminalExecutionSnapshot struct {
	SessionID         string
	Latest            SessionProjectionCounts
	TerminalExecution TerminalExecution
}

type ClientActionRecord struct {
	ClientActionID string    `json:"clientActionId"`
	Action         string    `json:"action"`
	Status         string    `json:"status"`
	AckedAt        time.Time `json:"ackedAt"`
}

type Store interface {
	CreateSession(ctx context.Context, title string) (SessionSummary, error)
	UpsertSession(ctx context.Context, record SessionRecord) (SessionSummary, error)
	SaveProjection(ctx context.Context, sessionID string, projection ProjectionSnapshot) (SessionSummary, error)
	GetSession(ctx context.Context, sessionID string) (SessionRecord, error)
	ListSessions(ctx context.Context) ([]SessionSummary, error)
	DeleteSession(ctx context.Context, sessionID string) error
	MarkClientAction(ctx context.Context, sessionID string, record ClientActionRecord, ttl time.Duration, limit int) (duplicate bool, err error)
	SavePushToken(ctx context.Context, sessionID, token, platform string) error
	GetPushToken(ctx context.Context, sessionID string) (token string, platform string, err error)
	ListSkillCatalog(ctx context.Context) ([]SkillDefinition, error)
	SaveSkillCatalog(ctx context.Context, items []SkillDefinition) error
	GetSkillCatalogSnapshot(ctx context.Context) (SkillCatalogSnapshot, error)
	SaveSkillCatalogSnapshot(ctx context.Context, snapshot SkillCatalogSnapshot) error
	ListMemoryCatalog(ctx context.Context) ([]MemoryItem, error)
	SaveMemoryCatalog(ctx context.Context, items []MemoryItem) error
	GetMemoryCatalogSnapshot(ctx context.Context) (MemoryCatalogSnapshot, error)
	SaveMemoryCatalogSnapshot(ctx context.Context, snapshot MemoryCatalogSnapshot) error
	GetPermissionRuleSnapshot(ctx context.Context) (PermissionRuleSnapshot, error)
	SavePermissionRuleSnapshot(ctx context.Context, snapshot PermissionRuleSnapshot) error
}

type ProjectionSaveOption func(*ProjectionSaveOptions)

type ProjectionSaveOptions struct {
	JSONLSyncEntryCount *int
}

func WithJSONLSyncEntryCount(count int) ProjectionSaveOption {
	return func(opts *ProjectionSaveOptions) {
		opts.JSONLSyncEntryCount = &count
	}
}

type ProjectionOptionStore interface {
	SaveProjectionWithOptions(ctx context.Context, sessionID string, projection ProjectionSnapshot, opts ...ProjectionSaveOption) (SessionSummary, error)
}

type SessionSummaryStore interface {
	GetSessionSummary(ctx context.Context, sessionID string) (SessionSummary, error)
}

type NativeSessionDeletionStore interface {
	DeletedNativeSessionIDs(ctx context.Context) (NativeSessionDeletionSet, error)
}

type NativeSessionDeletionSet struct {
	ClaudeSessionIDs map[string]struct{}
	CodexThreadIDs   map[string]struct{}
}

type SessionLogEntryAppendStore interface {
	AppendSessionLogEntries(ctx context.Context, sessionID string, entries []SnapshotLogEntry, opts ...ProjectionSaveOption) (SessionSummary, error)
}

type SessionHistoryWindowStore interface {
	GetSessionHistoryWindow(ctx context.Context, req SessionHistoryWindowRequest) (SessionHistoryWindow, error)
}

type SessionRuntimeMetaStore interface {
	GetSessionRuntimeMetadata(ctx context.Context, sessionID string) (SessionRuntimeMetadata, error)
}

type SessionContextStore interface {
	GetSessionContext(ctx context.Context, sessionID string) (SessionContextSnapshot, error)
	SaveSessionContext(ctx context.Context, snapshot SessionContextSnapshot) (SessionSummary, error)
}

type SessionPermissionRuleStore interface {
	GetSessionPermissionRuleSnapshot(ctx context.Context, sessionID string) (SessionPermissionRuleSnapshot, error)
	SaveSessionPermissionRuleSnapshot(ctx context.Context, snapshot SessionPermissionRuleSnapshot) (SessionSummary, error)
}

type SessionDiffPageStore interface {
	GetSessionDiffPage(ctx context.Context, req SessionDiffPageRequest) (SessionDiffPage, error)
}

type SessionTerminalRangeStore interface {
	GetSessionTerminalRange(ctx context.Context, req SessionTerminalRangeRequest) (SessionTerminalRange, error)
}

type SessionTerminalExecutionPageStore interface {
	GetSessionTerminalExecutionPage(ctx context.Context, req SessionTerminalExecutionPageRequest) (SessionTerminalExecutionPage, error)
}

type SessionTerminalExecutionStore interface {
	GetSessionTerminalExecution(ctx context.Context, req SessionTerminalExecutionRequest) (SessionTerminalExecutionSnapshot, error)
}
