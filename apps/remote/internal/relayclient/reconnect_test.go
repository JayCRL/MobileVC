package relayclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"mobilevc/internal/gateway"
	"mobilevc/internal/relay"
	"mobilevc/internal/relay/e2ee"
)

func TestNormalizedBackoffDefaults(t *testing.T) {
	backoff := normalizedBackoff(ReconnectBackoff{})
	if backoff.Initial != 500*time.Millisecond {
		t.Fatalf("initial backoff: got %s", backoff.Initial)
	}
	if backoff.Max != 5*time.Second {
		t.Fatalf("max backoff: got %s", backoff.Max)
	}
}

func TestNextBackoffCapsAtMax(t *testing.T) {
	got := nextBackoff(ReconnectBackoff{
		Initial: 4 * time.Second,
		Max:     5 * time.Second,
	})
	if got != 5*time.Second {
		t.Fatalf("next backoff: got %s", got)
	}
}

func TestReconnectDeadlineStartsFromCurrentDisconnect(t *testing.T) {
	grace := 30 * time.Second
	first := reconnectDeadline(grace)
	time.Sleep(time.Millisecond)
	second := reconnectDeadline(grace)
	if !second.After(first) {
		t.Fatalf("deadline was not refreshed: first=%s second=%s", first, second)
	}
}

func TestReconnectUntilAcceptedDoesNotExpireAtAgentGrace(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := reconnectUntilAccepted(ctx, Config{
			RelayURL:         "ws://127.0.0.1:1",
			AgentGracePeriod: time.Millisecond,
			ReconnectBackoff: ReconnectBackoff{
				Initial: time.Millisecond,
				Max:     time.Millisecond,
			},
		}, "rs_saved", "agent-reconnect-secret")
		errCh <- err
	}()

	select {
	case err := <-errCh:
		t.Fatalf("reconnect returned before context cancellation: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("reconnect did not stop after context cancellation")
	}
}

func TestRunRegistersNewSessionAfterRotate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	registrations := make(chan relay.AgentRegisterFrame, 2)
	relayURL := startRelayRegisterStub(t, ctx, registrations)
	nodeStore, err := e2ee.LoadOrCreateNodeIdentityStore(filepath.Join(t.TempDir(), e2ee.NodeIdentityFileName))
	if err != nil {
		t.Fatal(err)
	}
	downloadRoot := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "agent-session.json")
	events := make(chan PairingReadyEvent, 2)
	handler := &rotateThenStopHandler{cancel: cancel}
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, Config{
			RelayURL:          relayURL,
			PairingTTL:        time.Minute,
			AgentGracePeriod:  time.Minute,
			PairingEventPath:  filepath.Join(t.TempDir(), "pairing.json"),
			SessionStatePath:  statePath,
			DownloadRoots:     []string{downloadRoot},
			Capabilities:      e2ee.PlaintextTestCapabilities(),
			NodeIdentityStore: nodeStore,
		}, handler, func(_ string, event PairingReadyEvent) error {
			events <- event
			return nil
		})
	}()

	firstEvent := readPairingEvent(t, events)
	secondEvent := readPairingEvent(t, events)
	if firstEvent.SessionID == secondEvent.SessionID {
		t.Fatalf("rotate reused relay session id %q", firstEvent.SessionID)
	}
	if firstEvent.PairingSecret == secondEvent.PairingSecret {
		t.Fatal("rotate reused pairing secret")
	}
	firstRegister := readAgentRegister(t, registrations)
	secondRegister := readAgentRegister(t, registrations)
	if firstRegister.SessionID != firstEvent.SessionID || secondRegister.SessionID != secondEvent.SessionID {
		t.Fatalf("registrations did not match pairing events: %#v %#v", firstRegister, secondRegister)
	}
	saved, err := newAgentSessionStore(statePath).load()
	if err != nil {
		t.Fatalf("load saved agent session: %v", err)
	}
	if saved.SessionID != secondEvent.SessionID {
		t.Fatalf("saved agent session was not rotated: %#v event=%#v", saved, secondEvent)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after test cancellation")
	}
}

