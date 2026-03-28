package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"mobilevc/internal/logx"
	"mobilevc/internal/protocol"
	"mobilevc/internal/runner"
	"mobilevc/internal/session"
)

var ErrNoActiveRunner = errors.New("no active runner")
var ErrRunnerNotInteractive = errors.New("runner is not ready for interactive input")
var ErrHotSwapUnsupportedRunner = errors.New("active runner cannot be hot-swapped")
var ErrResumeSessionUnavailable = errors.New("resume session id is unavailable")
var ErrResumeConversationNotFound = errors.New("resume conversation not found")

const claudeSessionIDFlag = "--session-id"

func normalizeClaudeLifecycle(value string) string {
	switch strings.TrimSpace(value) {
	case "inactive", "starting", "active", "waiting_input", "resumable", "unknown":
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func deriveClaudeLifecycleLocked(activeRunner runner.Runner, meta protocol.RuntimeMeta, activeSession string, resumeSessionID string) string {
	if lifecycle := normalizeClaudeLifecycle(meta.ClaudeLifecycle); lifecycle != "" {
		return lifecycle
	}
	command := strings.TrimSpace(meta.Command)
	isClaude := runnerIsClaudeSession(activeRunner, command, command)
	trimmedResume := strings.TrimSpace(firstNonEmptyRuntimeValue(meta.ResumeSessionID, resumeSessionID))
	if activeRunner == nil || strings.TrimSpace(activeSession) == "" {
		if trimmedResume != "" {
			return "resumable"
		}
		if isClaude {
			return "unknown"
		}
		return "inactive"
	}
	if !isClaude {
		if trimmedResume != "" {
			return "resumable"
		}
		return "inactive"
	}
	if provider, ok := activeRunner.(runner.InteractiveStateProvider); ok && provider.CanAcceptInteractiveInput() {
		return "waiting_input"
	}
	if trimmedResume != "" {
		return "active"
	}
	return "starting"
}

type manager struct {
	mu                 sync.Mutex
	activeRunner       runner.Runner
	activeMeta         protocol.RuntimeMeta
	activeSession      string
	resumeSessionID    string
	temporaryElevated  bool
	safePermissionMode string
	claudeLifecycle    string
}

func newManager() *manager {
	return &manager{claudeLifecycle: "inactive"}
}

func (m *manager) start(sessionID string, run runner.Runner, meta protocol.RuntimeMeta) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeRunner != nil {
		return errors.New("another command is already running")
	}
	m.activeRunner = run
	m.activeMeta = meta
	m.activeSession = sessionID
	if resumeSessionID := strings.TrimSpace(meta.ResumeSessionID); resumeSessionID != "" {
		m.resumeSessionID = resumeSessionID
	}
	if mode := strings.TrimSpace(meta.PermissionMode); mode != "" && strings.TrimSpace(m.safePermissionMode) == "" {
		m.safePermissionMode = mode
	}
	m.claudeLifecycle = deriveClaudeLifecycleLocked(run, meta, sessionID, m.resumeSessionID)
	m.activeMeta.ClaudeLifecycle = m.claudeLifecycle
	return nil
}

func (m *manager) current() (runner.Runner, protocol.RuntimeMeta, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeRunner, m.activeMeta, m.activeSession
}

func (m *manager) currentRunner() runner.Runner {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeRunner
}

func (m *manager) finishIfCurrent(run runner.Runner) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeRunner != run {
		return false
	}
	m.activeRunner = nil
	m.activeMeta = protocol.RuntimeMeta{}
	m.activeSession = ""
	if strings.TrimSpace(m.resumeSessionID) != "" {
		m.claudeLifecycle = "resumable"
	} else {
		m.claudeLifecycle = "inactive"
	}
	return true
}

func (m *manager) isRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeRunner != nil
}

func (m *manager) updateMeta(fn func(*protocol.RuntimeMeta)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(&m.activeMeta)
	if resumeSessionID := strings.TrimSpace(m.activeMeta.ResumeSessionID); resumeSessionID != "" {
		m.resumeSessionID = resumeSessionID
	}
	m.claudeLifecycle = deriveClaudeLifecycleLocked(m.activeRunner, m.activeMeta, m.activeSession, m.resumeSessionID)
	m.activeMeta.ClaudeLifecycle = m.claudeLifecycle
}

func (m *manager) updateResumeSessionID(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sessionID = strings.TrimSpace(sessionID); sessionID != "" {
		m.resumeSessionID = sessionID
		m.activeMeta.ResumeSessionID = sessionID
		m.claudeLifecycle = deriveClaudeLifecycleLocked(m.activeRunner, m.activeMeta, m.activeSession, m.resumeSessionID)
		m.activeMeta.ClaudeLifecycle = m.claudeLifecycle
	}
}

