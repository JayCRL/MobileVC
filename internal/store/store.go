package store

import (
	"context"
	"mobilevc/internal/session"
	"time"
)

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
}

type SnapshotLogEntry struct {
	Kind      string           `json:"kind"`
	Message   string           `json:"message,omitempty"`
	Label     string           `json:"label,omitempty"`
	Timestamp string           `json:"timestamp,omitempty"`
	Stream    string           `json:"stream,omitempty"`
	Text      string           `json:"text,omitempty"`
	Context   *SnapshotContext `json:"context,omitempty"`
}

type ProjectionSnapshot struct {
	Diffs               []session.DiffContext        `json:"diffs,omitempty"`
	CurrentDiff         *session.DiffContext         `json:"currentDiff,omitempty"`
	CurrentStep         *SnapshotContext             `json:"currentStep,omitempty"`
	LatestError         *SnapshotContext             `json:"latestError,omitempty"`
	LogEntries          []SnapshotLogEntry           `json:"logEntries,omitempty"`
	RawTerminalByStream map[string]string            `json:"rawTerminalByStream,omitempty"`
	Controller          session.ControllerSnapshot   `json:"controller,omitempty"`
	Runtime             SessionRuntime               `json:"runtime,omitempty"`
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
}
