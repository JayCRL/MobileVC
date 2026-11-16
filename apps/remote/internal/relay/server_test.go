package relay

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"mobilevc/internal/relay/e2ee"
)

func TestRelayPairingAndOpaqueForwarding(t *testing.T) {
	server := newTestRelayServer(t)
	defer server.Close()

	sessionID := "rs_test"
	pairingSecret := "pair-secret-128-bit-minimum"
	reconnectSecret := "agent-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgent(t, agent, sessionID, pairingSecret, reconnectSecret, time.Now().Add(time.Minute))

	client := dialRelay(t, server.URL, "/relay/client")
	defer client.Close()
	if err := client.WriteJSON(ClientPairFrame{
		Type: TypeClientPair, Version: Version, SessionID: sessionID, PairingSecret: pairingSecret,
	}); err != nil {
		t.Fatalf("pair client: %v", err)
	}
	var paired ClientPairedFrame
	if err := client.ReadJSON(&paired); err != nil {
		t.Fatalf("read paired: %v", err)
	}
	if paired.ClientReconnectSecret == "" {
		t.Fatal("missing client reconnect secret")
	}

	payload := []byte(`{"action":"unknown_business_action","secret":"opaque"}`)
	env := testEnvelope(sessionID, paired.ClientID, DirectionClientToAgent, payload)
	if err := client.WriteJSON(env); err != nil {
		t.Fatalf("forward from client: %v", err)
	}
	readAttached(t, agent, paired.ClientID)
	var got ForwardEnvelope
	if err := agent.ReadJSON(&got); err != nil {
		t.Fatalf("agent read forward: %v", err)
	}
	if got.Payload != env.Payload {
		t.Fatalf("payload changed: got %q want %q", got.Payload, env.Payload)
	}

	agentEnv := testEnvelope(sessionID, paired.ClientID, DirectionAgentToClient, []byte(`{"event":"opaque_agent_event"}`))
	if err := agent.WriteJSON(agentEnv); err != nil {
		t.Fatalf("forward from agent: %v", err)
	}
	var clientGot ForwardEnvelope
	if err := client.ReadJSON(&clientGot); err != nil {
		t.Fatalf("client read forward: %v", err)
	}
	if clientGot.Payload != agentEnv.Payload {
		t.Fatalf("agent payload changed: got %q want %q", clientGot.Payload, agentEnv.Payload)
	}
}

func TestRelayPairingSecretConsumed(t *testing.T) {
	server := newTestRelayServer(t)
	defer server.Close()

	sessionID := "rs_reuse"
	pairingSecret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgent(t, agent, sessionID, pairingSecret, "reconnect-secret", time.Now().Add(time.Minute))

	first := dialRelay(t, server.URL, "/relay/client")
	defer first.Close()
	pairClient(t, first, sessionID, pairingSecret)
	second := dialRelay(t, server.URL, "/relay/client")
	defer second.Close()
	if err := second.WriteJSON(ClientPairFrame{
		Type: TypeClientPair, Version: Version, SessionID: sessionID, PairingSecret: pairingSecret,
	}); err != nil {
		t.Fatalf("pair second client: %v", err)
	}
	var errFrame ErrorFrame
	if err := second.ReadJSON(&errFrame); err != nil {
		t.Fatalf("read second pair error: %v", err)
	}
	if errFrame.Code != CodePairingRejected || errFrame.Message != "pairing rejected" {
		t.Fatalf("unexpected error frame: %#v", errFrame)
	}
	if errFrame.Type != TypeRelayError || errFrame.Version != Version {
		t.Fatalf("unexpected error frame metadata: %#v", errFrame)
	}
}

