package relayclient

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"mobilevc/internal/relay"
	"mobilevc/internal/relay/e2ee"
)

type agentE2EEHandshakeHandler struct {
	mu            sync.Mutex
	sessionID     string
	pairingSecret string
	capabilities  e2ee.CapabilitySet
	nodeIdentity  *e2ee.NodeIdentity
	deviceTrust   *e2ee.DeviceTrustStore
	pending       map[string]pendingHandshake
	completed     map[string]completedHandshake
}

type pendingHandshake struct {
	input         e2ee.HandshakeInput
	nodeEphemeral *e2ee.EphemeralKeyPair
	deviceID      string
}

type completedHandshake struct {
	clientID string
	deviceID string
	keys     *e2ee.TrafficKeys
}

func newAgentE2EEHandshakeHandler(sessionID string, pairingSecret string, capabilities e2ee.CapabilitySet, nodeIdentity *e2ee.NodeIdentity) *agentE2EEHandshakeHandler {
	return newAgentE2EEHandshakeHandlerWithDeviceTrust(sessionID, pairingSecret, capabilities, nodeIdentity, nil)
}

func newAgentE2EEHandshakeHandlerWithDeviceTrust(sessionID string, pairingSecret string, capabilities e2ee.CapabilitySet, nodeIdentity *e2ee.NodeIdentity, deviceTrust *e2ee.DeviceTrustStore) *agentE2EEHandshakeHandler {
	if nodeIdentity == nil {
		return nil
	}
	if err := e2ee.ValidateProductionCapabilities(capabilities); err != nil {
		return nil
	}
	return &agentE2EEHandshakeHandler{
		sessionID: sessionID, pairingSecret: pairingSecret,
		capabilities: capabilities, nodeIdentity: nodeIdentity, deviceTrust: deviceTrust,
		pending:   map[string]pendingHandshake{},
		completed: map[string]completedHandshake{},
	}
}

