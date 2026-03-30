package ws

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"mobilevc/internal/protocol"
	"mobilevc/internal/runner"
	"mobilevc/internal/session"
	"mobilevc/internal/store"
)

type stubRunner struct {
	mu                        sync.Mutex
	events                    []any
	writeCh                   chan []byte
	writeErr                  error
	holdOpen                  bool
	interactive               bool
	started                   chan struct{}
	lastPermissionMode        string
	permissionModes           []string
	onStart                   func()
	hasPendingPermission      bool
	permissionResponseErr     error
	permissionResponseWriteCh chan string
	claudeSessionID           string
	lastReq                   runner.ExecRequest
	closedCh                  chan struct{}
}

func newStubRunner(events ...any) *stubRunner {
	return &stubRunner{
		events:                    events,
		writeCh:                   make(chan []byte, 8),
		started:                   make(chan struct{}),
		permissionResponseWriteCh: make(chan string, 8),
		closedCh:                  make(chan struct{}, 1),
	}
}

func newHoldingStubRunner(events ...any) *stubRunner {
	runner := newStubRunner(events...)
	runner.holdOpen = true
	runner.interactive = true
	runner.hasPendingPermission = true
	return runner
}

func newNonInteractiveHoldingStubRunner(events ...any) *stubRunner {
	runner := newStubRunner(events...)
	runner.holdOpen = true
	runner.interactive = false
	runner.hasPendingPermission = true
	return runner
}

func (s *stubRunner) Run(ctx context.Context, req runner.ExecRequest, sink runner.EventSink) error {
	s.mu.Lock()
	s.lastReq = req
	s.mu.Unlock()
	select {
	case <-s.started:
	default:
		close(s.started)
	}
	if s.onStart != nil {
		s.onStart()
	}
	for _, event := range s.events {
		sink(event)
	}
	if !s.holdOpen {
		return nil
	}
	<-ctx.Done()
	return nil
}

func (s *stubRunner) Write(ctx context.Context, data []byte) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.writeCh <- append([]byte(nil), data...):
		return nil
	}
}

func (s *stubRunner) Close() error {
	select {
	case s.closedCh <- struct{}{}:
	default:
	}
	return nil
}

func (s *stubRunner) SetPermissionMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastPermissionMode = mode
	s.permissionModes = append(s.permissionModes, mode)
}

func (s *stubRunner) CanAcceptInteractiveInput() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.interactive
}

func (s *stubRunner) HasPendingPermissionRequest() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hasPendingPermission
}

func (s *stubRunner) WritePermissionResponse(ctx context.Context, decision string) error {
	if s.permissionResponseErr != nil {
		return s.permissionResponseErr
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.permissionResponseWriteCh <- decision:
		s.mu.Lock()
		s.hasPendingPermission = false
		s.mu.Unlock()
		return nil
	}
}

func (s *stubRunner) WaitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-s.started:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not start")
	}
}

func (s *stubRunner) WaitClosed(t *testing.T) {
	t.Helper()
	select {
	case <-s.closedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("runner was not closed")
	}
}

func (s *stubRunner) ClaudeSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.claudeSessionID
}

type writeOnlyStubRunner struct {
	base *stubRunner
}

func (s *writeOnlyStubRunner) Run(ctx context.Context, req runner.ExecRequest, sink runner.EventSink) error {
	return s.base.Run(ctx, req, sink)
}

func (s *writeOnlyStubRunner) Write(ctx context.Context, data []byte) error {
	return s.base.Write(ctx, data)
}

func (s *writeOnlyStubRunner) Close() error {
	return s.base.Close()
}

func (s *writeOnlyStubRunner) SetPermissionMode(mode string) {
	s.base.SetPermissionMode(mode)
}

func (s *writeOnlyStubRunner) CanAcceptInteractiveInput() bool {
	return s.base.CanAcceptInteractiveInput()
}

func (s *writeOnlyStubRunner) WaitStarted(t *testing.T) {
	s.base.WaitStarted(t)
}

func newTestHandler() *Handler {
	return NewHandler("test", nil)
}

func newTestConn(t *testing.T, h *Handler) *websocket.Conn {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/?token=test"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	return conn
}

func readEventMap(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	var event map[string]any
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatalf("read event: %v", err)
	}
	return event
}

func readInitialEvents(t *testing.T, conn *websocket.Conn) (map[string]any, map[string]any) {
	t.Helper()
	first := readEventMap(t, conn)
	second := readEventMap(t, conn)
	return first, second
}

func requireEventType(t *testing.T, event map[string]any, want string) {
	t.Helper()
	if event["type"] != want {
		t.Fatalf("expected %s event, got %#v", want, event)
	}
}

func requireAgentState(t *testing.T, event map[string]any, wantState string, wantAwait bool) {
	t.Helper()
	requireEventType(t, event, protocol.EventTypeAgentState)
	if event["state"] != wantState {
		t.Fatalf("expected agent state %q, got %#v", wantState, event)
	}
	await, _ := event["awaitInput"].(bool)
	if await != wantAwait {
		t.Fatalf("expected awaitInput=%v, got %#v", wantAwait, event)
	}
}

type switchableStubRunner struct {
	mu       sync.Mutex
	writeCh  chan []byte
	sink     runner.EventSink
	req      runner.ExecRequest
	started  chan struct{}
	closed   chan struct{}
	closeErr error
}

func newSwitchableStubRunner() *switchableStubRunner {
	return &switchableStubRunner{
		writeCh: make(chan []byte, 8),
		started: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (s *switchableStubRunner) Run(ctx context.Context, req runner.ExecRequest, sink runner.EventSink) error {
	s.mu.Lock()
	s.req = req
	s.sink = sink
	s.mu.Unlock()
	close(s.started)
	<-ctx.Done()
	return s.closeErr
}

func (s *switchableStubRunner) Write(ctx context.Context, data []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.writeCh <- append([]byte(nil), data...):
		return nil
	}
}

func (s *switchableStubRunner) Close() error {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return nil
}

func (s *switchableStubRunner) CanAcceptInteractiveInput() bool {
	return true
}

func (s *switchableStubRunner) Emit(event any) {
	s.mu.Lock()
	sink := s.sink
	s.mu.Unlock()
	if sink != nil {
		sink(event)
	}
}

func (s *switchableStubRunner) WaitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-s.started:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not start")
	}
}

func (s *switchableStubRunner) WaitClosed(t *testing.T) {
	t.Helper()
	select {
	case <-s.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("runner was not closed")
	}
}

func readInitialSessionID(t *testing.T, conn *websocket.Conn) string {
	t.Helper()
	first, second := readInitialEvents(t, conn)
	requireEventType(t, first, protocol.EventTypeSessionState)
	requireAgentState(t, second, "IDLE", false)
	sessionID, _ := first["sessionId"].(string)
	if sessionID == "" {
		t.Fatalf("expected initial session id, got %#v", first)
	}
	return sessionID
}

func readUntilType(t *testing.T, conn *websocket.Conn, want string) map[string]any {
	t.Helper()
	for i := 0; i < 20; i++ {
		event := readEventMap(t, conn)
		if event["type"] == want {
			return event
		}
	}
	t.Fatalf("did not receive %s event", want)
	return nil
}

func readUntilSessionHistory(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	return readUntilType(t, conn, protocol.EventTypeSessionHistory)
}

func readUntilSessionCreated(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	return readUntilType(t, conn, protocol.EventTypeSessionCreated)
}

func sessionLogTexts(record store.SessionRecord) []string {
	out := make([]string, 0, len(record.Projection.LogEntries))
	for _, entry := range record.Projection.LogEntries {
		switch entry.Kind {
		case "markdown", "system", "user":
			if strings.TrimSpace(entry.Message) != "" {
				out = append(out, entry.Message)
			}
		case "terminal":
			if strings.TrimSpace(entry.Text) != "" {
				out = append(out, entry.Text)
			}
		case "error":
			if entry.Context != nil && strings.TrimSpace(entry.Context.Message) != "" {
				out = append(out, entry.Context.Message)
			}
		case "step":
			if entry.Context != nil && strings.TrimSpace(entry.Context.Message) != "" {
				out = append(out, entry.Context.Message)
			}
		case "diff":
			if entry.Context != nil && strings.TrimSpace(entry.Context.Title) != "" {
				out = append(out, entry.Context.Title)
			}
		}
	}
	return out
}

func containsText(items []string, want string) bool {
	for _, item := range items {
		if strings.Contains(item, want) {
			return true
		}
	}
	return false
}

func createHistorySessionForHandlerTest(t *testing.T, h *Handler, conn *websocket.Conn, title string) string {
	t.Helper()
	if err := conn.WriteJSON(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: title}); err != nil {
		t.Fatalf("write session create request: %v", err)
	}
	created := readUntilSessionCreated(t, conn)
	summary, ok := created["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary payload, got %#v", created)
	}
	sessionID, _ := summary["id"].(string)
	if sessionID == "" {
		t.Fatalf("expected created session id, got %#v", created)
	}
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)
	return sessionID
}

func TestHandlerSkillCatalogLifecycle(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.SkillLauncher = nil
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "skill-session")

	if err := conn.WriteJSON(protocol.ClientEvent{Action: "skill_catalog_get"}); err != nil {
		t.Fatalf("write skill_catalog_get request: %v", err)
	}
	getEvent := readUntilType(t, conn, protocol.EventTypeSkillCatalogResult)
	items, ok := getEvent["items"].([]any)
	if !ok {
		t.Fatalf("expected skill catalog items, got %#v", getEvent)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty persisted skill catalog initially, got %#v", items)
	}

	if err := conn.WriteJSON(protocol.SkillCatalogRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "skill_catalog_upsert"},
		Skill: protocol.SkillDefinition{
			Name:        "local-review-extra",
			Description: "local skill",
			Prompt:      "please review",
			ResultView:  "review-card",
			TargetType:  "diff",
		},
	}); err != nil {
		t.Fatalf("write skill_catalog_upsert request: %v", err)
	}
	upsertEvent := readUntilType(t, conn, protocol.EventTypeSkillCatalogResult)
	upsertItems, ok := upsertEvent["items"].([]any)
	if !ok {
		t.Fatalf("expected skill catalog items, got %#v", upsertEvent)
	}
	foundLocal := false
	for _, raw := range upsertItems {
		item, _ := raw.(map[string]any)
		if item["name"] == "local-review-extra" {
			foundLocal = true
			if item["source"] != string(store.SkillSourceLocal) {
				t.Fatalf("expected local source, got %#v", item)
			}
		}
	}
	if !foundLocal {
		t.Fatalf("expected local skill in catalog, got %#v", upsertItems)
	}

	if err := conn.WriteJSON(protocol.ClientEvent{Action: "skill_sync_pull"}); err != nil {
		t.Fatalf("write skill_sync_pull request: %v", err)
	}
	syncEvent := readUntilType(t, conn, protocol.EventTypeSkillSyncResult)
	if syncEvent["msg"] != "skill 同步完成" {
		t.Fatalf("unexpected skill sync event: %#v", syncEvent)
	}
	syncResult := readUntilType(t, conn, protocol.EventTypeCatalogSyncResult)
	if syncResult["domain"] != "skill" || syncResult["success"] != true {
		t.Fatalf("unexpected catalog sync result: %#v", syncResult)
	}
	syncedCatalog := readUntilType(t, conn, protocol.EventTypeSkillCatalogResult)
	syncedItems, ok := syncedCatalog["items"].([]any)
	if !ok {
		t.Fatalf("expected synced skill catalog items, got %#v", syncedCatalog)
	}
	meta, ok := syncedCatalog["meta"].(map[string]any)
	if !ok {
		t.Fatalf("expected skill catalog meta, got %#v", syncedCatalog)
	}
	if meta["syncState"] != string(store.CatalogSyncStateSynced) || meta["sourceOfTruth"] != string(store.CatalogSourceTruthClaude) {
		t.Fatalf("unexpected skill catalog meta: %#v", meta)
	}
	foundExternal := false
	for _, raw := range syncedItems {
		item, _ := raw.(map[string]any)
		if item["name"] == "external-diff-summary" {
			foundExternal = true
			if item["source"] != string(store.SkillSourceExternal) {
				t.Fatalf("expected external source, got %#v", item)
			}
			if item["syncState"] != string(store.CatalogSyncStateSynced) {
				t.Fatalf("expected synced item state, got %#v", item)
			}
		}
	}
	if !foundExternal {
		t.Fatalf("expected external synced skill, got %#v", syncedItems)
	}
}