func TestRunReconnectsSavedAgentSessionWithoutNewPairingEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agentReconnects := make(chan relay.AgentReconnectFrame, 1)
	relayURL := startRelayReconnectStub(t, ctx, agentReconnects)
	nodeStore, err := e2ee.LoadOrCreateNodeIdentityStore(filepath.Join(t.TempDir(), e2ee.NodeIdentityFileName))
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(t.TempDir(), "agent-session.json")
	store := newAgentSessionStore(statePath)
	if err := store.save(agentSessionState{
		SessionID:       "rs_saved",
		ReconnectSecret: "agent-reconnect-secret",
	}); err != nil {
		t.Fatalf("save session state: %v", err)
	}
	events := make(chan PairingReadyEvent, 1)
	handler := &cancelHandler{cancel: cancel}
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, Config{
			RelayURL:          relayURL,
			PairingTTL:        time.Minute,
			AgentGracePeriod:  time.Minute,
			PairingEventPath:  filepath.Join(t.TempDir(), "pairing.json"),
			SessionStatePath:  statePath,
			DownloadRoots:     []string{t.TempDir()},
			Capabilities:      e2ee.PlaintextTestCapabilities(),
			NodeIdentityStore: nodeStore,
		}, handler, func(_ string, event PairingReadyEvent) error {
			events <- event
			return nil
		})
	}()

	reconnect := readAgentReconnect(t, agentReconnects)
	if reconnect.SessionID != "rs_saved" || reconnect.AgentReconnectSecret != "agent-reconnect-secret" {
		t.Fatalf("unexpected reconnect frame: %#v", reconnect)
	}
	select {
	case event := <-events:
		t.Fatalf("unexpected new pairing event for saved session: %#v", event)
	default:
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after reconnect test cancellation")
	}
}

func TestRunServesHandlerAfterProductionE2EEHandshake(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	registrations := make(chan relay.AgentRegisterFrame, 1)
	clientEphemeral, err := e2ee.NewEphemeralKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	pairingEvents := make(chan PairingReadyEvent, 1)
	relayURL := startRelayRegisterProductionE2EEStub(t, ctx, registrations, pairingEvents, clientEphemeral)
	nodeStore, err := e2ee.LoadOrCreateNodeIdentityStore(filepath.Join(t.TempDir(), e2ee.NodeIdentityFileName))
	if err != nil {
		t.Fatal(err)
	}
	handlerCalled := make(chan gateway.RelayE2EEInfo, 1)
	handler := &relayE2EEInfoHandler{cancel: cancel, called: handlerCalled}
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, Config{
			RelayURL:          relayURL,
			PairingTTL:        time.Minute,
			AgentGracePeriod:  time.Minute,
			PairingEventPath:  filepath.Join(t.TempDir(), "pairing.json"),
			SessionStatePath:  filepath.Join(t.TempDir(), "agent-session.json"),
			DownloadRoots:     []string{t.TempDir()},
			Capabilities:      e2ee.ProductionCapabilities(),
			NodeIdentityStore: nodeStore,
		}, handler, func(_ string, event PairingReadyEvent) error {
			pairingEvents <- event
			return nil
		})
	}()

	register := readAgentRegister(t, registrations)
	if register.Capabilities == nil {
		t.Fatal("registration omitted production capabilities")
	}
	if err := e2ee.ValidateProductionCapabilities(*register.Capabilities); err != nil {
		t.Fatalf("registration did not use production capabilities: %v", err)
	}
	info := readRelayE2EEInfo(t, handlerCalled)
	if !info.Enabled || info.SessionID != register.SessionID || info.ClientID != "rc_test" || info.HandshakeID != "hs_pairing" {
		t.Fatalf("handler was not served after production e2ee readiness: register=%#v info=%#v", register, info)
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after production e2ee handler cancellation")
	}
}

