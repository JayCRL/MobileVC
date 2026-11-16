package relay

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"mobilevc/internal/logx"
	"mobilevc/internal/relay/e2ee"
)

func (s *Server) forwardLoop(peer *peerConn, sessionID string, direction string) {
	for {
		raw, err := peer.ReadRawFrame(s.maxPostAuthFrameBytes())
		if err != nil {
			if errors.Is(err, errFrameTooLarge) {
				writeError(peer, CodeFrameTooLarge)
			}
			return
		}
		if err := s.dispatchPostAuthFrame(peer, sessionID, direction, raw); err != nil {
			if errors.Is(err, errPayloadTooLarge) {
				continue
			}
			return
		}
	}
}

func (s *Server) maxPostAuthFrameBytes() int64 {
	payloadLimit := int64(s.cfg.MaxPayloadBytes) * 2
	if payloadLimit < s.cfg.MaxControlFrameBytes {
		return s.cfg.MaxControlFrameBytes
	}
	return payloadLimit
}

func (s *Server) dispatchPostAuthFrame(peer *peerConn, sessionID string, direction string, raw []byte) error {
	frame, _, err := decodeControlFrame(raw)
	if err != nil {
		writeError(peer, CodeProtocolError)
		return err
	}
	if frame.Type == TypeRelayForward {
		var env ForwardEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			writeError(peer, CodeProtocolError)
			return err
		}
		return s.forwardEnvelope(peer, sessionID, direction, env)
	}
	if frame.Type == TypeRelayPong {
		return nil
	}
	if isE2EEHandshakeFrameType(frame.Type) {
		return s.forwardE2EEHandshakeFrame(peer, sessionID, direction, frame.Type, raw)
	}
	writeError(peer, CodeProtocolError)
	return errors.New("unsupported post-auth relay frame")
}

func isE2EEHandshakeFrameType(frameType string) bool {
	switch frameType {
	case TypeClientE2EEHello, TypeAgentE2EEHello, TypeClientE2EEProof, TypeAgentE2EEResult:
		return true
	default:
		return false
	}
}

func (s *Server) forwardE2EEHandshakeFrame(peer *peerConn, sessionID string, direction string, frameType string, raw []byte) error {
	frame, err := decodeE2EEHandshakeFrame(frameType, raw)
	if err != nil {
		writeError(peer, CodeE2EEHandshakeFailed)
		return err
	}
	if err := s.validateE2EEHandshakeRouting(peer, sessionID, direction, frame); err != nil {
		writeError(peer, CodeProtocolError)
		return err
	}
	target := s.targetConn(sessionID, direction)
	if target == nil {
		writeError(peer, s.targetUnavailableCode(sessionID, direction))
		return errors.New("target unavailable")
	}
	if err := target.Enqueue(frame); err != nil {
		writeError(peer, CodeQueueFull)
		return err
	}
	return nil
}

func decodeE2EEHandshakeFrame(frameType string, raw []byte) (any, error) {
	switch frameType {
	case TypeClientE2EEHello:
		var frame E2EEClientHelloFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			return nil, err
		}
		_, err := e2ee.ValidateClientHelloFrame(frame)
		return frame, err
	case TypeAgentE2EEHello:
		var frame E2EEAgentHelloFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			return nil, err
		}
		_, err := e2ee.ValidateAgentHelloFrame(frame)
		return frame, err
	case TypeClientE2EEProof:
		var frame E2EEClientProofFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			return nil, err
		}
		_, err := e2ee.ValidateClientProofFrame(frame)
		return frame, err
	case TypeAgentE2EEResult:
		var frame E2EEAgentResultFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			return nil, err
		}
		return frame, e2ee.ValidateAgentResultFrame(frame)
	default:
		return nil, errors.New("unsupported e2ee handshake frame")
	}
}

func (s *Server) validateE2EEHandshakeRouting(peer *peerConn, sessionID string, direction string, frame any) error {
	var frameSessionID string
	var clientID string
	var expectedRole peerRole
	var expectedDirection string
	switch typed := frame.(type) {
	case E2EEClientHelloFrame:
		frameSessionID, clientID = typed.SessionID, typed.ClientID
		expectedRole, expectedDirection = roleClient, DirectionClientToAgent
	case E2EEClientProofFrame:
		frameSessionID, clientID = typed.SessionID, typed.ClientID
		expectedRole, expectedDirection = roleClient, DirectionClientToAgent
	case E2EEAgentHelloFrame:
		frameSessionID, clientID = typed.SessionID, typed.ClientID
		expectedRole, expectedDirection = roleAgent, DirectionAgentToClient
	case E2EEAgentResultFrame:
		frameSessionID, clientID = typed.SessionID, typed.ClientID
		expectedRole, expectedDirection = roleAgent, DirectionAgentToClient
	default:
		return errors.New("unsupported e2ee handshake routing")
	}
	if peer.role != expectedRole || direction != expectedDirection || frameSessionID != sessionID {
		return errors.New("invalid e2ee handshake routing")
	}
	if !s.forwardClientIDMatches(peer, sessionID, clientID) {
		return errors.New("invalid e2ee handshake client id")
	}
	return nil
}