func TestHandlerCatalogAuthoringSkillAutoUpsertsAndEmitsCatalog(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	runnerStub := newSwitchableStubRunner()
	h.NewPtyRunner = func() runner.Runner { return runnerStub }
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "authoring-skill-session")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "author a skill",
		Mode:        "pty",
		RuntimeMeta: protocol.RuntimeMeta{Source: "catalog-authoring", TargetType: "skill", ResultView: "skill-catalog"},
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	runnerStub.WaitStarted(t)
	runnerStub.Emit(protocol.CatalogAuthoringResultEvent{
		Event:  protocol.NewBaseEvent(protocol.EventTypeCatalogAuthoringResult, runnerStub.req.SessionID),
		Domain: "skill",
		Skill: &protocol.SkillDefinition{
			Name:        "authoring-skill",
			Description: "generated",
			Prompt:      "do it",
			TargetType:  "diff",
			ResultView:  "review-card",
		},
	})

	catalogEvent := readUntilType(t, conn, protocol.EventTypeSkillCatalogResult)
	items, ok := catalogEvent["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected one skill item, got %#v", catalogEvent)
	}
	item, _ := items[0].(map[string]any)
	if item["name"] != "authoring-skill" {
		t.Fatalf("unexpected skill payload: %#v", item)
	}
}

func TestHandlerCatalogAuthoringMemoryAutoUpsertsAndEmitsCatalog(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	runnerStub := newSwitchableStubRunner()
	h.NewPtyRunner = func() runner.Runner { return runnerStub }
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "authoring-memory-session")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "author a memory",
		Mode:        "pty",
		RuntimeMeta: protocol.RuntimeMeta{Source: "catalog-authoring", TargetType: "memory", ResultView: "memory-catalog"},
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	runnerStub.WaitStarted(t)
	runnerStub.Emit(protocol.CatalogAuthoringResultEvent{
		Event:  protocol.NewBaseEvent(protocol.EventTypeCatalogAuthoringResult, runnerStub.req.SessionID),
		Domain: "memory",
		Memory: &protocol.MemoryItem{ID: "mem-author", Title: "Author", Content: "generated memory"},
	})

	listEvent := readUntilType(t, conn, protocol.EventTypeMemoryListResult)
	items, ok := listEvent["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected one memory item, got %#v", listEvent)
	}
	item, _ := items[0].(map[string]any)
	if item["id"] != "mem-author" {
		t.Fatalf("unexpected memory payload: %#v", item)
	}
}

func TestHandlerCatalogAuthoringInvalidPayloadEmitsError(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	runnerStub := newSwitchableStubRunner()
	h.NewPtyRunner = func() runner.Runner { return runnerStub }
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "authoring-invalid-session")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "author invalid",
		Mode:        "pty",
		RuntimeMeta: protocol.RuntimeMeta{Source: "catalog-authoring", TargetType: "skill", ResultView: "skill-catalog"},
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	runnerStub.WaitStarted(t)
	runnerStub.Emit(protocol.CatalogAuthoringResultEvent{
		Event:  protocol.NewBaseEvent(protocol.EventTypeCatalogAuthoringResult, runnerStub.req.SessionID),
		Domain: "skill",
		Skill:  &protocol.SkillDefinition{},
	})

	errorEvent := readUntilType(t, conn, protocol.EventTypeError)
	if _, ok := errorEvent["msg"].(string); !ok {
		t.Fatalf("expected error event, got %#v", errorEvent)
	}
}

func TestHandlerMemoryListAndUpsert(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "memory-session")

	if err := conn.WriteJSON(protocol.ClientEvent{Action: "memory_list"}); err != nil {
		t.Fatalf("write memory_list request: %v", err)
	}
	listEvent := readUntilType(t, conn, protocol.EventTypeMemoryListResult)
	items, ok := listEvent["items"].([]any)
	if !ok {
		t.Fatalf("expected memory items array, got %#v", listEvent)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty memory catalog, got %#v", items)
	}

	if err := conn.WriteJSON(protocol.MemoryRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "memory_upsert"},
		Item:        protocol.MemoryItem{ID: "m-test", Title: "Test Memory", Content: "remember this"},
	}); err != nil {
		t.Fatalf("write memory_upsert request: %v", err)
	}
	upsertEvent := readUntilType(t, conn, protocol.EventTypeMemoryListResult)
	upsertItems, ok := upsertEvent["items"].([]any)
	if !ok {
		t.Fatalf("expected memory items array after upsert, got %#v", upsertEvent)
	}
	if len(upsertItems) != 1 {
		t.Fatalf("expected one memory item after upsert, got %#v", upsertItems)
	}
	item, _ := upsertItems[0].(map[string]any)
	if item["id"] != "m-test" || item["content"] != "remember this" {
		t.Fatalf("unexpected memory item payload: %#v", item)
	}
	persisted, err := tempStore.ListMemoryCatalog(context.Background())
	if err != nil {
		t.Fatalf("list persisted memory catalog: %v", err)
	}
	if len(persisted) != 1 || persisted[0].ID != "m-test" {
		t.Fatalf("unexpected persisted memory items: %#v", persisted)
	}
}

func TestHandlerMemorySyncPullEmitsCatalogLifecycle(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "memory-sync-session")

	if err := conn.WriteJSON(protocol.MemoryRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "memory_upsert"},
		Item:        protocol.MemoryItem{ID: "m-test", Title: "Test Memory", Content: "remember this"},
	}); err != nil {
		t.Fatalf("write memory_upsert request: %v", err)
	}
	_ = readUntilType(t, conn, protocol.EventTypeMemoryListResult)

	if err := conn.WriteJSON(protocol.ClientEvent{Action: "memory_sync_pull"}); err != nil {
		t.Fatalf("write memory_sync_pull request: %v", err)
	}
	statusEvent := readUntilType(t, conn, protocol.EventTypeCatalogSyncStatus)
	if statusEvent["domain"] != "memory" {
		t.Fatalf("unexpected memory sync status: %#v", statusEvent)
	}
	resultEvent := readUntilType(t, conn, protocol.EventTypeCatalogSyncResult)
	if resultEvent["domain"] != "memory" || resultEvent["success"] != true {
		t.Fatalf("unexpected memory sync result: %#v", resultEvent)
	}
	listEvent := readUntilType(t, conn, protocol.EventTypeMemoryListResult)
	meta, ok := listEvent["meta"].(map[string]any)
	if !ok {
		t.Fatalf("expected memory meta, got %#v", listEvent)
	}
	if meta["syncState"] != string(store.CatalogSyncStateSynced) || meta["sourceOfTruth"] != string(store.CatalogSourceTruthClaude) {
		t.Fatalf("unexpected memory meta after sync: %#v", meta)
	}
	items, ok := listEvent["items"].([]any)
	if !ok {
		t.Fatalf("expected memory items after sync, got %#v", listEvent)
	}
	if len(items) != 1 {
		t.Fatalf("expected one local memory item to remain after sync, got %#v", items)
	}
	item, _ := items[0].(map[string]any)
	if item["id"] != "m-test" || item["syncState"] != string(store.CatalogSyncStateDraft) {
		t.Fatalf("unexpected synced memory item payload: %#v", item)
	}
}

func TestHandlerPermissionDecisionWithoutActiveClaudeRunnerDoesNotResumeLoop(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	runnerStub := newStubRunner()
	h.NewPtyRunner = func() runner.Runner { return runnerStub }
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	sessionID := createHistorySessionForHandlerTest(t, h, conn, "permission-session")

	if _, err := tempStore.SaveProjection(context.Background(), sessionID, store.ProjectionSnapshot{
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		Runtime: store.SessionRuntime{
			Command:         "bash",
			ResumeSessionID: "resume-123",
			PermissionMode:  "default",
			CWD:             "/workspace",
		},
	}); err != nil {
		t.Fatalf("save projection: %v", err)
	}

	if err := conn.WriteJSON(protocol.PermissionDecisionRequestEvent{
		ClientEvent:     protocol.ClientEvent{Action: "permission_decision"},
		Decision:        "approve",
		PermissionMode:  "default",
		ResumeSessionID: "resume-123",
		PromptMessage:   "Allow write?",
		FallbackCommand: "bash",
		FallbackCWD:     "/workspace",
	}); err != nil {
		t.Fatalf("write permission_decision request: %v", err)
	}

	errorEvent := readUntilType(t, conn, protocol.EventTypeError)
	msg, _ := errorEvent["msg"].(string)
	if msg != "当前没有可交互的 Claude 会话，无法继续处理该权限请求" {
		t.Fatalf("unexpected error event: %#v", errorEvent)
	}
	select {
	case data := <-runnerStub.writeCh:
		t.Fatalf("expected no permission input replay, got %q", string(data))
	case <-time.After(200 * time.Millisecond):
	}
}

func TestHandlerInitialConnectionDoesNotTreatConnectionIDAsSession(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	conn := newTestConn(t, h)
	first, second := readInitialEvents(t, conn)
	requireEventType(t, first, protocol.EventTypeSessionState)
	requireAgentState(t, second, "IDLE", false)
	if sessionID, _ := first["sessionId"].(string); sessionID != "" {
		t.Fatalf("expected empty initial session id, got %#v", first)
	}
	for i := 0; i < 3; i++ {
		event := readEventMap(t, conn)
		if event["type"] == protocol.EventTypeError {
			msg, _ := event["msg"].(string)
			if strings.Contains(msg, "session not found: conn-") {
				t.Fatalf("unexpected connection-id session lookup error: %#v", event)
			}
		}
	}
}

func TestHandlerSessionContextGetWithoutSelectedSessionReturnsEmptyResult(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = readUntilType(t, conn, protocol.EventTypeSkillCatalogResult)
	_ = readUntilType(t, conn, protocol.EventTypeMemoryListResult)
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)
	if err := conn.WriteJSON(protocol.ClientEvent{Action: "session_context_get"}); err != nil {
		t.Fatalf("write session_context_get request: %v", err)
	}
	event := readUntilType(t, conn, protocol.EventTypeSessionContextResult)
	ctx, ok := event["sessionContext"].(map[string]any)
	if !ok {
		t.Fatalf("expected sessionContext payload, got %#v", event)
	}
	if len(ctx) != 0 {
		t.Fatalf("expected empty session context, got %#v", ctx)
	}
}

func TestHandlerSessionContextUpdateWithoutSelectedSessionReturnsError(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = readUntilType(t, conn, protocol.EventTypeSkillCatalogResult)
	_ = readUntilType(t, conn, protocol.EventTypeMemoryListResult)
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)
	if err := conn.WriteJSON(protocol.SessionContextUpdateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_context_update"}, EnabledSkillNames: []string{"review"}}); err != nil {
		t.Fatalf("write session_context_update request: %v", err)
	}
	event := readUntilType(t, conn, protocol.EventTypeError)
	msg, _ := event["msg"].(string)
	if !strings.Contains(msg, "请先创建或加载会话") {
		t.Fatalf("expected explicit no-session error, got %#v", event)
	}
}

func TestHandlerExecWithoutSelectedSessionReturnsError(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = readUntilType(t, conn, protocol.EventTypeSkillCatalogResult)
	_ = readUntilType(t, conn, protocol.EventTypeMemoryListResult)
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "claude",
		Mode:        "pty",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}

	event := readUntilType(t, conn, protocol.EventTypeError)
	msg, _ := event["msg"].(string)
	if !strings.Contains(msg, "请先创建或加载会话") {
		t.Fatalf("expected explicit no-session error, got %#v", event)
	}
	items, err := tempStore.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no projection/session writes, got %#v", items)
	}
}

func TestHandlerInputWithoutSelectedSessionReturnsError(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = readUntilType(t, conn, protocol.EventTypeSkillCatalogResult)
	_ = readUntilType(t, conn, protocol.EventTypeMemoryListResult)
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)

	if err := conn.WriteJSON(protocol.InputRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "input"},
		Data:        "hello\n",
	}); err != nil {
		t.Fatalf("write input request: %v", err)
	}

	event := readUntilType(t, conn, protocol.EventTypeError)
	msg, _ := event["msg"].(string)
	if !strings.Contains(msg, "请先创建或加载会话") {
		t.Fatalf("expected explicit no-session error, got %#v", event)
	}
	items, err := tempStore.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no projection/session writes, got %#v", items)
	}
}

func TestHandlerDeleteCurrentSessionFallsBackToEmptyState(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = readUntilType(t, conn, protocol.EventTypeSkillCatalogResult)
	_ = readUntilType(t, conn, protocol.EventTypeMemoryListResult)
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)
	sessionID := createHistorySessionForHandlerTest(t, h, conn, "only-session")
	if err := conn.WriteJSON(protocol.SessionDeleteRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_delete"}, SessionID: sessionID}); err != nil {
		t.Fatalf("write session_delete request: %v", err)
	}
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)
	event := readUntilType(t, conn, protocol.EventTypeSessionState)
	if sessionValue, _ := event["sessionId"].(string); sessionValue != "" {
		t.Fatalf("expected empty session after delete fallback, got %#v", event)
	}
	if state, _ := event["state"].(string); state != string(session.StateActive) {
		t.Fatalf("expected active empty state, got %#v", event)
	}

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "claude",
		Mode:        "pty",
	}); err != nil {
		t.Fatalf("write exec request after delete: %v", err)
	}
	guardEvent := readUntilType(t, conn, protocol.EventTypeError)
	msg, _ := guardEvent["msg"].(string)
	if !strings.Contains(msg, "请先创建或加载会话") {
		t.Fatalf("expected explicit no-session error after delete fallback, got %#v", guardEvent)
	}
}

