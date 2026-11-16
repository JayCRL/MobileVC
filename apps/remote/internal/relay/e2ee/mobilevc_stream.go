package e2ee

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const (
	MobileVCStreamID   uint64 = 1
	MobileVCStreamType        = "mobilevc.ws"
)

type RelayForwardFrame struct {
	Type            string `json:"type"`
	Version         int    `json:"version"`
	SessionID       string `json:"sessionId"`
	ClientID        string `json:"clientId"`
	Direction       string `json:"direction"`
	MessageID       string `json:"messageId"`
	ContentType     string `json:"contentType"`
	Encryption      string `json:"encryption"`
	PayloadEncoding string `json:"payloadEncoding"`
	Payload         string `json:"payload"`
	StreamID        uint64 `json:"streamId"`
	Counter         uint64 `json:"counter"`
	HandshakeID     string `json:"handshakeId"`
}

type MobileVCStreamCodec struct {
	SessionID   string
	ClientID    string
	HandshakeID string
	SendKey     []byte
	ReceiveKey  []byte
	SendDir     string
	ReceiveDir  string
	mu          sync.Mutex
	sendCounter map[uint64]uint64
	seen        map[uint64]map[uint64]struct{}
}

func NewMobileVCStreamCodec(sessionID, clientID, handshakeID string, sendKey, receiveKey []byte, sendDir string) (*MobileVCStreamCodec, error) {
	receiveDir, err := oppositeDirection(sendDir)
	if err != nil {
		return nil, err
	}
	if len(sendKey) != KeyLength || len(receiveKey) != KeyLength {
		return nil, ErrInvalidLength
	}
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(clientID) == "" || strings.TrimSpace(handshakeID) == "" {
		return nil, errors.New("mobilevc e2ee stream session, client, and handshake ids are required")
	}
	return &MobileVCStreamCodec{
		SessionID: sessionID, ClientID: clientID, HandshakeID: handshakeID,
		SendKey: append([]byte(nil), sendKey...), ReceiveKey: append([]byte(nil), receiveKey...),
		SendDir: sendDir, ReceiveDir: receiveDir,
		sendCounter: map[uint64]uint64{}, seen: map[uint64]map[uint64]struct{}{},
	}, nil
}

func NewClientMobileVCStreamCodec(sessionID, clientID, handshakeID string, keys *TrafficKeys) (*MobileVCStreamCodec, error) {
	if keys == nil {
		return nil, errors.New("mobilevc e2ee stream keys are required")
	}
	return NewMobileVCStreamCodec(
		sessionID, clientID, handshakeID,
		keys.ClientToAgent, keys.AgentToClient, DirectionClientToAgent,
	)
}

func NewAgentMobileVCStreamCodec(sessionID, clientID, handshakeID string, keys *TrafficKeys) (*MobileVCStreamCodec, error) {
	if keys == nil {
		return nil, errors.New("mobilevc e2ee stream keys are required")
	}
	return NewMobileVCStreamCodec(
		sessionID, clientID, handshakeID,
		keys.AgentToClient, keys.ClientToAgent, DirectionAgentToClient,
	)
}

func (c *MobileVCStreamCodec) Encode(messageID string, plaintext []byte) (RelayForwardFrame, error) {
	return c.EncodeStream(MobileVCStreamID, messageID, plaintext)
}

func (c *MobileVCStreamCodec) EncodeStream(streamID uint64, messageID string, plaintext []byte) (RelayForwardFrame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if streamID == 0 {
		return RelayForwardFrame{}, errors.New("mobilevc e2ee stream id is required")
	}
	if strings.TrimSpace(messageID) == "" {
		return RelayForwardFrame{}, errors.New("mobilevc e2ee stream message id is required")
	}
	counter := c.sendCounter[streamID]
	ctx := c.frameContext(c.SendDir, streamID, counter)
	sealed, err := Encrypt(c.SendKey, plaintext, ctx)
	if err != nil {
		return RelayForwardFrame{}, err
	}
	c.sendCounter[streamID] = counter + 1
	return RelayForwardFrame{
		Type: "relay.forward", Version: 1, SessionID: c.SessionID, ClientID: c.ClientID,
		Direction: c.SendDir, MessageID: messageID, ContentType: "mobilevc.ws.v1",
		Encryption: Suite, PayloadEncoding: "base64url",
		Payload:  base64.RawURLEncoding.EncodeToString(sealed),
		StreamID: streamID, Counter: counter, HandshakeID: c.HandshakeID,
	}, nil
}

