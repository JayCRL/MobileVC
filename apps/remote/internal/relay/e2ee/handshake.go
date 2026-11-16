package e2ee

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

const (
	HandshakeKindPairing   = "pairing"
	HandshakeKindReconnect = "reconnect"
	ProofPurposePairing    = "pairing_secret"
	ProofPurposeDevice     = "device_credential"
)

var ErrHandshakeFailed = errors.New("e2ee handshake failed")

type HandshakeInput struct {
	Kind                     string
	SessionID                string
	ClientID                 string
	HandshakeID              string
	RelayProtocolVersion     int
	E2EEProtocolVersion      int
	TunnelProtocolVersion    int
	CryptoSuite              string
	ClientEphemeralPublicKey []byte
	NodeEphemeralPublicKey   []byte
	NodeIdentityPublicKey    []byte
	DeviceIdentityPublicKey  []byte
	RequiresE2EE             bool
	PlaintextTestMode        bool
	SupportsMultiplexStreams bool
	SupportsFileDownload     bool
	SupportsDeviceManagement bool
}

type EphemeralKeyPair struct {
	PrivateScalar []byte
	PublicKey     []byte
}

type TrafficKeys struct {
	ClientToAgent []byte
	AgentToClient []byte
}

func NewEphemeralKeyPair() (*EphemeralKeyPair, error) {
	privateKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate e2ee ephemeral key: %w", err)
	}
	return &EphemeralKeyPair{
		PrivateScalar: privateKey.Bytes(),
		PublicKey:     privateKey.PublicKey().Bytes(),
	}, nil
}