func TestHandlerSessionContextGetUpdateAndRestore(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	sessionID := createHistorySessionForHandlerTest(t, h, conn, "context-session")

	if err := conn.WriteJSON(protocol.ClientEvent{Action: "session_context_get"}); err != nil {
		t.Fatalf("write session_context_get request: %v", err)
	}
	initialEvent := readUntilType(t, conn, protocol.EventTypeSessionContextResult)
	initialContext, ok := initialEvent["sessionContext"].(map[string]any)
	if !ok {
		t.Fatalf("expected sessionContext payload, got %#v", initialEvent)
	}
	if len(initialContext) != 0 {
		t.Fatalf("expected empty initial sessionContext, got %#v", initialContext)
	}

	if err := conn.WriteJSON(protocol.SessionContextUpdateRequestEvent{
		ClientEvent:       protocol.ClientEvent{Action: "session_context_update"},
		EnabledSkillNames: []string{"review", "analyze"},
		EnabledMemoryIDs:  []string{"m-test", "m-2"},
	}); err != nil {
		t.Fatalf("write session_context_update request: %v", err)
	}
	updatedEvent := readUntilType(t, conn, protocol.EventTypeSessionContextResult)
	updatedContext, ok := updatedEvent["sessionContext"].(map[string]any)
	if !ok {
		t.Fatalf("expected updated sessionContext payload, got %#v", updatedEvent)
	}
	skillNames, ok := updatedContext["enabledSkillNames"].([]any)
	if !ok || len(skillNames) != 2 {
		t.Fatalf("expected enabledSkillNames, got %#v", updatedContext)
	}
	memoryIDs, ok := updatedContext["enabledMemoryIds"].([]any)
	if !ok || len(memoryIDs) != 2 {
		t.Fatalf("expected enabledMemoryIds, got %#v", updatedContext)
	}

	record, err := h.SessionStore.GetSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("get updated session: %v", err)
	}
	if len(record.Projection.SessionContext.EnabledSkillNames) != 2 || len(record.Projection.SessionContext.EnabledMemoryIDs) != 2 {
		t.Fatalf("unexpected persisted session context: %#v", record.Projection.SessionContext)
	}
	if record.Projection.SkillCatalogMeta.SyncState != store.CatalogSyncStateIdle || record.Projection.MemoryCatalogMeta.SyncState != store.CatalogSyncStateIdle {
		t.Fatalf("session context update should preserve catalog sync state defaults: %#v %#v", record.Projection.SkillCatalogMeta, record.Projection.MemoryCatalogMeta)
	}

	if err := conn.WriteJSON(protocol.SessionLoadRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_load"}, SessionID: sessionID}); err != nil {
		t.Fatalf("write session_load request: %v", err)
	}
	history := readUntilSessionHistory(t, conn)
	historyContext, ok := history["sessionContext"].(map[string]any)
	if !ok {
		t.Fatalf("expected sessionContext in session history, got %#v", history)
	}
	historySkills, ok := historyContext["enabledSkillNames"].([]any)
	if !ok || len(historySkills) != 2 {
		t.Fatalf("expected restored enabledSkillNames, got %#v", historyContext)
	}
	historyMemory, ok := historyContext["enabledMemoryIds"].([]any)
	if !ok || len(historyMemory) != 2 {
		t.Fatalf("expected restored enabledMemoryIds, got %#v", historyContext)
	}
}

func TestHandlerSessionContextUpdateAndRestore(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	sessionID := createHistorySessionForHandlerTest(t, h, conn, "context-session")

	if err := conn.WriteJSON(protocol.ClientEvent{Action: "session_context_get"}); err != nil {
		t.Fatalf("write session_context_get request: %v", err)
	}
	initialEvent := readUntilType(t, conn, protocol.EventTypeSessionContextResult)
	initialContext, ok := initialEvent["sessionContext"].(map[string]any)
	if !ok {
		t.Fatalf("expected sessionContext payload, got %#v", initialEvent)
	}
	if len(initialContext) != 0 {
		t.Fatalf("expected empty initial sessionContext, got %#v", initialContext)
	}

	if err := conn.WriteJSON(protocol.SessionContextUpdateRequestEvent{
		ClientEvent:       protocol.ClientEvent{Action: "session_context_update"},
		EnabledSkillNames: []string{"review", "analyze"},
		EnabledMemoryIDs:  []string{"m-test", "m-2"},
	}); err != nil {
		t.Fatalf("write session_context_update request: %v", err)
	}
	updatedEvent := readUntilType(t, conn, protocol.EventTypeSessionContextResult)
	updatedContext, ok := updatedEvent["sessionContext"].(map[string]any)
	if !ok {
		t.Fatalf("expected updated sessionContext payload, got %#v", updatedEvent)
	}
	skillNames, ok := updatedContext["enabledSkillNames"].([]any)
	if !ok || len(skillNames) != 2 {
		t.Fatalf("expected enabledSkillNames, got %#v", updatedContext)
	}
	memoryIDs, ok := updatedContext["enabledMemoryIds"].([]any)
	if !ok || len(memoryIDs) != 2 {
		t.Fatalf("expected enabledMemoryIds, got %#v", updatedContext)
	}

	record, err := h.SessionStore.GetSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("get updated session: %v", err)
	}
	if len(record.Projection.SessionContext.EnabledSkillNames) != 2 || len(record.Projection.SessionContext.EnabledMemoryIDs) != 2 {
		t.Fatalf("unexpected persisted session context: %#v", record.Projection.SessionContext)
	}
	if record.Projection.SkillCatalogMeta.SyncState != store.CatalogSyncStateIdle || record.Projection.MemoryCatalogMeta.SyncState != store.CatalogSyncStateIdle {
		t.Fatalf("session context update should preserve catalog sync state defaults: %#v %#v", record.Projection.SkillCatalogMeta, record.Projection.MemoryCatalogMeta)
	}

	if err := conn.WriteJSON(protocol.SessionLoadRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_load"}, SessionID: sessionID}); err != nil {
		t.Fatalf("write session_load request: %v", err)
	}
	history := readUntilSessionHistory(t, conn)
	historyContext, ok := history["sessionContext"].(map[string]any)
	if !ok {
		t.Fatalf("expected sessionContext in session history, got %#v", history)
	}
	historySkills, ok := historyContext["enabledSkillNames"].([]any)
	if !ok || len(historySkills) != 2 {
		t.Fatalf("expected restored enabledSkillNames, got %#v", historyContext)
	}
	historyMemory, ok := historyContext["enabledMemoryIds"].([]any)
	if !ok || len(historyMemory) != 2 {
		t.Fatalf("expected restored enabledMemoryIds, got %#v", historyContext)
	}
	reviewState := readUntilType(t, conn, protocol.EventTypeReviewState)
	if reviewState["type"] != protocol.EventTypeReviewState {
		t.Fatalf("expected review_state after session_load, got %#v", reviewState)
	}
}

func TestHandlerReviewStateGetReturnsProjectionGroups(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	sessionID := createHistorySessionForHandlerTest(t, h, conn, "review-state-session")

	record, err := h.SessionStore.GetSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	record.Projection = normalizeProjectionSnapshot(store.ProjectionSnapshot{
		Diffs: []session.DiffContext{{
			ContextID:     "diff-1",
			Title:         "handler diff",
			Path:          "internal/ws/handler.go",
			Diff:          "@@ -1 +1 @@",
			Lang:          "go",
			PendingReview: true,
			ExecutionID:   "exec-1",
			GroupID:       "group-1",
			GroupTitle:    "修改组 1",
			ReviewStatus:  "pending",
		}},
		RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
	})
	if _, err := h.SessionStore.SaveProjection(context.Background(), sessionID, record.Projection); err != nil {
		t.Fatalf("save projection: %v", err)
	}

	if err := conn.WriteJSON(protocol.ClientEvent{Action: "review_state_get"}); err != nil {
		t.Fatalf("write review_state_get request: %v", err)
	}
	event := readUntilType(t, conn, protocol.EventTypeReviewState)
	groups, ok := event["groups"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatalf("expected one review group, got %#v", event)
	}
	activeGroup, ok := event["activeGroup"].(map[string]any)
	if !ok {
		t.Fatalf("expected activeGroup payload, got %#v", event)
	}
	if activeGroup["id"] != "group-1" {
		t.Fatalf("expected activeGroup id group-1, got %#v", activeGroup)
	}
}

func TestHandlerExecFlow(t *testing.T) {
	execRunner := newStubRunner(
		protocol.NewLogEvent("ignored", "hello from runner", "stdout"),
		protocol.NewSessionStateEvent("ignored", "closed", "command finished"),
	)

	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewExecRunner = func() runner.Runner { return execRunner }

	conn := newTestConn(t, h)
	first, second := readInitialEvents(t, conn)
	requireEventType(t, first, protocol.EventTypeSessionState)
	requireAgentState(t, second, "IDLE", false)
	_ = createHistorySessionForHandlerTest(t, h, conn, "exec-flow")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "printf 'ignored'",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}

	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "THINKING", false)
	if event := readUntilType(t, conn, protocol.EventTypeLog); event["msg"] != "hello from runner" || event["stream"] != "stdout" {
		t.Fatalf("expected stdout log event, got %#v", event)
	}
	if event := readUntilType(t, conn, protocol.EventTypeSessionState); event["state"] != "closed" {
		t.Fatalf("expected closed session event, got %#v", event)
	}
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "IDLE", false)
}

func TestProjectionBuildsReviewGroupsFromDiffs(t *testing.T) {
	snapshot := store.ProjectionSnapshot{RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""}}
	event := protocol.ApplyRuntimeMeta(
		protocol.NewFileDiffEvent("session-1", "internal/ws/handler.go", "handler diff", "diff --git a/internal/ws/handler.go b/internal/ws/handler.go", "go"),
		protocol.RuntimeMeta{ContextID: "diff-1", ExecutionID: "exec-1", GroupID: "group-1", GroupTitle: "修改组 1"},
	)
	snapshot, changed := applyEventToProjection(snapshot, event)
	if !changed {
		t.Fatal("expected file_diff to change projection")
	}
	if len(snapshot.ReviewGroups) != 1 {
		t.Fatalf("expected one review group, got %#v", snapshot.ReviewGroups)
	}
	group := snapshot.ReviewGroups[0]
	if group.ID != "group-1" {
		t.Fatalf("expected group id group-1, got %#v", group)
	}
	if group.PendingCount != 1 || !group.PendingReview {
		t.Fatalf("expected pending review group, got %#v", group)
	}
	if snapshot.ActiveReviewGroup == nil || snapshot.ActiveReviewGroup.ID != "group-1" {
		t.Fatalf("expected active review group to be restored, got %#v", snapshot.ActiveReviewGroup)
	}
}

func TestApplyReviewDecisionToProjectionUpdatesReviewState(t *testing.T) {
	snapshot := normalizeProjectionSnapshot(store.ProjectionSnapshot{
		Diffs: []session.DiffContext{{
			ContextID:     "diff-1",
			Title:         "handler diff",
			Path:          "internal/ws/handler.go",
			Diff:          "@@ -1 +1 @@",
			Lang:          "go",
			PendingReview: true,
			ExecutionID:   "exec-1",
			GroupID:       "group-1",
			GroupTitle:    "修改组 1",
			ReviewStatus:  "pending",
		}},
	})
	snapshot = applyReviewDecisionToProjection(snapshot, protocol.ReviewDecisionRequestEvent{
		Decision:    "accept",
		ContextID:   "diff-1",
		TargetPath:  "internal/ws/handler.go",
		ExecutionID: "exec-1",
		GroupID:     "group-1",
	}, "accept", session.DiffContext{})
	if len(snapshot.ReviewGroups) != 1 {
		t.Fatalf("expected one review group, got %#v", snapshot.ReviewGroups)
	}
	group := snapshot.ReviewGroups[0]
	if group.ReviewStatus != "accepted" {
		t.Fatalf("expected accepted group, got %#v", group)
	}
	if group.PendingReview || group.PendingCount != 0 {
		t.Fatalf("expected no pending reviews after accept, got %#v", group)
	}
	if len(group.Files) != 1 || group.Files[0].ReviewStatus != "accepted" {
		t.Fatalf("expected accepted review file, got %#v", group.Files)
	}
	if snapshot.CurrentDiff == nil || snapshot.CurrentDiff.ReviewStatus != "accepted" || snapshot.CurrentDiff.PendingReview {
		t.Fatalf("expected current diff to be marked accepted, got %#v", snapshot.CurrentDiff)
	}
}

