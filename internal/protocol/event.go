package protocol

import (
	"strings"
	"time"
)

const (
	EventTypeLog                    = "log"
	EventTypeProgress               = "progress"
	EventTypeError                  = "error"
	EventTypePromptRequest          = "prompt_request"
	EventTypeInteractionRequest     = "interaction_request"
	EventTypeSessionState           = "session_state"
	EventTypeAgentState             = "agent_state"
	EventTypeRuntimePhase           = "runtime_phase"
	EventTypeFSListResult           = "fs_list_result"
	EventTypeFSReadResult           = "fs_read_result"
	EventTypeStepUpdate             = "step_update"
	EventTypeFileDiff               = "file_diff"
	EventTypeRuntimeInfoResult      = "runtime_info_result"
	EventTypeSessionCreated         = "session_created"
	EventTypeSessionListResult      = "session_list_result"
	EventTypeSessionHistory         = "session_history"
	EventTypeReviewState            = "review_state"
	EventTypeSkillCatalogResult     = "skill_catalog_result"
	EventTypeMemoryListResult       = "memory_list_result"
	EventTypeCatalogAuthoringResult = "catalog_authoring_result"
	EventTypeSessionContextResult   = "session_context_result"
	EventTypeSkillSyncResult        = "skill_sync_result"
	EventTypeCatalogSyncStatus      = "catalog_sync_status"
	EventTypeCatalogSyncResult      = "catalog_sync_result"
	EventTypeADBDevicesResult       = "adb_devices_result"
	EventTypeADBStreamState         = "adb_stream_state"
	EventTypeADBFrame               = "adb_frame"
)

type RuntimeMeta struct {
	Source          string `json:"source,omitempty"`
	SkillName       string `json:"skillName,omitempty"`
	Target          string `json:"target,omitempty"`
	TargetType      string `json:"targetType,omitempty"`
	TargetPath      string `json:"targetPath,omitempty"`
	ResultView      string `json:"resultView,omitempty"`
	ResumeSessionID string `json:"resumeSessionId,omitempty"`
	ExecutionID     string `json:"executionId,omitempty"`
	GroupID         string `json:"groupId,omitempty"`
	GroupTitle      string `json:"groupTitle,omitempty"`
	ContextID       string `json:"contextId,omitempty"`
	ContextTitle    string `json:"contextTitle,omitempty"`
	TargetText      string `json:"targetText,omitempty"`
	Command         string `json:"command,omitempty"`
	Engine          string `json:"engine,omitempty"`
	CWD             string `json:"cwd,omitempty"`
	PermissionMode  string `json:"permissionMode,omitempty"`
	ClaudeLifecycle string `json:"claudeLifecycle,omitempty"`
}

type Event struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	SessionID string    `json:"sessionId,omitempty"`
	RuntimeMeta
}

type ClientEvent struct {
	Action string `json:"action"`
}

type ExecRequestEvent struct {
	ClientEvent
	Command        string `json:"cmd"`
	CWD            string `json:"cwd,omitempty"`
	Mode           string `json:"mode,omitempty"`
	PermissionMode string `json:"permissionMode,omitempty"`
	RuntimeMeta
}

type InputRequestEvent struct {
	ClientEvent
	Data           string `json:"data"`
	PermissionMode string `json:"permissionMode,omitempty"`
}

type ReviewDecisionRequestEvent struct {
	ClientEvent
	Decision       string `json:"decision"`
	ExecutionID    string `json:"executionId,omitempty"`
	GroupID        string `json:"groupId,omitempty"`
	GroupTitle     string `json:"groupTitle,omitempty"`
	ContextID      string `json:"contextId,omitempty"`
	ContextTitle   string `json:"contextTitle,omitempty"`
	TargetPath     string `json:"targetPath,omitempty"`
	PermissionMode string `json:"permissionMode,omitempty"`
}

type PermissionDecisionRequestEvent struct {
	ClientEvent
	Decision           string `json:"decision"`
	PermissionMode     string `json:"permissionMode,omitempty"`
	ResumeSessionID    string `json:"resumeSessionId,omitempty"`
	TargetPath         string `json:"targetPath,omitempty"`
	ContextID          string `json:"contextId,omitempty"`
	ContextTitle       string `json:"contextTitle,omitempty"`
	PromptMessage      string `json:"promptMessage,omitempty"`
	FallbackCommand    string `json:"command,omitempty"`
	FallbackCWD        string `json:"cwd,omitempty"`
	FallbackEngine     string `json:"engine,omitempty"`
	FallbackTarget     string `json:"target,omitempty"`
	FallbackTargetType string `json:"targetType,omitempty"`
}

