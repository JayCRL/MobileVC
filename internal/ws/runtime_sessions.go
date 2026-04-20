package ws

import (
	"strings"
	"sync"
	"time"

	"mobilevc/internal/protocol"
	runtimepkg "mobilevc/internal/runtime"
)

const defaultRuntimeSessionReleaseAfter = 15 * time.Minute
const defaultRuntimeSessionPendingLimit = 64
const defaultRuntimeSessionSinkBufferSize = 256

type runtimeSession struct {
	mu            sync.RWMutex
	service       *runtimepkg.Service
	listeners     map[string]func(any)
	releaseTimer  *time.Timer
	pendingCursor int64
	pendingEvents []any

	sinkCh  chan any
	sinkMu  sync.Mutex
	sinkRef int
}

func newRuntimeSession(service *runtimepkg.Service) *runtimeSession {
	return &runtimeSession{
		service:       service,
		listeners:     make(map[string]func(any)),
		pendingEvents: make([]any, 0, defaultRuntimeSessionPendingLimit),
	}
}

func (s *runtimeSession) setListener(id string, listener func(any)) {
	if strings.TrimSpace(id) == "" || listener == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners[id] = listener
}

func (s *runtimeSession) removeListener(id string) {
	if strings.TrimSpace(id) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.listeners, id)
}

func (s *runtimeSession) listenerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.listeners)
}

func (s *runtimeSession) emit(event any) {
	s.mu.RLock()
	listeners := make([]func(any), 0, len(s.listeners))
	for _, listener := range s.listeners {
		listeners = append(listeners, listener)
	}
	s.mu.RUnlock()
	for _, listener := range listeners {
		listener(event)
	}
}

func (s *runtimeSession) EnsureBufferedSink() func(event any) {
	s.sinkMu.Lock()
	defer s.sinkMu.Unlock()
	if s.sinkCh == nil {
		s.sinkCh = make(chan any, defaultRuntimeSessionSinkBufferSize)
		go func() {
			for event := range s.sinkCh {
				s.emit(event)
			}
		}()
	}
	return func(event any) {
		if event == nil {
			return
		}
		select {
		case s.sinkCh <- event:
		default:
		}
	}
}

func (s *runtimeSession) appendPending(event any) any {
	if event == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingCursor++
	event = protocol.ApplyEventCursor(event, s.pendingCursor)
	s.pendingEvents = append(s.pendingEvents, event)
	if len(s.pendingEvents) > defaultRuntimeSessionPendingLimit {
		s.pendingEvents = append([]any(nil), s.pendingEvents[len(s.pendingEvents)-defaultRuntimeSessionPendingLimit:]...)
	}
	return event
}

func (s *runtimeSession) latestCursor() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pendingCursor
}

func (s *runtimeSession) pendingSince(cursor int64) []any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.pendingEvents) == 0 {
		return nil
	}
	items := make([]any, 0, len(s.pendingEvents))
	for _, event := range s.pendingEvents {
		if eventCursorFromEvent(event) <= cursor {
			continue
		}
		items = append(items, event)
	}
	return items
}

type runtimeSessionRegistry struct {
	mu           sync.Mutex
	sessions     map[string]*runtimeSession
	newService   func(string) *runtimepkg.Service
	releaseAfter time.Duration
}

func newRuntimeSessionRegistry(
	newService func(string) *runtimepkg.Service,
	releaseAfter time.Duration,
) *runtimeSessionRegistry {
	if releaseAfter <= 0 {
		releaseAfter = defaultRuntimeSessionReleaseAfter
	}
	return &runtimeSessionRegistry{
		sessions:     make(map[string]*runtimeSession),
		newService:   newService,
		releaseAfter: releaseAfter,
	}
}

func (r *runtimeSessionRegistry) Ensure(sessionID string) *runtimeSession {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ensureLocked(sessionID)
}

func (r *runtimeSessionRegistry) Attach(sessionID, listenerID string, listener func(any)) *runtimeSession {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.ensureLocked(sessionID)
	entry.setListener(listenerID, listener)
	entry.EnsureBufferedSink()
	if entry.releaseTimer != nil {
		entry.releaseTimer.Stop()
		entry.releaseTimer = nil
	}
	return entry
}