func TestWithRuntimeSnapshotPrefersLiveLifecycleOverStaleStarting(t *testing.T) {
	snapshot := withRuntimeSnapshot(store.ProjectionSnapshot{
		Controller: session.ControllerSnapshot{
			SessionID:       "s1",
			CurrentCommand:  "claude",
			ResumeSession:   "resume-1",
			ClaudeLifecycle: "starting",
			ActiveMeta:      protocol.RuntimeMeta{Command: "claude", ResumeSessionID: "resume-1", ClaudeLifecycle: "starting"},
		},
		Runtime: store.SessionRuntime{ResumeSessionID: "resume-1", Command: "claude", ClaudeLifecycle: "starting"},
	}, nil)
	if snapshot.Runtime.ClaudeLifecycle != "resumable" {
		t.Fatalf("expected resumable lifecycle, got %#v", snapshot.Runtime)
	}
}

func TestSessionHistoryNormalizesStaleStartingToResumable(t *testing.T) {
	history := newSessionHistoryEventFromRecord(store.SessionRecord{
		Summary: store.SessionSummary{ID: "session-1", Title: "history"},
		Projection: store.ProjectionSnapshot{
			Controller: session.ControllerSnapshot{
				SessionID:       "session-1",
				CurrentCommand:  "claude",
				ResumeSession:   "resume-1",
				ClaudeLifecycle: "starting",
				ActiveMeta:      protocol.RuntimeMeta{Command: "claude", ResumeSessionID: "resume-1", ClaudeLifecycle: "starting"},
			},
			Runtime:             store.SessionRuntime{ResumeSessionID: "resume-1", Command: "claude", ClaudeLifecycle: "starting"},
			RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""},
		},
	})
	if history.ResumeRuntimeMeta.ClaudeLifecycle != "resumable" {
		t.Fatalf("expected resumable lifecycle in history, got %#v", history.ResumeRuntimeMeta)
	}
}

func TestProjectionHistoryIncludesTerminalExecutions(t *testing.T) {
	executionID := "exec-test-1"
	snapshot := store.ProjectionSnapshot{RawTerminalByStream: map[string]string{"stdout": "", "stderr": ""}}

	var changed bool
	snapshot, changed = applyEventToProjection(snapshot, protocol.ApplyRuntimeMeta(protocol.NewExecutionLogEvent("session-1", executionID, "echo hello", "", "started", nil), protocol.RuntimeMeta{Command: "echo hello", CWD: "/tmp"}))
	if !changed {
		t.Fatal("expected started event to change projection")
	}
	snapshot, changed = applyEventToProjection(snapshot, protocol.ApplyRuntimeMeta(protocol.NewExecutionLogEvent("session-1", executionID, "hello from runner", "stdout", "stdout", nil), protocol.RuntimeMeta{Command: "echo hello", CWD: "/tmp"}))
	if !changed {
		t.Fatal("expected stdout event to change projection")
	}
	snapshot, changed = applyEventToProjection(snapshot, protocol.ApplyRuntimeMeta(protocol.NewExecutionLogEvent("session-1", executionID, "second stdout", "stdout", "stdout", nil), protocol.RuntimeMeta{Command: "echo hello", CWD: "/tmp"}))
	if !changed {
		t.Fatal("expected second stdout event to change projection")
	}
	snapshot, changed = applyEventToProjection(snapshot, protocol.ApplyRuntimeMeta(protocol.NewExecutionLogEvent("session-1", executionID, "stderr from runner", "stderr", "stderr", nil), protocol.RuntimeMeta{Command: "echo hello", CWD: "/tmp"}))
	if !changed {
		t.Fatal("expected stderr event to change projection")
	}
	snapshot, changed = applyEventToProjection(snapshot, protocol.ApplyRuntimeMeta(protocol.NewExecutionLogEvent("session-1", executionID, "", "", "finished", intPtr(0)), protocol.RuntimeMeta{Command: "echo hello", CWD: "/tmp"}))
	if !changed {
		t.Fatal("expected finished event to change projection")
	}

	if got := snapshot.RawTerminalByStream["stdout"]; got != "hello from runner\nsecond stdout" {
		t.Fatalf("expected aggregated stdout stream, got %q", got)
	}
	if got := snapshot.RawTerminalByStream["stderr"]; got != "stderr from runner" {
		t.Fatalf("expected aggregated stderr stream, got %q", got)
	}
	if len(snapshot.TerminalExecutions) != 1 {
		t.Fatalf("expected one terminal execution in snapshot, got %#v", snapshot.TerminalExecutions)
	}
	item := snapshot.TerminalExecutions[0]
	if item.ExecutionID != executionID {
		t.Fatalf("expected execution id %q, got %#v", executionID, item)
	}
	if item.Command != "echo hello" {
		t.Fatalf("expected command echo hello, got %#v", item)
	}
	if item.CWD != "/tmp" {
		t.Fatalf("expected cwd /tmp, got %#v", item)
	}
	if item.Stdout != "hello from runner\nsecond stdout" {
		t.Fatalf("expected stdout aggregation, got %#v", item)
	}
	if item.Stderr != "stderr from runner" {
		t.Fatalf("expected stderr aggregation, got %#v", item)
	}
	if item.ExitCode == nil || *item.ExitCode != 0 {
		t.Fatalf("expected exitCode 0, got %#v", item)
	}

	record := store.SessionRecord{
		Summary: store.SessionSummary{ID: "session-1", Title: "exec-history"},
		Projection: store.ProjectionSnapshot{
			RawTerminalByStream: snapshot.RawTerminalByStream,
			TerminalExecutions:  snapshot.TerminalExecutions,
			ReviewGroups: []session.ReviewGroup{{
				ID:            "group-1",
				Title:         "修改组 1",
				ExecutionID:   executionID,
				PendingReview: true,
				ReviewStatus:  "pending",
				CurrentFileID: "diff-1",
				CurrentPath:   "internal/ws/handler.go",
				PendingCount:  1,
				Files: []session.ReviewFile{{
					ContextID:     "diff-1",
					Title:         "handler diff",
					Path:          "internal/ws/handler.go",
					Diff:          "@@ -1 +1 @@",
					Lang:          "go",
					PendingReview: true,
					ExecutionID:   executionID,
					ReviewStatus:  "pending",
				}},
			}},
			ActiveReviewGroup: &session.ReviewGroup{ID: "group-1", Title: "修改组 1", ExecutionID: executionID, PendingReview: true},
		},
	}
	history := newSessionHistoryEventFromRecord(record)
	if len(history.ReviewGroups) != 1 {
		t.Fatalf("expected one review group in history, got %#v", history.ReviewGroups)
	}
	if history.ActiveReviewGroup == nil || history.ActiveReviewGroup.ID != "group-1" {
		t.Fatalf("expected active review group in history, got %#v", history.ActiveReviewGroup)
	}
	if history.RawTerminalByStream["stdout"] != "hello from runner\nsecond stdout" {
		t.Fatalf("expected history stdout stream, got %#v", history.RawTerminalByStream)
	}
	if history.RawTerminalByStream["stderr"] != "stderr from runner" {
		t.Fatalf("expected history stderr stream, got %#v", history.RawTerminalByStream)
	}
	if len(history.TerminalExecutions) != 1 {
		t.Fatalf("expected one terminal execution in history, got %#v", history.TerminalExecutions)
	}
	historyItem := history.TerminalExecutions[0]
	if historyItem.ExecutionID != executionID {
		t.Fatalf("expected history execution id %q, got %#v", executionID, historyItem)
	}
	if historyItem.Command != "echo hello" || historyItem.CWD != "/tmp" || historyItem.Stdout != "hello from runner\nsecond stdout" || historyItem.Stderr != "stderr from runner" {
		t.Fatalf("unexpected history execution payload: %#v", historyItem)
	}
	if historyItem.ExitCode == nil || *historyItem.ExitCode != 0 {
		t.Fatalf("expected history exitCode 0, got %#v", historyItem)
	}
}

func intPtr(v int) *int {
	return &v
}

func TestHandlerPtyInputFlow(t *testing.T) {
	ptyRunner := newHoldingStubRunner(
		protocol.NewPromptRequestEvent("ignored", "Proceed? [y/N]", []string{"y", "n"}),
	)

	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }

	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "pty-input-flow")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "printf 'ignored'",
		Mode:        "pty",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}

	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "THINKING", false)
	_ = readUntilType(t, conn, protocol.EventTypePromptRequest)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "WAIT_INPUT", true)

	if err := conn.WriteJSON(protocol.InputRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "input"},
		Data:        "y\n",
	}); err != nil {
		t.Fatalf("write input request: %v", err)
	}

	select {
	case payload := <-ptyRunner.writeCh:
		if string(payload) != "y\n" {
			t.Fatalf("expected y\\n payload, got %q", string(payload))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive input payload")
	}

	requireAgentState(t, readEventMap(t, conn), "THINKING", false)
}

func TestHandlerEmitsAgentStateForToolEventsAndFinish(t *testing.T) {
	ptyRunner := newStubRunner(
		protocol.NewStepUpdateEvent("ignored", "Reading internal/ws/handler.go", "running", "internal/ws/handler.go", "reading", "Reading internal/ws/handler.go"),
		protocol.NewFileDiffEvent("ignored", "internal/ws/handler.go", "Updating internal/ws/handler.go", "diff --git a/internal/ws/handler.go b/internal/ws/handler.go", "go"),
	)

	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }

	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "tool-events")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "claude",
		Mode:        "pty",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}

	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "THINKING", false)
	_ = readUntilType(t, conn, protocol.EventTypeStepUpdate)
	toolEvent := readUntilType(t, conn, protocol.EventTypeAgentState)
	requireAgentState(t, toolEvent, "RUNNING_TOOL", false)
	if toolEvent["step"] != "Reading internal/ws/handler.go" {
		t.Fatalf("expected step in agent state, got %#v", toolEvent)
	}
	_ = readUntilType(t, conn, protocol.EventTypeFileDiff)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "RUNNING_TOOL", false)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "IDLE", false)
}

func TestHandlerClaudeSessionStartsInWaitInput(t *testing.T) {
	ptyRunner := newHoldingStubRunner(
		protocol.NewPromptRequestEvent("ignored", "Claude 会话已就绪，可继续输入", nil),
	)

	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }

	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "claude-wait-input")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "claude",
		Mode:        "pty",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}

	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "THINKING", false)
	_ = readUntilType(t, conn, protocol.EventTypePromptRequest)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "WAIT_INPUT", true)
}

