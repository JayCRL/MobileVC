package relayclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"mobilevc/internal/data"
	"mobilevc/internal/engine"
	"mobilevc/internal/gateway"
	"mobilevc/internal/protocol"
	"mobilevc/internal/relay"
)

type relayBlockingProjectionStore struct {
	data.Store
	mu          sync.Mutex
	saveStarted chan struct{}
	saveRelease chan struct{}
	saveDone    chan struct{}
	saveCount   int
	saveDelay   time.Duration
}

func newRelayBlockingProjectionStore(store data.Store) *relayBlockingProjectionStore {
	return &relayBlockingProjectionStore{
		Store:       store,
		saveStarted: make(chan struct{}),
		saveRelease: make(chan struct{}),
		saveDone:    make(chan struct{}),
		saveDelay:   3 * time.Second,
	}
}

func (s *relayBlockingProjectionStore) SaveProjection(ctx context.Context, sessionID string, projection data.ProjectionSnapshot) (data.SessionSummary, error) {
	return s.SaveProjectionWithOptions(ctx, sessionID, projection)
}

func (s *relayBlockingProjectionStore) SaveProjectionWithOptions(ctx context.Context, sessionID string, projection data.ProjectionSnapshot, opts ...data.ProjectionSaveOption) (data.SessionSummary, error) {
	s.mu.Lock()
	s.saveCount++
	count := s.saveCount
	s.mu.Unlock()
	if count == 1 {
		close(s.saveStarted)
		defer close(s.saveDone)
		if s.saveDelay > 0 {
			timer := time.NewTimer(s.saveDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return data.SessionSummary{}, ctx.Err()
			case <-timer.C:
			}
		}
		select {
		case <-ctx.Done():
			return data.SessionSummary{}, ctx.Err()
		case <-s.saveRelease:
		}
	}
	return relaySaveProjectionWithOptions(s.Store, ctx, sessionID, projection, opts...)
}

func (s *relayBlockingProjectionStore) waitSaveStarted(t *testing.T) {
	t.Helper()
	select {
	case <-s.saveStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("projection save did not start")
	}
}

func (s *relayBlockingProjectionStore) releaseSave() {
	select {
	case <-s.saveRelease:
	default:
		close(s.saveRelease)
	}
}

func (s *relayBlockingProjectionStore) waitSaveDone(t *testing.T) {
	t.Helper()
	select {
	case <-s.saveDone:
	case <-time.After(5 * time.Second):
		t.Fatal("projection save did not finish")
	}
}

func relaySaveProjectionWithOptions(store data.Store, ctx context.Context, sessionID string, projection data.ProjectionSnapshot, opts ...data.ProjectionSaveOption) (data.SessionSummary, error) {
	if optionStore, ok := store.(data.ProjectionOptionStore); ok {
		return optionStore.SaveProjectionWithOptions(ctx, sessionID, projection, opts...)
	}
	return store.SaveProjection(ctx, sessionID, projection)
}

type relayHoldingRunner struct {
	started chan struct{}
	closed  chan struct{}
}

func newRelayHoldingRunner() *relayHoldingRunner {
	return &relayHoldingRunner{
		started: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (r *relayHoldingRunner) Run(ctx context.Context, _ engine.ExecRequest, _ engine.EventSink) error {
	select {
	case <-r.started:
	default:
		close(r.started)
	}
	<-ctx.Done()
	return nil
}

func (r *relayHoldingRunner) Write(ctx context.Context, _ []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (r *relayHoldingRunner) Close() error {
	select {
	case <-r.closed:
	default:
		close(r.closed)
	}
	return nil
}

func (r *relayHoldingRunner) CanAcceptInteractiveInput() bool {
	return true
}

func (r *relayHoldingRunner) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-r.started:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not start")
	}
}

func TestGatewayConnRelayFramesContinueDuringProjectionSave(t *testing.T) {
	fileStore, err := data.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new temp store: %v", err)
	}
	blockingStore := newRelayBlockingProjectionStore(fileStore)
	runner := newRelayHoldingRunner()
	handler := gateway.NewHandler("test", blockingStore)
	t.Cleanup(handler.Close)
	handler.NewPtyRunner = func() engine.Runner { return runner }

	serverConn, clientConn := newRelayClientTestConns(t)
	t.Cleanup(func() { _ = serverConn.Close() })
	gatewayConn := newGatewayConn(clientConn, "rs_gateway")
	gatewayConn.readQueueTimeout = 50 * time.Millisecond
	t.Cleanup(func() { _ = gatewayConn.Close() })

	handlerDone := make(chan struct{})
	go func() {
		handler.ServeClientConn(context.Background(), gatewayConn)
		close(handlerDone)
	}()
	t.Cleanup(func() {
		_ = gatewayConn.Close()
		select {
		case <-handlerDone:
		case <-time.After(2 * time.Second):
			t.Fatal("gateway handler did not stop")
		}
	})

	writeRelayFrame(t, serverConn, relay.ClientAttachedFrame{
		Type: relay.TypeClientAttached, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_phone",
	})
	waitRelayAttached(t, gatewayConn)
	writeRelayPayload(t, serverConn, "initial-ping", map[string]any{
		"action": "ping",
		"pingId": "initial-ping",
	})
	readRelayPong(t, serverConn, "initial-ping")

	writeRelayPayload(t, serverConn, "create", map[string]any{
		"action": "session_create",
		"title":  "relay-projection-save",
	})
	sessionID := readRelaySessionCreated(t, serverConn)

	writeRelayPayload(t, serverConn, "exec", map[string]any{
		"action":         "exec",
		"sessionId":      sessionID,
		"clientActionId": "exec-block-save",
		"cmd":            "claude",
		"mode":           "pty",
	})
	blockingStore.waitSaveStarted(t)

	pingIDs := make([]string, 0, relayReadQueueSize+4)
	for i := 0; i < relayReadQueueSize+4; i++ {
		pingID := "during-save-" + strconv.Itoa(i)
		pingIDs = append(pingIDs, pingID)
		writeRelayPayload(t, serverConn, pingID, map[string]any{
			"action": "ping",
			"pingId": pingID,
		})
	}
	readRelayPongs(t, serverConn, pingIDs)

	if err := gatewayConn.closeReason(); err != nil && strings.Contains(err.Error(), errRelayReadQueueFull.Error()) {
		t.Fatalf("relay gateway read queue filled during projection save: %v", err)
	}

	blockingStore.releaseSave()
	blockingStore.waitSaveDone(t)
	runner.waitStarted(t)
}