func TestRelayClientReconnectsWithReconnectSecret(t *testing.T) {
	server := newTestRelayServer(t)
	defer server.Close()

	sessionID := "rs_client_reconnect"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgent(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	clientID, reconnectSecret := pairClientWithReconnectSecret(t, client, sessionID, secret)
	readAttached(t, agent, clientID)
	_ = client.Close()

	reconnected := dialRelay(t, server.URL, "/relay/client")
	defer reconnected.Close()
	if err := reconnected.WriteJSON(ClientReconnectFrame{
		Type: TypeClientReconnect, Version: Version,
		SessionID: sessionID, ClientID: clientID,
		ClientReconnectSecret: reconnectSecret,
	}); err != nil {
		t.Fatalf("client reconnect: %v", err)
	}
	var paired ClientPairedFrame
	if err := reconnected.ReadJSON(&paired); err != nil {
		t.Fatalf("read reconnect paired: %v", err)
	}
	if paired.ClientID != clientID || paired.ClientReconnectSecret != "" {
		t.Fatalf("unexpected reconnect paired frame: %#v", paired)
	}
	readAttached(t, agent, clientID)
}

func TestRelayRejectsClientReconnectWithWrongSecret(t *testing.T) {
	server := newTestRelayServer(t)
	defer server.Close()

	sessionID := "rs_bad_client_reconnect"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgent(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	defer client.Close()
	clientID := pairClient(t, client, sessionID, secret)
	readAttached(t, agent, clientID)

	bad := dialRelay(t, server.URL, "/relay/client")
	defer bad.Close()
	if err := bad.WriteJSON(ClientReconnectFrame{
		Type: TypeClientReconnect, Version: Version,
		SessionID: sessionID, ClientID: clientID,
		ClientReconnectSecret: "wrong-secret",
	}); err != nil {
		t.Fatalf("bad client reconnect: %v", err)
	}
	var errFrame ErrorFrame
	if err := bad.ReadJSON(&errFrame); err != nil {
		t.Fatalf("read reconnect error: %v", err)
	}
	if errFrame.Code != CodeDeviceUnknown {
		t.Fatalf("unexpected reconnect error: %#v", errFrame)
	}
}

func TestRelayRejectsForwardWithWrongClientID(t *testing.T) {
	server := newTestRelayServer(t)
	defer server.Close()

	sessionID := "rs_wrong_client"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgent(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	defer client.Close()
	pairClient(t, client, sessionID, secret)

	env := testEnvelope(sessionID, "rc_wrong", DirectionClientToAgent, []byte(`{"action":"x"}`))
	if err := client.WriteJSON(env); err != nil {
		t.Fatalf("send wrong client id forward: %v", err)
	}
	var errFrame ErrorFrame
	if err := client.ReadJSON(&errFrame); err != nil {
		t.Fatalf("read wrong client id error: %v", err)
	}
	if errFrame.Code != CodeProtocolError {
		t.Fatalf("unexpected error frame: %#v", errFrame)
	}
}

func TestRelayRejectsPlaintextForwardWhenE2EERequired(t *testing.T) {
	server := newLimitedTestRelayServer(t, Config{RequireE2EE: true})
	defer server.Close()

	sessionID := "rs_e2ee_required"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgentWithCapabilities(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute), productionAgentCapabilities())
	client := dialRelay(t, server.URL, "/relay/client")
	defer client.Close()
	clientID := pairClient(t, client, sessionID, secret)
	readAttached(t, agent, clientID)

	env := testEnvelope(sessionID, clientID, DirectionClientToAgent, []byte(`{"action":"x"}`))
	if err := client.WriteJSON(env); err != nil {
		t.Fatalf("send plaintext forward: %v", err)
	}
	var errFrame ErrorFrame
	if err := client.ReadJSON(&errFrame); err != nil {
		t.Fatalf("read e2ee required error: %v", err)
	}
	if errFrame.Code != CodeE2EERequired {
		t.Fatalf("unexpected error frame: %#v", errFrame)
	}
}

func TestRelayForwardsE2EEFrameWhenE2EERequired(t *testing.T) {
	server := newLimitedTestRelayServer(t, Config{RequireE2EE: true})
	defer server.Close()

	sessionID := "rs_e2ee_forward"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgentWithCapabilities(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute), productionAgentCapabilities())
	client := dialRelay(t, server.URL, "/relay/client")
	defer client.Close()
	clientID := pairClient(t, client, sessionID, secret)
	readAttached(t, agent, clientID)

	env := testEnvelope(sessionID, clientID, DirectionClientToAgent, []byte(`sealed`))
	env.Encryption = EncryptionE2EEV1
	env.StreamID = 9
	env.HandshakeID = "hs_required"
	if err := client.WriteJSON(env); err != nil {
		t.Fatalf("send e2ee forward: %v", err)
	}
	var got ForwardEnvelope
	if err := agent.ReadJSON(&got); err != nil {
		t.Fatalf("agent read e2ee forward: %v", err)
	}
	if got.Encryption != EncryptionE2EEV1 || got.StreamID != 9 || got.HandshakeID != "hs_required" {
		t.Fatalf("unexpected e2ee forward metadata: %#v", got)
	}
}

func TestRelayForwardsE2EEHandshakeControlFrames(t *testing.T) {
	server := newLimitedTestRelayServer(t, Config{RequireE2EE: true})
	defer server.Close()

	sessionID := "rs_e2ee_handshake"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgentWithCapabilities(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute), productionAgentCapabilities())
	client := dialRelay(t, server.URL, "/relay/client")
	defer client.Close()
	clientID := pairClient(t, client, sessionID, secret)
	readAttached(t, agent, clientID)

	clientEphemeral, err := e2ee.NewEphemeralKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	capabilities := e2ee.ProductionCapabilities()
	clientHello := E2EEClientHelloFrame{
		Type: TypeClientE2EEHello, Version: Version,
		SessionID: sessionID, ClientID: clientID, HandshakeID: "hs_control_1",
		Kind: e2ee.HandshakeKindPairing, Capabilities: &capabilities,
		ClientEphemeralPublicKey: e2ee.EncodeFrameBytes(clientEphemeral.PublicKey),
	}
	if err := client.WriteJSON(clientHello); err != nil {
		t.Fatalf("send client e2ee hello: %v", err)
	}
	var gotHello E2EEClientHelloFrame
	if err := agent.ReadJSON(&gotHello); err != nil {
		t.Fatalf("agent read e2ee hello: %v", err)
	}
	if gotHello.Type != TypeClientE2EEHello || gotHello.HandshakeID != "hs_control_1" {
		t.Fatalf("unexpected e2ee hello: %#v", gotHello)
	}

	result := E2EEAgentResultFrame{
		Type: TypeAgentE2EEResult, Version: Version,
		SessionID: sessionID, ClientID: clientID, HandshakeID: "hs_control_1",
		OK: true,
	}
	if err := agent.WriteJSON(result); err != nil {
		t.Fatalf("send agent e2ee result: %v", err)
	}
	var gotResult E2EEAgentResultFrame
	if err := client.ReadJSON(&gotResult); err != nil {
		t.Fatalf("client read e2ee result: %v", err)
	}
	if gotResult.Type != TypeAgentE2EEResult || !gotResult.OK {
		t.Fatalf("unexpected e2ee result: %#v", gotResult)
	}
}