func TestHandlerInputRestoresSafePermissionModeBeforeSending(t *testing.T) {
	firstRunner := newHoldingStubRunner(protocol.ApplyRuntimeMeta(
		protocol.NewPromptRequestEvent("ignored", "写 README 需要你的授权", []string{"y", "n"}),
		protocol.RuntimeMeta{ResumeSessionID: "resume-input-123"},
	))
	firstRunner.claudeSessionID = "resume-input-123"
	secondRunner := newHoldingStubRunner()
	runnerIndex := 0
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner {
		runnerIndex++
		if runnerIndex == 1 {
			return firstRunner
		}
		return secondRunner
	}
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "input-restore-safe")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{ClientEvent: protocol.ClientEvent{Action: "exec"}, Command: "claude", Mode: "pty", PermissionMode: "default"}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	firstRunner.WaitStarted(t)
	_ = readUntilType(t, conn, protocol.EventTypePromptRequest)
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)

	if err := conn.WriteJSON(protocol.InputRequestEvent{ClientEvent: protocol.ClientEvent{Action: "input"}, Data: "请创建 README.md 并写入 hello\n"}); err != nil {
		t.Fatalf("write input request: %v", err)
	}
	select {
	case payload := <-firstRunner.writeCh:
		if string(payload) != "请创建 README.md 并写入 hello\n" {
			t.Fatalf("unexpected initial input payload: %q", string(payload))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive initial input payload")
	}

	if err := conn.WriteJSON(protocol.PermissionDecisionRequestEvent{ClientEvent: protocol.ClientEvent{Action: "permission_decision"}, Decision: "approve", PermissionMode: "default", TargetPath: "README.md", PromptMessage: "写 README 需要你的授权"}); err != nil {
		t.Fatalf("write permission decision request: %v", err)
	}
	firstRunner.WaitClosed(t)
	secondRunner.WaitStarted(t)
	select {
	case payload := <-secondRunner.writeCh:
		if got := string(payload); got != "我已经批准这次文件修改权限。  不要继续复用刚才那次失败的工具调用；请基于这次已批准的权限，立即发起新的 Write/Edit 操作。  本次已授权的目标文件：`README.md`。  不要再次请求权限，不要只做解释，直接完成这次文件修改。  你上一次请求权限时的原始上下文如下，请按该上下文重新完成写入：写 README 需要你的授权\n" {
			t.Fatalf("unexpected continuation payload: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive continuation payload")
	}

	if err := conn.WriteJSON(protocol.InputRequestEvent{ClientEvent: protocol.ClientEvent{Action: "input"}, Data: "next\n"}); err != nil {
		t.Fatalf("write input request: %v", err)
	}
	select {
	case payload := <-secondRunner.writeCh:
		if string(payload) != "next\n" {
			t.Fatalf("unexpected restored input payload: %q", string(payload))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive restored input payload")
	}
	if secondRunner.lastReq.PermissionMode != "default" {
		t.Fatalf("expected default permission mode after restore, got %#v", secondRunner.lastReq)
	}
}

func TestHandlerInputAutoResumesDetachedClaudeSession(t *testing.T) {
	firstRunner := newStubRunner(protocol.ApplyRuntimeMeta(
		protocol.NewPromptRequestEvent("ignored", "已暂停，可继续", nil),
		protocol.RuntimeMeta{ResumeSessionID: "resume-chat-123"},
	))
	firstRunner.claudeSessionID = "resume-chat-123"
	secondRunner := newHoldingStubRunner()
	runnerIndex := 0

	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner {
		runnerIndex++
		if runnerIndex == 1 {
			return firstRunner
		}
		return secondRunner
	}

	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "input-auto-resume")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{ClientEvent: protocol.ClientEvent{Action: "exec"}, Command: "claude", Mode: "pty", PermissionMode: "default"}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	firstRunner.WaitStarted(t)
	_ = readUntilType(t, conn, protocol.EventTypePromptRequest)
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)
	time.Sleep(100 * time.Millisecond)

	if err := conn.WriteJSON(protocol.InputRequestEvent{ClientEvent: protocol.ClientEvent{Action: "input"}, Data: "second turn\n"}); err != nil {
		t.Fatalf("write input request: %v", err)
	}
	secondRunner.WaitStarted(t)
	if !strings.Contains(secondRunner.lastReq.Command, "--resume ") {
		t.Fatalf("expected resumed command, got %q", secondRunner.lastReq.Command)
	}
	select {
	case payload := <-secondRunner.writeCh:
		if got := string(payload); got != "second turn\n" {
			t.Fatalf("unexpected resumed input payload: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive resumed input payload")
	}
}

func TestHandlerInputWithoutRunner(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/?token=test"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var initial protocol.SessionStateEvent
	if err := conn.ReadJSON(&initial); err != nil {
		t.Fatalf("read initial event: %v", err)
	}
	var initialAgent map[string]any
	if err := conn.ReadJSON(&initialAgent); err != nil {
		t.Fatalf("read initial agent event: %v", err)
	}
	_ = createHistorySessionForHandlerTest(t, h, conn, "input-without-runner")

	if err := conn.WriteJSON(protocol.InputRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "input"},
		Data:        "x\n",
	}); err != nil {
		t.Fatalf("write input request: %v", err)
	}

	for {
		var event map[string]any
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatalf("read event: %v", err)
		}
		if event["type"] == protocol.EventTypeError {
			if event["msg"] != "当前没有活跃会话，且没有可恢复的 Claude 会话，请重新发起命令" {
				t.Fatalf("unexpected error event: %#v", event)
			}
			return
		}
	}
}

func TestHandlerInputRejectedForExecRunner(t *testing.T) {
	execRunner := newHoldingStubRunner()
	execRunner.writeErr = runner.ErrInputNotSupported

	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewExecRunner = func() runner.Runner { return execRunner }

	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/?token=test"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var initial protocol.SessionStateEvent
	if err := conn.ReadJSON(&initial); err != nil {
		t.Fatalf("read initial event: %v", err)
	}
	_ = createHistorySessionForHandlerTest(t, h, conn, "input-exec-runner")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "printf 'ignored'",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if err := conn.WriteJSON(protocol.InputRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "input"},
		Data:        "x\n",
	}); err != nil {
		t.Fatalf("write input request: %v", err)
	}

	for {
		var event map[string]any
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatalf("read event: %v", err)
		}
		if event["type"] == protocol.EventTypeError {
			if event["msg"] != "input is only supported for pty sessions" {
				t.Fatalf("unexpected error event: %#v", event)
			}
			return
		}
	}
}

func TestHandlerRecoversRunnerPanicAndReturnsErrorEvent(t *testing.T) {
	var logs bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	defer log.SetOutput(originalWriter)
	defer log.SetFlags(originalFlags)

	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.Upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	h.NewExecRunner = func() runner.Runner {
		return &panicRunner{}
	}

	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "panic-runner")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "panic please",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}

	event := readUntilType(t, conn, protocol.EventTypeError)
	if event["msg"] != "internal server error" {
		t.Fatalf("unexpected error event: %#v", event)
	}
	stack, _ := event["stack"].(string)
	if stack == "" {
		t.Fatalf("expected panic stack in error event, got %#v", event)
	}
	if !strings.Contains(logs.String(), "runner panic recovered") {
		t.Fatalf("expected runtime panic log, got %q", logs.String())
	}
}

type panicRunner struct{}

func (p *panicRunner) Run(ctx context.Context, req runner.ExecRequest, sink runner.EventSink) error {
	panic("boom")
}

func (p *panicRunner) Write(ctx context.Context, data []byte) error {
	return nil
}

func (p *panicRunner) Close() error {
	return nil
}

func (p *panicRunner) SetPermissionMode(mode string) {}

func TestParseMode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    runner.Mode
		wantErr error
	}{
		{name: "default", input: "", want: runner.ModeExec},
		{name: "exec", input: "exec", want: runner.ModeExec},
		{name: "pty", input: "pty", want: runner.ModePTY},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMode(tt.input)
			if err != nil {
				t.Fatalf("parse mode returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}

	if _, err := parseMode("weird"); err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestHandlerRejectsEmptyInput(t *testing.T) {
	h := newTestHandler()
	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/?token=test"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var initial protocol.SessionStateEvent
	if err := conn.ReadJSON(&initial); err != nil {
		t.Fatalf("read initial event: %v", err)
	}

	if err := conn.WriteJSON(protocol.InputRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "input"},
		Data:        "",
	}); err != nil {
		t.Fatalf("write input request: %v", err)
	}

	for {
		var event map[string]any
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatalf("read event: %v", err)
		}
		if event["type"] == protocol.EventTypeError {
			if event["msg"] != "input data is required" {
				t.Fatalf("unexpected error event: %#v", event)
			}
			return
		}
	}
}

func TestHandlerUnknownAction(t *testing.T) {
	h := newTestHandler()
	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/?token=test"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var initial protocol.SessionStateEvent
	if err := conn.ReadJSON(&initial); err != nil {
		t.Fatalf("read initial event: %v", err)
	}

	if err := conn.WriteJSON(map[string]any{"action": "nope"}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	for {
		var event map[string]any
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatalf("read event: %v", err)
		}
		if event["type"] == protocol.EventTypeError {
			if event["msg"] != "unknown action: nope" {
				t.Fatalf("unexpected error event: %#v", event)
			}
			return
		}
	}
}

func TestHandlerUnknownMode(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/?token=test"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var initial protocol.SessionStateEvent
	if err := conn.ReadJSON(&initial); err != nil {
		t.Fatalf("read initial event: %v", err)
	}
	var initialAgent map[string]any
	if err := conn.ReadJSON(&initialAgent); err != nil {
		t.Fatalf("read initial agent event: %v", err)
	}
	if err := conn.WriteJSON(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: "unknown-mode"}); err != nil {
		t.Fatalf("write session create request: %v", err)
	}
	_ = readUntilType(t, conn, protocol.EventTypeSessionCreated)
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "printf 'ignored'",
		Mode:        "weird",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}

	for {
		var event map[string]any
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatalf("read event: %v", err)
		}
		if event["type"] == protocol.EventTypeError {
			if event["msg"] != "unknown mode: weird" {
				t.Fatalf("unexpected error event: %#v", event)
			}
			return
		}
	}
}

func TestHandlerPermissionDecisionApproveTriggersHotSwap(t *testing.T) {
	firstRunner := newHoldingStubRunner(protocol.NewPromptRequestEvent("ignored", "写 README 需要你的授权", []string{"y", "n"}))
	firstRunner.claudeSessionID = "resume-approve-123"
	secondRunner := newHoldingStubRunner()
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	runnerIndex := 0
	h.NewPtyRunner = func() runner.Runner {
		runnerIndex++
		if runnerIndex == 1 {
			return firstRunner
		}
		return secondRunner
	}
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "permission-approve")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "exec"},
		Command:        "claude",
		Mode:           "pty",
		PermissionMode: "default",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	firstRunner.WaitStarted(t)
	prompt := readUntilType(t, conn, protocol.EventTypePromptRequest)
	if prompt["msg"] != "写 README 需要你的授权" {
		t.Fatalf("unexpected prompt event: %#v", prompt)
	}
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)

	if err := conn.WriteJSON(protocol.InputRequestEvent{ClientEvent: protocol.ClientEvent{Action: "input"}, Data: "请创建 README.md 并写入 hello\n"}); err != nil {
		t.Fatalf("write input request: %v", err)
	}
	select {
	case payload := <-firstRunner.writeCh:
		if string(payload) != "请创建 README.md 并写入 hello\n" {
			t.Fatalf("unexpected initial input payload: %q", string(payload))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive initial input payload")
	}

	if err := conn.WriteJSON(protocol.PermissionDecisionRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "permission_decision"},
		Decision:       "approve",
		PermissionMode: "default",
		TargetPath:     "README.md",
		ContextTitle:   "README",
		PromptMessage:  "写 README 需要你的授权",
	}); err != nil {
		t.Fatalf("write permission decision request: %v", err)
	}

	firstRunner.WaitClosed(t)
	secondRunner.WaitStarted(t)
	select {
	case payload := <-secondRunner.writeCh:
		if got := string(payload); got != "我已经批准这次文件修改权限。  不要继续复用刚才那次失败的工具调用；请基于这次已批准的权限，立即发起新的 Write/Edit 操作。  本次已授权的目标文件：`README.md`。  不要再次请求权限，不要只做解释，直接完成这次文件修改。  你上一次请求权限时的原始上下文如下，请按该上下文重新完成写入：写 README 需要你的授权\n" {
			t.Fatalf("unexpected continuation payload: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive continuation payload on hot-swapped runner")
	}
	if secondRunner.lastReq.PermissionMode != "acceptEdits" {
		t.Fatalf("expected acceptEdits restart, got %#v", secondRunner.lastReq)
	}
	if !strings.Contains(secondRunner.lastReq.Command, "--resume ") {
		t.Fatalf("expected resume command, got %q", secondRunner.lastReq.Command)
	}
	if !strings.Contains(secondRunner.lastReq.Command, "--print") {
		t.Fatalf("expected print mode on hot-swapped command, got %q", secondRunner.lastReq.Command)
	}
	if strings.Contains(secondRunner.lastReq.Command, "--session-id") {
		t.Fatalf("did not expect managed session id on hot-swapped command, got %q", secondRunner.lastReq.Command)
	}
	select {
	case decision := <-firstRunner.permissionResponseWriteCh:
		t.Fatalf("unexpected structured decision: %q", decision)
	case <-time.After(200 * time.Millisecond):
	}

	var thinking map[string]any
	for i := 0; i < 6; i++ {
		event := readUntilType(t, conn, protocol.EventTypeAgentState)
		if event["source"] == "permission-decision" {
			thinking = event
			break
		}
	}
	if thinking == nil {
		t.Fatal("did not receive permission-decision agent state")
	}
	if thinking["state"] != "THINKING" {
		t.Fatalf("expected THINKING state, got %#v", thinking)
	}
	if thinking["source"] != "permission-decision" {
		t.Fatalf("expected permission-decision source, got %#v", thinking)
	}
}