func (r *runtimeSessionRegistry) Release(sessionID, listenerID string, cleanupIfOrphaned bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	r.mu.Lock()
	entry, ok := r.sessions[sessionID]
	if !ok {
		r.mu.Unlock()
		return
	}
	entry.removeListener(listenerID)
	if entry.listenerCount() > 0 {
		r.mu.Unlock()
		return
	}
	if cleanupIfOrphaned {
		delete(r.sessions, sessionID)
		if entry.releaseTimer != nil {
			entry.releaseTimer.Stop()
			entry.releaseTimer = nil
		}
		r.mu.Unlock()
		entry.service.Cleanup()
		return
	}
	if entry.releaseTimer != nil {
		entry.releaseTimer.Stop()
	}
	entry.releaseTimer = time.AfterFunc(r.releaseAfter, func() {
		r.cleanupIfOrphaned(sessionID, entry)
	})
	r.mu.Unlock()
}

func (r *runtimeSessionRegistry) cleanupIfOrphaned(sessionID string, target *runtimeSession) {
	r.mu.Lock()
	current, ok := r.sessions[sessionID]
	if !ok || current != target {
		r.mu.Unlock()
		return
	}
	if current.listenerCount() > 0 {
		current.releaseTimer = nil
		r.mu.Unlock()
		return
	}
	delete(r.sessions, sessionID)
	current.releaseTimer = nil
	r.mu.Unlock()
	current.service.Cleanup()
}

func (r *runtimeSessionRegistry) CleanupAll() {
	r.mu.Lock()
	services := make([]*runtimepkg.Service, 0, len(r.sessions))
	for sessionID, entry := range r.sessions {
		delete(r.sessions, sessionID)
		if entry.releaseTimer != nil {
			entry.releaseTimer.Stop()
			entry.releaseTimer = nil
		}
		if entry.service != nil {
			services = append(services, entry.service)
		}
	}
	r.mu.Unlock()
	for _, service := range services {
		service.Cleanup()
	}
}

func (r *runtimeSessionRegistry) ensureLocked(sessionID string) *runtimeSession {
	if entry, ok := r.sessions[sessionID]; ok {
		return entry
	}
	entry := newRuntimeSession(r.newService(sessionID))
	r.sessions[sessionID] = entry
	return entry
}

func eventCursorFromEvent(event any) int64 {
	switch e := event.(type) {
	case protocol.Event:
		return e.EventCursor
	case protocol.LogEvent:
		return e.EventCursor
	case protocol.ProgressEvent:
		return e.EventCursor
	case protocol.ErrorEvent:
		return e.EventCursor
	case protocol.InteractionRequestEvent:
		return e.EventCursor
	case protocol.PromptRequestEvent:
		return e.EventCursor
	case protocol.SessionStateEvent:
		return e.EventCursor
	case protocol.AgentStateEvent:
		return e.EventCursor
	case protocol.RuntimePhaseEvent:
		return e.EventCursor
	case protocol.StepUpdateEvent:
		return e.EventCursor
	case protocol.FileDiffEvent:
		return e.EventCursor
	case protocol.FSListResultEvent:
		return e.EventCursor
	case protocol.FSReadResultEvent:
		return e.EventCursor
	case protocol.SessionCreatedEvent:
		return e.EventCursor
	case protocol.SessionListResultEvent:
		return e.EventCursor
	case protocol.SessionHistoryEvent:
		return e.EventCursor
	case protocol.SessionResumeResultEvent:
		return e.EventCursor
	case protocol.SessionResumeNoticeEvent:
		return e.EventCursor
	case protocol.ReviewStateEvent:
		return e.EventCursor
	case protocol.SkillCatalogResultEvent:
		return e.EventCursor
	case protocol.MemoryListResultEvent:
		return e.EventCursor
	case protocol.CatalogAuthoringResultEvent:
		return e.EventCursor
	case protocol.SessionContextResultEvent:
		return e.EventCursor
	case protocol.PermissionRuleListResultEvent:
		return e.EventCursor
	case protocol.PermissionAutoAppliedEvent:
		return e.EventCursor
	case protocol.SkillSyncResultEvent:
		return e.EventCursor
	case protocol.CatalogSyncStatusEvent:
		return e.EventCursor
	case protocol.CatalogSyncResultEvent:
		return e.EventCursor
	case protocol.RuntimeInfoResultEvent:
		return e.EventCursor
	case protocol.RuntimeProcessListResultEvent:
		return e.EventCursor
	case protocol.RuntimeProcessLogResultEvent:
		return e.EventCursor
	default:
		return 0
	}
}