func TestRelayRejectsInvalidE2EEHandshakeControlFrame(t *testing.T) {
	server := newLimitedTestRelayServer(t, Config{RequireE2EE: true})
	defer server.Close()

	sessionID := "rs_bad_e2ee_handshake"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgentWithCapabilities(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute), productionAgentCapabilities())
	client := dialRelay(t, server.URL, "/relay/client")
	defer client.Close()
	clientID := pairClient(t, client, sessionID, secret)
	readAttached(t, agent, clientID)

	capabilities := e2ee.ProductionCapabilities()
	badHello := E2EEClientHelloFrame{
		Type: TypeClientE2EEHello, Version: Version,
		SessionID: sessionID, ClientID: clientID, HandshakeID: "hs_bad",
		Kind: e2ee.HandshakeKindPairing, Capabilities: &capabilities,
		ClientEphemeralPublicKey: "not-base64url",
	}
	if err := client.WriteJSON(badHello); err != nil {
		t.Fatalf("send invalid e2ee hello: %v", err)
	}
	var errFrame ErrorFrame
	if err := client.ReadJSON(&errFrame); err != nil {
		t.Fatalf("read invalid e2ee error: %v", err)
	}
	if errFrame.Code != CodeE2EEHandshakeFailed {
		t.Fatalf("unexpected error frame: %#v", errFrame)
	}
}

func TestRelayRejectsPlaintextAgentCapabilitiesWhenE2EERequired(t *testing.T) {
	server := newLimitedTestRelayServer(t, Config{RequireE2EE: true})
	defer server.Close()

	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	if err := agent.WriteJSON(AgentRegisterFrame{
		Type:                     TypeAgentRegister,
		Version:                  Version,
		SessionID:                "rs_bad_capability",
		PairingSecretHash:        SecretHash("pair-secret"),
		AgentReconnectSecretHash: SecretHash("reconnect-secret"),
		PairingExpiresAt:         time.Now().Add(time.Minute).Unix(),
		Capabilities:             testAgentCapabilities(),
	}); err != nil {
		t.Fatalf("register plaintext-capability agent: %v", err)
	}
	var errFrame ErrorFrame
	if err := agent.ReadJSON(&errFrame); err != nil {
		t.Fatalf("read capability rejection: %v", err)
	}
	if errFrame.Code != CodeE2EEUnsupported {
		t.Fatalf("unexpected error frame: %#v", errFrame)
	}
}