type PlanDecisionRequestEvent struct {
	ClientEvent
	Decision        string `json:"decision"`
	SessionID       string `json:"sessionId,omitempty"`
	ExecutionID     string `json:"executionId,omitempty"`
	GroupID         string `json:"groupId,omitempty"`
	GroupTitle      string `json:"groupTitle,omitempty"`
	ContextID       string `json:"contextId,omitempty"`
	ContextTitle    string `json:"contextTitle,omitempty"`
	PromptMessage   string `json:"promptMessage,omitempty"`
	PermissionMode  string `json:"permissionMode,omitempty"`
	ResumeSessionID string `json:"resumeSessionId,omitempty"`
	Command         string `json:"command,omitempty"`
	CWD             string `json:"cwd,omitempty"`
	Engine          string `json:"engine,omitempty"`
	Target          string `json:"target,omitempty"`
	TargetType      string `json:"targetType,omitempty"`
	TargetPath      string `json:"targetPath,omitempty"`
	TargetText      string `json:"targetText,omitempty"`
}

type PermissionModeUpdateRequestEvent struct {
	ClientEvent
	PermissionMode string `json:"permissionMode,omitempty"`
}

type SkillRequestEvent struct {
	ClientEvent
	Name         string `json:"name"`
	Engine       string `json:"engine,omitempty"`
	CWD          string `json:"cwd,omitempty"`
	Target       string `json:"target,omitempty"`
	TargetType   string `json:"targetType,omitempty"`
	TargetPath   string `json:"targetPath,omitempty"`
	TargetDiff   string `json:"targetDiff,omitempty"`
	TargetTitle  string `json:"targetTitle,omitempty"`
	ResultView   string `json:"resultView,omitempty"`
	ContextID    string `json:"contextId,omitempty"`
	ContextTitle string `json:"contextTitle,omitempty"`
	TargetText   string `json:"targetText,omitempty"`
	TargetStack  string `json:"targetStack,omitempty"`
}

type SkillCatalogRequestEvent struct {
	ClientEvent
	Skill SkillDefinition `json:"skill,omitempty"`
}

type MemoryRequestEvent struct {
	ClientEvent
	Item MemoryItem `json:"item,omitempty"`
}

type SessionContextUpdateRequestEvent struct {
	ClientEvent
	EnabledSkillNames []string `json:"enabledSkillNames,omitempty"`
	EnabledMemoryIDs  []string `json:"enabledMemoryIds,omitempty"`
}

type SkillDefinition struct {
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	Prompt        string `json:"prompt,omitempty"`
	ResultView    string `json:"resultView,omitempty"`
	TargetType    string `json:"targetType,omitempty"`
	Source        string `json:"source,omitempty"`
	SourceOfTruth string `json:"sourceOfTruth,omitempty"`
	SyncState     string `json:"syncState,omitempty"`
	Editable      bool   `json:"editable,omitempty"`
	DriftDetected bool   `json:"driftDetected,omitempty"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
	LastSyncedAt  string `json:"lastSyncedAt,omitempty"`
}

type MemoryItem struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Content       string `json:"content"`
	Source        string `json:"source,omitempty"`
	SourceOfTruth string `json:"sourceOfTruth,omitempty"`
	SyncState     string `json:"syncState,omitempty"`
	Editable      bool   `json:"editable,omitempty"`
	DriftDetected bool   `json:"driftDetected,omitempty"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
	LastSyncedAt  string `json:"lastSyncedAt,omitempty"`
}

type CatalogMetadata struct {
	Domain        string `json:"domain,omitempty"`
	SourceOfTruth string `json:"sourceOfTruth,omitempty"`
	SyncState     string `json:"syncState,omitempty"`
	DriftDetected bool   `json:"driftDetected,omitempty"`
	LastSyncedAt  string `json:"lastSyncedAt,omitempty"`
	VersionToken  string `json:"versionToken,omitempty"`
	LastError     string `json:"lastError,omitempty"`
}

type SessionContext struct {
	EnabledSkillNames []string `json:"enabledSkillNames,omitempty"`
	EnabledMemoryIDs  []string `json:"enabledMemoryIds,omitempty"`
}