func TestHandlerPermissionDecisionApproveForCodexUsesDirectPermissionResponse(t *testing.T) {
	firstRunner := newHoldingStubRunner(protocol.NewPromptRequestEvent("ignored", "需要权限确认", []string{"approve", "deny"}))
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	runnerIndex := 0
	h.NewPtyRunner = func() runner.Runner {
		runnerIndex++
		return firstRunner
	}
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "permission-approve-codex")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "exec"},
		Command:        "codex",
		Mode:           "pty",
		PermissionMode: "default",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	firstRunner.WaitStarted(t)
	_ = readUntilType(t, conn, protocol.EventTypePromptRequest)
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)

	if err := conn.WriteJSON(protocol.PermissionDecisionRequestEvent{
		ClientEvent:     protocol.ClientEvent{Action: "permission_decision"},
		Decision:        "approve",
		PermissionMode:  "default",
		PromptMessage:   "需要权限确认",
		FallbackCommand: "codex",
		FallbackEngine:  "codex",
	}); err != nil {
		t.Fatalf("write permission decision request: %v", err)
	}

	select {
	case decision := <-firstRunner.permissionResponseWriteCh:
		if decision != "approve" {
			t.Fatalf("unexpected permission response: %q", decision)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive direct codex permission response")
	}
	if runnerIndex != 1 {
		t.Fatalf("expected no hot swap runner restart for codex, got runner count=%d", runnerIndex)
	}
}

func TestHandlerPermissionDecisionDenySendsPromptAsNormalInput(t *testing.T) {
	ptyRunner := newHoldingStubRunner(protocol.NewPromptRequestEvent("ignored", "写 README 需要你的授权", []string{"y", "n"}))
	ptyRunner.claudeSessionID = "resume-deny-123"
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "permission-deny")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "exec"},
		Command:        "claude",
		Mode:           "pty",
		PermissionMode: "default",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)
	_ = readUntilType(t, conn, protocol.EventTypePromptRequest)
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)

	if err := conn.WriteJSON(protocol.PermissionDecisionRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "permission_decision"},
		Decision:       "deny",
		PermissionMode: "default",
		TargetPath:     "README.md",
		PromptMessage:  "写 README 需要你的授权",
	}); err != nil {
		t.Fatalf("write permission decision request: %v", err)
	}

	select {
	case payload := <-ptyRunner.writeCh:
		if !strings.Contains(string(payload), "用户拒绝了刚才请求的文件修改/写入权限") {
			t.Fatalf("unexpected deny payload: %q", string(payload))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive deny prompt payload")
	}
	select {
	case decision := <-ptyRunner.permissionResponseWriteCh:
		t.Fatalf("unexpected structured deny decision: %q", decision)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestHandlerPlanDecisionWritesDecisionPayloadToRunner(t *testing.T) {
	ptyRunner := newHoldingStubRunner(protocol.NewPromptRequestEvent("ignored", "Claude 会话已就绪，可继续输入", nil))
	ptyRunner.claudeSessionID = "resume-plan-123"

	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }

	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "plan-decision")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "claude",
		Mode:        "pty",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)
	_ = readUntilType(t, conn, protocol.EventTypePromptRequest)
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)

	decision := `{"kind":"plan","sessionId":"session-test","answers":{"question-1":"继续"}}`
	if err := conn.WriteJSON(protocol.PlanDecisionRequestEvent{
		ClientEvent:     protocol.ClientEvent{Action: "plan_decision"},
		Decision:        decision,
		SessionID:       "session-test",
		ResumeSessionID: "resume-plan-123",
		ExecutionID:     "exec-plan-1",
		GroupID:         "group-plan-1",
		GroupTitle:      "Plan group",
		ContextID:       "ctx-plan-1",
		ContextTitle:    "Plan context",
		PromptMessage:   "请选择下一步",
		PermissionMode:  "default",
		Command:         "claude",
		CWD:             ".",
		TargetPath:      "README.md",
	}); err != nil {
		t.Fatalf("write plan decision request: %v", err)
	}

	select {
	case payload := <-ptyRunner.writeCh:
		if string(payload) != decision+"\n" {
			t.Fatalf("unexpected plan payload: %q", string(payload))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive plan payload")
	}
}

func TestHandlerPermissionDecisionWithoutRunnerReturnsError(t *testing.T) {
	h := newTestHandler()
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.PermissionDecisionRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "permission_decision"},
		Decision:       "approve",
		PermissionMode: "default",
		TargetPath:     "README.md",
		PromptMessage:  "写 README 需要你的授权",
	}); err != nil {
		t.Fatalf("write permission decision request: %v", err)
	}

	event := readUntilType(t, conn, protocol.EventTypeError)
	if event["msg"] != "当前没有可交互的 Claude 会话，无法继续处理该权限请求" {
		t.Fatalf("unexpected error event: %#v", event)
	}
}

func TestHandlerPermissionDecisionWithManagedFreshClaudeSessionCanHotSwap(t *testing.T) {
	firstRunner := newHoldingStubRunner(protocol.NewPromptRequestEvent("ignored", "写 README 需要你的授权", []string{"y", "n"}))
	secondRunner := newHoldingStubRunner()
	runnerIndex := 0

	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner {
		runnerIndex++
		if runnerIndex == 1 {
			return firstRunner
		}
		return secondRunner
	}
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "permission-no-pending")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "exec"},
		Command:        "claude",
		Mode:           "pty",
		PermissionMode: "default",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	firstRunner.WaitStarted(t)
	prompt := readUntilType(t, conn, protocol.EventTypePromptRequest)
	if resumeID, _ := prompt["resumeSessionId"].(string); strings.TrimSpace(resumeID) == "" {
		t.Fatalf("expected managed resume session id on prompt, got %#v", prompt)
	}
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)

	if err := conn.WriteJSON(protocol.PermissionDecisionRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "permission_decision"},
		Decision:       "approve",
		PermissionMode: "default",
		TargetPath:     "README.md",
		PromptMessage:  "写 README 需要你的授权",
	}); err != nil {
		t.Fatalf("write permission decision request: %v", err)
	}

	firstRunner.WaitClosed(t)
	secondRunner.WaitStarted(t)
	select {
	case <-secondRunner.writeCh:
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive continuation payload on hot-swapped runner")
	}
}

func TestHandlerPermissionDecisionWithoutHotSwapSupportReturnsError(t *testing.T) {
	base := newHoldingStubRunner(protocol.NewPromptRequestEvent("ignored", "写 README 需要你的授权", []string{"y", "n"}))
	runnerWithoutClaudeSession := &writeOnlyStubRunner{base: base}
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner { return runnerWithoutClaudeSession }
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "permission-no-control-support")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "exec"},
		Command:        "bash",
		Mode:           "pty",
		PermissionMode: "default",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	runnerWithoutClaudeSession.WaitStarted(t)

	if err := conn.WriteJSON(protocol.PermissionDecisionRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "permission_decision"},
		Decision:       "approve",
		PermissionMode: "default",
		TargetPath:     "README.md",
		PromptMessage:  "写 README 需要你的授权",
	}); err != nil {
		t.Fatalf("write permission decision request: %v", err)
	}

	event := readUntilType(t, conn, protocol.EventTypeError)
	if event["msg"] != "当前活跃会话不是可热重启恢复的 Claude PTY 会话" {
		t.Fatalf("unexpected error event: %#v", event)
	}
	select {
	case payload := <-base.writeCh:
		t.Fatalf("unexpected normal input payload: %q", string(payload))
	case <-time.After(200 * time.Millisecond):
	}
}

func TestHandlerPermissionDecisionApproveResumesAfterRunnerEnded(t *testing.T) {
	firstRunner := newStubRunner(protocol.ApplyRuntimeMeta(
		protocol.NewPromptRequestEvent("ignored", "写 README 需要你的授权", []string{"y", "n"}),
		protocol.RuntimeMeta{ResumeSessionID: "resume-123"},
	))
	firstRunner.claudeSessionID = "resume-123"
	secondRunner := newHoldingStubRunner()
	secondRunner.interactive = false
	secondRunner.onStart = func() {
		go func() {
			time.Sleep(80 * time.Millisecond)
			secondRunner.mu.Lock()
			secondRunner.interactive = true
			secondRunner.hasPendingPermission = true
			secondRunner.mu.Unlock()
		}()
	}
	runnerIndex := 0

	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner {
		runnerIndex++
		if runnerIndex == 1 {
			return firstRunner
		}
		return secondRunner
	}
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "permission-resume")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "exec"},
		Command:        "claude",
		Mode:           "pty",
		PermissionMode: "default",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	firstRunner.WaitStarted(t)
	prompt := readUntilType(t, conn, protocol.EventTypePromptRequest)
	if resumeID, _ := prompt["resumeSessionId"].(string); strings.TrimSpace(resumeID) == "" {
		t.Fatalf("expected managed resume session id on prompt, got %#v", prompt)
	}
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)

	if err := conn.WriteJSON(protocol.PermissionDecisionRequestEvent{
		ClientEvent:     protocol.ClientEvent{Action: "permission_decision"},
		Decision:        "approve",
		PermissionMode:  "default",
		ResumeSessionID: "resume-123",
		FallbackCommand: "claude",
		FallbackCWD:     "/tmp",
		TargetPath:      "README.md",
		PromptMessage:   "写 README 需要你的授权",
	}); err != nil {
		t.Fatalf("write permission decision request: %v", err)
	}

	secondRunner.WaitStarted(t)
	select {
	case payload := <-secondRunner.writeCh:
		if got := string(payload); got != "我已经批准这次文件修改权限。  不要继续复用刚才那次失败的工具调用；请基于这次已批准的权限，立即发起新的 Write/Edit 操作。  本次已授权的目标文件：`README.md`。  不要再次请求权限，不要只做解释，直接完成这次文件修改。  你上一次请求权限时的原始上下文如下，请按该上下文重新完成写入：写 README 需要你的授权\n" {
			t.Fatalf("unexpected continuation payload on resumed runner: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive continuation payload on resumed runner")
	}
}

func TestHandlerPermissionDecisionWithNonInteractiveRunnerStillHotSwaps(t *testing.T) {
	firstRunner := newNonInteractiveHoldingStubRunner(protocol.NewPromptRequestEvent("ignored", "写 README 需要你的授权", []string{"y", "n"}))
	firstRunner.claudeSessionID = "resume-non-interactive-123"
	secondRunner := newHoldingStubRunner()
	runnerIndex := 0
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner {
		runnerIndex++
		if runnerIndex == 1 {
			return firstRunner
		}
		return secondRunner
	}
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "permission-non-interactive")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "exec"},
		Command:        "claude",
		Mode:           "pty",
		PermissionMode: "default",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	firstRunner.WaitStarted(t)

	if err := conn.WriteJSON(protocol.InputRequestEvent{ClientEvent: protocol.ClientEvent{Action: "input"}, Data: "请创建 README.md 并写入 hello\n"}); err != nil {
		t.Fatalf("write input request: %v", err)
	}
	select {
	case payload := <-firstRunner.writeCh:
		if string(payload) != "请创建 README.md 并写入 hello\n" {
			t.Fatalf("unexpected initial input payload: %q", string(payload))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive initial input payload")
	}

	if err := conn.WriteJSON(protocol.PermissionDecisionRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "permission_decision"},
		Decision:       "approve",
		PermissionMode: "default",
		TargetPath:     "README.md",
		PromptMessage:  "写 README 需要你的授权",
	}); err != nil {
		t.Fatalf("write permission decision request: %v", err)
	}
	firstRunner.WaitClosed(t)
	secondRunner.WaitStarted(t)
	select {
	case payload := <-secondRunner.writeCh:
		if got := string(payload); got != "我已经批准这次文件修改权限。  不要继续复用刚才那次失败的工具调用；请基于这次已批准的权限，立即发起新的 Write/Edit 操作。  本次已授权的目标文件：`README.md`。  不要再次请求权限，不要只做解释，直接完成这次文件修改。  你上一次请求权限时的原始上下文如下，请按该上下文重新完成写入：写 README 需要你的授权\n" {
			t.Fatalf("unexpected continuation payload: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive continuation payload")
	}
	thinking := readUntilType(t, conn, protocol.EventTypeAgentState)
	if thinking["state"] != "THINKING" {
		t.Fatalf("expected THINKING state, got %#v", thinking)
	}
}

func TestHandlerReviewDecisionWithNonInteractiveRunnerReturnsError(t *testing.T) {
	ptyRunner := newNonInteractiveHoldingStubRunner(protocol.NewPromptRequestEvent("ignored", "等待输入", nil))
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "review-non-interactive")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{ClientEvent: protocol.ClientEvent{Action: "exec"}, Command: "claude", Mode: "pty"}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	ptyRunner.WaitStarted(t)

	if err := conn.WriteJSON(protocol.ReviewDecisionRequestEvent{ClientEvent: protocol.ClientEvent{Action: "review_decision"}, Decision: "accept"}); err != nil {
		t.Fatalf("write review decision request: %v", err)
	}

	event := readUntilType(t, conn, protocol.EventTypeError)
	if event["msg"] != "当前 Claude 会话尚未进入可直接确认的交互阶段，请先等待当前会话就绪后再提交审核决策" {
		t.Fatalf("unexpected error event: %#v", event)
	}
	select {
	case payload := <-ptyRunner.writeCh:
		t.Fatalf("unexpected payload written to non-interactive runner: %q", string(payload))
	case <-time.After(200 * time.Millisecond):
	}
}