func (s *Server) forwardEnvelope(peer *peerConn, sessionID string, direction string, env ForwardEnvelope) error {
	var err error
	env, err = s.normalizeForward(peer, sessionID, direction, env)
	if err != nil {
		logForwardReject(peer, sessionID, direction, env, "normalize", err)
		writeError(peer, CodeProtocolError)
		return err
	}
	if err := validateForwardMetadata(env, sessionID, direction); err != nil {
		logForwardReject(peer, sessionID, direction, env, "metadata", err)
		writeError(peer, CodeProtocolError)
		return err
	}
	if err := s.forwardSecurityPolicy().Validate(env); err != nil {
		logForwardReject(peer, sessionID, direction, env, "security", err)
		writeError(peer, forwardValidationErrorCode(err))
		return err
	}
	if size, err := s.validatePayloadSize(env.Payload); err != nil {
		if errors.Is(err, errInvalidPayloadEncoding) {
			logx.Warn("relay", "invalid payload encoding: sessionID=%s clientID=%s direction=%s remote=%s err=%v", sessionID, env.ClientID, direction, peer.remote, err)
			writeError(peer, CodeProtocolError)
			return err
		}
		logx.Warn("relay", "payload too large: sessionID=%s clientID=%s direction=%s decodedBytes=%d maxBytes=%d remote=%s", sessionID, env.ClientID, direction, size, s.cfg.MaxPayloadBytes, peer.remote)
		writeErrorFrame(peer, payloadTooLargeFrame(size, s.cfg.MaxPayloadBytes))
		return err
	}
	target := s.targetConn(sessionID, direction)
	if target == nil {
		writeError(peer, s.targetUnavailableCode(sessionID, direction))
		return nil
	}
	if !s.forwardClientIDMatches(peer, sessionID, env.ClientID) {
		logForwardReject(peer, sessionID, direction, env, "client_id", errors.New("invalid forward client id"))
		writeError(peer, CodeProtocolError)
		return errors.New("invalid forward client id")
	}
	if err := target.Enqueue(env); err != nil {
		writeError(peer, CodeQueueFull)
		return nil
	}
	return nil
}

func logForwardReject(peer *peerConn, sessionID string, direction string, env ForwardEnvelope, stage string, err error) {
	logx.Warn(
		"relay",
		"reject relay forward: role=%s remote=%s sessionID=%s direction=%s envSessionID=%s clientID=%s envDirection=%s encryption=%s streamID=%d handshakeIDSet=%t counter=%d stage=%s err=%v",
		peer.role,
		peer.remote,
		sessionID,
		direction,
		env.SessionID,
		env.ClientID,
		env.Direction,
		env.Encryption,
		env.StreamID,
		strings.TrimSpace(env.HandshakeID) != "",
		env.Counter,
		stage,
		err,
	)
}

func (s *Server) normalizeForward(peer *peerConn, sessionID string, direction string, env ForwardEnvelope) (ForwardEnvelope, error) {
	if peer.role != roleAgent || direction != DirectionAgentToClient || strings.TrimSpace(env.ClientID) != "" {
		return env, nil
	}
	clientID, err := s.activeClientID(peer, sessionID)
	if err != nil {
		return env, err
	}
	env.ClientID = clientID
	return env, nil
}

func (s *Server) activeClientID(peer *peerConn, sessionID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.sessions[sessionID]
	if state == nil || state.agent != peer || state.client == nil {
		return "", errors.New("missing active relay client")
	}
	return state.clientID, nil
}

func (s *Server) forwardSecurityPolicy() ForwardSecurityPolicy {
	return ForwardSecurityPolicy{
		RequireE2EE:       s.cfg.RequireE2EE,
		PlaintextTestMode: s.cfg.PlaintextTestMode,
	}
}

func forwardValidationErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrE2EERequired):
		return CodeE2EERequired
	case errors.Is(err, ErrE2EEUnsupported):
		return CodeE2EEUnsupported
	default:
		return CodeProtocolError
	}
}