func (m *manager) setTemporaryElevation(enabled bool, safePermissionMode string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.temporaryElevated = enabled
	if enabled {
		m.safePermissionMode = strings.TrimSpace(safePermissionMode)
	}
	if !enabled && strings.TrimSpace(safePermissionMode) != "" {
		m.safePermissionMode = strings.TrimSpace(safePermissionMode)
	}
}

func (m *manager) closeActive() {
	m.mu.Lock()
	current := m.activeRunner
	m.activeRunner = nil
	m.activeMeta = protocol.RuntimeMeta{}
	m.activeSession = ""
	if strings.TrimSpace(m.resumeSessionID) != "" {
		m.claudeLifecycle = "resumable"
	} else {
		m.claudeLifecycle = "inactive"
	}
	m.mu.Unlock()
	if current != nil {
		_ = current.Close()
	}
}

func (m *manager) snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	canAcceptInteractiveInput := false
	if provider, ok := m.activeRunner.(runner.InteractiveStateProvider); ok && m.activeRunner != nil {
		canAcceptInteractiveInput = provider.CanAcceptInteractiveInput()
	}
	meta := m.activeMeta
	if strings.TrimSpace(meta.ResumeSessionID) == "" {
		meta.ResumeSessionID = m.resumeSessionID
	}
	lifecycle := deriveClaudeLifecycleLocked(m.activeRunner, meta, m.activeSession, m.resumeSessionID)
	meta.ClaudeLifecycle = lifecycle
	m.claudeLifecycle = lifecycle
	return Snapshot{
		Running:                   m.activeRunner != nil,
		CanAcceptInteractiveInput: canAcceptInteractiveInput,
		ActiveMeta:                meta,
		ActiveSession:             m.activeSession,
		ResumeSessionID:           m.resumeSessionID,
		TemporaryElevated:         m.temporaryElevated,
		SafePermissionMode:        m.safePermissionMode,
		ClaudeLifecycle:           lifecycle,
	}
}

type Service struct {
	controller *session.Controller
	manager    *manager
	deps       Dependencies
}

func NewService(sessionID string, deps Dependencies) *Service {
	if deps.NewExecRunner == nil {
		deps.NewExecRunner = func() runner.Runner { return runner.NewExecRunner() }
	}
	if deps.NewPtyRunner == nil {
		deps.NewPtyRunner = func() runner.Runner { return runner.NewPtyRunner() }
	}
	return &Service{
		controller: session.NewController(sessionID),
		manager:    newManager(),
		deps:       deps,
	}
}

func (s *Service) InitialEvent() protocol.AgentStateEvent {
	return s.controller.InitialEvent()
}

func (s *Service) Cleanup() {
	s.manager.closeActive()
}

func (s *Service) Execute(ctx context.Context, sessionID string, req ExecuteRequest, emit func(any)) error {
	selected := s.newRunner(req.Mode)
	preparedReq := s.prepareExecuteRequest(req)
	if err := s.manager.start(sessionID, selected, preparedReq.RuntimeMeta); err != nil {
		return err
	}
	for _, event := range s.controller.OnExecStart(preparedReq.Command, preparedReq.RuntimeMeta) {
		emit(event)
	}
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				stack := logx.StackTrace()
				message := fmt.Sprintf("runner panic recovered: %v", recovered)
				logx.Error("runtime", "%s\nsessionID=%s\n%s", message, sessionID, stack)
				emit(protocol.ApplyRuntimeMeta(protocol.NewErrorEvent(sessionID, "internal server error", stack), preparedReq.RuntimeMeta))
				if s.manager.finishIfCurrent(selected) {
					for _, event := range s.controller.OnCommandFinished(preparedReq.RuntimeMeta) {
						emit(event)
					}
				}
			}
		}()
		err := selected.Run(ctx, runner.ExecRequest{
			SessionID:      sessionID,
			Command:        preparedReq.Command,
			CWD:            preparedReq.CWD,
			Mode:           preparedReq.Mode,
			PermissionMode: preparedReq.PermissionMode,
			RuntimeMeta:    preparedReq.RuntimeMeta,
		}, func(event any) {
			if provider, ok := selected.(runner.ClaudeSessionProvider); ok {
				if resumeSessionID := strings.TrimSpace(provider.ClaudeSessionID()); resumeSessionID != "" {
					s.manager.updateResumeSessionID(resumeSessionID)
				}
			}
			if meta := extractRuntimeMetaFromEvent(event); strings.TrimSpace(meta.ResumeSessionID) != "" {
				if resumeSessionID := resolveResumeSessionID(selected, meta, preparedReq.RuntimeMeta); resumeSessionID != "" {
					s.manager.updateResumeSessionID(resumeSessionID)
				}
			}
			mappedEvent := protocol.ApplyRuntimeMeta(event, preparedReq.RuntimeMeta)
			emit(mappedEvent)
			for _, mapped := range s.controller.OnRunnerEvent(mappedEvent) {
				emit(mapped)
			}
		})
		if err != nil {
			emit(protocol.ApplyRuntimeMeta(protocol.NewErrorEvent(sessionID, err.Error(), ""), preparedReq.RuntimeMeta))
		}
		if s.manager.finishIfCurrent(selected) {
			for _, event := range s.controller.OnCommandFinished(preparedReq.RuntimeMeta) {
				emit(event)
			}
		}
	}()
	return nil
}

