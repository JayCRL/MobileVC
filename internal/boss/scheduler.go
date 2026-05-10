package boss

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"mobilevc/internal/engine"
	"mobilevc/internal/logx"
	"mobilevc/internal/protocol"
	"mobilevc/internal/session"
)

// SessionFactory creates a session.Service for a worker task.
type SessionFactory func(sessionID string) *session.Service

// Scheduler dispatches plan tasks to worker sessions.
type Scheduler struct {
	mu       sync.Mutex
	plan     *Plan
	taskIdx  int
	active   map[string]*session.Service // taskID → worker session
	done     map[string]bool
	failed   map[string]bool
	emit     func(any)
	factory  SessionFactory
}

// NewScheduler creates a scheduler.
func NewScheduler(factory SessionFactory, emit func(any)) *Scheduler {
	return &Scheduler{
		active:  make(map[string]*session.Service),
		done:    make(map[string]bool),
		failed:  make(map[string]bool),
		emit:    emit,
		factory: factory,
	}
}

// LoadPlan sets the plan to execute.
func (s *Scheduler) LoadPlan(plan *Plan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plan = plan
	s.taskIdx = 0
}

// NextBatch returns the next group of tasks that can run in parallel.
// Returns nil when all tasks are complete or some have failed.
func (s *Scheduler) NextBatch() []*Task {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.plan == nil {
		return nil
	}

	// Collect tasks that are ready (dependencies satisfied)
	var batch []*Task
	currentGroup := ""

	for _, t := range s.plan.Tasks {
		if s.done[t.ID] || s.failed[t.ID] {
			continue
		}
		// Check dependencies
		depsMet := true
		for _, depID := range t.Depends {
			if !s.done[depID] {
				depsMet = false
				break
			}
		}
		if !depsMet {
			continue
		}

		if len(batch) == 0 {
			// Start first batch
			currentGroup = t.Group
			batch = append(batch, t)
		} else if t.Group == currentGroup && currentGroup != "" {
			// Same parallel group
			batch = append(batch, t)
		} else if currentGroup == "" {
			// Single task batch
			batch = append(batch, t)
			return batch
		} else {
			// Next group — stop here
			return batch
		}
	}

	return batch
}

// Dispatch starts worker sessions for the given tasks.
func (s *Scheduler) Dispatch(ctx context.Context, tasks []*Task, cwd string) {
	for _, t := range tasks {
		taskID := t.ID
		taskSID := fmt.Sprintf("boss-%s-%d", taskID, time.Now().UnixNano())

		workerSvc := s.factory(taskSID)
		s.mu.Lock()
		s.active[taskID] = workerSvc
		s.mu.Unlock()

		t.Status = TaskRunning

		command := resolveWorkerCommand(t)
		taskPrompt := fmt.Sprintf("Execute task: %s\n\nReport completion using mobilevcRuntimePhase markers with phase='executing', kind='agent_done'. If the task fails, report with kind='task_failed'.", t.Title)

		wrappedEmit := func(event any) {
			if rp, ok := event.(protocol.RuntimePhaseEvent); ok {
				switch rp.Kind {
				case "agent_done":
					s.mu.Lock()
					s.done[taskID] = true
					delete(s.active, taskID)
					s.mu.Unlock()
					t.Status = TaskDone
					logx.Info("boss", "task done: taskID=%s title=%s", taskID, t.Title)
				case "task_failed":
					s.mu.Lock()
					s.failed[taskID] = true
					delete(s.active, taskID)
					s.mu.Unlock()
					t.Status = TaskFailed
					logx.Warn("boss", "task failed: taskID=%s title=%s", taskID, t.Title)
				}
			}
			s.emit(event)
		}

		execReq := session.ExecuteRequest{
			Command:        command,
			CWD:            cwd,
			Mode:           engine.ModePTY,
			PermissionMode: "acceptEdits",
			InitialInput:   taskPrompt + "\n",
			RuntimeMeta: protocol.RuntimeMeta{
				Source:         "boss-worker",
				Command:        command,
				Engine:         string(t.Worker),
				CWD:            cwd,
				PermissionMode: "acceptEdits",
			},
		}

		go func() {
			logx.Info("boss", "dispatching worker: taskID=%s sessionID=%s command=%s", taskID, taskSID, command)
			if err := workerSvc.Execute(ctx, taskSID, execReq, wrappedEmit); err != nil {
				logx.Error("boss", "worker start failed: taskID=%s err=%v", taskID, err)
				s.mu.Lock()
				s.failed[taskID] = true
				s.mu.Unlock()
				t.Status = TaskFailed
			}
		}()
	}
}

// AllDone returns true when all tasks are complete.
func (s *Scheduler) AllDone() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.plan.Tasks {
		if !s.done[t.ID] && !s.failed[t.ID] {
			return false
		}
	}
	return true
}

// HasFailures returns true if any task failed.
func (s *Scheduler) HasFailures() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.failed) > 0
}

// Progress returns a summary of task completion.
func (s *Scheduler) Progress() (done, failed, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.plan == nil {
		return 0, 0, 0
	}
	return len(s.done), len(s.failed), len(s.plan.Tasks)
}

func resolveWorkerCommand(t *Task) string {
	agent := strings.ToLower(strings.TrimSpace(t.Agent))
	switch {
	case strings.Contains(agent, "codex"):
		t.Worker = WorkerCodex
		return "codex"
	default:
		t.Worker = WorkerClaude
		return "claude"
	}
}
