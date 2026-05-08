package planning

import (
	"encoding/json"
	"strings"
)

// Phase is the current planning phase.
type Phase string

const (
	PhasePlanning            Phase = "planning"
	PhaseAwaitingConfirmation Phase = "awaiting_confirmation"
	PhaseCheckpoint          Phase = "checkpoint"
	PhaseExecuting           Phase = "executing"
	PhaseReviewing           Phase = "reviewing"
	PhaseCompleted           Phase = "completed"
)

// TaskStatus is the status of a single task.
type TaskStatus string

const (
	TaskPending TaskStatus = "pending"
	TaskRunning TaskStatus = "running"
	TaskDone    TaskStatus = "done"
	TaskFailed  TaskStatus = "failed"
)

// TaskInfo tracks a single task in the plan.
type TaskInfo struct {
	ID     string     `json:"id"`
	Title  string     `json:"title"`
	Status TaskStatus `json:"status"`
	Agent  string     `json:"agent,omitempty"`
}

// SessionState is the full planning session state, sent to the client.
type SessionState struct {
	Phase       Phase      `json:"phase"`
	CurrentTask string     `json:"currentTask,omitempty"`
	CurrentAgent string    `json:"currentAgent,omitempty"`
	Message     string     `json:"msg"`
	Tasks       []TaskInfo `json:"tasks"`
}

// PhasePayload is the JSON that Claude emits via mobilevcRuntimePhase markers.
type PhasePayload struct {
	MobileVCRuntimePhase bool   `json:"mobilevcRuntimePhase"`
	Phase                string `json:"phase"`
	Kind                 string `json:"kind"`
	Message              string `json:"message,omitempty"`
	Msg                  string `json:"msg,omitempty"`
	TaskID               string `json:"taskId,omitempty"`
	TaskTitle            string `json:"taskTitle,omitempty"`
	Agent                string `json:"agent,omitempty"`
}

// ParsePhase tries to extract a planning phase payload from a text line.
// Returns nil if the line is not a valid planning phase marker.
func ParsePhase(text string) *PhasePayload {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	var p PhasePayload
	if err := json.Unmarshal([]byte(trimmed), &p); err != nil {
		return nil
	}
	if !p.MobileVCRuntimePhase {
		return nil
	}
	phase := strings.TrimSpace(p.Phase)
	if phase == "" {
		return nil
	}
	p.Phase = phase
	return &p
}

// ValidPlanningPhases are the phases we consider valid for a planning session.
var ValidPlanningPhases = map[string]bool{
	"planning":               true,
	"awaiting_confirmation":  true,
	"checkpoint":             true,
	"executing":              true,
	"reviewing":              true,
	"completed":              true,
}

// IsPlanningPhase returns true if the phase name is a planning phase.
func IsPlanningPhase(phase string) bool {
	return ValidPlanningPhases[strings.ToLower(strings.TrimSpace(phase))]
}

// Tracker maintains the planning session state and produces updates.
type Tracker struct {
	state SessionState
}

// NewTracker creates a new progress tracker.
func NewTracker() *Tracker {
	return &Tracker{
		state: SessionState{
			Phase: PhasePlanning,
			Tasks: make([]TaskInfo, 0),
		},
	}
}

// Apply processes a phase payload and returns the updated state if it changed.
func (t *Tracker) Apply(p *PhasePayload) *SessionState {
	if p == nil {
		return nil
	}

	phase := Phase(strings.ToLower(strings.TrimSpace(p.Phase)))
	if !IsPlanningPhase(string(phase)) {
		return nil
	}

	msg := p.Message
	if msg == "" {
		msg = p.Msg
	}

	changed := false

	if t.state.Phase != phase {
		t.state.Phase = phase
		changed = true
	}

	switch p.Kind {
	case "task_decomposition", "task_create":
		if p.TaskID != "" {
			t.state.Tasks = append(t.state.Tasks, TaskInfo{
				ID:     p.TaskID,
				Title:  p.TaskTitle,
				Status: TaskPending,
				Agent:  p.Agent,
			})
			changed = true
		}
	case "agent_start":
		t.state.CurrentAgent = p.Agent
		if p.TaskID != "" {
			t.state.CurrentTask = p.TaskID
			t.state.markTask(p.TaskID, TaskRunning)
		}
		t.state.Message = msg
		changed = true
	case "agent_done":
		if p.TaskID != "" {
			t.state.markTask(p.TaskID, TaskDone)
		}
		t.state.Message = msg
		changed = true
	case "task_failed":
		if p.TaskID != "" {
			t.state.markTask(p.TaskID, TaskFailed)
		}
		t.state.Message = msg
		changed = true
	case "summary":
		t.state.Message = msg
		changed = true
	default:
		if msg != "" && t.state.Message != msg {
			t.state.Message = msg
			changed = true
		}
	}

	if !changed {
		return nil
	}
	state := t.state
	return &state
}

// State returns the current tracking state.
func (t *Tracker) State() SessionState {
	return t.state
}

func (s *SessionState) markTask(id string, status TaskStatus) {
	for i := range s.Tasks {
		if s.Tasks[i].ID == id {
			s.Tasks[i].Status = status
			return
		}
	}
}