func (s *Service) SendInput(ctx context.Context, sessionID string, req InputRequest, emit func(any)) error {
	currentRunner, meta, currentSessionID := s.manager.current()
	if currentRunner == nil || currentSessionID == "" {
		return ErrNoActiveRunner
	}
	effectiveMeta := meta
	if req.RuntimeMeta.Source != "" || req.RuntimeMeta.SkillName != "" || req.RuntimeMeta.ResumeSessionID != "" || req.RuntimeMeta.ExecutionID != "" || req.RuntimeMeta.GroupID != "" || req.RuntimeMeta.GroupTitle != "" || req.RuntimeMeta.ContextID != "" || req.RuntimeMeta.ContextTitle != "" || req.RuntimeMeta.TargetText != "" || req.RuntimeMeta.TargetPath != "" || req.RuntimeMeta.PermissionMode != "" {
		effectiveMeta = protocol.MergeRuntimeMeta(effectiveMeta, req.RuntimeMeta)
		s.manager.updateMeta(func(m *protocol.RuntimeMeta) {
			*m = effectiveMeta
		})
		if req.RuntimeMeta.PermissionMode != "" {
			if pr, ok := currentRunner.(interface{ SetPermissionMode(string) }); ok {
				pr.SetPermissionMode(req.RuntimeMeta.PermissionMode)
			}
		}
	}
	if err := currentRunner.Write(ctx, []byte(req.Data)); err != nil {
		if errors.Is(err, runner.ErrInputNotSupported) {
			return runner.ErrInputNotSupported
		}
		if errors.Is(err, ErrNoActiveRunner) || strings.Contains(err.Error(), "no active pty session") {
			return ErrNoActiveRunner
		}
		if strings.Contains(strings.ToLower(err.Error()), "no conversation found with session id") {
			return ErrResumeConversationNotFound
		}
		if errors.Is(err, ErrRunnerNotInteractive) || strings.Contains(err.Error(), "runner is not ready for interactive input") {
			return ErrRunnerNotInteractive
		}
		return err
	}
	for _, event := range s.controller.OnInputSent(effectiveMeta) {
		emit(event)
	}
	return nil
}

func (s *Service) SendInputOrResume(ctx context.Context, sessionID string, execReq ExecuteRequest, inputReq InputRequest, emit func(any)) error {
	if err := s.SendInput(ctx, sessionID, inputReq, emit); err == nil {
		return nil
	} else if !errors.Is(err, ErrNoActiveRunner) {
		return err
	}

	if execReq.Mode != runner.ModePTY {
		return ErrNoActiveRunner
	}
	if !s.CanHotSwapClaudeSession(execReq) {
		return ErrNoActiveRunner
	}
	if !s.HasResumeSession(execReq) {
		return ErrNoActiveRunner
	}

	restartReq, _, err := s.buildDetachedHotSwapStreamRequest(execReq, execReq.PermissionMode)
	if err != nil {
		return err
	}
	if err := s.Execute(ctx, sessionID, restartReq, emit); err != nil {
		return err
	}
	if err := s.waitForRunnerStart(ctx); err != nil {
		return err
	}
	if err := s.sendInputWhenRunnerReady(ctx, sessionID, InputRequest{Data: inputReq.Data, RuntimeMeta: protocol.MergeRuntimeMeta(inputReq.RuntimeMeta, protocol.RuntimeMeta{
		ResumeSessionID: restartReq.RuntimeMeta.ResumeSessionID,
		PermissionMode:  restartReq.PermissionMode,
	})}, emit); err != nil {
		return err
	}
	return nil
}