type ADBDevice struct {
	Serial      string `json:"serial"`
	State       string `json:"state,omitempty"`
	Model       string `json:"model,omitempty"`
	Product     string `json:"product,omitempty"`
	DeviceName  string `json:"deviceName,omitempty"`
	TransportID string `json:"transportId,omitempty"`
}

type FSListRequestEvent struct {
	ClientEvent
	Path string `json:"path,omitempty"`
}

type FSReadRequestEvent struct {
	ClientEvent
	Path string `json:"path,omitempty"`
}

type ADBDevicesRequestEvent struct {
	ClientEvent
}

type ADBStreamStartRequestEvent struct {
	ClientEvent
	Serial     string `json:"serial,omitempty"`
	IntervalMS int    `json:"intervalMs,omitempty"`
}

type ADBStreamStopRequestEvent struct {
	ClientEvent
}

type ADBEmulatorStartRequestEvent struct {
	ClientEvent
	AVD string `json:"avd,omitempty"`
}

type ADBTapRequestEvent struct {
	ClientEvent
	Serial string `json:"serial,omitempty"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
}

type RuntimeInfoRequestEvent struct {
	ClientEvent
	Query string `json:"query,omitempty"`
	CWD   string `json:"cwd,omitempty"`
}

type SlashCommandRequestEvent struct {
	ClientEvent
	Command        string `json:"command"`
	CWD            string `json:"cwd,omitempty"`
	Engine         string `json:"engine,omitempty"`
	PermissionMode string `json:"permissionMode,omitempty"`
	TargetType     string `json:"targetType,omitempty"`
	TargetPath     string `json:"targetPath,omitempty"`
	TargetDiff     string `json:"targetDiff,omitempty"`
	TargetTitle    string `json:"targetTitle,omitempty"`
	ContextID      string `json:"contextId,omitempty"`
	ContextTitle   string `json:"contextTitle,omitempty"`
	TargetText     string `json:"targetText,omitempty"`
	TargetStack    string `json:"targetStack,omitempty"`
}

type SessionCreateRequestEvent struct {
	ClientEvent
	Title string `json:"title,omitempty"`
}

type SessionLoadRequestEvent struct {
	ClientEvent
	SessionID string `json:"sessionId"`
}

type SessionDeleteRequestEvent struct {
	ClientEvent
	SessionID string `json:"sessionId"`
}

type SessionSummary struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	CreatedAt   string      `json:"createdAt,omitempty"`
	UpdatedAt   string      `json:"updatedAt,omitempty"`
	LastPreview string      `json:"lastPreview,omitempty"`
	EntryCount  int         `json:"entryCount,omitempty"`
	Runtime     RuntimeMeta `json:"runtime,omitempty"`
}

type HistoryContext struct {
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

type ReviewFile struct {
	ID            string `json:"id,omitempty"`
	Path          string `json:"path,omitempty"`
	Title         string `json:"title,omitempty"`
	Diff          string `json:"diff,omitempty"`
	Lang          string `json:"lang,omitempty"`
	PendingReview bool   `json:"pendingReview,omitempty"`
	ReviewStatus  string `json:"reviewStatus,omitempty"`
	ExecutionID   string `json:"executionId,omitempty"`
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

type HistoryLogEntry struct {
	Kind        string          `json:"kind"`
	Message     string          `json:"message,omitempty"`
	Label       string          `json:"label,omitempty"`
	Timestamp   string          `json:"timestamp,omitempty"`
	Stream      string          `json:"stream,omitempty"`
	Text        string          `json:"text,omitempty"`
	ExecutionID string          `json:"executionId,omitempty"`
	Phase       string          `json:"phase,omitempty"`
	ExitCode    *int            `json:"exitCode,omitempty"`
	Context     *HistoryContext `json:"context,omitempty"`
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

type SessionCreatedEvent struct {
	Event
	Summary SessionSummary `json:"summary"`
}

type SessionListResultEvent struct {
	Event
	Items []SessionSummary `json:"items"`
}

type SessionHistoryEvent struct {
	Event
	Summary             SessionSummary      `json:"summary"`
	LogEntries          []HistoryLogEntry   `json:"logEntries,omitempty"`
	Diffs               []HistoryContext    `json:"diffs,omitempty"`
	CurrentDiff         *HistoryContext     `json:"currentDiff,omitempty"`
	ReviewGroups        []ReviewGroup       `json:"reviewGroups,omitempty"`
	ActiveReviewGroup   *ReviewGroup        `json:"activeReviewGroup,omitempty"`
	CurrentStep         *HistoryContext     `json:"currentStep,omitempty"`
	LatestError         *HistoryContext     `json:"latestError,omitempty"`
	RawTerminalByStream map[string]string   `json:"rawTerminalByStream,omitempty"`
	TerminalExecutions  []TerminalExecution `json:"terminalExecutions,omitempty"`
	SessionContext      SessionContext      `json:"sessionContext,omitempty"`
	SkillCatalogMeta    CatalogMetadata     `json:"skillCatalogMeta,omitempty"`
	MemoryCatalogMeta   CatalogMetadata     `json:"memoryCatalogMeta,omitempty"`
	CanResume           bool                `json:"canResume,omitempty"`
	ResumeRuntimeMeta   RuntimeMeta         `json:"resumeRuntimeMeta,omitempty"`
}

type ReviewStateEvent struct {
	Event
	Groups      []ReviewGroup `json:"groups,omitempty"`
	ActiveGroup *ReviewGroup  `json:"activeGroup,omitempty"`
}

type SkillCatalogResultEvent struct {
	Event
	Meta  CatalogMetadata   `json:"meta,omitempty"`
	Items []SkillDefinition `json:"items"`
}

type MemoryListResultEvent struct {
	Event
	Meta  CatalogMetadata `json:"meta,omitempty"`
	Items []MemoryItem    `json:"items"`
}

type CatalogAuthoringResultEvent struct {
	Event
	Domain  string           `json:"domain,omitempty"`
	Skill   *SkillDefinition `json:"skill,omitempty"`
	Memory  *MemoryItem      `json:"memory,omitempty"`
	Message string           `json:"msg,omitempty"`
}

type SessionContextResultEvent struct {
	Event
	SessionContext SessionContext `json:"sessionContext"`
}

type SkillSyncResultEvent struct {
	Event
	Message string `json:"msg,omitempty"`
}

type CatalogSyncStatusEvent struct {
	Event
	Domain string          `json:"domain,omitempty"`
	Meta   CatalogMetadata `json:"meta,omitempty"`
}

type CatalogSyncResultEvent struct {
	Event
	Domain  string          `json:"domain,omitempty"`
	Meta    CatalogMetadata `json:"meta,omitempty"`
	Success bool            `json:"success"`
	Message string          `json:"msg,omitempty"`
}

type RuntimeInfoItem struct {
	Label     string `json:"label"`
	Value     string `json:"value,omitempty"`
	Status    string `json:"status,omitempty"`
	Available bool   `json:"available"`
	Detail    string `json:"detail,omitempty"`
}

type LogEvent struct {
	Event
	Message  string `json:"msg"`
	Stream   string `json:"stream,omitempty"`
	Phase    string `json:"phase,omitempty"`
	ExitCode *int   `json:"exitCode,omitempty"`
}

type ProgressEvent struct {
	Event
	Message string `json:"msg,omitempty"`
	Percent int    `json:"percent"`
}

type ErrorEvent struct {
	Event
	Message    string `json:"msg"`
	Stack      string `json:"stack,omitempty"`
	Code       string `json:"code,omitempty"`
	TargetPath string `json:"targetPath,omitempty"`
	Step       string `json:"step,omitempty"`
	Command    string `json:"command,omitempty"`
}

type PromptRequestEvent struct {
	Event
	Message string   `json:"msg,omitempty"`
	Options []string `json:"options,omitempty"`
}

type InteractionAction struct {
	ID          string `json:"id,omitempty"`
	Label       string `json:"label,omitempty"`
	Variant     string `json:"variant,omitempty"`
	Value       string `json:"value,omitempty"`
	Decision    string `json:"decision,omitempty"`
	SubmitMode  string `json:"submitMode,omitempty"`
	NeedsInput  bool   `json:"needsInput,omitempty"`
	Destructive bool   `json:"destructive,omitempty"`
}

type InteractionRequestEvent struct {
	Event
	Kind             string              `json:"kind,omitempty"`
	Title            string              `json:"title,omitempty"`
	Message          string              `json:"msg,omitempty"`
	Options          []string            `json:"options,omitempty"`
	Actions          []InteractionAction `json:"actions,omitempty"`
	ContextID        string              `json:"contextId,omitempty"`
	ContextTitle     string              `json:"contextTitle,omitempty"`
	TargetPath       string              `json:"targetPath,omitempty"`
	ExecutionID      string              `json:"executionId,omitempty"`
	GroupID          string              `json:"groupId,omitempty"`
	GroupTitle       string              `json:"groupTitle,omitempty"`
	ResumeSessionID  string              `json:"resumeSessionId,omitempty"`
	PermissionMode   string              `json:"permissionMode,omitempty"`
	InputLabel       string              `json:"inputLabel,omitempty"`
	InputPlaceholder string              `json:"inputPlaceholder,omitempty"`
}

type SessionStateEvent struct {
	Event
	State   string `json:"state"`
	Message string `json:"msg,omitempty"`
}

type AgentStateEvent struct {
	Event
	State      string `json:"state"`
	Message    string `json:"msg,omitempty"`
	AwaitInput bool   `json:"awaitInput,omitempty"`
	Command    string `json:"command,omitempty"`
	Step       string `json:"step,omitempty"`
	Tool       string `json:"tool,omitempty"`
}

type RuntimePhaseEvent struct {
	Event
	Phase   string `json:"phase"`
	Kind    string `json:"kind,omitempty"`
	Message string `json:"msg,omitempty"`
}

func (e RuntimePhaseEvent) GetRuntimeMeta() RuntimeMeta { return e.RuntimeMeta }

type StepUpdateEvent struct {
	Event
	Message string `json:"msg,omitempty"`
	Status  string `json:"status,omitempty"`
	Target  string `json:"target,omitempty"`
	Tool    string `json:"tool,omitempty"`
	Command string `json:"command,omitempty"`
}

type FileDiffEvent struct {
	Event
	Path  string `json:"path,omitempty"`
	Title string `json:"title,omitempty"`
	Diff  string `json:"diff,omitempty"`
	Lang  string `json:"lang,omitempty"`
}

type FSItem struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

type FSListResultEvent struct {
	Event
	CurrentPath string   `json:"current_path"`
	Items       []FSItem `json:"items"`
}

type FSReadResultEvent struct {
	Event
	Path     string `json:"path"`
	Content  string `json:"content"`
	Size     int64  `json:"size"`
	Lang     string `json:"lang,omitempty"`
	Encoding string `json:"encoding,omitempty"`
	IsText   bool   `json:"isText"`
}

type RuntimeInfoResultEvent struct {
	Event
	Query       string            `json:"query,omitempty"`
	Title       string            `json:"title,omitempty"`
	Items       []RuntimeInfoItem `json:"items,omitempty"`
	Unavailable bool              `json:"unavailable,omitempty"`
	Message     string            `json:"msg,omitempty"`
}

type ADBDevicesResultEvent struct {
	Event
	Devices           []ADBDevice `json:"devices,omitempty"`
	SelectedSerial    string      `json:"selectedSerial,omitempty"`
	AvailableAVDs     []string    `json:"availableAvds,omitempty"`
	PreferredAVD      string      `json:"preferredAvd,omitempty"`
	ADBAvailable      bool        `json:"adbAvailable"`
	EmulatorAvailable bool        `json:"emulatorAvailable"`
	SuggestedAction   string      `json:"suggestedAction,omitempty"`
	Message           string      `json:"msg,omitempty"`
}

type ADBStreamStateEvent struct {
	Event
	Running    bool   `json:"running"`
	Serial     string `json:"serial,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	IntervalMS int    `json:"intervalMs,omitempty"`
	Message    string `json:"msg,omitempty"`
}