func TestRunReconnectsSavedAgentSessionAfterPairingTTL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agentReconnects := make(chan relay.AgentReconnectFrame, 1)
	relayURL := startRelayReconnectStub(t, ctx, agentReconnects)
	nodeStore, err := e2ee.LoadOrCreateNodeIdentityStore(filepath.Join(t.TempDir(), e2ee.NodeIdentityFileName))
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(t.TempDir(), "agent-session.json")
	if err := newAgentSessionStore(statePath).save(agentSessionState{
		SessionID:       "rs_saved",
		ReconnectSecret: "saved-reconnect-secret",
	}); err != nil {
		t.Fatalf("save session state: %v", err)
	}
	events := make(chan PairingReadyEvent, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, Config{
			RelayURL:          relayURL,
			PairingTTL:        time.Minute,
			AgentGracePeriod:  time.Minute,
			PairingEventPath:  filepath.Join(t.TempDir(), "pairing.json"),
			SessionStatePath:  statePath,
			DownloadRoots:     []string{t.TempDir()},
			Capabilities:      e2ee.PlaintextTestCapabilities(),
			NodeIdentityStore: nodeStore,
		}, &cancelHandler{cancel: cancel}, func(_ string, event PairingReadyEvent) error {
			events <- event
			return nil
		})
	}()

	reconnect := readAgentReconnect(t, agentReconnects)
	if reconnect.SessionID != "rs_saved" || reconnect.AgentReconnectSecret != "saved-reconnect-secret" {
		t.Fatalf("unexpected reconnect frame: %#v", reconnect)
	}
	select {
	case event := <-events:
		t.Fatalf("unexpected new pairing event for saved session: %#v", event)
	default:
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after expired saved session test cancellation")
	}
}

func TestRunDeletesRejectedSavedSessionAndRegistersNewPairing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agentReconnects := make(chan relay.AgentReconnectFrame, 1)
	registrations := make(chan relay.AgentRegisterFrame, 1)
	relayURL := startRelayRejectReconnectThenRegisterStub(t, ctx, agentReconnects, registrations)
	nodeStore, err := e2ee.LoadOrCreateNodeIdentityStore(filepath.Join(t.TempDir(), e2ee.NodeIdentityFileName))
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(t.TempDir(), "agent-session.json")
	if err := newAgentSessionStore(statePath).save(agentSessionState{
		SessionID:       "rs_stale",
		ReconnectSecret: "stale-reconnect-secret",
	}); err != nil {
		t.Fatalf("save stale session state: %v", err)
	}
	events := make(chan PairingReadyEvent, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, Config{
			RelayURL:          relayURL,
			PairingTTL:        time.Minute,
			AgentGracePeriod:  time.Minute,
			PairingEventPath:  filepath.Join(t.TempDir(), "pairing.json"),
			SessionStatePath:  statePath,
			DownloadRoots:     []string{t.TempDir()},
			Capabilities:      e2ee.PlaintextTestCapabilities(),
			NodeIdentityStore: nodeStore,
		}, &cancelHandler{cancel: cancel}, func(_ string, event PairingReadyEvent) error {
			events <- event
			return nil
		})
	}()

	if reconnect := readAgentReconnect(t, agentReconnects); reconnect.SessionID != "rs_stale" {
		t.Fatalf("unexpected stale reconnect frame: %#v", reconnect)
	}
	register := readAgentRegister(t, registrations)
	event := readPairingEvent(t, events)
	if register.SessionID == "rs_stale" || register.SessionID != event.SessionID {
		t.Fatalf("new registration did not replace stale session: register=%#v event=%#v", register, event)
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after rejected reconnect test cancellation")
	}
}

type rotateThenStopHandler struct {
	calls  atomic.Int32
	cancel context.CancelFunc
}

type cancelHandler struct {
	cancel context.CancelFunc
}

type relayE2EEInfoHandler struct {
	cancel context.CancelFunc
	called chan<- gateway.RelayE2EEInfo
}

func (h *cancelHandler) ServeClientConn(_ context.Context, client gateway.ClientConn) {
	h.cancel()
	_ = client.Close()
}

func (h *relayE2EEInfoHandler) ServeClientConn(_ context.Context, client gateway.ClientConn) {
	info := gateway.RelayE2EEInfo{}
	if reporter, ok := client.(interface{ RelayE2EEInfo() gateway.RelayE2EEInfo }); ok {
		info = reporter.RelayE2EEInfo()
	}
	h.called <- info
	h.cancel()
	_ = client.Close()
}

