package relay

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const testShortAgentGrace = 50 * time.Millisecond

func TestRelayExpiresDisconnectedAgentSessionAfterGrace(t *testing.T) {
	server := newLimitedTestRelayServer(t, Config{AgentGracePeriod: testShortAgentGrace})
	defer server.Close()

	sessionID := "rs_expire_agent"
	first := dialRelay(t, server.URL, "/relay/agent")
	registerAgent(t, first, sessionID, "pair-secret-one", "reconnect-secret-one", time.Now().Add(time.Minute))
	_ = first.Close()
	time.Sleep(testShortAgentGrace * 2)

	second := dialRelay(t, server.URL, "/relay/agent")
	defer second.Close()
	registerAgent(t, second, sessionID, "pair-secret-two", "reconnect-secret-two", time.Now().Add(time.Minute))
}

func TestRelayClosesClientWhenDisconnectedAgentExpires(t *testing.T) {
	server := newLimitedTestRelayServer(t, Config{AgentGracePeriod: testShortAgentGrace})
	defer server.Close()

	sessionID := "rs_close_client"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	registerAgent(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	defer client.Close()
	pairClient(t, client, sessionID, secret)

	_ = agent.Close()
	_ = client.SetReadDeadline(time.Now().Add(testShortAgentGrace * 4))
	if err := client.ReadJSON(&ErrorFrame{}); err == nil {
		t.Fatal("expected relay client connection to close")
	}
}

func TestRelayKeepsTrustedDeviceSessionAfterAgentGrace(t *testing.T) {
	relayServer, server := newInspectableRelayServer(t, Config{AgentGracePeriod: testShortAgentGrace})
	defer server.Close()

	sessionID := "rs_keep_bound_device"
	pairingSecret := "pair-secret-128-bit-minimum"
	agentReconnectSecret := "agent-reconnect-secret"
	agent := dialRelay(t, server.URL, "/relay/agent")
	registerAgent(t, agent, sessionID, pairingSecret, agentReconnectSecret, time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	clientID, clientReconnectSecret := pairClientWithReconnectSecret(t, client, sessionID, pairingSecret)
	readAttached(t, agent, clientID)

	_ = client.Close()
	_ = agent.Close()
	time.Sleep(testShortAgentGrace * 2)

	relayServer.mu.Lock()
	state := relayServer.sessions[sessionID]
	if state == nil {
		relayServer.mu.Unlock()
		t.Fatal("expected bound-device session to survive agent grace expiry")
	}
	if state.agent != nil || state.client != nil {
		relayServer.mu.Unlock()
		t.Fatalf("expected disconnected session, got agent=%v client=%v", state.agent != nil, state.client != nil)
	}
	relayServer.mu.Unlock()

	reconnectedAgent := dialRelay(t, server.URL, "/relay/agent")
	defer reconnectedAgent.Close()
	reconnectAgent(t, reconnectedAgent, sessionID, agentReconnectSecret)

	reconnectedClient := dialRelay(t, server.URL, "/relay/client")
	defer reconnectedClient.Close()
	reconnectClient(t, reconnectedClient, sessionID, clientID, clientReconnectSecret)
	readAttached(t, reconnectedAgent, clientID)
}

func TestRelayAgentReconnectTakesOverStaleActiveAgent(t *testing.T) {
	server := newLimitedTestRelayServer(t, Config{AgentGracePeriod: testShortAgentGrace})
	defer server.Close()

	sessionID := "rs_agent_takeover"
	pairingSecret := "pair-secret-128-bit-minimum"
	agentReconnectSecret := "agent-reconnect-secret"
	agent := dialRelay(t, server.URL, "/relay/agent")
	registerAgent(t, agent, sessionID, pairingSecret, agentReconnectSecret, time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	clientID, clientReconnectSecret := pairClientWithReconnectSecret(t, client, sessionID, pairingSecret)
	readAttached(t, agent, clientID)

	reconnectedAgent := dialRelay(t, server.URL, "/relay/agent")
	defer reconnectedAgent.Close()
	reconnectAgent(t, reconnectedAgent, sessionID, agentReconnectSecret)

	_ = agent.SetReadDeadline(time.Now().Add(testShortAgentGrace * 4))
	if err := agent.ReadJSON(&ErrorFrame{}); err == nil {
		t.Fatal("expected old agent connection to close after reconnect takeover")
	}

	reconnectedClient := dialRelay(t, server.URL, "/relay/client")
	defer reconnectedClient.Close()
	reconnectClient(t, reconnectedClient, sessionID, clientID, clientReconnectSecret)
	readAttached(t, reconnectedAgent, clientID)
}

func TestRelayClientReconnectReportsAgentDisconnectedAfterGraceForTrustedDevice(t *testing.T) {
	server := newLimitedTestRelayServer(t, Config{AgentGracePeriod: testShortAgentGrace})
	defer server.Close()

	sessionID := "rs_bound_device_agent_absent"
	pairingSecret := "pair-secret-128-bit-minimum"
	agentReconnectSecret := "agent-reconnect-secret"
	agent := dialRelay(t, server.URL, "/relay/agent")
	registerAgent(t, agent, sessionID, pairingSecret, agentReconnectSecret, time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	clientID, clientReconnectSecret := pairClientWithReconnectSecret(t, client, sessionID, pairingSecret)
	readAttached(t, agent, clientID)

	_ = client.Close()
	_ = agent.Close()
	time.Sleep(testShortAgentGrace * 2)

	reconnectingClient := dialRelay(t, server.URL, "/relay/client")
	defer reconnectingClient.Close()
	writeReconnectClient(t, reconnectingClient, sessionID, clientID, clientReconnectSecret)
	readRelayError(t, reconnectingClient, CodeAgentDisconnected)
}

func TestRelayClientReconnectWithWrongSecretAfterAgentGraceStaysUnknown(t *testing.T) {
	server := newLimitedTestRelayServer(t, Config{AgentGracePeriod: testShortAgentGrace})
	defer server.Close()

	sessionID := "rs_bound_device_wrong_secret"
	pairingSecret := "pair-secret-128-bit-minimum"
	agentReconnectSecret := "agent-reconnect-secret"
	agent := dialRelay(t, server.URL, "/relay/agent")
	registerAgent(t, agent, sessionID, pairingSecret, agentReconnectSecret, time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	clientID, _ := pairClientWithReconnectSecret(t, client, sessionID, pairingSecret)
	readAttached(t, agent, clientID)

	_ = client.Close()
	_ = agent.Close()
	time.Sleep(testShortAgentGrace * 2)

	reconnectingClient := dialRelay(t, server.URL, "/relay/client")
	defer reconnectingClient.Close()
	writeReconnectClient(t, reconnectingClient, sessionID, clientID, "wrong-secret")
	readRelayError(t, reconnectingClient, CodeDeviceUnknown)
}

func TestRelayNotifiesAgentWhenClientAttaches(t *testing.T) {
	server := newTestRelayServer(t)
	defer server.Close()

	sessionID := "rs_attached"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgent(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	defer client.Close()
	clientID := pairClient(t, client, sessionID, secret)

	var attached ClientAttachedFrame
	if err := agent.ReadJSON(&attached); err != nil {
		t.Fatalf("read client attached: %v", err)
	}
	if attached.SessionID != sessionID || attached.ClientID != clientID {
		t.Fatalf("unexpected client attached frame: %#v", attached)
	}
}

func TestRelayAgentReconnectClosesStaleClient(t *testing.T) {
	server := newTestRelayServer(t)
	defer server.Close()

	sessionID := "rs_reconnect_attached"
	secret := "pair-secret-128-bit-minimum"
	reconnectSecret := "reconnect-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	registerAgent(t, agent, sessionID, secret, reconnectSecret, time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	defer client.Close()
	clientID, clientReconnectSecret := pairClientWithReconnectSecret(t, client, sessionID, secret)
	readAttached(t, agent, clientID)
	_ = agent.Close()

	reconnected := reconnectAgentEventually(t, server.URL, sessionID, reconnectSecret)
	defer reconnected.Close()

	assertRelayClientClosed(t, client)
	reconnectedClient := dialRelay(t, server.URL, "/relay/client")
	defer reconnectedClient.Close()
	reconnectClient(t, reconnectedClient, sessionID, clientID, clientReconnectSecret)
	readAttached(t, reconnected, clientID)
}

func TestRelayReportsAgentDisconnectedDuringGrace(t *testing.T) {
	relayServer, server := newInspectableRelayServer(t, Config{AgentGracePeriod: testShortAgentGrace})
	defer server.Close()

	sessionID := "rs_agent_grace"
	secret := "pair-secret-128-bit-minimum"
	reconnectSecret := "agent-reconnect-secret"
	agent := dialRelay(t, server.URL, "/relay/agent")
	registerAgent(t, agent, sessionID, secret, reconnectSecret, time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	defer client.Close()
	clientID := pairClient(t, client, sessionID, secret)
	readAttached(t, agent, clientID)
	_ = agent.Close()
	waitForAgentOnlyDisconnected(t, relayServer, sessionID)

	env := testEnvelope(sessionID, clientID, DirectionClientToAgent, []byte(`{"action":"during_agent_reconnect"}`))
	if err := client.WriteJSON(env); err != nil {
		t.Fatalf("write client forward while agent reconnects: %v", err)
	}
	readRelayError(t, client, CodeAgentDisconnected)
}

func TestRelayClientReconnectReportsAgentDisconnectedDuringGrace(t *testing.T) {
	relayServer, server := newInspectableRelayServer(t, Config{AgentGracePeriod: testShortAgentGrace})
	defer server.Close()

	sessionID := "rs_client_agent_grace"
	secret := "pair-secret-128-bit-minimum"
	reconnectSecret := "agent-reconnect-secret"
	agent := dialRelay(t, server.URL, "/relay/agent")
	registerAgent(t, agent, sessionID, secret, reconnectSecret, time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	clientID, clientReconnectSecret := pairClientWithReconnectSecret(t, client, sessionID, secret)
	readAttached(t, agent, clientID)
	_ = client.Close()
	_ = agent.Close()
	waitForAgentOnlyDisconnected(t, relayServer, sessionID)

	reconnectingClient := dialRelay(t, server.URL, "/relay/client")
	defer reconnectingClient.Close()
	if err := reconnectingClient.WriteJSON(ClientReconnectFrame{
		Type: TypeClientReconnect, Version: Version,
		SessionID: sessionID, ClientID: clientID,
		ClientReconnectSecret: clientReconnectSecret,
	}); err != nil {
		t.Fatalf("write client reconnect while agent reconnects: %v", err)
	}
	readRelayError(t, reconnectingClient, CodeAgentDisconnected)
}

func TestRelayAllowsFirstAgentForwardWithEmptyClientID(t *testing.T) {
	server := newTestRelayServer(t)
	defer server.Close()

	sessionID := "rs_empty_client"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgent(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	defer client.Close()
	clientID := pairClient(t, client, sessionID, secret)
	readAttached(t, agent, clientID)

	env := testEnvelope(sessionID, "", DirectionAgentToClient, []byte(`{"event":"initial"}`))
	if err := agent.WriteJSON(env); err != nil {
		t.Fatalf("write empty-client agent forward: %v", err)
	}
	var got ForwardEnvelope
	if err := client.ReadJSON(&got); err != nil {
		t.Fatalf("read normalized agent forward: %v", err)
	}
	if got.ClientID != clientID || got.Payload != env.Payload {
		t.Fatalf("unexpected normalized forward: %#v", got)
	}
}

func TestRelayKeepsAgentAfterClientDeliveryFailure(t *testing.T) {
	server := newTestRelayServer(t)
	defer server.Close()

	sessionID := "rs_delivery_failure"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgent(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	clientID, clientReconnectSecret := pairClientWithReconnectSecret(t, client, sessionID, secret)
	readAttached(t, agent, clientID)
	_ = client.Close()
	time.Sleep(20 * time.Millisecond)

	missed := testEnvelope(sessionID, clientID, DirectionAgentToClient, []byte(`{"event":"missed"}`))
	if err := agent.WriteJSON(missed); err != nil {
		t.Fatalf("write agent forward without client: %v", err)
	}
	var errFrame ErrorFrame
	if err := agent.ReadJSON(&errFrame); err != nil {
		t.Fatalf("read target unavailable error: %v", err)
	}
	if errFrame.Code != CodeTargetUnavailable {
		t.Fatalf("unexpected delivery error: %#v", errFrame)
	}

	reconnected := dialRelay(t, server.URL, "/relay/client")
	defer reconnected.Close()
	if err := reconnected.WriteJSON(ClientReconnectFrame{
		Type: TypeClientReconnect, Version: Version,
		SessionID: sessionID, ClientID: clientID,
		ClientReconnectSecret: clientReconnectSecret,
	}); err != nil {
		t.Fatalf("client reconnect: %v", err)
	}
	var paired ClientPairedFrame
	if err := reconnected.ReadJSON(&paired); err != nil {
		t.Fatalf("read reconnect paired: %v", err)
	}
	readAttached(t, agent, clientID)

	delivered := testEnvelope(sessionID, clientID, DirectionAgentToClient, []byte(`{"event":"delivered"}`))
	if err := agent.WriteJSON(delivered); err != nil {
		t.Fatalf("write agent forward after delivery error: %v", err)
	}
	var got ForwardEnvelope
	if err := reconnected.ReadJSON(&got); err != nil {
		t.Fatalf("read forwarded payload after reconnect: %v", err)
	}
	if got.Payload != delivered.Payload {
		t.Fatalf("unexpected forwarded payload: %#v", got)
	}
}

func TestRelayAcceptsPongControlFramePostAuth(t *testing.T) {
	server := newTestRelayServer(t)
	defer server.Close()

	sessionID := "rs_pong"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgent(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute))
	client := dialRelay(t, server.URL, "/relay/client")
	defer client.Close()
	clientID := pairClient(t, client, sessionID, secret)
	readAttached(t, agent, clientID)

	if err := agent.WriteJSON(ControlFrame{Type: TypeRelayPong, Version: Version}); err != nil {
		t.Fatalf("write relay pong: %v", err)
	}
	env := testEnvelope(sessionID, clientID, DirectionAgentToClient, []byte(`{"event":"after-pong"}`))
	if err := agent.WriteJSON(env); err != nil {
		t.Fatalf("write forward after pong: %v", err)
	}
	var got ForwardEnvelope
	if err := client.ReadJSON(&got); err != nil {
		t.Fatalf("read forward after pong: %v", err)
	}
	if got.Payload != env.Payload {
		t.Fatalf("unexpected forwarded payload: %#v", got)
	}
}

func readAttached(t *testing.T, conn *websocket.Conn, clientID string) {
	t.Helper()
	var attached ClientAttachedFrame
	if err := conn.ReadJSON(&attached); err != nil {
		t.Fatalf("read client attached: %v", err)
	}
	if attached.ClientID != clientID {
		t.Fatalf("attached client id: got %q want %q", attached.ClientID, clientID)
	}
}

func assertRelayClientClosed(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var frame map[string]any
	if err := conn.ReadJSON(&frame); err == nil {
		t.Fatalf("expected stale relay client to close, got frame: %#v", frame)
	}
}

func waitForAgentOnlyDisconnected(t *testing.T, server *Server, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		server.mu.Lock()
		state := server.sessions[sessionID]
		disconnected := state != nil && state.agent == nil
		server.mu.Unlock()
		if disconnected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("agent disconnect was not observed before timeout")
}

func reconnectAgentEventually(t *testing.T, baseURL string, sessionID string, secret string) *websocket.Conn {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		conn := dialRelay(t, baseURL, "/relay/agent")
		if tryReconnectAgent(t, conn, sessionID, secret) {
			return conn
		}
		_ = conn.Close()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("agent reconnect did not succeed before timeout")
	return nil
}

func tryReconnectAgent(t *testing.T, conn *websocket.Conn, sessionID string, secret string) bool {
	t.Helper()
	if err := conn.WriteJSON(AgentReconnectFrame{
		Type: TypeAgentReconnect, Version: Version,
		SessionID: sessionID, AgentReconnectSecret: secret,
	}); err != nil {
		t.Fatalf("write reconnect frame: %v", err)
	}
	var registered AgentRegisteredFrame
	if err := conn.ReadJSON(&registered); err != nil {
		t.Fatalf("read reconnect response: %v", err)
	}
	return registered.Type == TypeAgentRegistered && registered.SessionID == sessionID
}
