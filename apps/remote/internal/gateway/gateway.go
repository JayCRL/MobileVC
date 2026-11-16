package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"mobilevc/internal/adb"
	"mobilevc/internal/data"
	"mobilevc/internal/data/claudesync"
	"mobilevc/internal/data/codexsync"
	"mobilevc/internal/data/skills"
	"mobilevc/internal/engine"
	"mobilevc/internal/logx"
	"mobilevc/internal/protocol"
	"mobilevc/internal/push"
	"mobilevc/internal/relay/e2ee"
	"mobilevc/internal/session"
)

const wsDebugPreviewLimit = 240
const sessionListCacheTTL = 1500 * time.Millisecond
const clientActionDedupeTTL = 24 * time.Hour
const clientActionDedupeLimit = 500
const sessionResumeHistoryLimit = 120
const gatewayWriteQueueSize = 128
const gatewayPriorityWriteQueueSize = 32
const gatewayReadQueueSize = 32
const projectionPersistFlushInterval = 350 * time.Millisecond
const projectionSessionListFreshnessDelay = 400 * time.Millisecond
const projectionPersistQueueSize = 64
const projectionPersistTimeout = 10 * time.Second
const projectionPersistRetryDelay = 750 * time.Millisecond
const gatewayRelaySafePayloadBudgetBytes = 6 * 1024 * 1024

func sessionHistoryWindowLimit(requested int) int {
	if requested <= 0 {
		return sessionResumeHistoryLimit
	}
	return requested
}

type sessionLoadTraceStage struct {
	name     string
	duration time.Duration
}

type sessionLoadTrace struct {
	connectionID       string
	initialSessionID   string
	requestedSessionID string
	remoteAddr         string
	reason             string
	startedAt          time.Time
	lastStepAt         time.Time
	stages             []sessionLoadTraceStage
}

type projectionTraceMetrics struct {
	logEntries         int
	diffs              int
	terminalExecutions int
	stdoutBytes        int
	stderrBytes        int
}

type loadedSessionHistoryWindow struct {
	window data.SessionHistoryWindow
	ok     bool
}

type sessionListCacheEntry struct {
	cwd        string
	generation uint64
	createdAt  time.Time
	items      []data.SessionSummary
}

type projectionPersistRequest struct {
	sessionID string
	snapshot  data.ProjectionSnapshot
	force     bool
}

type projectionPersistSessionMeta struct {
	claudeSessionUUID        string
	cwd                      string
	jsonlSyncEntryCount      int
	jsonlSyncEntryCountDirty bool
}

type sessionListFreshnessState struct {
	timer       *time.Timer
	dirty       bool
	fingerprint string
	generation  uint64
}

type freshnessSubscriber struct {
	id   string
	emit func(any) bool
}

type gatewayWriteQueues struct {
	writeCh                    chan any
	priorityWriteCh            chan any
	mu                         sync.Mutex
	pendingSessionUpdatedByKey map[string]*protocol.SessionUpdatedEvent
}

type projectionPersistWorker struct {
	ctx       context.Context
	cancel    context.CancelFunc
	store     data.Store
	onPersist func(sessionID string, summary data.SessionSummary)

	reqCh chan projectionPersistRequest
	done  chan struct{}

	mu      sync.Mutex
	pending map[string]data.ProjectionSnapshot
	meta    map[string]projectionPersistSessionMeta
	retry   *time.Timer
}

type handlerProjectionWriter struct {
	mu        sync.Mutex
	store     data.Store
	worker    *projectionPersistWorker
	onPersist func(sessionID string, summary data.SessionSummary)
}

func newSessionLoadTrace(connectionID, selectedSessionID, requestedSessionID, remoteAddr, reason string) *sessionLoadTrace {
	now := time.Now()
	return &sessionLoadTrace{
		connectionID:       strings.TrimSpace(connectionID),
		initialSessionID:   strings.TrimSpace(selectedSessionID),
		requestedSessionID: strings.TrimSpace(requestedSessionID),
		remoteAddr:         strings.TrimSpace(remoteAddr),
		reason:             strings.TrimSpace(reason),
		startedAt:          now,
		lastStepAt:         now,
	}
}

func (t *sessionLoadTrace) Step(name string) {
	if t == nil {
		return
	}
	now := time.Now()
	t.stages = append(t.stages, sessionLoadTraceStage{
		name:     strings.TrimSpace(name),
		duration: now.Sub(t.lastStepAt),
	})
	t.lastStepAt = now
}

func (t *sessionLoadTrace) Finish(finalSessionID string, activeRuntimeLoad bool, metrics projectionTraceMetrics) {
	if t == nil {
		return
	}
	total := time.Since(t.startedAt)
	stageDurations := make([]string, 0, len(t.stages))
	for _, stage := range t.stages {
		stageDurations = append(stageDurations, fmt.Sprintf("%s=%s", stage.name, stage.duration.Round(time.Millisecond)))
	}
	logx.Info(
		"ws",
		"session_load timings: connectionID=%s initialSessionID=%s requestedSessionID=%s finalSessionID=%s remoteAddr=%s reason=%q activeRuntimeLoad=%v total=%s stages=%s logEntries=%d diffs=%d terminalExecutions=%d stdoutBytes=%d stderrBytes=%d",
		t.connectionID,
		t.initialSessionID,
		t.requestedSessionID,
		strings.TrimSpace(finalSessionID),
		t.remoteAddr,
		t.reason,
		activeRuntimeLoad,
		total.Round(time.Millisecond),
		strings.Join(stageDurations, ","),
		metrics.logEntries,
		metrics.diffs,
		metrics.terminalExecutions,
		metrics.stdoutBytes,
		metrics.stderrBytes,
	)
}

func projectionMetrics(projection data.ProjectionSnapshot) projectionTraceMetrics {
	projection = session.NormalizeProjectionSnapshot(projection)
	return projectionTraceMetrics{
		logEntries:         len(projection.LogEntries),
		diffs:              len(projection.Diffs),
		terminalExecutions: len(projection.TerminalExecutions),
		stdoutBytes:        len(projection.RawTerminalByStream["stdout"]),
		stderrBytes:        len(projection.RawTerminalByStream["stderr"]),
	}
}

func newProjectionPersistWorker(ctx context.Context, store data.Store, onPersist func(string, data.SessionSummary)) *projectionPersistWorker {
	workerCtx, cancel := context.WithCancel(ctx)
	w := &projectionPersistWorker{
		ctx:       workerCtx,
		cancel:    cancel,
		store:     store,
		onPersist: onPersist,
		reqCh:     make(chan projectionPersistRequest, projectionPersistQueueSize),
		done:      make(chan struct{}),
		pending:   make(map[string]data.ProjectionSnapshot),
		meta:      make(map[string]projectionPersistSessionMeta),
	}
	go w.run()
	return w
}

func (w *projectionPersistWorker) updateMeta(record data.SessionRecord) {
	if w == nil {
		return
	}
	sessionID := strings.TrimSpace(record.Summary.ID)
	if sessionID == "" {
		return
	}
	cwd := strings.TrimSpace(record.Summary.Runtime.CWD)
	if cwd == "" {
		cwd = strings.TrimSpace(record.Projection.Runtime.CWD)
	}
	w.mu.Lock()
	current := w.meta[sessionID]
	jsonlSyncEntryCount := record.Summary.JSONLSyncEntryCount
	jsonlSyncEntryCountDirty := false
	if current.jsonlSyncEntryCountDirty && current.jsonlSyncEntryCount > jsonlSyncEntryCount {
		jsonlSyncEntryCount = current.jsonlSyncEntryCount
		jsonlSyncEntryCountDirty = true
	}
	w.meta[sessionID] = projectionPersistSessionMeta{
		claudeSessionUUID:        strings.TrimSpace(record.Summary.ClaudeSessionUUID),
		cwd:                      cwd,
		jsonlSyncEntryCount:      jsonlSyncEntryCount,
		jsonlSyncEntryCountDirty: jsonlSyncEntryCountDirty,
	}
	w.mu.Unlock()
}

func (w *projectionPersistWorker) enqueue(sessionID string, snapshot data.ProjectionSnapshot, force bool) {
	if w == nil || w.store == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	req := projectionPersistRequest{sessionID: strings.TrimSpace(sessionID), snapshot: snapshot, force: force}
	select {
	case w.reqCh <- req:
		return
	case <-w.ctx.Done():
		return
	default:
		w.mergePending(req)
		select {
		case w.reqCh <- projectionPersistRequest{force: force}:
		case <-w.ctx.Done():
		default:
		}
	}
}

func (w *projectionPersistWorker) close() {
	if w == nil {
		return
	}
	w.drainQueued()
	w.flushAll()
	w.cancel()
	w.stopRetryTimer()
	select {
	case <-w.done:
	case <-time.After(projectionPersistTimeout):
		logx.Warn("ws", "projection persist worker close timed out")
	}
}

func (w *projectionPersistWorker) run() {
	defer close(w.done)
	ticker := time.NewTicker(projectionPersistFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case req := <-w.reqCh:
			if strings.TrimSpace(req.sessionID) != "" {
				w.mergePending(req)
			}
			if req.force {
				w.flushAll()
			}
		case <-ticker.C:
			w.flushAll()
		case <-w.ctx.Done():
			w.drainQueued()
			w.flushAll()
			return
		}
	}
}

func (w *projectionPersistWorker) mergePending(req projectionPersistRequest) {
	sessionID := strings.TrimSpace(req.sessionID)
	if sessionID == "" {
		return
	}
	w.mu.Lock()
	w.pending[sessionID] = req.snapshot
	w.mu.Unlock()
}

func (w *projectionPersistWorker) scheduleRetry() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.retry != nil {
		return
	}
	w.retry = time.AfterFunc(projectionPersistRetryDelay, func() {
		w.mu.Lock()
		w.retry = nil
		w.mu.Unlock()
		delivered := false
		select {
		case w.reqCh <- projectionPersistRequest{force: true}:
			delivered = true
		case <-w.ctx.Done():
			return
		default:
			logx.Warn("ws", "projection persist retry signal skipped because queue is full")
		}
		if !delivered {
			w.scheduleRetry()
		}
	})
}

func (w *projectionPersistWorker) stopRetryTimer() {
	if w == nil {
		return
	}
	w.mu.Lock()
	retry := w.retry
	w.retry = nil
	w.mu.Unlock()
	if retry != nil {
		retry.Stop()
	}
}

func (w *projectionPersistWorker) drainQueued() {
	for {
		select {
		case req := <-w.reqCh:
			if strings.TrimSpace(req.sessionID) != "" {
				w.mergePending(req)
			}
		default:
			return
		}
	}
}

func (w *projectionPersistWorker) takePending() map[string]data.ProjectionSnapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) == 0 {
		return nil
	}
	items := w.pending
	w.pending = make(map[string]data.ProjectionSnapshot)
	return items
}

func (w *projectionPersistWorker) flushAll() {
	items := w.takePending()
	for sessionID, snapshot := range items {
		w.persist(sessionID, snapshot)
	}
}

func (w *projectionPersistWorker) persist(sessionID string, snapshot data.ProjectionSnapshot) {
	if w == nil || w.store == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	newCount, opts := w.syncClaudeJSONL(sessionID, snapshot)
	saveCtx, cancel := context.WithTimeout(context.Background(), projectionPersistTimeout)
	defer cancel()
	summary, err := saveProjectionWithOptions(w.store, saveCtx, sessionID, snapshot, opts...)
	if err != nil {
		logx.Error("ws", "save session projection failed: sessionID=%s err=%v", sessionID, err)
		w.mergePending(projectionPersistRequest{sessionID: sessionID, snapshot: snapshot})
		w.scheduleRetry()
		return
	}
	w.mu.Lock()
	meta := w.meta[sessionID]
	meta.claudeSessionUUID = strings.TrimSpace(summary.ClaudeSessionUUID)
	if strings.TrimSpace(summary.Runtime.CWD) != "" {
		meta.cwd = strings.TrimSpace(summary.Runtime.CWD)
	}
	if newCount > 0 {
		meta.jsonlSyncEntryCount = newCount
	} else {
		meta.jsonlSyncEntryCount = summary.JSONLSyncEntryCount
	}
	meta.jsonlSyncEntryCountDirty = false
	w.meta[sessionID] = meta
	w.mu.Unlock()
	if w.onPersist != nil {
		w.onPersist(sessionID, summary)
	}
}

func (w *projectionPersistWorker) syncClaudeJSONL(sessionID string, snapshot data.ProjectionSnapshot) (int, []data.ProjectionSaveOption) {
	w.mu.Lock()
	meta := w.meta[sessionID]
	w.mu.Unlock()
	csuuid := strings.TrimSpace(meta.claudeSessionUUID)
	cwd := strings.TrimSpace(firstNonEmptyString(meta.cwd, snapshot.Runtime.CWD))
	if csuuid == "" || cwd == "" {
		return 0, w.dirtyJSONLSyncOptions(sessionID)
	}
	events, newCount := claudesync.ExtractJSONLEvents(snapshot.LogEntries, meta.jsonlSyncEntryCount)
	if len(events) == 0 {
		return 0, w.dirtyJSONLSyncOptions(sessionID)
	}
	if err := claudesync.WriteSessionToJSONL(cwd, csuuid, events); err != nil {
		logx.Error("ws", "sync claude jsonl failed: sessionID=%s err=%v", sessionID, err)
		return 0, w.dirtyJSONLSyncOptions(sessionID)
	}
	w.markJSONLSyncEntryCountDirty(sessionID, newCount)
	return newCount, []data.ProjectionSaveOption{data.WithJSONLSyncEntryCount(newCount)}
}

func (w *projectionPersistWorker) markJSONLSyncEntryCountDirty(sessionID string, count int) {
	if w == nil || count <= 0 || strings.TrimSpace(sessionID) == "" {
		return
	}
	w.mu.Lock()
	meta := w.meta[sessionID]
	if count > meta.jsonlSyncEntryCount {
		meta.jsonlSyncEntryCount = count
	}
	meta.jsonlSyncEntryCountDirty = true
	w.meta[sessionID] = meta
	w.mu.Unlock()
}

func (w *projectionPersistWorker) dirtyJSONLSyncOptions(sessionID string) []data.ProjectionSaveOption {
	if w == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	w.mu.Lock()
	meta := w.meta[sessionID]
	w.mu.Unlock()
	if !meta.jsonlSyncEntryCountDirty || meta.jsonlSyncEntryCount <= 0 {
		return nil
	}
	return []data.ProjectionSaveOption{data.WithJSONLSyncEntryCount(meta.jsonlSyncEntryCount)}
}

func saveProjectionWithOptions(store data.Store, ctx context.Context, sessionID string, snapshot data.ProjectionSnapshot, opts ...data.ProjectionSaveOption) (data.SessionSummary, error) {
	if optionStore, ok := store.(data.ProjectionOptionStore); ok {
		return optionStore.SaveProjectionWithOptions(ctx, sessionID, snapshot, opts...)
	}
	if len(opts) > 0 {
		return data.SessionSummary{}, fmt.Errorf("save projection with options requires data.ProjectionOptionStore: sessionID=%s store=%T", strings.TrimSpace(sessionID), store)
	}
	return store.SaveProjection(ctx, sessionID, snapshot)
}

func newHandlerProjectionWriter(onPersist func(string, data.SessionSummary)) *handlerProjectionWriter {
	return &handlerProjectionWriter{onPersist: onPersist}
}

func (w *handlerProjectionWriter) enqueue(store data.Store, sessionID string, snapshot data.ProjectionSnapshot, force bool) {
	if w == nil || store == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	worker := w.ensure(store)
	if worker == nil {
		return
	}
	worker.enqueue(sessionID, snapshot, force)
}

func (w *handlerProjectionWriter) updateMeta(store data.Store, record data.SessionRecord) {
	if w == nil || store == nil {
		return
	}
	worker := w.ensure(store)
	if worker == nil {
		return
	}
	worker.updateMeta(record)
}

func (w *handlerProjectionWriter) ensure(store data.Store) *projectionPersistWorker {
	if w == nil || store == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.worker != nil && w.store == store {
		return w.worker
	}
	if w.worker != nil {
		w.worker.close()
	}
	w.store = store
	w.worker = newProjectionPersistWorker(context.Background(), store, w.onPersist)
	return w.worker
}

func (w *handlerProjectionWriter) close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	worker := w.worker
	w.worker = nil
	w.store = nil
	w.mu.Unlock()
	if worker != nil {
		worker.close()
	}
}

type secretLogPattern struct {
	re          *regexp.Regexp
	replacement string
}

var secretLogPatterns = []secretLogPattern{
	{
		re:          regexp.MustCompile(`(?i)(authorization\s*[:=]\s*bearer\s+)[^\s"'\\]+`),
		replacement: `${1}<redacted>`,
	},
	{
		re:          regexp.MustCompile(`(?i)((?:api[_-]?key|token|password|secret|auth[_-]?token)\s*[:=]\s*)[^\s"'\\]+`),
		replacement: `${1}<redacted>`,
	},
	{
		re:          regexp.MustCompile(`(?i)((?:--(?:api-key|token|password|secret|auth-token))(?:=|\s+))[^\s"'\\]+`),
		replacement: `${1}<redacted>`,
	},
}

func normalizePermissionModeForClaude(mode string) string {
	return session.NormalizeClaudePermissionMode(mode)
}

type Handler struct {
	AuthToken        string
	NewExecRunner    func() engine.Runner
	NewPtyRunner     func() engine.Runner
	Upgrader         websocket.Upgrader
	SkillLauncher    *skills.Launcher
	SessionStore     data.Store
	PushService      push.Service
	DeviceTrust      *e2ee.DeviceTrustStore
	NodeIdentity     *e2ee.NodeIdentityStore
	runtimeSessions  *runtimeSessionRegistry
	relayDeviceConns *relayDeviceConnectionRegistry
	projectionWriter *handlerProjectionWriter

	skillLauncherMu sync.RWMutex
	asyncActionsMu  sync.Mutex
	asyncActions    map[string]struct{}

	sessionListGeneration  atomic.Uint64
	sessionListFreshMu     sync.Mutex
	sessionListFreshStates map[string]*sessionListFreshnessState
	freshnessSubsMu        sync.RWMutex
	freshnessSubscribers   map[string]freshnessSubscriber

	muProgressPush   sync.Mutex
	lastProgressPush map[string]time.Time
}

func NewHandler(authToken string, sessionStore data.Store) *Handler {
	handler := &Handler{
		AuthToken: authToken,
		NewExecRunner: func() engine.Runner {
			return engine.NewExecRunner()
		},
		NewPtyRunner: func() engine.Runner {
			return engine.NewPtyRunner()
		},
		SkillLauncher:          skills.NewLauncher(sessionStore),
		SessionStore:           sessionStore,
		PushService:            &push.NoopService{},
		relayDeviceConns:       newRelayDeviceConnectionRegistry(),
		asyncActions:           make(map[string]struct{}),
		sessionListFreshStates: make(map[string]*sessionListFreshnessState),
		freshnessSubscribers:   make(map[string]freshnessSubscriber),
		lastProgressPush:       make(map[string]time.Time),
		Upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
	handler.projectionWriter = newHandlerProjectionWriter(handler.handleProjectionPersisted)
	handler.runtimeSessions = newRuntimeSessionRegistry(func(sessionID string) *session.Service {
		return session.NewService(sessionID, session.Dependencies{
			NewExecRunner: handler.NewExecRunner,
			NewPtyRunner:  handler.NewPtyRunner,
		})
	}, defaultRuntimeSessionReleaseAfter, func(sessionID string) {
		if sessionStore == nil || strings.TrimSpace(sessionID) == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		record, err := sessionStore.GetSession(ctx, sessionID)
		if err != nil {
			return
		}
		record.Summary.ExecutionActive = false
		sessionStore.UpsertSession(ctx, record)
	})
	return handler
}

func (h *Handler) currentSessionListGeneration() uint64 {
	if h == nil {
		return 0
	}
	return h.sessionListGeneration.Load()
}

func (h *Handler) bumpSessionListGeneration() uint64 {
	if h == nil {
		return 0
	}
	return h.sessionListGeneration.Add(1)
}

func (h *Handler) scheduleSessionListFreshnessBump(sessionID string, summary data.SessionSummary) uint64 {
	if h == nil {
		return 0
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return h.currentSessionListGeneration()
	}
	fingerprint := sessionSummaryFreshnessFingerprint(summary)
	h.sessionListFreshMu.Lock()
	if h.sessionListFreshStates == nil {
		h.sessionListFreshStates = make(map[string]*sessionListFreshnessState)
	}
	state := h.sessionListFreshStates[sessionID]
	if state == nil {
		generation := h.bumpSessionListGeneration()
		state = &sessionListFreshnessState{
			fingerprint: fingerprint,
			generation:  generation,
		}
		h.sessionListFreshStates[sessionID] = state
		state.timer = time.AfterFunc(projectionSessionListFreshnessDelay, func() {
			h.flushSessionListFreshnessState(sessionID)
		})
		h.sessionListFreshMu.Unlock()
		return generation
	}
	if state.fingerprint != fingerprint {
		state.fingerprint = fingerprint
		state.dirty = true
	}
	generation := state.generation
	if generation == 0 {
		generation = h.currentSessionListGeneration()
	}
	h.sessionListFreshMu.Unlock()
	return generation
}

func (h *Handler) flushSessionListFreshnessState(sessionID string) {
	if h == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	h.sessionListFreshMu.Lock()
	state := h.sessionListFreshStates[sessionID]
	if state == nil {
		h.sessionListFreshMu.Unlock()
		return
	}
	if state.dirty {
		state.dirty = false
		generation := h.bumpSessionListGeneration()
		state.generation = generation
		state.timer = time.AfterFunc(projectionSessionListFreshnessDelay, func() {
			h.flushSessionListFreshnessState(sessionID)
		})
		h.sessionListFreshMu.Unlock()
		h.emitSessionUpdatedHint(sessionID, generation, "projection_persisted")
		return
	}
	delete(h.sessionListFreshStates, sessionID)
	h.sessionListFreshMu.Unlock()
}

func (h *Handler) handleProjectionPersisted(sessionID string, summary data.SessionSummary) {
	if h == nil {
		return
	}
	generation := h.scheduleSessionListFreshnessBump(sessionID, summary)
	h.emitSessionUpdatedHint(sessionID, generation, "projection_persisted")
}

func (h *Handler) emitSessionUpdatedHint(sessionID string, generation uint64, reason string) {
	if h == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	cursor := int64(0)
	if h.runtimeSessions != nil {
		if entry := h.runtimeSessions.Get(sessionID); entry != nil {
			cursor = entry.latestCursor()
		}
	}
	if generation == 0 {
		generation = h.currentSessionListGeneration()
	}
	h.publishFreshnessEvent(protocol.NewSessionUpdatedEvent(sessionID, generation, cursor, reason))
}

func (h *Handler) registerFreshnessSubscriber(id string, emit func(any) bool) func() {
	if h == nil || strings.TrimSpace(id) == "" || emit == nil {
		return func() {}
	}
	id = strings.TrimSpace(id)
	h.freshnessSubsMu.Lock()
	if h.freshnessSubscribers == nil {
		h.freshnessSubscribers = make(map[string]freshnessSubscriber)
	}
	h.freshnessSubscribers[id] = freshnessSubscriber{id: id, emit: emit}
	h.freshnessSubsMu.Unlock()
	return func() {
		h.freshnessSubsMu.Lock()
		delete(h.freshnessSubscribers, id)
		h.freshnessSubsMu.Unlock()
	}
}

func (h *Handler) publishFreshnessEvent(event any) {
	if h == nil || event == nil {
		return
	}
	h.freshnessSubsMu.RLock()
	subscribers := make([]freshnessSubscriber, 0, len(h.freshnessSubscribers))
	for _, subscriber := range h.freshnessSubscribers {
		subscribers = append(subscribers, subscriber)
	}
	h.freshnessSubsMu.RUnlock()
	for _, subscriber := range subscribers {
		if subscriber.emit == nil {
			continue
		}
		if ok := subscriber.emit(event); !ok {
			logx.Warn("ws", "session freshness notification dropped: subscriberID=%s eventType=%T", subscriber.id, event)
		}
	}
}

func sessionSummaryFreshnessFingerprint(summary data.SessionSummary) string {
	parts := []string{
		strings.TrimSpace(summary.ID),
		strings.TrimSpace(summary.Title),
		summary.UpdatedAt.UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(summary.LastPreview),
		fmt.Sprintf("%d", summary.EntryCount),
		strings.TrimSpace(summary.Runtime.CWD),
		strings.TrimSpace(summary.Runtime.Engine),
		strings.TrimSpace(summary.Runtime.Command),
		strings.TrimSpace(summary.Runtime.PermissionMode),
		strings.TrimSpace(summary.Runtime.CodexSandboxMode),
		strings.TrimSpace(summary.Runtime.ClaudeLifecycle),
		strings.TrimSpace(summary.Runtime.Source),
		strings.TrimSpace(summary.Source),
		fmt.Sprintf("%t", summary.External),
		strings.TrimSpace(summary.Ownership),
		fmt.Sprintf("%t", summary.ExecutionActive),
		strings.TrimSpace(summary.ClaudeSessionUUID),
		fmt.Sprintf("%d", summary.JSONLSyncEntryCount),
	}
	return strings.Join(parts, "\x1f")
}

func (h *Handler) currentSkillLauncher() *skills.Launcher {
	if h == nil {
		return nil
	}
	h.skillLauncherMu.RLock()
	defer h.skillLauncherMu.RUnlock()
	return h.SkillLauncher
}

func (h *Handler) replaceSkillLauncher(launcher *skills.Launcher) {
	if h == nil {
		return
	}
	h.skillLauncherMu.Lock()
	h.SkillLauncher = launcher
	h.skillLauncherMu.Unlock()
}

func (h *Handler) Close() {
	if h == nil {
		return
	}
	if h.runtimeSessions != nil {
		h.runtimeSessions.CleanupAll()
	}
	if h.projectionWriter != nil {
		h.projectionWriter.close()
	}
	h.sessionListFreshMu.Lock()
	for sessionID, state := range h.sessionListFreshStates {
		if state != nil && state.timer != nil {
			state.timer.Stop()
		}
		delete(h.sessionListFreshStates, sessionID)
	}
	h.sessionListFreshMu.Unlock()
	h.freshnessSubsMu.Lock()
	h.freshnessSubscribers = make(map[string]freshnessSubscriber)
	h.freshnessSubsMu.Unlock()
}

type gatewayAsyncActionOptions struct {
	Action       string
	SessionID    string
	ConnectionID string
	RemoteAddr   string
	OnBusy       func()
	OnPanic      func(any, string)
	Run          func()
}

type runtimeEventProcessor struct {
	SessionID       string
	Runtime         *runtimeSession
	Service         *session.Service
	Emit            func(any)
	Context         func() (context.Context, context.CancelFunc)
	BuildProjection func(string, *session.Service) data.ProjectionSnapshot
	Persist         func(string, data.ProjectionSnapshot, bool)
}

func (h *Handler) runtimeEventContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Second)
}

func (h *Handler) runtimeEventSinkForSession(sessionID string, fallbackEmit func(any)) func(any) {
	runtimeSessionID := strings.TrimSpace(sessionID)
	if h == nil {
		if fallbackEmit != nil {
			return fallbackEmit
		}
		return func(any) {}
	}
	if runtimeSessionID == "" || h.runtimeSessions == nil {
		return h.runtimeEventSink(runtimeEventProcessor{
			Emit: fallbackEmit,
		})
	}
	entry := h.runtimeSessions.Ensure(runtimeSessionID)
	service := (*session.Service)(nil)
	if entry != nil {
		service = entry.service
	}
	return h.runtimeEventSink(runtimeEventProcessor{
		SessionID:       runtimeSessionID,
		Runtime:         entry,
		Service:         service,
		Emit:            fallbackEmit,
		Context:         h.runtimeEventContext,
		BuildProjection: h.buildRuntimeProjectionSnapshotForEvent,
		Persist:         h.persistRuntimeProjection,
	})
}

func (h *Handler) runtimeEventSink(opts runtimeEventProcessor) func(any) {
	runtimeSessionID := strings.TrimSpace(opts.SessionID)
	emitSessionEvent := func(event any) {
		if opts.Runtime != nil {
			opts.Runtime.emit(event)
			return
		}
		if opts.Emit != nil {
			opts.Emit(event)
		}
	}
	return func(event any) {
		h.processRuntimeEvent(opts, runtimeSessionID, emitSessionEvent, event)
	}
}

func (h *Handler) processRuntimeEvent(opts runtimeEventProcessor, runtimeSessionID string, emitSessionEvent func(any), event any) {
	if h == nil || event == nil {
		return
	}
	if strings.TrimSpace(runtimeSessionID) == "" {
		emitSessionEvent(event)
		return
	}
	switch event.(type) {
	case protocol.PromptRequestEvent:
		if h.processPermissionPromptEvent(opts, runtimeSessionID, emitSessionEvent, event) {
			return
		}
	case protocol.InteractionRequestEvent:
		if h.processInteractionRequestEvent(opts, runtimeSessionID, emitSessionEvent, event) {
			return
		}
	}
	switch e := event.(type) {
	case protocol.CatalogAuthoringResultEvent:
		h.processCatalogAuthoringEvent(opts, runtimeSessionID, emitSessionEvent, e)
	default:
		h.applyAndPersistRuntimeEvent(opts, runtimeSessionID, emitSessionEvent, event)
	}
}

func (h *Handler) processPermissionPromptEvent(opts runtimeEventProcessor, runtimeSessionID string, emitSessionEvent func(any), event any) bool {
	e, ok := event.(protocol.PromptRequestEvent)
	if !ok {
		return false
	}
	if strings.TrimSpace(e.RuntimeMeta.BlockingKind) != "permission" {
		return false
	}
	if requestID := permissionRequestIDFromEvent(event); requestID != "" && opts.BuildProjection != nil {
		currentProjection := opts.BuildProjection(runtimeSessionID, opts.Service)
		if isResolvedPermissionID(currentProjection.Controller.ResolvedPermissionIDs, requestID) {
			logx.Info("ws", "permission event already resolved, skipping: sessionID=%s requestID=%s", runtimeSessionID, requestID)
			return true
		}
	}
	if h.tryAutoApplyRuntimePermission(opts, runtimeSessionID, emitSessionEvent, event) {
		logx.Info("ws", "permission event auto-applied: sessionID=%s", runtimeSessionID)
		return true
	}
	logx.Info("ws", "permission event forwarding to client: sessionID=%s eventType=%T", runtimeSessionID, event)
	return false
}

func (h *Handler) processInteractionRequestEvent(opts runtimeEventProcessor, runtimeSessionID string, emitSessionEvent func(any), event any) bool {
	if h.tryAutoApplyRuntimePermission(opts, runtimeSessionID, emitSessionEvent, event) {
		logx.Info("ws", "interaction event auto-applied: sessionID=%s", runtimeSessionID)
		return true
	}
	logx.Info("ws", "interaction event forwarding to client: sessionID=%s eventType=%T", runtimeSessionID, event)
	return false
}

func (h *Handler) tryAutoApplyRuntimePermission(opts runtimeEventProcessor, runtimeSessionID string, emitSessionEvent func(any), event any) bool {
	if opts.Context == nil {
		return false
	}
	eventCtx, eventCancel := opts.Context()
	defer eventCancel()
	applied, err := maybeAutoApplyPermissionEvent(
		eventCtx,
		h.SessionStore,
		runtimeSessionID,
		event,
		opts.Service,
		emitSessionEvent,
		h.runtimeEventSinkForSession(runtimeSessionID, opts.Emit),
	)
	if err == nil && applied {
		return true
	}
	return false
}

func (h *Handler) processCatalogAuthoringEvent(opts runtimeEventProcessor, runtimeSessionID string, emitSessionEvent func(any), event protocol.CatalogAuthoringResultEvent) {
	switch event.Domain {
	case "skill":
		if event.Skill == nil {
			emitSessionEvent(protocol.NewErrorEvent(runtimeSessionID, "catalog authoring 缺少 skill payload", ""))
			return
		}
		if opts.Context == nil {
			emitSessionEvent(protocol.NewErrorEvent(runtimeSessionID, "runtime event context unavailable", ""))
			return
		}
		eventCtx, eventCancel := opts.Context()
		err := upsertLocalSkill(h.SessionStore, eventCtx, *event.Skill)
		eventCancel()
		if err != nil {
			emitSessionEvent(protocol.NewErrorEvent(runtimeSessionID, err.Error(), ""))
			return
		}
		h.replaceSkillLauncher(skills.NewLauncher(h.SessionStore))
		eventCtx, eventCancel = opts.Context()
		emitSkillCatalogResult(emitSessionEvent, h.SessionStore, eventCtx, runtimeSessionID)
		eventCancel()
	case "memory":
		if event.Memory == nil {
			emitSessionEvent(protocol.NewErrorEvent(runtimeSessionID, "catalog authoring 缺少 memory payload", ""))
			return
		}
		if opts.Context == nil {
			emitSessionEvent(protocol.NewErrorEvent(runtimeSessionID, "runtime event context unavailable", ""))
			return
		}
		eventCtx, eventCancel := opts.Context()
		syncCWD := resolveCatalogSyncCWD(h.SessionStore, eventCtx, runtimeSessionID, "")
		err := upsertMemoryItem(h.SessionStore, eventCtx, *event.Memory, syncCWD)
		eventCancel()
		if err != nil {
			emitSessionEvent(protocol.NewErrorEvent(runtimeSessionID, err.Error(), ""))
			return
		}
		eventCtx, eventCancel = opts.Context()
		emitMemoryListResult(emitSessionEvent, h.SessionStore, eventCtx, runtimeSessionID)
		eventCancel()
	default:
		emitSessionEvent(protocol.NewErrorEvent(runtimeSessionID, "未知 catalog authoring domain", ""))
	}
}

func (h *Handler) applyAndPersistRuntimeEvent(opts runtimeEventProcessor, runtimeSessionID string, emitSessionEvent func(any), event any) {
	event = prepareSessionEventForResume(opts.Runtime, runtimeSessionID, event)
	emitSessionEvent(event)
	h.sendPushNotificationIfNeeded(context.Background(), runtimeSessionID, event)
	if opts.BuildProjection == nil {
		return
	}
	projection := opts.BuildProjection(runtimeSessionID, opts.Service)
	snapshot, ok := session.ApplyEventToProjection(projection, event)
	if ok {
		if opts.Persist != nil {
			opts.Persist(runtimeSessionID, snapshot, false)
		}
		if status, ok := session.AIStatusEventForBackendEvent(runtimeSessionID, opts.Service, snapshot, event); ok {
			h.emitRuntimeAIStatus(opts, runtimeSessionID, emitSessionEvent, status)
		}
		if opts.Runtime != nil {
			opts.Runtime.markPersisted(eventCursorFromEvent(event))
		}
		return
	}
	if status, ok := session.AIStatusEventForBackendEvent(runtimeSessionID, opts.Service, projection, event); ok {
		h.emitRuntimeAIStatus(opts, runtimeSessionID, emitSessionEvent, status)
	}
}

func (h *Handler) emitRuntimeAIStatus(opts runtimeEventProcessor, runtimeSessionID string, emitSessionEvent func(any), status protocol.AIStatusEvent) {
	event := any(status)
	if opts.Runtime != nil {
		event = prepareSessionEventForResume(opts.Runtime, runtimeSessionID, status)
	}
	emitSessionEvent(event)
}

func (h *Handler) buildRuntimeProjectionSnapshotForEvent(sessionID string, service *session.Service) data.ProjectionSnapshot {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	loaded := readProjectionFromSessionStoreRaw(h.SessionStore, ctx, sessionID, "runtime-event", "")
	return h.mergeActiveLiveProjection(sessionID, finalizeProjectionSnapshotForRuntimeService(sessionID, service, loaded))
}

func (h *Handler) persistRuntimeProjection(sessionID string, snapshot data.ProjectionSnapshot, force bool) {
	if h == nil || h.SessionStore == nil || strings.TrimSpace(sessionID) == "" {
		if h != nil && h.SessionStore == nil {
			logx.Warn("ws", "skip runtime projection persistence because session store is unavailable: sessionID=%s", sessionID)
		}
		return
	}
	merged := session.NormalizeProjectionSnapshot(snapshot)
	if entry := h.runtimeSessions.Get(sessionID); entry != nil {
		merged = entry.updateLatestProjection(merged)
	}
	if h.projectionWriter != nil {
		h.projectionWriter.enqueue(h.SessionStore, sessionID, merged, force)
	}
}

func (h *Handler) mergeActiveLiveProjection(sessionID string, base data.ProjectionSnapshot) data.ProjectionSnapshot {
	if h == nil || h.runtimeSessions == nil {
		return session.NormalizeProjectionSnapshot(base)
	}
	entry := h.runtimeSessions.Get(sessionID)
	if entry == nil {
		return session.NormalizeProjectionSnapshot(base)
	}
	live, _, ok := entry.latestProjectionSnapshot()
	if !ok {
		return session.NormalizeProjectionSnapshot(base)
	}
	return mergeProjectionSnapshotsForCache(base, live)
}

func (h *Handler) tryStartAsyncAction(ctx context.Context, opts gatewayAsyncActionOptions) bool {
	if h == nil || opts.Run == nil {
		return false
	}
	key := gatewayAsyncActionKey(opts.Action, opts.SessionID)
	if key == "" {
		return false
	}
	h.asyncActionsMu.Lock()
	if h.asyncActions == nil {
		h.asyncActions = make(map[string]struct{})
	}
	if _, exists := h.asyncActions[key]; exists {
		h.asyncActionsMu.Unlock()
		if opts.OnBusy != nil {
			opts.OnBusy()
		}
		return false
	}
	h.asyncActions[key] = struct{}{}
	h.asyncActionsMu.Unlock()

	go func() {
		defer func() {
			h.asyncActionsMu.Lock()
			delete(h.asyncActions, key)
			h.asyncActionsMu.Unlock()
		}()
		defer func() {
			if recovered := recover(); recovered != nil {
				stack := logx.StackTrace()
				logx.Error(
					"ws",
					"async action panic recovered: connectionID=%s sessionID=%s remoteAddr=%s action=%s panic=%v\n%s",
					opts.ConnectionID,
					opts.SessionID,
					opts.RemoteAddr,
					opts.Action,
					recovered,
					stack,
				)
				if opts.OnPanic != nil {
					opts.OnPanic(recovered, stack)
				}
			}
		}()
		if ctx.Err() != nil {
			return
		}
		opts.Run()
	}()
	return true
}

func gatewayAsyncActionKey(action, sessionID string) string {
	action = strings.TrimSpace(action)
	sessionID = strings.TrimSpace(sessionID)
	if action == "" || sessionID == "" {
		return ""
	}
	return action + "\x00" + sessionID
}

func catalogAsyncScopeKey() string {
	return "catalog"
}

type ClientConn interface {
	ReadJSON(v any) error
	WriteJSON(v any) error
	Close() error
	RemoteAddr() string
	Origin() string
}

type gatewayWriteRequest struct {
	event any
	done  chan error
}

type gatewayCoalescedSessionUpdatedWrite struct {
	queue *gatewayWriteQueues
	key   string
	event *protocol.SessionUpdatedEvent
}

type gatewayReadResult struct {
	payload map[string]any
	err     error
}

type gatewaySessionListResult struct {
	actionKey  string
	sequence   uint64
	filterCWD  string
	generation uint64
	items      []data.SessionSummary
	err        error
}

type gatewaySessionLoadResult struct {
	actionKey         string
	sequence          uint64
	previousSessionID string
	req               protocol.SessionLoadRequestEvent
	loadHistoryLimit  int
	trace             *sessionLoadTrace
	activeRuntimeLoad bool
	record            data.SessionRecord
	historyWindow     data.SessionHistoryWindow
	historyWindowOK   bool
	err               error
}

type gatewaySessionResumeResult struct {
	actionKey           string
	sequence            uint64
	previousSessionID   string
	req                 protocol.SessionResumeRequestEvent
	resumeHistoryLimit  int
	activeRuntimeResume bool
	record              data.SessionRecord
	historyWindow       data.SessionHistoryWindow
	historyWindowOK     bool
	err                 error
}

type gatewayAsyncEventResult struct {
	actionKey string
	action    string
	sessionID string
	event     any
	events    []any
	err       error
}

type gatewaySessionDeltaPendingRequest struct {
	targetSessionID string
	req             protocol.SessionDeltaRequestEvent
}

func enqueueGatewayWrite(ctx context.Context, writeCh chan<- any, priorityWriteCh chan<- any, event any) {
	targetCh := writeCh
	if isPriorityGatewayEvent(event) {
		targetCh = priorityWriteCh
	}
	session.Enqueue(ctx, targetCh, event)
}

func tryEnqueueGatewayWrite(ctx context.Context, writeCh chan<- any, priorityWriteCh chan<- any, event any) bool {
	targetCh := writeCh
	if isPriorityGatewayEvent(event) {
		targetCh = priorityWriteCh
	}
	select {
	case <-ctx.Done():
		return false
	case targetCh <- event:
		return true
	default:
		return false
	}
}

func newGatewayWriteQueues() *gatewayWriteQueues {
	return &gatewayWriteQueues{
		writeCh:                    make(chan any, gatewayWriteQueueSize),
		priorityWriteCh:            make(chan any, gatewayPriorityWriteQueueSize),
		pendingSessionUpdatedByKey: make(map[string]*protocol.SessionUpdatedEvent),
	}
}

func (q *gatewayWriteQueues) tryEnqueue(ctx context.Context, event any) bool {
	if q == nil {
		return false
	}
	if sessionUpdated, ok := event.(protocol.SessionUpdatedEvent); ok {
		return q.tryEnqueueSessionUpdated(ctx, sessionUpdated)
	}
	return tryEnqueueGatewayWrite(ctx, q.writeCh, q.priorityWriteCh, event)
}

func (q *gatewayWriteQueues) tryEnqueueSessionUpdated(ctx context.Context, event protocol.SessionUpdatedEvent) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	key := strings.TrimSpace(event.SessionID)
	if key == "" {
		key = "__unknown_session__"
	}
	if pending := q.pendingSessionUpdatedByKey[key]; pending != nil {
		*pending = latestSessionUpdatedEvent(*pending, event)
		return true
	}
	pending := latestSessionUpdatedEvent(protocol.SessionUpdatedEvent{}, event)
	wrapper := gatewayCoalescedSessionUpdatedWrite{
		queue: q,
		key:   key,
		event: &pending,
	}
	q.pendingSessionUpdatedByKey[key] = &pending
	select {
	case <-ctx.Done():
		delete(q.pendingSessionUpdatedByKey, key)
		return false
	case q.priorityWriteCh <- wrapper:
		return true
	default:
		delete(q.pendingSessionUpdatedByKey, key)
		return false
	}
}

func latestSessionUpdatedEvent(current protocol.SessionUpdatedEvent, next protocol.SessionUpdatedEvent) protocol.SessionUpdatedEvent {
	if strings.TrimSpace(current.SessionID) == "" {
		return next
	}
	if strings.TrimSpace(next.SessionID) == "" {
		return current
	}
	latest := current
	if next.Generation > latest.Generation {
		latest.Generation = next.Generation
	}
	if next.EventCursor > latest.EventCursor {
		latest.EventCursor = next.EventCursor
	}
	if !next.Timestamp.IsZero() && (latest.Timestamp.IsZero() || next.Timestamp.After(latest.Timestamp)) {
		latest.Event.Timestamp = next.Timestamp
	}
	if strings.TrimSpace(next.Reason) != "" {
		latest.Reason = next.Reason
	}
	return latest
}

func sessionIDFromGatewayEvent(event any) string {
	switch e := event.(type) {
	case protocol.Event:
		return strings.TrimSpace(e.SessionID)
	case protocol.LogEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.ProgressEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.ErrorEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.ClientActionAckEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.CompactionEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.ContextWindowUsageEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.InteractionRequestEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.PromptRequestEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.SessionStateEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.AgentStateEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.AIStatusEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.RuntimePhaseEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.TaskSnapshotEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.StepUpdateEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.FileDiffEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.ADBFrameEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.SessionCreatedEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.SessionListResultEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.SessionHistoryEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.SessionDeltaEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.SessionUpdatedEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.SessionHistoryPageEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.SessionTerminalRangeEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.SessionDiffPageEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.SessionTerminalExecutionPageEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.SessionResumeResultEvent:
		return strings.TrimSpace(e.SessionID)
	case protocol.SessionResumeNoticeEvent:
		return strings.TrimSpace(e.SessionID)
	case map[string]any:
		raw, ok := e["sessionId"]
		if !ok || raw == nil {
			return ""
		}
		sessionID := strings.TrimSpace(fmt.Sprint(raw))
		if sessionID == "<nil>" {
			return ""
		}
		return sessionID
	default:
		return ""
	}
}

func (w gatewayCoalescedSessionUpdatedWrite) payload() (protocol.SessionUpdatedEvent, bool) {
	if w.queue == nil || w.event == nil {
		return protocol.SessionUpdatedEvent{}, false
	}
	w.queue.mu.Lock()
	defer w.queue.mu.Unlock()
	event := *w.event
	if w.queue.pendingSessionUpdatedByKey != nil && w.queue.pendingSessionUpdatedByKey[w.key] == w.event {
		delete(w.queue.pendingSessionUpdatedByKey, w.key)
	}
	return event, strings.TrimSpace(event.SessionID) != ""
}

func isPriorityGatewayEvent(event any) bool {
	switch payload := event.(type) {
	case protocol.ClientActionAckEvent:
		return true
	case map[string]any:
		return strings.TrimSpace(fmt.Sprint(payload["type"])) == "pong"
	case protocol.TaskSnapshotEvent:
		return isGatewayHealthSnapshot(payload)
	case protocol.AIStatusEvent:
		return isGatewayHealthAIStatus(payload)
	case protocol.PromptRequestEvent, protocol.InteractionRequestEvent:
		return true
	case protocol.ErrorEvent:
		return true
	case protocol.SessionUpdatedEvent:
		return true
	case gatewayWriteRequest:
		return isPriorityGatewayEvent(payload.event)
	case gatewayCoalescedSessionUpdatedWrite:
		return true
	default:
		return false
	}
}

func isDroppableGatewayEvent(event any) bool {
	switch payload := event.(type) {
	case protocol.LogEvent:
		return true
	case protocol.ProgressEvent:
		return true
	case protocol.AgentStateEvent:
		return true
	case protocol.RuntimePhaseEvent:
		return true
	case protocol.StepUpdateEvent:
		return true
	case protocol.FileDiffEvent:
		return true
	case protocol.ADBFrameEvent:
		return true
	case protocol.TaskSnapshotEvent:
		return !isGatewayHealthSnapshot(payload)
	case protocol.AIStatusEvent:
		return !isGatewayHealthAIStatus(payload)
	case gatewayWriteRequest:
		return isDroppableGatewayEvent(payload.event)
	default:
		return false
	}
}

func isGatewayHealthSnapshot(event protocol.TaskSnapshotEvent) bool {
	message := strings.TrimSpace(strings.ToLower(event.Message))
	step := strings.TrimSpace(strings.ToLower(event.Step))
	return message == "heartbeat" ||
		message == "sync" ||
		strings.Contains(message, "(heartbeat)") ||
		strings.Contains(message, "(sync)") ||
		step == "heartbeat" ||
		step == "sync"
}

func isGatewayHealthAIStatus(event protocol.AIStatusEvent) bool {
	phase := strings.TrimSpace(strings.ToLower(event.Phase))
	label := strings.TrimSpace(strings.ToLower(event.Label))
	return phase == "settled" ||
		strings.Contains(label, "heartbeat") ||
		strings.Contains(label, "sync")
}

func nextGatewayWriterEvent(ctx context.Context, writeCh <-chan any, priorityWriteCh <-chan any) (any, bool) {
	select {
	case event, ok := <-priorityWriteCh:
		return event, ok
	default:
	}
	select {
	case <-ctx.Done():
		return nil, false
	case event, ok := <-priorityWriteCh:
		return event, ok
	case event, ok := <-writeCh:
		return event, ok
	}
}

type relayE2EEClientConn interface {
	RelayE2EEInfo() RelayE2EEInfo
}

type RelayE2EEInfo struct {
	Enabled     bool
	SessionID   string
	ClientID    string
	HandshakeID string
	DeviceID    string
}

type websocketClientConn struct {
	conn       *websocket.Conn
	remoteAddr string
	origin     string
}

func (c *websocketClientConn) ReadJSON(v any) error {
	messageType, payload, err := c.conn.ReadMessage()
	if err != nil {
		return err
	}
	if messageType != websocket.TextMessage {
		return fmt.Errorf("only text messages are supported")
	}
	return json.Unmarshal(payload, v)
}

func (c *websocketClientConn) WriteJSON(v any) error {
	if err := c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	if err := c.conn.WriteJSON(v); err != nil {
		return err
	}
	return c.conn.SetWriteDeadline(time.Time{})
}

func (c *websocketClientConn) Close() error       { return c.conn.Close() }
func (c *websocketClientConn) RemoteAddr() string { return c.remoteAddr }
func (c *websocketClientConn) Origin() string     { return c.origin }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" || token != h.AuthToken {
		logx.Warn("ws", "reject unauthorized request: remoteAddr=%s", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := h.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		logx.Error("ws", "websocket upgrade failed: remoteAddr=%s err=%v", r.RemoteAddr, err)
		return
	}
	conn.SetPingHandler(func(appData string) error {
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(10*time.Second))
	})
	h.ServeClientConn(r.Context(), &websocketClientConn{
		conn:       conn,
		remoteAddr: r.RemoteAddr,
		origin:     strings.TrimSpace(r.Header.Get("Origin")),
	})
}

func (h *Handler) ServeClientConn(parentCtx context.Context, client ClientConn) {
	connectionID := fmt.Sprintf("conn-%d", time.Now().UTC().UnixNano())
	selectedSessionID := ""
	remoteAddr := client.RemoteAddr()
	connected := false
	sessionListFilterCWD := ""

	emitIfPossible := func(event any) {
		_ = client.WriteJSON(event)
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			stack := logx.StackTrace()
			logx.Error("ws", "serve panic recovered: connectionID=%s sessionID=%s remoteAddr=%s panic=%v\n%s", connectionID, selectedSessionID, remoteAddr, recovered, stack)
			if connected {
				emitIfPossible(protocol.NewErrorEvent(selectedSessionID, "internal server error", stack))
			}
		}
	}()

	connected = true
	defer client.Close()

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	newDetachedRuntimeService := func() *session.Service {
		return session.NewService("", session.Dependencies{
			NewExecRunner: h.NewExecRunner,
			NewPtyRunner:  h.NewPtyRunner,
		})
	}
	runtimeSvc := newDetachedRuntimeService()
	var activeRuntimeSession *runtimeSession // nil for detached service, set when Attach() succeeds
	readCh := make(chan gatewayReadResult, gatewayReadQueueSize)
	sessionListResultCh := make(chan gatewaySessionListResult, 1)
	sessionLoadResultCh := make(chan gatewaySessionLoadResult, 1)
	sessionResumeResultCh := make(chan gatewaySessionResumeResult, 1)
	asyncEventResultCh := make(chan gatewayAsyncEventResult, gatewayWriteQueueSize)
	writeQueues := newGatewayWriteQueues()
	writeCh := writeQueues.writeCh
	priorityWriteCh := writeQueues.priorityWriteCh
	writeErrCh := make(chan error, 1)
	signalWriterFailure := func(err error) {
		if err == nil {
			return
		}
		select {
		case writeErrCh <- err:
		default:
		}
		cancel()
		_ = client.Close()
	}
	var writerWG sync.WaitGroup
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		defer func() {
			if recovered := recover(); recovered != nil {
				stack := logx.StackTrace()
				logx.Error("ws", "writer panic recovered: connectionID=%s sessionID=%s remoteAddr=%s panic=%v\n%s", connectionID, selectedSessionID, remoteAddr, recovered, stack)
				signalWriterFailure(fmt.Errorf("writer panic: %v", recovered))
			}
		}()
		for {
			event, ok := nextGatewayWriterEvent(ctx, writeCh, priorityWriteCh)
			if !ok {
				return
			}
			payload := event
			var done chan error
			if req, ok := event.(gatewayWriteRequest); ok {
				payload = req.event
				done = req.done
			}
			if req, ok := event.(gatewayCoalescedSessionUpdatedWrite); ok {
				next, ok := req.payload()
				if !ok {
					continue
				}
				payload = next
			}
			if err := client.WriteJSON(payload); err != nil {
				if done != nil {
					done <- err
					close(done)
				}
				logx.Error("ws", "write websocket event failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				signalWriterFailure(err)
				return
			}
			if done != nil {
				done <- nil
				close(done)
			}
		}
	}()

	go func() {
		for {
			payload := map[string]any{}
			if err := client.ReadJSON(&payload); err != nil {
				select {
				case readCh <- gatewayReadResult{err: err}:
				case <-ctx.Done():
				}
				return
			}
			select {
			case readCh <- gatewayReadResult{payload: payload}:
			case <-ctx.Done():
				return
			}
		}
	}()

	logx.Info("ws", "connection established: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)

	sessionProjectionContext := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), 2*time.Second)
	}
	sessionListCache := sessionListCacheEntry{}
	invalidateSessionListCache := func() {
		sessionListCache = sessionListCacheEntry{}
	}
	var projectionCacheMu sync.Mutex
	projectionCache := make(map[string]data.ProjectionSnapshot)
	cacheProjection := func(sessionID string, projection data.ProjectionSnapshot, merge bool) data.ProjectionSnapshot {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			return session.NormalizeProjectionSnapshot(projection)
		}
		normalized := session.NormalizeProjectionSnapshot(projection)
		projectionCacheMu.Lock()
		if merge {
			if current, ok := projectionCache[sessionID]; ok {
				normalized = mergeProjectionSnapshotsForCache(current, normalized)
			}
		}
		projectionCache[sessionID] = cloneProjectionSnapshot(normalized)
		projectionCacheMu.Unlock()
		if !merge {
			if entry := h.runtimeSessions.Get(sessionID); entry != nil {
				entry.updateLatestProjection(normalized)
			}
		}
		return cloneProjectionSnapshot(normalized)
	}
	cachedProjection := func(sessionID string) (data.ProjectionSnapshot, bool) {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			return data.ProjectionSnapshot{}, false
		}
		projectionCacheMu.Lock()
		projection, ok := projectionCache[sessionID]
		projectionCacheMu.Unlock()
		return cloneProjectionSnapshot(projection), ok
	}
	readSessionContextForAction := func(action, sessionID string) (data.SessionContext, error) {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			return data.SessionContext{}, nil
		}
		if h.SessionStore == nil {
			return data.SessionContext{}, fmt.Errorf("session store unavailable")
		}
		contextStore, ok := h.SessionStore.(data.SessionContextStore)
		if !ok {
			return data.SessionContext{}, fmt.Errorf("%s requires session context store for session %s", action, sessionID)
		}
		snapshot, err := contextStore.GetSessionContext(context.WithoutCancel(ctx), sessionID)
		if err != nil {
			return data.SessionContext{}, err
		}
		return snapshot.SessionContext, nil
	}
	saveSelectedSessionContext := func(sessionID string, sessionContext data.SessionContext) (data.SessionContext, error) {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			return data.SessionContext{}, fmt.Errorf("session ID is required")
		}
		if h.SessionStore == nil {
			return data.SessionContext{}, fmt.Errorf("session store unavailable")
		}
		contextStore, ok := h.SessionStore.(data.SessionContextStore)
		if !ok {
			return data.SessionContext{}, fmt.Errorf("session_context_update requires session context store for session %s", sessionID)
		}
		normalized := data.SessionContext{
			EnabledSkillNames: append([]string(nil), sessionContext.EnabledSkillNames...),
			EnabledMemoryIDs:  append([]string(nil), sessionContext.EnabledMemoryIDs...),
			Configured:        true,
		}
		if _, err := contextStore.SaveSessionContext(context.WithoutCancel(ctx), data.SessionContextSnapshot{
			SessionID:      sessionID,
			SessionContext: normalized,
		}); err != nil {
			return data.SessionContext{}, err
		}
		if cached, ok := cachedProjection(sessionID); ok {
			cached.SessionContext = normalized
			cached.SessionContextSet = true
			cacheProjection(sessionID, cached, false)
		}
		return normalized, nil
	}
	readRuntimeProcessExecutionLog := func(sessionID string, item protocol.RuntimeProcessItem) (data.TerminalExecution, string, error) {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			return data.TerminalExecution{}, "", fmt.Errorf("session ID is required")
		}
		executionID := strings.TrimSpace(item.ExecutionID)
		if executionID == "" {
			return data.TerminalExecution{}, "该进程没有可用的终端执行日志", nil
		}
		if h.SessionStore == nil {
			return data.TerminalExecution{}, "", fmt.Errorf("session store unavailable")
		}
		executionStore, ok := h.SessionStore.(data.SessionTerminalExecutionStore)
		if !ok {
			return data.TerminalExecution{}, "", fmt.Errorf("runtime_process_log_get requires session terminal execution store for session %s", sessionID)
		}
		snapshot, err := executionStore.GetSessionTerminalExecution(context.WithoutCancel(ctx), data.SessionTerminalExecutionRequest{
			SessionID:     sessionID,
			ExecutionID:   executionID,
			IncludeOutput: true,
		})
		if err != nil {
			if isSessionTerminalExecutionNotFoundError(err, executionID, sessionID) {
				return data.TerminalExecution{}, "该进程没有可用的终端执行日志", nil
			}
			return data.TerminalExecution{}, "", err
		}
		execution := snapshot.TerminalExecution
		message := ""
		if strings.TrimSpace(execution.Stdout) == "" && strings.TrimSpace(execution.Stderr) == "" {
			message = "该进程暂无已捕获的 stdout / stderr"
		}
		return execution, message, nil
	}
	updateProjectionMeta := func(record data.SessionRecord) {
		if h.projectionWriter == nil {
			return
		}
		h.projectionWriter.updateMeta(h.SessionStore, record)
	}
	finalizeProjectionSnapshotForService := func(sessionID string, service *session.Service, loaded data.ProjectionSnapshot) data.ProjectionSnapshot {
		return finalizeProjectionSnapshotForRuntimeService(sessionID, service, loaded)
	}

	readProjectionFromRuntimeMetadata := func(sessionID string) (data.ProjectionSnapshot, bool) {
		metaStore, ok := h.SessionStore.(data.SessionRuntimeMetaStore)
		if !ok {
			logx.Warn("ws", "runtime metadata projection unavailable because store does not implement SessionRuntimeMetaStore: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, sessionID, remoteAddr)
			return data.ProjectionSnapshot{}, false
		}
		meta, err := metaStore.GetSessionRuntimeMetadata(context.WithoutCancel(ctx), sessionID)
		if err != nil {
			logx.Warn("ws", "runtime metadata projection read failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
			return data.ProjectionSnapshot{}, false
		}
		return meta.Record.Projection, true
	}

	buildProjectionSnapshotForService := func(sessionID string, service *session.Service) data.ProjectionSnapshot {
		storeCtx, storeCancel := sessionProjectionContext()
		defer storeCancel()
		loaded := data.ProjectionSnapshot{}
		if cached, ok := cachedProjection(sessionID); ok {
			loaded = cached
			return h.mergeActiveLiveProjection(sessionID, finalizeProjectionSnapshotForService(sessionID, service, loaded))
		}
		if service != nil {
			runtimeSnapshot := service.RuntimeSnapshot()
			if runtimeSnapshot.Running && strings.TrimSpace(runtimeSnapshot.ActiveSession) == strings.TrimSpace(sessionID) {
				if metaProjection, ok := readProjectionFromRuntimeMetadata(sessionID); ok {
					loaded = metaProjection
				}
				return h.mergeActiveLiveProjection(sessionID, finalizeProjectionSnapshotForService(sessionID, service, loaded))
			}
		}
		loaded = readProjectionFromSessionStore(h.SessionStore, storeCtx, sessionID, connectionID, remoteAddr)
		return h.mergeActiveLiveProjection(sessionID, finalizeProjectionSnapshotForService(sessionID, service, loaded))
	}

	buildRuntimeProjectionSnapshotForService := func(sessionID string, service *session.Service) data.ProjectionSnapshot {
		storeCtx, storeCancel := sessionProjectionContext()
		defer storeCancel()
		loaded, ok := cachedProjection(sessionID)
		if !ok {
			loaded = readProjectionFromSessionStoreRaw(h.SessionStore, storeCtx, sessionID, connectionID, remoteAddr)
		}
		return h.mergeActiveLiveProjection(sessionID, finalizeProjectionSnapshotForService(sessionID, service, loaded))
	}

	buildStatusProjectionSnapshotForService := func(sessionID string, service *session.Service) data.ProjectionSnapshot {
		loaded, ok := cachedProjection(sessionID)
		if !ok {
			loaded, _ = readProjectionFromRuntimeMetadata(sessionID)
		}
		return h.mergeActiveLiveProjection(sessionID, finalizeProjectionSnapshotForService(sessionID, service, loaded))
	}

	buildProjectionSnapshotFor := func(sessionID string) data.ProjectionSnapshot {
		trimmedSessionID := strings.TrimSpace(sessionID)
		service := runtimeSvc
		if trimmedSessionID == "" {
			service = nil
		} else if trimmedSessionID != strings.TrimSpace(selectedSessionID) {
			service = nil
			if entry := h.runtimeSessions.Ensure(trimmedSessionID); entry != nil {
				service = entry.service
			}
		}
		return buildProjectionSnapshotForService(trimmedSessionID, service)
	}
	loadFullProjectionForInteractiveMutation := func(action, sessionID string) data.ProjectionSnapshot {
		// Interactive mutations can require runtime, controller, diff, permission, and
		// context state at once. Keep this named boundary out of selected-session
		// bootstrap/read hydration paths so remaining full projection loads are auditable.
		logx.Info("ws", "full projection load for interactive mutation: connectionID=%s sessionID=%s remoteAddr=%s action=%s", connectionID, sessionID, remoteAddr, action)
		return buildProjectionSnapshotFor(sessionID)
	}

	runtimeForSession := func(sessionID string) (*runtimeSession, *session.Service) {
		trimmedSessionID := strings.TrimSpace(sessionID)
		if trimmedSessionID == "" {
			return nil, runtimeSvc
		}
		if trimmedSessionID == strings.TrimSpace(selectedSessionID) && activeRuntimeSession != nil {
			return activeRuntimeSession, activeRuntimeSession.service
		}
		entry := h.runtimeSessions.Ensure(trimmedSessionID)
		if entry != nil {
			return entry, entry.service
		}
		return nil, runtimeSvc
	}

	activeRuntimeRecord := func(sessionID string) bool {
		trimmedSessionID := strings.TrimSpace(sessionID)
		if trimmedSessionID == "" || claudesync.IsMirrorSessionID(trimmedSessionID) {
			return false
		}
		entry := h.runtimeSessions.Get(trimmedSessionID)
		if entry == nil || entry.service == nil {
			return false
		}
		snapshot := entry.service.RuntimeSnapshot()
		if !snapshot.Running {
			return false
		}
		if strings.TrimSpace(snapshot.ActiveSession) == trimmedSessionID {
			return true
		}
		if codexsync.IsMirrorSessionID(trimmedSessionID) {
			resumeSessionID := strings.TrimSpace(codexsync.ThreadIDFromMirror(trimmedSessionID))
			if resumeSessionID == "" {
				return false
			}
			candidates := []string{
				snapshot.ResumeSessionID,
				snapshot.ActiveMeta.ResumeSessionID,
			}
			for _, candidate := range candidates {
				if strings.TrimSpace(candidate) == resumeSessionID {
					return true
				}
			}
		}
		return false
	}

	loadStoredSessionRecordForRequest := func(requestedSessionID string) (data.SessionRecord, error) {
		if h.SessionStore == nil {
			return data.SessionRecord{}, fmt.Errorf("session store unavailable")
		}
		targetSessionID := strings.TrimSpace(requestedSessionID)
		if targetSessionID == "" {
			return data.SessionRecord{}, fmt.Errorf("session ID is required")
		}
		return h.SessionStore.GetSession(ctx, targetSessionID)
	}

	loadSessionRecordForRequest := func(requestedSessionID string) (data.SessionRecord, error) {
		if activeRuntimeRecord(requestedSessionID) {
			return loadStoredSessionRecordForRequest(requestedSessionID)
		}
		return loadSessionRecord(ctx, h.SessionStore, requestedSessionID)
	}

	loadSessionDeltaRecord := func(requestedSessionID string) (data.SessionRecord, error) {
		targetSessionID := strings.TrimSpace(requestedSessionID)
		if targetSessionID == "" {
			return data.SessionRecord{}, fmt.Errorf("session ID is required")
		}
		if codexsync.IsMirrorSessionID(targetSessionID) || claudesync.IsMirrorSessionID(targetSessionID) {
			return loadSessionRecord(ctx, h.SessionStore, targetSessionID)
		}
		return loadStoredSessionRecordForRequest(targetSessionID)
	}

	canUseLoadedSessionHistoryWindow := func(record data.SessionRecord) bool {
		if isNativeMirrorSessionID(record.Summary.ID) {
			return codexMirrorCacheMatchesSource(record)
		}
		return true
	}

	loadSessionHistoryWindow := func(targetSessionID string, before, limit int) (data.SessionHistoryWindow, bool, error) {
		targetSessionID = strings.TrimSpace(targetSessionID)
		if targetSessionID == "" {
			return data.SessionHistoryWindow{}, false, fmt.Errorf("session ID is required")
		}
		windowStore, ok := h.SessionStore.(data.SessionHistoryWindowStore)
		if !ok {
			return data.SessionHistoryWindow{}, false, fmt.Errorf("session history window store unavailable for session %s", targetSessionID)
		}
		window, err := windowStore.GetSessionHistoryWindow(context.WithoutCancel(ctx), data.SessionHistoryWindowRequest{
			SessionID: targetSessionID,
			Before:    before,
			Limit:     limit,
		})
		if err != nil {
			return data.SessionHistoryWindow{}, true, err
		}
		return window, true, nil
	}

	loadUsableSessionHistoryWindow := func(targetSessionID string, before, limit int, activeRuntime bool) (loadedSessionHistoryWindow, error) {
		window, ok, err := loadSessionHistoryWindow(targetSessionID, before, limit)
		if err != nil {
			if isNativeMirrorSessionID(targetSessionID) && errors.Is(err, os.ErrNotExist) {
				return loadedSessionHistoryWindow{}, nil
			}
			return loadedSessionHistoryWindow{}, err
		}
		if !ok || !canUseLoadedSessionHistoryWindow(window.Record) {
			if !isNativeMirrorSessionID(targetSessionID) {
				return loadedSessionHistoryWindow{}, fmt.Errorf("session history window unavailable for session %s", targetSessionID)
			}
			return loadedSessionHistoryWindow{}, nil
		}
		return loadedSessionHistoryWindow{window: window, ok: true}, nil
	}

	emitBackpressureHint := func(event any) bool {
		sessionID := sessionIDFromGatewayEvent(event)
		if sessionID == "" {
			sessionID = strings.TrimSpace(selectedSessionID)
		}
		if sessionID == "" {
			return true
		}
		generation := h.currentSessionListGeneration()
		cursor := int64(0)
		if h.runtimeSessions != nil {
			if entry := h.runtimeSessions.Get(sessionID); entry != nil {
				cursor = entry.latestCursor()
			}
		}
		if writeQueues.tryEnqueue(ctx, protocol.NewSessionUpdatedEvent(sessionID, generation, cursor, "gateway_write_backpressure")) {
			return true
		}
		logx.Warn("ws", "gateway backpressure hint enqueue failed: connectionID=%s sessionID=%s remoteAddr=%s eventType=%T", connectionID, sessionID, remoteAddr, event)
		signalWriterFailure(fmt.Errorf("gateway priority write queue full for backpressure hint"))
		return false
	}
	emit := func(event any) {
		if event == nil {
			return
		}
		if writeQueues.tryEnqueue(ctx, event) {
			return
		}
		if isPriorityGatewayEvent(event) {
			logx.Warn("ws", "priority websocket event dropped because gateway write queue is full: connectionID=%s sessionID=%s remoteAddr=%s eventType=%T", connectionID, selectedSessionID, remoteAddr, event)
			signalWriterFailure(fmt.Errorf("gateway priority write queue full for event %T", event))
			return
		}
		if !isDroppableGatewayEvent(event) {
			logx.Warn("ws", "required websocket event cannot be enqueued because gateway write queue is full: connectionID=%s sessionID=%s remoteAddr=%s eventType=%T", connectionID, selectedSessionID, remoteAddr, event)
			signalWriterFailure(fmt.Errorf("gateway write queue full for required event %T", event))
			return
		}
		logx.Warn("ws", "droppable websocket event skipped because gateway write queue is full: connectionID=%s sessionID=%s remoteAddr=%s eventType=%T", connectionID, selectedSessionID, remoteAddr, event)
		emitBackpressureHint(event)
	}

	emitSessionHistoryForStoredSessionWindow := func(targetSessionID string, limit int, sessionRuntime *runtimeSession) (bool, error) {
		loadedWindow, err := loadUsableSessionHistoryWindow(targetSessionID, 0, limit, false)
		if err != nil {
			return false, err
		}
		if !loadedWindow.ok {
			return false, nil
		}
		historyWindow := loadedWindow.window
		record := historyWindow.Record
		_, targetRuntimeSvc := runtimeForSession(record.Summary.ID)
		freshProjection := session.NormalizeProjectionSnapshot(record.Projection)
		runtimeProjection := h.mergeActiveLiveProjection(record.Summary.ID, finalizeProjectionSnapshotForService(record.Summary.ID, targetRuntimeSvc, freshProjection))
		record.Projection = runtimeProjection
		record.Projection.LogEntries = nil
		record.Summary.Runtime = record.Projection.Runtime
		historyWindow.Record = record
		emit(session.SessionHistoryWindowEventFromWindowWithCursorAndPayloadLimit(historyWindow, sessionRecordRuntimeAlive(record, targetRuntimeSvc), deltaCursorSnapshot(sessionRuntime), gatewayRelaySafePayloadBudgetBytes))
		return true, nil
	}

	persistProjection := func(sessionID string, snapshot data.ProjectionSnapshot, force bool) {
		if h.SessionStore == nil || strings.TrimSpace(sessionID) == "" {
			if h.SessionStore == nil {
				logx.Warn("ws", "skip projection persistence because session store is unavailable: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, sessionID, remoteAddr)
			}
			return
		}
		merged := cacheProjection(sessionID, snapshot, true)
		if entry := h.runtimeSessions.Get(sessionID); entry != nil {
			merged = entry.updateLatestProjection(merged)
		}
		if h.projectionWriter != nil {
			h.projectionWriter.enqueue(h.SessionStore, sessionID, merged, force)
		}
	}

	persistProjectionFor := func(sessionID string, snapshot data.ProjectionSnapshot) {
		persistProjection(sessionID, snapshot, false)
	}

	persistProjectionNow := func(sessionID string, snapshot data.ProjectionSnapshot) {
		persistProjection(sessionID, snapshot, true)
	}

	lookupClaudeSessionUUID := func(sessionID string) string {
		if h.SessionStore == nil || strings.TrimSpace(sessionID) == "" {
			return ""
		}
		metaStore, ok := h.SessionStore.(data.SessionRuntimeMetaStore)
		if !ok {
			logx.Warn("ws", "claude session uuid lookup skipped because runtime metadata store unavailable: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, sessionID, remoteAddr)
			return ""
		}
		meta, err := metaStore.GetSessionRuntimeMetadata(context.WithoutCancel(ctx), sessionID)
		if err != nil {
			logx.Warn("ws", "claude session uuid lookup failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
			return ""
		}
		return strings.TrimSpace(meta.Record.Summary.ClaudeSessionUUID)
	}

	buildLiveRuntimeDeltaIfNewer := func(meta data.SessionRuntimeMetadata, req protocol.SessionDeltaRequestEvent, sessionRuntime *runtimeSession, sessionRuntimeSvc *session.Service, runtimeAlive bool) (protocol.SessionDeltaEvent, bool) {
		if sessionRuntime == nil {
			return protocol.SessionDeltaEvent{}, false
		}
		liveProjection, _, ok := sessionRuntime.latestProjectionSnapshot()
		if !ok {
			return protocol.SessionDeltaEvent{}, false
		}
		liveProjection = session.NormalizeProjectionSnapshot(liveProjection)
		liveCount := len(liveProjection.LogEntries)
		if liveCount <= meta.Latest.LogEntryCount || liveCount <= req.Known.LogEntryCount {
			return protocol.SessionDeltaEvent{}, false
		}
		record := meta.Record
		record.Projection = h.mergeActiveLiveProjection(record.Summary.ID, finalizeProjectionSnapshotForService(record.Summary.ID, sessionRuntimeSvc, liveProjection))
		record.Summary.Runtime = record.Projection.Runtime
		if record.Summary.EntryCount < liveCount {
			record.Summary.EntryCount = liveCount
		}
		deltaEvent := session.SessionDeltaEventFromRecordWithPayloadLimit(record, req.Known, deltaCursorSnapshot(sessionRuntime), runtimeAlive, gatewayRelaySafePayloadBudgetBytes, sessionResumeHistoryLimit)
		if deltaEvent.RequiresFullSync {
			return protocol.SessionDeltaEvent{}, false
		}
		logx.Info("ws", "session delta response: connectionID=%s sessionID=%s remoteAddr=%s reason=%q runtimeAlive=%v canResume=%v requiresFullSync=%v logEntries=%d liveRuntimeProjection=true", connectionID, record.Summary.ID, remoteAddr, req.Reason, runtimeAlive, sessionRuntime != nil && runtimeAlive, deltaEvent.RequiresFullSync, liveCount)
		return deltaEvent, true
	}

	buildWindowMetadataDeltaIfPossible := func(req protocol.SessionDeltaRequestEvent, targetSessionID string, sessionRuntime *runtimeSession, sessionRuntimeSvc *session.Service) (protocol.SessionDeltaEvent, bool, error) {
		if req.Known.LogEntryCount < 0 {
			return protocol.SessionDeltaEvent{}, false, nil
		}
		metaStore, ok := h.SessionStore.(data.SessionRuntimeMetaStore)
		if !ok {
			if !isNativeMirrorSessionID(targetSessionID) {
				return protocol.SessionDeltaEvent{}, true, fmt.Errorf("session runtime metadata store unavailable for session %s", targetSessionID)
			}
			return protocol.SessionDeltaEvent{}, false, nil
		}
		meta, err := metaStore.GetSessionRuntimeMetadata(context.WithoutCancel(ctx), targetSessionID)
		if err != nil {
			if isNativeMirrorSessionID(targetSessionID) && errors.Is(err, os.ErrNotExist) {
				return protocol.SessionDeltaEvent{}, false, nil
			}
			return protocol.SessionDeltaEvent{}, true, err
		}
		if isNativeMirrorSessionID(targetSessionID) && !codexMirrorCacheMatchesSource(meta.Record) {
			return protocol.SessionDeltaEvent{}, false, nil
		}
		if isNativeMirrorSessionID(targetSessionID) && req.Known.LogEntryCount > meta.Latest.LogEntryCount {
			return protocol.SessionDeltaEvent{}, false, nil
		}
		record := meta.Record
		freshProjection := session.NormalizeProjectionSnapshot(record.Projection)
		runtimeProjection := h.mergeActiveLiveProjection(record.Summary.ID, finalizeProjectionSnapshotForService(record.Summary.ID, sessionRuntimeSvc, freshProjection))
		record.Projection = runtimeProjection
		record.Projection.LogEntries = nil
		record.Summary.Runtime = record.Projection.Runtime
		meta.Record = record
		runtimeAlive := sessionRecordRuntimeAlive(record, sessionRuntimeSvc)
		if deltaEvent, ok := buildLiveRuntimeDeltaIfNewer(meta, req, sessionRuntime, sessionRuntimeSvc, runtimeAlive); ok {
			return deltaEvent, true, nil
		}
		if !isNativeMirrorSessionID(targetSessionID) && strings.TrimSpace(meta.Record.Summary.ClaudeSessionUUID) != "" {
			cwd := strings.TrimSpace(firstNonEmptyString(meta.Record.Summary.Runtime.CWD, meta.Record.Projection.Runtime.CWD))
			csuuid := strings.TrimSpace(meta.Record.Summary.ClaudeSessionUUID)
			if cwd != "" && csuuid != "" {
				newEntries, newCount, mergeErr := claudesync.ReadJSONLAppendEntries(cwd, csuuid, meta.Record.Summary.JSONLSyncEntryCount)
				if mergeErr != nil {
					return protocol.SessionDeltaEvent{}, true, mergeErr
				}
				if len(newEntries) > 0 {
					appendStore, ok := h.SessionStore.(data.SessionLogEntryAppendStore)
					if !ok {
						return protocol.SessionDeltaEvent{}, true, fmt.Errorf("session log entry append store unavailable for claude jsonl refresh: %s", targetSessionID)
					}
					summary, appendErr := appendStore.AppendSessionLogEntries(context.WithoutCancel(ctx), targetSessionID, newEntries, data.WithJSONLSyncEntryCount(newCount))
					if appendErr != nil {
						return protocol.SessionDeltaEvent{}, true, appendErr
					}
					record.Summary = summary
					meta.Record = record
					meta.Latest.LogEntryCount = summary.EntryCount
					appendWindow, err := loadUsableSessionHistoryWindow(targetSessionID, meta.Latest.LogEntryCount, sessionHistoryWindowLimit(meta.Latest.LogEntryCount-req.Known.LogEntryCount), false)
					if err != nil {
						return protocol.SessionDeltaEvent{}, true, err
					}
					if !appendWindow.ok {
						return protocol.SessionDeltaEvent{}, true, fmt.Errorf("session history append window unavailable for session %s", targetSessionID)
					}
					appendWindow.window.Record = record
					if appendWindow.window.LogEntryTotal != meta.Latest.LogEntryCount {
						return protocol.SessionDeltaEvent{}, true, fmt.Errorf("session history window count mismatch for %s: window total %d, metadata count %d", targetSessionID, appendWindow.window.LogEntryTotal, meta.Latest.LogEntryCount)
					}
					deltaEvent := session.SessionDeltaEventFromHistoryWindowWithCounts(appendWindow.window, meta.Latest, req.Known, deltaCursorSnapshot(sessionRuntime), runtimeAlive, gatewayRelaySafePayloadBudgetBytes)
					logx.Info("ws", "session delta response: connectionID=%s sessionID=%s remoteAddr=%s reason=%q runtimeAlive=%v canResume=%v requiresFullSync=%v logEntries=%d appendWindow=true claudeJSONLRefresh=true appendEntries=%d", connectionID, record.Summary.ID, remoteAddr, req.Reason, runtimeAlive, sessionRuntime != nil && runtimeAlive, deltaEvent.RequiresFullSync, meta.Latest.LogEntryCount, len(deltaEvent.AppendLogEntries))
					return deltaEvent, true, nil
				}
				if newCount > meta.Record.Summary.JSONLSyncEntryCount {
					meta.Record.Summary.JSONLSyncEntryCount = newCount
				}
			}
		}
		if req.Known.LogEntryCount < meta.Latest.LogEntryCount {
			loadedWindow, err := loadUsableSessionHistoryWindow(targetSessionID, meta.Latest.LogEntryCount, sessionHistoryWindowLimit(meta.Latest.LogEntryCount-req.Known.LogEntryCount), false)
			if err != nil {
				return protocol.SessionDeltaEvent{}, true, err
			}
			if !loadedWindow.ok {
				if !isNativeMirrorSessionID(targetSessionID) {
					return protocol.SessionDeltaEvent{}, true, fmt.Errorf("session history append window unavailable for session %s", targetSessionID)
				}
				return protocol.SessionDeltaEvent{}, false, nil
			}
			window := loadedWindow.window
			window.Record = record
			if window.LogEntryTotal != meta.Latest.LogEntryCount {
				return protocol.SessionDeltaEvent{}, true, fmt.Errorf("session history window count mismatch for %s: window total %d, metadata count %d", targetSessionID, window.LogEntryTotal, meta.Latest.LogEntryCount)
			}
			deltaEvent := session.SessionDeltaEventFromHistoryWindowWithCounts(window, meta.Latest, req.Known, deltaCursorSnapshot(sessionRuntime), runtimeAlive, gatewayRelaySafePayloadBudgetBytes)
			logx.Info("ws", "session delta response: connectionID=%s sessionID=%s remoteAddr=%s reason=%q runtimeAlive=%v canResume=%v requiresFullSync=%v logEntries=%d appendWindow=true appendEntries=%d", connectionID, record.Summary.ID, remoteAddr, req.Reason, runtimeAlive, sessionRuntime != nil && runtimeAlive, deltaEvent.RequiresFullSync, meta.Latest.LogEntryCount, len(deltaEvent.AppendLogEntries))
			return deltaEvent, true, nil
		}
		deltaEvent := session.SessionDeltaEventFromRuntimeMetadata(meta, req.Known, deltaCursorSnapshot(sessionRuntime), runtimeAlive, req.Known.LogEntryCount > meta.Latest.LogEntryCount)
		if deltaEvent.RequiresFullSync {
			deltaEvent.PayloadLimited = true
			deltaEvent.PayloadLimitReason = "client_known_log_count_exceeds_latest"
		}
		logx.Info("ws", "session delta response: connectionID=%s sessionID=%s remoteAddr=%s reason=%q runtimeAlive=%v canResume=%v requiresFullSync=%v logEntries=%d runtimeMetaStore=true", connectionID, record.Summary.ID, remoteAddr, req.Reason, runtimeAlive, sessionRuntime != nil && runtimeAlive, deltaEvent.RequiresFullSync, meta.Latest.LogEntryCount)
		return deltaEvent, true, nil
	}
	loadProjectionRecordForAsyncEvent := func(targetSessionID string) (data.SessionRecord, *runtimeSession, error) {
		record, err := loadSessionDeltaRecord(targetSessionID)
		if err != nil {
			return data.SessionRecord{}, nil, err
		}
		updateProjectionMeta(record)
		sessionRuntime, sessionRuntimeSvc := runtimeForSession(record.Summary.ID)
		if !isNativeMirrorSessionID(record.Summary.ID) {
			record = mergeClaudeJSONLToRecord(ctx, h.SessionStore, record, sessionRuntimeSvc)
			updateProjectionMeta(record)
		}
		freshProjection := session.NormalizeProjectionSnapshot(record.Projection)
		runtimeProjection := h.mergeActiveLiveProjection(record.Summary.ID, finalizeProjectionSnapshotForService(record.Summary.ID, sessionRuntimeSvc, freshProjection))
		if isNativeMirrorSessionID(record.Summary.ID) {
			record.Projection = mergeProjectionWithOptionalRuntime(freshProjection, runtimeProjection, sessionRuntimeSvc, record.Summary.ID)
		} else {
			record.Projection = runtimeProjection
		}
		record.Summary.Runtime = record.Projection.Runtime
		return record, sessionRuntime, nil
	}
	buildSessionDeltaEvent := func(req protocol.SessionDeltaRequestEvent, targetSessionID string) (any, error) {
		sessionRuntime, sessionRuntimeSvc := runtimeForSession(targetSessionID)
		if deltaEvent, ok, err := buildWindowMetadataDeltaIfPossible(req, targetSessionID, sessionRuntime, sessionRuntimeSvc); err != nil {
			return nil, err
		} else if ok {
			return deltaEvent, nil
		}
		record, sessionRuntime, err := loadProjectionRecordForAsyncEvent(targetSessionID)
		if err != nil {
			return nil, err
		}
		_, sessionRuntimeSvc = runtimeForSession(record.Summary.ID)
		cacheProjection(record.Summary.ID, record.Projection, false)
		runtimeAlive := sessionRecordRuntimeAlive(record, sessionRuntimeSvc)
		deltaEvent := session.SessionDeltaEventFromRecordWithPayloadLimit(record, req.Known, deltaCursorSnapshot(sessionRuntime), runtimeAlive, gatewayRelaySafePayloadBudgetBytes, sessionResumeHistoryLimit)
		metrics := projectionMetrics(record.Projection)
		logx.Info("ws", "session delta response: connectionID=%s sessionID=%s remoteAddr=%s reason=%q runtimeAlive=%v canResume=%v requiresFullSync=%v logEntries=%d diffs=%d stdoutBytes=%d stderrBytes=%d", connectionID, record.Summary.ID, remoteAddr, req.Reason, runtimeAlive, sessionRuntime != nil && runtimeAlive, deltaEvent.RequiresFullSync, metrics.logEntries, metrics.diffs, metrics.stdoutBytes, metrics.stderrBytes)
		return deltaEvent, nil
	}
	unregisterFreshnessSubscriber := h.registerFreshnessSubscriber(connectionID, func(event any) bool {
		if writeQueues.tryEnqueue(ctx, event) {
			return true
		}
		if ctx.Err() != nil {
			return false
		}
		sessionID := sessionIDFromGatewayEvent(event)
		logx.Warn("ws", "gateway freshness event enqueue failed: connectionID=%s sessionID=%s remoteAddr=%s eventType=%T", connectionID, sessionID, remoteAddr, event)
		signalWriterFailure(fmt.Errorf("gateway priority write queue full for freshness event %T", event))
		return false
	})
	emitAndWait := func(event any) error {
		done := make(chan error, 1)
		req := gatewayWriteRequest{event: event, done: done}
		if !tryEnqueueGatewayWrite(ctx, writeCh, priorityWriteCh, req) {
			return fmt.Errorf("gateway write queue full for event %T", event)
		}
		select {
		case err := <-done:
			return err
		case err := <-writeErrCh:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	emitContextWindowUsageIfAvailable := func(sessionID string, service *session.Service) {
		if service == nil {
			return
		}
		ctxUsage, cancelUsage := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelUsage()
		usage, ok, err := service.CurrentContextWindowUsage(ctxUsage, sessionID)
		if err != nil {
			logx.Warn("ws", "load context window usage failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
			return
		}
		if !ok || usage.TokenLimit <= 0 {
			return
		}
		emit(protocol.ApplyRuntimeMeta(
			protocol.NewContextWindowUsageEvent(sessionID, usage),
			service.RuntimeSnapshot().ActiveMeta,
		))
	}
	ackClientAction := func(sessionID, action string, client protocol.ClientEvent) bool {
		clientActionID := strings.TrimSpace(client.ClientActionID)
		if clientActionID == "" {
			return true
		}
		targetSessionID := strings.TrimSpace(sessionID)
		if targetSessionID == "" {
			targetSessionID = strings.TrimSpace(selectedSessionID)
		}
		now := time.Now().UTC()
		accepted := true
		if h.SessionStore != nil && targetSessionID != "" {
			duplicate, err := h.SessionStore.MarkClientAction(ctx, targetSessionID, data.ClientActionRecord{
				ClientActionID: clientActionID,
				Action:         strings.TrimSpace(action),
				Status:         "accepted",
				AckedAt:        now,
			}, clientActionDedupeTTL, clientActionDedupeLimit)
			if err != nil {
				logx.Warn("ws", "persist client action marker failed: connectionID=%s sessionID=%s remoteAddr=%s action=%s clientActionID=%s err=%v", connectionID, targetSessionID, remoteAddr, action, clientActionID, err)
				emit(protocol.NewErrorEvent(targetSessionID, fmt.Sprintf("记录操作确认失败：%v", err), ""))
				return false
			}
			accepted = !duplicate
		}
		sessionRuntime := h.runtimeSessions.Ensure(targetSessionID)
		if sessionRuntime != nil {
			sessionRuntime.mu.Lock()
			sessionRuntime.lastClientMessageAt = now
			sessionRuntime.mu.Unlock()
			accepted = sessionRuntime.markClientAction(clientActionID, now) && accepted
		}
		emit(protocol.NewClientActionAckEvent(targetSessionID, action, clientActionID, "accepted", !accepted))
		return accepted
	}

	adbRTC := newADBWebRTCBridge(func() string {
		return selectedSessionID
	}, emit)
	defer adbRTC.Stop("")

	var adbMu sync.Mutex
	var adbCancel context.CancelFunc
	adbActiveSerial := ""

	stopADBStream := func(message string) {
		adbMu.Lock()
		cancel := adbCancel
		activeSerial := adbActiveSerial
		adbCancel = nil
		adbActiveSerial = ""
		adbMu.Unlock()
		if cancel != nil {
			cancel()
		}
		if strings.TrimSpace(message) != "" {
			emit(protocol.NewADBStreamStateEvent(selectedSessionID, false, activeSerial, 0, 0, 0, message))
		}
	}

	emitADBDevices := func(message string) {
		status := adb.DetectStatus(ctx)
		items := make([]protocol.ADBDevice, 0, len(status.Devices))
		for _, item := range status.Devices {
			items = append(items, protocol.ADBDevice{
				Serial:      item.Serial,
				State:       item.State,
				Model:       item.Model,
				Product:     item.Product,
				DeviceName:  item.DeviceName,
				TransportID: item.TransportID,
			})
		}
		statusMessage := strings.TrimSpace(status.Message)
		if strings.TrimSpace(message) != "" {
			statusMessage = message
		}
		emit(protocol.NewADBDevicesResultEvent(
			selectedSessionID,
			items,
			status.PreferredSerial,
			status.AvailableAVDs,
			status.PreferredAVD,
			status.ADBAvailable,
			status.EmulatorAvailable,
			status.SuggestedAction,
			statusMessage,
		))
	}

	buildSessionList := func(listCtx context.Context, filterCWD string, sessionIDSnapshot string) gatewaySessionListResult {
		result := gatewaySessionListResult{
			filterCWD:  normalizeSessionCWD(filterCWD),
			generation: h.currentSessionListGeneration(),
		}
		if h.SessionStore == nil {
			result.err = fmt.Errorf("session store unavailable")
			return result
		}
		items, err := h.SessionStore.ListSessions(listCtx)
		if err != nil {
			result.err = err
			return result
		}
		merged, mergeErr := mergeSessionSummaries(listCtx, h.SessionStore, items, result.filterCWD)
		if mergeErr != nil {
			logx.Warn("ws", "merge session summaries failed: connectionID=%s sessionID=%s remoteAddr=%s cwd=%q err=%v", connectionID, sessionIDSnapshot, remoteAddr, result.filterCWD, mergeErr)
			merged = filterStoreSessionsByCWD(items, result.filterCWD)
		}
		result.items = merged
		return result
	}

	startADBStream := func(serial string, interval time.Duration) {
		stopADBStream("")
		streamCtx, cancel := context.WithCancel(ctx)
		adbMu.Lock()
		adbCancel = cancel
		adbMu.Unlock()

		go func(sessionID string, requestedSerial string, frameInterval time.Duration) {
			resolvedSerial, err := adb.ResolveSerial(streamCtx, requestedSerial)
			if err != nil {
				emit(protocol.NewADBStreamStateEvent(sessionID, false, requestedSerial, 0, 0, int(frameInterval/time.Millisecond), err.Error()))
				return
			}

			adbMu.Lock()
			adbActiveSerial = resolvedSerial
			adbMu.Unlock()

			seq := 0
			for {
				frame, frameErr := adb.CaptureFrame(streamCtx, resolvedSerial)
				if frameErr != nil {
					if streamCtx.Err() != nil {
						return
					}
					emit(protocol.NewADBStreamStateEvent(sessionID, false, resolvedSerial, 0, 0, int(frameInterval/time.Millisecond), frameErr.Error()))
					stopADBStream("")
					return
				}
				seq++
				emit(protocol.NewADBFrameEvent(
					sessionID,
					frame.Serial,
					frame.Format,
					base64.StdEncoding.EncodeToString(frame.Data),
					frame.Width,
					frame.Height,
					seq,
				))
				emit(protocol.NewADBStreamStateEvent(sessionID, true, frame.Serial, frame.Width, frame.Height, int(frameInterval/time.Millisecond), "ADB 画面预览中"))

				timer := time.NewTimer(frameInterval)
				select {
				case <-streamCtx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}(selectedSessionID, serial, interval)
	}

	type runtimeSwitchReleaseMode int
	const (
		runtimeSwitchDetachPrevious runtimeSwitchReleaseMode = iota
		runtimeSwitchCleanupPrevious
	)
	switchRuntimeSession := func(sessionID string, releaseMode runtimeSwitchReleaseMode) {
		logx.Info("ws", "switch runtime session: connectionID=%s previousSessionID=%s nextSessionID=%s remoteAddr=%s", connectionID, selectedSessionID, sessionID, remoteAddr)
		nextSessionID := strings.TrimSpace(sessionID)
		previousSessionID := strings.TrimSpace(selectedSessionID)
		previousRuntimeSvc := runtimeSvc
		if previousSessionID == nextSessionID && nextSessionID != "" {
			if entry := h.runtimeSessions.Attach(nextSessionID, connectionID, emit); entry != nil {
				runtimeSvc = entry.service
				activeRuntimeSession = entry
			}
			selectedSessionID = nextSessionID
			return
		}
		if previousSessionID != "" {
			h.runtimeSessions.Release(previousSessionID, connectionID, releaseMode == runtimeSwitchCleanupPrevious)
		} else if previousRuntimeSvc != nil {
			previousRuntimeSvc.Cleanup()
		}
		selectedSessionID = nextSessionID
		if nextSessionID == "" {
			runtimeSvc = newDetachedRuntimeService()
			activeRuntimeSession = nil
			return
		}
		if entry := h.runtimeSessions.Attach(nextSessionID, connectionID, emit); entry != nil {
			runtimeSvc = entry.service
			activeRuntimeSession = entry
			return
		}
		runtimeSvc = newDetachedRuntimeService()
	}

	resolvePermissionDecisionRuntime := func(req protocol.PermissionDecisionRequestEvent) (string, *session.Service) {
		targetSessionID := strings.TrimSpace(firstNonEmptyString(req.SessionID, selectedSessionID))
		if targetSessionID != "" {
			if targetSessionID != strings.TrimSpace(selectedSessionID) {
				switchRuntimeSession(targetSessionID, runtimeSwitchDetachPrevious)
			}
			return targetSessionID, runtimeSvc
		}
		resumeSessionID := strings.TrimSpace(req.ResumeSessionID)
		if resumeSessionID == "" {
			return "", runtimeSvc
		}
		matchedSessionID, matchedRuntime := h.runtimeSessions.FindByResumeSessionID(resumeSessionID)
		if matchedSessionID == "" || matchedRuntime == nil {
			return "", runtimeSvc
		}
		switchRuntimeSession(matchedSessionID, runtimeSwitchDetachPrevious)
		return matchedSessionID, runtimeSvc
	}

	sessionLoadInFlight := make(map[string]struct{})
	sessionResumeInFlight := make(map[string]struct{})
	var latestSessionListSequence uint64
	var latestSessionSwitchSequence uint64
	pendingSessionSwitchTargetID := ""
	asyncEventInFlight := make(map[string]struct{})
	postSessionListActions := make(map[string][]func(gatewaySessionListResult))
	type pendingSessionListRequest struct {
		filterCWD string
		actions   []func(gatewaySessionListResult)
	}
	var pendingSessionList *pendingSessionListRequest
	sessionDeltaPending := make(map[string]gatewaySessionDeltaPendingRequest)
	var emitAndPersistFor func(sessionID string) func(any)
	var startSessionListWithAfter func(filterCWD string, after func(gatewaySessionListResult))
	invalidateSessionListCacheForMutation := func() {
		invalidateSessionListCache()
		h.bumpSessionListGeneration()
		latestSessionListSequence++
	}
	drainPendingSessionList := func() {
		if pendingSessionList == nil {
			return
		}
		pending := pendingSessionList
		pendingSessionList = nil
		after := func(nextResult gatewaySessionListResult) {
			for _, action := range pending.actions {
				if action != nil {
					action(nextResult)
				}
			}
		}
		if len(pending.actions) == 0 {
			after = nil
		}
		startSessionListWithAfter(pending.filterCWD, after)
	}

	applySessionListResult := func(result gatewaySessionListResult) {
		var actions []func(gatewaySessionListResult)
		if strings.TrimSpace(result.actionKey) != "" {
			delete(asyncEventInFlight, result.actionKey)
			actions = postSessionListActions[result.actionKey]
			delete(postSessionListActions, result.actionKey)
		}
		if result.sequence != latestSessionListSequence {
			logx.Info("ws", "ignore stale session_list result: connectionID=%s sessionID=%s remoteAddr=%s sequence=%d latestSequence=%d cwd=%q", connectionID, selectedSessionID, remoteAddr, result.sequence, latestSessionListSequence, result.filterCWD)
			drainPendingSessionList()
			return
		}
		sessionListFilterCWD = normalizeSessionCWD(result.filterCWD)
		if result.err != nil {
			logx.Error("ws", "list sessions failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, result.err)
			emit(protocol.NewErrorEventWithCode(selectedSessionID, result.err.Error(), "", "session_list_failed"))
			drainPendingSessionList()
			return
		}
		sessionListCache = sessionListCacheEntry{
			cwd:        sessionListFilterCWD,
			generation: result.generation,
			createdAt:  time.Now(),
			items:      cloneSessionSummaries(result.items),
		}
		emit(protocol.NewSessionListResultEvent(selectedSessionID, toProtocolSummaries(result.items)))
		for _, action := range actions {
			if action != nil {
				action(result)
			}
		}
		drainPendingSessionList()
	}

	startSessionListWithAfter = func(filterCWD string, after func(gatewaySessionListResult)) {
		if h.SessionStore == nil {
			logx.Warn("ws", "session list requested but session store unavailable: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
			emit(protocol.NewErrorEventWithCode(selectedSessionID, "session store unavailable", "", "session_list_failed"))
			return
		}
		requestedFilterCWD := normalizeSessionCWD(filterCWD)
		now := time.Now()
		generation := h.currentSessionListGeneration()
		if sessionListCache.cwd == requestedFilterCWD &&
			sessionListCache.generation == generation &&
			!sessionListCache.createdAt.IsZero() &&
			now.Sub(sessionListCache.createdAt) <= sessionListCacheTTL {
			cached := cloneSessionSummaries(sessionListCache.items)
			latestSessionListSequence++
			sessionListFilterCWD = requestedFilterCWD
			logx.Info("ws", "session list cache hit: connectionID=%s sessionID=%s remoteAddr=%s cwd=%q items=%d", connectionID, selectedSessionID, remoteAddr, sessionListFilterCWD, len(cached))
			emit(protocol.NewSessionListResultEvent(selectedSessionID, toProtocolSummaries(cached)))
			if after != nil {
				after(gatewaySessionListResult{
					sequence:   latestSessionListSequence,
					filterCWD:  requestedFilterCWD,
					generation: generation,
					items:      cached,
				})
			}
			return
		}
		actionKey := connectionID + ":session_list"
		sessionIDSnapshot := strings.TrimSpace(selectedSessionID)
		if _, exists := asyncEventInFlight[actionKey]; exists {
			if pendingSessionList == nil || pendingSessionList.filterCWD != requestedFilterCWD {
				pendingSessionList = &pendingSessionListRequest{filterCWD: requestedFilterCWD}
			}
			if after != nil {
				pendingSessionList.actions = append(pendingSessionList.actions, after)
			}
			return
		}
		latestSessionListSequence++
		listSequence := latestSessionListSequence
		asyncEventInFlight[actionKey] = struct{}{}
		if after != nil {
			postSessionListActions[actionKey] = append(postSessionListActions[actionKey], after)
		}
		go func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					stack := logx.StackTrace()
					logx.Error(
						"ws",
						"session_list async action panic recovered: connectionID=%s sessionID=%s remoteAddr=%s panic=%v\n%s",
						connectionID,
						actionKey,
						remoteAddr,
						recovered,
						stack,
					)
					select {
					case sessionListResultCh <- gatewaySessionListResult{actionKey: actionKey, sequence: listSequence, filterCWD: requestedFilterCWD, generation: generation, err: fmt.Errorf("internal server error")}:
					case <-ctx.Done():
					}
				}
			}()
			if ctx.Err() != nil {
				return
			}
			result := buildSessionList(ctx, requestedFilterCWD, sessionIDSnapshot)
			result.actionKey = actionKey
			result.sequence = listSequence
			if result.generation == 0 {
				result.generation = generation
			}
			select {
			case sessionListResultCh <- result:
			case <-ctx.Done():
			}
		}()
	}

	startSessionList := func(filterCWD string) {
		startSessionListWithAfter(filterCWD, nil)
	}
	startExplicitSessionList := func(filterCWD string) {
		startSessionListWithAfter(filterCWD, nil)
	}

	emitEmptySessionState := func() {
		emit(protocol.NewSessionStateEvent(selectedSessionID, string(session.StateActive), "session cleared"))
	}

	pendingSessionSwitchActive := func() bool {
		return strings.TrimSpace(pendingSessionSwitchTargetID) != ""
	}
	targetIsPendingSessionSwitch := func(sessionID string) bool {
		targetSessionID := strings.TrimSpace(sessionID)
		return targetSessionID != "" && targetSessionID == strings.TrimSpace(pendingSessionSwitchTargetID)
	}
	var startSessionDeltaGetAction func(targetSessionID string, req protocol.SessionDeltaRequestEvent, actionKey string)
	clearPendingSessionSwitch := func(sessionID string) {
		if targetIsPendingSessionSwitch(sessionID) {
			pendingSessionSwitchTargetID = ""
		}
	}
	canProcessSelectedSessionReadAction := func(action, targetSessionID string) bool {
		targetSessionID = strings.TrimSpace(targetSessionID)
		if targetSessionID == "" {
			return false
		}
		if pendingSessionSwitchActive() && !targetIsPendingSessionSwitch(targetSessionID) {
			logx.Info("ws", "ignore %s while session switch is pending: connectionID=%s sessionID=%s pendingSessionID=%s requestedSessionID=%s remoteAddr=%s", action, connectionID, selectedSessionID, pendingSessionSwitchTargetID, targetSessionID, remoteAddr)
			return false
		}
		if targetSessionID != strings.TrimSpace(selectedSessionID) && !targetIsPendingSessionSwitch(targetSessionID) {
			logx.Info("ws", "ignore %s for non-selected session: connectionID=%s sessionID=%s pendingSessionID=%s requestedSessionID=%s remoteAddr=%s", action, connectionID, selectedSessionID, pendingSessionSwitchTargetID, targetSessionID, remoteAddr)
			return false
		}
		return true
	}
	handleAsyncEventResult := func(result gatewayAsyncEventResult) {
		actionKey := strings.TrimSpace(result.actionKey)
		if actionKey != "" {
			delete(asyncEventInFlight, actionKey)
		}
		targetSessionID := strings.TrimSpace(result.sessionID)
		processResult := targetSessionID == "" || canProcessSelectedSessionReadAction(result.action+" result", targetSessionID)
		if processResult {
			if result.err != nil {
				logx.Warn("ws", "%s failed: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s err=%v", result.action, connectionID, selectedSessionID, targetSessionID, remoteAddr, result.err)
				emit(protocol.NewErrorEvent(selectedSessionID, result.err.Error(), ""))
			} else {
				if result.event != nil {
					emit(result.event)
				}
				for _, event := range result.events {
					if event != nil {
						emit(event)
					}
				}
			}
		}
		if result.action == "session_delta_get" && actionKey != "" {
			pending, ok := sessionDeltaPending[actionKey]
			delete(sessionDeltaPending, actionKey)
			if ok && startSessionDeltaGetAction != nil {
				pendingTargetSessionID := strings.TrimSpace(pending.targetSessionID)
				if pendingTargetSessionID != "" && canProcessSelectedSessionReadAction("session_delta_get pending", pendingTargetSessionID) {
					startSessionDeltaGetAction(pendingTargetSessionID, pending.req, actionKey)
				}
			}
		}
	}
	handleSessionLoadResult := func(result gatewaySessionLoadResult) {
		delete(sessionLoadInFlight, result.actionKey)
		req := result.req
		trace := result.trace
		activeRuntimeLoad := result.activeRuntimeLoad
		record := result.record
		if result.sequence != latestSessionSwitchSequence {
			logx.Info("ws", "ignore stale session_load result: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s sequence=%d latestSequence=%d", connectionID, selectedSessionID, req.SessionID, remoteAddr, result.sequence, latestSessionSwitchSequence)
			if trace != nil {
				trace.Finish("", activeRuntimeLoad, projectionTraceMetrics{})
			}
			return
		}
		if result.err != nil {
			clearPendingSessionSwitch(req.SessionID)
			logx.Warn("ws", "load session failed: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, req.SessionID, remoteAddr, result.err)
			if trace != nil {
				trace.Finish("", activeRuntimeLoad, projectionTraceMetrics{})
			}
			errorSessionID := strings.TrimSpace(req.SessionID)
			emit(protocol.NewErrorEventWithCode(errorSessionID, result.err.Error(), "", "session_load_failed"))
			return
		}
		if trace == nil {
			trace = newSessionLoadTrace(connectionID, selectedSessionID, req.SessionID, remoteAddr, req.Reason)
		}
		trace.Step("load_record")
		if !result.historyWindowOK {
			updateProjectionMeta(record)
		}
		switchRuntimeSession(record.Summary.ID, runtimeSwitchDetachPrevious)
		clearPendingSessionSwitch(record.Summary.ID)
		trace.Step("switch_runtime")
		if !activeRuntimeLoad && !isNativeMirrorSessionID(record.Summary.ID) && !result.historyWindowOK {
			record = mergeClaudeJSONLToRecord(ctx, h.SessionStore, record, runtimeSvc)
			updateProjectionMeta(record)
			invalidateSessionListCacheForMutation()
		}
		trace.Step("merge_claude_jsonl")
		augmentedProjection := session.NormalizeProjectionSnapshot(record.Projection)
		var runtimeProjection data.ProjectionSnapshot
		if result.historyWindowOK {
			runtimeProjection = h.mergeActiveLiveProjection(record.Summary.ID, finalizeProjectionSnapshotForService(record.Summary.ID, runtimeSvc, augmentedProjection))
		} else {
			runtimeProjection = buildProjectionSnapshotForService(record.Summary.ID, runtimeSvc)
		}
		trace.Step("build_projection")
		if isNativeMirrorSessionID(record.Summary.ID) {
			record.Projection = mergeProjectionWithOptionalRuntime(augmentedProjection, runtimeProjection, runtimeSvc, record.Summary.ID)
		} else {
			record.Projection = runtimeProjection
			if preferAugmentedLogEntries(augmentedProjection.LogEntries, record.Projection.LogEntries) {
				record.Projection.LogEntries = augmentedProjection.LogEntries
			}
		}
		record.Summary.Runtime = record.Projection.Runtime
		if !result.historyWindowOK {
			updateProjectionMeta(record)
			cacheProjection(record.Summary.ID, record.Projection, false)
		}
		runtimeSvc.SyncRuntimeMeta(protocol.RuntimeMeta{
			Command:          record.Projection.Runtime.Command,
			Engine:           record.Projection.Runtime.Engine,
			CodexSandboxMode: record.Projection.Runtime.CodexSandboxMode,
			CWD:              record.Projection.Runtime.CWD,
			PermissionMode:   record.Projection.Runtime.PermissionMode,
			ResumeSessionID:  record.Projection.Runtime.ResumeSessionID,
			ClaudeLifecycle:  record.Projection.Runtime.ClaudeLifecycle,
		})
		loadRuntimeAlive := sessionRecordRuntimeAlive(record, runtimeSvc)
		trace.Step("merge_projection")
		loadSessionRuntime, _ := runtimeForSession(record.Summary.ID)
		if result.historyWindowOK {
			result.historyWindow.Record = record
			result.historyWindow.Record.Projection.LogEntries = nil
			emit(session.SessionHistoryWindowEventFromWindowWithCursorAndPayloadLimit(result.historyWindow, loadRuntimeAlive, deltaCursorSnapshot(loadSessionRuntime), gatewayRelaySafePayloadBudgetBytes))
		} else {
			emit(session.SessionHistoryWindowEventFromRecordWithCursorAndPayloadLimit(record, loadRuntimeAlive, result.loadHistoryLimit, deltaCursorSnapshot(loadSessionRuntime), gatewayRelaySafePayloadBudgetBytes))
		}
		trace.Step("emit_history")
		emitContextWindowUsageIfAvailable(record.Summary.ID, runtimeSvc)
		if restored := restoredAgentStateEventFromRecord(record, loadRuntimeAlive); restored != nil {
			emit(*restored)
			if status, ok := session.AIStatusEventForBackendEvent(record.Summary.ID, runtimeSvc, record.Projection, *restored); ok {
				emit(status)
			}
		}
		trace.Step("emit_review_state")
		emit(protocol.NewSessionStateEvent(selectedSessionID, string(session.StateActive), "history loaded"))
		trace.Step("emit_session_state")
		trace.Finish(record.Summary.ID, activeRuntimeLoad, projectionMetrics(record.Projection))
	}
	handleSessionResumeResult := func(result gatewaySessionResumeResult) {
		delete(sessionResumeInFlight, result.actionKey)
		req := result.req
		if result.sequence != latestSessionSwitchSequence {
			logx.Info("ws", "ignore stale session_resume result: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s sequence=%d latestSequence=%d", connectionID, selectedSessionID, req.SessionID, remoteAddr, result.sequence, latestSessionSwitchSequence)
			return
		}
		if result.err != nil {
			clearPendingSessionSwitch(req.SessionID)
			logx.Warn("ws", "resume session failed: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, req.SessionID, remoteAddr, result.err)
			errorSessionID := strings.TrimSpace(req.SessionID)
			emit(protocol.NewErrorEventWithCode(errorSessionID, result.err.Error(), "", "session_resume_failed"))
			return
		}
		record := result.record
		historyWindow := result.historyWindow
		historyWindowOK := result.historyWindowOK
		if !historyWindowOK {
			updateProjectionMeta(record)
		}
		switchRuntimeSession(record.Summary.ID, runtimeSwitchDetachPrevious)
		clearPendingSessionSwitch(record.Summary.ID)
		sessionRuntime := h.runtimeSessions.Ensure(record.Summary.ID)
		resumeEmitAndPersist := emitAndPersistFor(selectedSessionID)
		runtimeSvc.SetSink(sessionRuntime.EnsureBufferedSinkWithProcessor(resumeEmitAndPersist))
		if !isNativeMirrorSessionID(record.Summary.ID) && !historyWindowOK {
			record = mergeClaudeJSONLToRecord(ctx, h.SessionStore, record, runtimeSvc)
			updateProjectionMeta(record)
		}
		augmentedProjection := session.NormalizeProjectionSnapshot(record.Projection)
		var runtimeProjection data.ProjectionSnapshot
		if historyWindowOK {
			runtimeProjection = h.mergeActiveLiveProjection(record.Summary.ID, finalizeProjectionSnapshotForService(record.Summary.ID, runtimeSvc, augmentedProjection))
		} else {
			runtimeProjection = buildProjectionSnapshotForService(record.Summary.ID, runtimeSvc)
		}
		projection := runtimeProjection
		if isNativeMirrorSessionID(record.Summary.ID) {
			projection = mergeProjectionWithOptionalRuntime(augmentedProjection, runtimeProjection, runtimeSvc, record.Summary.ID)
		} else if preferAugmentedLogEntries(augmentedProjection.LogEntries, projection.LogEntries) {
			projection.LogEntries = augmentedProjection.LogEntries
		}
		record.Projection = projection
		record.Summary.Runtime = projection.Runtime
		if !historyWindowOK {
			updateProjectionMeta(record)
			cacheProjection(record.Summary.ID, record.Projection, false)
		}
		resumeMeta := protocol.MergeRuntimeMeta(protocol.RuntimeMeta{
			Command:          projection.Runtime.Command,
			Engine:           projection.Runtime.Engine,
			CodexSandboxMode: projection.Runtime.CodexSandboxMode,
			CWD:              projection.Runtime.CWD,
			PermissionMode:   projection.Runtime.PermissionMode,
			ResumeSessionID:  projection.Runtime.ResumeSessionID,
			ClaudeLifecycle:  projection.Runtime.ClaudeLifecycle,
		}, req.RuntimeMeta)
		runtimeSvc.SyncRuntimeMeta(resumeMeta)
		if updatedProjection, changed := projectionWithResumeRuntimeMeta(projection, resumeMeta); changed {
			projection = updatedProjection
			record.Projection = projection
			record.Summary.Runtime = projection.Runtime
			persistProjectionNow(record.Summary.ID, record.Projection)
			if !historyWindowOK {
				updateProjectionMeta(record)
				cacheProjection(record.Summary.ID, record.Projection, false)
			}
		}
		runtimeAlive := sessionRecordRuntimeAlive(record, runtimeSvc)
		if runtimeAlive && session.ShouldEmitResumeRecoveryStateEvent(runtimeSvc, projection, req.LastKnownRuntimeState) {
			recovery := session.BuildResumeRecoveryStateEvent(record.Summary.ID, runtimeSvc, projection, req.LastKnownRuntimeState)
			emit(recovery)
			if status, ok := session.AIStatusEventForBackendEvent(record.Summary.ID, runtimeSvc, projection, recovery); ok {
				emit(status)
			}
		}
		if historyWindowOK {
			historyWindow.Record = record
			historyWindow.Record.Projection.LogEntries = nil
			emit(session.SessionHistoryWindowEventFromWindowWithCursorAndPayloadLimit(historyWindow, runtimeAlive, deltaCursorSnapshot(sessionRuntime), gatewayRelaySafePayloadBudgetBytes))
		} else {
			emit(session.SessionHistoryWindowEventFromRecordWithCursorAndPayloadLimit(record, runtimeAlive, result.resumeHistoryLimit, deltaCursorSnapshot(sessionRuntime), gatewayRelaySafePayloadBudgetBytes))
		}
		emitContextWindowUsageIfAvailable(record.Summary.ID, runtimeSvc)
		logx.Info("ws", "session history emitted: sessionID=%s runtimeAlive=%v ownership=%s limit=%d", record.Summary.ID, runtimeAlive, record.Summary.Ownership, result.resumeHistoryLimit)
		restoredState := ""
		var restoredAgentEvent *protocol.AgentStateEvent
		if restored := restoredAgentStateEventFromRecord(record, runtimeAlive); restored != nil {
			restoredState = restored.State
			restoredAgentEvent = restored
		}
		replayedCount := 0
		latestCursor := int64(0)
		currentPermissionID := ""
		replayedCurrentPermission := false
		if sessionRuntime != nil {
			effectiveCursor := req.LastSeenEventCursor
			if persisted := sessionRuntime.persistedCursor.Load(); persisted > effectiveCursor {
				effectiveCursor = persisted
			}
			currentPermissionID = strings.TrimSpace(runtimeSvc.CurrentPermissionRequestID(record.Summary.ID))
			for _, pendingEvent := range sessionRuntime.pendingSince(effectiveCursor) {
				if currentPermissionID != "" {
					if prompt, ok := pendingEvent.(protocol.PromptRequestEvent); ok &&
						strings.TrimSpace(prompt.BlockingKind) == "permission" &&
						strings.TrimSpace(prompt.PermissionRequestID) == currentPermissionID {
						replayedCurrentPermission = true
					}
				}
				replayedCount++
				emit(pendingEvent)
			}
			latestCursor = sessionRuntime.latestCursor()
		}
		if currentPermissionID != "" && !replayedCurrentPermission {
			var prompt *protocol.PromptRequestEvent
			if sessionRuntime != nil {
				prompt = sessionRuntime.latestPendingPermissionPrompt(currentPermissionID)
			}
			if prompt == nil {
				prompt = session.RefreshedPermissionPromptEventWithID(record.Summary.ID, protocol.PermissionDecisionRequestEvent{
					PermissionMode:      projection.Runtime.PermissionMode,
					PermissionRequestID: currentPermissionID,
					ResumeSessionID:     firstNonEmptyString(projection.Runtime.ResumeSessionID, runtimeSvc.RuntimeSnapshot().ResumeSessionID),
					PromptMessage:       "当前操作需要你的授权",
					FallbackCommand:     projection.Runtime.Command,
					FallbackCWD:         projection.Runtime.CWD,
					FallbackEngine:      projection.Runtime.Engine,
				}, runtimeSvc, currentPermissionID)
			}
			if prompt != nil {
				logx.Info("ws", "session resume refreshed pending permission prompt: sessionID=%s requestID=%s", record.Summary.ID, currentPermissionID)
				emit(*prompt)
			}
		}
		if restoredAgentEvent != nil {
			emit(*restoredAgentEvent)
			if status, ok := session.AIStatusEventForBackendEvent(record.Summary.ID, runtimeSvc, record.Projection, *restoredAgentEvent); ok {
				emit(status)
			}
		}
		if snapshot := runtimeSvc.BuildTaskSnapshotEvent(record.Summary.ID, taskCursorSnapshot(sessionRuntime), "resume", true); snapshot != nil {
			emit(*snapshot)
			if status, ok := session.AIStatusEventForBackendEvent(record.Summary.ID, runtimeSvc, record.Projection, *snapshot); ok {
				emit(status)
			}
		}
		emit(protocol.NewSessionResumeResultEvent(
			record.Summary.ID,
			latestCursor,
			runtimeAlive,
			session.ResolvedResumeRuntimeState(restoredState, record, runtimeSvc),
			runtimeAlive,
			replayedCount,
			"session resumed",
		))
	}
	startAsyncMultiEventAction := func(action, actionKey, targetSessionID string, build func() ([]any, error)) {
		if _, exists := asyncEventInFlight[actionKey]; exists {
			emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("%s is already running for this session", action), ""))
			return
		}
		asyncEventInFlight[actionKey] = struct{}{}
		if !h.tryStartAsyncAction(context.WithoutCancel(ctx), gatewayAsyncActionOptions{
			Action:       action,
			SessionID:    actionKey,
			ConnectionID: connectionID,
			RemoteAddr:   remoteAddr,
			OnBusy: func() {
				message := fmt.Sprintf("%s is already running for this session", action)
				select {
				case asyncEventResultCh <- gatewayAsyncEventResult{actionKey: actionKey, action: action, sessionID: targetSessionID, err: errors.New(message)}:
				case <-ctx.Done():
				}
			},
			OnPanic: func(any, string) {
				select {
				case asyncEventResultCh <- gatewayAsyncEventResult{actionKey: actionKey, action: action, sessionID: targetSessionID, err: fmt.Errorf("internal server error")}:
				case <-ctx.Done():
				}
			},
			Run: func() {
				events, err := build()
				select {
				case asyncEventResultCh <- gatewayAsyncEventResult{actionKey: actionKey, action: action, sessionID: targetSessionID, events: events, err: err}:
				case <-ctx.Done():
				}
			},
		}) {
			delete(asyncEventInFlight, actionKey)
		}
	}
	startAsyncEventAction := func(action, actionKey, targetSessionID string, build func() (any, error)) {
		startAsyncMultiEventAction(action, actionKey, targetSessionID, func() ([]any, error) {
			event, err := build()
			if event == nil {
				return nil, err
			}
			return []any{event}, err
		})
	}
	startSessionDeltaGetAction = func(targetSessionID string, req protocol.SessionDeltaRequestEvent, actionKey string) {
		if _, exists := asyncEventInFlight[actionKey]; exists {
			sessionDeltaPending[actionKey] = gatewaySessionDeltaPendingRequest{
				targetSessionID: targetSessionID,
				req:             req,
			}
			logx.Info("ws", "coalesce in-flight session_delta_get: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s reason=%q", connectionID, selectedSessionID, targetSessionID, remoteAddr, req.Reason)
			return
		}
		asyncEventInFlight[actionKey] = struct{}{}
		go func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					stack := logx.StackTrace()
					logx.Error(
						"ws",
						"session_delta_get panic recovered: connectionID=%s sessionID=%s remoteAddr=%s panic=%v\n%s",
						connectionID,
						targetSessionID,
						remoteAddr,
						recovered,
						stack,
					)
					select {
					case asyncEventResultCh <- gatewayAsyncEventResult{actionKey: actionKey, action: "session_delta_get", sessionID: targetSessionID, err: fmt.Errorf("internal server error")}:
					case <-ctx.Done():
					}
				}
			}()
			event, err := buildSessionDeltaEvent(req, targetSessionID)
			select {
			case asyncEventResultCh <- gatewayAsyncEventResult{actionKey: actionKey, action: "session_delta_get", sessionID: targetSessionID, event: event, err: err}:
			case <-ctx.Done():
			}
		}()
	}

	emitAndPersistFor = func(sessionID string) func(any) {
		return h.runtimeEventSinkForSession(sessionID, emit)
	}

	defer func() {
		stopADBStream("")
		unregisterFreshnessSubscriber()
		cancel()
		h.forgetRelayE2EEConnection(connectionID)
		if strings.TrimSpace(selectedSessionID) != "" {
			h.runtimeSessions.Release(selectedSessionID, connectionID, false)
		} else if runtimeSvc != nil {
			runtimeSvc.Cleanup()
		}
		writerWG.Wait()
		logx.Info("ws", "connection closed: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
	}()

	emit(protocol.NewSessionStateEvent(selectedSessionID, string(session.StateActive), "connected"))
	emit(runtimeSvc.InitialEvent())
	if h.SessionStore != nil {
		emitSkillCatalogResult(emit, h.SessionStore, ctx, selectedSessionID)
		emitMemoryListResult(emit, h.SessionStore, ctx, selectedSessionID)
		startSessionList(sessionListFilterCWD)
		if strings.TrimSpace(selectedSessionID) != "" {
			emittedWindow, err := emitSessionHistoryForStoredSessionWindow(selectedSessionID, sessionResumeHistoryLimit, activeRuntimeSession)
			if err != nil {
				logx.Warn("ws", "initial session history window restore skipped: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
			}
			if !emittedWindow && err == nil {
				logx.Warn("ws", "initial session history window restore skipped: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, fmt.Errorf("session history window unavailable"))
			}
		}
	}

	for {
		var payload map[string]any
		var payloadBytes []byte
		var clientEvent protocol.ClientEvent
		select {
		case err := <-writeErrCh:
			if err != nil {
				logx.Error("ws", "writer terminated with error: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
			}
			return
		case result := <-sessionListResultCh:
			applySessionListResult(result)
			continue
		case result := <-sessionLoadResultCh:
			handleSessionLoadResult(result)
			continue
		case result := <-sessionResumeResultCh:
			handleSessionResumeResult(result)
			continue
		case result := <-asyncEventResultCh:
			handleAsyncEventResult(result)
			continue
		case result := <-readCh:
			if result.err != nil {
				logx.Info("ws", "connection read loop ended: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, result.err)
				return
			}
			payload = result.payload
			if payload == nil {
				continue
			}
			h.trackRelayE2EEConnection(connectionID, client)
			var err error
			payloadBytes, err = json.Marshal(payload)
			if err != nil {
				logx.Warn("ws", "invalid websocket json payload: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid json: %v", err), ""))
				continue
			}
			if err := json.Unmarshal(payloadBytes, &clientEvent); err != nil {
				logx.Warn("ws", "invalid websocket json payload: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid json: %v", err), ""))
				continue
			}
		case <-ctx.Done():
			return
		}

		switch clientEvent.Action {
		case "relay_device_register":
			var req protocol.RelayDeviceRegisterRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				logx.Warn("ws", "invalid relay_device_register request: connectionID=%s remoteAddr=%s err=%v", connectionID, remoteAddr, err)
				emit(protocol.NewErrorEventWithCode(selectedSessionID, fmt.Sprintf("invalid relay_device_register request: %v", err), "", "e2ee_handshake_failed"))
				continue
			}
			result, err := h.registerRelayDevice(client, req)
			if err != nil {
				logx.Warn("ws", "relay device registration failed: connectionID=%s remoteAddr=%s err=%v", connectionID, remoteAddr, err)
				emit(protocol.NewErrorEventWithCode(selectedSessionID, err.Error(), "", relayDeviceRegisterErrorCode(err)))
				continue
			}
			h.trackRelayE2EEConnection(connectionID, client)
			emit(result)
		case "relay_device_list":
			result, err := h.listRelayDevices(client)
			if err != nil {
				logx.Warn("ws", "relay device list failed: connectionID=%s remoteAddr=%s err=%v", connectionID, remoteAddr, err)
				emit(protocol.NewErrorEventWithCode(selectedSessionID, err.Error(), "", relayDeviceRegisterErrorCode(err)))
				continue
			}
			emit(result)
		case "relay_device_revoke":
			var req protocol.RelayDeviceRevokeRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				logx.Warn("ws", "invalid relay_device_revoke request: connectionID=%s remoteAddr=%s err=%v", connectionID, remoteAddr, err)
				emit(protocol.NewErrorEventWithCode(selectedSessionID, fmt.Sprintf("invalid relay_device_revoke request: %v", err), "", "e2ee_handshake_failed"))
				continue
			}
			result, err := h.revokeRelayDevice(client, req)
			if err != nil {
				logx.Warn("ws", "relay device revoke failed: connectionID=%s remoteAddr=%s err=%v", connectionID, remoteAddr, err)
				emit(protocol.NewErrorEventWithCode(selectedSessionID, err.Error(), "", relayDeviceRegisterErrorCode(err)))
				continue
			}
			emit(result)
			if strings.TrimSpace(req.DeviceID) != "" {
				if list, err := h.listRelayDevices(client); err == nil {
					emit(list)
				}
			}
		case "relay_device_rotate":
			result, err := h.rotateRelayDevices(client)
			if err != nil {
				logx.Warn("ws", "relay device rotate failed: connectionID=%s remoteAddr=%s err=%v", connectionID, remoteAddr, err)
				emit(protocol.NewErrorEventWithCode(selectedSessionID, err.Error(), "", relayDeviceRegisterErrorCode(err)))
				continue
			}
			if err := emitAndWait(result); err != nil {
				logx.Warn("ws", "relay device rotate result write failed: connectionID=%s remoteAddr=%s err=%v", connectionID, remoteAddr, err)
				return
			}
			h.closeAllRelayDeviceConnections()
		case "session_create":
			var req protocol.SessionCreateRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				logx.Warn("ws", "invalid session_create request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_create request: %v", err), ""))
				continue
			}
			logx.Info("ws", "incoming session_create: connectionID=%s sessionID=%s remoteAddr=%s title=%q cwd=%q reason=%q", connectionID, selectedSessionID, remoteAddr, req.Title, req.CWD, req.Reason)
			if strings.TrimSpace(req.Reason) == "auto_bind" && strings.TrimSpace(selectedSessionID) != "" {
				logx.Info("ws", "ignore auto_bind session_create because session already selected: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
				startSessionList(sessionListFilterCWD)
				continue
			}
			if h.SessionStore == nil {
				logx.Error("ws", "session store unavailable for session_create: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			created, err := h.SessionStore.CreateSession(ctx, req.Title)
			if err != nil {
				logx.Error("ws", "create session failed: connectionID=%s sessionID=%s remoteAddr=%s title=%q err=%v", connectionID, selectedSessionID, remoteAddr, req.Title, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			invalidateSessionListCacheForMutation()
			if cwd := normalizeSessionCWD(req.CWD); cwd != "" {
				record := data.SessionRecord{
					Summary: created,
					Projection: data.ProjectionSnapshot{
						Runtime: data.SessionRuntime{
							CWD:    cwd,
							Source: "mobilevc",
						},
					},
				}
				record.Summary.Runtime = session.MergeStoreSessionRuntime(record.Summary.Runtime, record.Projection.Runtime)
				summary, err := h.SessionStore.UpsertSession(ctx, record)
				if err != nil {
					logx.Error("ws", "update created session runtime failed: connectionID=%s sessionID=%s remoteAddr=%s createdSessionID=%s err=%v", connectionID, selectedSessionID, remoteAddr, created.ID, err)
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
				invalidateSessionListCacheForMutation()
				created = summary
				record.Summary = summary
				updateProjectionMeta(record)
			}
			switchRuntimeSession(created.ID, runtimeSwitchDetachPrevious)
			emit(protocol.NewSessionCreatedEvent(selectedSessionID, toProtocolSummary(created)))
			emit(protocol.NewSessionStateEvent(selectedSessionID, string(session.StateActive), "session selected"))
			startSessionList(sessionListFilterCWD)
		case "ping":
			pong := map[string]any{
				"type":      "pong",
				"sessionId": selectedSessionID,
				"ts":        time.Now().UTC().Format(time.RFC3339Nano),
			}
			if pingID := strings.TrimSpace(fmt.Sprint(payload["pingId"])); pingID != "" && pingID != "<nil>" {
				pong["pingId"] = pingID
			}
			emit(pong)
			if pendingSessionSwitchActive() {
				continue
			}
			if snapshot := runtimeSvc.BuildTaskSnapshotEvent(selectedSessionID, taskCursorSnapshot(activeRuntimeSession), "heartbeat", false); snapshot != nil {
				emit(*snapshot)
				if status, ok := session.AIStatusEventForBackendEvent(selectedSessionID, runtimeSvc, buildStatusProjectionSnapshotForService(selectedSessionID, runtimeSvc), *snapshot); ok {
					emit(status)
				}
			}
		case "task_snapshot_get":
			if pendingSessionSwitchActive() {
				logx.Info("ws", "ignore task_snapshot_get while session switch is pending: connectionID=%s sessionID=%s pendingSessionID=%s remoteAddr=%s", connectionID, selectedSessionID, pendingSessionSwitchTargetID, remoteAddr)
				continue
			}
			if snapshot := runtimeSvc.BuildTaskSnapshotEvent(selectedSessionID, taskCursorSnapshot(activeRuntimeSession), "sync", true); snapshot != nil {
				emit(*snapshot)
				if status, ok := session.AIStatusEventForBackendEvent(selectedSessionID, runtimeSvc, buildStatusProjectionSnapshotForService(selectedSessionID, runtimeSvc), *snapshot); ok {
					emit(status)
				}
			}
		case "session_list":
			var req protocol.SessionListRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				logx.Warn("ws", "invalid session_list request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_list request: %v", err), ""))
				continue
			}
			if h.SessionStore == nil {
				logx.Error("ws", "session store unavailable for session_list: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			startExplicitSessionList(req.CWD)
		case "session_load":
			var req protocol.SessionLoadRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				logx.Warn("ws", "invalid session_load request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_load request: %v", err), ""))
				continue
			}
			loadHistoryLimit := sessionHistoryWindowLimit(req.Limit)
			logx.Info("ws", "incoming session_load: connectionID=%s sessionID=%s remoteAddr=%s requestedSessionID=%s cwd=%q reason=%q limit=%d", connectionID, selectedSessionID, remoteAddr, req.SessionID, req.CWD, req.Reason, loadHistoryLimit)
			if strings.TrimSpace(req.Reason) == "auto_bind" && strings.TrimSpace(selectedSessionID) != "" {
				logx.Info("ws", "ignore auto_bind session_load because session already selected: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s", connectionID, selectedSessionID, req.SessionID, remoteAddr)
				startSessionList(sessionListFilterCWD)
				continue
			}
			if h.SessionStore == nil {
				logx.Error("ws", "session store unavailable for session_load: connectionID=%s sessionID=%s remoteAddr=%s requestedSessionID=%s", connectionID, selectedSessionID, remoteAddr, req.SessionID)
				emit(protocol.NewErrorEventWithCode(strings.TrimSpace(req.SessionID), "session store unavailable", "", "session_load_failed"))
				continue
			}
			trace := newSessionLoadTrace(connectionID, selectedSessionID, req.SessionID, remoteAddr, req.Reason)
			activeRuntimeLoad := activeRuntimeRecord(req.SessionID)
			trace.Step("active_runtime_check")
			loadReq := req
			loadSelectedSessionID := selectedSessionID
			loadActionKey := connectionID + ":" + strings.TrimSpace(loadReq.SessionID)
			if _, exists := sessionLoadInFlight[loadActionKey]; exists {
				emit(protocol.NewErrorEventWithCode(strings.TrimSpace(loadReq.SessionID), "session load is already running for this connection", "", "session_load_failed"))
				trace.Finish("", activeRuntimeLoad, projectionTraceMetrics{})
				continue
			}
			latestSessionSwitchSequence++
			loadSequence := latestSessionSwitchSequence
			pendingSessionSwitchTargetID = strings.TrimSpace(loadReq.SessionID)
			sessionLoadInFlight[loadActionKey] = struct{}{}
			if !h.tryStartAsyncAction(context.WithoutCancel(ctx), gatewayAsyncActionOptions{
				Action:       "session_load",
				SessionID:    loadActionKey,
				ConnectionID: connectionID,
				RemoteAddr:   remoteAddr,
				OnBusy: func() {
					delete(sessionLoadInFlight, loadActionKey)
					clearPendingSessionSwitch(loadReq.SessionID)
					emit(protocol.NewErrorEventWithCode(strings.TrimSpace(loadReq.SessionID), "session load is already running for this connection", "", "session_load_failed"))
					if trace != nil {
						trace.Finish("", activeRuntimeLoad, projectionTraceMetrics{})
					}
				},
				OnPanic: func(recovered any, stack string) {
					logx.Error("ws", "session_load panic: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s panic=%v\n%s", connectionID, loadSelectedSessionID, loadReq.SessionID, remoteAddr, recovered, stack)
					select {
					case sessionLoadResultCh <- gatewaySessionLoadResult{
						actionKey:         loadActionKey,
						sequence:          loadSequence,
						previousSessionID: loadSelectedSessionID,
						req:               loadReq,
						loadHistoryLimit:  loadHistoryLimit,
						trace:             trace,
						activeRuntimeLoad: activeRuntimeLoad,
						err:               fmt.Errorf("internal server error"),
					}:
					case <-ctx.Done():
					}
				},
				Run: func() {
					result := gatewaySessionLoadResult{
						actionKey:         loadActionKey,
						sequence:          loadSequence,
						previousSessionID: loadSelectedSessionID,
						req:               loadReq,
						loadHistoryLimit:  loadHistoryLimit,
						trace:             trace,
						activeRuntimeLoad: activeRuntimeLoad,
					}
					loadedWindow, err := loadUsableSessionHistoryWindow(loadReq.SessionID, 0, loadHistoryLimit, activeRuntimeLoad)
					if err != nil {
						result.err = err
					} else if loadedWindow.ok {
						result.record = loadedWindow.window.Record
						result.historyWindow = loadedWindow.window
						result.historyWindowOK = true
					}
					if result.record.Summary.ID == "" && result.err == nil {
						if !activeRuntimeLoad && !isNativeMirrorSessionID(loadReq.SessionID) {
							result.err = fmt.Errorf("session history window unavailable for session %s", strings.TrimSpace(loadReq.SessionID))
						} else {
							record, err := loadSessionRecordForRequest(loadReq.SessionID)
							result.record = record
							result.err = err
						}
					}
					select {
					case sessionLoadResultCh <- result:
					case <-ctx.Done():
					}
				},
			}) {
				delete(sessionLoadInFlight, loadActionKey)
				clearPendingSessionSwitch(loadReq.SessionID)
				continue
			}
		case "session_history_page":
			var req protocol.SessionHistoryPageRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				logx.Warn("ws", "invalid session_history_page request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_history_page request: %v", err), ""))
				continue
			}
			targetSessionID := strings.TrimSpace(req.SessionID)
			if targetSessionID == "" {
				targetSessionID = strings.TrimSpace(selectedSessionID)
			}
			if targetSessionID == "" {
				emit(protocol.NewErrorEvent(selectedSessionID, "session ID is required", ""))
				continue
			}
			if !canProcessSelectedSessionReadAction("session_history_page", targetSessionID) {
				continue
			}
			if h.SessionStore == nil {
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			pageHistoryLimit := sessionHistoryWindowLimit(req.Limit)
			reqCopy := req
			actionKey := connectionID + ":session_history_page:" + targetSessionID + ":" + fmt.Sprint(reqCopy.Before) + ":" + fmt.Sprint(reqCopy.Limit)
			startAsyncEventAction("session_history_page", actionKey, targetSessionID, func() (any, error) {
				if loadedWindow, err := loadUsableSessionHistoryWindow(targetSessionID, reqCopy.Before, pageHistoryLimit, activeRuntimeRecord(targetSessionID)); err != nil {
					return nil, err
				} else if loadedWindow.ok {
					historyWindow := loadedWindow.window
					record := historyWindow.Record
					_, pageRuntimeSvc := runtimeForSession(record.Summary.ID)
					freshProjection := session.NormalizeProjectionSnapshot(record.Projection)
					runtimeProjection := h.mergeActiveLiveProjection(record.Summary.ID, finalizeProjectionSnapshotForService(record.Summary.ID, pageRuntimeSvc, freshProjection))
					record.Projection = runtimeProjection
					record.Projection.LogEntries = nil
					record.Summary.Runtime = record.Projection.Runtime
					historyWindow.Record = record
					pageEvent := session.SessionHistoryPageEventFromWindowWithPayloadLimit(historyWindow, gatewayRelaySafePayloadBudgetBytes)
					logx.Info("ws", "session history page response: connectionID=%s sessionID=%s remoteAddr=%s before=%d limit=%d start=%d total=%d entries=%d windowStore=true", connectionID, record.Summary.ID, remoteAddr, reqCopy.Before, pageHistoryLimit, pageEvent.LogEntryStart, pageEvent.LogEntryTotal, len(pageEvent.LogEntries))
					return pageEvent, nil
				}
				return nil, fmt.Errorf("session history window unavailable for session %s", targetSessionID)
			})
		case "session_terminal_range_get":
			var req protocol.SessionTerminalRangeRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				logx.Warn("ws", "invalid session_terminal_range_get request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_terminal_range_get request: %v", err), ""))
				continue
			}
			if h.SessionStore == nil {
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			targetSessionID := strings.TrimSpace(req.SessionID)
			if targetSessionID == "" {
				targetSessionID = strings.TrimSpace(selectedSessionID)
			}
			if targetSessionID == "" {
				emit(protocol.NewErrorEvent(selectedSessionID, "session ID is required", ""))
				continue
			}
			if !canProcessSelectedSessionReadAction("session_terminal_range_get", targetSessionID) {
				continue
			}
			reqCopy := req
			actionKey := connectionID + ":session_terminal_range_get:" + targetSessionID + ":" + strings.TrimSpace(reqCopy.Stream) + ":" + fmt.Sprint(reqCopy.Start) + ":" + fmt.Sprint(reqCopy.Limit)
			startAsyncEventAction("session_terminal_range_get", actionKey, targetSessionID, func() (any, error) {
				rangeStore, ok := h.SessionStore.(data.SessionTerminalRangeStore)
				if !ok {
					return nil, fmt.Errorf("session terminal range store unavailable")
				}
				sessionRuntime, _ := runtimeForSession(targetSessionID)
				item, err := rangeStore.GetSessionTerminalRange(context.WithoutCancel(ctx), data.SessionTerminalRangeRequest{
					SessionID: targetSessionID,
					Stream:    reqCopy.Stream,
					Start:     reqCopy.Start,
					Limit:     reqCopy.Limit,
				})
				if err != nil {
					return nil, err
				}
				rangeEvent := session.SessionTerminalRangeEventFromRangeWithPayloadLimit(item, deltaCursorSnapshot(sessionRuntime), gatewayRelaySafePayloadBudgetBytes)
				logx.Info("ws", "session terminal range response: connectionID=%s sessionID=%s remoteAddr=%s stream=%s start=%d end=%d total=%d payloadLimited=%v lightweightStore=true", connectionID, item.Record.Summary.ID, remoteAddr, rangeEvent.Stream, rangeEvent.Start, rangeEvent.End, rangeEvent.Total, rangeEvent.PayloadLimited)
				return rangeEvent, nil
			})
		case "session_diff_page_get":
			var req protocol.SessionDiffPageRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				logx.Warn("ws", "invalid session_diff_page_get request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_diff_page_get request: %v", err), ""))
				continue
			}
			if h.SessionStore == nil {
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			targetSessionID := strings.TrimSpace(req.SessionID)
			if targetSessionID == "" {
				targetSessionID = strings.TrimSpace(selectedSessionID)
			}
			if targetSessionID == "" {
				emit(protocol.NewErrorEvent(selectedSessionID, "session ID is required", ""))
				continue
			}
			if !canProcessSelectedSessionReadAction("session_diff_page_get", targetSessionID) {
				continue
			}
			reqCopy := req
			actionKey := connectionID + ":session_diff_page_get:" + targetSessionID + ":" + fmt.Sprint(reqCopy.Before) + ":" + fmt.Sprint(reqCopy.Limit)
			startAsyncEventAction("session_diff_page_get", actionKey, targetSessionID, func() (any, error) {
				diffStore, ok := h.SessionStore.(data.SessionDiffPageStore)
				if !ok {
					return nil, fmt.Errorf("session diff page store unavailable")
				}
				sessionRuntime, _ := runtimeForSession(targetSessionID)
				page, err := diffStore.GetSessionDiffPage(context.WithoutCancel(ctx), data.SessionDiffPageRequest{
					SessionID: targetSessionID,
					Before:    reqCopy.Before,
					Limit:     reqCopy.Limit,
				})
				if err != nil {
					return nil, err
				}
				pageEvent := session.SessionDiffPageEventFromPageWithPayloadLimit(page, deltaCursorSnapshot(sessionRuntime), gatewayRelaySafePayloadBudgetBytes)
				logx.Info("ws", "session diff page response: connectionID=%s sessionID=%s remoteAddr=%s before=%d limit=%d start=%d total=%d diffs=%d payloadLimited=%v lightweightStore=true", connectionID, page.Record.Summary.ID, remoteAddr, reqCopy.Before, reqCopy.Limit, pageEvent.DiffStart, pageEvent.DiffTotal, len(pageEvent.Diffs), pageEvent.PayloadLimited)
				return pageEvent, nil
			})
		case "session_terminal_execution_page_get":
			var req protocol.SessionTerminalExecutionPageRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				logx.Warn("ws", "invalid session_terminal_execution_page_get request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_terminal_execution_page_get request: %v", err), ""))
				continue
			}
			if h.SessionStore == nil {
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			targetSessionID := strings.TrimSpace(req.SessionID)
			if targetSessionID == "" {
				targetSessionID = strings.TrimSpace(selectedSessionID)
			}
			if targetSessionID == "" {
				emit(protocol.NewErrorEvent(selectedSessionID, "session ID is required", ""))
				continue
			}
			if !canProcessSelectedSessionReadAction("session_terminal_execution_page_get", targetSessionID) {
				continue
			}
			reqCopy := req
			actionKey := connectionID + ":session_terminal_execution_page_get:" + targetSessionID + ":" + fmt.Sprint(reqCopy.Before) + ":" + fmt.Sprint(reqCopy.Limit) + ":" + fmt.Sprint(reqCopy.IncludeOutput)
			startAsyncEventAction("session_terminal_execution_page_get", actionKey, targetSessionID, func() (any, error) {
				executionStore, ok := h.SessionStore.(data.SessionTerminalExecutionPageStore)
				if !ok {
					return nil, fmt.Errorf("session terminal execution page store unavailable")
				}
				sessionRuntime, _ := runtimeForSession(targetSessionID)
				page, err := executionStore.GetSessionTerminalExecutionPage(context.WithoutCancel(ctx), data.SessionTerminalExecutionPageRequest{
					SessionID:     targetSessionID,
					Before:        reqCopy.Before,
					Limit:         reqCopy.Limit,
					IncludeOutput: reqCopy.IncludeOutput,
				})
				if err != nil {
					return nil, err
				}
				pageEvent := session.SessionTerminalExecutionPageEventFromPageWithPayloadLimit(page, deltaCursorSnapshot(sessionRuntime), gatewayRelaySafePayloadBudgetBytes)
				logx.Info("ws", "session terminal execution page response: connectionID=%s sessionID=%s remoteAddr=%s before=%d limit=%d start=%d total=%d executions=%d includeOutput=%v payloadLimited=%v lightweightStore=true", connectionID, page.Record.Summary.ID, remoteAddr, reqCopy.Before, reqCopy.Limit, pageEvent.ExecutionStart, pageEvent.ExecutionTotal, len(pageEvent.TerminalExecutions), pageEvent.IncludeOutput, pageEvent.PayloadLimited)
				return pageEvent, nil
			})
		case "register_push_token":
			var req protocol.RegisterPushTokenRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				logx.Warn("ws", "invalid register_push_token request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid register_push_token request: %v", err), ""))
				continue
			}
			targetSessionID := strings.TrimSpace(req.SessionID)
			if targetSessionID == "" {
				targetSessionID = strings.TrimSpace(selectedSessionID)
			}
			token := strings.TrimSpace(req.Token)
			platform := strings.TrimSpace(req.Platform)
			logx.Info("ws", "register push token: connectionID=%s sessionID=%s remoteAddr=%s requestedSessionID=%s resolvedSessionID=%s platform=%s tokenPresent=%v", connectionID, selectedSessionID, remoteAddr, req.SessionID, targetSessionID, platform, token != "")
			h.handleRegisterPushToken(ctx, targetSessionID, token, platform, emit)
		case "session_delta_get":
			var req protocol.SessionDeltaRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				logx.Warn("ws", "invalid session_delta_get request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_delta_get request: %v", err), ""))
				continue
			}
			if h.SessionStore == nil {
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			targetSessionID := strings.TrimSpace(req.SessionID)
			if targetSessionID == "" {
				targetSessionID = strings.TrimSpace(selectedSessionID)
			}
			if targetSessionID == "" {
				emit(protocol.NewErrorEvent(selectedSessionID, "session ID is required", ""))
				continue
			}
			if pendingSessionSwitchActive() && !targetIsPendingSessionSwitch(targetSessionID) {
				logx.Info("ws", "ignore session delta while session switch is pending: connectionID=%s sessionID=%s pendingSessionID=%s requestedSessionID=%s remoteAddr=%s reason=%q", connectionID, selectedSessionID, pendingSessionSwitchTargetID, targetSessionID, remoteAddr, req.Reason)
				continue
			}
			if targetSessionID != strings.TrimSpace(selectedSessionID) && !targetIsPendingSessionSwitch(targetSessionID) {
				logx.Info("ws", "ignore session delta for non-selected target: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s reason=%q", connectionID, selectedSessionID, targetSessionID, remoteAddr, req.Reason)
				emit(protocol.NewSessionDeltaEvent(
					targetSessionID,
					protocol.SessionSummary{ID: targetSessionID},
					req.Known,
					req.Known,
					nil,
					nil,
					nil,
					nil,
					nil,
					nil,
					nil,
					nil,
					nil,
					protocol.ContextWindowUsage{},
					protocol.SessionContext{},
					protocol.CatalogMetadata{},
					protocol.CatalogMetadata{},
					false,
					false,
					protocol.RuntimeMeta{},
					false,
				))
				continue
			}
			reqCopy := req
			actionKey := connectionID + ":session_delta_get:" + targetSessionID
			startSessionDeltaGetAction(targetSessionID, reqCopy, actionKey)
		case "session_resume":
			var req protocol.SessionResumeRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				logx.Warn("ws", "invalid session_resume request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_resume request: %v", err), ""))
				continue
			}
			resumeHistoryLimit := sessionHistoryWindowLimit(req.Limit)
			logx.Info("ws", "incoming session_resume: connectionID=%s sessionID=%s remoteAddr=%s requestedSessionID=%s cwd=%q reason=%q lastSeenEventCursor=%d lastKnownRuntimeState=%q limit=%d", connectionID, selectedSessionID, remoteAddr, req.SessionID, req.CWD, req.Reason, req.LastSeenEventCursor, req.LastKnownRuntimeState, resumeHistoryLimit)
			if h.SessionStore == nil {
				logx.Error("ws", "session store unavailable for session_resume: connectionID=%s sessionID=%s remoteAddr=%s requestedSessionID=%s", connectionID, selectedSessionID, remoteAddr, req.SessionID)
				emit(protocol.NewErrorEventWithCode(strings.TrimSpace(firstNonEmptyString(req.SessionID, selectedSessionID)), "session store unavailable", "", "session_resume_failed"))
				continue
			}
			resumeReq := req
			resumeTargetSessionID := strings.TrimSpace(resumeReq.SessionID)
			if resumeTargetSessionID == "" {
				resumeTargetSessionID = strings.TrimSpace(selectedSessionID)
			}
			if resumeTargetSessionID == "" {
				emit(protocol.NewErrorEvent(selectedSessionID, "session ID is required", ""))
				continue
			}
			resumeReq.SessionID = resumeTargetSessionID
			resumeActionKey := connectionID + ":session_resume:" + resumeTargetSessionID
			if _, exists := sessionResumeInFlight[resumeActionKey]; exists {
				emit(protocol.NewErrorEventWithCode(resumeTargetSessionID, "session resume is already running for this connection", "", "session_resume_failed"))
				continue
			}
			latestSessionSwitchSequence++
			resumeSequence := latestSessionSwitchSequence
			resumePreviousSessionID := strings.TrimSpace(selectedSessionID)
			pendingSessionSwitchTargetID = resumeTargetSessionID
			sessionResumeInFlight[resumeActionKey] = struct{}{}
			activeRuntimeResume := activeRuntimeRecord(resumeTargetSessionID)
			if !h.tryStartAsyncAction(context.WithoutCancel(ctx), gatewayAsyncActionOptions{
				Action:       "session_resume",
				SessionID:    resumeActionKey,
				ConnectionID: connectionID,
				RemoteAddr:   remoteAddr,
				OnBusy: func() {
					message := "session resume is already running for this connection"
					select {
					case sessionResumeResultCh <- gatewaySessionResumeResult{actionKey: resumeActionKey, sequence: resumeSequence, previousSessionID: resumePreviousSessionID, req: resumeReq, resumeHistoryLimit: resumeHistoryLimit, activeRuntimeResume: activeRuntimeResume, err: errors.New(message)}:
					case <-ctx.Done():
					}
				},
				OnPanic: func(recovered any, stack string) {
					logx.Error("ws", "session_resume panic: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s panic=%v\n%s", connectionID, selectedSessionID, resumeReq.SessionID, remoteAddr, recovered, stack)
					select {
					case sessionResumeResultCh <- gatewaySessionResumeResult{actionKey: resumeActionKey, sequence: resumeSequence, previousSessionID: resumePreviousSessionID, req: resumeReq, resumeHistoryLimit: resumeHistoryLimit, activeRuntimeResume: activeRuntimeResume, err: fmt.Errorf("internal server error")}:
					case <-ctx.Done():
					}
				},
				Run: func() {
					result := gatewaySessionResumeResult{
						actionKey:           resumeActionKey,
						sequence:            resumeSequence,
						previousSessionID:   resumePreviousSessionID,
						req:                 resumeReq,
						resumeHistoryLimit:  resumeHistoryLimit,
						activeRuntimeResume: activeRuntimeResume,
					}
					loadedWindow, err := loadUsableSessionHistoryWindow(resumeTargetSessionID, 0, resumeHistoryLimit, activeRuntimeResume)
					if err != nil {
						result.err = err
					} else if loadedWindow.ok {
						result.record = loadedWindow.window.Record
						result.historyWindow = loadedWindow.window
						result.historyWindowOK = true
					}
					if result.record.Summary.ID == "" && result.err == nil {
						if !activeRuntimeResume && !isNativeMirrorSessionID(resumeTargetSessionID) {
							result.err = fmt.Errorf("session history window unavailable for session %s", resumeTargetSessionID)
						} else {
							record, err := loadSessionRecordForRequest(resumeTargetSessionID)
							result.record = record
							result.err = err
						}
					}
					select {
					case sessionResumeResultCh <- result:
					case <-ctx.Done():
					}
				},
			}) {
				delete(sessionResumeInFlight, resumeActionKey)
				clearPendingSessionSwitch(resumeTargetSessionID)
				continue
			}
		case "session_delete":
			var req protocol.SessionDeleteRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				logx.Warn("ws", "invalid session_delete request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_delete request: %v", err), ""))
				continue
			}
			if h.SessionStore == nil {
				logx.Error("ws", "session store unavailable for session_delete: connectionID=%s sessionID=%s remoteAddr=%s requestedSessionID=%s", connectionID, selectedSessionID, remoteAddr, req.SessionID)
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			deletedSessionID := strings.TrimSpace(req.SessionID)
			if deletedSessionID == "" {
				emit(protocol.NewErrorEventWithCode(selectedSessionID, "session ID is required", "", "session_delete_failed"))
				continue
			}
			summary, err := lookupSessionSummaryForDelete(context.WithoutCancel(ctx), h.SessionStore, deletedSessionID)
			if err != nil {
				logx.Warn("ws", "delete session lookup failed: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, deletedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEventWithCode(selectedSessionID, err.Error(), "", "session_delete_failed"))
				continue
			}
			if isExternalNativeSessionSummary(summary) {
				emit(protocol.NewErrorEventWithCode(selectedSessionID, "Codex 原生会话仅支持恢复，不支持在 MobileVC 内删除", "", "session_delete_failed"))
				continue
			}
			deletingCurrent := deletedSessionID == strings.TrimSpace(selectedSessionID)
			if err := h.SessionStore.DeleteSession(ctx, deletedSessionID); err != nil {
				logx.Warn("ws", "delete session failed: connectionID=%s sessionID=%s requestedSessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, deletedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEventWithCode(selectedSessionID, err.Error(), "", "session_delete_failed"))
				continue
			}
			h.runtimeSessions.CleanupSession(deletedSessionID)
			invalidateSessionListCacheForMutation()
			if !deletingCurrent {
				startSessionList(sessionListFilterCWD)
				continue
			}
			startSessionListWithAfter(sessionListFilterCWD, func(result gatewaySessionListResult) {
				if result.err != nil {
					return
				}
				fallbackSessionID := ""
				for _, item := range result.items {
					if isExternalNativeSessionSummary(item) {
						continue
					}
					itemID := strings.TrimSpace(item.ID)
					if itemID != "" && itemID != deletedSessionID {
						fallbackSessionID = itemID
						break
					}
				}
				switchRuntimeSession(fallbackSessionID, runtimeSwitchDetachPrevious)
				if fallbackSessionID == "" {
					emitEmptySessionState()
					return
				}
				if ok, err := emitSessionHistoryForStoredSessionWindow(fallbackSessionID, sessionResumeHistoryLimit, activeRuntimeSession); err != nil {
					logx.Warn("ws", "load fallback session window after delete failed: connectionID=%s sessionID=%s fallbackSessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, fallbackSessionID, remoteAddr, err)
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					return
				} else if ok {
					emit(protocol.NewSessionStateEvent(selectedSessionID, string(session.StateActive), "history loaded"))
					return
				}
				windowErr := fmt.Errorf("session history window unavailable for fallback session %s", fallbackSessionID)
				logx.Warn("ws", "load fallback session window after delete unavailable: connectionID=%s sessionID=%s fallbackSessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, fallbackSessionID, remoteAddr, windowErr)
				emit(protocol.NewErrorEvent(selectedSessionID, windowErr.Error(), ""))
			})
		case "session_context_get":
			targetSessionID := strings.TrimSpace(selectedSessionID)
			if targetSessionID == "" {
				emit(protocol.NewSessionContextResultEvent(targetSessionID, toProtocolSessionContext(data.SessionContext{})))
				continue
			}
			if !canProcessSelectedSessionReadAction("session_context_get", targetSessionID) {
				continue
			}
			actionKey := connectionID + ":session_context_get:" + targetSessionID
			startAsyncEventAction("session_context_get", actionKey, targetSessionID, func() (any, error) {
				sessionContext, err := readSessionContextForAction("session_context_get", targetSessionID)
				if err != nil {
					return nil, err
				}
				return protocol.NewSessionContextResultEvent(targetSessionID, toProtocolSessionContext(sessionContext)), nil
			})
		case "permission_rule_list":
			targetSessionID := strings.TrimSpace(selectedSessionID)
			if targetSessionID != "" && !canProcessSelectedSessionReadAction("permission_rule_list", targetSessionID) {
				continue
			}
			actionKey := connectionID + ":permission_rule_list:" + targetSessionID
			startAsyncMultiEventAction("permission_rule_list", actionKey, targetSessionID, func() ([]any, error) {
				return buildPermissionRuleListEvents(h.SessionStore, context.WithoutCancel(ctx), targetSessionID)
			})
		case "permission_rule_upsert":
			var req protocol.PermissionRuleRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid permission_rule_upsert request: %v", err), ""))
				continue
			}
			rule := fromProtocolPermissionRule(req.Rule)
			if strings.TrimSpace(rule.ID) == "" {
				emit(protocol.NewErrorEvent(selectedSessionID, "permission rule id is required", ""))
				continue
			}
			switch rule.Scope {
			case data.PermissionScopePersistent:
				snapshot, err := h.SessionStore.GetPermissionRuleSnapshot(ctx)
				if err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
				snapshot.Items = upsertPermissionRule(snapshot.Items, rule)
				if err := h.SessionStore.SavePermissionRuleSnapshot(ctx, snapshot); err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
			default:
				if err := saveSessionPermissionRules(ctx, h.SessionStore, selectedSessionID, func(snapshot data.SessionPermissionRuleSnapshot) data.SessionPermissionRuleSnapshot {
					snapshot.Items = upsertPermissionRule(snapshot.Items, rule)
					return snapshot
				}); err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
				invalidateSessionListCacheForMutation()
			}
			emitPermissionRuleList(emit, h.SessionStore, ctx, selectedSessionID)
		case "permission_rule_delete":
			var req protocol.PermissionRuleDeleteRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid permission_rule_delete request: %v", err), ""))
				continue
			}
			switch data.PermissionScope(strings.TrimSpace(req.Scope)) {
			case data.PermissionScopePersistent:
				snapshot, err := h.SessionStore.GetPermissionRuleSnapshot(ctx)
				if err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
				snapshot.Items = deletePermissionRule(snapshot.Items, strings.TrimSpace(req.ID))
				if err := h.SessionStore.SavePermissionRuleSnapshot(ctx, snapshot); err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
			default:
				ruleID := strings.TrimSpace(req.ID)
				if err := saveSessionPermissionRules(ctx, h.SessionStore, selectedSessionID, func(snapshot data.SessionPermissionRuleSnapshot) data.SessionPermissionRuleSnapshot {
					snapshot.Items = deletePermissionRule(snapshot.Items, ruleID)
					return snapshot
				}); err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
				invalidateSessionListCacheForMutation()
			}
			emitPermissionRuleList(emit, h.SessionStore, ctx, selectedSessionID)
		case "permission_rules_set_enabled":
			var req protocol.PermissionRuleToggleRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid permission_rules_set_enabled request: %v", err), ""))
				continue
			}
			switch data.PermissionScope(strings.TrimSpace(req.Scope)) {
			case data.PermissionScopePersistent:
				snapshot, err := h.SessionStore.GetPermissionRuleSnapshot(ctx)
				if err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
				snapshot.Enabled = req.Enabled
				if err := h.SessionStore.SavePermissionRuleSnapshot(ctx, snapshot); err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
			default:
				if err := saveSessionPermissionRules(ctx, h.SessionStore, selectedSessionID, func(snapshot data.SessionPermissionRuleSnapshot) data.SessionPermissionRuleSnapshot {
					snapshot.Enabled = req.Enabled
					return snapshot
				}); err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
				invalidateSessionListCacheForMutation()
			}
			emitPermissionRuleList(emit, h.SessionStore, ctx, selectedSessionID)
		case "review_state_get":
			emit(protocol.NewErrorEventWithCode(selectedSessionID, "review_state_get is deprecated; use session_diff_page_get", "", "review_state_get_deprecated"))
		case "session_context_update":
			var req protocol.SessionContextUpdateRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				logx.Warn("ws", "invalid session_context_update request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid session_context_update request: %v", err), ""))
				continue
			}
			if strings.TrimSpace(selectedSessionID) == "" {
				emit(protocol.NewErrorEvent(selectedSessionID, "请先创建或加载会话后再更新 session context", ""))
				continue
			}
			sessionContext, err := saveSelectedSessionContext(selectedSessionID, data.SessionContext{
				EnabledSkillNames: req.EnabledSkillNames,
				EnabledMemoryIDs:  req.EnabledMemoryIDs,
				Configured:        true,
			})
			if err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			invalidateSessionListCacheForMutation()
			emit(protocol.NewSessionContextResultEvent(selectedSessionID, toProtocolSessionContext(sessionContext)))
		case "skill_catalog_get":
			emitSkillCatalogResult(emit, h.SessionStore, ctx, selectedSessionID)
		case "skill_catalog_upsert":
			var req protocol.SkillCatalogRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				logx.Warn("ws", "invalid skill_catalog_upsert request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid skill_catalog_upsert request: %v", err), ""))
				continue
			}
			if err := upsertLocalSkill(h.SessionStore, ctx, req.Skill); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			h.replaceSkillLauncher(skills.NewLauncher(h.SessionStore))
			emitSkillCatalogResult(emit, h.SessionStore, ctx, selectedSessionID)
		case "skill_sync_pull":
			if h.SessionStore == nil {
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			sessionID := strings.TrimSpace(selectedSessionID)
			if !ackClientAction(sessionID, "skill_sync_pull", clientEvent) {
				logx.Info("ws", "duplicate client action ignored: connectionID=%s sessionID=%s remoteAddr=%s action=skill_sync_pull clientActionID=%s", connectionID, sessionID, remoteAddr, clientEvent.ClientActionID)
				continue
			}
			workCtx := context.WithoutCancel(ctx)
			if !h.tryStartAsyncAction(workCtx, gatewayAsyncActionOptions{
				Action:       "skill_sync_pull",
				SessionID:    catalogAsyncScopeKey(),
				ConnectionID: connectionID,
				RemoteAddr:   remoteAddr,
				OnBusy: func() {
					message := "skill catalog sync is already running"
					emit(protocol.NewCatalogSyncResultEvent(sessionID, string(data.CatalogDomainSkill), false, message, protocol.CatalogMetadata{}))
					emit(protocol.NewErrorEvent(sessionID, message, ""))
				},
				OnPanic: func(any, string) {
					message := "internal server error"
					emit(protocol.NewCatalogSyncResultEvent(sessionID, string(data.CatalogDomainSkill), false, message, protocol.CatalogMetadata{}))
					emit(protocol.NewErrorEvent(sessionID, message, ""))
				},
				Run: func() {
					h.runSkillSyncPull(workCtx, sessionID, emit)
				},
			}) {
				continue
			}
		case "memory_list":
			emitMemoryListResult(emit, h.SessionStore, ctx, selectedSessionID)
		case "memory_sync_pull":
			var req protocol.MemoryRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				logx.Warn("ws", "invalid memory_sync_pull request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid memory_sync_pull request: %v", err), ""))
				continue
			}
			if h.SessionStore == nil {
				emit(protocol.NewErrorEvent(selectedSessionID, "session store unavailable", ""))
				continue
			}
			sessionID := strings.TrimSpace(selectedSessionID)
			if !ackClientAction(sessionID, "memory_sync_pull", req.ClientEvent) {
				logx.Info("ws", "duplicate client action ignored: connectionID=%s sessionID=%s remoteAddr=%s action=memory_sync_pull clientActionID=%s", connectionID, sessionID, remoteAddr, req.ClientActionID)
				continue
			}
			syncCWD := resolveCatalogSyncCWD(h.SessionStore, ctx, sessionID, firstNonEmptyString(req.CWD, sessionListFilterCWD))
			workCtx := context.WithoutCancel(ctx)
			if !h.tryStartAsyncAction(workCtx, gatewayAsyncActionOptions{
				Action:       "memory_sync_pull",
				SessionID:    catalogAsyncScopeKey(),
				ConnectionID: connectionID,
				RemoteAddr:   remoteAddr,
				OnBusy: func() {
					message := "memory catalog sync is already running"
					emit(protocol.NewCatalogSyncResultEvent(sessionID, string(data.CatalogDomainMemory), false, message, protocol.CatalogMetadata{}))
					emit(protocol.NewErrorEvent(sessionID, message, ""))
				},
				OnPanic: func(any, string) {
					message := "internal server error"
					emit(protocol.NewCatalogSyncResultEvent(sessionID, string(data.CatalogDomainMemory), false, message, protocol.CatalogMetadata{}))
					emit(protocol.NewErrorEvent(sessionID, message, ""))
				},
				Run: func() {
					h.runMemorySyncPull(workCtx, sessionID, syncCWD, connectionID, remoteAddr, emit)
				},
			}) {
				continue
			}
		case "memory_upsert":
			var req protocol.MemoryRequestEvent
			if err := json.Unmarshal(payloadBytes, &req); err != nil {
				logx.Warn("ws", "invalid memory_upsert request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid memory_upsert request: %v", err), ""))
				continue
			}
			syncCWD := resolveCatalogSyncCWD(h.SessionStore, ctx, selectedSessionID, firstNonEmptyString(req.CWD, sessionListFilterCWD))
			if err := upsertMemoryItem(h.SessionStore, ctx, req.Item, syncCWD); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			emitMemoryListResult(emit, h.SessionStore, ctx, selectedSessionID)
		case "ai_turn":
			var aiReq protocol.AITurnRequestEvent
			if err := json.Unmarshal(payloadBytes, &aiReq); err != nil {
				logx.Warn("ws", "invalid ai_turn request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid ai_turn request: %v", err), ""))
				continue
			}
			logx.Info("ws", "incoming action: connectionID=%s sessionID=%s remoteAddr=%s action=ai_turn engine=%q permissionMode=%q cwd=%q dataPreview=%q", connectionID, selectedSessionID, remoteAddr, aiReq.Engine, aiReq.PermissionMode, aiReq.CWD, wsDebugPreview(aiReq.Data))
			sessionID := strings.TrimSpace(firstNonEmptyString(aiReq.SessionID, selectedSessionID))
			if sessionID == "" || sessionID == connectionID {
				emit(protocol.NewErrorEvent(selectedSessionID, "请先创建或加载会话后再发送命令", ""))
				continue
			}
			if sessionID != strings.TrimSpace(selectedSessionID) {
				switchRuntimeSession(sessionID, runtimeSwitchDetachPrevious)
			}
			if !ackClientAction(sessionID, "ai_turn", aiReq.ClientEvent) {
				logx.Info("ws", "duplicate client action ignored: connectionID=%s sessionID=%s remoteAddr=%s action=ai_turn clientActionID=%s", connectionID, sessionID, remoteAddr, aiReq.ClientActionID)
				continue
			}
			sessionRuntime, service := runtimeForSession(sessionID)
			emitAndPersist := emitAndPersistFor(sessionID)
			if sessionRuntime != nil {
				service.SetSink(sessionRuntime.EnsureBufferedSinkWithProcessor(emitAndPersist))
			}
			controller := service.ControllerSnapshot()
			snapshot := service.RuntimeSnapshot()
			projection := loadFullProjectionForInteractiveMutation(clientEvent.Action, sessionID)
			engineName := strings.TrimSpace(strings.ToLower(firstNonEmptyString(
				aiReq.Engine,
				aiReq.RuntimeMeta.Engine,
				snapshot.ActiveMeta.Engine,
				controller.ActiveMeta.Engine,
				projection.Runtime.Engine,
			)))
			if engineName == "" {
				engineName = "claude"
			}
			if strings.TrimSpace(aiReq.PermissionMode) != "" {
				service.UpdatePermissionModeForEngine(aiReq.PermissionMode, engineName)
				controller = service.ControllerSnapshot()
				snapshot = service.RuntimeSnapshot()
				projection = loadFullProjectionForInteractiveMutation(clientEvent.Action, sessionID)
			}
			command := strings.TrimSpace(firstNonEmptyString(
				aiReq.RuntimeMeta.Command,
				snapshot.ActiveMeta.Command,
				controller.CurrentCommand,
				controller.ActiveMeta.Command,
				projection.Runtime.Command,
				defaultAICommandFromEngine(engineName),
			))
			commandEngine := strings.TrimSpace(strings.ToLower(commandHead(command)))
			if !isAISessionCommandLike(command) || (commandEngine != "" && engineName != "" && commandEngine != engineName) {
				command = defaultAICommandFromEngine(engineName)
			}
			command = applyAICommandPreferences(command, engineName, aiReq.RuntimeMeta.Model, aiReq.RuntimeMeta.ReasoningEffort)
			cwd := firstNonEmptyString(
				aiReq.CWD,
				aiReq.RuntimeMeta.CWD,
				snapshot.ActiveMeta.CWD,
				controller.ActiveMeta.CWD,
				projection.Runtime.CWD,
				"/",
			)
			permissionMode := session.NormalizePermissionModeForEngine(firstNonEmptyString(
				aiReq.PermissionMode,
				snapshot.ActiveMeta.PermissionMode,
				controller.ActiveMeta.PermissionMode,
				projection.Runtime.PermissionMode,
			), engineName)
			codexSandboxMode := ""
			if engineName == "codex" {
				codexSandboxMode = firstNonEmptyString(
					aiReq.RuntimeMeta.CodexSandboxMode,
					snapshot.ActiveMeta.CodexSandboxMode,
					controller.ActiveMeta.CodexSandboxMode,
					projection.Runtime.CodexSandboxMode,
				)
			}
			visibleUserText := strings.TrimRight(aiReq.Data, "\n")
			inputData := aiReq.Data
			attachments, err := persistImageAttachments(ctx, sessionID, aiReq.ImageAttachments)
			if err != nil {
				logx.Warn("ws", "persist ai_turn image attachments failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
				continue
			}
			inputData = appendAttachmentPathPrompt(inputData, attachmentPaths(attachments))
			if strings.TrimSpace(inputData) != "" {
				service.RecordUserInput(inputData)
				skillPrefix, err := skills.BuildEnabledSkillsPrefix(h.SessionStore, projection.SessionContext)
				if err != nil {
					logx.Warn("ws", "build enabled skills prefix failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
				}
				memoryPrefix, memoryErr := skills.BuildEnabledMemoryPrefix(h.SessionStore, projection.SessionContext)
				if memoryErr != nil {
					logx.Warn("ws", "build enabled memory prefix failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, memoryErr)
				}
				inputData = skills.InjectConversationPrefixes(inputData, skillPrefix, memoryPrefix)
			}
			reqMeta := protocol.MergeRuntimeMeta(aiReq.RuntimeMeta, protocol.RuntimeMeta{
				Source:            fallback(aiReq.Source, "ai_turn"),
				Command:           command,
				Engine:            engineName,
				CodexSandboxMode:  codexSandboxMode,
				CWD:               cwd,
				PermissionMode:    permissionMode,
				ClaudeSessionUUID: lookupClaudeSessionUUID(sessionID),
			})
			execReq := session.ExecuteRequest{
				Command:        command,
				CWD:            cwd,
				Mode:           engine.ModePTY,
				PermissionMode: permissionMode,
				RuntimeMeta:    reqMeta,
			}
			runnerInputText := strings.TrimRight(inputData, "\n")
			if runnerInputText == "" {
				if projectionWithUser, ok := appendUserProjectionEntry(projection, sessionID, command, "命令", connectionID, remoteAddr, attachments); ok {
					persistProjectionNow(sessionID, projectionWithUser)
				}
				logx.Info("ws", "dispatch ai_turn execute: connectionID=%s sessionID=%s remoteAddr=%s command=%q cwd=%q permissionMode=%q", connectionID, sessionID, remoteAddr, command, cwd, permissionMode)
				if err := service.Execute(ctx, sessionID, execReq, emitAndPersist); err != nil {
					logx.Error("ws", "ai_turn execute failed: connectionID=%s sessionID=%s remoteAddr=%s command=%q err=%v", connectionID, sessionID, remoteAddr, command, err)
					emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
				}
				continue
			}
			inputReq := session.InputRequest{
				Data: inputData,
				RuntimeMeta: protocol.RuntimeMeta{
					Source:           "ai_turn",
					PermissionMode:   permissionMode,
					CodexSandboxMode: reqMeta.CodexSandboxMode,
				},
			}
			logx.Info("ws", "dispatch ai_turn input/resume: connectionID=%s sessionID=%s remoteAddr=%s command=%q cwd=%q permissionMode=%q preview=%q", connectionID, sessionID, remoteAddr, command, cwd, permissionMode, wsDebugPreview(inputData))
			if service.ShouldEmitTransientResumeThinkingEvent(execReq) {
				emit(protocol.ApplyRuntimeMeta(
					protocol.NewAgentStateEvent(
						sessionID,
						string(session.ControllerStateThinking),
						"恢复会话中",
						false,
						execReq.Command,
						projection.Controller.LastStep,
						projection.Controller.LastTool,
					),
					protocol.MergeRuntimeMeta(projection.Controller.ActiveMeta, protocol.RuntimeMeta{
						Source:           "ai_turn",
						ResumeSessionID:  execReq.RuntimeMeta.ResumeSessionID,
						Command:          execReq.Command,
						Engine:           firstNonEmptyString(execReq.RuntimeMeta.Engine, projection.Runtime.Engine),
						CodexSandboxMode: execReq.RuntimeMeta.CodexSandboxMode,
						CWD:              execReq.CWD,
						PermissionMode:   execReq.PermissionMode,
						ClaudeLifecycle:  "active",
					}),
				))
			}
			err = service.SendInputOrResume(ctx, sessionID, execReq, inputReq, emitAndPersist)
			if err != nil {
				if errors.Is(err, session.ErrNoActiveRunner) || errors.Is(err, session.ErrResumeSessionUnavailable) {
					execReq.InitialInput = inputData
					logx.Info("ws", "ai_turn starting new AI runner: connectionID=%s sessionID=%s remoteAddr=%s command=%q cwd=%q", connectionID, sessionID, remoteAddr, execReq.Command, execReq.CWD)
					err = service.Execute(ctx, sessionID, execReq, emitAndPersist)
				}
			}
			if err != nil {
				message := err.Error()
				if errors.Is(err, engine.ErrInputNotSupported) {
					message = "input is only supported for pty sessions"
				} else if errors.Is(err, session.ErrNoActiveRunner) {
					message = "当前没有活跃会话，且没有可恢复的 AI 会话，请重新发起命令"
				} else if errors.Is(err, session.ErrResumeSessionUnavailable) {
					message = "当前没有 resume id，无法恢复 AI 会话，请重新发起命令"
				} else if errors.Is(err, session.ErrResumeConversationNotFound) {
					message = "当前 AI 会话的 resume id 已失效或不存在，请重新发起命令"
				} else if errors.Is(err, session.ErrRunnerStartTimeout) {
					message = "AI 会话恢复超时，请稍后重试或重新发起命令"
				} else if errors.Is(err, session.ErrRunnerNotInteractive) {
					message = "AI 恢复后未进入可输入状态，请稍后重试"
				}
				logx.Warn("ws", "ai_turn failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(sessionID, message, ""))
				continue
			}
			if projectionWithUser, ok := appendUserProjectionEntry(buildRuntimeProjectionSnapshotForService(sessionID, service), sessionID, visibleUserText, "回复", connectionID, remoteAddr, attachments); ok {
				persistProjectionNow(sessionID, projectionWithUser)
			}
		case "exec":
			var reqEvent protocol.ExecRequestEvent
			if err := json.Unmarshal(payloadBytes, &reqEvent); err != nil {
				logx.Warn("ws", "invalid exec request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid exec request: %v", err), ""))
				continue
			}
			logx.Info("ws", "incoming action: connectionID=%s sessionID=%s remoteAddr=%s action=exec cwd=%q mode=%q permissionMode=%q source=%q target=%q targetType=%q contextID=%q preview=%q", connectionID, selectedSessionID, remoteAddr, reqEvent.CWD, reqEvent.Mode, reqEvent.PermissionMode, reqEvent.Source, reqEvent.Target, reqEvent.TargetType, reqEvent.ContextID, wsDebugPreview(reqEvent.Command))
			if strings.TrimSpace(reqEvent.Command) == "" {
				logx.Warn("ws", "reject empty exec command: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
				emit(protocol.NewErrorEvent(selectedSessionID, "cmd is required", ""))
				continue
			}
			sessionID := strings.TrimSpace(firstNonEmptyString(reqEvent.SessionID, selectedSessionID))
			if sessionID == "" || sessionID == connectionID {
				emit(protocol.NewErrorEvent(selectedSessionID, "请先创建或加载会话后再发送命令", ""))
				continue
			}
			if sessionID != strings.TrimSpace(selectedSessionID) {
				switchRuntimeSession(sessionID, runtimeSwitchDetachPrevious)
			}
			if !ackClientAction(sessionID, "exec", reqEvent.ClientEvent) {
				logx.Info("ws", "duplicate client action ignored: connectionID=%s sessionID=%s remoteAddr=%s action=exec clientActionID=%s", connectionID, sessionID, remoteAddr, reqEvent.ClientActionID)
				continue
			}
			sessionRuntime, service := runtimeForSession(sessionID)
			emitAndPersist := emitAndPersistFor(sessionID)
			mode, err := session.ParseMode(reqEvent.Mode)
			if err != nil {
				logx.Warn("ws", "parse exec mode failed: connectionID=%s sessionID=%s remoteAddr=%s mode=%q err=%v", connectionID, sessionID, remoteAddr, reqEvent.Mode, err)
				emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
				continue
			}
			if projectionWithUser, ok := appendUserProjectionEntry(buildRuntimeProjectionSnapshotForService(sessionID, service), sessionID, reqEvent.Command, "命令", connectionID, remoteAddr, nil); ok {
				persistProjectionNow(sessionID, projectionWithUser)
			}
			logx.Info("ws", "dispatch exec: connectionID=%s sessionID=%s remoteAddr=%s action=exec mode=%s cwd=%q permissionMode=%q preview=%q", connectionID, sessionID, remoteAddr, mode, reqEvent.CWD, reqEvent.PermissionMode, wsDebugPreview(reqEvent.Command))
			if sessionRuntime != nil {
				service.SetSink(sessionRuntime.EnsureBufferedSinkWithProcessor(emitAndPersist))
			}
			initialInput := reqEvent.InputData
			if initialInput != "" {
				service.RecordUserInput(initialInput)
				projection := loadFullProjectionForInteractiveMutation(clientEvent.Action, sessionID)
				if shouldInjectEnabledSkillsForInput(
					reqEvent.Command,
					reqEvent.Engine,
					reqEvent.Engine,
					projection.Runtime.Engine,
				) {
					skillPrefix, err := skills.BuildEnabledSkillsPrefix(h.SessionStore, projection.SessionContext)
					if err != nil {
						logx.Warn("ws", "build enabled skills prefix failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
					}
					memoryPrefix, memoryErr := skills.BuildEnabledMemoryPrefix(h.SessionStore, projection.SessionContext)
					if memoryErr != nil {
						logx.Warn("ws", "build enabled memory prefix failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, memoryErr)
					}
					initialInput = skills.InjectConversationPrefixes(initialInput, skillPrefix, memoryPrefix)
				}
			}
			err = service.Execute(ctx, sessionID, session.ExecuteRequest{
				Command:        reqEvent.Command,
				CWD:            reqEvent.CWD,
				Mode:           mode,
				PermissionMode: reqEvent.PermissionMode,
				InitialInput:   initialInput,
				RuntimeMeta: protocol.RuntimeMeta{
					Source:            fallback(reqEvent.Source, "command"),
					SkillName:         reqEvent.SkillName,
					Target:            reqEvent.Target,
					TargetType:        reqEvent.TargetType,
					TargetPath:        reqEvent.TargetPath,
					ResultView:        reqEvent.ResultView,
					ContextID:         reqEvent.ContextID,
					ContextTitle:      reqEvent.ContextTitle,
					TargetText:        reqEvent.TargetText,
					Command:           reqEvent.Command,
					Engine:            reqEvent.Engine,
					CodexSandboxMode:  reqEvent.RuntimeMeta.CodexSandboxMode,
					CWD:               reqEvent.CWD,
					PermissionMode:    reqEvent.PermissionMode,
					ClaudeSessionUUID: lookupClaudeSessionUUID(sessionID),
				},
			}, emitAndPersist)
			if err != nil {
				logx.Error("ws", "service execute failed: connectionID=%s sessionID=%s remoteAddr=%s action=exec err=%v", connectionID, sessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
			}
		case "input":
			var inputEvent protocol.InputRequestEvent
			if err := json.Unmarshal(payloadBytes, &inputEvent); err != nil {
				logx.Warn("ws", "invalid input request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid input request: %v", err), ""))
				continue
			}
			logx.Info("ws", "incoming action: connectionID=%s sessionID=%s remoteAddr=%s action=input permissionMode=%q dataPreview=%q", connectionID, selectedSessionID, remoteAddr, inputEvent.PermissionMode, wsDebugPreview(inputEvent.Data))
			if inputEvent.Data == "" {
				logx.Warn("ws", "reject empty input payload: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
				emit(protocol.NewErrorEvent(selectedSessionID, "input data is required", ""))
				continue
			}
			sessionID := strings.TrimSpace(firstNonEmptyString(inputEvent.SessionID, selectedSessionID))
			if sessionID == "" || sessionID == connectionID {
				emit(protocol.NewErrorEvent(selectedSessionID, "请先创建或加载会话后再发送命令", ""))
				continue
			}
			if sessionID != strings.TrimSpace(selectedSessionID) {
				switchRuntimeSession(sessionID, runtimeSwitchDetachPrevious)
			}
			if !ackClientAction(sessionID, "input", inputEvent.ClientEvent) {
				logx.Info("ws", "duplicate client action ignored: connectionID=%s sessionID=%s remoteAddr=%s action=input clientActionID=%s", connectionID, sessionID, remoteAddr, inputEvent.ClientActionID)
				continue
			}
			sessionRuntime, service := runtimeForSession(sessionID)
			emitAndPersist := emitAndPersistFor(sessionID)
			if shouldTreatInputAsAICommand(inputEvent.Data) {
				command := strings.TrimSpace(inputEvent.Data)
				promotedEngine := "claude"
				if isCodexRuntime(data.SessionRuntime{Command: command, Engine: inputEvent.RuntimeMeta.Engine}) {
					promotedEngine = "codex"
				}
				permissionMode := session.NormalizePermissionModeForEngine(inputEvent.PermissionMode, promotedEngine)
				logx.Info("ws", "promote input to exec: connectionID=%s sessionID=%s remoteAddr=%s action=input command=%q", connectionID, sessionID, remoteAddr, command)
				if projectionWithUser, ok := appendUserProjectionEntry(buildRuntimeProjectionSnapshotForService(sessionID, service), sessionID, command, "命令", connectionID, remoteAddr, nil); ok {
					persistProjectionNow(sessionID, projectionWithUser)
				}
				if sessionRuntime != nil {
					service.SetSink(sessionRuntime.EnsureBufferedSinkWithProcessor(emitAndPersist))
				}
				if err := service.Execute(ctx, sessionID, session.ExecuteRequest{
					Command:        command,
					CWD:            firstNonEmptyString(inputEvent.CWD, "/"),
					Mode:           engine.ModePTY,
					PermissionMode: permissionMode,
					RuntimeMeta: protocol.RuntimeMeta{
						Source:            "input-promoted-exec",
						Command:           command,
						Engine:            promotedEngine,
						CodexSandboxMode:  inputEvent.RuntimeMeta.CodexSandboxMode,
						CWD:               firstNonEmptyString(inputEvent.CWD, "/"),
						PermissionMode:    permissionMode,
						ClaudeSessionUUID: lookupClaudeSessionUUID(sessionID),
					},
				}, emitAndPersist); err != nil {
					logx.Error("ws", "promoted input execute failed: connectionID=%s sessionID=%s remoteAddr=%s command=%q err=%v", connectionID, sessionID, remoteAddr, command, err)
					emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
				}
				continue
			}
			controller := service.ControllerSnapshot()
			projection := loadFullProjectionForInteractiveMutation(clientEvent.Action, sessionID)
			snapshot := service.RuntimeSnapshot()
			inputData := inputEvent.Data
			attachments, err := persistImageAttachments(ctx, sessionID, inputEvent.ImageAttachments)
			if err != nil {
				logx.Warn("ws", "persist input image attachments failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
				continue
			}
			inputData = appendAttachmentPathPrompt(inputData, attachmentPaths(attachments))
			service.RecordUserInput(inputData)
			if shouldInjectEnabledSkillsForInput(
				firstNonEmptyString(snapshot.ActiveMeta.Command, controller.CurrentCommand, projection.Runtime.Command),
				snapshot.ActiveMeta.Engine,
				controller.ActiveMeta.Engine,
				projection.Runtime.Engine,
			) {
				skillPrefix, err := skills.BuildEnabledSkillsPrefix(h.SessionStore, projection.SessionContext)
				if err != nil {
					logx.Warn("ws", "build enabled skills prefix failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
				}
				memoryPrefix, memoryErr := skills.BuildEnabledMemoryPrefix(h.SessionStore, projection.SessionContext)
				if memoryErr != nil {
					logx.Warn("ws", "build enabled memory prefix failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, memoryErr)
				}
				inputData = skills.InjectConversationPrefixes(inputData, skillPrefix, memoryPrefix)
			}
			resumeCommand := firstNonEmptyString(
				snapshot.ActiveMeta.Command,
				controller.CurrentCommand,
				projection.Runtime.Command,
				defaultAICommandFromEngine(
					snapshot.ActiveMeta.Engine,
					controller.ActiveMeta.Engine,
					projection.Runtime.Engine,
				),
			)
			resumeEngine := firstNonEmptyString(
				snapshot.ActiveMeta.Engine,
				controller.ActiveMeta.Engine,
				projection.Runtime.Engine,
			)
			if isCodexRuntime(data.SessionRuntime{
				Command: resumeCommand,
				Engine:  resumeEngine,
				Source:  projection.Runtime.Source,
			}) {
				resumeEngine = "codex"
			}
			inputMeta := protocol.RuntimeMeta{}
			if pm := inputEvent.PermissionMode; pm != "" {
				service.UpdatePermissionModeForEngine(pm, resumeEngine)
				controller = service.ControllerSnapshot()
				snapshot = service.RuntimeSnapshot()
				inputMeta.PermissionMode = service.ControllerSnapshot().ActiveMeta.PermissionMode
			}
			// 权限请求待处理时，阻止普通文本输入写入 Claude stdin，
			// 防止 Claude 在等待结构化权限响应时收到无效文本导致 hang。
			if currentRunner := service.CurrentRunner(); currentRunner != nil {
				if prw, ok := currentRunner.(engine.PermissionResponseWriter); ok &&
					session.ShouldBlockInputForPendingPermission(prw, service, projection, sessionRuntime) {
					logx.Warn("ws", "input blocked by pending permission request: connectionID=%s sessionID=%s remoteAddr=%s preview=%q", connectionID, sessionID, remoteAddr, wsDebugPreview(inputData))
					emit(protocol.NewErrorEvent(sessionID, "有权限请求待处理，请先在 App 中完成授权", ""))
					continue
				}
			}
			logx.Info("ws", "dispatch input: connectionID=%s sessionID=%s remoteAddr=%s action=input permissionMode=%q preview=%q", connectionID, sessionID, remoteAddr, inputMeta.PermissionMode, wsDebugPreview(inputData))
			resumePermissionMode := session.NormalizePermissionModeForEngine(firstNonEmptyString(
				inputEvent.PermissionMode,
				snapshot.ActiveMeta.PermissionMode,
				controller.ActiveMeta.PermissionMode,
				projection.Runtime.PermissionMode,
			), resumeEngine)
			codexSandboxMode := ""
			if resumeEngine == "codex" {
				codexSandboxMode = firstNonEmptyString(
					inputEvent.RuntimeMeta.CodexSandboxMode,
					snapshot.ActiveMeta.CodexSandboxMode,
					controller.ActiveMeta.CodexSandboxMode,
					projection.Runtime.CodexSandboxMode,
				)
				inputMeta.CodexSandboxMode = codexSandboxMode
			}
			resumeReq := session.ExecuteRequest{
				Command: resumeCommand,
				CWD: firstNonEmptyString(
					snapshot.ActiveMeta.CWD,
					controller.ActiveMeta.CWD,
					projection.Runtime.CWD,
				),
				Mode:           engine.ModePTY,
				PermissionMode: resumePermissionMode,
				RuntimeMeta: protocol.RuntimeMeta{
					Source: "input",
					ResumeSessionID: firstNonEmptyString(
						snapshot.ResumeSessionID,
						snapshot.ActiveMeta.ResumeSessionID,
						controller.ResumeSession,
						projection.Runtime.ResumeSessionID,
					),
					Command:          resumeCommand,
					Engine:           resumeEngine,
					CodexSandboxMode: codexSandboxMode,
					CWD: firstNonEmptyString(
						snapshot.ActiveMeta.CWD,
						controller.ActiveMeta.CWD,
						projection.Runtime.CWD,
					),
					PermissionMode:    resumePermissionMode,
					ClaudeSessionUUID: lookupClaudeSessionUUID(sessionID),
				},
			}
			if service.ShouldEmitTransientResumeThinkingEvent(resumeReq) {
				emit(protocol.ApplyRuntimeMeta(
					protocol.NewAgentStateEvent(
						sessionID,
						string(session.ControllerStateThinking),
						"恢复会话中",
						false,
						resumeReq.Command,
						projection.Controller.LastStep,
						projection.Controller.LastTool,
					),
					protocol.MergeRuntimeMeta(projection.Controller.ActiveMeta, protocol.RuntimeMeta{
						Source:           "input",
						ResumeSessionID:  resumeReq.RuntimeMeta.ResumeSessionID,
						Command:          resumeReq.Command,
						Engine:           firstNonEmptyString(resumeReq.RuntimeMeta.Engine, projection.Runtime.Engine),
						CodexSandboxMode: resumeReq.RuntimeMeta.CodexSandboxMode,
						CWD:              resumeReq.CWD,
						PermissionMode:   resumeReq.PermissionMode,
						ClaudeLifecycle:  "active",
					}),
				))
			}
			if err := service.SendInputOrResume(ctx, sessionID, resumeReq, session.InputRequest{Data: inputData, RuntimeMeta: inputMeta}, emitAndPersist); err != nil {
				message := err.Error()
				if errors.Is(err, engine.ErrInputNotSupported) {
					message = "input is only supported for pty sessions"
				} else if errors.Is(err, session.ErrNoActiveRunner) {
					message = "当前没有活跃会话，且没有可恢复的 Claude 会话，请重新发起命令"
				} else if errors.Is(err, session.ErrResumeSessionUnavailable) {
					message = "当前没有 resume id，无法恢复 Claude 会话，请重新发起命令"
				} else if errors.Is(err, session.ErrResumeConversationNotFound) {
					message = "当前 Claude 会话的 resume id 已失效或不存在，请重新发起命令"
				} else if errors.Is(err, session.ErrRunnerStartTimeout) {
					message = "Claude 会话恢复超时，请稍后重试或重新发起命令"
				} else if errors.Is(err, session.ErrRunnerNotInteractive) {
					message = "Claude 恢复后未进入可输入状态，请稍后重试"
				}
				logx.Warn("ws", "service send input failed: connectionID=%s sessionID=%s remoteAddr=%s action=input err=%v", connectionID, sessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(sessionID, message, ""))
			} else {
				if projectionWithUser, ok := appendUserProjectionEntry(buildRuntimeProjectionSnapshotForService(sessionID, service), sessionID, strings.TrimRight(inputEvent.Data, "\n"), "回复", connectionID, remoteAddr, attachments); ok {
					persistProjectionNow(sessionID, projectionWithUser)
				}
			}
		case "stop":
			if strings.TrimSpace(selectedSessionID) == "" || selectedSessionID == connectionID {
				continue
			}
			if !ackClientAction(selectedSessionID, "stop", clientEvent) {
				logx.Info("ws", "duplicate client action ignored: connectionID=%s sessionID=%s remoteAddr=%s action=stop clientActionID=%s", connectionID, selectedSessionID, remoteAddr, clientEvent.ClientActionID)
				continue
			}
			_, service := runtimeForSession(selectedSessionID)
			if err := service.StopActive(selectedSessionID, emitAndPersistFor(selectedSessionID)); err != nil {
				if errors.Is(err, session.ErrNoActiveRunner) {
					emit(protocol.NewSessionStateEvent(selectedSessionID, "stopped", "当前没有可停止的运行"))
					continue
				}
				logx.Warn("ws", "stop active runner failed: connectionID=%s sessionID=%s remoteAddr=%s action=stop err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
			}
		case "permission_decision":
			var permissionEvent protocol.PermissionDecisionRequestEvent
			if err := json.Unmarshal(payloadBytes, &permissionEvent); err != nil {
				logx.Warn("ws", "invalid permission decision request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid permission decision request: %v", err), ""))
				continue
			}
			logx.Info("ws", "incoming action: connectionID=%s sessionID=%s remoteAddr=%s action=permission_decision decision=%q permissionRequestID=%q permissionMode=%q resumeSessionID=%q targetPath=%q contextID=%q fallbackCWD=%q fallbackCommandPreview=%q promptPreview=%q", connectionID, selectedSessionID, remoteAddr, permissionEvent.Decision, permissionEvent.PermissionRequestID, permissionEvent.PermissionMode, permissionEvent.ResumeSessionID, permissionEvent.TargetPath, permissionEvent.ContextID, permissionEvent.FallbackCWD, wsDebugPreview(permissionEvent.FallbackCommand), wsDebugPreview(permissionEvent.PromptMessage))
			decision := strings.TrimSpace(strings.ToLower(permissionEvent.Decision))
			if decision != "approve" && decision != "deny" {
				logx.Warn("ws", "reject invalid permission decision: connectionID=%s sessionID=%s remoteAddr=%s decision=%q", connectionID, selectedSessionID, remoteAddr, permissionEvent.Decision)
				emit(protocol.NewErrorEvent(selectedSessionID, "permission decision must be one of: approve, deny", ""))
				continue
			}
			sessionID, service := resolvePermissionDecisionRuntime(permissionEvent)
			if !ackClientAction(sessionID, "permission_decision", permissionEvent.ClientEvent) {
				logx.Info("ws", "duplicate client action ignored: connectionID=%s sessionID=%s remoteAddr=%s action=permission_decision clientActionID=%s", connectionID, sessionID, remoteAddr, permissionEvent.ClientActionID)
				continue
			}
			sessionRuntime, _ := runtimeForSession(sessionID)
			emitAndPersist := emitAndPersistFor(sessionID)
			projection := loadFullProjectionForInteractiveMutation(clientEvent.Action, sessionID)
			controller := service.ControllerSnapshot()
			if requestedID := strings.TrimSpace(permissionEvent.PermissionRequestID); requestedID != "" {
				currentID := strings.TrimSpace(service.CurrentPermissionRequestID(sessionID))
				if currentID != "" && currentID != requestedID {
					var refreshed *protocol.PromptRequestEvent
					if sessionRuntime != nil {
						refreshed = sessionRuntime.latestPendingPermissionPrompt(currentID)
					}
					if refreshed == nil {
						refreshed = session.RefreshedPermissionPromptEvent(sessionID, permissionEvent, service)
					}
					if refreshed != nil {
						logx.Info("ws", "permission decision request id stale, refreshing current request id: connectionID=%s sessionID=%s remoteAddr=%s clientRequestID=%q currentRequestID=%q decision=%q", connectionID, sessionID, remoteAddr, requestedID, currentID, decision)
						emit(*refreshed)
						continue
					}
				}
			}
			if projectionWithUser, ok := appendUserProjectionEntry(projection, sessionID, strings.TrimSpace(permissionEvent.PromptMessage), "权限决策", connectionID, remoteAddr, nil); ok {
				persistProjectionNow(sessionID, projectionWithUser)
				projection = projectionWithUser
			}
			scope := strings.TrimSpace(permissionEvent.Scope)
			if decision == "approve" && (scope == string(data.PermissionScopeSession) || scope == string(data.PermissionScopePersistent)) {
				rule := buildPermissionRule(permissionEvent, scope, projection, controller)
				switch data.PermissionScope(scope) {
				case data.PermissionScopePersistent:
					snapshot, err := h.SessionStore.GetPermissionRuleSnapshot(ctx)
					if err != nil {
						logx.Error("ws", "load persistent permission rules failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
					} else {
						snapshot.Enabled = true
						snapshot.Items = upsertPermissionRule(snapshot.Items, rule)
						if err := h.SessionStore.SavePermissionRuleSnapshot(ctx, snapshot); err != nil {
							logx.Error("ws", "save persistent permission rule failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
						}
					}
				default:
					projection.PermissionRulesEnabled = true
					projection.PermissionRules = upsertPermissionRule(projection.PermissionRules, rule)
					if err := saveSessionPermissionRules(ctx, h.SessionStore, sessionID, func(snapshot data.SessionPermissionRuleSnapshot) data.SessionPermissionRuleSnapshot {
						snapshot.Enabled = true
						snapshot.Items = upsertPermissionRule(snapshot.Items, rule)
						return snapshot
					}); err != nil {
						logx.Error("ws", "save session permission rule failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
						emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
					} else {
						invalidateSessionListCacheForMutation()
					}
				}
				emitPermissionRuleList(emit, h.SessionStore, ctx, sessionID)
			}
			err := executePermissionDecision(ctx, sessionID, permissionEvent, service, projection, controller, emitAndPersist)
			if err == nil {
				continue
			}
			message := err.Error()
			if errors.Is(err, session.ErrNoActiveRunner) {
				message = "当前没有可交互的 Claude 会话，无法继续处理该权限请求"
			} else if errors.Is(err, session.ErrPermissionRequestExpired) {
				if refreshed := session.RefreshedPermissionPromptEvent(sessionID, permissionEvent, service); refreshed != nil {
					logx.Info("ws", "permission decision expired, refreshing current request id: connectionID=%s sessionID=%s remoteAddr=%s clientRequestID=%q currentRequestID=%q", connectionID, sessionID, remoteAddr, permissionEvent.PermissionRequestID, refreshed.PermissionRequestID)
					emit(*refreshed)
					continue
				}
				message = "当前权限请求已失效，请等待 AI 重新发起操作后再确认"
			} else if errors.Is(err, engine.ErrInputNotSupported) {
				message = "当前会话不支持交互输入，请先恢复 Claude PTY 会话"
			} else if errors.Is(err, session.ErrResumeSessionUnavailable) {
				message = "当前没有可恢复的 Claude 会话，无法继续此次权限批准"
			} else if errors.Is(err, session.ErrResumeConversationNotFound) {
				message = "当前 Claude 会话的 resume id 已失效或不存在，无法继续此次权限批准"
			} else if errors.Is(err, session.ErrRunnerStartTimeout) {
				message = "Claude 会话恢复超时，无法继续此次权限批准"
			} else if errors.Is(err, session.ErrRunnerNotInteractive) {
				message = "Claude 恢复后未进入可输入状态，无法继续刚才的操作"
			}
			logx.Warn("ws", "permission decision failed: connectionID=%s sessionID=%s remoteAddr=%s decision=%s err=%v", connectionID, sessionID, remoteAddr, decision, err)
			emit(protocol.NewErrorEvent(sessionID, message, ""))

		case "review_decision":
			var reviewEvent protocol.ReviewDecisionRequestEvent
			if err := json.Unmarshal(payloadBytes, &reviewEvent); err != nil {
				logx.Warn("ws", "invalid review decision request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid review decision request: %v", err), ""))
				continue
			}
			logx.Info("ws", "incoming action: connectionID=%s sessionID=%s remoteAddr=%s action=review_decision decision=%q executionID=%q groupID=%q groupTitle=%q contextID=%q contextTitle=%q targetPath=%q permissionMode=%q isReviewOnly=%t", connectionID, selectedSessionID, remoteAddr, reviewEvent.Decision, reviewEvent.ExecutionID, reviewEvent.GroupID, reviewEvent.GroupTitle, reviewEvent.ContextID, reviewEvent.ContextTitle, reviewEvent.TargetPath, reviewEvent.PermissionMode, reviewEvent.IsReviewOnly)
			decision := strings.TrimSpace(strings.ToLower(reviewEvent.Decision))
			if decision != "accept" && decision != "revert" && decision != "revise" {
				logx.Warn("ws", "reject invalid review decision: connectionID=%s sessionID=%s remoteAddr=%s decision=%q", connectionID, selectedSessionID, remoteAddr, reviewEvent.Decision)
				emit(protocol.NewErrorEvent(selectedSessionID, "review decision must be one of: accept, revert, revise", ""))
				continue
			}
			sessionID := selectedSessionID
			if !ackClientAction(sessionID, "review_decision", reviewEvent.ClientEvent) {
				logx.Info("ws", "duplicate client action ignored: connectionID=%s sessionID=%s remoteAddr=%s action=review_decision clientActionID=%s", connectionID, sessionID, remoteAddr, reviewEvent.ClientActionID)
				continue
			}
			_, service := runtimeForSession(sessionID)
			emitAndPersist := emitAndPersistFor(sessionID)
			projection := loadFullProjectionForInteractiveMutation(clientEvent.Action, sessionID)
			controller := service.ControllerSnapshot()
			effectivePermissionMode := strings.TrimSpace(reviewEvent.PermissionMode)
			if effectivePermissionMode == "" {
				effectivePermissionMode = strings.TrimSpace(controller.ActiveMeta.PermissionMode)
			}
			if effectivePermissionMode == "" {
				effectivePermissionMode = strings.TrimSpace(projection.Runtime.PermissionMode)
			}
			if !service.CanAcceptInteractiveInput() {
				emit(protocol.NewErrorEvent(selectedSessionID, "当前 Claude 会话尚未进入可直接确认的交互阶段，请先等待当前会话就绪后再提交审核决策", ""))
				continue
			}
			if effectivePermissionMode != "" {
				service.UpdatePermissionMode(effectivePermissionMode)
			}
			var currentDiff session.DiffContext
			if projection.CurrentDiff != nil {
				currentDiff = *projection.CurrentDiff
			}
			interactionType := "none"
			if controller.ActiveMeta.BlockingKind != "" {
				interactionType = controller.ActiveMeta.BlockingKind
			}
			logx.Info("ws", "review_decision routing: connectionID=%s sessionID=%s targetId=%q groupId=%q interactionType=%q isReviewOnly=%t", connectionID, sessionID, firstNonEmptyString(reviewEvent.ContextID, currentDiff.ContextID), firstNonEmptyString(reviewEvent.GroupID, currentDiff.GroupID), interactionType, reviewEvent.IsReviewOnly)
			if err := service.ReviewDecision(ctx, sessionID, session.ReviewDecisionRequest{
				Decision:     decision,
				IsReviewOnly: reviewEvent.IsReviewOnly,
				RuntimeMeta: protocol.RuntimeMeta{
					Source:         "review-decision",
					ExecutionID:    firstNonEmptyString(reviewEvent.ExecutionID, currentDiff.ExecutionID),
					GroupID:        firstNonEmptyString(reviewEvent.GroupID, reviewEvent.ExecutionID, currentDiff.GroupID, reviewEvent.ContextID),
					GroupTitle:     firstNonEmptyString(reviewEvent.GroupTitle, currentDiff.GroupTitle, reviewEvent.ContextTitle),
					ContextID:      firstNonEmptyString(reviewEvent.ContextID, currentDiff.ContextID),
					ContextTitle:   firstNonEmptyString(reviewEvent.ContextTitle, currentDiff.Title),
					TargetPath:     firstNonEmptyString(reviewEvent.TargetPath, currentDiff.Path),
					TargetText:     decision,
					Command:        firstNonEmptyString(controller.ActiveMeta.Command, projection.Runtime.Command),
					CWD:            firstNonEmptyString(controller.ActiveMeta.CWD, projection.Runtime.CWD),
					PermissionMode: effectivePermissionMode,
				},
			}, emitAndPersist); err != nil {
				message := err.Error()
				if errors.Is(err, engine.ErrInputNotSupported) {
					message = "当前会话不支持交互输入，请先恢复 Claude PTY 会话"
				} else if errors.Is(err, session.ErrNoActiveRunner) {
					message = "当前没有可交互会话，请先恢复会话后再审核 diff"
				} else if errors.Is(err, session.ErrRunnerStartTimeout) {
					message = "当前会话恢复超时，请稍后重试再审核 diff"
				} else if errors.Is(err, session.ErrRunnerNotInteractive) {
					message = "当前 Claude 会话尚未进入可直接确认的交互阶段，请先等待当前会话就绪后再提交审核决策"
				}
				logx.Warn("ws", "send review decision failed: connectionID=%s sessionID=%s remoteAddr=%s decision=%s err=%v", connectionID, sessionID, remoteAddr, decision, err)
				emit(protocol.NewErrorEvent(sessionID, message, ""))
				continue
			}
			projection = session.ApplyReviewDecisionToProjection(projection, reviewEvent, decision, currentDiff)
			persistProjectionFor(sessionID, projection)
			emitReviewStateFromProjection(emit, sessionID, projection)
		case "plan_decision":
			var planEvent protocol.PlanDecisionRequestEvent
			if err := json.Unmarshal(payloadBytes, &planEvent); err != nil {
				logx.Warn("ws", "invalid plan decision request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid plan decision request: %v", err), ""))
				continue
			}
			logx.Info("ws", "incoming action: connectionID=%s sessionID=%s remoteAddr=%s action=plan_decision decision=%q executionID=%q groupID=%q contextID=%q promptPreview=%q", connectionID, selectedSessionID, remoteAddr, planEvent.Decision, planEvent.ExecutionID, planEvent.GroupID, planEvent.ContextID, wsDebugPreview(planEvent.PromptMessage))
			if !ackClientAction(selectedSessionID, "plan_decision", planEvent.ClientEvent) {
				logx.Info("ws", "duplicate client action ignored: connectionID=%s sessionID=%s remoteAddr=%s action=plan_decision clientActionID=%s", connectionID, selectedSessionID, remoteAddr, planEvent.ClientActionID)
				continue
			}
			_, service := runtimeForSession(selectedSessionID)
			emitAndPersist := emitAndPersistFor(selectedSessionID)
			projection := loadFullProjectionForInteractiveMutation(clientEvent.Action, selectedSessionID)
			controller := service.ControllerSnapshot()
			req := session.PlanDecisionRequest{
				Decision: planEvent.Decision,
				RuntimeMeta: protocol.RuntimeMeta{
					Source:          "plan-decision",
					ResumeSessionID: planEvent.ResumeSessionID,
					ExecutionID:     planEvent.ExecutionID,
					GroupID:         planEvent.GroupID,
					GroupTitle:      planEvent.GroupTitle,
					ContextID:       planEvent.ContextID,
					ContextTitle:    planEvent.ContextTitle,
					TargetPath:      planEvent.TargetPath,
					TargetText:      planEvent.TargetText,
					Command:         firstNonEmptyString(planEvent.Command, controller.ActiveMeta.Command, projection.Runtime.Command),
					Engine:          firstNonEmptyString(planEvent.Engine, controller.ActiveMeta.Engine),
					CWD:             firstNonEmptyString(planEvent.CWD, controller.ActiveMeta.CWD, projection.Runtime.CWD),
					Target:          firstNonEmptyString(planEvent.Target, controller.ActiveMeta.Target),
					TargetType:      firstNonEmptyString(planEvent.TargetType, controller.ActiveMeta.TargetType),
					PermissionMode:  firstNonEmptyString(planEvent.PermissionMode, controller.ActiveMeta.PermissionMode, projection.Runtime.PermissionMode),
				},
			}
			if err := service.PlanDecision(ctx, selectedSessionID, req, emitAndPersist); err != nil {
				message := err.Error()
				if errors.Is(err, session.ErrNoActiveRunner) {
					message = "当前没有可交互的 Claude 会话，无法继续处理该 plan 请求"
				} else if errors.Is(err, session.ErrRunnerStartTimeout) {
					message = "当前 Claude 会话恢复超时，无法继续处理该 plan 请求"
				} else if errors.Is(err, session.ErrRunnerNotInteractive) {
					message = "当前 Claude 会话尚未进入可提交 plan 的交互阶段，请先等待当前会话就绪后再提交"
				}
				logx.Warn("ws", "send plan decision failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, message, ""))
			}
		case "set_permission_mode":
			var modeEvent protocol.PermissionModeUpdateRequestEvent
			if err := json.Unmarshal(payloadBytes, &modeEvent); err != nil {
				logx.Warn("ws", "invalid permission mode request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid permission mode request: %v", err), ""))
				continue
			}
			_, service := runtimeForSession(selectedSessionID)
			projection := loadFullProjectionForInteractiveMutation(clientEvent.Action, selectedSessionID)
			engineName := strings.TrimSpace(projection.Runtime.Engine)
			if isCodexRuntime(projection.Runtime) {
				engineName = "codex"
			}
			service.UpdatePermissionModeForEngine(modeEvent.PermissionMode, engineName)
			effectivePermissionMode := service.ControllerSnapshot().ActiveMeta.PermissionMode
			if strings.TrimSpace(effectivePermissionMode) != "" {
				projection = loadFullProjectionForInteractiveMutation(clientEvent.Action, selectedSessionID)
				projection.Runtime.PermissionMode = effectivePermissionMode
				if effectivePermissionMode == "auto" || effectivePermissionMode == "bypassPermissions" {
					projection = session.ApplyAutoReviewAcceptanceToProjection(projection)
					emitReviewStateFromProjection(emit, selectedSessionID, projection)
				}
				persistProjectionFor(selectedSessionID, projection)
			}
			emitAndPersistFor(selectedSessionID)(protocol.ApplyRuntimeMeta(service.InitialEvent(), protocol.RuntimeMeta{PermissionMode: effectivePermissionMode}))
		case "skill_exec":
			var skillEvent protocol.SkillRequestEvent
			if err := json.Unmarshal(payloadBytes, &skillEvent); err != nil {
				logx.Warn("ws", "invalid skill request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid skill request: %v", err), ""))
				continue
			}
			skillLauncher := h.currentSkillLauncher()
			if skillLauncher == nil {
				logx.Error("ws", "skill launcher unavailable: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, selectedSessionID, remoteAddr)
				emit(protocol.NewErrorEvent(selectedSessionID, "skill launcher is unavailable", ""))
				continue
			}
			sessionContext := data.SessionContext{}
			if h.SessionStore != nil {
				loadedContext, err := readSessionContextForAction("skill_exec", selectedSessionID)
				if err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
				sessionContext = loadedContext
			}
			sessionID := selectedSessionID
			_, service := runtimeForSession(sessionID)
			emitAndPersist := emitAndPersistFor(sessionID)
			if err := executeSkillRequest(ctx, sessionID, skillEvent, sessionContext, service, skillLauncher, emitAndPersist); err != nil {
				logx.Error("ws", "execute skill request failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
			}
		case "runtime_info":
			var infoReq protocol.RuntimeInfoRequestEvent
			if err := json.Unmarshal(payloadBytes, &infoReq); err != nil {
				logx.Warn("ws", "invalid runtime_info request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid runtime_info request: %v", err), ""))
				continue
			}
			result, err := session.BuildRuntimeInfoResult(selectedSessionID, infoReq.Query, fallback(infoReq.CWD, "."), runtimeSvc)
			if err != nil {
				logx.Warn("ws", "build runtime info failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			emit(result)
		case protocol.ActionContextWindowUsageGet, protocol.ActionContextWindowsUsageGetAlias:
			var usageReq protocol.ContextWindowUsageRequestEvent
			if err := json.Unmarshal(payloadBytes, &usageReq); err != nil {
				logx.Warn("ws", "invalid %s request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", clientEvent.Action, connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid %s request: %v", clientEvent.Action, err), ""))
				continue
			}
			targetSessionID := strings.TrimSpace(usageReq.SessionID)
			if targetSessionID == "" {
				targetSessionID = strings.TrimSpace(selectedSessionID)
			}
			_, targetService := runtimeForSession(targetSessionID)
			emitContextWindowUsageIfAvailable(targetSessionID, targetService)
		case "runtime_process_list":
			rootPID, items, err := runtimeSvc.ActiveProcessTree(ctx)
			if err != nil {
				logx.Warn("ws", "build runtime process list failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			message := ""
			if len(items) == 0 {
				message = "当前没有活跃的后台进程"
			}
			emit(protocol.NewRuntimeProcessListResultEvent(selectedSessionID, rootPID, items, message))
		case "runtime_process_log_get":
			var processReq protocol.RuntimeProcessLogRequestEvent
			if err := json.Unmarshal(payloadBytes, &processReq); err != nil {
				logx.Warn("ws", "invalid runtime_process_log_get request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid runtime_process_log_get request: %v", err), ""))
				continue
			}
			if processReq.PID <= 0 {
				emit(protocol.NewErrorEvent(selectedSessionID, "pid 必须为正整数", ""))
				continue
			}
			_, items, err := runtimeSvc.ActiveProcessTree(ctx)
			if err != nil {
				logx.Warn("ws", "load runtime process before log failed: connectionID=%s sessionID=%s remoteAddr=%s pid=%d err=%v", connectionID, selectedSessionID, remoteAddr, processReq.PID, err)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			item, ok := findRuntimeProcessItem(items, processReq.PID)
			if !ok {
				emit(protocol.NewErrorEvent(selectedSessionID, "指定进程不存在或已退出", ""))
				continue
			}
			execution, message, err := readRuntimeProcessExecutionLog(selectedSessionID, item)
			if err != nil {
				logx.Warn("ws", "load runtime process log failed: connectionID=%s sessionID=%s remoteAddr=%s pid=%d executionID=%q err=%v", connectionID, selectedSessionID, remoteAddr, processReq.PID, item.ExecutionID, err)
				emit(protocol.NewErrorEventWithCode(selectedSessionID, err.Error(), "", "runtime_process_log_unavailable"))
				continue
			}
			emit(protocol.NewRuntimeProcessLogResultEvent(
				selectedSessionID,
				item.PID,
				item.ExecutionID,
				firstNonEmptyString(execution.Command, item.Command),
				firstNonEmptyString(execution.CWD, item.CWD),
				item.Source,
				execution.Stdout,
				execution.Stderr,
				message,
			))
		case "adb_devices":
			var adbReq protocol.ADBDevicesRequestEvent
			if err := json.Unmarshal(payloadBytes, &adbReq); err != nil {
				logx.Warn("ws", "invalid adb_devices request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid adb_devices request: %v", err), ""))
				continue
			}
			emitADBDevices("ADB 设备列表已刷新")
		case "adb_stream_start":
			var adbReq protocol.ADBStreamStartRequestEvent
			if err := json.Unmarshal(payloadBytes, &adbReq); err != nil {
				logx.Warn("ws", "invalid adb_stream_start request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid adb_stream_start request: %v", err), ""))
				continue
			}
			interval := time.Duration(adbReq.IntervalMS) * time.Millisecond
			if interval <= 0 {
				interval = 700 * time.Millisecond
			}
			if interval < 250*time.Millisecond {
				interval = 250 * time.Millisecond
			}
			adbRTC.Stop("")
			startADBStream(adbReq.Serial, interval)
		case "adb_stream_stop":
			var adbReq protocol.ADBStreamStopRequestEvent
			if err := json.Unmarshal(payloadBytes, &adbReq); err != nil {
				logx.Warn("ws", "invalid adb_stream_stop request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid adb_stream_stop request: %v", err), ""))
				continue
			}
			stopADBStream("ADB 画面预览已停止")
			adbRTC.Stop("")
		case "adb_emulator_start":
			var adbReq protocol.ADBEmulatorStartRequestEvent
			if err := json.Unmarshal(payloadBytes, &adbReq); err != nil {
				logx.Warn("ws", "invalid adb_emulator_start request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid adb_emulator_start request: %v", err), ""))
				continue
			}
			if err := adb.StartEmulator(adbReq.AVD); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			emitADBDevices("模拟器启动中，等待设备上线…")
		case "adb_tap":
			var adbReq protocol.ADBTapRequestEvent
			if err := json.Unmarshal(payloadBytes, &adbReq); err != nil {
				logx.Warn("ws", "invalid adb_tap request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid adb_tap request: %v", err), ""))
				continue
			}
			if adbReq.X < 0 || adbReq.Y < 0 {
				emit(protocol.NewErrorEvent(selectedSessionID, "adb tap 坐标必须为非负整数", ""))
				continue
			}
			adbMu.Lock()
			activeSerial := adbActiveSerial
			adbMu.Unlock()
			serial := strings.TrimSpace(adbReq.Serial)
			if serial == "" {
				serial = activeSerial
			}
			if err := adb.Tap(ctx, serial, adbReq.X, adbReq.Y); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
		case "adb_swipe":
			var adbReq protocol.ADBSwipeRequestEvent
			if err := json.Unmarshal(payloadBytes, &adbReq); err != nil {
				logx.Warn("ws", "invalid adb_swipe request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid adb_swipe request: %v", err), ""))
				continue
			}
			if adbReq.StartX < 0 || adbReq.StartY < 0 || adbReq.EndX < 0 || adbReq.EndY < 0 {
				emit(protocol.NewErrorEvent(selectedSessionID, "adb swipe 坐标必须为非负整数", ""))
				continue
			}
			adbMu.Lock()
			activeSerial := adbActiveSerial
			adbMu.Unlock()
			serial := strings.TrimSpace(adbReq.Serial)
			if serial == "" {
				serial = activeSerial
			}
			if err := adb.Swipe(ctx, serial, adbReq.StartX, adbReq.StartY, adbReq.EndX, adbReq.EndY, adbReq.DurationMS); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
		case "adb_keyevent":
			var adbReq protocol.ADBKeyeventRequestEvent
			if err := json.Unmarshal(payloadBytes, &adbReq); err != nil {
				logx.Warn("ws", "invalid adb_keyevent request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid adb_keyevent request: %v", err), ""))
				continue
			}
			if strings.TrimSpace(adbReq.Keycode) == "" {
				emit(protocol.NewErrorEvent(selectedSessionID, "adb keyevent keycode 不能为空", ""))
				continue
			}
			adbMu.Lock()
			activeSerial := adbActiveSerial
			adbMu.Unlock()
			serial := strings.TrimSpace(adbReq.Serial)
			if serial == "" {
				serial = activeSerial
			}
			if err := adb.Keyevent(ctx, serial, adbReq.Keycode); err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
		case "adb_webrtc_offer":
			var adbReq protocol.ADBWebRTCOfferRequestEvent
			if err := json.Unmarshal(payloadBytes, &adbReq); err != nil {
				logx.Warn("ws", "invalid adb_webrtc_offer request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid adb_webrtc_offer request: %v", err), ""))
				continue
			}
			logx.Info(
				"ws",
				"incoming adb_webrtc_offer: connectionID=%s sessionID=%s remoteAddr=%s serial=%q sdpType=%q sdpBytes=%d iceServers=%d",
				connectionID,
				selectedSessionID,
				remoteAddr,
				adbReq.Serial,
				adbReq.Type,
				len(strings.TrimSpace(adbReq.SDP)),
				len(adbReq.ICEServers),
			)
			stopADBStream("")
			if err := adbRTC.HandleOffer(ctx, adbReq.Serial, adbReq.Type, adbReq.SDP, adbReq.ICEServers); err != nil {
				logx.Warn(
					"ws",
					"adb_webrtc_offer failed: connectionID=%s sessionID=%s remoteAddr=%s serial=%q err=%v",
					connectionID,
					selectedSessionID,
					remoteAddr,
					adbReq.Serial,
					err,
				)
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			logx.Info(
				"ws",
				"adb_webrtc_offer handled: connectionID=%s sessionID=%s remoteAddr=%s serial=%q",
				connectionID,
				selectedSessionID,
				remoteAddr,
				adbReq.Serial,
			)
		case "adb_webrtc_stop":
			var adbReq protocol.ADBWebRTCStopRequestEvent
			if err := json.Unmarshal(payloadBytes, &adbReq); err != nil {
				logx.Warn("ws", "invalid adb_webrtc_stop request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid adb_webrtc_stop request: %v", err), ""))
				continue
			}
			logx.Info(
				"ws",
				"incoming adb_webrtc_stop: connectionID=%s sessionID=%s remoteAddr=%s",
				connectionID,
				selectedSessionID,
				remoteAddr,
			)
			adbRTC.Stop("ADB WebRTC 调试已停止")
		case "slash_command":
			var slashReq protocol.SlashCommandRequestEvent
			if err := json.Unmarshal(payloadBytes, &slashReq); err != nil {
				logx.Warn("ws", "invalid slash_command request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid slash_command request: %v", err), ""))
				continue
			}
			logx.Info(
				"ws",
				"incoming slash_command: connectionID=%s sessionID=%s remoteAddr=%s command=%q engine=%q runtimeCommand=%q clientActionID=%s",
				connectionID,
				selectedSessionID,
				remoteAddr,
				slashReq.Command,
				slashReq.Engine,
				slashReq.Command,
				slashReq.ClientActionID,
			)
			sessionIDForAck := strings.TrimSpace(firstNonEmptyString(slashReq.SessionID, selectedSessionID))
			if sessionIDForAck == "" {
				sessionIDForAck = selectedSessionID
			}
			if !ackClientAction(sessionIDForAck, "slash_command", slashReq.ClientEvent) {
				logx.Info("ws", "duplicate client action ignored: connectionID=%s sessionID=%s remoteAddr=%s action=slash_command clientActionID=%s", connectionID, sessionIDForAck, remoteAddr, slashReq.ClientActionID)
				continue
			}
			parsedSlash, err := parseSlashCommand(slashReq.Command)
			if err != nil {
				emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
				continue
			}
			sessionContext := data.SessionContext{}
			if h.SessionStore != nil && strings.TrimSpace(selectedSessionID) != "" {
				loadedContext, err := readSessionContextForAction("slash_command", selectedSessionID)
				if err != nil {
					emit(protocol.NewErrorEvent(selectedSessionID, err.Error(), ""))
					continue
				}
				sessionContext = loadedContext
			}
			sessionID := strings.TrimSpace(firstNonEmptyString(slashReq.SessionID, selectedSessionID))
			if sessionID == "" && (parsedSlash.spec.category == "local" || parsedSlash.spec.category == "runtime-info") {
				sessionID = selectedSessionID
			}
			if sessionID == "" && parsedSlash.spec.category == "exec" {
				sessionID = connectionID
			}
			if sessionID == "" &&
				parsedSlash.spec.category != "local" &&
				parsedSlash.spec.category != "runtime-info" {
				emit(protocol.NewErrorEvent(selectedSessionID, "请先创建或加载会话后再发送命令", ""))
				continue
			}
			if sessionID == connectionID && parsedSlash.spec.category != "exec" {
				emit(protocol.NewErrorEvent(selectedSessionID, "请先创建或加载会话后再发送命令", ""))
				continue
			}
			if sessionID != strings.TrimSpace(selectedSessionID) {
				switchRuntimeSession(sessionID, runtimeSwitchDetachPrevious)
			}
			service := runtimeSvc
			emitAndPersist := emitAndPersistFor(sessionID)
			if err := handleSlashCommand(ctx, sessionID, slashReq, sessionContext, service, h.currentSkillLauncher(), emitAndPersist); err != nil {
				logx.Error("ws", "handle slash command failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
			}
		case "compact":
			var compactReq protocol.CompactRequestEvent
			if err := json.Unmarshal(payloadBytes, &compactReq); err != nil {
				logx.Warn("ws", "invalid compact request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid compact request: %v", err), ""))
				continue
			}
			sessionIDForAck := strings.TrimSpace(firstNonEmptyString(compactReq.SessionID, selectedSessionID))
			if sessionIDForAck == "" {
				sessionIDForAck = selectedSessionID
			}
			if !ackClientAction(sessionIDForAck, "compact", compactReq.ClientEvent) {
				logx.Info("ws", "duplicate client action ignored: connectionID=%s sessionID=%s remoteAddr=%s action=compact clientActionID=%s", connectionID, sessionIDForAck, remoteAddr, compactReq.ClientActionID)
				continue
			}
			targetSessionID := strings.TrimSpace(firstNonEmptyString(compactReq.SessionID, selectedSessionID))
			if targetSessionID == "" {
				emit(protocol.NewCompactResultEvent(selectedSessionID, false, "请先创建或加载会话后再执行 Compact"))
				continue
			}
			if targetSessionID != strings.TrimSpace(selectedSessionID) {
				switchRuntimeSession(targetSessionID, runtimeSwitchDetachPrevious)
			}
			emitAndPersist := emitAndPersistFor(targetSessionID)
			sessionRuntime, service := runtimeForSession(targetSessionID)
			if sessionRuntime != nil {
				service.SetSink(sessionRuntime.EnsureBufferedSinkWithProcessor(emitAndPersist))
			}
			compactMeta := protocol.MergeRuntimeMeta(compactReq.RuntimeMeta, protocol.RuntimeMeta{
				CWD:              compactReq.CWD,
				Engine:           compactReq.Engine,
				PermissionMode:   compactReq.PermissionMode,
				CodexSandboxMode: compactReq.CodexSandboxMode,
			})
			if !h.tryStartAsyncAction(ctx, gatewayAsyncActionOptions{
				Action:       "compact",
				SessionID:    targetSessionID,
				ConnectionID: connectionID,
				RemoteAddr:   remoteAddr,
				OnBusy: func() {
					message := "compact is already running for this session"
					logx.Warn("ws", "reject concurrent compact: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, targetSessionID, remoteAddr)
					emit(protocol.NewCompactResultEvent(targetSessionID, false, message))
				},
				OnPanic: func(any, string) {
					emit(protocol.NewCompactResultEvent(targetSessionID, false, "internal server error"))
				},
				Run: func() {
					if err := service.Compact(ctx, targetSessionID, compactMeta, emitAndPersist); err != nil {
						logx.Error("ws", "handle compact failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, targetSessionID, remoteAddr, err)
						emit(protocol.NewCompactResultEvent(targetSessionID, false, err.Error()))
						return
					}
					emit(protocol.NewCompactResultEvent(targetSessionID, true, ""))
				},
			}) {
				continue
			}
		case "fs_list":
			var fsListReq protocol.FSListRequestEvent
			if err := json.Unmarshal(payloadBytes, &fsListReq); err != nil {
				logx.Warn("ws", "invalid fs_list request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid fs_list request: %v", err), ""))
				continue
			}
			result, err := listDirectory(selectedSessionID, fsListReq.Path)
			if err != nil {
				logx.Warn("ws", "list directory failed: connectionID=%s sessionID=%s remoteAddr=%s path=%q err=%v", connectionID, selectedSessionID, remoteAddr, fsListReq.Path, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("list directory: %v", err), ""))
				continue
			}
			emit(result)
		case "fs_read":
			var fsReadReq protocol.FSReadRequestEvent
			if err := json.Unmarshal(payloadBytes, &fsReadReq); err != nil {
				logx.Warn("ws", "invalid fs_read request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid fs_read request: %v", err), ""))
				continue
			}
			result, err := readFile(selectedSessionID, fsReadReq.Path)
			if err != nil {
				logx.Warn("ws", "read file failed: connectionID=%s sessionID=%s remoteAddr=%s path=%q err=%v", connectionID, selectedSessionID, remoteAddr, fsReadReq.Path, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("read file: %v", err), ""))
				continue
			}
			emit(result)
		case "fs_write":
			var fsWriteReq protocol.FSWriteRequestEvent
			if err := json.Unmarshal(payloadBytes, &fsWriteReq); err != nil {
				logx.Warn("ws", "invalid fs_write request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid fs_write request: %v", err), ""))
				continue
			}
			result, err := writeFile(selectedSessionID, fsWriteReq.Path, fsWriteReq.Content)
			if err != nil {
				logx.Warn("ws", "write file failed: connectionID=%s sessionID=%s remoteAddr=%s path=%q err=%v", connectionID, selectedSessionID, remoteAddr, fsWriteReq.Path, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("write file: %v", err), ""))
				continue
			}
			emit(result)
		case "media_preview":
			var previewReq protocol.MediaPreviewRequestEvent
			if err := json.Unmarshal(payloadBytes, &previewReq); err != nil {
				logx.Warn("ws", "invalid media_preview request: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, selectedSessionID, remoteAddr, err)
				emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("invalid media_preview request: %v", err), ""))
				continue
			}
			result := readMediaPreview(selectedSessionID, previewReq.AttachmentID, previewReq.Path)
			if result.Status != "ok" {
				logx.Warn("ws", "media preview failed: connectionID=%s sessionID=%s remoteAddr=%s attachmentID=%q path=%q status=%q message=%q", connectionID, selectedSessionID, remoteAddr, previewReq.AttachmentID, previewReq.Path, result.Status, result.Message)
			}
			emit(result)
		default:
			logx.Warn("ws", "unknown action: connectionID=%s sessionID=%s remoteAddr=%s action=%s", connectionID, selectedSessionID, remoteAddr, clientEvent.Action)
			emit(protocol.NewErrorEvent(selectedSessionID, fmt.Sprintf("unknown action: %s", clientEvent.Action), ""))
		}
	}

}

func appendUserProjectionEntry(projection data.ProjectionSnapshot, sessionID, text, label, connectionID, remoteAddr string, attachments []protocol.TimelineAttachment) (data.ProjectionSnapshot, bool) {
	if strings.TrimSpace(sessionID) == "" || (strings.TrimSpace(text) == "" && len(attachments) == 0) {
		if strings.TrimSpace(sessionID) == "" {
			logx.Warn("ws", "skip append user projection entry because session id is empty: connectionID=%s remoteAddr=%s", connectionID, remoteAddr)
		}
		return projection, false
	}
	projection = session.NormalizeProjectionSnapshot(projection)
	projection.LogEntries = append(projection.LogEntries, data.SnapshotLogEntry{
		Kind:        "user",
		Message:     text,
		Label:       label,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Attachments: append([]protocol.TimelineAttachment(nil), attachments...),
	})
	return projection, true
}

func finalizeProjectionSnapshotForRuntimeService(sessionID string, service *session.Service, loaded data.ProjectionSnapshot) data.ProjectionSnapshot {
	projection := loaded
	if strings.TrimSpace(sessionID) != "" && service != nil {
		projection = session.WithRuntimeSnapshot(projection, service)
	}
	if diff := loaded.CurrentDiff; diff != nil {
		projection.CurrentDiff = diff
	}
	if len(projection.Diffs) == 0 && projection.CurrentDiff != nil {
		projection.Diffs = []data.DiffContext{*projection.CurrentDiff}
	}
	return session.NormalizeProjectionSnapshot(projection)
}

func mergeProjectionSnapshotsForCache(current data.ProjectionSnapshot, incoming data.ProjectionSnapshot) data.ProjectionSnapshot {
	current = session.NormalizeProjectionSnapshot(current)
	incoming = session.NormalizeProjectionSnapshot(incoming)
	merged := incoming
	merged.LogEntries = mergeSnapshotLogEntries(current.LogEntries, incoming.LogEntries)
	merged.RawTerminalByStream = mergeRawTerminalByStream(current.RawTerminalByStream, incoming.RawTerminalByStream)
	merged.TerminalExecutions = mergeTerminalExecutions(current.TerminalExecutions, incoming.TerminalExecutions)
	merged.Controller = session.MergeControllerSnapshot(current.Controller, incoming.Controller)
	merged.Runtime = session.MergeStoreSessionRuntime(current.Runtime, incoming.Runtime)
	if incoming.CurrentDiff == nil && current.CurrentDiff != nil {
		merged.CurrentDiff = current.CurrentDiff
	}
	if len(incoming.Diffs) == 0 && len(current.Diffs) > 0 {
		merged.Diffs = current.Diffs
	}
	if len(incoming.ReviewGroups) == 0 && len(current.ReviewGroups) > 0 {
		merged.ReviewGroups = current.ReviewGroups
	}
	if incoming.ActiveReviewGroup == nil && current.ActiveReviewGroup != nil {
		merged.ActiveReviewGroup = current.ActiveReviewGroup
	}
	if incoming.CurrentStep == nil && current.CurrentStep != nil {
		merged.CurrentStep = current.CurrentStep
	}
	if incoming.LatestError == nil && current.LatestError != nil {
		merged.LatestError = current.LatestError
	}
	if incoming.ContextWindowUsage.TokenLimit <= 0 && current.ContextWindowUsage.TokenLimit > 0 {
		merged.ContextWindowUsage = current.ContextWindowUsage
	}
	incomingSessionContextSet := incoming.SessionContextSet || incoming.SessionContext.Configured
	currentSessionContextSet := current.SessionContextSet || current.SessionContext.Configured
	if !incomingSessionContextSet && currentSessionContextSet {
		merged.SessionContext = current.SessionContext
		merged.SessionContextSet = true
	}
	if len(incoming.PermissionRules) == 0 && len(current.PermissionRules) > 0 {
		merged.PermissionRules = current.PermissionRules
	}
	if !incoming.PermissionRulesEnabled && current.PermissionRulesEnabled && len(incoming.PermissionRules) == 0 {
		merged.PermissionRulesEnabled = current.PermissionRulesEnabled
	}
	if incoming.SkillCatalogMeta.SyncState == "" && current.SkillCatalogMeta.SyncState != "" {
		merged.SkillCatalogMeta = current.SkillCatalogMeta
	}
	if incoming.MemoryCatalogMeta.SyncState == "" && current.MemoryCatalogMeta.SyncState != "" {
		merged.MemoryCatalogMeta = current.MemoryCatalogMeta
	}
	return session.NormalizeProjectionSnapshot(merged)
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

func projectionWithResumeRuntimeMeta(projection data.ProjectionSnapshot, meta protocol.RuntimeMeta) (data.ProjectionSnapshot, bool) {
	next := session.NormalizeProjectionSnapshot(projection)
	runtime := next.Runtime
	runtime = session.MergeStoreSessionRuntime(runtime, data.SessionRuntime{
		ResumeSessionID:  meta.ResumeSessionID,
		Command:          meta.Command,
		Engine:           meta.Engine,
		PermissionMode:   meta.PermissionMode,
		CodexSandboxMode: meta.CodexSandboxMode,
		CWD:              meta.CWD,
		ClaudeLifecycle:  meta.ClaudeLifecycle,
		Source:           meta.Source,
	})
	if runtime == next.Runtime {
		return next, false
	}
	next.Runtime = runtime
	return session.NormalizeProjectionSnapshot(next), true
}

func wsDebugPreview(value string) string {
	trimmed := redactLogSecrets(value)
	trimmed = strings.ReplaceAll(strings.TrimSpace(trimmed), "\n", `\n`)
	trimmed = strings.ReplaceAll(trimmed, "\r", `\r`)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= wsDebugPreviewLimit {
		return trimmed
	}
	return string(runes[:wsDebugPreviewLimit]) + "…"
}

func redactLogSecrets(value string) string {
	redacted := value
	for _, pattern := range secretLogPatterns {
		redacted = pattern.re.ReplaceAllString(redacted, pattern.replacement)
	}
	return redacted
}

func wsDebugBoolLabel(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func readProjectionFromSessionStore(sessionStore data.Store, ctx context.Context, sessionID, connectionID, remoteAddr string) data.ProjectionSnapshot {
	record, ok := readSessionRecordForProjection(sessionStore, ctx, sessionID, connectionID, remoteAddr)
	if !ok {
		return data.ProjectionSnapshot{RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""}}
	}
	augmented := augmentLocalClaudeRecordFromNative(ctx, record)
	return augmented.Projection
}

func readProjectionFromSessionStoreRaw(sessionStore data.Store, ctx context.Context, sessionID, connectionID, remoteAddr string) data.ProjectionSnapshot {
	record, ok := readSessionRecordForProjection(sessionStore, ctx, sessionID, connectionID, remoteAddr)
	if !ok {
		return data.ProjectionSnapshot{RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""}}
	}
	return record.Projection
}

func readSessionRecordForProjection(sessionStore data.Store, ctx context.Context, sessionID, connectionID, remoteAddr string) (data.SessionRecord, bool) {
	if sessionStore == nil || strings.TrimSpace(sessionID) == "" {
		if sessionStore == nil {
			logx.Warn("ws", "projection restore skipped because session store unavailable: connectionID=%s sessionID=%s remoteAddr=%s", connectionID, sessionID, remoteAddr)
		}
		return data.SessionRecord{}, false
	}
	record, err := sessionStore.GetSession(ctx, sessionID)
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		fallbackCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		record, err = sessionStore.GetSession(fallbackCtx, sessionID)
	}
	if err != nil {
		logx.Warn("ws", "read projection from session store failed: connectionID=%s sessionID=%s remoteAddr=%s err=%v", connectionID, sessionID, remoteAddr, err)
		return data.SessionRecord{}, false
	}
	return record, true
}

func mergeProjectionWithOptionalRuntime(base data.ProjectionSnapshot, runtimeProjection data.ProjectionSnapshot, svc *session.Service, sessionID string) data.ProjectionSnapshot {
	base = session.NormalizeProjectionSnapshot(base)
	runtimeProjection = session.NormalizeProjectionSnapshot(runtimeProjection)
	if svc == nil {
		return base
	}
	runtimeSnapshot := svc.RuntimeSnapshot()
	runtimeActiveForSession := runtimeSnapshot.Running && strings.TrimSpace(runtimeSnapshot.ActiveSession) == strings.TrimSpace(sessionID)
	if !runtimeActiveForSession {
		return base
	}
	merged := mergeNativeMirrorProjection(base, runtimeProjection, nativeMirrorSourceForSessionID(sessionID))
	merged.LogEntries = mergeCodexMirrorLogEntries(base.LogEntries, runtimeProjection.LogEntries)
	return session.NormalizeProjectionSnapshot(merged)
}

func nativeMirrorSourceForSessionID(sessionID string) string {
	switch {
	case claudesync.IsMirrorSessionID(sessionID):
		return "claude-native"
	case codexsync.IsMirrorSessionID(sessionID):
		return "codex-native"
	default:
		return "mobilevc"
	}
}

func isNativeMirrorSessionID(sessionID string) bool {
	return claudesync.IsMirrorSessionID(sessionID) || codexsync.IsMirrorSessionID(sessionID)
}

func codexMirrorCacheMatchesSource(record data.SessionRecord) bool {
	record = data.SessionRecord{
		Summary:       record.Summary,
		Projection:    session.NormalizeProjectionSnapshot(record.Projection),
		ClientActions: record.ClientActions,
	}
	if !codexsync.IsMirrorSessionID(record.Summary.ID) {
		return false
	}
	runtime := session.MergeStoreSessionRuntime(record.Summary.Runtime, record.Projection.Runtime)
	if strings.TrimSpace(runtime.SourcePath) == "" || runtime.SourceSize <= 0 || runtime.SourceModUnixNS <= 0 {
		return false
	}
	if runtime.SourceEntryCount <= 0 || runtime.SourceEntryCount != record.Summary.EntryCount {
		return false
	}
	info, err := os.Stat(runtime.SourcePath)
	if err != nil {
		return false
	}
	return info.Size() == runtime.SourceSize && info.ModTime().UnixNano() == runtime.SourceModUnixNS
}

func sessionRecordRuntimeAlive(record data.SessionRecord, svc *session.Service) bool {
	allowStoredRuntime := record.Summary.External ||
		codexsync.IsMirrorSessionID(record.Summary.ID) ||
		claudesync.IsMirrorSessionID(record.Summary.ID)
	return session.SessionRecordRuntimeAlive(record, svc, allowStoredRuntime)
}

func taskCursorSnapshot(sessionRuntime *runtimeSession) session.TaskCursorSnapshot {
	if sessionRuntime == nil {
		return session.TaskCursorSnapshot{}
	}
	return session.TaskCursorSnapshot{
		LatestCursor: sessionRuntime.latestCursor(),
		LastOutputAt: sessionRuntime.lastOutputTime(),
	}
}

func deltaCursorSnapshot(sessionRuntime *runtimeSession) session.DeltaCursorSnapshot {
	if sessionRuntime == nil {
		return session.DeltaCursorSnapshot{}
	}
	return session.DeltaCursorSnapshot{LatestCursor: sessionRuntime.latestCursor()}
}

func toProtocolSummary(item data.SessionSummary) protocol.SessionSummary {
	return session.ToProtocolSummary(item)
}

func toProtocolSummaries(items []data.SessionSummary) []protocol.SessionSummary {
	result := make([]protocol.SessionSummary, 0, len(items))
	for _, item := range items {
		result = append(result, toProtocolSummary(item))
	}
	return result
}

func cloneSessionSummaries(items []data.SessionSummary) []data.SessionSummary {
	if items == nil {
		return nil
	}
	cloned := make([]data.SessionSummary, len(items))
	copy(cloned, items)
	return cloned
}

func cloneProjectionSnapshot(snapshot data.ProjectionSnapshot) data.ProjectionSnapshot {
	cloned := snapshot
	cloned.Diffs = append([]data.DiffContext(nil), snapshot.Diffs...)
	if snapshot.CurrentDiff != nil {
		diff := *snapshot.CurrentDiff
		cloned.CurrentDiff = &diff
	}
	cloned.ReviewGroups = cloneReviewGroups(snapshot.ReviewGroups)
	if snapshot.ActiveReviewGroup != nil {
		group := cloneReviewGroup(*snapshot.ActiveReviewGroup)
		cloned.ActiveReviewGroup = &group
	}
	if snapshot.CurrentStep != nil {
		step := *snapshot.CurrentStep
		cloned.CurrentStep = &step
	}
	if snapshot.LatestError != nil {
		err := *snapshot.LatestError
		cloned.LatestError = &err
	}
	cloned.LogEntries = cloneSnapshotLogEntries(snapshot.LogEntries)
	if snapshot.RawTerminalByStream != nil {
		cloned.RawTerminalByStream = make(map[string]string, len(snapshot.RawTerminalByStream))
		for key, value := range snapshot.RawTerminalByStream {
			cloned.RawTerminalByStream[key] = value
		}
	}
	cloned.TerminalExecutions = append([]data.TerminalExecution(nil), snapshot.TerminalExecutions...)
	cloned.Controller.RecentDiffs = append([]data.DiffContext(nil), snapshot.Controller.RecentDiffs...)
	cloned.Controller.ResolvedPermissionIDs = append([]string(nil), snapshot.Controller.ResolvedPermissionIDs...)
	cloned.SessionContext.EnabledSkillNames = append([]string(nil), snapshot.SessionContext.EnabledSkillNames...)
	cloned.SessionContext.EnabledMemoryIDs = append([]string(nil), snapshot.SessionContext.EnabledMemoryIDs...)
	cloned.PermissionRules = append([]data.PermissionRule(nil), snapshot.PermissionRules...)
	return cloned
}

func cloneSnapshotLogEntries(items []data.SnapshotLogEntry) []data.SnapshotLogEntry {
	if items == nil {
		return nil
	}
	cloned := make([]data.SnapshotLogEntry, len(items))
	for i, item := range items {
		cloned[i] = item
		if item.Context != nil {
			ctx := *item.Context
			cloned[i].Context = &ctx
		}
		cloned[i].Attachments = append([]protocol.TimelineAttachment(nil), item.Attachments...)
	}
	return cloned
}

func cloneReviewGroups(items []data.ReviewGroup) []data.ReviewGroup {
	if items == nil {
		return nil
	}
	cloned := make([]data.ReviewGroup, len(items))
	for i, item := range items {
		cloned[i] = cloneReviewGroup(item)
	}
	return cloned
}

func cloneReviewGroup(item data.ReviewGroup) data.ReviewGroup {
	item.Files = append([]data.ReviewFile(nil), item.Files...)
	return item
}

func toProtocolCatalogMetadata(meta data.CatalogMetadata) protocol.CatalogMetadata {
	return session.ToProtocolCatalogMetadata(meta)
}

func toProtocolSessionContext(ctx data.SessionContext) protocol.SessionContext {
	return session.ToProtocolSessionContext(ctx)
}

func toProtocolSkillDefinitions(items []data.SkillDefinition) []protocol.SkillDefinition {
	result := make([]protocol.SkillDefinition, 0, len(items))
	for _, item := range items {
		updatedAt := ""
		if !item.UpdatedAt.IsZero() {
			updatedAt = item.UpdatedAt.Format(time.RFC3339)
		}
		lastSyncedAt := ""
		if !item.LastSyncedAt.IsZero() {
			lastSyncedAt = item.LastSyncedAt.Format(time.RFC3339)
		}
		result = append(result, protocol.SkillDefinition{
			Name:          item.Name,
			Description:   item.Description,
			Prompt:        item.Prompt,
			ResultView:    item.ResultView,
			TargetType:    item.TargetType,
			Source:        string(item.Source),
			SourceOfTruth: string(item.SourceOfTruth),
			SyncState:     string(item.SyncState),
			Editable:      item.Editable,
			DriftDetected: item.DriftDetected,
			UpdatedAt:     updatedAt,
			LastSyncedAt:  lastSyncedAt,
		})
	}
	return result
}

func toProtocolMemoryItems(items []data.MemoryItem) []protocol.MemoryItem {
	result := make([]protocol.MemoryItem, 0, len(items))
	for _, item := range items {
		updatedAt := ""
		if !item.UpdatedAt.IsZero() {
			updatedAt = item.UpdatedAt.Format(time.RFC3339)
		}
		lastSyncedAt := ""
		if !item.LastSyncedAt.IsZero() {
			lastSyncedAt = item.LastSyncedAt.Format(time.RFC3339)
		}
		result = append(result, protocol.MemoryItem{
			ID:            item.ID,
			Title:         item.Title,
			Content:       item.Content,
			Source:        item.Source,
			SourceOfTruth: string(item.SourceOfTruth),
			SyncState:     string(item.SyncState),
			Editable:      item.Editable,
			DriftDetected: item.DriftDetected,
			UpdatedAt:     updatedAt,
			LastSyncedAt:  lastSyncedAt,
		})
	}
	return result
}

func loadSelectedSessionRecord(sessionStore data.Store, ctx context.Context, sessionID string, emit func(any)) (data.SessionRecord, bool) {
	if sessionStore == nil {
		emit(protocol.NewErrorEvent(sessionID, "session store unavailable", ""))
		return data.SessionRecord{}, false
	}
	record, err := sessionStore.GetSession(ctx, sessionID)
	if err != nil {
		emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
		return data.SessionRecord{}, false
	}
	return record, true
}

func emitSkillCatalogResult(emit func(any), sessionStore data.Store, ctx context.Context, sessionID string) {
	if sessionStore == nil {
		emit(protocol.NewErrorEvent(sessionID, "session store unavailable", ""))
		return
	}
	snapshot, err := sessionStore.GetSkillCatalogSnapshot(ctx)
	if err != nil {
		emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
		return
	}
	emit(protocol.NewSkillCatalogResultEvent(sessionID, toProtocolCatalogMetadata(snapshot.Meta), toProtocolSkillDefinitions(snapshot.Items)))
}

func emitMemoryListResult(emit func(any), sessionStore data.Store, ctx context.Context, sessionID string) {
	if sessionStore == nil {
		emit(protocol.NewErrorEvent(sessionID, "session store unavailable", ""))
		return
	}
	snapshot, err := sessionStore.GetMemoryCatalogSnapshot(ctx)
	if err != nil {
		emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
		return
	}
	emit(protocol.NewMemoryListResultEvent(sessionID, toProtocolCatalogMetadata(snapshot.Meta), toProtocolMemoryItems(snapshot.Items)))
}

func findRuntimeProcessItem(items []protocol.RuntimeProcessItem, pid int) (protocol.RuntimeProcessItem, bool) {
	for _, item := range items {
		if item.PID == pid {
			return item, true
		}
	}
	return protocol.RuntimeProcessItem{}, false
}

func isSessionTerminalExecutionNotFoundError(err error, executionID, sessionID string) bool {
	if err == nil {
		return false
	}
	executionID = strings.TrimSpace(executionID)
	sessionID = strings.TrimSpace(sessionID)
	if executionID == "" || sessionID == "" {
		return false
	}
	return strings.Contains(err.Error(), fmt.Sprintf("terminal execution %s not found for session %s", executionID, sessionID))
}

func upsertLocalSkill(sessionStore data.Store, ctx context.Context, item protocol.SkillDefinition) error {
	if sessionStore == nil {
		return fmt.Errorf("session store unavailable")
	}
	snapshot, err := sessionStore.GetSkillCatalogSnapshot(ctx)
	if err != nil {
		return err
	}
	updatedAt := time.Now().UTC()
	next := data.SkillDefinition{
		Name:          strings.TrimSpace(item.Name),
		Description:   strings.TrimSpace(item.Description),
		Prompt:        strings.TrimSpace(item.Prompt),
		ResultView:    strings.TrimSpace(item.ResultView),
		TargetType:    strings.TrimSpace(item.TargetType),
		Source:        data.SkillSourceLocal,
		SourceOfTruth: data.CatalogSourceTruthClaude,
		SyncState:     data.CatalogSyncStateDraft,
		Editable:      true,
		DriftDetected: true,
		UpdatedAt:     updatedAt,
	}
	if next.Name == "" {
		return fmt.Errorf("skill name is required")
	}
	found := false
	for i := range snapshot.Items {
		if snapshot.Items[i].Name == next.Name {
			snapshot.Items[i] = next
			found = true
			break
		}
	}
	if !found {
		snapshot.Items = append(snapshot.Items, next)
	}
	snapshot.Meta.SyncState = data.CatalogSyncStateDraft
	snapshot.Meta.DriftDetected = true
	snapshot.Meta.SourceOfTruth = data.CatalogSourceTruthClaude
	snapshot.Meta.LastError = ""
	return sessionStore.SaveSkillCatalogSnapshot(ctx, snapshot)
}

func syncExternalSkills(sessionStore data.Store, ctx context.Context, sourceOfTruth data.CatalogSourceOfTruth) error {
	if sessionStore == nil {
		return fmt.Errorf("session store unavailable")
	}
	snapshot, err := sessionStore.GetSkillCatalogSnapshot(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	externalItems, err := loadExternalSkillDefinitions(sourceOfTruth, now)
	if err != nil {
		return err
	}
	filtered := make([]data.SkillDefinition, 0, len(snapshot.Items)+len(externalItems))
	seen := make(map[string]struct{}, len(snapshot.Items)+len(externalItems))
	for _, item := range snapshot.Items {
		if item.Source == data.SkillSourceLocal {
			filtered = append(filtered, item)
			seen[item.Name] = struct{}{}
		}
	}
	for _, item := range externalItems {
		if _, ok := seen[item.Name]; ok {
			continue
		}
		filtered = append(filtered, item)
	}
	snapshot.Items = filtered
	snapshot.Meta.SourceOfTruth = sourceOfTruth
	snapshot.Meta.SyncState = data.CatalogSyncStateSynced
	snapshot.Meta.DriftDetected = catalogHasDraftSkill(filtered)
	snapshot.Meta.LastSyncedAt = now
	snapshot.Meta.LastError = ""
	snapshot.Meta.VersionToken = fmt.Sprintf("skills-%d", now.UnixNano())
	return sessionStore.SaveSkillCatalogSnapshot(ctx, snapshot)
}

func (h *Handler) runSkillSyncPull(ctx context.Context, sessionID string, emit func(any)) {
	if h == nil || h.SessionStore == nil {
		emit(protocol.NewErrorEvent(sessionID, "session store unavailable", ""))
		return
	}
	sourceOfTruth := resolveCatalogSourceOfTruth(h.SessionStore, ctx, sessionID)
	snapshot, err := h.SessionStore.GetSkillCatalogSnapshot(ctx)
	if err != nil {
		emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
		return
	}
	snapshot.Meta.SourceOfTruth = sourceOfTruth
	snapshot.Meta.SyncState = data.CatalogSyncStateSyncing
	snapshot.Meta.LastError = ""
	if err := h.SessionStore.SaveSkillCatalogSnapshot(ctx, snapshot); err != nil {
		emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
		return
	}
	emit(protocol.NewCatalogSyncStatusEvent(sessionID, string(data.CatalogDomainSkill), toProtocolCatalogMetadata(snapshot.Meta)))
	if err := syncExternalSkills(h.SessionStore, ctx, sourceOfTruth); err != nil {
		snapshot.Meta.SyncState = data.CatalogSyncStateFailed
		snapshot.Meta.LastError = err.Error()
		if saveErr := h.SessionStore.SaveSkillCatalogSnapshot(ctx, snapshot); saveErr != nil {
			logx.Error("ws", "save failed skill sync state failed: sessionID=%s err=%v", sessionID, saveErr)
		}
		emit(protocol.NewCatalogSyncResultEvent(sessionID, string(data.CatalogDomainSkill), false, err.Error(), toProtocolCatalogMetadata(snapshot.Meta)))
		emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
		return
	}
	h.replaceSkillLauncher(skills.NewLauncher(h.SessionStore))
	updatedSnapshot, err := h.SessionStore.GetSkillCatalogSnapshot(ctx)
	if err != nil {
		emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
		return
	}
	emit(protocol.NewSkillSyncResultEvent(sessionID, "skill 同步完成"))
	emit(protocol.NewCatalogSyncResultEvent(sessionID, string(data.CatalogDomainSkill), true, "skill 同步完成", toProtocolCatalogMetadata(updatedSnapshot.Meta)))
	emitSkillCatalogResult(emit, h.SessionStore, ctx, sessionID)
}

func upsertMemoryItem(sessionStore data.Store, ctx context.Context, item protocol.MemoryItem, cwd string) error {
	if sessionStore == nil {
		return fmt.Errorf("session store unavailable")
	}
	snapshot, err := sessionStore.GetMemoryCatalogSnapshot(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	next := data.MemoryItem{
		ID:            strings.TrimSpace(item.ID),
		Title:         strings.TrimSpace(item.Title),
		Content:       strings.TrimSpace(item.Content),
		Source:        firstNonEmptyString(strings.TrimSpace(item.Source), "local"),
		SourceOfTruth: data.CatalogSourceTruthClaude,
		SyncState:     data.CatalogSyncStateDraft,
		Editable:      true,
		DriftDetected: true,
		UpdatedAt:     now,
	}
	if next.ID == "" {
		next.ID = fmt.Sprintf("memory-%d", now.UnixNano())
	}
	if next.Title == "" {
		return fmt.Errorf("memory title is required")
	}
	found := false
	for i := range snapshot.Items {
		if snapshot.Items[i].ID == next.ID {
			next.LastSyncedAt = snapshot.Items[i].LastSyncedAt
			snapshot.Items[i] = next
			found = true
			break
		}
	}
	if !found {
		snapshot.Items = append(snapshot.Items, next)
	}
	snapshot.Meta.SourceOfTruth = data.CatalogSourceTruthClaude
	snapshot.Meta.SyncState = data.CatalogSyncStateDraft
	snapshot.Meta.DriftDetected = true
	snapshot.Meta.LastError = ""
	if err := sessionStore.SaveMemoryCatalogSnapshot(ctx, snapshot); err != nil {
		return err
	}

	// Write to disk for local items
	if next.Source == "local" {
		if writeErr := writeMemoryItemToDisk(next, cwd); writeErr != nil {
			logx.Warn("ws", "failed to write memory to disk: id=%s cwd=%s err=%v", next.ID, cwd, writeErr)
		}
	}

	return nil
}

func writeMemoryItemToDisk(item data.MemoryItem, cwd string) error {
	if item.Source != "local" {
		return nil
	}

	memoryDir, err := findClaudeProjectMemoryDir(cwd)
	if err != nil {
		return fmt.Errorf("find memory dir: %w", err)
	}

	if memoryDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home dir: %w", err)
		}
		encodedPath := encodeClaudeProjectPath(cwd)
		memoryDir = filepath.Join(homeDir, ".claude", "projects", encodedPath, "memory")
	}

	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		return fmt.Errorf("create memory dir: %w", err)
	}

	filePath := filepath.Join(memoryDir, item.ID+".md")

	var content strings.Builder
	content.WriteString("---\n")
	content.WriteString(fmt.Sprintf("name: %s\n", item.ID))
	if item.Title != "" {
		content.WriteString(fmt.Sprintf("description: %s\n", item.Title))
	}
	content.WriteString("---\n")
	if item.Content != "" {
		content.WriteString(item.Content)
		if !strings.HasSuffix(item.Content, "\n") {
			content.WriteString("\n")
		}
	}

	if err := os.WriteFile(filePath, []byte(content.String()), 0644); err != nil {
		return fmt.Errorf("write memory file: %w", err)
	}

	return nil
}

func syncExternalMemories(sessionStore data.Store, ctx context.Context, cwd string, sourceOfTruth data.CatalogSourceOfTruth) error {
	if sessionStore == nil {
		return fmt.Errorf("session store unavailable")
	}
	snapshot, err := sessionStore.GetMemoryCatalogSnapshot(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	externalItems, err := loadExternalMemoryItems(sourceOfTruth, cwd, now)
	if err != nil {
		return err
	}
	filtered := make([]data.MemoryItem, 0, len(snapshot.Items)+len(externalItems))
	seen := make(map[string]struct{}, len(snapshot.Items)+len(externalItems))
	for _, item := range snapshot.Items {
		if item.Source == "local" {
			filtered = append(filtered, item)
			seen[item.ID] = struct{}{}
		}
	}
	for _, item := range externalItems {
		if _, ok := seen[item.ID]; ok {
			continue
		}
		filtered = append(filtered, item)
	}
	snapshot.Items = filtered
	snapshot.Meta.SourceOfTruth = sourceOfTruth
	snapshot.Meta.SyncState = data.CatalogSyncStateSynced
	snapshot.Meta.DriftDetected = catalogHasDraftMemory(filtered)
	snapshot.Meta.LastSyncedAt = now
	snapshot.Meta.LastError = ""
	snapshot.Meta.VersionToken = fmt.Sprintf("memory-%d", now.UnixNano())
	return sessionStore.SaveMemoryCatalogSnapshot(ctx, snapshot)
}

func (h *Handler) runMemorySyncPull(ctx context.Context, sessionID, syncCWD, connectionID, remoteAddr string, emit func(any)) {
	if h == nil || h.SessionStore == nil {
		emit(protocol.NewErrorEvent(sessionID, "session store unavailable", ""))
		return
	}
	sourceOfTruth := resolveCatalogSourceOfTruth(h.SessionStore, ctx, sessionID)
	snapshot, err := h.SessionStore.GetMemoryCatalogSnapshot(ctx)
	if err != nil {
		emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
		return
	}
	snapshot.Meta.SourceOfTruth = sourceOfTruth
	snapshot.Meta.SyncState = data.CatalogSyncStateSyncing
	snapshot.Meta.LastError = ""
	if err := h.SessionStore.SaveMemoryCatalogSnapshot(ctx, snapshot); err != nil {
		emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
		return
	}
	emit(protocol.NewCatalogSyncStatusEvent(sessionID, string(data.CatalogDomainMemory), toProtocolCatalogMetadata(snapshot.Meta)))
	logx.Info("ws", "memory sync pull: connectionID=%s sessionID=%s remoteAddr=%s syncCWD=%q sourceOfTruth=%s", connectionID, sessionID, remoteAddr, syncCWD, sourceOfTruth)
	if err := syncExternalMemories(h.SessionStore, ctx, syncCWD, sourceOfTruth); err != nil {
		snapshot.Meta.SyncState = data.CatalogSyncStateFailed
		snapshot.Meta.LastError = err.Error()
		if saveErr := h.SessionStore.SaveMemoryCatalogSnapshot(ctx, snapshot); saveErr != nil {
			logx.Error("ws", "save failed memory sync state failed: sessionID=%s err=%v", sessionID, saveErr)
		}
		emit(protocol.NewCatalogSyncResultEvent(sessionID, string(data.CatalogDomainMemory), false, err.Error(), toProtocolCatalogMetadata(snapshot.Meta)))
		emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
		return
	}
	updatedSnapshot, err := h.SessionStore.GetMemoryCatalogSnapshot(ctx)
	if err != nil {
		emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
		return
	}
	emit(protocol.NewCatalogSyncResultEvent(sessionID, string(data.CatalogDomainMemory), true, "memory 同步完成", toProtocolCatalogMetadata(updatedSnapshot.Meta)))
	emitMemoryListResult(emit, h.SessionStore, ctx, sessionID)
}

func resolveCatalogSyncCWD(sessionStore data.Store, ctx context.Context, sessionID, fallbackCWD string) string {
	if sessionStore != nil && strings.TrimSpace(sessionID) != "" {
		if metaStore, ok := sessionStore.(data.SessionRuntimeMetaStore); ok {
			meta, err := metaStore.GetSessionRuntimeMetadata(ctx, sessionID)
			if err == nil {
				record := meta.Record
				if cwd := normalizeSessionCWD(record.Projection.Runtime.CWD); cwd != "" {
					return cwd
				}
				if cwd := normalizeSessionCWD(record.Summary.Runtime.CWD); cwd != "" {
					return cwd
				}
			}
		}
		// Catalog sync is not a selected-session bootstrap path. Non-side-domain
		// stores keep the legacy full-record lookup because they have no runtime
		// metadata reader to derive cwd from.
		if _, ok := sessionStore.(data.SessionRuntimeMetaStore); !ok {
			record, err := sessionStore.GetSession(ctx, sessionID)
			if err == nil {
				if cwd := normalizeSessionCWD(record.Projection.Runtime.CWD); cwd != "" {
					return cwd
				}
				if cwd := normalizeSessionCWD(record.Summary.Runtime.CWD); cwd != "" {
					return cwd
				}
			}
		}
	}
	return normalizeSessionCWD(fallbackCWD)
}

func resolveCatalogSourceOfTruth(sessionStore data.Store, ctx context.Context, sessionID string) data.CatalogSourceOfTruth {
	if sessionStore == nil || strings.TrimSpace(sessionID) == "" {
		return data.CatalogSourceTruthClaude
	}
	if metaStore, ok := sessionStore.(data.SessionRuntimeMetaStore); ok {
		meta, err := metaStore.GetSessionRuntimeMetadata(ctx, sessionID)
		if err != nil {
			return data.CatalogSourceTruthClaude
		}
		record := meta.Record
		if isCodexRuntime(record.Projection.Runtime) || isCodexRuntime(record.Summary.Runtime) || strings.EqualFold(strings.TrimSpace(record.Summary.Source), "codex-native") {
			return data.CatalogSourceTruthCodex
		}
		return data.CatalogSourceTruthClaude
	}
	// Catalog sync is outside selected-session bootstrap. This fallback is kept
	// only for non-side-domain stores that cannot expose runtime metadata.
	record, err := sessionStore.GetSession(ctx, sessionID)
	if err != nil {
		return data.CatalogSourceTruthClaude
	}
	if isCodexRuntime(record.Projection.Runtime) || isCodexRuntime(record.Summary.Runtime) || strings.EqualFold(strings.TrimSpace(record.Summary.Source), "codex-native") {
		return data.CatalogSourceTruthCodex
	}
	return data.CatalogSourceTruthClaude
}

func isCodexRuntime(runtime data.SessionRuntime) bool {
	if strings.EqualFold(strings.TrimSpace(runtime.Source), "codex-native") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(runtime.Engine), "codex") {
		return true
	}
	head := strings.ToLower(strings.TrimSpace(commandHead(runtime.Command)))
	return head == "codex" || strings.HasSuffix(head, "/codex") || strings.HasSuffix(head, `\codex`) || head == "codex.exe"
}

func isClaudeRuntime(runtime data.SessionRuntime) bool {
	if strings.EqualFold(strings.TrimSpace(runtime.Source), "claude-native") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(runtime.Engine), "claude") {
		return true
	}
	head := strings.ToLower(strings.TrimSpace(commandHead(runtime.Command)))
	return head == "claude" || strings.HasSuffix(head, "/claude") || strings.HasSuffix(head, `\claude`) || head == "claude.exe"
}

func permissionRequestIDFromEvent(event any) string {
	if e, ok := event.(protocol.PromptRequestEvent); ok {
		return strings.TrimSpace(e.PermissionRequestID)
	}
	return ""
}

func isResolvedPermissionID(resolvedIDs []string, requestID string) bool {
	for _, id := range resolvedIDs {
		if strings.TrimSpace(id) == requestID {
			return true
		}
	}
	return false
}

func commandHead(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func loadExternalSkillDefinitions(sourceOfTruth data.CatalogSourceOfTruth, now time.Time) ([]data.SkillDefinition, error) {
	switch sourceOfTruth {
	case data.CatalogSourceTruthCodex:
		return loadCodexSkillDefinitions(now)
	default:
		return loadClaudeSkillDefinitions(now)
	}
}

func loadExternalMemoryItems(sourceOfTruth data.CatalogSourceOfTruth, cwd string, now time.Time) ([]data.MemoryItem, error) {
	switch sourceOfTruth {
	case data.CatalogSourceTruthCodex:
		return loadCodexMemories(now)
	default:
		return loadClaudeProjectMemories(cwd, now)
	}
}

func loadClaudeSkillDefinitions(now time.Time) ([]data.SkillDefinition, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir for skill sync: %w", err)
	}
	return loadSkillsFromRoots(
		[]string{
			filepath.Join(homeDir, ".claude", "skills"),
			filepath.Join(homeDir, ".agents", "skills"),
			filepath.Join(homeDir, ".agent", "skills"),
		},
		now,
		data.CatalogSourceTruthClaude,
	)
}

func loadCodexSkillDefinitions(now time.Time) ([]data.SkillDefinition, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir for codex skill sync: %w", err)
	}
	return loadSkillsFromRoots(
		[]string{
			filepath.Join(homeDir, ".codex", "skills"),
			filepath.Join(homeDir, ".agents", "skills"),
			filepath.Join(homeDir, ".agent", "skills"),
		},
		now,
		data.CatalogSourceTruthCodex,
	)
}

func loadSkillsFromRoots(roots []string, now time.Time, sourceOfTruth data.CatalogSourceOfTruth) ([]data.SkillDefinition, error) {
	entries := make([]data.SkillDefinition, 0)
	seen := make(map[string]struct{})
	for _, root := range roots {
		rootEntries, err := loadSkillsFromRoot(root, now, sourceOfTruth)
		if err != nil {
			return nil, err
		}
		for _, item := range rootEntries {
			if _, ok := seen[item.Name]; ok {
				continue
			}
			seen[item.Name] = struct{}{}
			entries = append(entries, item)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

func loadSkillsFromRoot(root string, now time.Time, sourceOfTruth data.CatalogSourceOfTruth) ([]data.SkillDefinition, error) {
	entries := make([]data.SkillDefinition, 0)
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.EqualFold(d.Name(), "SKILL.md") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("read skill file %s: %w", path, err)
		}
		meta, body := parseMarkdownFrontMatter(string(content))
		name := firstNonEmptyString(strings.TrimSpace(meta["name"]), strings.TrimSpace(filepath.Base(filepath.Dir(path))))
		if name == "" {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat skill file %s: %w", path, err)
		}
		entries = append(entries, data.SkillDefinition{
			Name:          name,
			Description:   strings.TrimSpace(meta["description"]),
			Prompt:        strings.TrimSpace(body),
			ResultView:    firstNonEmptyString(strings.TrimSpace(meta["resultview"]), "review-card"),
			TargetType:    firstNonEmptyString(strings.TrimSpace(meta["targettype"]), "context"),
			Source:        data.SkillSourceExternal,
			SourceOfTruth: sourceOfTruth,
			SyncState:     data.CatalogSyncStateSynced,
			Editable:      false,
			DriftDetected: false,
			UpdatedAt:     info.ModTime().UTC(),
			LastSyncedAt:  now,
		})
		return nil
	})
	if walkErr != nil {
		if errors.Is(walkErr, os.ErrNotExist) {
			return nil, nil
		}
		if sourceOfTruth == data.CatalogSourceTruthCodex {
			return nil, fmt.Errorf("read codex skills dir: %w", walkErr)
		}
		return nil, fmt.Errorf("read claude skills dir: %w", walkErr)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

func loadClaudeProjectMemories(cwd string, now time.Time) ([]data.MemoryItem, error) {
	memoryDir, err := findClaudeProjectMemoryDir(cwd)
	if err != nil {
		return nil, err
	}
	logx.Info("ws", "load claude project memories: cwd=%q memoryDir=%q", cwd, memoryDir)
	if memoryDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(memoryDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read claude memory dir: %w", err)
	}
	items := make([]data.MemoryItem, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		path := filepath.Join(memoryDir, name)
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read claude memory %s: %w", name, err)
		}
		if hasBinaryContent(content) {
			continue
		}
		meta, body := parseMarkdownFrontMatter(string(content))
		id := strings.TrimSuffix(name, filepath.Ext(name))
		title := firstNonEmptyString(
			strings.TrimSpace(meta["title"]),
			strings.TrimSpace(meta["name"]),
			extractMarkdownTitle(body),
			id,
		)
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat claude memory %s: %w", name, err)
		}
		items = append(items, data.MemoryItem{
			ID:            id,
			Title:         title,
			Content:       strings.TrimSpace(body),
			Source:        "claude-project-memory",
			SourceOfTruth: data.CatalogSourceTruthClaude,
			SyncState:     data.CatalogSyncStateSynced,
			Editable:      false,
			DriftDetected: false,
			UpdatedAt:     info.ModTime().UTC(),
			LastSyncedAt:  now,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	logx.Info("ws", "loaded claude project memories: cwd=%q memoryDir=%q count=%d ids=%v", cwd, memoryDir, len(items), ids)
	return items, nil
}

func loadCodexMemories(now time.Time) ([]data.MemoryItem, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir for codex memory sync: %w", err)
	}
	root := filepath.Join(homeDir, ".codex", "memories")
	items := make([]data.MemoryItem, 0)
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		lower := strings.ToLower(d.Name())
		if !strings.HasSuffix(lower, ".md") && !strings.HasSuffix(lower, ".txt") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read codex memory %s: %w", d.Name(), err)
		}
		if hasBinaryContent(content) {
			return nil
		}
		meta, body := parseMarkdownFrontMatter(string(content))
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = d.Name()
		}
		id := strings.TrimSuffix(filepath.ToSlash(rel), filepath.Ext(rel))
		id = strings.ReplaceAll(id, "/", "-")
		title := firstNonEmptyString(
			strings.TrimSpace(meta["title"]),
			strings.TrimSpace(meta["name"]),
			extractMarkdownTitle(body),
			id,
		)
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat codex memory %s: %w", d.Name(), err)
		}
		items = append(items, data.MemoryItem{
			ID:            id,
			Title:         title,
			Content:       strings.TrimSpace(body),
			Source:        "codex-memory",
			SourceOfTruth: data.CatalogSourceTruthCodex,
			SyncState:     data.CatalogSyncStateSynced,
			Editable:      false,
			DriftDetected: false,
			UpdatedAt:     info.ModTime().UTC(),
			LastSyncedAt:  now,
		})
		return nil
	})
	if walkErr != nil {
		if errors.Is(walkErr, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read codex memory dir: %w", walkErr)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func findClaudeProjectMemoryDir(cwd string) (string, error) {
	startVariants := pathLookupVariants(cwd)
	if len(startVariants) == 0 {
		return "", nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir for memory sync: %w", err)
	}
	stopVariants := pathLookupVariants(homeDir)
	checked := make(map[string]struct{})
	for _, start := range startVariants {
		current := start
		for {
			if sameAnyPath(current, stopVariants) {
				break
			}
			candidate := filepath.Join(homeDir, ".claude", "projects", encodeClaudeProjectPath(current), "memory")
			if _, seen := checked[candidate]; !seen {
				checked[candidate] = struct{}{}
				info, err := os.Stat(candidate)
				if err == nil && info.IsDir() {
					return candidate, nil
				}
			}
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	return "", nil
}

func sameFilePath(left, right string) bool {
	return filepath.Clean(strings.TrimSpace(left)) == filepath.Clean(strings.TrimSpace(right))
}

func sameAnyPath(path string, candidates []string) bool {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "" || cleaned == "." {
		return false
	}
	for _, candidate := range candidates {
		if sameFilePath(cleaned, candidate) {
			return true
		}
	}
	return false
}

func pathLookupVariants(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if absPath, err := filepath.Abs(trimmed); err == nil {
		trimmed = absPath
	}
	addVariant := func(items *[]string, seen map[string]struct{}, value string) {
		cleaned := filepath.Clean(strings.TrimSpace(value))
		if cleaned == "" || cleaned == "." {
			return
		}
		if _, ok := seen[cleaned]; ok {
			return
		}
		seen[cleaned] = struct{}{}
		*items = append(*items, cleaned)
	}
	variants := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	addVariant(&variants, seen, trimmed)
	addPrivatePathAliasVariants(&variants, seen, trimmed)
	if resolved, err := filepath.EvalSymlinks(trimmed); err == nil && strings.TrimSpace(resolved) != "" {
		addVariant(&variants, seen, resolved)
		addPrivatePathAliasVariants(&variants, seen, resolved)
	}
	return variants
}

func addPrivatePathAliasVariants(items *[]string, seen map[string]struct{}, value string) {
	cleaned := filepath.Clean(strings.TrimSpace(value))
	if cleaned == "" || cleaned == "." {
		return
	}
	if strings.HasPrefix(cleaned, "/private/") {
		alias := strings.TrimPrefix(cleaned, "/private")
		if alias == "" {
			alias = string(os.PathSeparator)
		}
		if _, err := os.Stat(alias); err == nil {
			cleanedAlias := filepath.Clean(alias)
			if _, ok := seen[cleanedAlias]; !ok {
				seen[cleanedAlias] = struct{}{}
				*items = append(*items, cleanedAlias)
			}
		}
		return
	}
	alias := filepath.Join(string(os.PathSeparator), "private", strings.TrimPrefix(cleaned, string(os.PathSeparator)))
	if _, err := os.Stat(alias); err == nil {
		cleanedAlias := filepath.Clean(alias)
		if _, ok := seen[cleanedAlias]; !ok {
			seen[cleanedAlias] = struct{}{}
			*items = append(*items, cleanedAlias)
		}
	}
}

func encodeClaudeProjectPath(path string) string {
	normalized := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	if normalized == "" || normalized == "." {
		return ""
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "-")
	encoded := replacer.Replace(normalized)
	if !strings.HasPrefix(encoded, "-") {
		encoded = "-" + encoded
	}
	return encoded
}

func parseMarkdownFrontMatter(content string) (map[string]string, string) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return nil, normalized
	}
	end := strings.Index(normalized[4:], "\n---\n")
	if end < 0 {
		return nil, normalized
	}
	rawFrontMatter := normalized[4 : 4+end]
	body := normalized[4+end+5:]
	meta := make(map[string]string)
	for _, line := range strings.Split(rawFrontMatter, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		meta[key] = value
	}
	return meta, strings.TrimSpace(body)
}

func extractMarkdownTitle(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		if title != "" {
			return title
		}
	}
	return ""
}

func catalogHasDraftSkill(items []data.SkillDefinition) bool {
	for _, item := range items {
		if item.Source == data.SkillSourceLocal &&
			(item.SyncState != data.CatalogSyncStateSynced || item.DriftDetected) {
			return true
		}
	}
	return false
}

func catalogHasDraftMemory(items []data.MemoryItem) bool {
	for _, item := range items {
		if item.Source == "local" &&
			(item.SyncState != data.CatalogSyncStateSynced || item.DriftDetected) {
			return true
		}
	}
	return false
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func defaultAICommandFromEngine(values ...string) string {
	for _, value := range values {
		switch strings.TrimSpace(strings.ToLower(value)) {
		case "codex":
			return "codex"
		case "gemini":
			return "gemini"
		case "claude":
			return "claude"
		}
	}
	return "claude"
}

func applyAICommandPreferences(command, engine, model, reasoningEffort string) string {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		trimmed = defaultAICommandFromEngine(engine)
	}
	normalizedEngine := strings.TrimSpace(strings.ToLower(firstNonEmptyString(engine, commandHead(trimmed))))
	fields := strings.Fields(trimmed)
	lower := strings.ToLower(trimmed)
	switch normalizedEngine {
	case "claude":
		model = strings.TrimSpace(model)
		if strings.EqualFold(model, "default") {
			return strings.Join(removeCommandFlagValue(fields, "-m", "--model"), " ")
		}
		if model == "" || strings.Contains(lower, " --model ") || strings.Contains(lower, " -m ") {
			return trimmed
		}
		return trimmed + " --model " + shellQuote(model)
	case "codex":
		parts := append([]string(nil), fields...)
		model = strings.TrimSpace(model)
		if strings.EqualFold(model, "default") {
			parts = removeCommandFlagValue(parts, "-m", "--model")
		} else if model != "" {
			parts = upsertCommandFlagValue(parts, model, "-m", "--model")
		}
		effort := strings.TrimSpace(strings.ToLower(reasoningEffort))
		if effort != "" {
			parts = upsertCodexReasoningEffort(parts, effort)
		}
		return strings.Join(parts, " ")
	default:
		return trimmed
	}
}

func upsertCommandFlagValue(fields []string, value string, flags ...string) []string {
	for i, field := range fields {
		for _, flag := range flags {
			if field == flag {
				if i+1 < len(fields) {
					fields[i+1] = value
					return fields
				}
				return append(fields, value)
			}
			if strings.HasPrefix(field, flag+"=") {
				fields[i] = flag + "=" + value
				return fields
			}
		}
	}
	return append(fields, flags[0], value)
}

func removeCommandFlagValue(fields []string, flags ...string) []string {
	if len(fields) == 0 {
		return fields
	}
	filtered := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		removed := false
		for _, flag := range flags {
			if field == flag {
				removed = true
				if i+1 < len(fields) {
					i++
				}
				break
			}
			if strings.HasPrefix(field, flag+"=") {
				removed = true
				break
			}
		}
		if !removed {
			filtered = append(filtered, field)
		}
	}
	return filtered
}

func upsertCodexReasoningEffort(fields []string, effort string) []string {
	configValue := "model_reasoning_effort=" + effort
	for i, field := range fields {
		lower := strings.ToLower(field)
		if field == "--config" && i+1 < len(fields) && strings.Contains(strings.ToLower(fields[i+1]), "model_reasoning_effort=") {
			fields[i+1] = configValue
			return fields
		}
		if strings.HasPrefix(lower, "--config=model_reasoning_effort=") {
			fields[i] = "--config=" + configValue
			return fields
		}
		if strings.Contains(lower, "model_reasoning_effort=") {
			fields[i] = configValue
			return fields
		}
	}
	return append(fields, "--config", configValue)
}

func commandHasFlag(fields []string, flags ...string) bool {
	for _, field := range fields {
		for _, flag := range flags {
			if field == flag || strings.HasPrefix(field, flag+"=") {
				return true
			}
		}
	}
	return false
}

func commandHasCodexReasoningEffort(fields []string) bool {
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), "model_reasoning_effort=") {
			return true
		}
	}
	return false
}

func shellQuote(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "''"
	}
	if !strings.ContainsAny(trimmed, " \t\n'\"\\$`!#&;|<>*?()[]{}") {
		return trimmed
	}
	return "'" + strings.ReplaceAll(trimmed, "'", `'\''`) + "'"
}

func shouldInjectEnabledSkillsForInput(command string, engines ...string) bool {
	if isAISessionCommandLike(command) {
		return true
	}
	for _, engine := range engines {
		switch strings.TrimSpace(strings.ToLower(engine)) {
		case "claude", "codex", "gemini":
			return true
		}
	}
	return false
}

func isAISessionCommandLike(command string) bool {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return false
	}
	head := strings.ToLower(strings.TrimSpace(fields[0]))
	return head == "claude" ||
		strings.HasSuffix(head, "/claude") ||
		strings.HasSuffix(head, `\\claude`) ||
		head == "claude.exe" ||
		head == "codex" ||
		strings.HasSuffix(head, "/codex") ||
		strings.HasSuffix(head, `\\codex`) ||
		head == "codex.exe" ||
		head == "gemini" ||
		strings.HasSuffix(head, "/gemini") ||
		strings.HasSuffix(head, `\\gemini`) ||
		head == "gemini.exe"
}

func emitReviewStateFromProjection(emit func(any), sessionID string, projection data.ProjectionSnapshot) {
	emit(session.ReviewStateEventFromProjection(sessionID, projection))
}

func restoredAgentStateEventFromRecord(record data.SessionRecord, hasActiveRunner bool) *protocol.AgentStateEvent {
	return session.RestoredAgentStateEventFromRecord(record, hasActiveRunner, isExternalNativeActiveRecord(record))
}

func isExternalNativeActiveRecord(record data.SessionRecord) bool {
	if !codexsync.IsMirrorSessionID(record.Summary.ID) && !claudesync.IsMirrorSessionID(record.Summary.ID) {
		if !record.Summary.External {
			return false
		}
	}
	projection := session.NormalizeProjectionSnapshot(record.Projection)
	state := strings.TrimSpace(string(projection.Controller.State))
	lifecycle := strings.TrimSpace(firstNonEmptyString(
		projection.Controller.ClaudeLifecycle,
		projection.Controller.ActiveMeta.ClaudeLifecycle,
		projection.Runtime.ClaudeLifecycle,
		record.Summary.Runtime.ClaudeLifecycle,
	))
	return session.IsBusyRuntimeState(state) || lifecycle == "active" || lifecycle == "starting"
}

func prepareSessionEventForResume(sessionRuntime *runtimeSession, sessionID string, event any) any {
	event = session.MarkSystemBootstrapEvent(event)
	if sessionRuntime == nil || strings.TrimSpace(sessionID) == "" {
		return event
	}
	event = sessionRuntime.appendPending(event)
	if sessionRuntime.listenerCount() == 0 {
		if notice := detachedResumeNoticeEvent(sessionID, event); notice != nil {
			sessionRuntime.appendPending(*notice)
		}
	}
	return event
}

func detachedResumeNoticeEvent(sessionID string, event any) *protocol.SessionResumeNoticeEvent {
	switch e := event.(type) {
	case protocol.LogEvent:
		if !session.IsVisibleAssistantReplyLog(e) {
			return nil
		}
		notice := protocol.NewSessionResumeNoticeEvent(sessionID, "assistant_reply", "info", "MobileVC", strings.TrimSpace(e.Message))
		notice.RuntimeMeta = protocol.MergeRuntimeMeta(notice.RuntimeMeta, e.RuntimeMeta)
		return &notice
	case protocol.ErrorEvent:
		if strings.TrimSpace(e.Message) == "" {
			return nil
		}
		if e.Code == "ws_closed" || e.Code == "ws_stream_error" {
			return nil
		}
		notice := protocol.NewSessionResumeNoticeEvent(sessionID, "error", "error", "MobileVC", strings.TrimSpace(e.Message))
		notice.RuntimeMeta = protocol.MergeRuntimeMeta(notice.RuntimeMeta, e.RuntimeMeta)
		return &notice
	default:
		return nil
	}
}

func looksLikeAssistantResumeNotice(event protocol.LogEvent) bool {
	return session.IsVisibleAssistantReplyLog(event)
}

func parseMode(raw string) (engine.Mode, error) {
	return session.ParseMode(raw)
}

func listDirectory(sessionID, rawPath string) (protocol.FSListResultEvent, error) {
	target := strings.TrimSpace(rawPath)
	if target == "" {
		target = "."
	}
	cleanTarget := filepath.Clean(target)
	absPath, err := filepath.Abs(cleanTarget)
	if err != nil {
		return protocol.FSListResultEvent{}, err
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return protocol.FSListResultEvent{}, err
	}

	items := make([]protocol.FSItem, 0, len(entries))
	for _, entry := range entries {
		item := protocol.FSItem{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
		}
		if info, err := entry.Info(); err == nil {
			item.Size = info.Size()
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})

	return protocol.NewFSListResultEvent(sessionID, absPath, items), nil
}

func readFile(sessionID, rawPath string) (protocol.FSReadResultEvent, error) {
	target := strings.TrimSpace(rawPath)
	if target == "" {
		return protocol.FSReadResultEvent{}, fmt.Errorf("path is required")
	}

	cleanTarget := filepath.Clean(target)
	absPath, err := filepath.Abs(cleanTarget)
	if err != nil {
		return protocol.FSReadResultEvent{}, err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return protocol.FSReadResultEvent{}, err
	}
	if info.IsDir() {
		return protocol.FSReadResultEvent{}, fmt.Errorf("path is a directory")
	}
	if info.Size() > maxInlineFileReadBytes {
		return protocol.FSReadResultEvent{}, fmt.Errorf("file exceeds inline read limit of %d bytes; use file download", maxInlineFileReadBytes)
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return protocol.FSReadResultEvent{}, err
	}

	isImage := isPreviewableImagePath(absPath)
	isText := !hasBinaryContent(content) && !isImage
	textContent := string(content)
	if isImage {
		if info.Size() > maxImageAttachmentBytes {
			return protocol.FSReadResultEvent{}, fmt.Errorf("image exceeds %d bytes", maxImageAttachmentBytes)
		}
		textContent = base64.StdEncoding.EncodeToString(content)
	} else if !isText {
		textContent = ""
	}
	return protocol.NewFSReadResultEvent(sessionID, absPath, textContent, info.Size(), detectLangFromPath(absPath), "utf-8", isText), nil
}

func writeFile(sessionID, rawPath, content string) (protocol.FSWriteResultEvent, error) {
	target := strings.TrimSpace(rawPath)
	if target == "" {
		return protocol.FSWriteResultEvent{}, fmt.Errorf("path is required")
	}
	if len([]byte(content)) > maxInlineFileReadBytes {
		return protocol.FSWriteResultEvent{}, fmt.Errorf("file exceeds inline write limit of %d bytes", maxInlineFileReadBytes)
	}

	cleanTarget := filepath.Clean(target)
	absPath, err := filepath.Abs(cleanTarget)
	if err != nil {
		return protocol.FSWriteResultEvent{}, err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return protocol.FSWriteResultEvent{}, err
	}
	if info.IsDir() {
		return protocol.FSWriteResultEvent{}, fmt.Errorf("path is a directory")
	}
	if isPreviewableImagePath(absPath) {
		return protocol.FSWriteResultEvent{}, fmt.Errorf("image files cannot be edited inline")
	}

	dir := filepath.Dir(absPath)
	tempFile, err := os.CreateTemp(dir, "."+filepath.Base(absPath)+".*.tmp")
	if err != nil {
		return protocol.FSWriteResultEvent{}, err
	}
	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := tempFile.WriteString(content); err != nil {
		_ = tempFile.Close()
		return protocol.FSWriteResultEvent{}, err
	}
	if err := tempFile.Chmod(info.Mode().Perm()); err != nil {
		_ = tempFile.Close()
		return protocol.FSWriteResultEvent{}, err
	}
	if err := tempFile.Close(); err != nil {
		return protocol.FSWriteResultEvent{}, err
	}
	if err := os.Rename(tempPath, absPath); err != nil {
		return protocol.FSWriteResultEvent{}, err
	}
	cleanupTemp = false

	writtenInfo, err := os.Stat(absPath)
	if err != nil {
		return protocol.FSWriteResultEvent{}, err
	}
	return protocol.NewFSWriteResultEvent(sessionID, absPath, content, writtenInfo.Size(), detectLangFromPath(absPath), "utf-8", true), nil
}

func readMediaPreview(sessionID, attachmentID, rawPath string) protocol.MediaPreviewResultEvent {
	target := strings.TrimSpace(rawPath)
	if target == "" {
		return protocol.NewMediaPreviewResultEvent(sessionID, attachmentID, "", "", 0, "", "error", "path is required")
	}
	absPath, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return protocol.NewMediaPreviewResultEvent(sessionID, attachmentID, target, "", 0, "", "error", err.Error())
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return protocol.NewMediaPreviewResultEvent(sessionID, attachmentID, absPath, "", 0, "", "error", err.Error())
	}
	if info.IsDir() {
		return protocol.NewMediaPreviewResultEvent(sessionID, attachmentID, absPath, "", 0, "", "unsupported", "path is a directory")
	}
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(absPath)))
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return protocol.NewMediaPreviewResultEvent(sessionID, attachmentID, absPath, "", info.Size(), mimeType, "unsupported", "file is not an image")
	}
	if info.Size() > maxImageAttachmentBytes {
		return protocol.NewMediaPreviewResultEvent(sessionID, attachmentID, absPath, "", info.Size(), mimeType, "unsupported", fmt.Sprintf("image exceeds %d bytes", maxImageAttachmentBytes))
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return protocol.NewMediaPreviewResultEvent(sessionID, attachmentID, absPath, "", info.Size(), mimeType, "error", err.Error())
	}
	return protocol.NewMediaPreviewResultEvent(
		sessionID,
		attachmentID,
		absPath,
		base64.StdEncoding.EncodeToString(content),
		info.Size(),
		mimeType,
		"ok",
		"",
	)
}

func isPreviewableImagePath(path string) bool {
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	return strings.HasPrefix(strings.ToLower(mimeType), "image/")
}

func detectLangFromPath(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch ext {
	case "js", "jsx":
		return "javascript"
	case "ts", "tsx":
		return "typescript"
	case "json":
		return "json"
	case "jsonc":
		return "jsonc"
	case "md":
		return "markdown"
	case "go":
		return "go"
	case "py":
		return "python"
	case "java":
		return "java"
	case "kt":
		return "kotlin"
	case "swift":
		return "swift"
	case "html":
		return "html"
	case "css", "scss":
		return ext
	case "yml", "yaml":
		return "yaml"
	case "xml":
		return "xml"
	case "sh", "bash":
		return "bash"
	case "sql":
		return "sql"
	case "txt":
		return "plaintext"
	default:
		return "plaintext"
	}
}

func hasBinaryContent(content []byte) bool {
	limit := len(content)
	if limit > 1024 {
		limit = 1024
	}
	for i := 0; i < limit; i++ {
		if content[i] == 0 {
			return true
		}
	}
	return false
}

func normalizeSessionCWD(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	trimmed = trimWindowsDevicePathPrefix(trimmed)
	absPath, err := filepath.Abs(trimmed)
	if err == nil {
		trimmed = absPath
	}
	if resolved, err := filepath.EvalSymlinks(trimmed); err == nil && strings.TrimSpace(resolved) != "" {
		trimmed = trimWindowsDevicePathPrefix(resolved)
	}
	return filepath.Clean(trimmed)
}

func trimWindowsDevicePathPrefix(value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, `\\?\UNC\`) {
		return `\\` + strings.TrimPrefix(trimmed, `\\?\UNC\`)
	}
	if strings.HasPrefix(trimmed, `\\?\`) {
		return strings.TrimPrefix(trimmed, `\\?\`)
	}
	return trimmed
}

func filterStoreSessionsByCWD(items []data.SessionSummary, filterCWD string) []data.SessionSummary {
	normalized := normalizeSessionCWD(filterCWD)
	if normalized == "" {
		return items
	}
	filtered := make([]data.SessionSummary, 0, len(items))
	for _, item := range items {
		cwd := normalizeSessionCWD(item.Runtime.CWD)
		if cwd == "" || cwd == normalized {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func isExternalCodexSummary(item data.SessionSummary) bool {
	return item.External || strings.EqualFold(strings.TrimSpace(item.Source), "codex-native")
}

func isExternalNativeSessionSummary(item data.SessionSummary) bool {
	return item.External ||
		strings.EqualFold(strings.TrimSpace(item.Source), "codex-native") ||
		strings.EqualFold(strings.TrimSpace(item.Source), "claude-native") ||
		claudesync.IsMirrorSessionID(item.ID) ||
		codexsync.IsMirrorSessionID(item.ID)
}

func lookupSessionSummaryForDelete(ctx context.Context, sessionStore data.Store, sessionID string) (data.SessionSummary, error) {
	if sessionStore == nil {
		return data.SessionSummary{}, fmt.Errorf("session store unavailable")
	}
	if summaryStore, ok := sessionStore.(data.SessionSummaryStore); ok {
		return summaryStore.GetSessionSummary(ctx, sessionID)
	}
	record, err := sessionStore.GetSession(ctx, sessionID)
	if err != nil {
		return data.SessionSummary{}, err
	}
	return record.Summary, nil
}

func summaryCodexThreadID(item data.SessionSummary) string {
	if resumeID := strings.TrimSpace(item.Runtime.ResumeSessionID); resumeID != "" {
		return resumeID
	}
	if codexsync.IsMirrorSessionID(item.ID) {
		return codexsync.ThreadIDFromMirror(item.ID)
	}
	return ""
}

func resolveTrackedCodexThreadID(ctx context.Context, sessionStore data.Store, item data.SessionSummary) string {
	if threadID := summaryCodexThreadID(item); threadID != "" {
		return threadID
	}
	if sessionStore == nil || strings.TrimSpace(item.ID) == "" {
		return ""
	}
	metaStore, ok := sessionStore.(data.SessionRuntimeMetaStore)
	if !ok {
		return ""
	}
	meta, err := metaStore.GetSessionRuntimeMetadata(ctx, item.ID)
	if err != nil {
		return ""
	}
	record := meta.Record
	if resumeID := strings.TrimSpace(record.Projection.Runtime.ResumeSessionID); resumeID != "" {
		return resumeID
	}
	if resumeID := strings.TrimSpace(record.Projection.Controller.ResumeSession); resumeID != "" {
		return resumeID
	}
	if codexsync.IsMirrorSessionID(record.Projection.Controller.SessionID) {
		return codexsync.ThreadIDFromMirror(record.Projection.Controller.SessionID)
	}
	return ""
}

func trackedMobileVCCodexThreads(ctx context.Context, sessionStore data.Store, items []data.SessionSummary) map[string]struct{} {
	tracked := make(map[string]struct{}, len(items))
	for _, item := range items {
		if isExternalCodexSummary(item) || !isCodexRuntime(item.Runtime) {
			continue
		}
		if threadID := resolveTrackedCodexThreadID(ctx, sessionStore, item); threadID != "" {
			tracked[threadID] = struct{}{}
		}
	}
	return tracked
}

func filterHiddenCodexSummaries(items []data.SessionSummary, hiddenThreadIDs map[string]struct{}) []data.SessionSummary {
	if len(items) == 0 || len(hiddenThreadIDs) == 0 {
		return items
	}
	filtered := make([]data.SessionSummary, 0, len(items))
	for _, item := range items {
		if isCodexNativeSummary(item) {
			if _, ok := hiddenThreadIDs[summaryCodexThreadID(item)]; ok {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func isCodexNativeSummary(item data.SessionSummary) bool {
	return codexsync.IsMirrorSessionID(item.ID) ||
		strings.EqualFold(strings.TrimSpace(item.Source), "codex-native") ||
		strings.EqualFold(strings.TrimSpace(item.Runtime.Source), "codex-native")
}

func preferCodexThreadSummary(current, candidate data.SessionSummary) data.SessionSummary {
	currentExternal := isExternalCodexSummary(current)
	candidateExternal := isExternalCodexSummary(candidate)
	if currentExternal != candidateExternal {
		if currentExternal {
			return candidate
		}
		return current
	}
	if candidate.UpdatedAt.After(current.UpdatedAt) {
		return candidate
	}
	if candidate.UpdatedAt.Equal(current.UpdatedAt) && candidate.CreatedAt.After(current.CreatedAt) {
		return candidate
	}
	return current
}

func dedupeCodexThreadSummaries(items []data.SessionSummary) []data.SessionSummary {
	if len(items) <= 1 {
		return items
	}
	selected := make(map[string]data.SessionSummary, len(items))
	order := make([]string, 0, len(items))
	for _, item := range items {
		key := item.ID
		if threadID := summaryCodexThreadID(item); threadID != "" && isCodexRuntime(item.Runtime) && isCodexNativeSummary(item) {
			key = "codex-thread:" + threadID
		}
		if existing, ok := selected[key]; ok {
			selected[key] = preferCodexThreadSummary(existing, item)
			continue
		}
		selected[key] = item
		order = append(order, key)
	}
	deduped := make([]data.SessionSummary, 0, len(selected))
	for _, key := range order {
		item, ok := selected[key]
		if ok {
			deduped = append(deduped, item)
		}
	}
	sort.Slice(deduped, func(i, j int) bool {
		return deduped[i].UpdatedAt.After(deduped[j].UpdatedAt)
	})
	return deduped
}

func isExternalClaudeSummary(item data.SessionSummary) bool {
	if item.External && strings.EqualFold(strings.TrimSpace(item.Source), "claude-native") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(item.Source), "claude-native") {
		return true
	}
	if claudesync.IsMirrorSessionID(item.ID) {
		return true
	}
	return false
}

func summaryClaudeSessionID(item data.SessionSummary) string {
	if claudesync.IsMirrorSessionID(item.ID) {
		return claudesync.SessionIDFromMirror(item.ID)
	}
	if isClaudeRuntime(item.Runtime) {
		if resumeID := strings.TrimSpace(item.Runtime.ResumeSessionID); resumeID != "" {
			return resumeID
		}
	}
	return ""
}

func resolveTrackedClaudeSessionID(ctx context.Context, sessionStore data.Store, item data.SessionSummary) string {
	if sid := strings.TrimSpace(item.ClaudeSessionUUID); sid != "" {
		return sid
	}
	if sid := summaryClaudeSessionID(item); sid != "" {
		return sid
	}
	if sessionStore == nil || strings.TrimSpace(item.ID) == "" {
		return ""
	}
	metaStore, ok := sessionStore.(data.SessionRuntimeMetaStore)
	if !ok {
		return ""
	}
	meta, err := metaStore.GetSessionRuntimeMetadata(ctx, item.ID)
	if err != nil {
		return ""
	}
	record := meta.Record
	if resumeID := strings.TrimSpace(record.Projection.Runtime.ResumeSessionID); resumeID != "" && isClaudeRuntime(record.Projection.Runtime) {
		return resumeID
	}
	if isClaudeRuntime(record.Projection.Runtime) {
		if resumeID := strings.TrimSpace(record.Projection.Controller.ResumeSession); resumeID != "" {
			return resumeID
		}
	}
	if claudesync.IsMirrorSessionID(record.Projection.Controller.SessionID) {
		return claudesync.SessionIDFromMirror(record.Projection.Controller.SessionID)
	}
	return ""
}

func trackedMobileVCClaudeSessions(ctx context.Context, sessionStore data.Store, items []data.SessionSummary) map[string]struct{} {
	tracked := make(map[string]struct{}, len(items))
	for _, item := range items {
		if isExternalClaudeSummary(item) {
			continue
		}
		if strings.TrimSpace(item.ClaudeSessionUUID) == "" && !isClaudeRuntime(item.Runtime) {
			continue
		}
		// Track ClaudeSessionUUID (for sessions created by MobileVC).
		if sid := strings.TrimSpace(item.ClaudeSessionUUID); sid != "" {
			tracked[sid] = struct{}{}
		}
		// Also track resumeSessionId (may differ from ClaudeSessionUUID
		// if the user ran "claude --session-id <other>").
		if sid := summaryClaudeSessionID(item); sid != "" {
			tracked[sid] = struct{}{}
		}
		if sid := resolveTrackedClaudeSessionID(ctx, sessionStore, item); sid != "" {
			tracked[sid] = struct{}{}
		}
	}
	return tracked
}

func mergeSessionSummaries(ctx context.Context, sessionStore data.Store, items []data.SessionSummary, filterCWD string) ([]data.SessionSummary, error) {
	filteredStoreItems := filterStoreSessionsByCWD(items, filterCWD)
	deletedNative := data.NativeSessionDeletionSet{}
	if deletionStore, ok := sessionStore.(data.NativeSessionDeletionStore); ok {
		var err error
		deletedNative, err = deletionStore.DeletedNativeSessionIDs(ctx)
		if err != nil {
			return nil, err
		}
	}
	hiddenThreadIDs, err := codexsync.ListNativeHiddenThreadIDs(ctx, filterCWD)
	if err != nil {
		logx.Warn("ws", "list hidden codex sessions failed: cwd=%q err=%v", filterCWD, err)
		hiddenThreadIDs = nil
	}
	filteredStoreItems = filterHiddenCodexSummaries(filteredStoreItems, hiddenThreadIDs)
	trackedThreads := trackedMobileVCCodexThreads(ctx, sessionStore, filteredStoreItems)
	trackedClaudeSessions := trackedMobileVCClaudeSessions(ctx, sessionStore, filteredStoreItems)
	nativeThreads, err := codexsync.ListNativeThreads(ctx, filterCWD)
	if err != nil {
		logx.Warn("ws", "list codex native sessions failed: cwd=%q err=%v", filterCWD, err)
		nativeThreads = nil
	}
	nativeClaude, err := claudesync.ListNativeSessions(ctx, filterCWD)
	if err != nil {
		logx.Warn("ws", "list claude native sessions failed: cwd=%q err=%v", filterCWD, err)
		nativeClaude = nil
	}
	merged := make([]data.SessionSummary, 0, len(filteredStoreItems)+len(nativeThreads)+len(nativeClaude))
	seen := make(map[string]struct{}, len(filteredStoreItems)+len(nativeThreads)+len(nativeClaude))
	for _, item := range filteredStoreItems {
		if isDeletedNativeSummary(item, deletedNative) {
			continue
		}
		if isExternalCodexSummary(item) {
			if _, ok := trackedThreads[summaryCodexThreadID(item)]; ok {
				continue
			}
		}
		if isExternalClaudeSummary(item) {
			if _, ok := trackedClaudeSessions[summaryClaudeSessionID(item)]; ok {
				continue
			}
		}
		if strings.TrimSpace(item.Source) == "" {
			item.Source = item.Runtime.Source
		}
		if strings.TrimSpace(item.Source) == "" {
			item.Source = "mobilevc"
		}
		merged = append(merged, item)
		seen[item.ID] = struct{}{}
	}
	for _, thread := range nativeThreads {
		if _, ok := deletedNative.CodexThreadIDs[strings.TrimSpace(thread.ThreadID)]; ok {
			continue
		}
		record := codexsync.MirrorRecord(thread)
		if _, ok := seen[record.Summary.ID]; ok {
			continue
		}
		if metaStore, ok := sessionStore.(data.SessionRuntimeMetaStore); ok {
			if stored, getErr := metaStore.GetSessionRuntimeMetadata(ctx, record.Summary.ID); getErr == nil {
				record = mergeNativeMirrorRecord(record, stored.Record, "codex-native")
			}
		}
		merged = append(merged, record.Summary)
		seen[record.Summary.ID] = struct{}{}
	}
	for _, native := range nativeClaude {
		if _, ok := deletedNative.ClaudeSessionIDs[strings.TrimSpace(native.SessionID)]; ok {
			continue
		}
		if _, ok := trackedClaudeSessions[strings.TrimSpace(native.SessionID)]; ok {
			continue
		}
		record := claudesync.MirrorRecord(native)
		if _, ok := seen[record.Summary.ID]; ok {
			continue
		}
		if metaStore, ok := sessionStore.(data.SessionRuntimeMetaStore); ok {
			if stored, getErr := metaStore.GetSessionRuntimeMetadata(ctx, record.Summary.ID); getErr == nil {
				record = mergeNativeMirrorRecord(record, stored.Record, "claude-native")
			}
		}
		merged = append(merged, record.Summary)
		seen[record.Summary.ID] = struct{}{}
	}
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].UpdatedAt.After(merged[j].UpdatedAt)
	})
	return dedupeCodexThreadSummaries(merged), nil
}

func isDeletedNativeSummary(item data.SessionSummary, deleted data.NativeSessionDeletionSet) bool {
	if isExternalClaudeSummary(item) {
		if _, ok := deleted.ClaudeSessionIDs[summaryClaudeSessionID(item)]; ok {
			return true
		}
	}
	if isCodexNativeSummary(item) {
		if _, ok := deleted.CodexThreadIDs[summaryCodexThreadID(item)]; ok {
			return true
		}
	}
	return false
}

func loadSessionRecord(ctx context.Context, sessionStore data.Store, sessionID string) (data.SessionRecord, error) {
	if sessionStore == nil {
		return data.SessionRecord{}, fmt.Errorf("session store unavailable")
	}
	if claudesync.IsMirrorSessionID(sessionID) {
		existing, existingErr := sessionStore.GetSession(ctx, sessionID)
		native, nativeErr := claudesync.FindNativeSession(ctx, sessionID)
		if nativeErr == nil {
			record := claudesync.MirrorRecord(native)
			if existingErr == nil {
				record = mergeNativeMirrorRecord(record, existing, "claude-native")
			}
			summary, upsertErr := sessionStore.UpsertSession(ctx, record)
			if upsertErr != nil {
				logx.Warn("ws", "upsert claude mirror session failed: sessionID=%s err=%v", sessionID, upsertErr)
				return record, nil
			}
			record.Summary = summary
			return record, nil
		}
		if existingErr == nil && existing.Summary.ID != "" {
			logx.Warn("ws", "claude sync failed but using cached record: sessionID=%s nativeErr=%v", sessionID, nativeErr)
			return existing, nil
		}
		return data.SessionRecord{}, fmt.Errorf("claude session not found and no valid cache: %w", nativeErr)
	}
	if !codexsync.IsMirrorSessionID(sessionID) {
		record, err := sessionStore.GetSession(ctx, sessionID)
		if err != nil {
			return record, err
		}
		return augmentLocalClaudeRecordFromNative(ctx, record), nil
	}
	existing, existingErr := sessionStore.GetSession(ctx, sessionID)
	thread, nativeErr := codexsync.FindNativeThread(ctx, sessionID)
	if nativeErr == nil {
		record := codexsync.MirrorRecord(thread)
		if existingErr == nil {
			record = mergeNativeMirrorRecord(record, existing, "codex-native")
		}
		summary, upsertErr := sessionStore.UpsertSession(ctx, record)
		if upsertErr != nil {
			logx.Warn("ws", "upsert codex mirror session failed: sessionID=%s err=%v", sessionID, upsertErr)
			return record, nil
		}
		record.Summary = summary
		return record, nil
	}
	if existingErr == nil && existing.Summary.ID != "" {
		logx.Warn("ws", "codex sync failed but using cached record: sessionID=%s nativeErr=%v", sessionID, nativeErr)
		return existing, nil
	}
	return data.SessionRecord{}, fmt.Errorf("codex session not found and no valid cache: %w", nativeErr)
}

func preferAugmentedLogEntries(augmented, current []data.SnapshotLogEntry) bool {
	if len(augmented) > len(current) {
		return true
	}
	aMd, cMd := 0, 0
	for _, e := range augmented {
		if e.Kind == "markdown" {
			aMd++
		}
	}
	for _, e := range current {
		if e.Kind == "markdown" {
			cMd++
		}
	}
	return aMd > cMd
}

func augmentLocalClaudeRecordFromNative(ctx context.Context, record data.SessionRecord) data.SessionRecord {
	if !isClaudeRuntime(record.Projection.Runtime) {
		return record
	}
	resumeID := strings.TrimSpace(record.Projection.Runtime.ResumeSessionID)
	if resumeID == "" {
		resumeID = strings.TrimSpace(record.Projection.Controller.ResumeSession)
	}
	if resumeID == "" {
		return record
	}
	hasMarkdown := false
	hasUser := false
	for _, entry := range record.Projection.LogEntries {
		switch entry.Kind {
		case "markdown":
			hasMarkdown = true
		case "user":
			hasUser = true
		}
	}
	if hasMarkdown {
		return record
	}
	if !hasUser {
		return record
	}
	native, err := claudesync.FindNativeSession(ctx, resumeID)
	if err != nil {
		return record
	}
	if len(native.LogEntries) == 0 {
		return record
	}
	logx.Info("ws", "augment local claude record from native jsonl: sessionID=%s resumeID=%s nativeEntries=%d", record.Summary.ID, resumeID, len(native.LogEntries))
	augmented := record
	augmented.Projection.LogEntries = append([]data.SnapshotLogEntry(nil), native.LogEntries...)
	if augmented.Summary.EntryCount < len(augmented.Projection.LogEntries) {
		augmented.Summary.EntryCount = len(augmented.Projection.LogEntries)
	}
	if trimmed := strings.TrimSpace(native.LastAssistantText); trimmed != "" && strings.TrimSpace(augmented.Summary.LastPreview) == "" {
		augmented.Summary.LastPreview = trimmed
	}
	return augmented
}

func mergeNativeMirrorRecord(fresh data.SessionRecord, existing data.SessionRecord, source string) data.SessionRecord {
	fresh.Projection = mergeNativeMirrorProjection(fresh.Projection, existing.Projection, source)
	fresh.Summary.CreatedAt = laterNonZeroTime(fresh.Summary.CreatedAt, existing.Summary.CreatedAt)
	fresh.Summary.UpdatedAt = laterNonZeroTime(fresh.Summary.UpdatedAt, existing.Summary.UpdatedAt)
	fresh.Summary.Runtime = session.MergeStoreSessionRuntime(data.SessionRuntime{}, fresh.Projection.Runtime)
	if fresh.Summary.Runtime.Source == "" {
		fresh.Summary.Runtime.Source = source
	}
	fresh.Summary.Source = source
	fresh.Summary.External = true
	fresh.Summary.Ownership = source
	fresh.ClientActions = existing.ClientActions
	return fresh
}

func preserveNativeMirrorLocalSettings(fresh data.ProjectionSnapshot, existing data.ProjectionSnapshot) data.ProjectionSnapshot {
	fresh = session.NormalizeProjectionSnapshot(fresh)
	existing = session.NormalizeProjectionSnapshot(existing)
	if len(existing.Diffs) > 0 {
		fresh.Diffs = existing.Diffs
	}
	if existing.CurrentDiff != nil {
		fresh.CurrentDiff = existing.CurrentDiff
	}
	if len(existing.ReviewGroups) > 0 {
		fresh.ReviewGroups = existing.ReviewGroups
	}
	if existing.ActiveReviewGroup != nil {
		fresh.ActiveReviewGroup = existing.ActiveReviewGroup
	}
	if existing.CurrentStep != nil {
		fresh.CurrentStep = existing.CurrentStep
	}
	if existing.LatestError != nil {
		fresh.LatestError = existing.LatestError
	}
	fresh.SessionContext = existing.SessionContext
	fresh.SessionContextSet = existing.SessionContext.Configured
	fresh.PermissionRulesEnabled = existing.PermissionRulesEnabled
	if len(existing.PermissionRules) > 0 {
		fresh.PermissionRules = existing.PermissionRules
	}
	fresh.SkillCatalogMeta = existing.SkillCatalogMeta
	fresh.MemoryCatalogMeta = existing.MemoryCatalogMeta

	return session.NormalizeProjectionSnapshot(fresh)
}

func mergeNativeMirrorProjection(fresh data.ProjectionSnapshot, existing data.ProjectionSnapshot, source string) data.ProjectionSnapshot {
	fresh = preserveNativeMirrorLocalSettings(fresh, existing)
	fresh.RawTerminalByStream = mergeRawTerminalByStream(fresh.RawTerminalByStream, existing.RawTerminalByStream)
	fresh.TerminalExecutions = mergeTerminalExecutions(fresh.TerminalExecutions, existing.TerminalExecutions)
	fresh.Controller = mergeNativeMirrorController(fresh.Controller, existing.Controller, source)
	fresh.Runtime = mergeNativeMirrorRuntime(fresh.Runtime, existing.Runtime, source)
	if fresh.ContextWindowUsage.TokenLimit <= 0 {
		fresh.ContextWindowUsage = existing.ContextWindowUsage
	}
	if fresh.Runtime.Source == "" {
		fresh.Runtime.Source = source
	}
	return session.NormalizeProjectionSnapshot(fresh)
}

func mergeNativeMirrorRuntime(fresh data.SessionRuntime, existing data.SessionRuntime, source string) data.SessionRuntime {
	merged := session.MergeStoreSessionRuntime(fresh, existing)
	expectedEngine := strings.TrimSuffix(source, "-native")
	if !strings.EqualFold(strings.TrimSpace(merged.Engine), expectedEngine) {
		merged.Engine = firstNonEmptyString(fresh.Engine, expectedEngine)
	}
	if source == "codex-native" && !isCodexMirrorRuntimeCommand(merged.Command) {
		merged.Command = firstNonEmptyString(fresh.Command, "codex")
	}
	if source == "claude-native" && !isClaudeMirrorRuntimeCommand(merged.Command) {
		merged.Command = firstNonEmptyString(fresh.Command, "claude")
	}
	if strings.TrimSpace(merged.ResumeSessionID) != strings.TrimSpace(fresh.ResumeSessionID) {
		merged.ResumeSessionID = fresh.ResumeSessionID
	}
	merged.SourcePath = fresh.SourcePath
	merged.SourceSize = fresh.SourceSize
	merged.SourceModUnixNS = fresh.SourceModUnixNS
	merged.SourceEntryCount = fresh.SourceEntryCount
	merged.Source = firstNonEmptyString(fresh.Source, merged.Source, source)
	return merged
}

func mergeNativeMirrorController(fresh session.ControllerSnapshot, existing session.ControllerSnapshot, source string) session.ControllerSnapshot {
	merged := session.MergeControllerSnapshot(fresh, existing)
	merged.ResumeSession = firstNonEmptyString(fresh.ResumeSession, merged.ResumeSession)
	merged.CurrentCommand = firstNonEmptyString(fresh.CurrentCommand, merged.CurrentCommand)
	merged.ActiveMeta = protocol.MergeRuntimeMeta(merged.ActiveMeta, protocol.RuntimeMeta{
		ResumeSessionID:  fresh.ActiveMeta.ResumeSessionID,
		Command:          fresh.ActiveMeta.Command,
		Engine:           fresh.ActiveMeta.Engine,
		CWD:              fresh.ActiveMeta.CWD,
		Source:           firstNonEmptyString(fresh.ActiveMeta.Source, source),
		SourcePath:       fresh.ActiveMeta.SourcePath,
		SourceSize:       fresh.ActiveMeta.SourceSize,
		SourceModUnixNS:  fresh.ActiveMeta.SourceModUnixNS,
		SourceEntryCount: fresh.ActiveMeta.SourceEntryCount,
	})
	expectedEngine := strings.TrimSuffix(source, "-native")
	if !strings.EqualFold(strings.TrimSpace(merged.ActiveMeta.Engine), expectedEngine) {
		merged.ActiveMeta.Engine = firstNonEmptyString(fresh.ActiveMeta.Engine, expectedEngine)
	}
	if source == "codex-native" && !isCodexMirrorRuntimeCommand(merged.ActiveMeta.Command) {
		merged.ActiveMeta.Command = firstNonEmptyString(fresh.ActiveMeta.Command, "codex")
	}
	if source == "claude-native" && !isClaudeMirrorRuntimeCommand(merged.ActiveMeta.Command) {
		merged.ActiveMeta.Command = firstNonEmptyString(fresh.ActiveMeta.Command, "claude")
	}
	if strings.TrimSpace(merged.ActiveMeta.ResumeSessionID) != strings.TrimSpace(fresh.ActiveMeta.ResumeSessionID) {
		merged.ActiveMeta.ResumeSessionID = fresh.ActiveMeta.ResumeSessionID
	}
	return merged
}

func isCodexMirrorRuntimeCommand(command string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(command))
	return trimmed == "codex" || strings.HasPrefix(trimmed, "codex ")
}

func isClaudeMirrorRuntimeCommand(command string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(command))
	return trimmed == "claude" || strings.HasPrefix(trimmed, "claude ")
}

func mergeSnapshotLogEntries(base []data.SnapshotLogEntry, overlay []data.SnapshotLogEntry) []data.SnapshotLogEntry {
	if len(base) == 0 {
		return append([]data.SnapshotLogEntry(nil), overlay...)
	}
	merged := append([]data.SnapshotLogEntry(nil), base...)
	seen := make(map[string]struct{}, len(base)+len(overlay))
	for _, entry := range base {
		seen[snapshotLogEntryKey(entry)] = struct{}{}
	}
	for _, entry := range overlay {
		key := snapshotLogEntryKey(entry)
		if _, ok := seen[key]; ok {
			continue
		}
		merged = append(merged, entry)
		seen[key] = struct{}{}
	}
	return merged
}

func mergeCodexMirrorLogEntries(base []data.SnapshotLogEntry, overlay []data.SnapshotLogEntry) []data.SnapshotLogEntry {
	return sortSnapshotLogEntriesByTime(mergeSnapshotLogEntries(base, overlay))
}

func sortSnapshotLogEntriesByTime(entries []data.SnapshotLogEntry) []data.SnapshotLogEntry {
	indexed := make([]struct {
		index int
		entry data.SnapshotLogEntry
		time  time.Time
	}, 0, len(entries))
	for i, entry := range entries {
		indexed = append(indexed, struct {
			index int
			entry data.SnapshotLogEntry
			time  time.Time
		}{
			index: i,
			entry: entry,
			time:  snapshotLogEntryTime(entry),
		})
	}
	sort.SliceStable(indexed, func(i, j int) bool {
		left := indexed[i]
		right := indexed[j]
		if left.time.IsZero() || right.time.IsZero() {
			return left.index < right.index
		}
		if left.time.Equal(right.time) {
			return left.index < right.index
		}
		return left.time.Before(right.time)
	})
	sorted := make([]data.SnapshotLogEntry, 0, len(indexed))
	for _, item := range indexed {
		sorted = append(sorted, item.entry)
	}
	return sorted
}

func snapshotLogEntryTime(entry data.SnapshotLogEntry) time.Time {
	for _, raw := range []string{
		strings.TrimSpace(entry.Timestamp),
		snapshotContextTimestamp(entry.Context),
	} {
		if raw == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return parsed
		}
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func snapshotContextTimestamp(context *data.SnapshotContext) string {
	if context == nil {
		return ""
	}
	return strings.TrimSpace(context.Timestamp)
}

func snapshotLogEntryKey(entry data.SnapshotLogEntry) string {
	contextKey := ""
	if entry.Context != nil {
		contextKey = strings.Join([]string{
			entry.Context.ID,
			entry.Context.Type,
			entry.Context.Message,
			entry.Context.Path,
			entry.Context.Title,
			entry.Context.Timestamp,
		}, "\x1f")
	}
	exitCode := ""
	if entry.ExitCode != nil {
		exitCode = fmt.Sprintf("%d", *entry.ExitCode)
	}
	return strings.Join([]string{
		entry.Kind,
		entry.Message,
		entry.Label,
		entry.Timestamp,
		entry.Stream,
		entry.Text,
		entry.ExecutionID,
		entry.Phase,
		exitCode,
		contextKey,
	}, "\x1f")
}

func mergeRawTerminalByStream(base map[string]string, overlay map[string]string) map[string]string {
	merged := map[string]string{"stdout": "", "stderr": ""}
	for _, stream := range []string{"stdout", "stderr"} {
		merged[stream] = firstNonEmptyString(overlay[stream], base[stream])
	}
	return merged
}

func mergeTerminalExecutions(base []data.TerminalExecution, overlay []data.TerminalExecution) []data.TerminalExecution {
	if len(base) == 0 {
		return append([]data.TerminalExecution(nil), overlay...)
	}
	merged := append([]data.TerminalExecution(nil), base...)
	indexByID := make(map[string]int, len(merged))
	for i, item := range merged {
		if strings.TrimSpace(item.ExecutionID) != "" {
			indexByID[item.ExecutionID] = i
		}
	}
	for _, item := range overlay {
		if id := strings.TrimSpace(item.ExecutionID); id != "" {
			if index, ok := indexByID[id]; ok {
				current := merged[index]
				merged[index] = data.TerminalExecution{
					ExecutionID: id,
					Command:     firstNonEmptyString(item.Command, current.Command),
					CWD:         firstNonEmptyString(item.CWD, current.CWD),
					StartedAt:   firstNonEmptyString(item.StartedAt, current.StartedAt),
					FinishedAt:  firstNonEmptyString(item.FinishedAt, current.FinishedAt),
					ExitCode:    firstNonNilInt(item.ExitCode, current.ExitCode),
					Stdout:      firstNonEmptyString(item.Stdout, current.Stdout),
					Stderr:      firstNonEmptyString(item.Stderr, current.Stderr),
				}
				continue
			}
			indexByID[id] = len(merged)
		}
		merged = append(merged, item)
	}
	return merged
}

func shouldTreatInputAsAICommand(data string) bool {
	fields := strings.Fields(strings.TrimSpace(data))
	if len(fields) == 0 {
		return false
	}
	if len(fields) > 1 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "codex", "claude", "gemini":
		return true
	default:
		return false
	}
}

func firstNonNilInt(values ...*int) *int {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func laterNonZeroTime(values ...time.Time) time.Time {
	var latest time.Time
	for _, value := range values {
		if value.IsZero() {
			continue
		}
		if latest.IsZero() || value.After(latest) {
			latest = value
		}
	}
	return latest
}

// lastUserPromptFromEntries returns the text of the most recent user entry.
func lastUserPromptFromEntries(entries []data.SnapshotLogEntry) string {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Kind == "user" {
			text := strings.TrimSpace(entries[i].Message)
			if text == "" {
				text = strings.TrimSpace(entries[i].Text)
			}
			if text != "" {
				return text
			}
		}
	}
	return ""
}

// mergeClaudeJSONLToRecord checks if the session has a linked Claude CLI JSONL
// file. If the JSONL has new entries (added by Claude CLI continuing the
// conversation), they are merged into the record's LogEntries and persisted.
func mergeClaudeJSONLToRecord(ctx context.Context, sessionStore data.Store, record data.SessionRecord, runtimeSvc *session.Service) data.SessionRecord {
	csuuid := strings.TrimSpace(record.Summary.ClaudeSessionUUID)
	if csuuid == "" {
		return record
	}
	cwd := strings.TrimSpace(record.Summary.Runtime.CWD)
	if cwd == "" {
		cwd = strings.TrimSpace(record.Projection.Runtime.CWD)
	}
	if cwd == "" {
		return record
	}
	newEntries, newCount, err := claudesync.MergeJSONLToSession(cwd, csuuid, record.Projection.LogEntries)
	if err != nil || len(newEntries) == 0 {
		// If no new entries but session was already external, re-evaluate
		// lifecycle based on recency of existing log entries.
		if record.Summary.External {
			latest := latestLogEntryTime(record.Projection.LogEntries)
			if !latest.IsZero() && time.Since(latest) < 60*time.Second {
				record.Projection.Runtime.ClaudeLifecycle = "active"
			} else {
				record.Projection.Runtime.ClaudeLifecycle = "resumable"
			}
		}
		return record
	}
	record.Projection.LogEntries = append(record.Projection.LogEntries, newEntries...)
	record.Summary.JSONLSyncEntryCount = newCount
	record.Summary.UpdatedAt = time.Now().UTC()

	// Only upgrade ownership when the session is NOT actively managed
	// by MobileVC's own runtime. If MobileVC launched the Claude CLI,
	// the JSONL entries are ours and ownership stays "mobilevc".
	runtimeManaged := false
	if runtimeSvc != nil {
		snapshot := runtimeSvc.RuntimeSnapshot()
		runtimeManaged = snapshot.Running && strings.TrimSpace(snapshot.ActiveSession) == strings.TrimSpace(record.Summary.ID)
	}
	// 守卫：会话之前已被明确标记为 mobilevc 所有时，即使 runtimeSvc 暂时不存活
	// （例如 server 进程刚重启、runner 尚未恢复），也不要因为 JSONL 出现新条目就升级
	// 为 claude-native。这些"新条目"通常是 mobilevc 自己之前的 runner 写下后未及时
	// 同步到 record 里的 catch-up，并非桌面端 Claude CLI 真的接管。
	previousOwnership := strings.ToLower(strings.TrimSpace(record.Summary.Ownership))
	mobilevcOwned := previousOwnership == "mobilevc"
	if !runtimeManaged && !mobilevcOwned {
		record.Summary.External = true
		record.Summary.Ownership = "claude-native"
		if record.Summary.Source == "" || record.Summary.Source == "mobilevc" {
			record.Summary.Source = "claude-native"
		}
		if record.Summary.Runtime.Source == "" || record.Summary.Runtime.Source == "mobilevc" {
			record.Summary.Runtime.Source = "claude-native"
		}
	} else if runtimeManaged {
		// Reset stale ownership when MobileVC runtime is actively managing
		// this session — undo any External/ownership from previous sessions.
		record.Summary.External = false
		record.Summary.Ownership = "mobilevc"
		if record.Summary.Runtime.Source == "" || record.Summary.Runtime.Source == "claude-native" {
			record.Summary.Runtime.Source = "mobilevc"
		}
	}

	// Set lifecycle based on recency of the latest entry.
	latest := latestLogEntryTime(record.Projection.LogEntries)
	if !latest.IsZero() && time.Since(latest) < 60*time.Second {
		record.Projection.Runtime.ClaudeLifecycle = "active"
	} else {
		record.Projection.Runtime.ClaudeLifecycle = "resumable"
	}

	saveCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := sessionStore.UpsertSession(saveCtx, record); err != nil {
		logx.Error("ws", "merge claude jsonl to session failed: sessionID=%s err=%v", record.Summary.ID, err)
	}
	return record
}

// latestLogEntryTime returns the most recent timestamp among the given log entries.
func latestLogEntryTime(entries []data.SnapshotLogEntry) time.Time {
	var latest time.Time
	for _, e := range entries {
		ts, err := time.Parse(time.RFC3339, e.Timestamp)
		if err != nil {
			continue
		}
		if ts.After(latest) {
			latest = ts
		}
	}
	return latest
}
