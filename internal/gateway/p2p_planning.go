package gateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mobilevc/internal/engine"
	"mobilevc/internal/logx"
	"mobilevc/internal/planning"
	"mobilevc/internal/protocol"
	"mobilevc/internal/session"
)

// buildPlanningCommand constructs the shell command for starting a planning
// session.  Strips --print, -p, and --input-format which are handled by the
// PTY runner's buildClaudeStreamJSONCommand.  The caller is responsible for
// ensuring the ANTHROPIC_API_KEY is available in the environment (e.g. via
// ~/.claude/settings.json or the shell profile).
func (h *Handler) buildPlanningCommand() string {
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

	// Only shell-quote args that contain spaces or special characters;
	// simple tokens like "claude" and "--verbose" must remain unquoted
	// so that PTY engine command-name detection works correctly.
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

// planningExecParams holds the resolved values needed to execute a planning session.
type planningExecParams struct {
	SessionID string
	APIKey    string
	Task      string
	BaseURL   string
	CWD       string
}

// tryResolvePlanningParams loads or validates the API key, checks Claude is
// installed, and resolves the session ID and working directory.  If anything is
// missing or invalid an error event is sent to emitFn and false is returned.
func (h *Handler) tryResolvePlanningParams(ctx context.Context, envelope P2PMessageEnvelope, emitFn func(any)) (planningExecParams, bool) {
	mgr := &planning.Manager{}
	if result := mgr.CheckClaude(ctx); !result.Installed {
		emitFn(protocol.NewPlanningCheckEvent(envelope.SessionID, false, "", result.Error, result.InstallHint))
		emitFn(protocol.NewErrorEvent(envelope.SessionID, "Claude Code CLI 未安装", ""))
		return planningExecParams{}, false
	}
	sessionID := envelope.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("session-%d", time.Now().UTC().UnixNano())
	}
	cwd := h.workspaceRootOrSlash()
	return planningExecParams{
		SessionID: sessionID,
		Task:      envelope.Task,
		BaseURL:   envelope.BaseURL,
		CWD:       cwd,
	}, true
}

// executePlanningSession builds the command and executes it through the
// session service, wiring up the planning-phase tracker.
func (h *Handler) executePlanningSession(
	ctx context.Context,
	svc *session.Service,
	params planningExecParams,
	emitFn func(any),
) {
	cmdStr := h.buildPlanningCommand()
	if params.Task != "" {
		cmdStr += " -p '" + strings.ReplaceAll(params.Task, "'", "'\\''") + "'"
	}

	logx.Info("planning", "session starting: sessionID=%s cwd=%s task=%q", params.SessionID, params.CWD, params.Task)
	emitFn(protocol.NewPlanningStateEvent(params.SessionID, "planning", "", "", "Starting planning session...", nil))

	tracker := planning.NewTracker()
	wrappedEmit := func(event any) {
		if rp, ok := event.(protocol.RuntimePhaseEvent); ok {
			if planning.IsPlanningPhase(rp.Phase) {
				logx.Info("planning", "phase detected: sessionID=%s phase=%s kind=%s msg=%q", params.SessionID, rp.Phase, rp.Kind, rp.Message)
				p := &planning.PhasePayload{Phase: rp.Phase, Kind: rp.Kind, Message: rp.Message}
				if state := tracker.Apply(p); state != nil {
					logx.Info("planning", "phase applied: sessionID=%s phase=%s currentTask=%s currentAgent=%s tasks=%d", params.SessionID, state.Phase, state.CurrentTask, state.CurrentAgent, len(state.Tasks))
					tasks := make([]protocol.PlanTask, len(state.Tasks))
					for i, t := range state.Tasks {
						tasks[i] = protocol.PlanTask{ID: t.ID, Title: t.Title, Status: string(t.Status), Agent: t.Agent}
					}
					emitFn(protocol.NewPlanningStateEvent(params.SessionID, string(state.Phase), state.CurrentTask, state.CurrentAgent, state.Message, tasks))
					return
				}
			}
		}
		emitFn(event)
	}

	execReq := session.ExecuteRequest{
		Command:        cmdStr,
		CWD:            params.CWD,
		Mode:           engine.ModePTY,
		PermissionMode: "acceptEdits",
		InitialInput:   "\n",
		RuntimeMeta: protocol.RuntimeMeta{
			Source:         "planning",
			Command:        cmdStr,
			Engine:         "claude",
			CWD:            params.CWD,
			PermissionMode: "acceptEdits",
		},
	}
	if err := svc.Execute(ctx, params.SessionID, execReq, wrappedEmit); err != nil {
		logx.Error("p2p", "planning_start execute failed: sessionID=%s err=%v", params.SessionID, err)
		emitFn(protocol.NewErrorEvent(params.SessionID, err.Error(), ""))
	}
}