func (s *Service) SendPermissionDecision(ctx context.Context, sessionID string, decision string, meta protocol.RuntimeMeta, emit func(any)) error {
	currentRunner, activeMeta, currentSessionID := s.manager.current()
	if currentRunner == nil || currentSessionID == "" {
		return ErrNoActiveRunner
	}
	responder, ok := currentRunner.(runner.PermissionResponseWriter)
	if !ok {
		return runner.ErrInputNotSupported
	}
	if !responder.HasPendingPermissionRequest() {
		return runner.ErrNoPendingControlRequest
	}
	effectiveMeta := activeMeta
	if meta.Source != "" || meta.SkillName != "" || meta.ResumeSessionID != "" || meta.ExecutionID != "" || meta.GroupID != "" || meta.GroupTitle != "" || meta.ContextID != "" || meta.ContextTitle != "" || meta.TargetText != "" || meta.TargetPath != "" || meta.PermissionMode != "" {
		effectiveMeta = protocol.MergeRuntimeMeta(effectiveMeta, meta)
		s.manager.updateMeta(func(m *protocol.RuntimeMeta) {
			*m = effectiveMeta
		})
		if meta.PermissionMode != "" {
			if pr, ok := currentRunner.(interface{ SetPermissionMode(string) }); ok {
				pr.SetPermissionMode(meta.PermissionMode)
			}
		}
	}
	if err := responder.WritePermissionResponse(ctx, decision); err != nil {
		if errors.Is(err, runner.ErrNoPendingControlRequest) {
			return runner.ErrNoPendingControlRequest
		}
		if errors.Is(err, ErrNoActiveRunner) || strings.Contains(err.Error(), "no active pty session") {
			return ErrNoActiveRunner
		}
		if strings.Contains(strings.ToLower(err.Error()), "no conversation found with session id") {
			return ErrResumeConversationNotFound
		}
		return err
	}
	for _, event := range s.controller.OnInputSent(effectiveMeta) {
		emit(event)
	}
	return nil
}

func (s *Service) HotSwapApproveWithTemporaryElevation(ctx context.Context, sessionID string, req ExecuteRequest, continuation string, emit func(any)) error {
	restartReq, safePermissionMode, err := s.buildHotSwapStreamRequest(sessionID, req, "acceptEdits")
	if err != nil {
		return err
	}
	s.manager.closeActive()
	if err := s.Execute(ctx, sessionID, restartReq, emit); err != nil {
		return err
	}
	s.manager.setTemporaryElevation(true, safePermissionMode)
	if err := s.waitForRunnerStart(ctx); err != nil {
		return err
	}
	if err := s.sendInputWhenRunnerReady(ctx, sessionID, InputRequest{Data: continuation, RuntimeMeta: protocol.RuntimeMeta{
		Source:          req.RuntimeMeta.Source,
		ResumeSessionID: restartReq.RuntimeMeta.ResumeSessionID,
		PermissionMode:  restartReq.PermissionMode,
	}}, emit); err != nil {
		return err
	}
	return nil
}

func (s *Service) HotSwapApproveFromResume(ctx context.Context, sessionID string, req ExecuteRequest, continuation string, emit func(any)) error {
	restartReq, safePermissionMode, err := s.buildDetachedHotSwapStreamRequest(req, "acceptEdits")
	if err != nil {
		return err
	}
	if err := s.Execute(ctx, sessionID, restartReq, emit); err != nil {
		return err
	}
	s.manager.setTemporaryElevation(true, safePermissionMode)
	if err := s.waitForRunnerStart(ctx); err != nil {
		return err
	}
	if err := s.sendInputWhenRunnerReady(ctx, sessionID, InputRequest{Data: continuation, RuntimeMeta: protocol.RuntimeMeta{
		Source:          req.RuntimeMeta.Source,
		ResumeSessionID: restartReq.RuntimeMeta.ResumeSessionID,
		PermissionMode:  restartReq.PermissionMode,
	}}, emit); err != nil {
		return err
	}
	return nil
}

func (s *Service) RestoreSafePermissionModeBeforeInput(ctx context.Context, sessionID string, req ExecuteRequest, userInput string, emit func(any)) error {
	snapshot := s.RuntimeSnapshot()
	if !snapshot.TemporaryElevated {
		return nil
	}
	safeMode := strings.TrimSpace(snapshot.SafePermissionMode)
	if safeMode == "" {
		safeMode = "default"
	}
	if snapshot.ActiveSession == "" && strings.TrimSpace(snapshot.ResumeSessionID) != "" {
		restartReq, _, err := s.buildDetachedHotSwapStreamRequest(req, safeMode)
		if err != nil {
			return err
		}
		s.manager.closeActive()
		if err := s.Execute(ctx, sessionID, restartReq, emit); err != nil {
			return err
		}
		s.manager.setTemporaryElevation(false, safeMode)
		if err := s.waitForRunnerStart(ctx); err != nil {
			return err
		}
		if err := s.sendInputWhenRunnerReady(ctx, sessionID, InputRequest{Data: userInput, RuntimeMeta: protocol.RuntimeMeta{
			ResumeSessionID: restartReq.RuntimeMeta.ResumeSessionID,
			PermissionMode:  restartReq.PermissionMode,
		}}, emit); err != nil {
			return err
		}
		return nil
	}
	restartReq, _, err := s.buildHotSwapStreamRequest(sessionID, req, safeMode)
	if err != nil {
		return err
	}
	s.manager.closeActive()
	if err := s.Execute(ctx, sessionID, restartReq, emit); err != nil {
		return err
	}
	s.manager.setTemporaryElevation(false, safeMode)
	if err := s.waitForRunnerStart(ctx); err != nil {
		return err
	}
	if err := s.sendInputWhenRunnerReady(ctx, sessionID, InputRequest{Data: userInput, RuntimeMeta: protocol.RuntimeMeta{
		ResumeSessionID: restartReq.RuntimeMeta.ResumeSessionID,
		PermissionMode:  restartReq.PermissionMode,
	}}, emit); err != nil {
		return err
	}
	return nil
}

