package protocol

import "time"

const (
	EventTypeLog           = "log"
	EventTypeProgress      = "progress"
	EventTypeError         = "error"
	EventTypePromptRequest = "prompt_request"
	EventTypeSessionState  = "session_state"
	EventTypeAgentState    = "agent_state"
	EventTypeFSListResult  = "fs_list_result"
	EventTypeStepUpdate    = "step_update"
	EventTypeFileDiff      = "file_diff"
)

type Event struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	SessionID string    `json:"sessionId,omitempty"`
}

type ClientEvent struct {
	Action string `json:"action"`
}

type ExecRequestEvent struct {
	ClientEvent
	Command string `json:"cmd"`
	CWD     string `json:"cwd,omitempty"`
	Mode    string `json:"mode,omitempty"`
}

type InputRequestEvent struct {
	ClientEvent
	Data string `json:"data"`
}

type FSListRequestEvent struct {
	ClientEvent
	Path string `json:"path,omitempty"`
}

type LogEvent struct {
	Event
	Message string `json:"msg"`
	Stream  string `json:"stream,omitempty"`
}

type ProgressEvent struct {
	Event
	Message string `json:"msg,omitempty"`
	Percent int    `json:"percent"`
}

type ErrorEvent struct {
	Event
	Message string `json:"msg"`
	Stack   string `json:"stack,omitempty"`
	Code    string `json:"code,omitempty"`
}

type PromptRequestEvent struct {
	Event
	Message string   `json:"msg,omitempty"`
	Options []string `json:"options,omitempty"`
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

type StepUpdateEvent struct {
	Event
	Message string `json:"msg,omitempty"`
	Status  string `json:"status,omitempty"`
	Target  string `json:"target,omitempty"`
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

func NewStepUpdateEvent(sessionID, message, status, target string) StepUpdateEvent {
	return StepUpdateEvent{
		Event:   NewBaseEvent(EventTypeStepUpdate, sessionID),
		Message: message,
		Status:  status,
		Target:  target,
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
