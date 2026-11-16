package relay

import (
	"testing"
	"time"
)

func TestRelayPairingCreatesDevice(t *testing.T) {
	relayServer, server := newInspectableRelayServer(t, Config{})
	defer server.Close()

	sessionID := "rs_device_pair"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgent(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute))

	client := dialRelay(t, server.URL, "/relay/client")
	defer client.Close()
	clientID := pairClient(t, client, sessionID, secret)

	devices := relayServer.Devices(sessionID)
	if len(devices) != 1 {
		t.Fatalf("device count: got %d want 1", len(devices))
	}
	if devices[0].ClientID != clientID || !devices[0].Connected || devices[0].Revoked {
		t.Fatalf("unexpected device info: %#v", devices[0])
	}
}

func TestRelayClientReconnectUpdatesDeviceLastSeen(t *testing.T) {
	relayServer, server := newInspectableRelayServer(t, Config{})
	defer server.Close()

	sessionID := "rs_device_reconnect"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgent(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute))

	client := dialRelay(t, server.URL, "/relay/client")
	clientID, reconnectSecret := pairClientWithReconnectSecret(t, client, sessionID, secret)
	readAttached(t, agent, clientID)
	before := onlyDevice(t, relayServer.Devices(sessionID)).LastSeenAt
	_ = client.Close()
	time.Sleep(time.Millisecond)

	reconnected := dialRelay(t, server.URL, "/relay/client")
	defer reconnected.Close()
	reconnectClient(t, reconnected, sessionID, clientID, reconnectSecret)
	after := onlyDevice(t, relayServer.Devices(sessionID))

	if after.ClientID != clientID || !after.Connected || !after.LastSeenAt.After(before) {
		t.Fatalf("unexpected reconnected device: before=%s after=%#v", before, after)
	}
}

func TestRelayStatePersistsClientReconnectAcrossRelayRestart(t *testing.T) {
	statePath := t.TempDir() + "/relay-state.json"
	sessionID := "rs_persisted_device"
	secret := "pair-secret-128-bit-minimum"
	agentReconnectSecret := "reconnect-secret-128-bit-minimum"

	firstRelay, firstServer := newInspectableRelayServer(t, Config{StatePath: statePath})
	agent := dialRelay(t, firstServer.URL, "/relay/agent")
	registerAgent(t, agent, sessionID, secret, agentReconnectSecret, time.Now().Add(time.Minute))
	client := dialRelay(t, firstServer.URL, "/relay/client")
	clientID, clientReconnectSecret := pairClientWithReconnectSecret(t, client, sessionID, secret)
	readAttached(t, agent, clientID)
	_ = client.Close()
	_ = agent.Close()
	waitForRelaySessionDisconnected(t, firstRelay, sessionID)
	firstServer.Close()
	if got := onlyDevice(t, firstRelay.Devices(sessionID)); got.ClientID != clientID {
		t.Fatalf("unexpected first relay device: %#v", got)
	}

	secondRelay, secondServer := newInspectableRelayServer(t, Config{StatePath: statePath})
	reconnectedAgent := dialRelay(t, secondServer.URL, "/relay/agent")
	reconnectAgent(t, reconnectedAgent, sessionID, agentReconnectSecret)

	reconnectedClient := dialRelay(t, secondServer.URL, "/relay/client")
	reconnectClient(t, reconnectedClient, sessionID, clientID, clientReconnectSecret)
	readAttached(t, reconnectedAgent, clientID)
	_ = reconnectedClient.Close()
	_ = reconnectedAgent.Close()
	waitForRelaySessionDisconnected(t, secondRelay, sessionID)
	secondServer.Close()
}

func TestRelayStatePersistsUnusedPairingAcrossRelayRestart(t *testing.T) {
	statePath := t.TempDir() + "/relay-state.json"
	sessionID := "rs_persisted_pairing"
	secret := "pair-secret-128-bit-minimum"
	agentReconnectSecret := "reconnect-secret-128-bit-minimum"

	firstRelay, firstServer := newInspectableRelayServer(t, Config{StatePath: statePath})
	agent := dialRelay(t, firstServer.URL, "/relay/agent")
	registerAgent(t, agent, sessionID, secret, agentReconnectSecret, time.Now().Add(time.Minute))
	_ = agent.Close()
	waitForRelaySessionDisconnected(t, firstRelay, sessionID)
	firstServer.Close()

	secondRelay, secondServer := newInspectableRelayServer(t, Config{StatePath: statePath})
	reconnectedAgent := dialRelay(t, secondServer.URL, "/relay/agent")
	reconnectAgent(t, reconnectedAgent, sessionID, agentReconnectSecret)

	client := dialRelay(t, secondServer.URL, "/relay/client")
	clientID := pairClient(t, client, sessionID, secret)
	readAttached(t, reconnectedAgent, clientID)
	_ = client.Close()
	_ = reconnectedAgent.Close()
	waitForRelaySessionDisconnected(t, secondRelay, sessionID)
	secondServer.Close()
}

func TestRelayRevokedDeviceCannotReconnect(t *testing.T) {
	relayServer, server := newInspectableRelayServer(t, Config{})
	defer server.Close()

	sessionID := "rs_device_revoke"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgent(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute))

	client := dialRelay(t, server.URL, "/relay/client")
	clientID, reconnectSecret := pairClientWithReconnectSecret(t, client, sessionID, secret)
	readAttached(t, agent, clientID)

	if !relayServer.RevokeDevice(sessionID, clientID) {
		t.Fatal("expected device revoke to succeed")
	}
	devices := relayServer.Devices(sessionID)
	if got := onlyDevice(t, devices); !got.Revoked || got.Connected {
		t.Fatalf("unexpected revoked device: %#v", got)
	}

	reconnected := dialRelay(t, server.URL, "/relay/client")
	defer reconnected.Close()
	writeReconnectClient(t, reconnected, sessionID, clientID, reconnectSecret)
	readRelayError(t, reconnected, CodeDeviceRevoked)
}