func (c *MobileVCStreamCodec) Decode(frame RelayForwardFrame) ([]byte, error) {
	if frame.StreamID != MobileVCStreamID {
		return nil, errors.New("invalid mobilevc e2ee stream metadata")
	}
	return c.DecodeStream(frame)
}

func (c *MobileVCStreamCodec) DecodeStream(frame RelayForwardFrame) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.validateFrame(frame); err != nil {
		return nil, err
	}
	seenByStream := c.seen[frame.StreamID]
	if seenByStream == nil {
		seenByStream = map[uint64]struct{}{}
		c.seen[frame.StreamID] = seenByStream
	}
	if _, ok := seenByStream[frame.Counter]; ok {
		return nil, fmt.Errorf("e2ee replay detected")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(frame.Payload)
	if err != nil {
		return nil, fmt.Errorf("decode mobilevc e2ee payload: %w", err)
	}
	plaintext, err := Decrypt(c.ReceiveKey, sealed, c.frameContext(c.ReceiveDir, frame.StreamID, frame.Counter))
	if err != nil {
		return nil, err
	}
	seenByStream[frame.Counter] = struct{}{}
	return plaintext, nil
}

func (c *MobileVCStreamCodec) EncodeJSON(messageID string, payload any) (RelayForwardFrame, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return RelayForwardFrame{}, err
	}
	return c.Encode(messageID, raw)
}

func (c *MobileVCStreamCodec) DecodeJSON(frame RelayForwardFrame, out any) error {
	raw, err := c.Decode(frame)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (c *MobileVCStreamCodec) EncodeTunnelFrame(messageID string, frame TunnelFrame) (RelayForwardFrame, error) {
	raw, err := MarshalTunnelFrame(frame)
	if err != nil {
		return RelayForwardFrame{}, err
	}
	return c.EncodeStream(frame.StreamID, messageID, raw)
}

func (c *MobileVCStreamCodec) DecodeTunnelFrame(frame RelayForwardFrame) (TunnelFrame, error) {
	raw, err := c.DecodeStream(frame)
	if err != nil {
		return TunnelFrame{}, err
	}
	return UnmarshalTunnelFrame(raw)
}

func (c *MobileVCStreamCodec) DecodeTunnelFrameForRouting(frame RelayForwardFrame) (TunnelFrame, error) {
	raw, err := c.DecodeStream(frame)
	if err != nil {
		return TunnelFrame{}, err
	}
	return UnmarshalTunnelFrameForRouting(raw)
}

func (c *MobileVCStreamCodec) frameContext(direction string, streamID uint64, counter uint64) FrameContext {
	return FrameContext{
		SessionID: c.SessionID, ClientID: c.ClientID, HandshakeID: c.HandshakeID,
		Direction: direction, StreamID: streamID, Counter: counter,
	}
}

func (c *MobileVCStreamCodec) validateFrame(frame RelayForwardFrame) error {
	if frame.Type != "relay.forward" || frame.Version != 1 {
		return errors.New("invalid mobilevc e2ee relay frame")
	}
	if frame.SessionID != c.SessionID || frame.ClientID != c.ClientID || frame.Direction != c.ReceiveDir {
		return errors.New("invalid mobilevc e2ee routing")
	}
	if frame.ContentType != "mobilevc.ws.v1" || frame.Encryption != Suite || frame.PayloadEncoding != "base64url" {
		return errors.New("invalid mobilevc e2ee content metadata")
	}
	if frame.StreamID == 0 || frame.HandshakeID != c.HandshakeID {
		return errors.New("invalid mobilevc e2ee stream metadata")
	}
	if strings.TrimSpace(frame.MessageID) == "" || strings.TrimSpace(frame.Payload) == "" {
		return errors.New("invalid mobilevc e2ee payload metadata")
	}
	return nil
}

func oppositeDirection(direction string) (string, error) {
	switch direction {
	case DirectionClientToAgent:
		return DirectionAgentToClient, nil
	case DirectionAgentToClient:
		return DirectionClientToAgent, nil
	default:
		return "", ErrInvalidDirection
	}
}