func (s *Service) ReviewDecision(ctx context.Context, sessionID string, req ReviewDecisionRequest, emit func(any)) error {
	decision := strings.TrimSpace(strings.ToLower(req.Decision))
	if decision == "" {
		return errors.New("review decision is required")
	}
	payload := reviewDecisionPayload(decision)
	if payload == "" {
		return errors.New("review decision must be one of: accept, revert, revise")
	}
	meta := req.RuntimeMeta
	meta.Source = "review-decision"
	meta.TargetText = decision
	return s.SendInput(ctx, sessionID, InputRequest{Data: payload, RuntimeMeta: meta}, emit)
}

func (s *Service) PlanDecision(ctx context.Context, sessionID string, req PlanDecisionRequest, emit func(any)) error {
	decision := strings.TrimSpace(req.Decision)
	if decision == "" {
		return errors.New("plan decision is required")
	}
	if strings.TrimSpace(req.Command) == "" {
		req.Command = "claude"
	}
	meta := req.RuntimeMeta
	meta.Source = "plan-decision"
	meta.TargetText = decision
	if req.ResumeSessionID != "" {
		meta.ResumeSessionID = firstNonEmptyRuntimeValue(req.ResumeSessionID, meta.ResumeSessionID)
	}
	return s.SendInput(ctx, sessionID, InputRequest{Data: decision + "\n", RuntimeMeta: meta}, emit)
}

func (s *Service) CanAcceptInteractiveInput() bool {
	snapshot := s.manager.snapshot()
	return snapshot.CanAcceptInteractiveInput
}

func (s *Service) IsRunning() bool {
	return s.manager.isRunning()
}


func (s *Service) RuntimeSnapshot() Snapshot {
	return s.manager.snapshot()
}

func (s *Service) CurrentRunner() runner.Runner {
	return s.manager.currentRunner()
}

func (s *Service) CanHotSwapClaudeSession(req ExecuteRequest) bool {
	currentRunner, activeMeta, currentSessionID := s.manager.current()
	if req.Mode != runner.ModePTY {
		return false
	}
	if currentRunner != nil && currentSessionID != "" {
		return runnerIsClaudeSession(currentRunner, req.Command, activeMeta.Command)
	}
	return runnerIsClaudeSession(nil, req.Command, activeMeta.Command, s.manager.snapshot().ActiveMeta.Command)
}

func (s *Service) HasResumeSession(req ExecuteRequest) bool {
	currentRunner, activeMeta, _ := s.manager.current()
	resumeSessionID := resolveResumeSessionID(currentRunner, req.RuntimeMeta, activeMeta, s.manager.snapshot().ActiveMeta, protocol.RuntimeMeta{ResumeSessionID: s.manager.snapshot().ResumeSessionID})
	return strings.TrimSpace(resumeSessionID) != ""
}

func (s *Service) ControllerSnapshot() session.ControllerSnapshot {
	return s.controller.Snapshot()
}

func (s *Service) RecordUserInput(input string) {
	s.controller.RecordUserInput(input)
}

func (s *Service) UpdatePermissionMode(mode string) {
	trimmed := strings.TrimSpace(mode)
	s.manager.updateMeta(func(m *protocol.RuntimeMeta) {
		m.PermissionMode = trimmed
	})
	if trimmed != "" {
		s.manager.setTemporaryElevation(false, trimmed)
	}
	s.controller.UpdatePermissionMode(trimmed)
	r, _, _ := s.manager.current()
	if r == nil {
		return
	}
	if pr, ok := r.(interface{ SetPermissionMode(string) }); ok {
		pr.SetPermissionMode(trimmed)
	}
}

func (s *Service) newRunner(mode runner.Mode) runner.Runner {
	switch mode {
	case runner.ModePTY:
		return s.deps.NewPtyRunner()
	default:
		return s.deps.NewExecRunner()
	}
}