func TestRelayRotateSessionCredentialsRevokesDevices(t *testing.T) {
	relayServer, server := newInspectableRelayServer(t, Config{})
	defer server.Close()

	sessionID := "rs_device_rotate"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgent(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute))

	client := dialRelay(t, server.URL, "/relay/client")
	clientID, reconnectSecret := pairClientWithReconnectSecret(t, client, sessionID, secret)
	readAttached(t, agent, clientID)

	if !relayServer.RotateSessionCredentials(sessionID) {
		t.Fatal("expected credential rotation to succeed")
	}
	devices := relayServer.Devices(sessionID)
	if len(devices) != 0 {
		t.Fatalf("rotation should clear runtime devices: %#v", devices)
	}

	reconnected := dialRelay(t, server.URL, "/relay/client")
	defer reconnected.Close()
	writeReconnectClient(t, reconnected, sessionID, clientID, reconnectSecret)
	readRelayError(t, reconnected, CodeDeviceUnknown)
}

func TestRelayClientReconnectWrongSecretReturnsDeviceUnknown(t *testing.T) {
	relayServer, server := newInspectableRelayServer(t, Config{})
	defer server.Close()

	sessionID := "rs_device_wrong_secret"
	secret := "pair-secret-128-bit-minimum"
	agent := dialRelay(t, server.URL, "/relay/agent")
	defer agent.Close()
	registerAgent(t, agent, sessionID, secret, "reconnect-secret", time.Now().Add(time.Minute))

	client := dialRelay(t, server.URL, "/relay/client")
	clientID, _ := pairClientWithReconnectSecret(t, client, sessionID, secret)
	readAttached(t, agent, clientID)
	if got := onlyDevice(t, relayServer.Devices(sessionID)); got.ClientID != clientID {
		t.Fatalf("unexpected device: %#v", got)
	}

	reconnected := dialRelay(t, server.URL, "/relay/client")
	defer reconnected.Close()
	writeReconnectClient(t, reconnected, sessionID, clientID, "wrong-secret")
	readRelayError(t, reconnected, CodeDeviceUnknown)
}

func reconnectClient(t *testing.T, conn interface {
	WriteJSON(any) error
	ReadJSON(any) error
}, sessionID string, clientID string, secret string) {
	t.Helper()
	writeReconnectClient(t, conn, sessionID, clientID, secret)
	var paired ClientPairedFrame
	if err := conn.ReadJSON(&paired); err != nil {
		t.Fatalf("read client reconnect response: %v", err)
	}
	if paired.Type != TypeClientPaired || paired.ClientID != clientID {
		t.Fatalf("unexpected client reconnect response: %#v", paired)
	}
}

func reconnectAgent(t *testing.T, conn interface {
	WriteJSON(any) error
	ReadJSON(any) error
}, sessionID string, secret string) {
	t.Helper()
	if err := conn.WriteJSON(AgentReconnectFrame{
		Type:                 TypeAgentReconnect,
		Version:              Version,
		SessionID:            sessionID,
		AgentReconnectSecret: secret,
	}); err != nil {
		t.Fatalf("write agent reconnect: %v", err)
	}
	var registered AgentRegisteredFrame
	if err := conn.ReadJSON(&registered); err != nil {
		t.Fatalf("read agent reconnect response: %v", err)
	}
	if registered.Type != TypeAgentRegistered || registered.SessionID != sessionID {
		t.Fatalf("unexpected agent reconnect response: %#v", registered)
	}
}

func writeReconnectClient(t *testing.T, conn interface{ WriteJSON(any) error }, sessionID string, clientID string, secret string) {
	t.Helper()
	if err := conn.WriteJSON(ClientReconnectFrame{
		Type:                  TypeClientReconnect,
		Version:               Version,
		SessionID:             sessionID,
		ClientID:              clientID,
		ClientReconnectSecret: secret,
	}); err != nil {
		t.Fatalf("write client reconnect: %v", err)
	}
}

func readPairingRejected(t *testing.T, conn interface{ ReadJSON(any) error }) {
	t.Helper()
	readRelayError(t, conn, CodePairingRejected)
}

func readRelayError(t *testing.T, conn interface{ ReadJSON(any) error }, wantCode string) {
	t.Helper()
	var errFrame ErrorFrame
	if err := conn.ReadJSON(&errFrame); err != nil {
		t.Fatalf("read relay error: %v", err)
	}
	if errFrame.Code != wantCode {
		t.Fatalf("unexpected error frame: %#v", errFrame)
	}
}

func onlyDevice(t *testing.T, devices []DeviceInfo) DeviceInfo {
	t.Helper()
	if len(devices) != 1 {
		t.Fatalf("device count: got %d want 1", len(devices))
	}
	return devices[0]
}

func waitForRelaySessionDisconnected(t *testing.T, server *Server, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		server.mu.Lock()
		state := server.sessions[sessionID]
		disconnected := state == nil || (state.agent == nil && state.client == nil)
		server.mu.Unlock()
		if disconnected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("relay session %s did not disconnect before timeout", sessionID)
}
