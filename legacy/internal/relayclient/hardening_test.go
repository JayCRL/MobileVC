package relayclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"mobilevc/internal/relay"
	"mobilevc/internal/relay/e2ee"
)

func TestGatewayConnConsumesClientAttachedBeforeForward(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	gateway := newGatewayConn(clientConn, "rs_gateway")

	go func() {
		_ = serverConn.WriteJSON(relay.ClientAttachedFrame{
			Type: relay.TypeClientAttached, Version: relay.Version,
			SessionID: "rs_gateway", ClientID: "rc_attached",
		})
		_ = serverConn.WriteJSON(testRelayForward("rc_attached"))
	}()

	var payload map[string]any
	if err := gateway.ReadJSON(&payload); err != nil {
		t.Fatalf("read gateway payload: %v", err)
	}
	if gateway.RemoteAddr() != "relay:rs_gateway/rc_attached" {
		t.Fatalf("remote addr did not include client id: %s", gateway.RemoteAddr())
	}
}

func TestGatewayConnAcceptsPaddedBase64URLPayload(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	gateway := newGatewayConn(clientConn, "rs_gateway")
	t.Cleanup(func() { _ = gateway.Close() })

	env := testRelayForward("rc_attached")
	env.Payload = base64.URLEncoding.EncodeToString([]byte(`{"action":"ping"}`))
	go func() {
		_ = serverConn.WriteJSON(relay.ClientAttachedFrame{
			Type: relay.TypeClientAttached, Version: relay.Version,
			SessionID: "rs_gateway", ClientID: "rc_attached",
		})
		_ = serverConn.WriteJSON(env)
	}()

	var payload map[string]any
	if err := gateway.ReadJSON(&payload); err != nil {
		t.Fatalf("read padded gateway payload: %v", err)
	}
	if payload["action"] != "ping" {
		t.Fatalf("unexpected padded payload: %#v", payload)
	}
}

func TestGatewayConnWritesCurrentClientID(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	gateway := newGatewayConn(clientConn, "rs_gateway")

	go func() {
		_ = serverConn.WriteJSON(relay.ClientAttachedFrame{
			Type: relay.TypeClientAttached, Version: relay.Version,
			SessionID: "rs_gateway", ClientID: "rc_attached",
		})
		_ = serverConn.WriteJSON(testRelayForward("rc_attached"))
	}()
	var payload map[string]any
	if err := gateway.ReadJSON(&payload); err != nil {
		t.Fatalf("read attached gateway payload: %v", err)
	}
	if err := gateway.WriteJSON(map[string]string{"event": "ready"}); err != nil {
		t.Fatalf("write gateway payload: %v", err)
	}
	var env relay.ForwardEnvelope
	if err := serverConn.ReadJSON(&env); err != nil {
		t.Fatalf("read written forward: %v", err)
	}
	if env.ClientID != "rc_attached" {
		t.Fatalf("written client id: got %q", env.ClientID)
	}
}

func TestGatewayConnProductionWriteSetsWriteDeadline(t *testing.T) {
	serverConn, clientConn, trackedConn := newTrackedRelayClientTestConns(t)
	defer serverConn.Close()
	t.Cleanup(func() { _ = clientConn.Close() })
	trackedConn.resetWriteDeadlines()

	attachCh := make(chan struct{})
	close(attachCh)
	gateway := &gatewayConn{
		conn:      clientConn,
		sessionID: "rs_gateway",
		clientID:  "rc_attached",
		attachCh:  attachCh,
		readDone:  make(chan struct{}),
		closeCh:   make(chan struct{}),
	}

	if err := gateway.WriteJSON(map[string]string{"event": "ready"}); err != nil {
		t.Fatalf("write gateway event: %v", err)
	}
	if !trackedConn.sawNonZeroWriteDeadline() {
		t.Fatal("runtime relay write did not set a non-zero write deadline")
	}
	var outbound relay.ForwardEnvelope
	if err := serverConn.ReadJSON(&outbound); err != nil {
		t.Fatalf("read outbound frame: %v", err)
	}
	if outbound.ClientID != "rc_attached" {
		t.Fatalf("unexpected outbound client id: %#v", outbound)
	}
}

func TestGatewayConnRespondsToRelayPing(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	gateway := newGatewayConn(clientConn, "rs_gateway")
	t.Cleanup(func() { _ = gateway.Close() })

	if err := serverConn.WriteJSON(relay.ControlFrame{Type: relay.TypeRelayPing, Version: relay.Version}); err != nil {
		t.Fatalf("write relay ping: %v", err)
	}
	var pong relay.ControlFrame
	if err := serverConn.ReadJSON(&pong); err != nil {
		t.Fatalf("read relay pong: %v", err)
	}
	if pong.Type != relay.TypeRelayPong || pong.Version != relay.Version {
		t.Fatalf("unexpected pong frame: %#v", pong)
	}
}

func TestGatewayConnRejectsRelayErrorAsControlFrame(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	gateway := newGatewayConn(clientConn, "rs_gateway")
	t.Cleanup(func() { _ = gateway.Close() })

	if err := serverConn.WriteJSON(relay.NewErrorFrame(relay.CodeE2EERequired)); err != nil {
		t.Fatalf("write relay error: %v", err)
	}
	var payload map[string]any
	err := gateway.ReadJSON(&payload)
	if err == nil || !strings.Contains(err.Error(), relay.CodeE2EERequired) {
		t.Fatalf("expected relay control error, got %v", err)
	}
}

func TestGatewayConnIgnoresRelayDeliveryErrors(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	gateway := newGatewayConn(clientConn, "rs_gateway")
	t.Cleanup(func() { _ = gateway.Close() })

	go func() {
		_ = serverConn.WriteJSON(relay.NewErrorFrame(relay.CodeTargetUnavailable))
		_ = serverConn.WriteJSON(relay.ClientAttachedFrame{
			Type: relay.TypeClientAttached, Version: relay.Version,
			SessionID: "rs_gateway", ClientID: "rc_attached",
		})
		_ = serverConn.WriteJSON(testRelayForward("rc_attached"))
	}()

	var payload map[string]any
	if err := gateway.ReadJSON(&payload); err != nil {
		t.Fatalf("delivery error should not close gateway read loop: %v", err)
	}
	if payload["action"] != "ping" {
		t.Fatalf("unexpected payload after delivery error: %#v", payload)
	}
}

func TestGatewayConnRejectsWrongSessionRelayPing(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	gateway := newGatewayConn(clientConn, "rs_gateway")
	t.Cleanup(func() { _ = gateway.Close() })

	if err := serverConn.WriteJSON(map[string]any{
		"type":      relay.TypeRelayPing,
		"version":   relay.Version,
		"sessionId": "rs_other",
	}); err != nil {
		t.Fatalf("write relay ping: %v", err)
	}
	var payload map[string]any
	err := gateway.ReadJSON(&payload)
	if err == nil || !strings.Contains(err.Error(), "relay ping routing") {
		t.Fatalf("expected relay ping routing error, got %v", err)
	}
}

func TestGatewayConnRejectsE2EEControlFramesUntilHandshakeIsWired(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	gateway := newGatewayConn(clientConn, "rs_gateway")
	t.Cleanup(func() { _ = gateway.Close() })

	if err := serverConn.WriteJSON(relay.E2EEClientHelloFrame{
		Type: relay.TypeClientE2EEHello, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached", HandshakeID: "hs_test",
	}); err != nil {
		t.Fatalf("write e2ee control frame: %v", err)
	}
	var payload map[string]any
	err := gateway.ReadJSON(&payload)
	if err == nil || !strings.Contains(err.Error(), "e2ee handshake") {
		t.Fatalf("expected explicit e2ee control rejection, got %v", err)
	}
}