func TestHandlerReviewDecisionSendsPromptToRunner(t *testing.T) {
	ptyRunner := newHoldingStubRunner(protocol.NewPromptRequestEvent("ignored", "等待输入", nil))
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "review-session")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{ClientEvent: protocol.ClientEvent{Action: "exec"}, Command: "claude", Mode: "pty"}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	ptyRunner.WaitStarted(t)

	if err := conn.WriteJSON(protocol.ReviewDecisionRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "review_decision"},
		Decision:       "accept",
		ContextID:      "diff:1",
		ContextTitle:   "最近 Diff",
		TargetPath:     "internal/ws/handler.go",
		PermissionMode: "acceptEdits",
	}); err != nil {
		t.Fatalf("write review decision request: %v", err)
	}

	select {
	case payload := <-ptyRunner.writeCh:
		got := string(payload)
		if got != "1\n" {
			t.Fatalf("unexpected review decision payload: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive review decision payload")
	}

	var thinking map[string]any
	for i := 0; i < 5; i++ {
		event := readUntilType(t, conn, protocol.EventTypeAgentState)
		if event["source"] == "review-decision" {
			thinking = event
			break
		}
	}
	if thinking == nil {
		t.Fatal("did not receive review-decision agent state")
	}
	if thinking["state"] != "THINKING" {
		t.Fatalf("expected THINKING state, got %#v", thinking)
	}
	if thinking["source"] != "review-decision" {
		t.Fatalf("expected review-decision source, got %#v", thinking)
	}
	if thinking["permissionMode"] != "acceptEdits" {
		t.Fatalf("expected acceptEdits permission mode, got %#v", thinking)
	}
}

func TestHandlerReviewDecisionUpdatesProjectionAndReviewState(t *testing.T) {
	ptyRunner := newHoldingStubRunner(
		protocol.NewPromptRequestEvent("ignored", "等待输入", nil),
		protocol.NewFileDiffEvent("ignored", "hhh.txt", "新增 hhh.txt", "+++ b/hhh.txt\n@@\n+测试功能\n", "text"),
	)
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	sessionID := createHistorySessionForHandlerTest(t, h, conn, "review-flow")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{ClientEvent: protocol.ClientEvent{Action: "exec"}, Command: "claude", Mode: "pty"}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	ptyRunner.WaitStarted(t)
	_ = readUntilType(t, conn, protocol.EventTypePromptRequest)
	_ = readUntilType(t, conn, protocol.EventTypeFileDiff)

	if err := conn.WriteJSON(protocol.ReviewDecisionRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "review_decision"},
		Decision:       "accept",
		ContextID:      "hhh.txt",
		TargetPath:     "hhh.txt",
		GroupID:        "hhh.txt",
		GroupTitle:     "hhh.txt",
		PermissionMode: "default",
	}); err != nil {
		t.Fatalf("write review decision request: %v", err)
	}

	select {
	case payload := <-ptyRunner.writeCh:
		if got := string(payload); got != "1\n" {
			t.Fatalf("unexpected review decision payload: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive review decision payload")
	}

	var reviewState map[string]any
	for i := 0; i < 8; i++ {
		event := readUntilType(t, conn, protocol.EventTypeReviewState)
		if event["type"] == protocol.EventTypeError {
			t.Fatalf("unexpected error event while waiting for review state: %#v", event)
		}
		reviewState = event
		break
	}
	if reviewState == nil {
		t.Fatal("did not receive review_state event")
	}
	groups, ok := reviewState["groups"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatalf("expected one review group, got %#v", reviewState)
	}
	activeGroup, ok := reviewState["activeGroup"].(map[string]any)
	if !ok {
		t.Fatalf("expected activeGroup payload, got %#v", reviewState)
	}
	if activeGroup["reviewStatus"] != "accepted" {
		t.Fatalf("expected accepted active group, got %#v", activeGroup)
	}
	files, ok := groups[0].(map[string]any)["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("expected one review file, got %#v", groups[0])
	}
	file, ok := files[0].(map[string]any)
	if !ok {
		t.Fatalf("expected review file payload, got %#v", files[0])
	}
	if pending, _ := file["pendingReview"].(bool); pending {
		t.Fatalf("expected review to be cleared, got %#v", file)
	}
	if status := file["reviewStatus"]; status != "accepted" {
		t.Fatalf("expected accepted review status, got %#v", file)
	}

	record, err := h.SessionStore.GetSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if len(record.Projection.Diffs) != 1 {
		t.Fatalf("expected one persisted diff, got %#v", record.Projection.Diffs)
	}
	if record.Projection.Diffs[0].PendingReview {
		t.Fatalf("expected persisted diff review to be cleared, got %#v", record.Projection.Diffs[0])
	}
	if record.Projection.Diffs[0].ReviewStatus != "accepted" {
		t.Fatalf("expected persisted diff to be accepted, got %#v", record.Projection.Diffs[0])
	}
}

func TestHandlerSetPermissionModeUpdatesRunner(t *testing.T) {
	ptyRunner := newHoldingStubRunner(protocol.NewPromptRequestEvent("ignored", "等待输入", nil))
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "permission-mode-session")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{ClientEvent: protocol.ClientEvent{Action: "exec"}, Command: "claude", Mode: "pty", PermissionMode: "acceptEdits"}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	ptyRunner.WaitStarted(t)

	if err := conn.WriteJSON(protocol.PermissionModeUpdateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "set_permission_mode"}, PermissionMode: "default"}); err != nil {
		t.Fatalf("write permission mode request: %v", err)
	}

	var state map[string]any
	for i := 0; i < 5; i++ {
		event := readUntilType(t, conn, protocol.EventTypeAgentState)
		if event["permissionMode"] == "default" {
			state = event
			break
		}
	}
	if state == nil {
		t.Fatal("did not receive updated permissionMode agent state")
	}
	if state["permissionMode"] != "default" {
		t.Fatalf("expected updated permission mode, got %#v", state)
	}
	if ptyRunner.lastPermissionMode != "default" {
		t.Fatalf("expected runner permission mode to update, got %q", ptyRunner.lastPermissionMode)
	}
}

func TestHandlerSetPermissionModeUpdatesActiveRunner(t *testing.T) {
	ptyRunner := newHoldingStubRunner(protocol.NewPromptRequestEvent("ignored", "等待输入", nil))
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "permission-mode-active-session")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "exec"},
		Command:        "claude",
		Mode:           "pty",
		PermissionMode: "acceptEdits",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	ptyRunner.WaitStarted(t)

	if err := conn.WriteJSON(protocol.PermissionModeUpdateRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "set_permission_mode"},
		PermissionMode: "default",
	}); err != nil {
		t.Fatalf("write set_permission_mode request: %v", err)
	}

	var state map[string]any
	for i := 0; i < 5; i++ {
		event := readUntilType(t, conn, protocol.EventTypeAgentState)
		if event["permissionMode"] == "default" {
			state = event
			break
		}
	}
	if state == nil {
		t.Fatal("did not receive updated permissionMode agent state")
	}
	if state["permissionMode"] != "default" {
		t.Fatalf("expected permissionMode to be default, got %#v", state)
	}
	if ptyRunner.lastPermissionMode != "default" {
		t.Fatalf("expected runner permission mode to update, got %q", ptyRunner.lastPermissionMode)
	}
}

func TestHandlerReviewDecisionWithoutRunner(t *testing.T) {
	h := newTestHandler()
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.ReviewDecisionRequestEvent{ClientEvent: protocol.ClientEvent{Action: "review_decision"}, Decision: "revert"}); err != nil {
		t.Fatalf("write review decision request: %v", err)
	}

	event := readUntilType(t, conn, protocol.EventTypeError)
	if event["msg"] != "当前 Claude 会话尚未进入可直接确认的交互阶段，请先等待当前会话就绪后再提交审核决策" {
		t.Fatalf("unexpected error event: %#v", event)
	}
}

func TestHandlerReviewDecisionAcceptAllowedInDefaultMode(t *testing.T) {
	ptyRunner := newHoldingStubRunner(protocol.NewPromptRequestEvent("ignored", "等待输入", nil))
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner { return ptyRunner }
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)
	_ = createHistorySessionForHandlerTest(t, h, conn, "review-default-mode")

	if err := conn.WriteJSON(protocol.ExecRequestEvent{ClientEvent: protocol.ClientEvent{Action: "exec"}, Command: "claude", Mode: "pty", PermissionMode: "default"}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)
	_ = readUntilType(t, conn, protocol.EventTypePromptRequest)
	_ = readUntilType(t, conn, protocol.EventTypeAgentState)

	if err := conn.WriteJSON(protocol.ReviewDecisionRequestEvent{
		ClientEvent:    protocol.ClientEvent{Action: "review_decision"},
		Decision:       "accept",
		ContextID:      "diff:1",
		ContextTitle:   "最近 Diff",
		TargetPath:     "internal/ws/handler.go",
		PermissionMode: "default",
	}); err != nil {
		t.Fatalf("write review decision request: %v", err)
	}

	select {
	case payload := <-ptyRunner.writeCh:
		if string(payload) != "1\n" {
			t.Fatalf("unexpected review payload: %q", string(payload))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive review decision payload")
	}

	thinking := readUntilType(t, conn, protocol.EventTypeAgentState)
	if thinking["state"] != "THINKING" {
		t.Fatalf("expected THINKING state, got %#v", thinking)
	}
	if thinking["permissionMode"] != "default" {
		t.Fatalf("expected default permission mode, got %#v", thinking)
	}
}

func TestHandlerReviewDecisionRejectsUnknownDecision(t *testing.T) {
	h := newTestHandler()
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.ReviewDecisionRequestEvent{ClientEvent: protocol.ClientEvent{Action: "review_decision"}, Decision: "shipit"}); err != nil {
		t.Fatalf("write review decision request: %v", err)
	}

	event := readUntilType(t, conn, protocol.EventTypeError)
	if event["msg"] != "review decision must be one of: accept, revert, revise" {
		t.Fatalf("unexpected error event: %#v", event)
	}
}

func TestHandlerRuntimeInfoReturnsContextSnapshot(t *testing.T) {
	h := newTestHandler()
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.RuntimeInfoRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "runtime_info"},
		Query:       "context",
		CWD:         ".",
	}); err != nil {
		t.Fatalf("write runtime_info request: %v", err)
	}

	event := readUntilType(t, conn, protocol.EventTypeRuntimeInfoResult)
	if event["query"] != "context" {
		t.Fatalf("expected context query, got %#v", event)
	}
	items, ok := event["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("expected runtime info items, got %#v", event)
	}
}

func TestHandlerRuntimeInfoRejectsUnknownQuery(t *testing.T) {
	h := newTestHandler()
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.RuntimeInfoRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "runtime_info"},
		Query:       "mystery",
		CWD:         ".",
	}); err != nil {
		t.Fatalf("write runtime_info request: %v", err)
	}

	event := readUntilType(t, conn, protocol.EventTypeError)
	if event["msg"] != "unsupported runtime_info query: mystery" {
		t.Fatalf("unexpected error event: %#v", event)
	}
}

func TestHandlerSlashCommandRuntimeInfoQueries(t *testing.T) {
	tests := []struct {
		name    string
		command string
		query   string
	}{
		{name: "help", command: "/help", query: "help"},
		{name: "context", command: "/context", query: "context"},
		{name: "model", command: "/model", query: "model"},
		{name: "cost", command: "/cost", query: "cost"},
		{name: "doctor", command: "/doctor", query: "doctor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler()
			conn := newTestConn(t, h)
			_, _ = readInitialEvents(t, conn)

			if err := conn.WriteJSON(protocol.SlashCommandRequestEvent{
				ClientEvent: protocol.ClientEvent{Action: "slash_command"},
				Command:     tt.command,
				CWD:         ".",
			}); err != nil {
				t.Fatalf("write slash command request: %v", err)
			}

			event := readUntilType(t, conn, protocol.EventTypeRuntimeInfoResult)
			if event["query"] != tt.query {
				t.Fatalf("expected query %q, got %#v", tt.query, event)
			}
		})
	}
}

