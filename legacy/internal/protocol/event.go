package protocol

import (
	"strings"
	"time"
)

const (
	ActionContextWindowUsageGet       = "context_window_usage_get"
	ActionContextWindowsUsageGetAlias = "context_windows_usage_get"

	EventTypeLog                       = "log"
	EventTypeProgress                  = "progress"
	EventTypeError                     = "error"
	EventTypeClientActionAck           = "client_action_ack"
	EventTypeCompactResult             = "compact_result"
	EventTypeCompaction                = "compaction"
	EventTypeContextWindowUsage        = "context_window_usage"
	EventTypePromptRequest             = "prompt_request"
	EventTypeInteractionRequest        = "interaction_request"
	EventTypeSessionResumeResult       = "session_resume_result"
	EventTypeSessionResumeNotice       = "session_resume_notice"
	EventTypeSessionState              = "session_state"
	EventTypeAgentState                = "agent_state"
	EventTypeAIStatus                  = "ai_status"
	EventTypeRuntimePhase              = "runtime_phase"
	EventTypeTaskSnapshot              = "task_snapshot"
	EventTypeFSListResult              = "fs_list_result"
	EventTypeFSReadResult              = "fs_read_result"
	EventTypeFSWriteResult             = "fs_write_result"
	EventTypeMediaPreviewResult        = "media_preview_result"
	EventTypeStepUpdate                = "step_update"
	EventTypeFileDiff                  = "file_diff"
	EventTypeRuntimeInfoResult         = "runtime_info_result"
	EventTypeRuntimeProcessList        = "runtime_process_list_result"
	EventTypeRuntimeProcessLog         = "runtime_process_log_result"
	EventTypeSessionCreated            = "session_created"
	EventTypeSessionListResult         = "session_list_result"
	EventTypeSessionHistory            = "session_history"
	EventTypeSessionHistoryPage        = "session_history_page"
	EventTypeSessionDelta              = "session_delta"
	EventTypeSessionTerminalRange      = "session_terminal_range"
	EventTypeSessionDiffPage           = "session_diff_page"
	EventTypeSessionTerminalExecPage   = "session_terminal_execution_page"
	EventTypeSessionUpdated            = "session_updated"
	EventTypeReviewState               = "review_state"
	EventTypeSkillCatalogResult        = "skill_catalog_result"
	EventTypeMemoryListResult          = "memory_list_result"
	EventTypeCatalogAuthoringResult    = "catalog_authoring_result"
	EventTypeSessionContextResult      = "session_context_result"
	EventTypePermissionRuleListResult  = "permission_rule_list_result"
	EventTypePermissionAutoApplied     = "permission_auto_applied"
	EventTypeSkillSyncResult           = "skill_sync_result"
	EventTypeCatalogSyncStatus         = "catalog_sync_status"
	EventTypeCatalogSyncResult         = "catalog_sync_result"
	EventTypeADBDevicesResult          = "adb_devices_result"
	EventTypeADBStreamState            = "adb_stream_state"
	EventTypeADBFrame                  = "adb_frame"
	EventTypeADBWebRTCAnswer           = "adb_webrtc_answer"
	EventTypeADBWebRTCState            = "adb_webrtc_state"
	EventTypeRelayDeviceRegisterResult = "relay_device_register_result"
	EventTypeRelayDeviceListResult     = "relay_device_list_result"
	EventTypeRelayDeviceRevokeResult   = "relay_device_revoke_result"
	EventTypeRelayDeviceRotateResult   = "relay_device_rotate_result"
	EventTypeThinking                  = "thinking"
)

type RuntimeMeta struct {
	Source              string `json:"source,omitempty"`
	SkillName           string `json:"skillName,omitempty"`
	Target              string `json:"target,omitempty"`
	TargetType          string `json:"targetType,omitempty"`
	TargetPath          string `json:"targetPath,omitempty"`
	ResultView          string `json:"resultView,omitempty"`
	ResumeSessionID     string `json:"resumeSessionId,omitempty"`
	ExecutionID         string `json:"executionId,omitempty"`
	GroupID             string `json:"groupId,omitempty"`
	GroupTitle          string `json:"groupTitle,omitempty"`
	ContextID           string `json:"contextId,omitempty"`
	ContextTitle        string `json:"contextTitle,omitempty"`
	TargetText          string `json:"targetText,omitempty"`
	Command             string `json:"command,omitempty"`
	Engine              string `json:"engine,omitempty"`
	Model               string `json:"model,omitempty"`
	ReasoningEffort     string `json:"reasoningEffort,omitempty"`
	CodexSandboxMode    string `json:"codexSandboxMode,omitempty"`
	CWD                 string `json:"cwd,omitempty"`
	PermissionMode      string `json:"permissionMode,omitempty"`
	PermissionRequestID string `json:"permissionRequestId,omitempty"`
	ClaudeSessionUUID   string `json:"claudeSessionUUID,omitempty"`
	ClaudeLifecycle     string `json:"claudeLifecycle,omitempty"`
	BlockingKind        string `json:"blockingKind,omitempty"`
	SourcePath          string `json:"sourcePath,omitempty"`
	SourceSize          int64  `json:"sourceSize,omitempty"`
	SourceModUnixNS     int64  `json:"sourceModUnixNs,omitempty"`
	SourceEntryCount    int    `json:"sourceEntryCount,omitempty"`
}

type Event struct {
	Type        string    `json:"type"`
	Timestamp   time.Time `json:"timestamp"`
	SessionID   string    `json:"sessionId,omitempty"`
	EventCursor int64     `json:"eventCursor,omitempty"`
	RuntimeMeta
}

type ClientEvent struct {
	Action         string `json:"action"`
	SessionID      string `json:"sessionId,omitempty"`
	ClientActionID string `json:"clientActionId,omitempty"`
}

type RelayDeviceRegisterRequestEvent struct {
	ClientEvent
	DeviceID                string `json:"deviceId"`
	DisplayName             string `json:"displayName"`
	DeviceIdentityPublicKey string `json:"deviceIdentityPublicKey"`
	DeviceCredential        string `json:"deviceCredential"`
}

type RelayDeviceRegisterResultEvent struct {
	Event
	DeviceID       string `json:"deviceId"`
	FingerprintHex string `json:"fingerprintHex"`
	Status         string `json:"status"`
}

type RelayDeviceListRequestEvent struct {
	ClientEvent
}

type RelayDeviceRevokeRequestEvent struct {
	ClientEvent
	DeviceID string `json:"deviceId"`
}

type RelayDeviceRotateRequestEvent struct {
	ClientEvent
}