func TestGatewayConnHandlesPairingE2EEHandshake(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	nodeIdentity, err := e2ee.GenerateNodeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	capabilities := e2ee.ProductionCapabilities()
	handshake := newAgentE2EEHandshakeHandler(
		"rs_gateway",
		"pair-secret-128-bit-minimum",
		capabilities,
		nodeIdentity,
	)
	gateway := newGatewayConnWithE2EE(clientConn, "rs_gateway", handshake)
	t.Cleanup(func() { _ = gateway.Close() })

	clientEphemeral, err := e2ee.NewEphemeralKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	clientHello := relay.E2EEClientHelloFrame{
		Type: relay.TypeClientE2EEHello, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached", HandshakeID: "hs_pairing",
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
	if fmt.Sprintf("%x", e2ee.Fingerprint(agentMaterial.NodeIdentityPublicKey)) != testNodeFingerprintHexFromIdentity(nodeIdentity) {
		t.Fatal("agent hello node identity fingerprint mismatch")
	}
	input := capabilities.ApplyToHandshake(e2ee.HandshakeInput{
		Kind:                     e2ee.HandshakeKindPairing,
		SessionID:                "rs_gateway",
		ClientID:                 "rc_attached",
		HandshakeID:              "hs_pairing",
		ClientEphemeralPublicKey: clientEphemeral.PublicKey,
		NodeEphemeralPublicKey:   agentMaterial.NodeEphemeralPublicKey,
		NodeIdentityPublicKey:    agentMaterial.NodeIdentityPublicKey,
	})
	transcript, err := e2ee.HandshakeTranscript(input)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := e2ee.VerifyNodeSignature(
		agentMaterial.NodeIdentityPublicKey,
		transcript,
		agentMaterial.NodeSignature,
	)
	if err != nil || !verified {
		t.Fatalf("node signature verification failed: verified=%v err=%v", verified, err)
	}
	proof := relay.E2EEClientProofFrame{
		Type: relay.TypeClientE2EEProof, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached", HandshakeID: "hs_pairing",
		Kind:         e2ee.HandshakeKindPairing,
		PairingProof: e2ee.EncodeFrameBytes(e2ee.PairingProof("pair-secret-128-bit-minimum", transcript)),
	}
	if err := serverConn.WriteJSON(proof); err != nil {
		t.Fatalf("write client proof: %v", err)
	}
	var result relay.E2EEAgentResultFrame
	if err := serverConn.ReadJSON(&result); err != nil {
		t.Fatalf("read agent result: %v", err)
	}
	if err := e2ee.ValidateAgentResultFrame(result); err != nil {
		t.Fatalf("validate agent result: %v", err)
	}
	if !result.OK {
		t.Fatalf("unexpected failed result: %#v", result)
	}
	agentKeys, ok := handshake.trafficKeys("hs_pairing")
	if !ok {
		t.Fatal("missing completed agent traffic keys")
	}
	clientKeys, err := e2ee.DeriveHandshakeTrafficKeys(
		clientEphemeral.PrivateScalar,
		agentMaterial.NodeEphemeralPublicKey,
		input,
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(agentKeys.ClientToAgent) != string(clientKeys.ClientToAgent) ||
		string(agentKeys.AgentToClient) != string(clientKeys.AgentToClient) {
		t.Fatal("agent and client traffic keys differ")
	}
}

func TestGatewayConnEncryptsRelayForwardAfterPairingE2EEHandshake(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	handshake, clientEphemeral := testPairingHandshakeHandler(t)
	gateway := newGatewayConnWithE2EE(clientConn, "rs_gateway", handshake)
	t.Cleanup(func() { _ = gateway.Close() })

	if err := serverConn.WriteJSON(relay.ClientAttachedFrame{
		Type: relay.TypeClientAttached, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached",
	}); err != nil {
		t.Fatalf("write attached: %v", err)
	}
	clientKeys := driveGatewayE2EEHandshake(t, serverConn, clientEphemeral)
	clientCodec, err := e2ee.NewClientMobileVCStreamCodec("rs_gateway", "rc_attached", "hs_pairing", clientKeys)
	if err != nil {
		t.Fatal(err)
	}
	inbound, err := clientCodec.EncodeJSON("msg_in", map[string]string{"action": "ping"})
	if err != nil {
		t.Fatal(err)
	}
	if err := serverConn.WriteJSON(relay.ForwardEnvelope(inbound)); err != nil {
		t.Fatalf("write encrypted inbound: %v", err)
	}
	var decoded map[string]any
	if err := gateway.ReadJSON(&decoded); err != nil {
		t.Fatalf("read encrypted inbound: %v", err)
	}
	if decoded["action"] != "ping" {
		t.Fatalf("unexpected decrypted inbound: %#v", decoded)
	}

	if err := gateway.WriteJSON(map[string]string{"event": "ready"}); err != nil {
		t.Fatalf("write encrypted outbound: %v", err)
	}
	var outbound relay.ForwardEnvelope
	if err := serverConn.ReadJSON(&outbound); err != nil {
		t.Fatalf("read encrypted outbound: %v", err)
	}
	if outbound.Encryption != relay.EncryptionE2EEV1 || strings.Contains(outbound.Payload, "ready") {
		t.Fatalf("outbound was not encrypted: %#v", outbound)
	}
	var plaintext map[string]any
	if err := clientCodec.DecodeJSON(e2ee.RelayForwardFrame(outbound), &plaintext); err != nil {
		t.Fatalf("decrypt outbound: %v", err)
	}
	if plaintext["event"] != "ready" {
		t.Fatalf("unexpected decrypted outbound: %#v", plaintext)
	}
}

func TestGatewayConnActivatesE2EEBeforeReportingHandshakeOK(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	handshake, clientEphemeral := testPairingHandshakeHandler(t)
	gateway := newGatewayConnWithE2EE(clientConn, "rs_gateway", handshake)
	t.Cleanup(func() { _ = gateway.Close() })

	if err := serverConn.WriteJSON(relay.ClientAttachedFrame{
		Type: relay.TypeClientAttached, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached",
	}); err != nil {
		t.Fatalf("write attached: %v", err)
	}
	clientKeys := driveGatewayE2EEHandshake(t, serverConn, clientEphemeral)
	if gateway.e2eeStream() == nil {
		t.Fatal("gateway reported e2ee ok before activating local stream")
	}
	clientCodec, err := e2ee.NewClientMobileVCStreamCodec("rs_gateway", "rc_attached", "hs_pairing", clientKeys)
	if err != nil {
		t.Fatal(err)
	}
	inbound, err := clientCodec.EncodeJSON("msg_after_ok", map[string]string{"action": "ping"})
	if err != nil {
		t.Fatal(err)
	}
	if err := serverConn.WriteJSON(relay.ForwardEnvelope(inbound)); err != nil {
		t.Fatalf("write encrypted inbound after ok: %v", err)
	}
	var decoded map[string]any
	if err := gateway.ReadJSON(&decoded); err != nil {
		t.Fatalf("read encrypted inbound after ok: %v", err)
	}
	if decoded["action"] != "ping" {
		t.Fatalf("unexpected decrypted inbound: %#v", decoded)
	}
}

func TestGatewayConnWaitsForE2EEBeforeWritingProductionForward(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	handshake, clientEphemeral := testPairingHandshakeHandler(t)
	gateway := newGatewayConnWithE2EE(clientConn, "rs_gateway", handshake)
	t.Cleanup(func() { _ = gateway.Close() })

	if err := serverConn.WriteJSON(relay.ClientAttachedFrame{
		Type: relay.TypeClientAttached, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached",
	}); err != nil {
		t.Fatalf("write attached: %v", err)
	}
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- gateway.WriteJSON(map[string]string{"event": "ready"})
	}()

	select {
	case err := <-writeDone:
		t.Fatalf("write completed before e2ee activation: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	clientKeys := driveGatewayE2EEHandshake(t, serverConn, clientEphemeral)
	if err := <-writeDone; err != nil {
		t.Fatalf("write after e2ee activation: %v", err)
	}
	var outbound relay.ForwardEnvelope
	if err := serverConn.ReadJSON(&outbound); err != nil {
		t.Fatalf("read encrypted outbound: %v", err)
	}
	if outbound.Encryption != relay.EncryptionE2EEV1 || strings.Contains(outbound.Payload, "ready") {
		t.Fatalf("outbound was not encrypted: %#v", outbound)
	}
	clientCodec, err := e2ee.NewClientMobileVCStreamCodec("rs_gateway", "rc_attached", "hs_pairing", clientKeys)
	if err != nil {
		t.Fatal(err)
	}
	var plaintext map[string]any
	if err := clientCodec.DecodeJSON(e2ee.RelayForwardFrame(outbound), &plaintext); err != nil {
		t.Fatalf("decrypt outbound: %v", err)
	}
	if plaintext["event"] != "ready" {
		t.Fatalf("unexpected decrypted outbound: %#v", plaintext)
	}
}

func TestGatewayConnWaitsUntilReadyBeforeServingHandler(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	handshake, clientEphemeral := testPairingHandshakeHandler(t)
	gateway := newGatewayConnWithE2EE(clientConn, "rs_gateway", handshake)
	t.Cleanup(func() { _ = gateway.Close() })

	ready := make(chan error, 1)
	go func() {
		ready <- gateway.waitReadyForHandler()
	}()

	select {
	case err := <-ready:
		t.Fatalf("handler readiness completed before client attach: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := serverConn.WriteJSON(relay.ClientAttachedFrame{
		Type: relay.TypeClientAttached, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached",
	}); err != nil {
		t.Fatalf("write attached: %v", err)
	}
	select {
	case err := <-ready:
		t.Fatalf("handler readiness completed before e2ee activation: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	driveGatewayE2EEHandshake(t, serverConn, clientEphemeral)
	select {
	case err := <-ready:
		if err != nil {
			t.Fatalf("handler readiness after e2ee activation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("handler readiness did not complete after e2ee activation")
	}
}

func TestGatewayConnWaitReadyForHandlerUnblocksOnClose(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	handshake, _ := testPairingHandshakeHandler(t)
	gateway := newGatewayConnWithE2EE(clientConn, "rs_gateway", handshake)

	ready := make(chan error, 1)
	go func() {
		ready <- gateway.waitReadyForHandler()
	}()
	if err := gateway.Close(); err != nil {
		t.Fatalf("close gateway: %v", err)
	}
	select {
	case err := <-ready:
		if err == nil || !strings.Contains(err.Error(), "relay connection closed") {
			t.Fatalf("expected explicit close readiness error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("handler readiness stayed blocked after close")
	}
}

func TestGatewayConnResetsE2EEWhenClientReattaches(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	handshake, clientEphemeral := testPairingHandshakeHandler(t)
	gateway := newGatewayConnWithE2EE(clientConn, "rs_gateway", handshake)
	t.Cleanup(func() { _ = gateway.Close() })

	if err := serverConn.WriteJSON(relay.ClientAttachedFrame{
		Type: relay.TypeClientAttached, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached",
	}); err != nil {
		t.Fatalf("write attached: %v", err)
	}
	clientKeys := driveGatewayE2EEHandshake(t, serverConn, clientEphemeral)
	firstCodec, err := e2ee.NewClientMobileVCStreamCodec("rs_gateway", "rc_attached", "hs_pairing", clientKeys)
	if err != nil {
		t.Fatal(err)
	}
	if err := gateway.WriteJSON(map[string]string{"event": "first"}); err != nil {
		t.Fatalf("write first encrypted event: %v", err)
	}
	var first relay.ForwardEnvelope
	if err := serverConn.ReadJSON(&first); err != nil {
		t.Fatalf("read first encrypted event: %v", err)
	}
	var firstPlain map[string]any
	if err := firstCodec.DecodeJSON(e2ee.RelayForwardFrame(first), &firstPlain); err != nil {
		t.Fatalf("decrypt first event: %v", err)
	}

	if err := serverConn.WriteJSON(relay.ClientAttachedFrame{
		Type: relay.TypeClientAttached, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached",
	}); err != nil {
		t.Fatalf("write reattached: %v", err)
	}
	if err := serverConn.WriteJSON(relay.ControlFrame{Type: relay.TypeRelayPing, Version: relay.Version}); err != nil {
		t.Fatalf("write relay ping after reattach: %v", err)
	}
	var pong relay.ControlFrame
	if err := serverConn.ReadJSON(&pong); err != nil {
		t.Fatalf("read relay pong after reattach: %v", err)
	}
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- gateway.WriteJSON(map[string]string{"event": "after-reattach"})
	}()
	select {
	case err := <-writeDone:
		t.Fatalf("write completed before rekey: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	nextEphemeral, err := e2ee.NewEphemeralKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	nextKeys := driveGatewayE2EEHandshakeWithIDs(t, serverConn, nextEphemeral, "rc_attached", "hs_rekey")
	secondCodec, err := e2ee.NewClientMobileVCStreamCodec("rs_gateway", "rc_attached", "hs_rekey", nextKeys)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write after rekey: %v", err)
	}
	var second relay.ForwardEnvelope
	if err := serverConn.ReadJSON(&second); err != nil {
		t.Fatalf("read second encrypted event: %v", err)
	}
	if second.HandshakeID != "hs_rekey" {
		t.Fatalf("expected rekey handshake id, got %#v", second)
	}
	var secondPlain map[string]any
	if err := secondCodec.DecodeJSON(e2ee.RelayForwardFrame(second), &secondPlain); err != nil {
		t.Fatalf("decrypt second event: %v", err)
	}
	if secondPlain["event"] != "after-reattach" {
		t.Fatalf("unexpected second plaintext: %#v", secondPlain)
	}
}

func TestGatewayConnReattachWakesOldE2EEReadyWaiters(t *testing.T) {
	oldReadyCh := make(chan struct{})
	gateway := &gatewayConn{
		sessionID:   "rs_gateway",
		attachCh:    make(chan struct{}),
		readDone:    make(chan struct{}),
		closeCh:     make(chan struct{}),
		e2eeReadyCh: oldReadyCh,
		e2ee:        &agentE2EEHandshakeHandler{},
		stream:      &e2ee.MobileVCStreamCodec{},
		streamHS:    "hs_old",
		tunnelSend:  e2ee.NewTunnelCounterState(),
		downloads:   map[uint64]*fileDownloadStream{},
	}
	done := make(chan error, 1)
	go func() {
		done <- gateway.waitE2EEReady(oldReadyCh)
	}()

	gateway.activateAttachedClient("rc_attached")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("old e2ee ready waiter returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("reattach did not wake waiters on old e2ee ready channel")
	}
	nextReadyCh, err := gateway.currentE2EEReadyCh()
	if err != nil {
		t.Fatalf("current e2ee ready channel: %v", err)
	}
	if nextReadyCh == oldReadyCh {
		t.Fatal("reattach kept old e2ee ready channel")
	}
}

func TestGatewayConnWriteTimesOutWaitingForE2EEReady(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	handshake, _ := testPairingHandshakeHandler(t)
	gateway := newGatewayConnWithE2EE(clientConn, "rs_gateway", handshake)
	gateway.e2eeReadyTimeout = 20 * time.Millisecond
	t.Cleanup(func() { _ = gateway.Close() })

	if err := serverConn.WriteJSON(relay.ClientAttachedFrame{
		Type: relay.TypeClientAttached, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached",
	}); err != nil {
		t.Fatalf("write attached: %v", err)
	}
	err := gateway.WriteJSON(map[string]string{"event": "blocked"})
	if err == nil {
		t.Fatal("expected e2ee readiness timeout")
	}
	if !strings.Contains(err.Error(), relay.CodeE2EEHandshakeFailed) || !strings.Contains(err.Error(), "did not become ready") {
		t.Fatalf("unexpected e2ee readiness timeout error: %v", err)
	}
}

func TestGatewayConnRejectsPendingHandshakeAfterReattach(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	handshake, clientEphemeral := testPairingHandshakeHandler(t)
	gateway := newGatewayConnWithE2EE(clientConn, "rs_gateway", handshake)
	t.Cleanup(func() { _ = gateway.Close() })

	if err := serverConn.WriteJSON(relay.ClientAttachedFrame{
		Type: relay.TypeClientAttached, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached",
	}); err != nil {
		t.Fatalf("write attached: %v", err)
	}
	proof := startGatewayE2EEHandshakeProof(t, serverConn, clientEphemeral, "rc_attached", "hs_stale")
	if err := serverConn.WriteJSON(relay.ClientAttachedFrame{
		Type: relay.TypeClientAttached, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached",
	}); err != nil {
		t.Fatalf("write reattached: %v", err)
	}
	if err := serverConn.WriteJSON(proof); err != nil {
		t.Fatalf("write stale proof: %v", err)
	}
	var result relay.E2EEAgentResultFrame
	if err := serverConn.ReadJSON(&result); err != nil {
		t.Fatalf("read stale proof result: %v", err)
	}
	if result.OK || result.ErrorCode != relay.CodeE2EEHandshakeFailed {
		t.Fatalf("expected stale proof to fail after reattach, got %#v", result)
	}
}

func TestGatewayConnServesEncryptedFileDownloadTunnel(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	handshake, clientEphemeral := testPairingHandshakeHandler(t)
	root := t.TempDir()
	gateway := newGatewayConnWithE2EE(clientConn, "rs_gateway", handshake, []string{root})
	t.Cleanup(func() { _ = gateway.Close() })

	filePath := filepath.Join(root, "relay-download.txt")
	content := strings.Repeat("secret-data-", 32)
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := serverConn.WriteJSON(relay.ClientAttachedFrame{
		Type: relay.TypeClientAttached, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached",
	}); err != nil {
		t.Fatalf("write attached: %v", err)
	}
	clientKeys := driveGatewayE2EEHandshake(t, serverConn, clientEphemeral)
	clientCodec, err := e2ee.NewClientMobileVCStreamCodec("rs_gateway", "rc_attached", "hs_pairing", clientKeys)
	if err != nil {
		t.Fatal(err)
	}
	gateway.SetRelayE2EEDeviceID("dev_bound")

	openFrame, err := e2ee.NewFileDownloadOpenFrame(42, e2ee.FileDownloadMetadata{Path: filePath}, 4)
	if err != nil {
		t.Fatal(err)
	}
	openForward, err := clientCodec.EncodeTunnelFrame("msg_download_open", openFrame)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(openForward.Payload, filePath) {
		t.Fatal("encrypted download open leaked path in relay payload")
	}
	if err := serverConn.WriteJSON(relay.ForwardEnvelope(openForward)); err != nil {
		t.Fatalf("write encrypted open: %v", err)
	}
	readErr := make(chan error, 1)
	go func() {
		var payload map[string]any
		readErr <- gateway.ReadJSON(&payload)
	}()

	downloaded := make([]byte, 0, len(content))
	sawOpen := false
	for {
		var env relay.ForwardEnvelope
		if err := serverConn.ReadJSON(&env); err != nil {
			t.Fatalf("read encrypted download frame: %v", err)
		}
		if env.StreamID != 42 || env.Encryption != relay.EncryptionE2EEV1 {
			t.Fatalf("unexpected encrypted download envelope: %#v", env)
		}
		if strings.Contains(env.Payload, content) || strings.Contains(env.Payload, filePath) {
			t.Fatal("encrypted download payload leaked plaintext")
		}
		tunnelFrame, err := clientCodec.DecodeTunnelFrame(e2ee.RelayForwardFrame(env))
		if err != nil {
			t.Fatalf("decode encrypted tunnel frame: %v", err)
		}
		switch tunnelFrame.Type {
		case e2ee.TunnelFrameStreamOpen:
			sawOpen = true
			if tunnelFrame.Metadata["fileName"] != "relay-download.txt" {
				t.Fatalf("unexpected open metadata: %#v", tunnelFrame.Metadata)
			}
		case e2ee.TunnelFrameStreamData:
			downloaded = append(downloaded, tunnelFrame.Payload...)
			ack, err := e2ee.NewFileDownloadAckFrame(42, tunnelFrame.Seq, 4)
			if err != nil {
				t.Fatal(err)
			}
			ackForward, err := clientCodec.EncodeTunnelFrame("msg_download_ack", ack)
			if err != nil {
				t.Fatal(err)
			}
			if err := serverConn.WriteJSON(relay.ForwardEnvelope(ackForward)); err != nil {
				t.Fatalf("write download ack: %v", err)
			}
		case e2ee.TunnelFrameStreamClose:
			if !sawOpen {
				t.Fatal("download closed before open response")
			}
			if string(downloaded) != content {
				t.Fatalf("downloaded content mismatch: %q", string(downloaded))
			}
			select {
			case err := <-readErr:
				t.Fatalf("download tunnel leaked into gateway ReadJSON: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			return
		default:
			t.Fatalf("unexpected tunnel frame: %#v", tunnelFrame)
		}
	}
}

func TestGatewayConnRejectsEncryptedFileDownloadWhenSelectedRouteDenied(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	handshake, clientEphemeral := testPairingHandshakeHandler(t)
	root := t.TempDir()
	policy := relay.NewSelectedRoutePolicy(
		[]relay.RouteRule{{Method: http.MethodGet, Path: "/healthz"}},
		[]relay.RouteRule{{Method: http.MethodGet, Path: relay.SelectedRouteWSMobileVC}},
	)
	gateway := newGatewayConnWithPolicy(clientConn, "rs_gateway", handshake, []string{root}, policy)
	t.Cleanup(func() { _ = gateway.Close() })

	filePath := filepath.Join(root, "relay-download.txt")
	if err := os.WriteFile(filePath, []byte("secret-data"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	clientCodec := activateGatewayE2EETestStream(t, serverConn, clientEphemeral)
	gateway.SetRelayE2EEDeviceID("dev_bound")
	openFrame, err := e2ee.NewFileDownloadOpenFrame(42, e2ee.FileDownloadMetadata{Path: filePath}, 4)
	if err != nil {
		t.Fatal(err)
	}
	writeEncryptedTunnelFrame(t, serverConn, clientCodec, "msg_download_open", openFrame)
	go drainGatewayRead(t, gateway)

	tunnelFrame := readEncryptedTunnelFrame(t, serverConn, clientCodec)
	if tunnelFrame.Type != e2ee.TunnelFrameStreamError || tunnelFrame.ErrorCode != relay.CodeDownloadDenied {
		t.Fatalf("expected denied selected route stream error, got %#v", tunnelFrame)
	}
}

func TestGatewayConnRejectsEncryptedFileDownloadBeforeDeviceBinding(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	handshake, clientEphemeral := testPairingHandshakeHandler(t)
	root := t.TempDir()
	gateway := newGatewayConnWithE2EE(clientConn, "rs_gateway", handshake, []string{root})
	t.Cleanup(func() { _ = gateway.Close() })

	filePath := filepath.Join(root, "relay-download.txt")
	if err := os.WriteFile(filePath, []byte("secret-data"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := serverConn.WriteJSON(relay.ClientAttachedFrame{
		Type: relay.TypeClientAttached, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached",
	}); err != nil {
		t.Fatalf("write attached: %v", err)
	}
	clientKeys := driveGatewayE2EEHandshake(t, serverConn, clientEphemeral)
	clientCodec, err := e2ee.NewClientMobileVCStreamCodec("rs_gateway", "rc_attached", "hs_pairing", clientKeys)
	if err != nil {
		t.Fatal(err)
	}
	openFrame, err := e2ee.NewFileDownloadOpenFrame(42, e2ee.FileDownloadMetadata{Path: filePath}, 4)
	if err != nil {
		t.Fatal(err)
	}
	openForward, err := clientCodec.EncodeTunnelFrame("msg_download_open", openFrame)
	if err != nil {
		t.Fatal(err)
	}
	if err := serverConn.WriteJSON(relay.ForwardEnvelope(openForward)); err != nil {
		t.Fatalf("write encrypted open: %v", err)
	}
	go func() {
		var payload map[string]any
		_ = gateway.ReadJSON(&payload)
	}()

	var env relay.ForwardEnvelope
	if err := serverConn.ReadJSON(&env); err != nil {
		t.Fatalf("read encrypted download error: %v", err)
	}
	tunnelFrame, err := clientCodec.DecodeTunnelFrame(e2ee.RelayForwardFrame(env))
	if err != nil {
		t.Fatalf("decode encrypted tunnel frame: %v", err)
	}
	if tunnelFrame.Type != e2ee.TunnelFrameStreamError || tunnelFrame.ErrorCode != relay.CodeDownloadDenied {
		t.Fatalf("expected download_denied stream error, got %#v", tunnelFrame)
	}
}

func TestGatewayConnRejectsMobileVCStreamWhenSelectedRouteDenied(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	handshake, clientEphemeral := testPairingHandshakeHandler(t)
	policy := relay.NewSelectedRoutePolicy(
		[]relay.RouteRule{{Method: http.MethodGet, Path: relay.SelectedRouteHTTPDownload}},
		[]relay.RouteRule{{Method: http.MethodGet, Path: "/events"}},
	)
	gateway := newGatewayConnWithPolicy(clientConn, "rs_gateway", handshake, nil, policy)
	t.Cleanup(func() { _ = gateway.Close() })

	clientCodec := activateGatewayE2EETestStream(t, serverConn, clientEphemeral)
	inbound, err := clientCodec.EncodeJSON("msg_in", map[string]string{"action": "ping"})
	if err != nil {
		t.Fatal(err)
	}
	if err := serverConn.WriteJSON(relay.ForwardEnvelope(inbound)); err != nil {
		t.Fatalf("write encrypted inbound: %v", err)
	}
	var decoded map[string]any
	err = gateway.ReadJSON(&decoded)
	if err == nil || !strings.Contains(err.Error(), relay.CodeProtocolError) {
		t.Fatalf("expected mobilevc selected route protocol error, got payload=%#v err=%v", decoded, err)
	}
}

func TestGatewayConnRejectsUnsupportedSelectedRouteStreamType(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	handshake, clientEphemeral := testPairingHandshakeHandler(t)
	gateway := newGatewayConnWithE2EE(clientConn, "rs_gateway", handshake)
	t.Cleanup(func() { _ = gateway.Close() })

	clientCodec := activateGatewayE2EETestStream(t, serverConn, clientEphemeral)
	openFrame := e2ee.TunnelFrame{
		Type: e2ee.TunnelFrameStreamOpen, Version: e2ee.TunnelVersion,
		StreamID: 44, StreamType: "shell.exec", Window: 4,
	}
	writeRawTunnelFrame(t, serverConn, clientCodec, "msg_unknown_open", openFrame)
	go drainGatewayRead(t, gateway)

	tunnelFrame := readEncryptedTunnelFrame(t, serverConn, clientCodec)
	if tunnelFrame.Type != e2ee.TunnelFrameStreamError || tunnelFrame.ErrorCode != relay.CodeProtocolError {
		t.Fatalf("expected unsupported selected route stream cancellation, got %#v", tunnelFrame)
	}
}

func TestOpenDownloadTargetEnforcesConfiguredRoots(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "allowed.txt")
	if err := os.WriteFile(allowed, []byte("ok"), 0o600); err != nil {
		t.Fatalf("write allowed fixture: %v", err)
	}
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "outside.txt")
	if err := os.WriteFile(outside, []byte("no"), 0o600); err != nil {
		t.Fatalf("write outside fixture: %v", err)
	}

	path, file, _, err := openDownloadTarget(allowed, []string{root})
	if err != nil {
		t.Fatalf("open allowed target: %v", err)
	}
	_ = file.Close()
	resolvedAllowed, err := filepath.EvalSymlinks(allowed)
	if err != nil {
		t.Fatalf("resolve allowed path: %v", err)
	}
	if path != resolvedAllowed {
		t.Fatalf("path: got %q want %q", path, allowed)
	}

	if _, _, _, err := openDownloadTarget(outside, []string{root}); err == nil || !strings.Contains(err.Error(), relay.CodeDownloadDenied) {
		t.Fatalf("expected outside root denial, got %v", err)
	}
}

func TestOpenDownloadTargetRejectsSymlinkOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "outside.txt")
	if err := os.WriteFile(outside, []byte("no"), 0o600); err != nil {
		t.Fatalf("write outside fixture: %v", err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if _, _, _, err := openDownloadTarget(link, []string{root}); err == nil || !strings.Contains(err.Error(), relay.CodeDownloadDenied) {
		t.Fatalf("expected symlink escape denial, got %v", err)
	}
}

func TestValidateConfigRequiresDownloadRootInProductionE2EE(t *testing.T) {
	cfg := Config{
		RelayURL:           "wss://relay.example.test",
		PairingTTL:         time.Minute,
		AgentGracePeriod:   time.Minute,
		PairingEventPath:   filepath.Join(t.TempDir(), "pairing.json"),
		Capabilities:       e2ee.ProductionCapabilities(),
		NodeFingerprintHex: strings.Repeat("a", 64),
	}
	nodeIdentity, err := e2ee.GenerateNodeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	cfg.NodeIdentity = nodeIdentity
	if err := validateConfig(cfg); err == nil || !strings.Contains(err.Error(), "download root") {
		t.Fatalf("expected missing download root error, got %v", err)
	}

	cfg.DownloadRoots = []string{t.TempDir()}
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("valid download root rejected: %v", err)
	}
}

func TestGatewayConnRejectsPlaintextForwardBeforeProductionE2EE(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	handshake, _ := testPairingHandshakeHandler(t)
	gateway := newGatewayConnWithE2EE(clientConn, "rs_gateway", handshake)
	t.Cleanup(func() { _ = gateway.Close() })

	if err := serverConn.WriteJSON(relay.ClientAttachedFrame{
		Type: relay.TypeClientAttached, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached",
	}); err != nil {
		t.Fatalf("write attached: %v", err)
	}
	if err := serverConn.WriteJSON(testRelayForward("rc_attached")); err != nil {
		t.Fatalf("write plaintext forward: %v", err)
	}
	var payload map[string]any
	err := gateway.ReadJSON(&payload)
	if err == nil || !strings.Contains(err.Error(), relay.CodeE2EERequired) {
		t.Fatalf("expected plaintext e2ee rejection, got payload=%#v err=%v", payload, err)
	}
}

func TestGatewayConnRejectsMismatchedPairingE2EEProofRoute(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	handshake, clientEphemeral := testPairingHandshakeHandler(t)
	gateway := newGatewayConnWithE2EE(clientConn, "rs_gateway", handshake)
	t.Cleanup(func() { _ = gateway.Close() })

	capabilities := e2ee.ProductionCapabilities()
	clientHello := relay.E2EEClientHelloFrame{
		Type: relay.TypeClientE2EEHello, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached", HandshakeID: "hs_pairing",
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
		SessionID:                "rs_gateway",
		ClientID:                 "rc_attached",
		HandshakeID:              "hs_pairing",
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
		SessionID: "rs_gateway", ClientID: "rc_other", HandshakeID: "hs_pairing",
		Kind:         e2ee.HandshakeKindPairing,
		PairingProof: e2ee.EncodeFrameBytes(e2ee.PairingProof("pair-secret-128-bit-minimum", transcript)),
	}
	if err := serverConn.WriteJSON(proof); err != nil {
		t.Fatalf("write client proof: %v", err)
	}
	var result relay.E2EEAgentResultFrame
	if err := serverConn.ReadJSON(&result); err != nil {
		t.Fatalf("read agent result: %v", err)
	}
	if result.OK || result.ErrorCode != relay.CodeE2EEHandshakeFailed {
		t.Fatalf("expected handshake failure result, got %#v", result)
	}
}

func TestGatewayConnHandlesReconnectE2EEHandshake(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	handshake, clientEphemeral, deviceIdentity, credential := testReconnectHandshakeHandler(t)
	gateway := newGatewayConnWithE2EE(clientConn, "rs_gateway", handshake)
	t.Cleanup(func() { _ = gateway.Close() })

	capabilities := e2ee.ProductionCapabilities()
	clientHello := relay.E2EEClientHelloFrame{
		Type: relay.TypeClientE2EEHello, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached", HandshakeID: "hs_reconnect",
		Kind: e2ee.HandshakeKindReconnect, Capabilities: &capabilities,
		ClientEphemeralPublicKey: e2ee.EncodeFrameBytes(clientEphemeral.PublicKey),
		DeviceID:                 "dev_pixel",
		DeviceIdentityPublicKey:  e2ee.EncodeFrameBytes(deviceIdentity.PublicKey),
	}
	if err := serverConn.WriteJSON(clientHello); err != nil {
		t.Fatalf("write reconnect hello: %v", err)
	}
	var agentHello relay.E2EEAgentHelloFrame
	if err := serverConn.ReadJSON(&agentHello); err != nil {
		t.Fatalf("read reconnect agent hello: %v", err)
	}
	agentMaterial, err := e2ee.ValidateAgentHelloFrame(agentHello)
	if err != nil {
		t.Fatalf("validate reconnect agent hello: %v", err)
	}
	input := capabilities.ApplyToHandshake(e2ee.HandshakeInput{
		Kind:                     e2ee.HandshakeKindReconnect,
		SessionID:                "rs_gateway",
		ClientID:                 "rc_attached",
		HandshakeID:              "hs_reconnect",
		ClientEphemeralPublicKey: clientEphemeral.PublicKey,
		NodeEphemeralPublicKey:   agentMaterial.NodeEphemeralPublicKey,
		NodeIdentityPublicKey:    agentMaterial.NodeIdentityPublicKey,
		DeviceIdentityPublicKey:  deviceIdentity.PublicKey,
	})
	transcript, err := e2ee.HandshakeTranscript(input)
	if err != nil {
		t.Fatal(err)
	}
	deviceSignature, err := deviceIdentity.SignTranscript(transcript)
	if err != nil {
		t.Fatal(err)
	}
	proof := relay.E2EEClientProofFrame{
		Type: relay.TypeClientE2EEProof, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached", HandshakeID: "hs_reconnect",
		Kind:            e2ee.HandshakeKindReconnect,
		DeviceProof:     e2ee.EncodeFrameBytes(e2ee.DeviceProof(credential, transcript)),
		DeviceSignature: e2ee.EncodeFrameBytes(deviceSignature),
	}
	if err := serverConn.WriteJSON(proof); err != nil {
		t.Fatalf("write reconnect proof: %v", err)
	}
	var result relay.E2EEAgentResultFrame
	if err := serverConn.ReadJSON(&result); err != nil {
		t.Fatalf("read reconnect result: %v", err)
	}
	if !result.OK {
		t.Fatalf("unexpected reconnect failure: %#v", result)
	}
	agentKeys, ok := handshake.trafficKeys("hs_reconnect")
	if !ok {
		t.Fatal("missing reconnect traffic keys")
	}
	clientKeys, err := e2ee.DeriveHandshakeTrafficKeys(
		clientEphemeral.PrivateScalar,
		agentMaterial.NodeEphemeralPublicKey,
		input,
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(agentKeys.ClientToAgent) != string(clientKeys.ClientToAgent) ||
		string(agentKeys.AgentToClient) != string(clientKeys.AgentToClient) {
		t.Fatal("reconnect traffic keys differ")
	}
}

func TestGatewayConnRejectsReconnectE2EEProofFailures(t *testing.T) {
	tests := []struct {
		name         string
		deviceID     string
		mutateProof  func(*relay.E2EEClientProofFrame)
		revokeDevice bool
		wantCode     string
	}{
		{name: "unknown device", deviceID: "dev_unknown", wantCode: relay.CodeDeviceUnknown},
		{name: "wrong credential", mutateProof: func(frame *relay.E2EEClientProofFrame) {
			frame.DeviceProof = e2ee.EncodeFrameBytes([]byte("wrong-proof"))
		}, wantCode: relay.CodeDeviceUnknown},
		{name: "bad signature", mutateProof: func(frame *relay.E2EEClientProofFrame) {
			frame.DeviceSignature = e2ee.EncodeFrameBytes([]byte("bad-signature"))
		}, wantCode: relay.CodeDeviceUnknown},
		{name: "revoked device", revokeDevice: true, wantCode: relay.CodeDeviceRevoked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverConn, clientConn := newRelayClientTestConns(t)
			defer serverConn.Close()
			handshake, clientEphemeral, deviceIdentity, credential := testReconnectHandshakeHandler(t)
			if tt.revokeDevice {
				if _, err := handshake.deviceTrust.RevokeDevice("dev_pixel", time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
			}
			gateway := newGatewayConnWithE2EE(clientConn, "rs_gateway", handshake)
			t.Cleanup(func() { _ = gateway.Close() })

			deviceID := "dev_pixel"
			if tt.deviceID != "" {
				deviceID = tt.deviceID
			}
			proof := driveReconnectHello(t, serverConn, clientEphemeral, deviceIdentity, deviceID, credential)
			if tt.mutateProof != nil {
				tt.mutateProof(proof)
			}
			if err := serverConn.WriteJSON(proof); err != nil {
				t.Fatalf("write reconnect proof: %v", err)
			}
			var result relay.E2EEAgentResultFrame
			if err := serverConn.ReadJSON(&result); err != nil {
				t.Fatalf("read reconnect result: %v", err)
			}
			if result.OK || result.ErrorCode != tt.wantCode {
				t.Fatalf("unexpected reconnect result: %#v", result)
			}
		})
	}
}

func TestGatewayConnCloseUnblocksFullReadQueueSend(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	gateway := &gatewayConn{
		conn: clientConn, readCh: make(chan readResult, relayReadQueueSize),
		closeCh: make(chan struct{}),
	}

	for i := 0; i < relayReadQueueSize; i++ {
		gateway.readCh <- readResult{}
	}
	if err := gateway.Close(); err != nil {
		t.Fatalf("close gateway: %v", err)
	}
	done := make(chan struct{})
	go func() {
		if gateway.sendReadResult(readResult{}) {
			t.Error("sendReadResult succeeded after gateway close")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sendReadResult stayed blocked after gateway close")
	}
}

func TestGatewayConnFullReadQueueFailsExplicitly(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	gateway := &gatewayConn{
		conn: clientConn, readCh: make(chan readResult, relayReadQueueSize),
		closeCh:          make(chan struct{}),
		readQueueTimeout: 10 * time.Millisecond,
	}

	for i := 0; i < relayReadQueueSize; i++ {
		gateway.readCh <- readResult{}
	}
	if gateway.sendReadResult(readResult{}) {
		t.Fatal("sendReadResult succeeded with a full read queue")
	}
	if err := gateway.readError(); err == nil || !strings.Contains(err.Error(), errRelayReadQueueFull.Error()) {
		t.Fatalf("expected explicit read queue full error, got %v", err)
	}
	select {
	case <-gateway.closeCh:
	case <-time.After(time.Second):
		t.Fatal("full read queue did not close the gateway")
	}
}

func TestRegisterAgentUsesControlReadDeadline(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	t.Cleanup(func() { clientConn.Close() })

	done := make(chan error, 1)
	go func() {
		done <- registerAgent(clientConn, agentRegisterRequest{
			SessionID:       "rs_timeout",
			PairSecret:      "pair-secret",
			ReconnectSecret: "reconnect-secret",
			ExpiresAt:       time.Now().Add(time.Minute),
		})
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected registration read timeout")
		}
	case <-time.After(relayControlTimeout + time.Second):
		t.Fatal("registration did not honor control read deadline")
	}
}

func TestRegisterAgentClearsWriteDeadlineAfterControlWrite(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	t.Cleanup(func() { clientConn.Close() })

	done := make(chan error, 1)
	go func() {
		done <- registerAgent(clientConn, agentRegisterRequest{
			SessionID:       "rs_deadline",
			PairSecret:      "pair-secret",
			ReconnectSecret: "reconnect-secret",
			ExpiresAt:       time.Now().Add(time.Minute),
		})
	}()

	var registered relay.AgentRegisterFrame
	if err := serverConn.ReadJSON(&registered); err != nil {
		t.Fatalf("read agent register: %v", err)
	}
	if err := serverConn.WriteJSON(relay.AgentRegisteredFrame{
		Type: relay.TypeAgentRegistered, Version: relay.Version,
		SessionID: "rs_deadline",
	}); err != nil {
		t.Fatalf("write registration response: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("register agent: %v", err)
	}
	time.Sleep(relayControlTimeout + 100*time.Millisecond)
	if err := clientConn.WriteJSON(map[string]string{"type": "after_deadline"}); err != nil {
		t.Fatalf("write after cleared deadline: %v", err)
	}
	var next map[string]string
	if err := serverConn.ReadJSON(&next); err != nil {
		t.Fatalf("read after cleared deadline: %v", err)
	}
	if next["type"] != "after_deadline" {
		t.Fatalf("unexpected post-deadline frame: %#v", next)
	}
}

func TestRegisterAgentSendsExplicitCapabilities(t *testing.T) {
	serverConn, clientConn := newRelayClientTestConns(t)
	defer serverConn.Close()
	t.Cleanup(func() { clientConn.Close() })

	done := make(chan error, 1)
	go func() {
		done <- registerAgent(clientConn, agentRegisterRequest{
			SessionID:       "rs_capability",
			PairSecret:      "pair-secret",
			ReconnectSecret: "reconnect-secret",
			ExpiresAt:       time.Now().Add(time.Minute),
			Capabilities:    e2ee.PlaintextTestCapabilities(),
		})
	}()

	var registered relay.AgentRegisterFrame
	if err := serverConn.ReadJSON(&registered); err != nil {
		t.Fatalf("read agent register: %v", err)
	}
	if registered.Capabilities == nil {
		t.Fatal("missing agent capabilities")
	}
	if err := e2ee.ValidatePlaintextTestCapabilities(*registered.Capabilities); err != nil {
		t.Fatalf("invalid plaintext capabilities: %v", err)
	}
	if err := serverConn.WriteJSON(relay.AgentRegisteredFrame{
		Type: relay.TypeAgentRegistered, Version: relay.Version,
		SessionID: "rs_capability",
	}); err != nil {
		t.Fatalf("write registration response: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("register agent: %v", err)
	}
}

func TestPairingEventIncludesCapabilities(t *testing.T) {
	event := PairingReadyEvent{
		Type:               "mobilevc.relay.pairing_ready",
		RelayURL:           "wss://relay.example.test",
		SessionID:          "rs_capability",
		PairingSecret:      "pair-secret",
		ExpiresAt:          time.Now().Add(time.Minute).Unix(),
		Capabilities:       e2ee.PlaintextTestCapabilities(),
		NodeFingerprintHex: testNodeFingerprintHex,
		LANHost:            "192.168.1.2",
		LANPort:            "8001",
		LANToken:           "direct-token",
		LANCWD:             "/workspace",
	}

	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal pairing event: %v", err)
	}
	var decoded PairingReadyEvent
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode pairing event: %v", err)
	}
	if err := e2ee.ValidatePlaintextTestCapabilities(decoded.Capabilities); err != nil {
		t.Fatalf("decoded capabilities: %v", err)
	}
	if decoded.NodeFingerprintHex != testNodeFingerprintHex {
		t.Fatalf("node fingerprint: got %q", decoded.NodeFingerprintHex)
	}
	if decoded.LANHost != "192.168.1.2" ||
		decoded.LANPort != "8001" ||
		decoded.LANToken != "direct-token" ||
		decoded.LANCWD != "/workspace" {
		t.Fatalf("decoded LAN endpoint: %#v", decoded)
	}
}

func TestValidateConfigRequiresNodeFingerprint(t *testing.T) {
	err := validateConfig(Config{
		RelayURL:         "wss://relay.example.test",
		PairingTTL:       time.Minute,
		AgentGracePeriod: time.Minute,
		PairingEventPath: "/tmp/mobilevc-relay-pairing.json",
	})
	if err == nil || !strings.Contains(err.Error(), "node fingerprint") {
		t.Fatalf("expected node fingerprint config error, got %v", err)
	}
}

const testNodeFingerprintHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func testNodeFingerprintHexFromIdentity(identity *e2ee.NodeIdentity) string {
	return fmt.Sprintf("%x", identity.Fingerprint)
}

func testPairingHandshakeHandler(t *testing.T) (*agentE2EEHandshakeHandler, *e2ee.EphemeralKeyPair) {
	t.Helper()
	nodeIdentity, err := e2ee.GenerateNodeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	clientEphemeral, err := e2ee.NewEphemeralKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	return newAgentE2EEHandshakeHandler(
		"rs_gateway",
		"pair-secret-128-bit-minimum",
		e2ee.ProductionCapabilities(),
		nodeIdentity,
	), clientEphemeral
}

func testReconnectHandshakeHandler(t *testing.T) (*agentE2EEHandshakeHandler, *e2ee.EphemeralKeyPair, *e2ee.NodeIdentity, string) {
	t.Helper()
	nodeIdentity, err := e2ee.GenerateNodeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	deviceIdentity, err := e2ee.GenerateNodeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	clientEphemeral, err := e2ee.NewEphemeralKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	trustStore, err := e2ee.LoadOrCreateDeviceTrustStore(t.TempDir() + "/" + e2ee.DeviceTrustFileName)
	if err != nil {
		t.Fatal(err)
	}
	credential := "device-credential-128-bit-minimum"
	if _, err := trustStore.RegisterDevice(e2ee.DeviceRegistration{
		ID: "dev_pixel", DisplayName: "Pixel", PublicKey: deviceIdentity.PublicKey,
		DeviceCredential: credential,
	}); err != nil {
		t.Fatal(err)
	}
	return newAgentE2EEHandshakeHandlerWithDeviceTrust(
		"rs_gateway",
		"pair-secret-128-bit-minimum",
		e2ee.ProductionCapabilities(),
		nodeIdentity,
		trustStore,
	), clientEphemeral, deviceIdentity, credential
}

func driveReconnectHello(t *testing.T, serverConn *websocket.Conn, clientEphemeral *e2ee.EphemeralKeyPair, deviceIdentity *e2ee.NodeIdentity, deviceID string, credential string) *relay.E2EEClientProofFrame {
	t.Helper()
	capabilities := e2ee.ProductionCapabilities()
	clientHello := relay.E2EEClientHelloFrame{
		Type: relay.TypeClientE2EEHello, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached", HandshakeID: "hs_reconnect",
		Kind: e2ee.HandshakeKindReconnect, Capabilities: &capabilities,
		ClientEphemeralPublicKey: e2ee.EncodeFrameBytes(clientEphemeral.PublicKey),
		DeviceID:                 deviceID,
		DeviceIdentityPublicKey:  e2ee.EncodeFrameBytes(deviceIdentity.PublicKey),
	}
	if err := serverConn.WriteJSON(clientHello); err != nil {
		t.Fatalf("write reconnect hello: %v", err)
	}
	var agentHello relay.E2EEAgentHelloFrame
	if err := serverConn.ReadJSON(&agentHello); err != nil {
		t.Fatalf("read reconnect agent hello: %v", err)
	}
	agentMaterial, err := e2ee.ValidateAgentHelloFrame(agentHello)
	if err != nil {
		t.Fatalf("validate reconnect agent hello: %v", err)
	}
	input := capabilities.ApplyToHandshake(e2ee.HandshakeInput{
		Kind:                     e2ee.HandshakeKindReconnect,
		SessionID:                "rs_gateway",
		ClientID:                 "rc_attached",
		HandshakeID:              "hs_reconnect",
		ClientEphemeralPublicKey: clientEphemeral.PublicKey,
		NodeEphemeralPublicKey:   agentMaterial.NodeEphemeralPublicKey,
		NodeIdentityPublicKey:    agentMaterial.NodeIdentityPublicKey,
		DeviceIdentityPublicKey:  deviceIdentity.PublicKey,
	})
	transcript, err := e2ee.HandshakeTranscript(input)
	if err != nil {
		t.Fatal(err)
	}
	deviceSignature, err := deviceIdentity.SignTranscript(transcript)
	if err != nil {
		t.Fatal(err)
	}
	return &relay.E2EEClientProofFrame{
		Type: relay.TypeClientE2EEProof, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached", HandshakeID: "hs_reconnect",
		Kind:            e2ee.HandshakeKindReconnect,
		DeviceProof:     e2ee.EncodeFrameBytes(e2ee.DeviceProof(credential, transcript)),
		DeviceSignature: e2ee.EncodeFrameBytes(deviceSignature),
	}
}

func driveGatewayE2EEHandshake(t *testing.T, serverConn *websocket.Conn, clientEphemeral *e2ee.EphemeralKeyPair) *e2ee.TrafficKeys {
	t.Helper()
	return driveGatewayE2EEHandshakeWithIDs(t, serverConn, clientEphemeral, "rc_attached", "hs_pairing")
}

func startGatewayE2EEHandshakeProof(
	t *testing.T,
	serverConn *websocket.Conn,
	clientEphemeral *e2ee.EphemeralKeyPair,
	clientID string,
	handshakeID string,
) relay.E2EEClientProofFrame {
	t.Helper()
	capabilities := e2ee.ProductionCapabilities()
	clientHello := relay.E2EEClientHelloFrame{
		Type: relay.TypeClientE2EEHello, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: clientID, HandshakeID: handshakeID,
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
		SessionID:                "rs_gateway",
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
	return relay.E2EEClientProofFrame{
		Type: relay.TypeClientE2EEProof, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: clientID, HandshakeID: handshakeID,
		Kind:         e2ee.HandshakeKindPairing,
		PairingProof: e2ee.EncodeFrameBytes(e2ee.PairingProof("pair-secret-128-bit-minimum", transcript)),
	}
}

func driveGatewayE2EEHandshakeWithIDs(
	t *testing.T,
	serverConn *websocket.Conn,
	clientEphemeral *e2ee.EphemeralKeyPair,
	clientID string,
	handshakeID string,
) *e2ee.TrafficKeys {
	t.Helper()
	capabilities := e2ee.ProductionCapabilities()
	clientHello := relay.E2EEClientHelloFrame{
		Type: relay.TypeClientE2EEHello, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: clientID, HandshakeID: handshakeID,
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
		SessionID:                "rs_gateway",
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
		SessionID: "rs_gateway", ClientID: clientID, HandshakeID: handshakeID,
		Kind:         e2ee.HandshakeKindPairing,
		PairingProof: e2ee.EncodeFrameBytes(e2ee.PairingProof("pair-secret-128-bit-minimum", transcript)),
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
	keys, err := e2ee.DeriveHandshakeTrafficKeys(
		clientEphemeral.PrivateScalar,
		agentMaterial.NodeEphemeralPublicKey,
		input,
	)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func activateGatewayE2EETestStream(t *testing.T, serverConn *websocket.Conn, clientEphemeral *e2ee.EphemeralKeyPair) *e2ee.MobileVCStreamCodec {
	t.Helper()
	if err := serverConn.WriteJSON(relay.ClientAttachedFrame{
		Type: relay.TypeClientAttached, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: "rc_attached",
	}); err != nil {
		t.Fatalf("write attached: %v", err)
	}
	clientKeys := driveGatewayE2EEHandshake(t, serverConn, clientEphemeral)
	codec, err := e2ee.NewClientMobileVCStreamCodec("rs_gateway", "rc_attached", "hs_pairing", clientKeys)
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func writeEncryptedTunnelFrame(t *testing.T, serverConn *websocket.Conn, codec *e2ee.MobileVCStreamCodec, messageID string, frame e2ee.TunnelFrame) {
	t.Helper()
	forward, err := codec.EncodeTunnelFrame(messageID, frame)
	if err != nil {
		t.Fatal(err)
	}
	if err := serverConn.WriteJSON(relay.ForwardEnvelope(forward)); err != nil {
		t.Fatalf("write encrypted tunnel frame: %v", err)
	}
}

func writeRawTunnelFrame(t *testing.T, serverConn *websocket.Conn, codec *e2ee.MobileVCStreamCodec, messageID string, frame e2ee.TunnelFrame) {
	t.Helper()
	raw, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal raw tunnel frame: %v", err)
	}
	forward, err := codec.EncodeStream(frame.StreamID, messageID, raw)
	if err != nil {
		t.Fatalf("encode raw tunnel frame: %v", err)
	}
	if err := serverConn.WriteJSON(relay.ForwardEnvelope(forward)); err != nil {
		t.Fatalf("write raw tunnel frame: %v", err)
	}
}

func readEncryptedTunnelFrame(t *testing.T, serverConn *websocket.Conn, codec *e2ee.MobileVCStreamCodec) e2ee.TunnelFrame {
	t.Helper()
	var env relay.ForwardEnvelope
	if err := serverConn.ReadJSON(&env); err != nil {
		t.Fatalf("read encrypted tunnel frame: %v", err)
	}
	tunnelFrame, err := codec.DecodeTunnelFrame(e2ee.RelayForwardFrame(env))
	if err != nil {
		t.Fatalf("decode encrypted tunnel frame: %v", err)
	}
	return tunnelFrame
}

func drainGatewayRead(t *testing.T, gateway *gatewayConn) {
	t.Helper()
	var payload map[string]any
	_ = gateway.ReadJSON(&payload)
}

func newRelayClientTestConns(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade relayclient test: %v", err)
			return
		}
		accepted <- conn
	}))
	t.Cleanup(server.Close)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial relayclient test: %v", err)
	}
	select {
	case serverConn := <-accepted:
		return serverConn, client
	case <-time.After(time.Second):
		t.Fatal("relayclient websocket was not accepted")
	}
	return nil, nil
}

func newTrackedRelayClientTestConns(t *testing.T) (*websocket.Conn, *websocket.Conn, *trackingConn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade tracked relayclient test: %v", err)
			return
		}
		accepted <- conn
	}))
	t.Cleanup(server.Close)

	var tracked *trackingConn
	dialer := websocket.Dialer{
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			tracked = &trackingConn{Conn: conn}
			return tracked, nil
		},
	}
	client, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial tracked relayclient test: %v", err)
	}
	if tracked == nil {
		t.Fatal("tracked relayclient connection was not installed")
	}
	select {
	case serverConn := <-accepted:
		return serverConn, client, tracked
	case <-time.After(time.Second):
		t.Fatal("tracked relayclient websocket was not accepted")
	}
	return nil, nil, nil
}

type trackingConn struct {
	net.Conn
	mu                 sync.Mutex
	writeDeadlineCalls int
	nonZeroWriteCalls  int
}

func (c *trackingConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	c.writeDeadlineCalls++
	if !t.IsZero() {
		c.nonZeroWriteCalls++
	}
	c.mu.Unlock()
	return c.Conn.SetWriteDeadline(t)
}

func (c *trackingConn) resetWriteDeadlines() {
	c.mu.Lock()
	c.writeDeadlineCalls = 0
	c.nonZeroWriteCalls = 0
	c.mu.Unlock()
}

func (c *trackingConn) sawNonZeroWriteDeadline() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nonZeroWriteCalls > 0
}

func testRelayForward(clientID string) relay.ForwardEnvelope {
	return relay.ForwardEnvelope{
		Type: relay.TypeRelayForward, Version: relay.Version,
		SessionID: "rs_gateway", ClientID: clientID,
		Direction:   relay.DirectionClientToAgent,
		ContentType: relay.ContentTypeMobileVC, Encryption: relay.EncryptionNone,
		MessageID: "msg_test", PayloadEncoding: relay.PayloadBase64URL,
		Payload: "eyJhY3Rpb24iOiJwaW5nIn0",
	}
}