func (s *Service) prepareExecuteRequest(req ExecuteRequest) ExecuteRequest {
	prepared := req
	prepared.Command = strings.TrimSpace(prepared.Command)
	if prepared.Command == "" {
		prepared.Command = "claude"
	}
	prepared.RuntimeMeta = protocol.MergeRuntimeMeta(prepared.RuntimeMeta, protocol.RuntimeMeta{
		Command:         prepared.Command,
		CWD:             prepared.CWD,
		PermissionMode:  prepared.PermissionMode,
		ClaudeLifecycle: firstNonEmptyRuntimeValue(prepared.RuntimeMeta.ClaudeLifecycle, func() string {
			if prepared.Mode == runner.ModePTY && runnerIsClaudeSession(nil, prepared.Command, prepared.RuntimeMeta.Command) {
				return "starting"
			}
			return "inactive"
		}()),
	})
	if prepared.Mode != runner.ModePTY || !runnerIsClaudeSession(nil, prepared.Command, prepared.RuntimeMeta.Command) {
		return prepared
	}
	if existingResumeID := strings.TrimSpace(extractResumeArg(prepared.Command)); existingResumeID != "" {
		prepared.RuntimeMeta.ResumeSessionID = firstNonEmptyRuntimeValue(prepared.RuntimeMeta.ResumeSessionID, existingResumeID)
		return prepared
	}
	if existingRuntimeResumeID := strings.TrimSpace(prepared.RuntimeMeta.ResumeSessionID); existingRuntimeResumeID != "" {
		return prepared
	}
	if existingSessionID := strings.TrimSpace(extractManagedClaudeSessionID(prepared.Command, prepared.RuntimeMeta.ResumeSessionID)); existingSessionID != "" {
		prepared.RuntimeMeta.ResumeSessionID = existingSessionID
		return prepared
	}
	managedSessionID := newManagedClaudeSessionID()
	prepared.Command = strings.TrimSpace(prepared.Command) + " " + claudeSessionIDFlag + " " + managedSessionID
	prepared.RuntimeMeta.Command = prepared.Command
	prepared.RuntimeMeta.ResumeSessionID = managedSessionID
	return prepared
}

func (s *Service) buildHotSwapRequest(sessionID string, req ExecuteRequest, targetPermissionMode string) (ExecuteRequest, string, error) {
	currentRunner, activeMeta, currentSessionID := s.manager.current()
	if currentRunner == nil || currentSessionID == "" {
		return ExecuteRequest{}, "", ErrNoActiveRunner
	}
	if req.Mode != runner.ModePTY {
		return ExecuteRequest{}, "", ErrHotSwapUnsupportedRunner
	}
	if !runnerIsClaudeSession(currentRunner, req.Command, activeMeta.Command) {
		return ExecuteRequest{}, "", ErrHotSwapUnsupportedRunner
	}
	resumeSessionID := resolveResumeSessionID(currentRunner, req.RuntimeMeta, activeMeta, s.manager.snapshot().ActiveMeta, protocol.RuntimeMeta{ResumeSessionID: s.manager.snapshot().ResumeSessionID})
	if resumeSessionID == "" {
		return ExecuteRequest{}, "", ErrResumeSessionUnavailable
	}
	safePermissionMode := strings.TrimSpace(req.RuntimeMeta.PermissionMode)
	if safePermissionMode == "" {
		safePermissionMode = strings.TrimSpace(activeMeta.PermissionMode)
	}
	if safePermissionMode == "" {
		safePermissionMode = strings.TrimSpace(s.manager.snapshot().SafePermissionMode)
	}
	if safePermissionMode == "" {
		safePermissionMode = "default"
	}
	mergedMeta := protocol.MergeRuntimeMeta(activeMeta, req.RuntimeMeta)
	managedResumeID := extractManagedClaudeSessionID(mergedMeta.Command, resumeSessionID)
	mergedMeta.ResumeSessionID = managedResumeID
	mergedMeta.PermissionMode = targetPermissionMode
	command := strings.TrimSpace(mergedMeta.Command)
	if command == "" {
		command = strings.TrimSpace(req.Command)
	}
	if command == "" {
		command = strings.TrimSpace(activeMeta.Command)
	}
	command = ensureResumeCommand(command, resumeSessionID)
	cwd := strings.TrimSpace(mergedMeta.CWD)
	if cwd == "" {
		cwd = strings.TrimSpace(req.CWD)
	}
	return ExecuteRequest{
		Command:        command,
		CWD:            cwd,
		Mode:           runner.ModePTY,
		PermissionMode: targetPermissionMode,
		RuntimeMeta:    mergedMeta,
	}, safePermissionMode, nil
}

