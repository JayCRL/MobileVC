package e2ee

import (
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const (
	FrameTypeClientE2EEHello = "client.e2ee_hello"
	FrameTypeAgentE2EEHello  = "agent.e2ee_hello"
	FrameTypeClientE2EEProof = "client.e2ee_proof"
	FrameTypeAgentE2EEResult = "agent.e2ee_result"
)

type ClientHelloFrame struct {
	Type                     string         `json:"type"`
	Version                  int            `json:"version"`
	SessionID                string         `json:"sessionId"`
	ClientID                 string         `json:"clientId"`
	HandshakeID              string         `json:"handshakeId"`
	Kind                     string         `json:"kind"`
	Capabilities             *CapabilitySet `json:"capabilities"`
	ClientEphemeralPublicKey string         `json:"clientEphemeralPublicKey"`
	DeviceID                 string         `json:"deviceId,omitempty"`
	DeviceIdentityPublicKey  string         `json:"deviceIdentityPublicKey,omitempty"`
}

type AgentHelloFrame struct {
	Type                   string         `json:"type"`
	Version                int            `json:"version"`
	SessionID              string         `json:"sessionId"`
	ClientID               string         `json:"clientId"`
	HandshakeID            string         `json:"handshakeId"`
	Capabilities           *CapabilitySet `json:"capabilities"`
	NodeEphemeralPublicKey string         `json:"nodeEphemeralPublicKey"`
	NodeIdentityPublicKey  string         `json:"nodeIdentityPublicKey"`
	NodeSignature          string         `json:"nodeSignature"`
}

type ClientProofFrame struct {
	Type            string `json:"type"`
	Version         int    `json:"version"`
	SessionID       string `json:"sessionId"`
	ClientID        string `json:"clientId"`
	HandshakeID     string `json:"handshakeId"`
	Kind            string `json:"kind"`
	PairingProof    string `json:"pairingProof,omitempty"`
	DeviceProof     string `json:"deviceProof,omitempty"`
	DeviceSignature string `json:"deviceSignature,omitempty"`
}

type AgentResultFrame struct {
	Type        string `json:"type"`
	Version     int    `json:"version"`
	SessionID   string `json:"sessionId"`
	ClientID    string `json:"clientId"`
	HandshakeID string `json:"handshakeId"`
	OK          bool   `json:"ok"`
	ErrorCode   string `json:"errorCode,omitempty"`
}

type FrameMaterial struct {
	Capabilities             CapabilitySet
	ClientEphemeralPublicKey []byte
	NodeEphemeralPublicKey   []byte
	NodeIdentityPublicKey    []byte
	DeviceIdentityPublicKey  []byte
	NodeSignature            []byte
	PairingProof             []byte
	DeviceProof              []byte
	DeviceSignature          []byte
}

func EncodeFrameBytes(value []byte) string {
	return base64.RawURLEncoding.EncodeToString(value)
}

func ValidateClientHelloFrame(frame ClientHelloFrame) (*FrameMaterial, error) {
	if err := validateFrameHeader(frame.Type, FrameTypeClientE2EEHello, frame.Version); err != nil {
		return nil, err
	}
	if err := validateFrameIDs(frame.SessionID, frame.ClientID, frame.HandshakeID); err != nil {
		return nil, err
	}
	if frame.Kind != HandshakeKindPairing && frame.Kind != HandshakeKindReconnect {
		return nil, fmt.Errorf("%w: invalid handshake kind", ErrHandshakeFailed)
	}
	capabilities, err := requiredCapabilities(frame.Capabilities)
	if err != nil {
		return nil, err
	}
	clientKey, err := decodeRequiredPublicKey("clientEphemeralPublicKey", frame.ClientEphemeralPublicKey)
	if err != nil {
		return nil, err
	}
	material := &FrameMaterial{
		Capabilities:             capabilities,
		ClientEphemeralPublicKey: clientKey,
	}
	if frame.Kind == HandshakeKindReconnect {
		if strings.TrimSpace(frame.DeviceID) == "" {
			return nil, fmt.Errorf("%w: device id is required", ErrHandshakeFailed)
		}
		deviceKey, err := decodeRequiredPublicKey("deviceIdentityPublicKey", frame.DeviceIdentityPublicKey)
		if err != nil {
			return nil, err
		}
		material.DeviceIdentityPublicKey = deviceKey
		return material, nil
	}
	if strings.TrimSpace(frame.DeviceID) != "" || strings.TrimSpace(frame.DeviceIdentityPublicKey) != "" {
		return nil, fmt.Errorf("%w: pairing hello has unexpected device identity", ErrHandshakeFailed)
	}
	return material, nil
}

func ValidateAgentHelloFrame(frame AgentHelloFrame) (*FrameMaterial, error) {
	if err := validateFrameHeader(frame.Type, FrameTypeAgentE2EEHello, frame.Version); err != nil {
		return nil, err
	}
	if err := validateFrameIDs(frame.SessionID, frame.ClientID, frame.HandshakeID); err != nil {
		return nil, err
	}
	capabilities, err := requiredCapabilities(frame.Capabilities)
	if err != nil {
		return nil, err
	}
	nodeEphemeral, err := decodeRequiredPublicKey("nodeEphemeralPublicKey", frame.NodeEphemeralPublicKey)
	if err != nil {
		return nil, err
	}
	nodeIdentity, err := decodeRequiredPublicKey("nodeIdentityPublicKey", frame.NodeIdentityPublicKey)
	if err != nil {
		return nil, err
	}
	signature, err := decodeRequiredBytes("nodeSignature", frame.NodeSignature)
	if err != nil {
		return nil, err
	}
	return &FrameMaterial{
		Capabilities:           capabilities,
		NodeEphemeralPublicKey: nodeEphemeral,
		NodeIdentityPublicKey:  nodeIdentity,
		NodeSignature:          signature,
	}, nil
}

func ValidateClientProofFrame(frame ClientProofFrame) (*FrameMaterial, error) {
	if err := validateFrameHeader(frame.Type, FrameTypeClientE2EEProof, frame.Version); err != nil {
		return nil, err
	}
	if err := validateFrameIDs(frame.SessionID, frame.ClientID, frame.HandshakeID); err != nil {
		return nil, err
	}
	if frame.Kind != HandshakeKindPairing && frame.Kind != HandshakeKindReconnect {
		return nil, fmt.Errorf("%w: invalid handshake kind", ErrHandshakeFailed)
	}
	material := &FrameMaterial{}
	if frame.Kind == HandshakeKindPairing {
		proof, err := decodeRequiredBytes("pairingProof", frame.PairingProof)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(frame.DeviceProof) != "" || strings.TrimSpace(frame.DeviceSignature) != "" {
			return nil, fmt.Errorf("%w: pairing proof has unexpected device fields", ErrHandshakeFailed)
		}
		material.PairingProof = proof
		return material, nil
	}
	proof, err := decodeRequiredBytes("deviceProof", frame.DeviceProof)
	if err != nil {
		return nil, err
	}
	signature, err := decodeRequiredBytes("deviceSignature", frame.DeviceSignature)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(frame.PairingProof) != "" {
		return nil, fmt.Errorf("%w: reconnect proof has unexpected pairing proof", ErrHandshakeFailed)
	}
	material.DeviceProof = proof
	material.DeviceSignature = signature
	return material, nil
}

func ValidateAgentResultFrame(frame AgentResultFrame) error {
	if err := validateFrameHeader(frame.Type, FrameTypeAgentE2EEResult, frame.Version); err != nil {
		return err
	}
	if err := validateFrameIDs(frame.SessionID, frame.ClientID, frame.HandshakeID); err != nil {
		return err
	}
	if frame.OK && strings.TrimSpace(frame.ErrorCode) != "" {
		return fmt.Errorf("%w: successful result has error code", ErrHandshakeFailed)
	}
	if !frame.OK && strings.TrimSpace(frame.ErrorCode) == "" {
		return fmt.Errorf("%w: failed result requires error code", ErrHandshakeFailed)
	}
	return nil
}

func requiredCapabilities(capabilities *CapabilitySet) (CapabilitySet, error) {
	if capabilities == nil {
		return CapabilitySet{}, fmt.Errorf("%w: missing capabilities", ErrHandshakeFailed)
	}
	if err := ValidateProductionCapabilities(*capabilities); err != nil {
		return CapabilitySet{}, err
	}
	return *capabilities, nil
}

func validateFrameHeader(actualType, expectedType string, version int) error {
	if actualType != expectedType || version != RelayProtocolVersion {
		return fmt.Errorf("%w: invalid handshake frame header", ErrHandshakeFailed)
	}
	return nil
}

func validateFrameIDs(sessionID, clientID, handshakeID string) error {
	if strings.TrimSpace(sessionID) == "" ||
		strings.TrimSpace(clientID) == "" ||
		strings.TrimSpace(handshakeID) == "" {
		return fmt.Errorf("%w: missing handshake routing id", ErrHandshakeFailed)
	}
	return nil
}

func decodeRequiredPublicKey(name string, encoded string) ([]byte, error) {
	value, err := decodeRequiredBytes(name, encoded)
	if err != nil {
		return nil, err
	}
	if err := validateP256PublicKey(value); err != nil {
		return nil, fmt.Errorf("%w: invalid %s", ErrHandshakeFailed, name)
	}
	return value, nil
}

func decodeRequiredBytes(name string, encoded string) ([]byte, error) {
	if strings.TrimSpace(encoded) == "" {
		return nil, fmt.Errorf("%w: missing %s", ErrHandshakeFailed, name)
	}
	value, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid %s encoding", ErrHandshakeFailed, name)
	}
	return value, nil
}

func validateP256PublicKey(value []byte) error {
	if len(value) == 0 {
		return errors.New("empty public key")
	}
	_, err := ecdh.P256().NewPublicKey(value)
	return err
}
