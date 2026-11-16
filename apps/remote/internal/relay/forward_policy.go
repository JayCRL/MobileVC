package relay

import (
	"errors"
	"strings"
)

var (
	ErrE2EERequired    = errors.New(CodeE2EERequired)
	ErrE2EEUnsupported = errors.New(CodeE2EEUnsupported)
)

type ForwardSecurityPolicy struct {
	RequireE2EE       bool
	PlaintextTestMode bool
}

func (p ForwardSecurityPolicy) Validate(env ForwardEnvelope) error {
	switch env.Encryption {
	case EncryptionNone:
		return p.validatePlaintext()
	case EncryptionE2EEV1:
		return validateEncryptedForward(env)
	default:
		return ErrE2EEUnsupported
	}
}

func (p ForwardSecurityPolicy) validatePlaintext() error {
	if p.RequireE2EE && !p.PlaintextTestMode {
		return ErrE2EERequired
	}
	if !p.PlaintextTestMode {
		return ErrE2EERequired
	}
	return nil
}

func validateEncryptedForward(env ForwardEnvelope) error {
	if env.StreamID == 0 || strings.TrimSpace(env.HandshakeID) == "" {
		return errors.New("missing e2ee forward metadata")
	}
	if env.PayloadEncoding != PayloadBase64URL {
		return errors.New("invalid e2ee payload encoding")
	}
	return nil
}