type ADBFrameEvent struct {
	Event
	Serial string `json:"serial,omitempty"`
	Format string `json:"format,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Seq    int    `json:"seq,omitempty"`
	Image  string `json:"image,omitempty"`
}

func NewBaseEvent(eventType, sessionID string) Event {
	return Event{
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		SessionID: sessionID,
	}
}

func NewLogEvent(sessionID, message, stream string) LogEvent {
	return LogEvent{
		Event:   NewBaseEvent(EventTypeLog, sessionID),
		Message: message,
		Stream:  stream,
		Phase:   stream,
	}
}

func NewExecutionLogEvent(sessionID, executionID, message, stream, phase string, exitCode *int) LogEvent {
	return LogEvent{
		Event: Event{
			Type:      EventTypeLog,
			Timestamp: time.Now().UTC(),
			SessionID: sessionID,
			RuntimeMeta: RuntimeMeta{
				ExecutionID: executionID,
			},
		},
		Message:  message,
		Stream:   stream,
		Phase:    phase,
		ExitCode: exitCode,
	}
}

func NewErrorEvent(sessionID, message, stack string) ErrorEvent {
	return ErrorEvent{
		Event:   NewBaseEvent(EventTypeError, sessionID),
		Message: message,
		Stack:   stack,
	}
}

func NewPromptRequestEvent(sessionID, message string, options []string) PromptRequestEvent {
	return PromptRequestEvent{
		Event:   NewBaseEvent(EventTypePromptRequest, sessionID),
		Message: message,
		Options: options,
	}
}

func NewInteractionRequestEvent(sessionID, kind, title, message string, actions []InteractionAction) InteractionRequestEvent {
	return InteractionRequestEvent{
		Event:   NewBaseEvent(EventTypeInteractionRequest, sessionID),
		Kind:    kind,
		Title:   title,
		Message: message,
		Actions: actions,
	}
}

func NewSessionStateEvent(sessionID, state, message string) SessionStateEvent {
	return SessionStateEvent{
		Event:   NewBaseEvent(EventTypeSessionState, sessionID),
		State:   state,
		Message: message,
	}
}

func NewAgentStateEvent(sessionID, state, message string, awaitInput bool, command, step, tool string) AgentStateEvent {
	return AgentStateEvent{
		Event:      NewBaseEvent(EventTypeAgentState, sessionID),
		State:      state,
		Message:    message,
		AwaitInput: awaitInput,
		Command:    command,
		Step:       step,
		Tool:       tool,
	}
}

func NewRuntimePhaseEvent(sessionID, phase, kind, message string) RuntimePhaseEvent {
	return RuntimePhaseEvent{
		Event:   NewBaseEvent(EventTypeRuntimePhase, sessionID),
		Phase:   phase,
		Kind:    kind,
		Message: message,
	}
}

func NewStepUpdateEvent(sessionID, message, status, target, tool, command string) StepUpdateEvent {
	return StepUpdateEvent{
		Event:   NewBaseEvent(EventTypeStepUpdate, sessionID),
		Message: message,
		Status:  status,
		Target:  target,
		Tool:    tool,
		Command: command,
	}
}

func NewFileDiffEvent(sessionID, path, title, diff, lang string) FileDiffEvent {
	return FileDiffEvent{
		Event: NewBaseEvent(EventTypeFileDiff, sessionID),
		Path:  path,
		Title: title,
		Diff:  diff,
		Lang:  lang,
	}
}

func NewFSListResultEvent(sessionID, currentPath string, items []FSItem) FSListResultEvent {
	return FSListResultEvent{
		Event:       NewBaseEvent(EventTypeFSListResult, sessionID),
		CurrentPath: currentPath,
		Items:       items,
	}
}

func NewFSReadResultEvent(sessionID, path, content string, size int64, lang, encoding string, isText bool) FSReadResultEvent {
	return FSReadResultEvent{
		Event:    NewBaseEvent(EventTypeFSReadResult, sessionID),
		Path:     path,
		Content:  content,
		Size:     size,
		Lang:     lang,
		Encoding: encoding,
		IsText:   isText,
	}
}

func NewSessionCreatedEvent(sessionID string, summary SessionSummary) SessionCreatedEvent {
	return SessionCreatedEvent{
		Event:   NewBaseEvent(EventTypeSessionCreated, sessionID),
		Summary: summary,
	}
}

func NewSessionListResultEvent(sessionID string, items []SessionSummary) SessionListResultEvent {
	return SessionListResultEvent{
		Event: NewBaseEvent(EventTypeSessionListResult, sessionID),
		Items: items,
	}
}

func NewSessionHistoryEvent(sessionID string, summary SessionSummary, logEntries []HistoryLogEntry, diffs []HistoryContext, currentDiff *HistoryContext, reviewGroups []ReviewGroup, activeReviewGroup *ReviewGroup, currentStep, latestError *HistoryContext, rawTerminalByStream map[string]string, terminalExecutions []TerminalExecution, sessionContext SessionContext, skillCatalogMeta, memoryCatalogMeta CatalogMetadata, canResume bool, resumeRuntimeMeta RuntimeMeta) SessionHistoryEvent {
	return SessionHistoryEvent{
		Event:               NewBaseEvent(EventTypeSessionHistory, sessionID),
		Summary:             summary,
		LogEntries:          logEntries,
		Diffs:               diffs,
		CurrentDiff:         currentDiff,
		ReviewGroups:        reviewGroups,
		ActiveReviewGroup:   activeReviewGroup,
		CurrentStep:         currentStep,
		LatestError:         latestError,
		RawTerminalByStream: rawTerminalByStream,
		TerminalExecutions:  terminalExecutions,
		SessionContext:      sessionContext,
		SkillCatalogMeta:    skillCatalogMeta,
		MemoryCatalogMeta:   memoryCatalogMeta,
		CanResume:           canResume,
		ResumeRuntimeMeta:   resumeRuntimeMeta,
	}
}

func NewSkillCatalogResultEvent(sessionID string, meta CatalogMetadata, items []SkillDefinition) SkillCatalogResultEvent {
	return SkillCatalogResultEvent{Event: NewBaseEvent(EventTypeSkillCatalogResult, sessionID), Meta: meta, Items: items}
}

func NewMemoryListResultEvent(sessionID string, meta CatalogMetadata, items []MemoryItem) MemoryListResultEvent {
	return MemoryListResultEvent{Event: NewBaseEvent(EventTypeMemoryListResult, sessionID), Meta: meta, Items: items}
}

func NewCatalogAuthoringResultEvent(sessionID, domain, message string, skill *SkillDefinition, memory *MemoryItem) CatalogAuthoringResultEvent {
	return CatalogAuthoringResultEvent{Event: NewBaseEvent(EventTypeCatalogAuthoringResult, sessionID), Domain: domain, Skill: skill, Memory: memory, Message: message}
}

func NewSessionContextResultEvent(sessionID string, sessionContext SessionContext) SessionContextResultEvent {
	return SessionContextResultEvent{Event: NewBaseEvent(EventTypeSessionContextResult, sessionID), SessionContext: sessionContext}
}

func NewSkillSyncResultEvent(sessionID, message string) SkillSyncResultEvent {
	return SkillSyncResultEvent{Event: NewBaseEvent(EventTypeSkillSyncResult, sessionID), Message: message}
}

func NewCatalogSyncStatusEvent(sessionID, domain string, meta CatalogMetadata) CatalogSyncStatusEvent {
	return CatalogSyncStatusEvent{Event: NewBaseEvent(EventTypeCatalogSyncStatus, sessionID), Domain: domain, Meta: meta}
}

func NewCatalogSyncResultEvent(sessionID, domain string, success bool, message string, meta CatalogMetadata) CatalogSyncResultEvent {
	return CatalogSyncResultEvent{Event: NewBaseEvent(EventTypeCatalogSyncResult, sessionID), Domain: domain, Success: success, Message: message, Meta: meta}
}

func NewReviewStateEvent(sessionID string, groups []ReviewGroup, activeGroup *ReviewGroup) ReviewStateEvent {
	return ReviewStateEvent{
		Event:       NewBaseEvent(EventTypeReviewState, sessionID),
		Groups:      groups,
		ActiveGroup: activeGroup,
	}
}

func NewRuntimeInfoResultEvent(sessionID, query, title, message string, unavailable bool, items []RuntimeInfoItem) RuntimeInfoResultEvent {
	return RuntimeInfoResultEvent{
		Event:       NewBaseEvent(EventTypeRuntimeInfoResult, sessionID),
		Query:       query,
		Title:       title,
		Message:     message,
		Unavailable: unavailable,
		Items:       items,
	}
}

func NewADBDevicesResultEvent(sessionID string, devices []ADBDevice, selectedSerial string, availableAVDs []string, preferredAVD string, adbAvailable, emulatorAvailable bool, suggestedAction, message string) ADBDevicesResultEvent {
	return ADBDevicesResultEvent{
		Event:             NewBaseEvent(EventTypeADBDevicesResult, sessionID),
		Devices:           devices,
		SelectedSerial:    selectedSerial,
		AvailableAVDs:     availableAVDs,
		PreferredAVD:      preferredAVD,
		ADBAvailable:      adbAvailable,
		EmulatorAvailable: emulatorAvailable,
		SuggestedAction:   suggestedAction,
		Message:           message,
	}
}

func NewADBStreamStateEvent(sessionID string, running bool, serial string, width, height, intervalMS int, message string) ADBStreamStateEvent {
	return ADBStreamStateEvent{
		Event:      NewBaseEvent(EventTypeADBStreamState, sessionID),
		Running:    running,
		Serial:     serial,
		Width:      width,
		Height:     height,
		IntervalMS: intervalMS,
		Message:    message,
	}
}

func NewADBFrameEvent(sessionID, serial, format, image string, width, height, seq int) ADBFrameEvent {
	return ADBFrameEvent{
		Event:  NewBaseEvent(EventTypeADBFrame, sessionID),
		Serial: serial,
		Format: format,
		Width:  width,
		Height: height,
		Seq:    seq,
		Image:  image,
	}
}

func MergeRuntimeMeta(base, overlay RuntimeMeta) RuntimeMeta {
	merged := base
	if overlay.Source != "" {
		merged.Source = overlay.Source
	}
	if overlay.SkillName != "" {
		merged.SkillName = overlay.SkillName
	}
	if overlay.Target != "" {
		merged.Target = overlay.Target
	}
	if overlay.TargetType != "" {
		merged.TargetType = overlay.TargetType
	}
	if overlay.TargetPath != "" {
		merged.TargetPath = overlay.TargetPath
	}
	if overlay.ResultView != "" {
		merged.ResultView = overlay.ResultView
	}
	if overlay.ResumeSessionID != "" {
		merged.ResumeSessionID = overlay.ResumeSessionID
	}
	if overlay.ExecutionID != "" {
		merged.ExecutionID = overlay.ExecutionID
	}
	if overlay.GroupID != "" {
		merged.GroupID = overlay.GroupID
	}
	if overlay.GroupTitle != "" {
		merged.GroupTitle = overlay.GroupTitle
	}
	if overlay.ContextID != "" {
		merged.ContextID = overlay.ContextID
	}
	if overlay.ContextTitle != "" {
		merged.ContextTitle = overlay.ContextTitle
	}
	if overlay.TargetText != "" {
		merged.TargetText = overlay.TargetText
	}
	if overlay.Command != "" {
		merged.Command = overlay.Command
	}
	if overlay.Engine != "" {
		merged.Engine = overlay.Engine
	}
	if overlay.CWD != "" {
		merged.CWD = overlay.CWD
	}
	if overlay.PermissionMode != "" {
		merged.PermissionMode = overlay.PermissionMode
	}
	if overlay.ClaudeLifecycle != "" {
		merged.ClaudeLifecycle = overlay.ClaudeLifecycle
	}
	return merged
}

func DefaultInteractionActions(kind string, options []string) []InteractionAction {
	actions := make([]InteractionAction, 0, len(options))
	for _, option := range options {
		value := strings.TrimSpace(option)
		if value == "" {
			continue
		}
		lower := strings.ToLower(value)
		label := value
		variant := "outlined"
		decision := ""
		switch kind {
		case "permission":
			if lower == "y" || lower == "yes" || lower == "approve" || lower == "allow" {
				label = "允许"
				variant = "primary"
				decision = "approve"
			} else if lower == "n" || lower == "no" || lower == "deny" || lower == "reject" {
				label = "拒绝"
				variant = "tonal"
				decision = "deny"
			}
		case "review":
			switch lower {
			case "accept":
				label = "接受"
				variant = "primary"
				decision = "accept"
			case "revert":
				label = "撤销"
				variant = "tonal"
				decision = "revert"
			case "revise":
				label = "继续调整"
				variant = "outlined"
				decision = "revise"
			}
		}
		actions = append(actions, InteractionAction{
			ID:       value,
			Label:    label,
			Variant:  variant,
			Value:    value,
			Decision: decision,
		})
	}
	return actions
}

func ApplyRuntimeMeta(event any, meta RuntimeMeta) any {
	switch e := event.(type) {
	case Event:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case LogEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case ProgressEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case ErrorEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case InteractionRequestEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case PromptRequestEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case SessionStateEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case AgentStateEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case RuntimePhaseEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case StepUpdateEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case FileDiffEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case FSListResultEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case FSReadResultEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case SessionCreatedEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case SessionListResultEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case SessionHistoryEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case ReviewStateEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case SkillCatalogResultEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case MemoryListResultEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case CatalogAuthoringResultEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case SessionContextResultEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case SkillSyncResultEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case CatalogSyncStatusEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case CatalogSyncResultEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case RuntimeInfoResultEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	default:
		return event
	}
}
