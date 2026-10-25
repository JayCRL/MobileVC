package e2ee

import (
	"bytes"
	"errors"
	"testing"
)

func TestPairingHandshakeAuthenticatesAndDerivesFreshKeys(t *testing.T) {
	clientEphemeral, nodeEphemeral, input := testHandshakeInput(t, HandshakeKindPairing)
	nodeIdentity := testNodeIdentity(t)
	input.NodeIdentityPublicKey = nodeIdentity.PublicKey
	pairingSecret := "pair-secret-128-bit-minimum"

	transcript, err := HandshakeTranscript(input)
	if err != nil {
		t.Fatal(err)
	}
	nodeSignature, err := nodeIdentity.SignTranscript(transcript)
	if err != nil {
		t.Fatal(err)
	}
	pairingProof := PairingProof(pairingSecret, transcript)

	if err := ValidatePairingHandshake(input, pairingSecret, pairingProof, nodeSignature); err != nil {
		t.Fatal(err)
	}

	clientKeys, err := DeriveHandshakeTrafficKeys(clientEphemeral.PrivateScalar, nodeEphemeral.PublicKey, input)
	if err != nil {
		t.Fatal(err)
	}
	nodeKeys, err := DeriveHandshakeTrafficKeys(nodeEphemeral.PrivateScalar, clientEphemeral.PublicKey, input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(clientKeys.ClientToAgent, nodeKeys.ClientToAgent) ||
		!bytes.Equal(clientKeys.AgentToClient, nodeKeys.AgentToClient) {
		t.Fatal("client and node derived different handshake traffic keys")
	}
	if bytes.Equal(clientKeys.ClientToAgent, clientKeys.AgentToClient) {
		t.Fatal("directional traffic keys must differ")
	}

	_, _, secondInput := testHandshakeInput(t, HandshakeKindPairing)
	secondInput.NodeIdentityPublicKey = nodeIdentity.PublicKey
	secondKeys, err := DeriveHandshakeTrafficKeys(clientEphemeral.PrivateScalar, nodeEphemeral.PublicKey, secondInput)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(clientKeys.ClientToAgent, secondKeys.ClientToAgent) {
		t.Fatal("fresh handshake id did not rekey traffic")
	}
}

func TestPairingHandshakeRejectsBadProofAndSignature(t *testing.T) {
	_, _, input := testHandshakeInput(t, HandshakeKindPairing)
	nodeIdentity := testNodeIdentity(t)
	input.NodeIdentityPublicKey = nodeIdentity.PublicKey
	transcript, err := HandshakeTranscript(input)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := nodeIdentity.SignTranscript(transcript)
	if err != nil {
		t.Fatal(err)
	}

	if err := ValidatePairingHandshake(input, "right", PairingProof("wrong", transcript), signature); !errors.Is(err, ErrHandshakeFailed) {
		t.Fatalf("expected bad proof failure, got %v", err)
	}

	tampered := append([]byte(nil), signature...)
	tampered[0] ^= 0x01
	if err := ValidatePairingHandshake(input, "right", PairingProof("right", transcript), tampered); !errors.Is(err, ErrHandshakeFailed) {
		t.Fatalf("expected bad signature failure, got %v", err)
	}
}

func TestReconnectHandshakeRequiresDeviceSignatureAndFreshKeys(t *testing.T) {
	clientEphemeral, nodeEphemeral, input := testHandshakeInput(t, HandshakeKindReconnect)
	nodeIdentity := testNodeIdentity(t)
	deviceIdentity := testNodeIdentity(t)
	input.NodeIdentityPublicKey = nodeIdentity.PublicKey
	input.DeviceIdentityPublicKey = deviceIdentity.PublicKey
	deviceCredential := "device-credential-128-bit-minimum"

	transcript, err := HandshakeTranscript(input)
	if err != nil {
		t.Fatal(err)
	}
	nodeSignature, err := nodeIdentity.SignTranscript(transcript)
	if err != nil {
		t.Fatal(err)
	}
	deviceSignature, err := deviceIdentity.SignTranscript(transcript)
	if err != nil {
		t.Fatal(err)
	}
	deviceProof := DeviceProof(deviceCredential, transcript)

	if err := ValidateReconnectHandshake(input, deviceCredential, deviceProof, nodeSignature, deviceSignature); err != nil {
		t.Fatal(err)
	}

	clientKeys, err := DeriveHandshakeTrafficKeys(clientEphemeral.PrivateScalar, nodeEphemeral.PublicKey, input)
	if err != nil {
		t.Fatal(err)
	}
	rekeyInput := input
	rekeyInput.HandshakeID = "hs_reconnect_02"
	rekeyKeys, err := DeriveHandshakeTrafficKeys(clientEphemeral.PrivateScalar, nodeEphemeral.PublicKey, rekeyInput)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(clientKeys.ClientToAgent, rekeyKeys.ClientToAgent) {
		t.Fatal("reconnect with a new handshake id reused the old traffic key")
	}

	badDeviceSignature := append([]byte(nil), deviceSignature...)
	badDeviceSignature[len(badDeviceSignature)-1] ^= 0x01
	if err := ValidateReconnectHandshake(input, deviceCredential, deviceProof, nodeSignature, badDeviceSignature); !errors.Is(err, ErrHandshakeFailed) {
		t.Fatalf("expected bad device signature failure, got %v", err)
	}
}

func testHandshakeInput(t *testing.T, kind string) (*EphemeralKeyPair, *EphemeralKeyPair, HandshakeInput) {
	t.Helper()
	clientEphemeral, err := NewEphemeralKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	nodeEphemeral, err := NewEphemeralKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	return clientEphemeral, nodeEphemeral, HandshakeInput{
		Kind:                     kind,
		SessionID:                "rs_handshake",
		ClientID:                 "rc_handshake",
		HandshakeID:              "hs_" + kind + "_01",
		RelayProtocolVersion:     1,
		E2EEProtocolVersion:      int(Version),
		TunnelProtocolVersion:    1,
		CryptoSuite:              Suite,
		ClientEphemeralPublicKey: clientEphemeral.PublicKey,
		NodeEphemeralPublicKey:   nodeEphemeral.PublicKey,
		RequiresE2EE:             true,
		SupportsMultiplexStreams: true,
		SupportsFileDownload:     true,
		SupportsDeviceManagement: true,
	}
}

func testNodeIdentity(t *testing.T) *NodeIdentity {
	t.Helper()
	identity, err := GenerateNodeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return identity
}