func writeRelayFrame(t *testing.T, conn any, frame any) {
	t.Helper()
	writer, ok := conn.(interface{ WriteJSON(any) error })
	if !ok {
		t.Fatalf("relay test connection cannot write JSON")
	}
	if err := writer.WriteJSON(frame); err != nil {
		t.Fatalf("write relay frame: %v", err)
	}
}

func waitRelayAttached(t *testing.T, gateway *gatewayConn) {
	t.Helper()
	select {
	case <-gateway.attachCh:
	case <-time.After(5 * time.Second):
		t.Fatal("relay client attach was not consumed by gateway connection")
	}
}

func writeRelayPayload(t *testing.T, conn any, messageID string, payload map[string]any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal relay payload: %v", err)
	}
	writeRelayFrame(t, conn, relay.ForwardEnvelope{
		Type:            relay.TypeRelayForward,
		Version:         relay.Version,
		SessionID:       "rs_gateway",
		ClientID:        "rc_phone",
		Direction:       relay.DirectionClientToAgent,
		MessageID:       messageID,
		ContentType:     relay.ContentTypeMobileVC,
		Encryption:      relay.EncryptionNone,
		PayloadEncoding: relay.PayloadBase64URL,
		Payload:         base64.RawURLEncoding.EncodeToString(raw),
	})
}

func readRelayPayload(t *testing.T, conn any) map[string]any {
	t.Helper()
	reader, ok := conn.(interface{ ReadJSON(any) error })
	if !ok {
		t.Fatalf("relay test connection cannot read JSON")
	}
	deadline, ok := conn.(interface{ SetReadDeadline(time.Time) error })
	if ok {
		if err := deadline.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			t.Fatalf("set relay read deadline: %v", err)
		}
	}
	for {
		var env relay.ForwardEnvelope
		if err := reader.ReadJSON(&env); err != nil {
			t.Fatalf("read relay forward: %v", err)
		}
		if env.Type != relay.TypeRelayForward || env.Direction != relay.DirectionAgentToClient {
			continue
		}
		raw, err := relay.DecodePayloadBase64URL(env.Payload)
		if err != nil {
			t.Fatalf("decode relay payload: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("unmarshal relay payload: %v", err)
		}
		return payload
	}
}

func readRelaySessionCreated(t *testing.T, conn any) string {
	t.Helper()
	for {
		payload := readRelayPayload(t, conn)
		if payload["type"] != protocol.EventTypeSessionCreated {
			continue
		}
		summary, ok := payload["summary"].(map[string]any)
		if !ok {
			t.Fatalf("expected session summary payload, got %#v", payload)
		}
		sessionID, _ := summary["id"].(string)
		if sessionID == "" {
			t.Fatalf("expected created session id, got %#v", payload)
		}
		return sessionID
	}
}

func readRelayPong(t *testing.T, conn any, pingID string) {
	t.Helper()
	readRelayPongs(t, conn, []string{pingID})
}

func readRelayPongs(t *testing.T, conn any, pingIDs []string) {
	t.Helper()
	pending := make(map[string]struct{}, len(pingIDs))
	for _, pingID := range pingIDs {
		pending[pingID] = struct{}{}
	}
	for {
		if len(pending) == 0 {
			return
		}
		payload := readRelayPayload(t, conn)
		if payload["type"] != "pong" {
			continue
		}
		pingID, _ := payload["pingId"].(string)
		if _, ok := pending[pingID]; ok {
			delete(pending, pingID)
		}
	}
}