func (s *Service) buildHotSwapStreamRequest(sessionID string, req ExecuteRequest, targetPermissionMode string) (ExecuteRequest, string, error) {
	restartReq, safePermissionMode, err := s.buildHotSwapRequest(sessionID, req, targetPermissionMode)
	if err != nil {
		return ExecuteRequest{}, "", err
	}
	command := strings.TrimSpace(restartReq.Command)
	if command == "" {
		command = "claude"
	}
	lower := strings.ToLower(command)
	if !strings.Contains(lower, " --print") && !strings.Contains(lower, " -p") {
		command += " --print"
	}
	if !strings.Contains(lower, " --verbose") {
		command += " --verbose"
	}
	if !strings.Contains(lower, "--output-format") {
		command += " --output-format stream-json"
	}
	if !strings.Contains(lower, "--input-format") {
		command += " --input-format stream-json"
	}
	if !strings.Contains(lower, "--permission-prompt-tool") {
		command += " --permission-prompt-tool stdio"
	}
	restartReq.Command = command
	restartReq.RuntimeMeta.Command = command
	return restartReq, safePermissionMode, nil
}

func (s *Service) buildHotSwapPromptRequest(sessionID string, req ExecuteRequest, targetPermissionMode string) (ExecuteRequest, string, error) {
	restartReq, safePermissionMode, err := s.buildHotSwapRequest(sessionID, req, targetPermissionMode)
	if err != nil {
		return ExecuteRequest{}, "", err
	}
	baseCommand := stripResumeArg(stripClaudeSessionIDArg(restartReq.Command))
	if strings.TrimSpace(baseCommand) == "" {
		baseCommand = "claude"
	}
	restartReq.Command = baseCommand
	restartReq.RuntimeMeta.Command = baseCommand
	return restartReq, safePermissionMode, nil
}

func (s *Service) buildDetachedHotSwapStreamRequest(req ExecuteRequest, targetPermissionMode string) (ExecuteRequest, string, error) {
	prepared := s.prepareExecuteRequest(req)
	if prepared.Mode != runner.ModePTY {
		return ExecuteRequest{}, "", ErrHotSwapUnsupportedRunner
	}
	if !runnerIsClaudeSession(nil, prepared.Command, prepared.RuntimeMeta.Command, s.manager.snapshot().ActiveMeta.Command) {
		return ExecuteRequest{}, "", ErrHotSwapUnsupportedRunner
	}
	resumeSessionID := resolveResumeSessionID(nil, prepared.RuntimeMeta, s.manager.snapshot().ActiveMeta, protocol.RuntimeMeta{ResumeSessionID: s.manager.snapshot().ResumeSessionID})
	if resumeSessionID == "" {
		return ExecuteRequest{}, "", ErrResumeSessionUnavailable
	}
	safePermissionMode := strings.TrimSpace(prepared.RuntimeMeta.PermissionMode)
	if safePermissionMode == "" {
		safePermissionMode = strings.TrimSpace(s.manager.snapshot().SafePermissionMode)
	}
	if safePermissionMode == "" {
		safePermissionMode = "default"
	}
	command := strings.TrimSpace(prepared.RuntimeMeta.Command)
	if command == "" {
		command = strings.TrimSpace(prepared.Command)
	}
	command = ensureResumeCommand(command, resumeSessionID)
	prepared.Command = command
	prepared.RuntimeMeta.Command = command
	prepared.RuntimeMeta.ResumeSessionID = extractManagedClaudeSessionID(command, resumeSessionID)
	prepared.PermissionMode = targetPermissionMode
	prepared.RuntimeMeta.PermissionMode = targetPermissionMode
	lower := strings.ToLower(command)
	if !strings.Contains(lower, " --print") && !strings.Contains(lower, " -p") {
		command += " --print"
	}
	if !strings.Contains(lower, " --verbose") {
		command += " --verbose"
	}
	if !strings.Contains(lower, "--output-format") {
		command += " --output-format stream-json"
	}
	if !strings.Contains(lower, "--input-format") {
		command += " --input-format stream-json"
	}
	if !strings.Contains(lower, "--permission-prompt-tool") {
		command += " --permission-prompt-tool stdio"
	}
	prepared.Command = command
	prepared.RuntimeMeta.Command = command
	return prepared, safePermissionMode, nil
}

func (s *Service) sendInputWhenRunnerReady(ctx context.Context, sessionID string, req InputRequest, emit func(any)) error {
	deadlineCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := s.SendInput(deadlineCtx, sessionID, req, emit)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrNoActiveRunner) && !errors.Is(err, ErrRunnerNotInteractive) {
			return err
		}
		select {
		case <-deadlineCtx.Done():
			if errors.Is(err, ErrRunnerNotInteractive) {
				return ErrRunnerNotInteractive
			}
			return ErrNoActiveRunner
		case <-ticker.C:
		}
	}
}