func validateForwardMetadata(env ForwardEnvelope, sessionID string, direction string) error {
	if env.Type != TypeRelayForward || env.Version != Version {
		return errors.New("invalid forward frame")
	}
	if env.SessionID != sessionID || env.Direction != direction {
		return errors.New("invalid forward routing")
	}
	if env.ContentType != ContentTypeMobileVC {
		return errors.New("invalid forward content")
	}
	if env.PayloadEncoding != PayloadBase64URL || strings.TrimSpace(env.MessageID) == "" {
		return errors.New("invalid forward payload metadata")
	}
	return nil
}

func (s *Server) forwardClientIDMatches(peer *peerConn, sessionID string, clientID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.sessions[sessionID]
	if state == nil || strings.TrimSpace(clientID) == "" {
		return false
	}
	if peer.role == roleClient {
		return state.client == peer && state.clientID == clientID
	}
	return state.agent == peer && state.clientID == clientID
}

var (
	errInvalidPayloadEncoding = errors.New("invalid payload encoding")
	errPayloadTooLarge        = errors.New("payload too large")
)

func (s *Server) validatePayloadSize(payload string) (int, error) {
	decoded, err := DecodePayloadBase64URL(payload)
	if err != nil {
		return 0, errInvalidPayloadEncoding
	}
	if len(decoded) > s.cfg.MaxPayloadBytes {
		return len(decoded), errPayloadTooLarge
	}
	return len(decoded), nil
}

// DecodePayloadBase64URL accepts both padded and unpadded base64url payloads.
func DecodePayloadBase64URL(payload string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err == nil {
		return decoded, nil
	}
	return base64.URLEncoding.DecodeString(payload)
}

func (s *Server) targetConn(sessionID string, direction string) *peerConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.sessions[sessionID]
	if state == nil {
		return nil
	}
	if direction == DirectionClientToAgent {
		return state.agent
	}
	return state.client
}

func (s *Server) targetUnavailableCode(sessionID string, direction string) string {
	if direction == DirectionClientToAgent && s.agentTemporarilyDisconnected(sessionID) {
		return CodeAgentDisconnected
	}
	return CodeTargetUnavailable
}

func (s *Server) agentTemporarilyDisconnected(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.sessions[sessionID]
	return state.agentDisconnectedWithinGrace(s.cfg.AgentGracePeriod) ||
		(state != nil && state.agent == nil && state.hasReconnectableDevices())
}

func (s *Server) markAgentDisconnected(sessionID string, peer *peerConn) {
	s.mu.Lock()
	state := s.sessions[sessionID]
	if state == nil || state.agent != peer {
		s.mu.Unlock()
		return
	}
	state.agent = nil
	state.agentDisconnectedAt = time.Now()
	disconnectedAt := state.agentDisconnectedAt
	_ = s.saveStateLocked()
	s.mu.Unlock()
	go s.expireDisconnectedAgent(sessionID, disconnectedAt)
}

func (s *Server) expireDisconnectedAgent(sessionID string, disconnectedAt time.Time) {
	time.Sleep(s.cfg.AgentGracePeriod)
	client := s.removeExpiredDisconnectedAgent(sessionID, disconnectedAt)
	if client != nil {
		_ = client.Close()
	}
}

func (s *Server) removeExpiredDisconnectedAgent(sessionID string, disconnectedAt time.Time) *peerConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.sessions[sessionID]
	if state == nil || state.agent != nil || !state.agentDisconnectedAt.Equal(disconnectedAt) {
		return nil
	}
	if state.hasReconnectableDevices() {
		client := state.client
		state.client = nil
		state.clientID = ""
		_ = s.saveStateLocked()
		return client
	}
	delete(s.sessions, sessionID)
	_ = s.saveStateLocked()
	return state.client
}

func (s *Server) markClientDisconnected(sessionID string, peer *peerConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.sessions[sessionID]
	if state == nil || state.client != peer {
		return
	}
	state.client = nil
	state.clientID = ""
	_ = s.saveStateLocked()
}

func writeError(peer *peerConn, code string) {
	logx.Warn("relay", "sending relay error: role=%s remote=%s code=%s", peer.role, peer.remote, code)
	_ = peer.WriteJSON(NewErrorFrame(code))
}

func writeErrorFrame(peer *peerConn, frame ErrorFrame) {
	logx.Warn("relay", "sending relay error: role=%s remote=%s code=%s", peer.role, peer.remote, frame.Code)
	_ = peer.WriteJSON(frame)
}

func payloadTooLargeFrame(decodedBytes int, maxBytes int) ErrorFrame {
	frame := NewErrorFrame(CodePayloadTooLarge)
	frame.DecodedBytes = decodedBytes
	frame.MaxBytes = maxBytes
	return frame
}
