package runtime

import (
	"context"
	"errors"
	"sync"

	"mobilevc/internal/protocol"
	"mobilevc/internal/runner"
	"mobilevc/internal/session"
)

type manager struct {
	mu            sync.Mutex
	activeRunner  runner.Runner
	activeMeta    protocol.RuntimeMeta
	activeSession string
}

func newManager() *manager {
	return &manager{}
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
	return nil
}

func (m *manager) current() (runner.Runner, protocol.RuntimeMeta, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeRunner, m.activeMeta, m.activeSession
}

func (m *manager) finish(run runner.Runner) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeRunner == run {
		m.activeRunner = nil
		m.activeMeta = protocol.RuntimeMeta{}
		m.activeSession = ""
	}
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
}

func (m *manager) closeActive() {
	m.mu.Lock()
	current := m.activeRunner
	m.activeRunner = nil
	m.activeMeta = protocol.RuntimeMeta{}
	m.activeSession = ""
	m.mu.Unlock()
	if current != nil {
		_ = current.Close()
	}
}

func (m *manager) snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Snapshot{
		Running:       m.activeRunner != nil,
		ActiveMeta:    m.activeMeta,
		ActiveSession: m.activeSession,
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
	if err := s.manager.start(sessionID, selected, req.RuntimeMeta); err != nil {
		return err
	}
	for _, event := range s.controller.OnExecStart(req.Command, req.RuntimeMeta) {
		emit(event)
	}
	go func() {
		err := selected.Run(ctx, runner.ExecRequest{
			SessionID:      sessionID,
			Command:        req.Command,
			CWD:            req.CWD,
			Mode:           req.Mode,
			PermissionMode: req.PermissionMode,
		}, func(event any) {
			mappedEvent := protocol.ApplyRuntimeMeta(event, req.RuntimeMeta)
			emit(mappedEvent)
			for _, mapped := range s.controller.OnRunnerEvent(mappedEvent) {
				emit(mapped)
			}
		})
		if err != nil {
			emit(protocol.ApplyRuntimeMeta(protocol.NewErrorEvent(sessionID, err.Error(), ""), req.RuntimeMeta))
		}
		s.manager.finish(selected)
		for _, event := range s.controller.OnCommandFinished(req.RuntimeMeta) {
			emit(event)
		}
	}()
	return nil
}

func (s *Service) SendInput(ctx context.Context, sessionID string, req InputRequest, emit func(any)) error {
	currentRunner, meta, currentSessionID := s.manager.current()
	if currentRunner == nil || currentSessionID == "" {
		return errors.New("no active runner")
	}
	if err := currentRunner.Write(ctx, []byte(req.Data)); err != nil {
		if errors.Is(err, runner.ErrInputNotSupported) {
			return runner.ErrInputNotSupported
		}
		return err
	}
	effectiveMeta := meta
	if req.RuntimeMeta.Source != "" || req.RuntimeMeta.SkillName != "" || req.RuntimeMeta.ResumeSessionID != "" || req.RuntimeMeta.ContextID != "" || req.RuntimeMeta.ContextTitle != "" || req.RuntimeMeta.TargetText != "" || req.RuntimeMeta.TargetPath != "" {
		effectiveMeta = protocol.MergeRuntimeMeta(effectiveMeta, req.RuntimeMeta)
	}
	for _, event := range s.controller.OnInputSent(effectiveMeta) {
		emit(event)
	}
	return nil
}

func (s *Service) IsRunning() bool {
	return s.manager.isRunning()
}

func (s *Service) RuntimeSnapshot() Snapshot {
	return s.manager.snapshot()
}

func (s *Service) UpdatePermissionMode(mode string) {
	s.manager.updateMeta(func(m *protocol.RuntimeMeta) {
		m.TargetText = m.TargetText
	})
	r, _, _ := s.manager.current()
	if r == nil {
		return
	}
	if pr, ok := r.(interface{ SetPermissionMode(string) }); ok {
		pr.SetPermissionMode(mode)
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