func (s *Service) waitForRunnerStart(ctx context.Context) error {
	deadlineCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if s.IsRunning() {
			return nil
		}
		select {
		case <-deadlineCtx.Done():
			if s.IsRunning() {
				return nil
			}
			return ErrNoActiveRunner
		case <-ticker.C:
		}
	}
}

func (s *Service) waitForInteractive(ctx context.Context) error {
	deadlineCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if s.CanAcceptInteractiveInput() {
			return nil
		}
		if !s.IsRunning() {
			return ErrNoActiveRunner
		}
		select {
		case <-deadlineCtx.Done():
			if s.CanAcceptInteractiveInput() {
				return nil
			}
			if !s.IsRunning() {
				return ErrNoActiveRunner
			}
			return ErrRunnerNotInteractive
		case <-ticker.C:
		}
	}
}

func runnerIsClaudeSession(current runner.Runner, commands ...string) bool {
	if _, ok := current.(runner.ClaudeSessionProvider); ok {
		return true
	}
	for _, command := range commands {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(command)), "claude") {
			return true
		}
	}
	return false
}

func resolveResumeSessionID(current runner.Runner, metas ...protocol.RuntimeMeta) string {
	for _, meta := range metas {
		if sessionID := extractManagedClaudeSessionID(meta.Command, meta.ResumeSessionID); sessionID != "" {
			return sessionID
		}
	}
	if provider, ok := current.(runner.ClaudeSessionProvider); ok {
		if sessionID := extractManagedClaudeSessionID("", provider.ClaudeSessionID()); sessionID != "" {
			return sessionID
		}
	}
	for _, meta := range metas {
		if sessionID := strings.TrimSpace(meta.ResumeSessionID); sessionID != "" {
			return sessionID
		}
	}
	return ""
}

func extractManagedClaudeSessionID(command, fallback string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	for i := 0; i < len(fields); i++ {
		if fields[i] == claudeSessionIDFlag && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return strings.TrimSpace(fallback)
}

func extractResumeArg(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	for i := 0; i < len(fields); i++ {
		if fields[i] == "--resume" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

func ensureResumeCommand(command, resumeSessionID string) string {
	trimmed := stripClaudeSessionIDArg(strings.TrimSpace(command))
	if trimmed == "" {
		trimmed = "claude"
	}
	if resumeSessionID != "" && !strings.Contains(strings.ToLower(trimmed), " --resume") {
		trimmed += " --resume " + resumeSessionID
	}
	return trimmed
}

func stripClaudeSessionIDArg(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return ""
	}
	filtered := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		if fields[i] == claudeSessionIDFlag {
			i++
			continue
		}
		filtered = append(filtered, fields[i])
	}
	return strings.Join(filtered, " ")
}

func stripResumeArg(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return ""
	}
	filtered := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		if fields[i] == "--resume" {
			i++
			continue
		}
		filtered = append(filtered, fields[i])
	}
	return strings.Join(filtered, " ")
}

func newManagedClaudeSessionID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("generate managed claude session id: %v", err))
	}
	encoded := hex.EncodeToString(buf)
	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32])
}

func firstNonEmptyRuntimeValue(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func hotSwapContinuationInput(targetPath, promptMessage string) string {
	targetPath = strings.TrimSpace(targetPath)
	promptMessage = strings.TrimSpace(promptMessage)
	lines := []string{
		"我已经批准这次文件修改权限。",
		"不要继续复用刚才那次失败的工具调用；请基于这次已批准的权限，立即重新发起一次新的 Write/Edit 操作。",
	}
	if targetPath != "" {
		lines = append(lines, "本次已授权的目标文件：`"+targetPath+"`。")
	}
	lines = append(lines, "不要再次请求权限，不要只做解释，直接完成这次文件修改。")
	if promptMessage != "" {
		lines = append(lines, "你上一次请求权限时的原始上下文如下，请按该上下文重新完成写入：", promptMessage)
	}
	return strings.Join(lines, "\n\n") + "\n"
}

func extractRuntimeMetaFromEvent(event any) protocol.RuntimeMeta {
	type runtimeMetaCarrier interface {
		GetRuntimeMeta() protocol.RuntimeMeta
	}
	if carrier, ok := event.(runtimeMetaCarrier); ok {
		return carrier.GetRuntimeMeta()
	}
	return protocol.RuntimeMeta{}
}

func reviewDecisionPayload(decision string) string {
	switch strings.TrimSpace(strings.ToLower(decision)) {
	case "accept":
		return "1\n"
	case "revert":
		return "2\n"
	case "revise":
		return "3\n"
	default:
		return ""
	}
}
