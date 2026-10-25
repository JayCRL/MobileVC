package e2ee

import "testing"

func TestValidatePairingHandshakeFrames(t *testing.T) {
	clientEphemeral, nodeEphemeral, input := testHandshakeInput(t, HandshakeKindPairing)
	nodeIdentity := testNodeIdentity(t)
	input.NodeIdentityPublicKey = nodeIdentity.PublicKey
	transcript, err := HandshakeTranscript(input)
	if err != nil {
		t.Fatal(err)
	}
	nodeSignature, err := nodeIdentity.SignTranscript(transcript)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := ProductionCapabilities()

	clientHello := ClientHelloFrame{
		Type:                     FrameTypeClientE2EEHello,
		Version:                  RelayProtocolVersion,
		SessionID:                input.SessionID,
		ClientID:                 input.ClientID,
		HandshakeID:              input.HandshakeID,
		Kind:                     input.Kind,
		Capabilities:             &capabilities,
		ClientEphemeralPublicKey: EncodeFrameBytes(clientEphemeral.PublicKey),
	}
	clientMaterial, err := ValidateClientHelloFrame(clientHello)
	if err != nil {
		t.Fatal(err)
	}
	if string(clientMaterial.ClientEphemeralPublicKey) != string(clientEphemeral.PublicKey) {
		t.Fatal("client hello did not decode client ephemeral key")
	}

	agentHello := AgentHelloFrame{
		Type:                   FrameTypeAgentE2EEHello,
		Version:                RelayProtocolVersion,
		SessionID:              input.SessionID,
		ClientID:               input.ClientID,
		HandshakeID:            input.HandshakeID,
		Capabilities:           &capabilities,
		NodeEphemeralPublicKey: EncodeFrameBytes(nodeEphemeral.PublicKey),
		NodeIdentityPublicKey:  EncodeFrameBytes(nodeIdentity.PublicKey),
		NodeSignature:          EncodeFrameBytes(nodeSignature),
	}
	agentMaterial, err := ValidateAgentHelloFrame(agentHello)
	if err != nil {
		t.Fatal(err)
	}
	if len(agentMaterial.NodeSignature) == 0 {
		t.Fatal("agent hello did not decode node signature")
	}

	proof := ClientProofFrame{
		Type:         FrameTypeClientE2EEProof,
		Version:      RelayProtocolVersion,
		SessionID:    input.SessionID,
		ClientID:     input.ClientID,
		HandshakeID:  input.HandshakeID,
		Kind:         input.Kind,
		PairingProof: EncodeFrameBytes(PairingProof("secret", transcript)),
	}
	proofMaterial, err := ValidateClientProofFrame(proof)
	if err != nil {
		t.Fatal(err)
	}
	if len(proofMaterial.PairingProof) == 0 {
		t.Fatal("client proof did not decode pairing proof")
	}

	result := AgentResultFrame{
		Type:        FrameTypeAgentE2EEResult,
		Version:     RelayProtocolVersion,
		SessionID:   input.SessionID,
		ClientID:    input.ClientID,
		HandshakeID: input.HandshakeID,
		OK:          true,
	}
	if err := ValidateAgentResultFrame(result); err != nil {
		t.Fatal(err)
	}
}

func TestValidateReconnectHandshakeFramesRequireDeviceIdentity(t *testing.T) {
	clientEphemeral, _, input := testHandshakeInput(t, HandshakeKindReconnect)
	deviceIdentity := testNodeIdentity(t)
	capabilities := ProductionCapabilities()

	hello := ClientHelloFrame{
		Type:                     FrameTypeClientE2EEHello,
		Version:                  RelayProtocolVersion,
		SessionID:                input.SessionID,
		ClientID:                 input.ClientID,
		HandshakeID:              input.HandshakeID,
		Kind:                     input.Kind,
		Capabilities:             &capabilities,
		ClientEphemeralPublicKey: EncodeFrameBytes(clientEphemeral.PublicKey),
		DeviceID:                 "rd_test",
		DeviceIdentityPublicKey:  EncodeFrameBytes(deviceIdentity.PublicKey),
	}
	if _, err := ValidateClientHelloFrame(hello); err != nil {
		t.Fatal(err)
	}

	hello.DeviceIdentityPublicKey = ""
	if _, err := ValidateClientHelloFrame(hello); err == nil {
		t.Fatal("expected missing device identity to fail")
	}
}

func TestValidateHandshakeFramesRejectInvalidShapes(t *testing.T) {
	capabilities := ProductionCapabilities()
	_, _, input := testHandshakeInput(t, HandshakeKindPairing)

	hello := ClientHelloFrame{
		Type:                     FrameTypeClientE2EEHello,
		Version:                  RelayProtocolVersion,
		SessionID:                input.SessionID,
		ClientID:                 input.ClientID,
		HandshakeID:              input.HandshakeID,
		Kind:                     input.Kind,
		Capabilities:             &capabilities,
		ClientEphemeralPublicKey: "not-base64url",
	}
	if _, err := ValidateClientHelloFrame(hello); err == nil {
		t.Fatal("expected malformed public key to fail")
	}

	hello.ClientEphemeralPublicKey = ""
	if _, err := ValidateClientHelloFrame(hello); err == nil {
		t.Fatal("expected missing public key to fail")
	}

	hello.ClientEphemeralPublicKey = EncodeFrameBytes([]byte{1, 2, 3})
	if _, err := ValidateClientHelloFrame(hello); err == nil {
		t.Fatal("expected invalid p256 public key to fail")
	}

	hello.Capabilities = nil
	if _, err := ValidateClientHelloFrame(hello); err == nil {
		t.Fatal("expected missing capabilities to fail")
	}

	plaintextCapabilities := PlaintextTestCapabilities()
	hello.Capabilities = &plaintextCapabilities
	hello.ClientEphemeralPublicKey = EncodeFrameBytes(input.ClientEphemeralPublicKey)
	if _, err := ValidateClientHelloFrame(hello); err == nil {
		t.Fatal("expected plaintext-test capabilities to fail")
	}

	result := AgentResultFrame{
		Type:        FrameTypeAgentE2EEResult,
		Version:     RelayProtocolVersion,
		SessionID:   input.SessionID,
		ClientID:    input.ClientID,
		HandshakeID: input.HandshakeID,
		OK:          false,
	}
	if err := ValidateAgentResultFrame(result); err == nil {
		t.Fatal("expected failed result without error code to fail")
	}
}

func TestValidateClientProofFrameRejectsKindFieldMixups(t *testing.T) {
	_, _, input := testHandshakeInput(t, HandshakeKindReconnect)
	frame := ClientProofFrame{
		Type:            FrameTypeClientE2EEProof,
		Version:         RelayProtocolVersion,
		SessionID:       input.SessionID,
		ClientID:        input.ClientID,
		HandshakeID:     input.HandshakeID,
		Kind:            HandshakeKindReconnect,
		PairingProof:    EncodeFrameBytes([]byte("unexpected")),
		DeviceProof:     EncodeFrameBytes([]byte("device-proof")),
		DeviceSignature: EncodeFrameBytes([]byte("device-signature")),
	}
	if _, err := ValidateClientProofFrame(frame); err == nil {
		t.Fatal("expected reconnect proof with pairing proof to fail")
	}

	frame.Kind = HandshakeKindPairing
	frame.PairingProof = EncodeFrameBytes([]byte("pairing-proof"))
	if _, err := ValidateClientProofFrame(frame); err == nil {
		t.Fatal("expected pairing proof with device fields to fail")
	}
}