func TestRelayRejectsSessionIDReuseAfterAgentDisconnect(t *testing.T) {
	server := newTestRelayServer(t)
	defer server.Close()

	sessionID := "rs_reuse_session"
	first := dialRelay(t, server.URL, "/relay/agent")
	registerAgent(t, first, sessionID, "pair-secret-one", "reconnect-secret-one", time.Now().Add(time.Minute))
	_ = first.Close()
	time.Sleep(20 * time.Millisecond)

	second := dialRelay(t, server.URL, "/relay/agent")
	defer second.Close()
	if err := second.WriteJSON(AgentRegisterFrame{
		Type:                     TypeAgentRegister,
		Version:                  Version,
		SessionID:                sessionID,
		PairingSecretHash:        SecretHash("pair-secret-two"),
		AgentReconnectSecretHash: SecretHash("reconnect-secret-two"),
		PairingExpiresAt:         time.Now().Add(time.Minute).Unix(),
		Capabilities:             testAgentCapabilities(),
	}); err != nil {
		t.Fatalf("register reused session id: %v", err)
	}
	var errFrame ErrorFrame
	if err := second.ReadJSON(&errFrame); err != nil {
		t.Fatalf("read reused session error: %v", err)
	}
	if errFrame.Code != CodeUnauthorized {
		t.Fatalf("unexpected error frame: %#v", errFrame)
	}
}

