package e2ee

import (
	"errors"
	"testing"
)

func TestProductionCapabilitiesPassValidation(t *testing.T) {
	capabilities := ProductionCapabilities()

	if err := ValidateProductionCapabilities(capabilities); err != nil {
		t.Fatalf("validate production capabilities: %v", err)
	}
}

func TestProductionCapabilitiesRejectPlaintextTestMode(t *testing.T) {
	capabilities := ProductionCapabilities()
	capabilities.PlaintextTestMode = true

	if err := ValidateProductionCapabilities(capabilities); !errors.Is(err, ErrHandshakeFailed) {
		t.Fatalf("expected production plaintext test-mode rejection, got %v", err)
	}
}

func TestProductionCapabilitiesRejectMissingTunnelFeature(t *testing.T) {
	capabilities := ProductionCapabilities()
	capabilities.SupportsFileDownload = false

	if err := ValidateProductionCapabilities(capabilities); !errors.Is(err, ErrHandshakeFailed) {
		t.Fatalf("expected missing capability rejection, got %v", err)
	}
}

func TestPlaintextTestCapabilitiesRequireExplicitTestMode(t *testing.T) {
	capabilities := PlaintextTestCapabilities()
	if err := ValidatePlaintextTestCapabilities(capabilities); err != nil {
		t.Fatalf("validate plaintext test capabilities: %v", err)
	}

	capabilities.PlaintextTestMode = false
	if err := ValidatePlaintextTestCapabilities(capabilities); !errors.Is(err, ErrHandshakeFailed) {
		t.Fatalf("expected implicit plaintext rejection, got %v", err)
	}
}

func TestCapabilitiesApplyToHandshakeTranscript(t *testing.T) {
	clientEphemeral, nodeEphemeral, input := testHandshakeInput(t, HandshakeKindPairing)
	capabilities := ProductionCapabilities()
	input = capabilities.ApplyToHandshake(input)
	input.ClientEphemeralPublicKey = clientEphemeral.PublicKey
	input.NodeEphemeralPublicKey = nodeEphemeral.PublicKey
	input.NodeIdentityPublicKey = testNodeIdentity(t).PublicKey

	if _, err := HandshakeTranscript(input); err != nil {
		t.Fatalf("capability-bound transcript: %v", err)
	}
}

func TestCapabilitiesRejectUnsupportedVersion(t *testing.T) {
	capabilities := ProductionCapabilities()
	capabilities.TunnelProtocolVersion = 2

	if err := ValidateProductionCapabilities(capabilities); !errors.Is(err, ErrHandshakeFailed) {
		t.Fatalf("expected unsupported version rejection, got %v", err)
	}
}