func HandshakeTranscript(input HandshakeInput) ([]byte, error) {
	if err := validateHandshakeInput(input); err != nil {
		return nil, err
	}
	out := make([]byte, 0, 256)
	out = append(out, 'M', 'V', 'C', 'H')
	out = appendUint16(out, Version)
	fields := []string{
		input.Kind,
		input.SessionID,
		input.ClientID,
		input.HandshakeID,
		input.CryptoSuite,
	}
	for _, field := range fields {
		var err error
		out, err = appendString(out, field)
		if err != nil {
			return nil, err
		}
	}
	out = appendInt(out, input.RelayProtocolVersion)
	out = appendInt(out, input.E2EEProtocolVersion)
	out = appendInt(out, input.TunnelProtocolVersion)
	out = appendBool(out, input.RequiresE2EE)
	out = appendBool(out, input.PlaintextTestMode)
	out = appendBool(out, input.SupportsMultiplexStreams)
	out = appendBool(out, input.SupportsFileDownload)
	out = appendBool(out, input.SupportsDeviceManagement)
	for _, key := range [][]byte{
		input.ClientEphemeralPublicKey,
		input.NodeEphemeralPublicKey,
		input.NodeIdentityPublicKey,
		input.DeviceIdentityPublicKey,
	} {
		var err error
		out, err = appendBytes(out, key)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func PairingProof(pairingSecret string, transcript []byte) []byte {
	return proof(ProofPurposePairing, pairingSecret, transcript)
}

func DeviceProof(deviceCredential string, transcript []byte) []byte {
	return proof(ProofPurposeDevice, deviceCredential, transcript)
}

func VerifyPairingProof(pairingSecret string, transcript []byte, expected []byte) bool {
	actual := PairingProof(pairingSecret, transcript)
	return len(actual) == len(expected) && subtle.ConstantTimeCompare(actual, expected) == 1
}

func VerifyDeviceProof(deviceCredential string, transcript []byte, expected []byte) bool {
	actual := DeviceProof(deviceCredential, transcript)
	return len(actual) == len(expected) && subtle.ConstantTimeCompare(actual, expected) == 1
}

func DeriveHandshakeTrafficKeys(privateScalar []byte, remotePublicKey []byte, input HandshakeInput) (*TrafficKeys, error) {
	transcript, err := HandshakeTranscript(input)
	if err != nil {
		return nil, err
	}
	sharedSecret, err := SharedSecret(privateScalar, remotePublicKey)
	if err != nil {
		return nil, err
	}
	transcriptHash := sha256.Sum256(transcript)
	keyMaterial, err := hkdf.Key(
		sha256.New,
		sharedSecret,
		transcriptHash[:],
		"mobilevc relay e2ee handshake traffic v1|"+Suite,
		KeyLength*2,
	)
	if err != nil {
		return nil, fmt.Errorf("derive handshake traffic keys: %w", err)
	}
	return &TrafficKeys{
		ClientToAgent: keyMaterial[:KeyLength],
		AgentToClient: keyMaterial[KeyLength:],
	}, nil
}

func ValidatePairingHandshake(input HandshakeInput, pairingSecret string, pairingProof []byte, nodeSignature []byte) error {
	transcript, err := HandshakeTranscript(input)
	if err != nil {
		return err
	}
	verified, err := VerifyNodeSignature(input.NodeIdentityPublicKey, transcript, nodeSignature)
	if err != nil {
		return err
	}
	if !verified || !VerifyPairingProof(pairingSecret, transcript, pairingProof) {
		return ErrHandshakeFailed
	}
	return nil
}

func ValidateReconnectHandshake(input HandshakeInput, deviceCredential string, deviceProof []byte, nodeSignature []byte, deviceSignature []byte) error {
	transcript, err := HandshakeTranscript(input)
	if err != nil {
		return err
	}
	nodeVerified, err := VerifyNodeSignature(input.NodeIdentityPublicKey, transcript, nodeSignature)
	if err != nil {
		return err
	}
	deviceVerified, err := VerifyNodeSignature(input.DeviceIdentityPublicKey, transcript, deviceSignature)
	if err != nil {
		return err
	}
	if !nodeVerified || !deviceVerified || !VerifyDeviceProof(deviceCredential, transcript, deviceProof) {
		return ErrHandshakeFailed
	}
	return nil
}

func validateHandshakeInput(input HandshakeInput) error {
	if input.Kind != HandshakeKindPairing && input.Kind != HandshakeKindReconnect {
		return fmt.Errorf("%w: invalid kind", ErrHandshakeFailed)
	}
	if input.CryptoSuite != Suite {
		return fmt.Errorf("%w: unsupported crypto suite", ErrHandshakeFailed)
	}
	if input.E2EEProtocolVersion != int(Version) {
		return fmt.Errorf("%w: unsupported e2ee version", ErrHandshakeFailed)
	}
	if input.PlaintextTestMode && input.RequiresE2EE {
		return fmt.Errorf("%w: conflicting plaintext and e2ee requirements", ErrHandshakeFailed)
	}
	for _, key := range [][]byte{
		input.ClientEphemeralPublicKey,
		input.NodeEphemeralPublicKey,
		input.NodeIdentityPublicKey,
	} {
		if _, err := ecdh.P256().NewPublicKey(key); err != nil {
			return fmt.Errorf("%w: invalid p256 public key", ErrHandshakeFailed)
		}
	}
	if input.Kind == HandshakeKindReconnect {
		if _, err := ecdh.P256().NewPublicKey(input.DeviceIdentityPublicKey); err != nil {
			return fmt.Errorf("%w: invalid device identity key", ErrHandshakeFailed)
		}
	}
	return nil
}

func proof(purpose string, secret string, transcript []byte) []byte {
	secretHash := sha256.Sum256([]byte(secret))
	encoded, err := proofFromSecretHash(purpose, secretHash[:], transcript)
	if err != nil {
		return nil
	}
	return encoded
}

func proofFromSecretHash(purpose string, secretHash []byte, transcript []byte) ([]byte, error) {
	if len(secretHash) != sha256.Size {
		return nil, ErrHandshakeFailed
	}
	transcriptHash := sha256.Sum256(transcript)
	material := append([]byte(purpose+"|"), secretHash...)
	material = append(material, '|')
	material = append(material, transcriptHash[:]...)
	sum := sha256.Sum256(material)
	return []byte(base64.RawURLEncoding.EncodeToString(sum[:])), nil
}

func appendBytes(out []byte, value []byte) ([]byte, error) {
	if len(value) > math.MaxUint16 {
		return nil, fmt.Errorf("%w: bytes exceed uint16 length", ErrInvalidAADField)
	}
	out = appendUint16(out, uint16(len(value)))
	return append(out, value...), nil
}

func appendInt(out []byte, value int) []byte {
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], uint32(value))
	return append(out, encoded[:]...)
}

func appendBool(out []byte, value bool) []byte {
	if value {
		return append(out, 1)
	}
	return append(out, 0)
}