func (h *agentE2EEHandshakeHandler) handleClientHello(frame relay.E2EEClientHelloFrame) (relay.E2EEAgentHelloFrame, error) {
	if h == nil {
		return relay.E2EEAgentHelloFrame{}, fmt.Errorf("relay e2ee handshake is not configured")
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	material, err := e2ee.ValidateClientHelloFrame(frame)
	if err != nil {
		return relay.E2EEAgentHelloFrame{}, err
	}
	if frame.SessionID != h.sessionID {
		return relay.E2EEAgentHelloFrame{}, fmt.Errorf("%w: invalid session", e2ee.ErrHandshakeFailed)
	}
	if frame.Kind == e2ee.HandshakeKindReconnect && h.deviceTrust == nil {
		return relay.E2EEAgentHelloFrame{}, fmt.Errorf("%w: device trust store is required", e2ee.ErrHandshakeFailed)
	}
	nodeEphemeral, err := e2ee.NewEphemeralKeyPair()
	if err != nil {
		return relay.E2EEAgentHelloFrame{}, err
	}
	input := material.Capabilities.ApplyToHandshake(e2ee.HandshakeInput{
		Kind:                     frame.Kind,
		SessionID:                frame.SessionID,
		ClientID:                 frame.ClientID,
		HandshakeID:              frame.HandshakeID,
		ClientEphemeralPublicKey: material.ClientEphemeralPublicKey,
		NodeEphemeralPublicKey:   nodeEphemeral.PublicKey,
		NodeIdentityPublicKey:    h.nodeIdentity.PublicKey,
		DeviceIdentityPublicKey:  material.DeviceIdentityPublicKey,
	})
	transcript, err := e2ee.HandshakeTranscript(input)
	if err != nil {
		return relay.E2EEAgentHelloFrame{}, err
	}
	signature, err := h.nodeIdentity.SignTranscript(transcript)
	if err != nil {
		return relay.E2EEAgentHelloFrame{}, err
	}
	h.pending[frame.HandshakeID] = pendingHandshake{
		input: input, nodeEphemeral: nodeEphemeral, deviceID: strings.TrimSpace(frame.DeviceID),
	}
	return relay.E2EEAgentHelloFrame{
		Type: relay.TypeAgentE2EEHello, Version: relay.Version,
		SessionID: frame.SessionID, ClientID: frame.ClientID, HandshakeID: frame.HandshakeID,
		Capabilities:           &h.capabilities,
		NodeEphemeralPublicKey: e2ee.EncodeFrameBytes(nodeEphemeral.PublicKey),
		NodeIdentityPublicKey:  e2ee.EncodeFrameBytes(h.nodeIdentity.PublicKey),
		NodeSignature:          e2ee.EncodeFrameBytes(signature),
	}, nil
}

func (h *agentE2EEHandshakeHandler) handleClientProof(frame relay.E2EEClientProofFrame) (relay.E2EEAgentResultFrame, error) {
	if h == nil {
		return e2eeAgentResult(frame, false, relay.CodeE2EEHandshakeFailed), fmt.Errorf("relay e2ee handshake is not configured")
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	material, err := e2ee.ValidateClientProofFrame(frame)
	if err != nil {
		return e2eeAgentResult(frame, false, relay.CodeE2EEHandshakeFailed), err
	}
	pending, ok := h.pending[frame.HandshakeID]
	if !ok || frame.Kind != pending.input.Kind || !sameHandshakeRoute(frame, pending.input) {
		return e2eeAgentResult(frame, false, relay.CodeE2EEHandshakeFailed), fmt.Errorf("%w: missing pending handshake", e2ee.ErrHandshakeFailed)
	}
	transcript, err := e2ee.HandshakeTranscript(pending.input)
	if err != nil {
		return e2eeAgentResult(frame, false, relay.CodeE2EEHandshakeFailed), err
	}
	if code, err := h.verifyProof(pending, material, transcript); err != nil {
		delete(h.pending, frame.HandshakeID)
		return e2eeAgentResult(frame, false, code), err
	}
	keys, err := e2ee.DeriveHandshakeTrafficKeys(
		pending.nodeEphemeral.PrivateScalar,
		pending.input.ClientEphemeralPublicKey,
		pending.input,
	)
	if err != nil {
		delete(h.pending, frame.HandshakeID)
		return e2eeAgentResult(frame, false, relay.CodeE2EEHandshakeFailed), err
	}
	delete(h.pending, frame.HandshakeID)
	h.completed[frame.HandshakeID] = completedHandshake{
		clientID: pending.input.ClientID,
		deviceID: pending.deviceID,
		keys:     keys,
	}
	return e2eeAgentResult(frame, true, ""), nil
}

func (h *agentE2EEHandshakeHandler) verifyProof(pending pendingHandshake, material *e2ee.FrameMaterial, transcript []byte) (string, error) {
	if pending.input.Kind == e2ee.HandshakeKindPairing {
		if !e2ee.VerifyPairingProof(h.pairingSecret, transcript, material.PairingProof) {
			return relay.CodeE2EEHandshakeFailed, e2ee.ErrHandshakeFailed
		}
		return "", nil
	}
	device, err := h.deviceTrust.VerifyDeviceProof(
		pending.deviceID,
		pending.input.DeviceIdentityPublicKey,
		transcript,
		material.DeviceProof,
		material.DeviceSignature,
	)
	if err != nil {
		return relayCodeForDeviceTrustError(err), err
	}
	if _, _, err := h.deviceTrust.MarkDeviceSeen(device.ID, pending.input.HandshakeID, time.Now().UTC()); err != nil {
		return relayCodeForDeviceTrustError(err), err
	}
	return "", nil
}

func (h *agentE2EEHandshakeHandler) trafficKeys(handshakeID string) (*e2ee.TrafficKeys, bool) {
	if h == nil {
		return nil, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	completed, ok := h.completed[handshakeID]
	return completed.keys, ok
}

func (h *agentE2EEHandshakeHandler) resetHandshakes() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	h.pending = map[string]pendingHandshake{}
	h.completed = map[string]completedHandshake{}
}

func (h *agentE2EEHandshakeHandler) completedDeviceID(handshakeID string) string {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.completed[handshakeID].deviceID
}

func (h *agentE2EEHandshakeHandler) completedCodec(handshakeID string) (*e2ee.MobileVCStreamCodec, bool, error) {
	if h == nil {
		return nil, false, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	completed, ok := h.completed[handshakeID]
	if !ok {
		return nil, false, nil
	}
	codec, err := e2ee.NewAgentMobileVCStreamCodec(
		h.sessionID,
		completed.clientID,
		handshakeID,
		completed.keys,
	)
	if err != nil {
		return nil, false, err
	}
	return codec, true, nil
}

func sameHandshakeRoute(frame relay.E2EEClientProofFrame, input e2ee.HandshakeInput) bool {
	return frame.SessionID == input.SessionID &&
		frame.ClientID == input.ClientID &&
		frame.HandshakeID == input.HandshakeID
}

func e2eeAgentResult(frame relay.E2EEClientProofFrame, ok bool, code string) relay.E2EEAgentResultFrame {
	return relay.E2EEAgentResultFrame{
		Type: relay.TypeAgentE2EEResult, Version: relay.Version,
		SessionID: frame.SessionID, ClientID: frame.ClientID, HandshakeID: frame.HandshakeID,
		OK: ok, ErrorCode: code,
	}
}

func relayCodeForDeviceTrustError(err error) string {
	if strings.Contains(err.Error(), relay.CodeDeviceRevoked) {
		return relay.CodeDeviceRevoked
	}
	return relay.CodeDeviceUnknown
}