type RelayTrustedDevice struct {
	DeviceID        string `json:"deviceId"`
	DisplayName     string `json:"displayName"`
	FingerprintHex  string `json:"fingerprintHex"`
	CreatedAt       string `json:"createdAt"`
	LastSeenAt      string `json:"lastSeenAt"`
	RevokedAt       string `json:"revokedAt,omitempty"`
	ActiveSessionID string `json:"activeSessionId,omitempty"`
	Connected       bool   `json:"connected"`
	CurrentDevice   bool   `json:"currentDevice"`
	Revoked         bool   `json:"revoked"`
}

type RelayDeviceListResultEvent struct {
	Event
	Devices []RelayTrustedDevice `json:"devices"`
}

type RelayDeviceRevokeResultEvent struct {
	Event
	DeviceID string `json:"deviceId"`
	Status   string `json:"status"`
}

type RelayDeviceRotateResultEvent struct {
	Event
	NodeFingerprintHex string `json:"nodeFingerprintHex"`
	Status             string `json:"status"`
}

type ExecRequestEvent struct {
	ClientEvent
	Command        string `json:"cmd"`
	CWD            string `json:"cwd,omitempty"`
	Mode           string `json:"mode,omitempty"`
	PermissionMode string `json:"permissionMode,omitempty"`
	InputData      string `json:"inputData,omitempty"`
	RuntimeMeta
}

type InputRequestEvent struct {
	ClientEvent
	Data             string            `json:"data"`
	PermissionMode   string            `json:"permissionMode,omitempty"`
	ImageAttachments []ImageAttachment `json:"imageAttachments,omitempty"`
	RuntimeMeta
}

type AITurnRequestEvent struct {
	ClientEvent
	Engine           string            `json:"engine,omitempty"`
	Data             string            `json:"data,omitempty"`
	CWD              string            `json:"cwd,omitempty"`
	PermissionMode   string            `json:"permissionMode,omitempty"`
	ImageAttachments []ImageAttachment `json:"imageAttachments,omitempty"`
	RuntimeMeta
}