func TestRelayRejectsOversizedDecodedPayload(t *testing.T) {
	const maxPayloadBytes = 1024
	server := newLimitedTestRelayServer(t, Config{MaxPayloadBytes: maxPayloadBytes})
	defer server.Close()

	sessionID := "rs_large"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgent(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	defer client.Close()
	clientID := pairClient(t, client, sessionID, secret)
	readAttached(t, agent, clientID)

	payload := strings.Repeat("x", maxPayloadBytes+1)
	if err := client.WriteJSON(testEnvelope(sessionID, clientID, DirectionClientToAgent, []byte(payload))); err != nil {
		t.Fatalf("send oversized forward: %v", err)
	}
	var errFrame ErrorFrame
	if err := client.ReadJSON(&errFrame); err != nil {
		t.Fatalf("read oversized error: %v", err)
	}
	if errFrame.Code != CodePayloadTooLarge {
		t.Fatalf("unexpected error frame: %#v", errFrame)
	}
	if errFrame.DecodedBytes != maxPayloadBytes+1 || errFrame.MaxBytes != maxPayloadBytes {
		t.Fatalf("unexpected payload size metadata: %#v", errFrame)
	}

	env := testEnvelope(sessionID, clientID, DirectionClientToAgent, []byte(`{"action":"after_oversized"}`))
	if err := client.WriteJSON(env); err != nil {
		t.Fatalf("send valid forward after oversized payload: %v", err)
	}
	var got ForwardEnvelope
	if err := agent.ReadJSON(&got); err != nil {
		t.Fatalf("agent read valid forward after oversized payload: %v", err)
	}
	if got.Payload != env.Payload {
		t.Fatalf("payload after oversized changed: got %q want %q", got.Payload, env.Payload)
	}
}

func TestRelayRejectsOversizedPostAuthFrameBeforeDecode(t *testing.T) {
	server := newLimitedTestRelayServer(t, Config{MaxPayloadBytes: 32})
	defer server.Close()

	sessionID := "rs_large_frame"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgent(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	defer client.Close()
	clientID := pairClient(t, client, sessionID, secret)
	readAttached(t, agent, clientID)

	oversized := map[string]any{
		"type":      TypeRelayForward,
		"version":   Version,
		"sessionId": sessionID,
		"clientId":  clientID,
		"padding":   strings.Repeat("x", 20*1024),
	}
	if err := client.WriteJSON(oversized); err != nil {
		t.Fatalf("send oversized frame: %v", err)
	}
	var errFrame ErrorFrame
	if err := client.ReadJSON(&errFrame); err != nil {
		t.Fatalf("read oversized frame error: %v", err)
	}
	if errFrame.Code != CodeFrameTooLarge {
		t.Fatalf("unexpected error frame: %#v", errFrame)
	}
}

func TestRelayAcceptsPaddedBase64URLPayload(t *testing.T) {
	server := newTestRelayServer(t)
	defer server.Close()

	sessionID := "rs_padded_payload"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgent(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	defer client.Close()
	clientID := pairClient(t, client, sessionID, secret)
	readAttached(t, agent, clientID)

	env := testEnvelope(sessionID, clientID, DirectionClientToAgent, []byte(`{"action":"ping"}`))
	env.Payload = base64.URLEncoding.EncodeToString([]byte(`{"action":"ping"}`))
	if err := client.WriteJSON(env); err != nil {
		t.Fatalf("send padded forward: %v", err)
	}
	var got ForwardEnvelope
	if err := agent.ReadJSON(&got); err != nil {
		t.Fatalf("agent read padded forward: %v", err)
	}
	if got.Payload != env.Payload {
		t.Fatalf("payload changed: got %q want %q", got.Payload, env.Payload)
	}
}

func TestRelayRejectsInvalidPayloadEncodingAsProtocolError(t *testing.T) {
	server := newTestRelayServer(t)
	defer server.Close()

	sessionID := "rs_invalid_payload"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgent(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	defer client.Close()
	clientID := pairClient(t, client, sessionID, secret)
	readAttached(t, agent, clientID)

	env := testEnvelope(sessionID, clientID, DirectionClientToAgent, []byte(`{"action":"ping"}`))
	env.Payload = "not base64url payload"
	if err := client.WriteJSON(env); err != nil {
		t.Fatalf("send invalid payload forward: %v", err)
	}
	var errFrame ErrorFrame
	if err := client.ReadJSON(&errFrame); err != nil {
		t.Fatalf("read invalid payload error: %v", err)
	}
	if errFrame.Code != CodeProtocolError {
		t.Fatalf("unexpected error frame: %#v", errFrame)
	}
}

func TestRelayAllowsAgentReconnectWithinGrace(t *testing.T) {
	server := newTestRelayServer(t)
	defer server.Close()

	sessionID := "rs_reconnect"
	secret := "pair-secret-128-bit-minimum"
	reconnectSecret := "agent-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	registerAgent(t, agent, sessionID, secret, reconnectSecret, time.Now().Add(time.Minute))
	_ = agent.Close()
	time.Sleep(20 * time.Millisecond)

	reconnected := dialRelay(t, server.URL, "/relay/agent")
	defer reconnected.Close()
	if err := reconnected.WriteJSON(AgentReconnectFrame{
		Type:                 TypeAgentReconnect,
		Version:              Version,
		SessionID:            sessionID,
		AgentReconnectSecret: reconnectSecret,
	}); err != nil {
		t.Fatalf("reconnect agent: %v", err)
	}
	var registered AgentRegisteredFrame
	if err := reconnected.ReadJSON(&registered); err != nil {
		t.Fatalf("read reconnect registered: %v", err)
	}
	if registered.SessionID != sessionID {
		t.Fatalf("reconnected session: got %q want %q", registered.SessionID, sessionID)
	}
}

func TestRelayRejectsPerIPConnectionCapacity(t *testing.T) {
	server := newLimitedTestRelayServer(t, Config{MaxConnsPerIP: 1})
	defer server.Close()

	first := dialRelay(t, server.URL, "/relay/agent")
	defer first.Close()
	_, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/relay/agent", nil)
	if err == nil {
		t.Fatal("expected second connection to be rejected")
	}
}

func TestRelayUsesTrustedForwardedIPForConnectionCapacity(t *testing.T) {
	server := newLimitedTestRelayServer(t, Config{
		MaxConnsPerIP:     1,
		TrustedProxyCIDRs: "127.0.0.1/32",
	})
	defer server.Close()

	first := dialRelayWithHeader(t, server.URL, "/relay/agent", http.Header{
		"X-Forwarded-For": []string{"198.51.100.10"},
	})
	defer first.Close()
	second := dialRelayWithHeader(t, server.URL, "/relay/agent", http.Header{
		"X-Forwarded-For": []string{"198.51.100.11"},
	})
	defer second.Close()
}

func TestRelayIgnoresForwardedIPWithoutTrustedProxy(t *testing.T) {
	server := newLimitedTestRelayServer(t, Config{MaxConnsPerIP: 1})
	defer server.Close()

	first := dialRelayWithHeader(t, server.URL, "/relay/agent", http.Header{
		"X-Forwarded-For": []string{"198.51.100.10"},
	})
	defer first.Close()
	_, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/relay/agent", http.Header{
		"X-Forwarded-For": []string{"198.51.100.11"},
	})
	if err == nil {
		t.Fatal("expected untrusted forwarded IP to be ignored for capacity")
	}
}

func TestValidateRelayURL(t *testing.T) {
	valid := []string{
		"wss://relay.example.com",
		"ws://127.0.0.1:9000",
		"ws://localhost:9000",
		"ws://192.168.1.10:9000",
		"ws://10.0.0.5:9000",
		"ws://172.16.0.5:9000",
	}
	for _, raw := range valid {
		if err := ValidateRelayURL(raw); err != nil {
			t.Fatalf("ValidateRelayURL(%q) failed: %v", raw, err)
		}
	}
	invalid := []string{"http://relay.example.com", "https://relay.example.com", "ws://relay.example.com"}
	for _, raw := range invalid {
		if err := ValidateRelayURL(raw); err == nil {
			t.Fatalf("ValidateRelayURL(%q) should fail", raw)
		}
	}
}