func TestHandlerSlashCommandLocalOnlyCommands(t *testing.T) {
	tests := []string{"/clear", "/exit", "/quit", "/fast"}
	for _, command := range tests {
		t.Run(command, func(t *testing.T) {
			h := newTestHandler()
			conn := newTestConn(t, h)
			_, _ = readInitialEvents(t, conn)

			if err := conn.WriteJSON(protocol.SlashCommandRequestEvent{
				ClientEvent: protocol.ClientEvent{Action: "slash_command"},
				Command:     command,
			}); err != nil {
				t.Fatalf("write slash command request: %v", err)
			}

			event := readUntilType(t, conn, protocol.EventTypeRuntimeInfoResult)
			items, ok := event["items"].([]any)
			if !ok || len(items) == 0 {
				t.Fatalf("expected runtime info items, got %#v", event)
			}
			first, ok := items[0].(map[string]any)
			if !ok || first["status"] != "local-only" {
				t.Fatalf("expected local-only status, got %#v", event)
			}
		})
	}
}

func TestHandlerSlashCommandDiffRequiresContext(t *testing.T) {
	h := newTestHandler()
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.SlashCommandRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "slash_command"},
		Command:     "/diff",
	}); err != nil {
		t.Fatalf("write slash command request: %v", err)
	}

	event := readUntilType(t, conn, protocol.EventTypeError)
	if event["msg"] != "/diff requires targetDiff context" {
		t.Fatalf("unexpected error event: %#v", event)
	}
}

func TestHandlerSlashCommandExecMappings(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{name: "init", command: "/init", want: "claude /init"},
		{name: "compact", command: "/compact", want: "claude /compact"},
		{name: "run", command: "/run echo hi", want: "echo hi"},
		{name: "add-dir", command: "/add-dir /tmp/demo", want: "claude /add-dir /tmp/demo"},
		{name: "git commit quote", command: "/git commit hello", want: "git commit -m \"hello\""},
		{name: "test fallback", command: "/test path/to/file", want: "go test ./..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runnerStub := newHoldingStubRunner()
			h := newTestHandler()
			h.NewPtyRunner = func() runner.Runner { return runnerStub }
			conn := newTestConn(t, h)
			_, _ = readInitialEvents(t, conn)

			if err := conn.WriteJSON(protocol.SlashCommandRequestEvent{
				ClientEvent: protocol.ClientEvent{Action: "slash_command"},
				Command:     tt.command,
				CWD:         ".",
			}); err != nil {
				t.Fatalf("write slash command request: %v", err)
			}

			thinking := readUntilType(t, conn, protocol.EventTypeAgentState)
			if thinking["state"] != "THINKING" {
				t.Fatalf("expected THINKING state, got %#v", thinking)
			}
			select {
			case <-runnerStub.writeCh:
				// ignore stray writes
			default:
			}
		})
	}
}

func TestHandlerSessionDeleteRemovesHistorySessionFromList(t *testing.T) {
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: "session-a"}); err != nil {
		t.Fatalf("write session create request: %v", err)
	}
	createdA := readUntilSessionCreated(t, conn)
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)
	summaryA, ok := createdA["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary payload, got %#v", createdA)
	}
	sessionA, _ := summaryA["id"].(string)
	if sessionA == "" {
		t.Fatalf("expected session A id, got %#v", createdA)
	}

	if err := conn.WriteJSON(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: "session-b"}); err != nil {
		t.Fatalf("write session create request: %v", err)
	}
	createdB := readUntilSessionCreated(t, conn)
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)
	summaryB, ok := createdB["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary payload, got %#v", createdB)
	}
	sessionB, _ := summaryB["id"].(string)
	if sessionB == "" || sessionB == sessionA {
		t.Fatalf("expected distinct session B id, got %q", sessionB)
	}

	if err := conn.WriteJSON(protocol.SessionDeleteRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_delete"}, SessionID: sessionA}); err != nil {
		t.Fatalf("write session delete request: %v", err)
	}

	listEvent := readUntilType(t, conn, protocol.EventTypeSessionListResult)
	items, ok := listEvent["items"].([]any)
	if !ok {
		t.Fatalf("expected session list items, got %#v", listEvent)
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["id"] == sessionA {
			t.Fatalf("expected deleted session removed from list, got %#v", items)
		}
	}
	if _, err := h.SessionStore.GetSession(context.Background(), sessionA); err == nil {
		t.Fatal("expected deleted history session lookup to fail")
	}
	if _, err := h.SessionStore.GetSession(context.Background(), sessionB); err != nil {
		t.Fatalf("expected current session to remain, got %v", err)
	}
}

func TestHandlerSessionDeleteCurrentSessionCleansRuntimeAndFallsBack(t *testing.T) {
	runnerA := newSwitchableStubRunner()
	firstRunner := runnerA
	runnerB := newSwitchableStubRunner()
	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner {
		if runnerA != nil {
			r := runnerA
			runnerA = nil
			return r
		}
		return runnerB
	}
	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: "session-a"}); err != nil {
		t.Fatalf("write initial session create request: %v", err)
	}
	createdA := readUntilSessionCreated(t, conn)
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)
	summaryA, ok := createdA["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary payload, got %#v", createdA)
	}
	sessionA, _ := summaryA["id"].(string)
	if sessionA == "" {
		t.Fatalf("expected session A id, got %#v", createdA)
	}

	if err := conn.WriteJSON(protocol.ExecRequestEvent{ClientEvent: protocol.ClientEvent{Action: "exec"}, Command: "claude", Mode: "pty"}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	firstRunner.WaitStarted(t)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "THINKING", false)

	if err := conn.WriteJSON(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: "session-b"}); err != nil {
		t.Fatalf("write session create request: %v", err)
	}
	createdB := readUntilSessionCreated(t, conn)
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)
	summaryB, ok := createdB["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary payload, got %#v", createdB)
	}
	sessionB, _ := summaryB["id"].(string)
	if sessionB == "" || sessionB == sessionA {
		t.Fatalf("expected distinct session B id, got %q", sessionB)
	}
	firstRunner.WaitClosed(t)

	if err := conn.WriteJSON(protocol.ExecRequestEvent{ClientEvent: protocol.ClientEvent{Action: "exec"}, Command: "claude", Mode: "pty"}); err != nil {
		t.Fatalf("write exec request for session B: %v", err)
	}
	runnerB.WaitStarted(t)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "THINKING", false)

	if err := conn.WriteJSON(protocol.SessionDeleteRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_delete"}, SessionID: sessionB}); err != nil {
		t.Fatalf("write session delete request: %v", err)
	}

	listEvent := readUntilType(t, conn, protocol.EventTypeSessionListResult)
	items, ok := listEvent["items"].([]any)
	if !ok {
		t.Fatalf("expected session list items, got %#v", listEvent)
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["id"] == sessionB {
			t.Fatalf("expected deleted current session removed from list, got %#v", items)
		}
	}
	history := readUntilSessionHistory(t, conn)
	if history["sessionId"] != sessionA {
		t.Fatalf("expected fallback history for session A, got %#v", history)
	}
	runnerB.WaitClosed(t)
	if _, err := h.SessionStore.GetSession(context.Background(), sessionB); err == nil {
		t.Fatal("expected deleted current session lookup to fail")
	}

	runnerB.Emit(protocol.NewLogEvent("ignored", "late output from deleted session B", "stdout"))
	runnerB.Emit(protocol.NewStepUpdateEvent("ignored", "late step from deleted session B", "running", "internal/ws/handler.go", "reading", "claude"))

	recordA, err := h.SessionStore.GetSession(context.Background(), sessionA)
	if err != nil {
		t.Fatalf("get session A: %v", err)
	}
	textsA := sessionLogTexts(recordA)
	if containsText(textsA, "late output from deleted session B") || containsText(textsA, "late step from deleted session B") {
		t.Fatalf("did not expect deleted session events to leak into fallback session, got %#v", textsA)
	}
}

func TestHandlerSessionLoadKeepsOldRunnerEventsInOriginalSessionProjection(t *testing.T) {
	runnerA := newSwitchableStubRunner()
	firstRunner := runnerA
	runnerB := newSwitchableStubRunner()

	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner {
		if runnerA != nil {
			r := runnerA
			runnerA = nil
			return r
		}
		return runnerB
	}

	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: "session-a"}); err != nil {
		t.Fatalf("write initial session create request: %v", err)
	}
	createdA := readUntilSessionCreated(t, conn)
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)
	summaryA, ok := createdA["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary payload, got %#v", createdA)
	}
	sessionA, _ := summaryA["id"].(string)
	if sessionA == "" {
		t.Fatalf("expected initial session id, got %#v", createdA)
	}

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "claude",
		Mode:        "pty",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	firstRunner.WaitStarted(t)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "THINKING", false)

	if err := conn.WriteJSON(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: "session-b"}); err != nil {
		t.Fatalf("write session create request: %v", err)
	}
	created := readUntilSessionCreated(t, conn)
	summary, ok := created["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary payload, got %#v", created)
	}
	sessionB, _ := summary["id"].(string)
	if sessionB == "" || sessionB == sessionA {
		t.Fatalf("expected new session id, got %q", sessionB)
	}
	firstRunner.WaitClosed(t)

	firstRunner.Emit(protocol.NewLogEvent("ignored", "late output from session A", "stdout"))
	firstRunner.Emit(protocol.NewStepUpdateEvent("ignored", "late step from session A", "running", "internal/ws/handler.go", "reading", "claude"))

	recordA, err := h.SessionStore.GetSession(context.Background(), sessionA)
	if err != nil {
		t.Fatalf("get session A: %v", err)
	}
	recordB, err := h.SessionStore.GetSession(context.Background(), sessionB)
	if err != nil {
		t.Fatalf("get session B: %v", err)
	}

	textsA := sessionLogTexts(recordA)
	textsB := sessionLogTexts(recordB)
	if !containsText(textsA, "late output from session A") {
		t.Fatalf("expected late output in session A projection, got %#v", textsA)
	}
	if !containsText(textsA, "late step from session A") {
		t.Fatalf("expected late step in session A projection, got %#v", textsA)
	}
	if containsText(textsB, "late output from session A") || containsText(textsB, "late step from session A") {
		t.Fatalf("did not expect session A events in session B projection, got %#v", textsB)
	}
}

func TestHandlerSessionLoadCleansUpPreviousRuntime(t *testing.T) {
	runnerA := newSwitchableStubRunner()
	firstRunner := runnerA
	runnerB := newSwitchableStubRunner()

	h := newTestHandler()
	tempStore, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	h.SessionStore = tempStore
	h.NewPtyRunner = func() runner.Runner {
		if runnerA != nil {
			r := runnerA
			runnerA = nil
			return r
		}
		return runnerB
	}

	conn := newTestConn(t, h)
	_, _ = readInitialEvents(t, conn)

	if err := conn.WriteJSON(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: "session-a"}); err != nil {
		t.Fatalf("write initial session create request: %v", err)
	}
	createdA := readUntilSessionCreated(t, conn)
	_ = readUntilType(t, conn, protocol.EventTypeSessionListResult)
	summaryA, ok := createdA["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary payload, got %#v", createdA)
	}
	sessionA, _ := summaryA["id"].(string)
	if sessionA == "" {
		t.Fatalf("expected initial session id, got %#v", createdA)
	}

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "claude",
		Mode:        "pty",
	}); err != nil {
		t.Fatalf("write exec request: %v", err)
	}
	firstRunner.WaitStarted(t)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "THINKING", false)

	if err := conn.WriteJSON(protocol.SessionCreateRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_create"}, Title: "session-b"}); err != nil {
		t.Fatalf("write session create request: %v", err)
	}
	created := readUntilSessionCreated(t, conn)
	summary, ok := created["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary payload, got %#v", created)
	}
	sessionB, _ := summary["id"].(string)
	if sessionB == "" || sessionB == sessionA {
		t.Fatalf("expected new session id, got %q", sessionB)
	}
	firstRunner.WaitClosed(t)

	if err := conn.WriteJSON(protocol.ExecRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "exec"},
		Command:     "claude",
		Mode:        "pty",
	}); err != nil {
		t.Fatalf("write exec request for session B: %v", err)
	}
	runnerB.WaitStarted(t)
	requireAgentState(t, readUntilType(t, conn, protocol.EventTypeAgentState), "THINKING", false)

	runnerB.Emit(protocol.NewLogEvent("ignored", "live output from session B", "stdout"))
	logEvent := readUntilType(t, conn, protocol.EventTypeLog)
	if logEvent["msg"] != "live output from session B" {
		t.Fatalf("unexpected log event payload: %#v", logEvent)
	}

	if err := conn.WriteJSON(protocol.SessionLoadRequestEvent{ClientEvent: protocol.ClientEvent{Action: "session_load"}, SessionID: sessionA}); err != nil {
		t.Fatalf("write session load request: %v", err)
	}
	history := readUntilSessionHistory(t, conn)
	if history["sessionId"] != sessionA {
		t.Fatalf("expected session history for session A, got %#v", history)
	}
	runnerB.WaitClosed(t)
}