type ImageAttachment struct {
	Name     string `json:"name,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
	Data     string `json:"data"`
}

type TimelineAttachment struct {
	ID            string `json:"id,omitempty"`
	Kind          string `json:"kind,omitempty"`
	Name          string `json:"name,omitempty"`
	MIMEType      string `json:"mimeType,omitempty"`
	Size          int64  `json:"size,omitempty"`
	Path          string `json:"path,omitempty"`
	PreviewStatus string `json:"previewStatus,omitempty"`
	Source        string `json:"source,omitempty"`
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
	IsReviewOnly   bool   `json:"is_review_only,omitempty"`
}

type PermissionDecisionRequestEvent struct {
	ClientEvent
	Decision            string `json:"decision"`
	Scope               string `json:"scope,omitempty"`
	PermissionMode      string `json:"permissionMode,omitempty"`
	CodexSandboxMode    string `json:"codexSandboxMode,omitempty"`
	PermissionRequestID string `json:"permissionRequestId,omitempty"`
	ResumeSessionID     string `json:"resumeSessionId,omitempty"`
	TargetPath          string `json:"targetPath,omitempty"`
	ContextID           string `json:"contextId,omitempty"`
	ContextTitle        string `json:"contextTitle,omitempty"`
	PromptMessage       string `json:"promptMessage,omitempty"`
	FallbackCommand     string `json:"command,omitempty"`
	FallbackCWD         string `json:"cwd,omitempty"`
	FallbackEngine      string `json:"engine,omitempty"`
	FallbackTarget      string `json:"target,omitempty"`
	FallbackTargetType  string `json:"targetType,omitempty"`
}

type PermissionRuleListRequestEvent struct {
	ClientEvent
}

type PermissionRuleRequestEvent struct {
	ClientEvent
	Rule PermissionRule `json:"rule,omitempty"`
}

type PermissionRuleDeleteRequestEvent struct {
	ClientEvent
	ID    string `json:"id"`
	Scope string `json:"scope,omitempty"`
}

type PermissionRuleToggleRequestEvent struct {
	ClientEvent
	Scope   string `json:"scope,omitempty"`
	Enabled bool   `json:"enabled"`
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

type CompactRequestEvent struct {
	ClientEvent
	CWD            string `json:"cwd,omitempty"`
	Engine         string `json:"engine,omitempty"`
	PermissionMode string `json:"permissionMode,omitempty"`
	RuntimeMeta
}

type SkillCatalogRequestEvent struct {
	ClientEvent
	Skill SkillDefinition `json:"skill,omitempty"`
}

type MemoryRequestEvent struct {
	ClientEvent
	CWD  string     `json:"cwd,omitempty"`
	Item MemoryItem `json:"item,omitempty"`
}

type SessionContextUpdateRequestEvent struct {
	ClientEvent
	EnabledSkillNames []string `json:"enabledSkillNames,omitempty"`
	EnabledMemoryIDs  []string `json:"enabledMemoryIds,omitempty"`
}

type ContextWindowUsageRequestEvent struct {
	ClientEvent
	SessionID string `json:"sessionId,omitempty"`
	CWD       string `json:"cwd,omitempty"`
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

type ContextWindowUsage struct {
	TokensUsed int `json:"tokensUsed,omitempty"`
	TokenLimit int `json:"tokenLimit,omitempty"`
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

type FSWriteRequestEvent struct {
	ClientEvent
	Path    string `json:"path,omitempty"`
	Content string `json:"content"`
}

type MediaPreviewRequestEvent struct {
	ClientEvent
	AttachmentID string `json:"attachmentId,omitempty"`
	Path         string `json:"path,omitempty"`
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

type ADBSwipeRequestEvent struct {
	ClientEvent
	Serial     string `json:"serial,omitempty"`
	StartX     int    `json:"startX"`
	StartY     int    `json:"startY"`
	EndX       int    `json:"endX"`
	EndY       int    `json:"endY"`
	DurationMS int    `json:"durationMs,omitempty"`
}

type ADBKeyeventRequestEvent struct {
	ClientEvent
	Serial  string `json:"serial,omitempty"`
	Keycode string `json:"keycode"`
}

type WebRTCIceServer struct {
	URLs       []string `json:"urls,omitempty"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type ADBWebRTCOfferRequestEvent struct {
	ClientEvent
	Serial     string            `json:"serial,omitempty"`
	Type       string            `json:"sdpType,omitempty"`
	SDP        string            `json:"sdp,omitempty"`
	ICEServers []WebRTCIceServer `json:"iceServers,omitempty"`
}

type ADBWebRTCStopRequestEvent struct {
	ClientEvent
}

type RuntimeInfoRequestEvent struct {
	ClientEvent
	Query string `json:"query,omitempty"`
	CWD   string `json:"cwd,omitempty"`
}

type RuntimeProcessListRequestEvent struct {
	ClientEvent
}

type RuntimeProcessLogRequestEvent struct {
	ClientEvent
	PID int `json:"pid"`
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
	Title  string `json:"title,omitempty"`
	CWD    string `json:"cwd,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type SessionListRequestEvent struct {
	ClientEvent
	CWD string `json:"cwd,omitempty"`
}

type SessionLoadRequestEvent struct {
	ClientEvent
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type RegisterPushTokenRequestEvent struct {
	ClientEvent
	SessionID string `json:"sessionId"`
	Token     string `json:"token"`
	Platform  string `json:"platform"`
}

type SessionResumeRequestEvent struct {
	ClientEvent
	RuntimeMeta
	SessionID             string `json:"sessionId"`
	CWD                   string `json:"cwd,omitempty"`
	Reason                string `json:"reason,omitempty"`
	LastSeenEventCursor   int64  `json:"lastSeenEventCursor,omitempty"`
	LastKnownRuntimeState string `json:"lastKnownRuntimeState,omitempty"`
	Limit                 int    `json:"limit,omitempty"`
}

type SessionDeltaKnown struct {
	EventCursor            int64 `json:"eventCursor,omitempty"`
	LogEntryCount          int   `json:"logEntryCount,omitempty"`
	DiffCount              int   `json:"diffCount,omitempty"`
	TerminalExecutionCount int   `json:"terminalExecutionCount,omitempty"`
	TerminalStdoutLength   int   `json:"terminalStdoutLength,omitempty"`
	TerminalStderrLength   int   `json:"terminalStderrLength,omitempty"`
}

type SessionDeltaRequestEvent struct {
	ClientEvent
	SessionID string            `json:"sessionId"`
	CWD       string            `json:"cwd,omitempty"`
	Reason    string            `json:"reason,omitempty"`
	Known     SessionDeltaKnown `json:"known,omitempty"`
}

type SessionHistoryPageRequestEvent struct {
	ClientEvent
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd,omitempty"`
	Before    int    `json:"before,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type SessionTerminalRangeRequestEvent struct {
	ClientEvent
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd,omitempty"`
	Stream    string `json:"stream,omitempty"`
	Start     int    `json:"start,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type SessionDiffPageRequestEvent struct {
	ClientEvent
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd,omitempty"`
	Before    int    `json:"before,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type SessionTerminalExecutionPageRequestEvent struct {
	ClientEvent
	SessionID     string `json:"sessionId"`
	CWD           string `json:"cwd,omitempty"`
	Before        int    `json:"before,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	IncludeOutput bool   `json:"includeOutput,omitempty"`
}

type SessionDeleteRequestEvent struct {
	ClientEvent
	SessionID string `json:"sessionId"`
}

type SessionSummary struct {
	ID              string      `json:"id"`
	Title           string      `json:"title"`
	CreatedAt       string      `json:"createdAt,omitempty"`
	UpdatedAt       string      `json:"updatedAt,omitempty"`
	LastPreview     string      `json:"lastPreview,omitempty"`
	EntryCount      int         `json:"entryCount,omitempty"`
	Source          string      `json:"source,omitempty"`
	External        bool        `json:"external,omitempty"`
	Ownership       string      `json:"ownership,omitempty"`
	ExecutionActive bool        `json:"executionActive,omitempty"`
	Runtime         RuntimeMeta `json:"runtime,omitempty"`
}

type HistoryContext struct {
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
	Kind        string               `json:"kind"`
	Message     string               `json:"message,omitempty"`
	Label       string               `json:"label,omitempty"`
	Timestamp   string               `json:"timestamp,omitempty"`
	Stream      string               `json:"stream,omitempty"`
	Text        string               `json:"text,omitempty"`
	ExecutionID string               `json:"executionId,omitempty"`
	Phase       string               `json:"phase,omitempty"`
	ExitCode    *int                 `json:"exitCode,omitempty"`
	Context     *HistoryContext      `json:"context,omitempty"`
	Attachments []TimelineAttachment `json:"attachments,omitempty"`
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
	LogEntryStart       int                 `json:"logEntryStart,omitempty"`
	LogEntryTotal       int                 `json:"logEntryTotal,omitempty"`
	HasMoreBefore       bool                `json:"hasMoreBefore,omitempty"`
	Diffs               []HistoryContext    `json:"diffs,omitempty"`
	CurrentDiff         *HistoryContext     `json:"currentDiff,omitempty"`
	ReviewGroups        []ReviewGroup       `json:"reviewGroups,omitempty"`
	ActiveReviewGroup   *ReviewGroup        `json:"activeReviewGroup,omitempty"`
	CurrentStep         *HistoryContext     `json:"currentStep,omitempty"`
	LatestError         *HistoryContext     `json:"latestError,omitempty"`
	RawTerminalByStream map[string]string   `json:"rawTerminalByStream,omitempty"`
	TerminalExecutions  []TerminalExecution `json:"terminalExecutions,omitempty"`
	ContextWindowUsage  ContextWindowUsage  `json:"contextWindowUsage,omitempty"`
	SessionContext      SessionContext      `json:"sessionContext,omitempty"`
	SkillCatalogMeta    CatalogMetadata     `json:"skillCatalogMeta,omitempty"`
	MemoryCatalogMeta   CatalogMetadata     `json:"memoryCatalogMeta,omitempty"`
	Latest              SessionDeltaKnown   `json:"latest,omitempty"`
	CanResume           bool                `json:"canResume,omitempty"`
	RuntimeAlive        bool                `json:"runtimeAlive,omitempty"`
	ResumeRuntimeMeta   RuntimeMeta         `json:"resumeRuntimeMeta,omitempty"`
	PayloadLimited      bool                `json:"payloadLimited,omitempty"`
	PayloadLimitReason  string              `json:"payloadLimitReason,omitempty"`
}

type SessionDeltaEvent struct {
	Event
	Summary             SessionSummary      `json:"summary"`
	Base                SessionDeltaKnown   `json:"base,omitempty"`
	Latest              SessionDeltaKnown   `json:"latest,omitempty"`
	AppendLogEntries    []HistoryLogEntry   `json:"appendLogEntries,omitempty"`
	UpsertDiffs         []HistoryContext    `json:"upsertDiffs,omitempty"`
	CurrentDiff         *HistoryContext     `json:"currentDiff,omitempty"`
	ReviewGroups        []ReviewGroup       `json:"reviewGroups,omitempty"`
	ActiveReviewGroup   *ReviewGroup        `json:"activeReviewGroup,omitempty"`
	CurrentStep         *HistoryContext     `json:"currentStep,omitempty"`
	LatestError         *HistoryContext     `json:"latestError,omitempty"`
	RawTerminalByStream map[string]string   `json:"rawTerminalByStream,omitempty"`
	TerminalExecutions  []TerminalExecution `json:"terminalExecutions,omitempty"`
	ContextWindowUsage  ContextWindowUsage  `json:"contextWindowUsage,omitempty"`
	SessionContext      SessionContext      `json:"sessionContext,omitempty"`
	SkillCatalogMeta    CatalogMetadata     `json:"skillCatalogMeta,omitempty"`
	MemoryCatalogMeta   CatalogMetadata     `json:"memoryCatalogMeta,omitempty"`
	CanResume           bool                `json:"canResume,omitempty"`
	RuntimeAlive        bool                `json:"runtimeAlive,omitempty"`
	ResumeRuntimeMeta   RuntimeMeta         `json:"resumeRuntimeMeta,omitempty"`
	RequiresFullSync    bool                `json:"requiresFullSync,omitempty"`
	PayloadLimited      bool                `json:"payloadLimited,omitempty"`
	PayloadLimitReason  string              `json:"payloadLimitReason,omitempty"`
}

type SessionUpdatedEvent struct {
	Event
	Generation  uint64 `json:"generation,omitempty"`
	EventCursor int64  `json:"eventCursor,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type SessionHistoryPageEvent struct {
	Event
	LogEntries         []HistoryLogEntry `json:"logEntries,omitempty"`
	LogEntryStart      int               `json:"logEntryStart,omitempty"`
	LogEntryTotal      int               `json:"logEntryTotal,omitempty"`
	HasMoreBefore      bool              `json:"hasMoreBefore,omitempty"`
	ResumeRuntimeMeta  RuntimeMeta       `json:"resumeRuntimeMeta,omitempty"`
	Latest             SessionDeltaKnown `json:"latest,omitempty"`
	PayloadLimited     bool              `json:"payloadLimited,omitempty"`
	PayloadLimitReason string            `json:"payloadLimitReason,omitempty"`
}

type SessionTerminalRangeEvent struct {
	Event
	Stream             string            `json:"stream,omitempty"`
	Start              int               `json:"start,omitempty"`
	End                int               `json:"end,omitempty"`
	Total              int               `json:"total,omitempty"`
	Content            string            `json:"content,omitempty"`
	Latest             SessionDeltaKnown `json:"latest,omitempty"`
	PayloadLimited     bool              `json:"payloadLimited,omitempty"`
	PayloadLimitReason string            `json:"payloadLimitReason,omitempty"`
	SuggestedLimit     int               `json:"suggestedLimit,omitempty"`
}

type SessionDiffPageEvent struct {
	Event
	Diffs              []HistoryContext  `json:"diffs,omitempty"`
	DiffStart          int               `json:"diffStart,omitempty"`
	DiffTotal          int               `json:"diffTotal,omitempty"`
	HasMoreBefore      bool              `json:"hasMoreBefore,omitempty"`
	ReviewGroups       []ReviewGroup     `json:"reviewGroups,omitempty"`
	ActiveReviewGroup  *ReviewGroup      `json:"activeReviewGroup,omitempty"`
	CurrentDiff        *HistoryContext   `json:"currentDiff,omitempty"`
	Latest             SessionDeltaKnown `json:"latest,omitempty"`
	PayloadLimited     bool              `json:"payloadLimited,omitempty"`
	PayloadLimitReason string            `json:"payloadLimitReason,omitempty"`
}

type SessionTerminalExecutionPageEvent struct {
	Event
	TerminalExecutions []TerminalExecution `json:"terminalExecutions,omitempty"`
	ExecutionStart     int                 `json:"executionStart,omitempty"`
	ExecutionTotal     int                 `json:"executionTotal,omitempty"`
	HasMoreBefore      bool                `json:"hasMoreBefore,omitempty"`
	IncludeOutput      bool                `json:"includeOutput,omitempty"`
	Latest             SessionDeltaKnown   `json:"latest,omitempty"`
	PayloadLimited     bool                `json:"payloadLimited,omitempty"`
	PayloadLimitReason string              `json:"payloadLimitReason,omitempty"`
}

type SessionResumeResultEvent struct {
	Event
	LatestCursor  int64  `json:"latestCursor,omitempty"`
	RuntimeAlive  bool   `json:"runtimeAlive"`
	RuntimeState  string `json:"runtimeState,omitempty"`
	Reattaching   bool   `json:"reattaching,omitempty"`
	ReplayedCount int    `json:"replayedCount,omitempty"`
	Message       string `json:"msg,omitempty"`
}

type SessionResumeNoticeEvent struct {
	Event
	NoticeType string `json:"noticeType,omitempty"`
	Level      string `json:"level,omitempty"`
	Title      string `json:"title,omitempty"`
	Message    string `json:"msg,omitempty"`
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

type PermissionRuleListResultEvent struct {
	Event
	SessionEnabled    bool             `json:"sessionEnabled"`
	PersistentEnabled bool             `json:"persistentEnabled"`
	SessionRules      []PermissionRule `json:"sessionRules,omitempty"`
	PersistentRules   []PermissionRule `json:"persistentRules,omitempty"`
}

type PermissionAutoAppliedEvent struct {
	Event
	RuleID  string `json:"ruleId,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Summary string `json:"summary,omitempty"`
	Message string `json:"msg,omitempty"`
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
	Meta      any    `json:"meta,omitempty"`
}

type RuntimeProcessItem struct {
	PID          int    `json:"pid"`
	PPID         int    `json:"ppid,omitempty"`
	State        string `json:"state,omitempty"`
	Elapsed      string `json:"elapsed,omitempty"`
	Command      string `json:"command,omitempty"`
	CWD          string `json:"cwd,omitempty"`
	ExecutionID  string `json:"executionId,omitempty"`
	Source       string `json:"source,omitempty"`
	Root         bool   `json:"root,omitempty"`
	LogAvailable bool   `json:"logAvailable,omitempty"`
}

type PermissionRule struct {
	ID               string `json:"id"`
	Scope            string `json:"scope,omitempty"`
	Enabled          bool   `json:"enabled"`
	Engine           string `json:"engine,omitempty"`
	Kind             string `json:"kind,omitempty"`
	CommandHead      string `json:"commandHead,omitempty"`
	TargetPathPrefix string `json:"targetPathPrefix,omitempty"`
	Summary          string `json:"summary,omitempty"`
	CreatedAt        string `json:"createdAt,omitempty"`
	LastMatchedAt    string `json:"lastMatchedAt,omitempty"`
	MatchCount       int    `json:"matchCount,omitempty"`
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

type ClientActionAckEvent struct {
	Event
	Action         string `json:"action,omitempty"`
	ClientActionID string `json:"clientActionId,omitempty"`
	Status         string `json:"status,omitempty"`
	Duplicate      bool   `json:"duplicate,omitempty"`
}

type CompactResultEvent struct {
	Event
	Accepted bool   `json:"accepted"`
	Error    string `json:"error,omitempty"`
}

type CompactionEvent struct {
	Event
	ContextID string `json:"contextId,omitempty"`
	Status    string `json:"status,omitempty"`
	Trigger   string `json:"trigger,omitempty"`
	Message   string `json:"msg,omitempty"`
}

type ContextWindowUsageEvent struct {
	Event
	Usage ContextWindowUsage `json:"usage"`
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

type AIStatusEvent struct {
	Event
	Visible bool   `json:"visible"`
	Label   string `json:"label,omitempty"`
	Phase   string `json:"phase,omitempty"`
}

func (e AIStatusEvent) GetRuntimeMeta() RuntimeMeta { return e.RuntimeMeta }

func (e CompactionEvent) GetRuntimeMeta() RuntimeMeta { return e.RuntimeMeta }

func (e ContextWindowUsageEvent) GetRuntimeMeta() RuntimeMeta { return e.RuntimeMeta }

type RuntimePhaseEvent struct {
	Event
	Phase   string `json:"phase"`
	Kind    string `json:"kind,omitempty"`
	Message string `json:"msg,omitempty"`
}

func (e RuntimePhaseEvent) GetRuntimeMeta() RuntimeMeta { return e.RuntimeMeta }

type TaskSnapshotEvent struct {
	Event
	State        string    `json:"state"`
	Message      string    `json:"msg,omitempty"`
	RuntimeAlive bool      `json:"runtimeAlive"`
	AwaitInput   bool      `json:"awaitInput,omitempty"`
	Command      string    `json:"command,omitempty"`
	Step         string    `json:"step,omitempty"`
	Tool         string    `json:"tool,omitempty"`
	LatestCursor int64     `json:"latestCursor,omitempty"`
	LastOutputAt time.Time `json:"lastOutputAt,omitempty"`
	Syncing      bool      `json:"syncing,omitempty"`
}

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

type FSWriteResultEvent struct {
	Event
	Path     string `json:"path"`
	Content  string `json:"content"`
	Size     int64  `json:"size"`
	Lang     string `json:"lang,omitempty"`
	Encoding string `json:"encoding,omitempty"`
	IsText   bool   `json:"isText"`
}

type MediaPreviewResultEvent struct {
	Event
	AttachmentID string `json:"attachmentId,omitempty"`
	Path         string `json:"path"`
	Content      string `json:"content,omitempty"`
	Size         int64  `json:"size,omitempty"`
	MIMEType     string `json:"mimeType,omitempty"`
	Status       string `json:"status"`
	Message      string `json:"message,omitempty"`
}

type RuntimeInfoResultEvent struct {
	Event
	Query       string            `json:"query,omitempty"`
	Title       string            `json:"title,omitempty"`
	Items       []RuntimeInfoItem `json:"items,omitempty"`
	Unavailable bool              `json:"unavailable,omitempty"`
	Message     string            `json:"msg,omitempty"`
}

type RuntimeProcessListResultEvent struct {
	Event
	RootPID int                  `json:"rootPid,omitempty"`
	Items   []RuntimeProcessItem `json:"items,omitempty"`
	Message string               `json:"msg,omitempty"`
}

type RuntimeProcessLogResultEvent struct {
	Event
	PID         int    `json:"pid,omitempty"`
	ExecutionID string `json:"executionId,omitempty"`
	Command     string `json:"command,omitempty"`
	CWD         string `json:"cwd,omitempty"`
	Source      string `json:"source,omitempty"`
	Stdout      string `json:"stdout,omitempty"`
	Stderr      string `json:"stderr,omitempty"`
	Message     string `json:"msg,omitempty"`
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

type ADBWebRTCAnswerEvent struct {
	Event
	Serial string `json:"serial,omitempty"`
	Type   string `json:"sdpType,omitempty"`
	SDP    string `json:"sdp,omitempty"`
}

type ADBWebRTCStateEvent struct {
	Event
	Running   bool   `json:"running"`
	Connected bool   `json:"connected"`
	Serial    string `json:"serial,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	Message   string `json:"msg,omitempty"`
}

type ThinkingEvent struct {
	Event
	Content string `json:"content"`
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

func NewErrorEventWithCode(sessionID, message, stack, code string) ErrorEvent {
	event := NewErrorEvent(sessionID, message, stack)
	event.Code = code
	return event
}

func NewClientActionAckEvent(sessionID, action, clientActionID, status string, duplicate bool) ClientActionAckEvent {
	return ClientActionAckEvent{
		Event:          NewBaseEvent(EventTypeClientActionAck, sessionID),
		Action:         action,
		ClientActionID: clientActionID,
		Status:         status,
		Duplicate:      duplicate,
	}
}

func NewCompactResultEvent(sessionID string, accepted bool, errText string) CompactResultEvent {
	return CompactResultEvent{
		Event:    NewBaseEvent(EventTypeCompactResult, sessionID),
		Accepted: accepted,
		Error:    strings.TrimSpace(errText),
	}
}

func NewCompactionEvent(sessionID, contextID, status, trigger, message string) CompactionEvent {
	return CompactionEvent{
		Event:     NewBaseEvent(EventTypeCompaction, sessionID),
		ContextID: strings.TrimSpace(contextID),
		Status:    strings.TrimSpace(status),
		Trigger:   strings.TrimSpace(trigger),
		Message:   strings.TrimSpace(message),
	}
}

func NewContextWindowUsageEvent(sessionID string, usage ContextWindowUsage) ContextWindowUsageEvent {
	return ContextWindowUsageEvent{
		Event: NewBaseEvent(EventTypeContextWindowUsage, sessionID),
		Usage: NormalizeContextWindowUsage(usage),
	}
}

func NewRelayDeviceRegisterResultEvent(sessionID, deviceID, fingerprintHex, status string) RelayDeviceRegisterResultEvent {
	return RelayDeviceRegisterResultEvent{
		Event:          NewBaseEvent(EventTypeRelayDeviceRegisterResult, sessionID),
		DeviceID:       deviceID,
		FingerprintHex: fingerprintHex,
		Status:         status,
	}
}

func NewRelayDeviceListResultEvent(sessionID string, devices []RelayTrustedDevice) RelayDeviceListResultEvent {
	if devices == nil {
		devices = []RelayTrustedDevice{}
	}
	return RelayDeviceListResultEvent{
		Event:   NewBaseEvent(EventTypeRelayDeviceListResult, sessionID),
		Devices: devices,
	}
}

func NewRelayDeviceRevokeResultEvent(sessionID, deviceID, status string) RelayDeviceRevokeResultEvent {
	return RelayDeviceRevokeResultEvent{
		Event:    NewBaseEvent(EventTypeRelayDeviceRevokeResult, sessionID),
		DeviceID: deviceID,
		Status:   status,
	}
}

func NewRelayDeviceRotateResultEvent(sessionID, nodeFingerprintHex, status string) RelayDeviceRotateResultEvent {
	return RelayDeviceRotateResultEvent{
		Event:              NewBaseEvent(EventTypeRelayDeviceRotateResult, sessionID),
		NodeFingerprintHex: nodeFingerprintHex,
		Status:             status,
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

func NewAIStatusEvent(sessionID string, visible bool, label, phase string, meta RuntimeMeta) AIStatusEvent {
	event := AIStatusEvent{
		Event:   NewBaseEvent(EventTypeAIStatus, sessionID),
		Visible: visible,
		Label:   label,
		Phase:   phase,
	}
	event.RuntimeMeta = MergeRuntimeMeta(event.RuntimeMeta, meta)
	return event
}

func NewRuntimePhaseEvent(sessionID, phase, kind, message string) RuntimePhaseEvent {
	return RuntimePhaseEvent{
		Event:   NewBaseEvent(EventTypeRuntimePhase, sessionID),
		Phase:   phase,
		Kind:    kind,
		Message: message,
	}
}

func NewTaskSnapshotEvent(sessionID, state, message string, runtimeAlive, awaitInput bool, command, step, tool string, latestCursor int64, lastOutputAt time.Time, meta RuntimeMeta) TaskSnapshotEvent {
	event := TaskSnapshotEvent{
		Event:        NewBaseEvent(EventTypeTaskSnapshot, sessionID),
		State:        state,
		Message:      message,
		RuntimeAlive: runtimeAlive,
		AwaitInput:   awaitInput,
		Command:      command,
		Step:         step,
		Tool:         tool,
		LatestCursor: latestCursor,
		LastOutputAt: lastOutputAt,
	}
	event.RuntimeMeta = MergeRuntimeMeta(event.RuntimeMeta, meta)
	return event
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

func NewFSWriteResultEvent(sessionID, path, content string, size int64, lang, encoding string, isText bool) FSWriteResultEvent {
	return FSWriteResultEvent{
		Event:    NewBaseEvent(EventTypeFSWriteResult, sessionID),
		Path:     path,
		Content:  content,
		Size:     size,
		Lang:     lang,
		Encoding: encoding,
		IsText:   isText,
	}
}

func NewMediaPreviewResultEvent(sessionID, attachmentID, path, content string, size int64, mimeType, status, message string) MediaPreviewResultEvent {
	return MediaPreviewResultEvent{
		Event:        NewBaseEvent(EventTypeMediaPreviewResult, sessionID),
		AttachmentID: attachmentID,
		Path:         path,
		Content:      content,
		Size:         size,
		MIMEType:     mimeType,
		Status:       status,
		Message:      message,
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

func NewSessionHistoryEvent(sessionID string, summary SessionSummary, logEntries []HistoryLogEntry, diffs []HistoryContext, currentDiff *HistoryContext, reviewGroups []ReviewGroup, activeReviewGroup *ReviewGroup, currentStep, latestError *HistoryContext, rawTerminalByStream map[string]string, terminalExecutions []TerminalExecution, contextWindowUsage ContextWindowUsage, sessionContext SessionContext, skillCatalogMeta, memoryCatalogMeta CatalogMetadata, canResume, runtimeAlive bool, resumeRuntimeMeta RuntimeMeta) SessionHistoryEvent {
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
		ContextWindowUsage:  NormalizeContextWindowUsage(contextWindowUsage),
		SessionContext:      sessionContext,
		SkillCatalogMeta:    skillCatalogMeta,
		MemoryCatalogMeta:   memoryCatalogMeta,
		CanResume:           canResume,
		RuntimeAlive:        runtimeAlive,
		ResumeRuntimeMeta:   resumeRuntimeMeta,
	}
}

func NewSessionHistoryWindowEvent(sessionID string, summary SessionSummary, logEntries []HistoryLogEntry, logEntryStart, logEntryTotal int, diffs []HistoryContext, currentDiff *HistoryContext, reviewGroups []ReviewGroup, activeReviewGroup *ReviewGroup, currentStep, latestError *HistoryContext, rawTerminalByStream map[string]string, terminalExecutions []TerminalExecution, contextWindowUsage ContextWindowUsage, sessionContext SessionContext, skillCatalogMeta, memoryCatalogMeta CatalogMetadata, canResume, runtimeAlive bool, resumeRuntimeMeta RuntimeMeta) SessionHistoryEvent {
	event := NewSessionHistoryEvent(sessionID, summary, logEntries, diffs, currentDiff, reviewGroups, activeReviewGroup, currentStep, latestError, rawTerminalByStream, terminalExecutions, contextWindowUsage, sessionContext, skillCatalogMeta, memoryCatalogMeta, canResume, runtimeAlive, resumeRuntimeMeta)
	event.LogEntryStart = logEntryStart
	event.LogEntryTotal = logEntryTotal
	event.HasMoreBefore = logEntryStart > 0
	return event
}

func NewSessionDeltaEvent(sessionID string, summary SessionSummary, base, latest SessionDeltaKnown, appendLogEntries []HistoryLogEntry, upsertDiffs []HistoryContext, currentDiff *HistoryContext, reviewGroups []ReviewGroup, activeReviewGroup *ReviewGroup, currentStep, latestError *HistoryContext, rawTerminalByStream map[string]string, terminalExecutions []TerminalExecution, contextWindowUsage ContextWindowUsage, sessionContext SessionContext, skillCatalogMeta, memoryCatalogMeta CatalogMetadata, canResume, runtimeAlive bool, resumeRuntimeMeta RuntimeMeta, requiresFullSync bool) SessionDeltaEvent {
	return SessionDeltaEvent{
		Event:               NewBaseEvent(EventTypeSessionDelta, sessionID),
		Summary:             summary,
		Base:                base,
		Latest:              latest,
		AppendLogEntries:    appendLogEntries,
		UpsertDiffs:         upsertDiffs,
		CurrentDiff:         currentDiff,
		ReviewGroups:        reviewGroups,
		ActiveReviewGroup:   activeReviewGroup,
		CurrentStep:         currentStep,
		LatestError:         latestError,
		RawTerminalByStream: rawTerminalByStream,
		TerminalExecutions:  terminalExecutions,
		ContextWindowUsage:  NormalizeContextWindowUsage(contextWindowUsage),
		SessionContext:      sessionContext,
		SkillCatalogMeta:    skillCatalogMeta,
		MemoryCatalogMeta:   memoryCatalogMeta,
		CanResume:           canResume,
		RuntimeAlive:        runtimeAlive,
		ResumeRuntimeMeta:   resumeRuntimeMeta,
		RequiresFullSync:    requiresFullSync,
	}
}

func NewSessionUpdatedEvent(sessionID string, generation uint64, eventCursor int64, reason string) SessionUpdatedEvent {
	return SessionUpdatedEvent{
		Event:       NewBaseEvent(EventTypeSessionUpdated, sessionID),
		Generation:  generation,
		EventCursor: eventCursor,
		Reason:      strings.TrimSpace(reason),
	}
}

func NewSessionHistoryPageEvent(sessionID string, logEntries []HistoryLogEntry, logEntryStart, logEntryTotal int, resumeRuntimeMeta RuntimeMeta, latest SessionDeltaKnown) SessionHistoryPageEvent {
	return SessionHistoryPageEvent{
		Event:             NewBaseEvent(EventTypeSessionHistoryPage, sessionID),
		LogEntries:        logEntries,
		LogEntryStart:     logEntryStart,
		LogEntryTotal:     logEntryTotal,
		HasMoreBefore:     logEntryStart > 0,
		ResumeRuntimeMeta: resumeRuntimeMeta,
		Latest:            latest,
	}
}

func NewSessionTerminalRangeEvent(sessionID, stream string, start, end, total int, content string, latest SessionDeltaKnown) SessionTerminalRangeEvent {
	return SessionTerminalRangeEvent{
		Event:   NewBaseEvent(EventTypeSessionTerminalRange, sessionID),
		Stream:  strings.TrimSpace(stream),
		Start:   start,
		End:     end,
		Total:   total,
		Content: content,
		Latest:  latest,
	}
}

func NewSessionDiffPageEvent(sessionID string, diffs []HistoryContext, diffStart, diffTotal int, reviewGroups []ReviewGroup, activeReviewGroup *ReviewGroup, currentDiff *HistoryContext, latest SessionDeltaKnown) SessionDiffPageEvent {
	return SessionDiffPageEvent{
		Event:             NewBaseEvent(EventTypeSessionDiffPage, sessionID),
		Diffs:             diffs,
		DiffStart:         diffStart,
		DiffTotal:         diffTotal,
		HasMoreBefore:     diffStart > 0,
		ReviewGroups:      reviewGroups,
		ActiveReviewGroup: activeReviewGroup,
		CurrentDiff:       currentDiff,
		Latest:            latest,
	}
}

func NewSessionTerminalExecutionPageEvent(sessionID string, terminalExecutions []TerminalExecution, executionStart, executionTotal int, includeOutput bool, latest SessionDeltaKnown) SessionTerminalExecutionPageEvent {
	return SessionTerminalExecutionPageEvent{
		Event:              NewBaseEvent(EventTypeSessionTerminalExecPage, sessionID),
		TerminalExecutions: terminalExecutions,
		ExecutionStart:     executionStart,
		ExecutionTotal:     executionTotal,
		HasMoreBefore:      executionStart > 0,
		IncludeOutput:      includeOutput,
		Latest:             latest,
	}
}

func NewSessionResumeResultEvent(sessionID string, latestCursor int64, runtimeAlive bool, runtimeState string, reattaching bool, replayedCount int, message string) SessionResumeResultEvent {
	return SessionResumeResultEvent{
		Event:         NewBaseEvent(EventTypeSessionResumeResult, sessionID),
		LatestCursor:  latestCursor,
		RuntimeAlive:  runtimeAlive,
		RuntimeState:  runtimeState,
		Reattaching:   reattaching,
		ReplayedCount: replayedCount,
		Message:       message,
	}
}

func NewSessionResumeNoticeEvent(sessionID, noticeType, level, title, message string) SessionResumeNoticeEvent {
	return SessionResumeNoticeEvent{
		Event:      NewBaseEvent(EventTypeSessionResumeNotice, sessionID),
		NoticeType: noticeType,
		Level:      level,
		Title:      title,
		Message:    message,
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

func NewPermissionRuleListResultEvent(sessionID string, sessionEnabled, persistentEnabled bool, sessionRules, persistentRules []PermissionRule) PermissionRuleListResultEvent {
	return PermissionRuleListResultEvent{
		Event:             NewBaseEvent(EventTypePermissionRuleListResult, sessionID),
		SessionEnabled:    sessionEnabled,
		PersistentEnabled: persistentEnabled,
		SessionRules:      sessionRules,
		PersistentRules:   persistentRules,
	}
}

func NewPermissionAutoAppliedEvent(sessionID, ruleID, scope, summary, message string) PermissionAutoAppliedEvent {
	return PermissionAutoAppliedEvent{
		Event:   NewBaseEvent(EventTypePermissionAutoApplied, sessionID),
		RuleID:  ruleID,
		Scope:   scope,
		Summary: summary,
		Message: message,
	}
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

func NewRuntimeProcessListResultEvent(sessionID string, rootPID int, items []RuntimeProcessItem, message string) RuntimeProcessListResultEvent {
	return RuntimeProcessListResultEvent{
		Event:   NewBaseEvent(EventTypeRuntimeProcessList, sessionID),
		RootPID: rootPID,
		Items:   items,
		Message: message,
	}
}

func NewRuntimeProcessLogResultEvent(sessionID string, pid int, executionID, command, cwd, source, stdout, stderr, message string) RuntimeProcessLogResultEvent {
	return RuntimeProcessLogResultEvent{
		Event:       NewBaseEvent(EventTypeRuntimeProcessLog, sessionID),
		PID:         pid,
		ExecutionID: executionID,
		Command:     command,
		CWD:         cwd,
		Source:      source,
		Stdout:      stdout,
		Stderr:      stderr,
		Message:     message,
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

func NewADBWebRTCAnswerEvent(sessionID, serial, sdpType, sdp string) ADBWebRTCAnswerEvent {
	return ADBWebRTCAnswerEvent{
		Event:  NewBaseEvent(EventTypeADBWebRTCAnswer, sessionID),
		Serial: serial,
		Type:   sdpType,
		SDP:    sdp,
	}
}

func NewADBWebRTCStateEvent(sessionID string, running, connected bool, serial string, width, height int, message string) ADBWebRTCStateEvent {
	return ADBWebRTCStateEvent{
		Event:     NewBaseEvent(EventTypeADBWebRTCState, sessionID),
		Running:   running,
		Connected: connected,
		Serial:    serial,
		Width:     width,
		Height:    height,
		Message:   message,
	}
}

func NewThinkingEvent(sessionID, content string) ThinkingEvent {
	return ThinkingEvent{
		Event:   NewBaseEvent(EventTypeThinking, sessionID),
		Content: content,
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
	if overlay.Model != "" {
		merged.Model = overlay.Model
	}
	if overlay.ReasoningEffort != "" {
		merged.ReasoningEffort = overlay.ReasoningEffort
	}
	if overlay.CodexSandboxMode != "" {
		merged.CodexSandboxMode = overlay.CodexSandboxMode
	}
	if overlay.CWD != "" {
		merged.CWD = overlay.CWD
	}
	if overlay.PermissionMode != "" {
		merged.PermissionMode = overlay.PermissionMode
	}
	if overlay.PermissionRequestID != "" {
		merged.PermissionRequestID = overlay.PermissionRequestID
	}
	if overlay.ClaudeLifecycle != "" {
		merged.ClaudeLifecycle = overlay.ClaudeLifecycle
	}
	if overlay.ClaudeSessionUUID != "" {
		merged.ClaudeSessionUUID = overlay.ClaudeSessionUUID
	}
	if overlay.BlockingKind != "" {
		merged.BlockingKind = overlay.BlockingKind
	}
	if overlay.SourcePath != "" {
		merged.SourcePath = overlay.SourcePath
	}
	if overlay.SourceSize != 0 {
		merged.SourceSize = overlay.SourceSize
	}
	if overlay.SourceModUnixNS != 0 {
		merged.SourceModUnixNS = overlay.SourceModUnixNS
	}
	if overlay.SourceEntryCount != 0 {
		merged.SourceEntryCount = overlay.SourceEntryCount
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
	case CompactionEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case ContextWindowUsageEvent:
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
	case AIStatusEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case RuntimePhaseEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case TaskSnapshotEvent:
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
	case SessionHistoryPageEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case SessionDeltaEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case SessionTerminalRangeEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case SessionDiffPageEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case SessionTerminalExecutionPageEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case SessionUpdatedEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case SessionResumeResultEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case SessionResumeNoticeEvent:
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
	case PermissionRuleListResultEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case PermissionAutoAppliedEvent:
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
	case RuntimeProcessListResultEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case RuntimeProcessLogResultEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	case ThinkingEvent:
		e.RuntimeMeta = MergeRuntimeMeta(e.RuntimeMeta, meta)
		return e
	default:
		return event
	}
}

func ApplyEventCursor(event any, cursor int64) any {
	if cursor <= 0 {
		return event
	}
	switch e := event.(type) {
	case Event:
		e.EventCursor = cursor
		return e
	case LogEvent:
		e.EventCursor = cursor
		return e
	case ProgressEvent:
		e.EventCursor = cursor
		return e
	case ErrorEvent:
		e.EventCursor = cursor
		return e
	case CompactionEvent:
		e.EventCursor = cursor
		return e
	case ContextWindowUsageEvent:
		e.EventCursor = cursor
		return e
	case InteractionRequestEvent:
		e.EventCursor = cursor
		return e
	case PromptRequestEvent:
		e.EventCursor = cursor
		return e
	case SessionStateEvent:
		e.EventCursor = cursor
		return e
	case AgentStateEvent:
		e.EventCursor = cursor
		return e
	case AIStatusEvent:
		e.EventCursor = cursor
		return e
	case RuntimePhaseEvent:
		e.EventCursor = cursor
		return e
	case TaskSnapshotEvent:
		e.EventCursor = cursor
		return e
	case StepUpdateEvent:
		e.EventCursor = cursor
		return e
	case FileDiffEvent:
		e.EventCursor = cursor
		return e
	case FSListResultEvent:
		e.EventCursor = cursor
		return e
	case FSReadResultEvent:
		e.EventCursor = cursor
		return e
	case SessionCreatedEvent:
		e.EventCursor = cursor
		return e
	case SessionListResultEvent:
		e.EventCursor = cursor
		return e
	case SessionHistoryEvent:
		e.EventCursor = cursor
		return e
	case SessionHistoryPageEvent:
		e.EventCursor = cursor
		return e
	case SessionDeltaEvent:
		e.EventCursor = cursor
		return e
	case SessionTerminalRangeEvent:
		e.EventCursor = cursor
		return e
	case SessionDiffPageEvent:
		e.EventCursor = cursor
		return e
	case SessionTerminalExecutionPageEvent:
		e.EventCursor = cursor
		return e
	case SessionUpdatedEvent:
		e.EventCursor = cursor
		return e
	case SessionResumeResultEvent:
		e.EventCursor = cursor
		return e
	case SessionResumeNoticeEvent:
		e.EventCursor = cursor
		return e
	case ReviewStateEvent:
		e.EventCursor = cursor
		return e
	case SkillCatalogResultEvent:
		e.EventCursor = cursor
		return e
	case MemoryListResultEvent:
		e.EventCursor = cursor
		return e
	case CatalogAuthoringResultEvent:
		e.EventCursor = cursor
		return e
	case SessionContextResultEvent:
		e.EventCursor = cursor
		return e
	case PermissionRuleListResultEvent:
		e.EventCursor = cursor
		return e
	case PermissionAutoAppliedEvent:
		e.EventCursor = cursor
		return e
	case SkillSyncResultEvent:
		e.EventCursor = cursor
		return e
	case CatalogSyncStatusEvent:
		e.EventCursor = cursor
		return e
	case CatalogSyncResultEvent:
		e.EventCursor = cursor
		return e
	case RuntimeInfoResultEvent:
		e.EventCursor = cursor
		return e
	case RuntimeProcessListResultEvent:
		e.EventCursor = cursor
		return e
	case RuntimeProcessLogResultEvent:
		e.EventCursor = cursor
		return e
	case ThinkingEvent:
		e.EventCursor = cursor
		return e
	default:
		return event
	}
}

func NormalizeContextWindowUsage(usage ContextWindowUsage) ContextWindowUsage {
	if usage.TokenLimit < 0 {
		usage.TokenLimit = 0
	}
	if usage.TokensUsed < 0 {
		usage.TokensUsed = 0
	}
	if usage.TokenLimit > 0 && usage.TokensUsed > usage.TokenLimit {
		usage.TokensUsed = usage.TokenLimit
	}
	return usage
}