func (h *rotateThenStopHandler) ServeClientConn(_ context.Context, client gateway.ClientConn) {
	if h.calls.Add(1) == 1 {
		rotator, ok := client.(interface{ RotateRelaySession() error })
		if !ok {
			panic("relay client does not support session rotation")
		}
		_ = rotator.RotateRelaySession()
		return
	}
	h.cancel()
	_ = client.Close()
}

func startRelayRegisterProductionE2EEStub(
	t *testing.T,
	ctx context.Context,
	registrations chan<- relay.AgentRegisterFrame,
	pairingEvents <-chan PairingReadyEvent,
	clientEphemeral *e2ee.EphemeralKeyPair,
) string {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/relay/agent" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade production e2ee relay stub: %v", err)
			return
		}
		go func() {
			defer conn.Close()
			var frame relay.AgentRegisterFrame
			if err := conn.ReadJSON(&frame); err != nil {
				return
			}
			registrations <- frame
			_ = conn.WriteJSON(relay.AgentRegisteredFrame{
				Type: relay.TypeAgentRegistered, Version: relay.Version, SessionID: frame.SessionID,
			})
			pairing := readPairingEvent(t, pairingEvents)
			_ = conn.WriteJSON(relay.ClientAttachedFrame{
				Type: relay.TypeClientAttached, Version: relay.Version,
				SessionID: frame.SessionID, ClientID: "rc_test",
			})
			driveGatewayPairingE2EEHandshake(t, conn, clientEphemeral, pairing.SessionID, "rc_test", "hs_pairing", pairing.PairingSecret)
			<-ctx.Done()
		}()
	}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func startRelayRegisterStub(t *testing.T, ctx context.Context, registrations chan<- relay.AgentRegisterFrame) string {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/relay/agent" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade relay register stub: %v", err)
			return
		}
		go func() {
			defer conn.Close()
			var frame relay.AgentRegisterFrame
			if err := conn.ReadJSON(&frame); err != nil {
				return
			}
			registrations <- frame
			_ = conn.WriteJSON(relay.AgentRegisteredFrame{
				Type: relay.TypeAgentRegistered, Version: relay.Version, SessionID: frame.SessionID,
			})
			_ = conn.WriteJSON(relay.ClientAttachedFrame{
				Type: relay.TypeClientAttached, Version: relay.Version,
				SessionID: frame.SessionID, ClientID: "rc_test",
			})
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				var raw map[string]any
				if err := conn.ReadJSON(&raw); err != nil {
					return
				}
			}
		}()
	}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func startRelayReconnectStub(t *testing.T, ctx context.Context, reconnects chan<- relay.AgentReconnectFrame) string {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/relay/agent" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade relay reconnect stub: %v", err)
			return
		}
		go func() {
			defer conn.Close()
			var frame relay.AgentReconnectFrame
			if err := conn.ReadJSON(&frame); err != nil {
				return
			}
			reconnects <- frame
			_ = conn.WriteJSON(relay.AgentRegisteredFrame{
				Type: relay.TypeAgentRegistered, Version: relay.Version, SessionID: frame.SessionID,
			})
			_ = conn.WriteJSON(relay.ClientAttachedFrame{
				Type: relay.TypeClientAttached, Version: relay.Version,
				SessionID: frame.SessionID, ClientID: "rc_test",
			})
			<-ctx.Done()
		}()
	}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func startRelayRejectReconnectThenRegisterStub(
	t *testing.T,
	ctx context.Context,
	reconnects chan<- relay.AgentReconnectFrame,
	registrations chan<- relay.AgentRegisterFrame,
) string {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/relay/agent" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade relay mixed stub: %v", err)
			return
		}
		go func() {
			defer conn.Close()
			var raw map[string]any
			if err := conn.ReadJSON(&raw); err != nil {
				return
			}
			if raw["type"] == relay.TypeAgentReconnect {
				var frame relay.AgentReconnectFrame
				encoded, _ := json.Marshal(raw)
				_ = json.Unmarshal(encoded, &frame)
				reconnects <- frame
				_ = conn.WriteJSON(relay.NewErrorFrame(relay.CodeUnauthorized))
				return
			}
			var frame relay.AgentRegisterFrame
			encoded, _ := json.Marshal(raw)
			_ = json.Unmarshal(encoded, &frame)
			registrations <- frame
			_ = conn.WriteJSON(relay.AgentRegisteredFrame{
				Type: relay.TypeAgentRegistered, Version: relay.Version, SessionID: frame.SessionID,
			})
			_ = conn.WriteJSON(relay.ClientAttachedFrame{
				Type: relay.TypeClientAttached, Version: relay.Version,
				SessionID: frame.SessionID, ClientID: "rc_test",
			})
			<-ctx.Done()
		}()
	}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func driveGatewayPairingE2EEHandshake(
	t *testing.T,
	serverConn *websocket.Conn,
	clientEphemeral *e2ee.EphemeralKeyPair,
	sessionID string,
	clientID string,
	handshakeID string,
	pairingSecret string,
) {
	t.Helper()
	capabilities := e2ee.ProductionCapabilities()
	clientHello := relay.E2EEClientHelloFrame{
		Type: relay.TypeClientE2EEHello, Version: relay.Version,
		SessionID: sessionID, ClientID: clientID, HandshakeID: handshakeID,
		Kind: e2ee.HandshakeKindPairing, Capabilities: &capabilities,
		ClientEphemeralPublicKey: e2ee.EncodeFrameBytes(clientEphemeral.PublicKey),
	}
	if err := serverConn.WriteJSON(clientHello); err != nil {
		t.Fatalf("write client hello: %v", err)
	}
	var agentHello relay.E2EEAgentHelloFrame
	if err := serverConn.ReadJSON(&agentHello); err != nil {
		t.Fatalf("read agent hello: %v", err)
	}
	agentMaterial, err := e2ee.ValidateAgentHelloFrame(agentHello)
	if err != nil {
		t.Fatalf("validate agent hello: %v", err)
	}
	input := capabilities.ApplyToHandshake(e2ee.HandshakeInput{
		Kind:                     e2ee.HandshakeKindPairing,
		SessionID:                sessionID,
		ClientID:                 clientID,
		HandshakeID:              handshakeID,
		ClientEphemeralPublicKey: clientEphemeral.PublicKey,
		NodeEphemeralPublicKey:   agentMaterial.NodeEphemeralPublicKey,
		NodeIdentityPublicKey:    agentMaterial.NodeIdentityPublicKey,
	})
	transcript, err := e2ee.HandshakeTranscript(input)
	if err != nil {
		t.Fatal(err)
	}
	proof := relay.E2EEClientProofFrame{
		Type: relay.TypeClientE2EEProof, Version: relay.Version,
		SessionID: sessionID, ClientID: clientID, HandshakeID: handshakeID,
		Kind:         e2ee.HandshakeKindPairing,
		PairingProof: e2ee.EncodeFrameBytes(e2ee.PairingProof(pairingSecret, transcript)),
	}
	if err := serverConn.WriteJSON(proof); err != nil {
		t.Fatalf("write client proof: %v", err)
	}
	var result relay.E2EEAgentResultFrame
	if err := serverConn.ReadJSON(&result); err != nil {
		t.Fatalf("read agent result: %v", err)
	}
	if !result.OK {
		t.Fatalf("unexpected handshake failure: %#v", result)
	}
}

func readPairingEvent(t *testing.T, events <-chan PairingReadyEvent) PairingReadyEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pairing event")
	}
	return PairingReadyEvent{}
}

func readAgentRegister(t *testing.T, registrations <-chan relay.AgentRegisterFrame) relay.AgentRegisterFrame {
	t.Helper()
	select {
	case frame := <-registrations:
		return frame
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for agent registration")
	}
	return relay.AgentRegisterFrame{}
}

func readAgentReconnect(t *testing.T, reconnects <-chan relay.AgentReconnectFrame) relay.AgentReconnectFrame {
	t.Helper()
	select {
	case frame := <-reconnects:
		return frame
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for agent reconnect")
	}
	return relay.AgentReconnectFrame{}
}

func readRelayE2EEInfo(t *testing.T, called <-chan gateway.RelayE2EEInfo) gateway.RelayE2EEInfo {
	t.Helper()
	select {
	case info := <-called:
		return info
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for production e2ee handler")
	}
	return gateway.RelayE2EEInfo{}
}
