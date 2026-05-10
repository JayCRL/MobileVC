package boss

import (
	"context"
	"fmt"
	"strings"

	"mobilevc/internal/engine"
	"mobilevc/internal/logx"
	"mobilevc/internal/planning"
	"mobilevc/internal/protocol"
	"mobilevc/internal/session"
)

// Phase represents the boss lifecycle phase.
type Phase string

const (
	PhaseIdle       Phase = "idle"
	PhaseInstalling Phase = "installing"
	PhasePlanning   Phase = "planning"
	PhaseConfirming Phase = "confirming"
	PhaseExecuting  Phase = "executing"
	PhaseReviewing  Phase = "reviewing"
	PhaseCompleted  Phase = "completed"
	PhaseFailed     Phase = "failed"
)

// Orchestrator manages the full boss mode lifecycle.
type Orchestrator struct {
	Phase     Phase
	Plan      *Plan
	Tasks     []*Task
	Scheduler *Scheduler
	Tracker   *planning.Tracker

	planSvc *session.Service
	planSID string
	emit    func(any)
}

// NewOrchestrator creates a new boss orchestrator.
func NewOrchestrator(emit func(any)) *Orchestrator {
	return &Orchestrator{
		Phase:   PhaseIdle,
		Tracker: planning.NewTracker(),
		emit:    emit,
	}
}

// StartPlanning begins a boss planning session.
func (o *Orchestrator) StartPlanning(ctx context.Context, svc *session.Service, sessionID, task, cwd string) error {
	o.Phase = PhasePlanning
	o.planSvc = svc
	o.planSID = sessionID

	cmdStr := buildPlanningCommand()
	if task != "" {
		cmdStr += " -p '" + escapeShell(task) + "'"
	}

	logx.Info("boss", "starting planning: sessionID=%s task=%q", sessionID, task)

	wrappedEmit := func(event any) {
		if rp, ok := event.(protocol.RuntimePhaseEvent); ok {
			if planning.IsPlanningPhase(rp.Phase) {
				p := &planning.PhasePayload{
					Phase:   rp.Phase,
					Kind:    rp.Kind,
					Message: rp.Message,
				}
				o.onPhasePayload(sessionID, p)
			}
		}
		o.emit(event)
	}

	execReq := session.ExecuteRequest{
		Command:        cmdStr,
		CWD:            cwd,
		Mode:           engine.ModePTY,
		PermissionMode: "acceptEdits",
		InitialInput:   "\n",
		RuntimeMeta: protocol.RuntimeMeta{
			Source:         "boss",
			Command:        cmdStr,
			Engine:         "claude",
			CWD:            cwd,
			PermissionMode: "acceptEdits",
		},
	}
	if err := svc.Execute(ctx, sessionID, execReq, wrappedEmit); err != nil {
		o.Phase = PhaseFailed
		return fmt.Errorf("start planning: %w", err)
	}
	return nil
}

// ConfirmPlan dispatches worker sessions for each task in the confirmed plan.
func (o *Orchestrator) ConfirmPlan(ctx context.Context, notes string) error {
	if o.Phase != PhaseConfirming {
		return fmt.Errorf("not in confirming phase, current: %s", o.Phase)
	}
	o.Phase = PhaseExecuting

	state := o.Tracker.State()
	o.Tasks = make([]*Task, 0, len(state.Tasks))
	for _, t := range state.Tasks {
		o.Tasks = append(o.Tasks, &Task{
			ID:     t.ID,
			Title:  t.Title,
			Agent:  t.Agent,
			Status: TaskPending,
		})
	}
	o.Plan = &Plan{Tasks: o.Tasks}

	logx.Info("boss", "plan confirmed: sessionID=%s tasks=%d", o.planSID, len(o.Tasks))

	// Build confirmation input
	confirmMsg := "CONFIRMED. Execute the plan step by step. Report progress using mobilevcRuntimePhase markers."
	if notes != "" {
		confirmMsg += "\n\nDeveloper notes: " + notes
	}

	// Send confirmation to the planning Claude session
	inputReq := session.InputRequest{
		Data: confirmMsg,
		RuntimeMeta: protocol.RuntimeMeta{
			Source:         "boss",
			PermissionMode: "acceptEdits",
		},
	}
	execReq := session.ExecuteRequest{
		Command:        "claude",
		Mode:           engine.ModePTY,
		PermissionMode: "acceptEdits",
		RuntimeMeta: protocol.RuntimeMeta{
			Source:         "boss",
			Engine:         "claude",
			PermissionMode: "acceptEdits",
		},
	}
	return o.planSvc.SendInputOrResume(ctx, o.planSID, execReq, inputReq, o.emit)
}

// AdjustPlan sends adjustment feedback to the planning session.
func (o *Orchestrator) AdjustPlan(ctx context.Context, notes string) error {
	inputReq := session.InputRequest{
		Data: "ADJUST: " + notes,
		RuntimeMeta: protocol.RuntimeMeta{
			Source:         "boss",
			PermissionMode: "acceptEdits",
		},
	}
	execReq := session.ExecuteRequest{
		Command:        "claude",
		Mode:           engine.ModePTY,
		PermissionMode: "acceptEdits",
		RuntimeMeta: protocol.RuntimeMeta{
			Source:         "boss",
			Engine:         "claude",
			PermissionMode: "acceptEdits",
		},
	}
	return o.planSvc.SendInputOrResume(ctx, o.planSID, execReq, inputReq, o.emit)
}

// CancelPlan cancels the current planning session.
func (o *Orchestrator) CancelPlan(ctx context.Context) {
	o.Phase = PhaseIdle
	o.planSvc.StopActive(o.planSID, o.emit)
	o.emit(protocol.NewPlanningStateEvent(o.planSID, "completed", "", "", "Planning cancelled", nil))
}

// onPhasePayload processes a planning phase marker from Claude output.
func (o *Orchestrator) onPhasePayload(sessionID string, p *planning.PhasePayload) {
	logx.Info("boss", "phase: sessionID=%s phase=%s kind=%s msg=%q", sessionID, p.Phase, p.Kind, p.Message)

	switch p.Phase {
	case "awaiting_confirmation":
		o.Phase = PhaseConfirming
	case "completed":
		o.Phase = PhaseCompleted
	case "executing":
		o.Phase = PhaseExecuting
	}

	if state := o.Tracker.Apply(p); state != nil {
		tasks := make([]protocol.PlanTask, len(state.Tasks))
		for i, t := range state.Tasks {
			tasks[i] = protocol.PlanTask{
				ID:     t.ID,
				Title:  t.Title,
				Status: string(t.Status),
				Agent:  t.Agent,
			}
		}
		o.emit(protocol.NewPlanningStateEvent(
			sessionID, string(state.Phase),
			state.CurrentTask, state.CurrentAgent,
			state.Message, tasks,
		))
	}
}

func buildPlanningCommand() string {
	mgr := &planning.Manager{}
	args := mgr.BuildCommand("")
	var finalArgs []string
	skipNext := false
	for _, a := range args {
		switch a {
		case "--print", "-p", "--input-format":
			skipNext = true
			continue
		}
		if skipNext {
			skipNext = false
			continue
		}
		finalArgs = append(finalArgs, a)
	}

	quotedArgs := make([]string, len(finalArgs))
	for i, a := range finalArgs {
		if strings.ContainsAny(a, " \t\n|&;<>$`\"'(){}[]*?!~#") {
			quotedArgs[i] = "'" + strings.ReplaceAll(a, "'", "'\\''") + "'"
		} else {
			quotedArgs[i] = a
		}
	}
	return strings.Join(quotedArgs, " ")
}

func escapeShell(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}
