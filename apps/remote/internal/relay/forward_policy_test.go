package relay

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestForwardSecurityPolicyRejectsPlaintextInProduction(t *testing.T) {
	policy := ForwardSecurityPolicy{RequireE2EE: true}
	err := policy.Validate(forwardPolicyEnvelope(EncryptionNone))

	if !isForwardPolicyError(err, CodeE2EERequired) {
		t.Fatalf("expected e2ee required error, got %v", err)
	}
}

func TestForwardSecurityPolicyAllowsPlaintextOnlyInTestMode(t *testing.T) {
	policy := ForwardSecurityPolicy{PlaintextTestMode: true}
	err := policy.Validate(forwardPolicyEnvelope(EncryptionNone))

	if err != nil {
		t.Fatalf("validate plaintext test mode: %v", err)
	}
}

func TestForwardSecurityPolicyAcceptsE2EEForwardMetadata(t *testing.T) {
	policy := ForwardSecurityPolicy{RequireE2EE: true}
	env := forwardPolicyEnvelope(EncryptionE2EEV1)
	env.StreamID = 7
	env.HandshakeID = "hs_123"
	env.Counter = 0

	if err := policy.Validate(env); err != nil {
		t.Fatalf("validate e2ee forward: %v", err)
	}
}

func TestE2EEForwardJSONIncludesZeroCounter(t *testing.T) {
	env := forwardPolicyEnvelope(EncryptionE2EEV1)
	env.StreamID = 1
	env.Counter = 0
	env.HandshakeID = "hs_123"
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["counter"]; !ok {
		t.Fatalf("missing zero counter field in %s", raw)
	}
}

func TestForwardSecurityPolicyRejectsIncompleteE2EEForwardMetadata(t *testing.T) {
	policy := ForwardSecurityPolicy{RequireE2EE: true}
	env := forwardPolicyEnvelope(EncryptionE2EEV1)
	env.StreamID = 7

	if err := policy.Validate(env); err == nil {
		t.Fatal("expected missing handshake id to fail")
	}
}

func TestForwardSecurityPolicyRejectsUnsupportedEncryption(t *testing.T) {
	policy := ForwardSecurityPolicy{RequireE2EE: true}
	err := policy.Validate(forwardPolicyEnvelope("future-suite"))

	if !isForwardPolicyError(err, CodeE2EEUnsupported) {
		t.Fatalf("expected unsupported e2ee error, got %v", err)
	}
}

func forwardPolicyEnvelope(encryption string) ForwardEnvelope {
	return ForwardEnvelope{
		Type:            TypeRelayForward,
		Version:         Version,
		SessionID:       "rs_test",
		ClientID:        "rc_test",
		Direction:       DirectionClientToAgent,
		MessageID:       "msg_test",
		ContentType:     ContentTypeMobileVC,
		Encryption:      encryption,
		PayloadEncoding: PayloadBase64URL,
		Payload:         "e30",
	}
}

func isForwardPolicyError(err error, code string) bool {
	switch code {
	case CodeE2EERequired:
		return errors.Is(err, ErrE2EERequired)
	case CodeE2EEUnsupported:
		return errors.Is(err, ErrE2EEUnsupported)
	default:
		return err != nil && err.Error() == code
	}
}
