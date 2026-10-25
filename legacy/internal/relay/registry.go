package relay

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"mobilevc/internal/relay/e2ee"
)

func (s *Server) registerAgent(peer *peerConn, raw []byte) (string, error) {
	state, err := newSessionState(peer, raw, s.cfg)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	if s.sessions[state.id] != nil {
		s.mu.Unlock()
		return "", errors.New("session already exists")
	}
	if len(s.sessions) >= s.cfg.MaxSessions {
		s.mu.Unlock()
		return "", errors.New("session capacity reached")
	}
	s.sessions[state.id] = state
	if err := s.saveStateLocked(); err != nil {
		delete(s.sessions, state.id)
		s.mu.Unlock()
		return "", err
	}
	s.mu.Unlock()
	return state.id, writeRegistered(peer, state.id)
}

func newSessionState(peer *peerConn, raw []byte, cfg Config) (*sessionState, error) {
	var frame AgentRegisterFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		return nil, err
	}
	capabilities, err := requiredAgentCapabilities(frame.Capabilities)
	if err != nil {
		return nil, err
	}
	if err := validateAgentCapabilities(capabilities, cfg); err != nil {
		return nil, err
	}
	state := &sessionState{
		id:                   strings.TrimSpace(frame.SessionID),
		pairingHash:          strings.TrimSpace(frame.PairingSecretHash),
		agentReconnectHash:   strings.TrimSpace(frame.AgentReconnectSecretHash),
		pairingExpiresAt:     time.Unix(frame.PairingExpiresAt, 0),
		agent:                peer,
		capabilities:         capabilities,
		pairFailuresByRemote: map[string]int{},
		devices:              map[string]*deviceState{},
	}
	peer.deviceName = strings.TrimSpace(frame.AgentName)
	peer.system = inferSystem(peer.userAgent, strings.TrimSpace(frame.AgentSystem+" "+frame.AgentName))
	if state.id == "" || state.pairingHash == "" || state.agentReconnectHash == "" {
		return nil, errors.New("missing agent registration fields")
	}
	return state, nil
}

func requiredAgentCapabilities(capabilities *e2ee.CapabilitySet) (e2ee.CapabilitySet, error) {
	if capabilities == nil {
		return e2ee.CapabilitySet{}, errors.New("missing agent capabilities")
	}
	return *capabilities, nil
}

func validateAgentCapabilities(capabilities e2ee.CapabilitySet, cfg Config) error {
	if cfg.RequireE2EE && !cfg.PlaintextTestMode {
		if err := e2ee.ValidateProductionCapabilities(capabilities); err != nil {
			return newCodeError(CodeE2EEUnsupported, fmt.Sprintf("agent e2ee capability rejected: %v", err))
		}
		return nil
	}
	if cfg.PlaintextTestMode {
		if err := e2ee.ValidatePlaintextTestCapabilities(capabilities); err != nil {
			return newCodeError(CodeE2EEUnsupported, fmt.Sprintf("agent plaintext capability rejected: %v", err))
		}
		return nil
	}
	return errors.New("invalid relay e2ee mode")
}

func (s *Server) reconnectAgent(peer *peerConn, raw []byte) (string, error) {
	var frame AgentReconnectFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		return "", err
	}
	s.mu.Lock()
	state := s.sessions[strings.TrimSpace(frame.SessionID)]
	if !s.canReconnectAgent(state, frame.AgentReconnectSecret) {
		s.mu.Unlock()
		return "", errors.New("agent reconnect rejected")
	}
	disconnectedAt := state.agentDisconnectedAt
	staleAgent := state.agent
	staleClient := state.client
	staleClientID := state.clientID
	state.agent = peer
	peer.deviceName = strings.TrimSpace(frame.AgentName)
	peer.system = inferSystem(peer.userAgent, strings.TrimSpace(frame.AgentSystem+" "+frame.AgentName))
	state.agentDisconnectedAt = time.Time{}
	state.client = nil
	state.clientID = ""
	sessionID := state.id
	if err := s.saveStateLocked(); err != nil {
		state.agent = nil
		state.agentDisconnectedAt = disconnectedAt
		state.client = staleClient
		state.clientID = staleClientID
		s.mu.Unlock()
		return "", err
	}
	s.mu.Unlock()
	if staleAgent != nil && staleAgent != peer {
		_ = staleAgent.Close()
	}
	if staleClient != nil {
		_ = staleClient.Close()
	}
	if err := writeRegistered(peer, sessionID); err != nil {
		s.rollbackAgentReconnect(sessionID, peer, disconnectedAt)
		return "", err
	}
	return sessionID, nil
}

func (s *Server) canReconnectAgent(state *sessionState, secret string) bool {
	if state == nil {
		return false
	}
	if !state.agentDisconnectedAt.IsZero() &&
		time.Since(state.agentDisconnectedAt) > s.cfg.AgentGracePeriod &&
		!state.hasReconnectableDevices() {
		delete(s.sessions, state.id)
		return false
	}
	return SecretHashMatches(state.agentReconnectHash, secret)
}

func (s *Server) pairClient(peer *peerConn, raw []byte, remote string) (string, string, error) {
	var frame ClientPairFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		return "", "", err
	}
	s.mu.Lock()
	state := s.sessions[strings.TrimSpace(frame.SessionID)]
	if !s.canPair(state, remote) || !SecretHashMatches(state.pairingHash, frame.PairingSecret) {
		s.recordPairingFailure(state, remote)
		s.mu.Unlock()
		return "", "", errors.New("pairing rejected")
	}
	clientID := "rc_" + uuid.NewString()
	clientReconnectSecret, err := NewSecret()
	if err != nil {
		s.mu.Unlock()
		return "", "", err
	}
	sessionID := state.id
	agent := state.agent
	pairingHash := state.pairingHash
	state.client = peer
	state.clientID = clientID
	peer.system = inferSystem(peer.userAgent, frame.DeviceName)
	state.clientReconnectHash = SecretHash(clientReconnectSecret)
	state.pairingHash = ""
	state.pairingConsumed = true
	s.rememberDeviceLocked(state, clientID, frame.DeviceName, state.clientReconnectHash)
	if err := s.saveStateLocked(); err != nil {
		state.client = nil
		state.clientID = ""
		state.clientReconnectHash = ""
		state.pairingHash = pairingHash
		state.pairingConsumed = false
		delete(state.devices, clientID)
		s.mu.Unlock()
		return "", "", err
	}
	s.mu.Unlock()
	if err := writePaired(peer, sessionID, clientID, clientReconnectSecret); err != nil {
		s.rollbackPairing(sessionID, peer, pairingHash)
		return "", "", err
	}
	if err := notifyClientAttached(agent, sessionID, clientID); err != nil {
		s.rollbackPairing(sessionID, peer, pairingHash)
		return "", "", err
	}
	return sessionID, clientID, nil
}

func (s *Server) reconnectClient(peer *peerConn, raw []byte) (string, string, error) {
	var frame ClientReconnectFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		return "", "", err
	}
	s.mu.Lock()
	state := s.sessions[strings.TrimSpace(frame.SessionID)]
	device, err := s.reconnectableDevice(state, frame.ClientID, frame.ClientReconnectSecret)
	if err != nil {
		s.mu.Unlock()
		return "", "", err
	}
	previousClient := state.client
	previousClientID := state.clientID
	state.client = peer
	state.clientID = device.ClientID
	if strings.TrimSpace(frame.DeviceName) != "" {
		device.Name = strings.TrimSpace(frame.DeviceName)
	}
	peer.system = inferSystem(peer.userAgent, device.Name)
	device.LastSeenAt = time.Now().UTC()
	sessionID := state.id
	clientID := device.ClientID
	agent := state.agent
	if err := s.saveStateLocked(); err != nil {
		state.client = previousClient
		state.clientID = previousClientID
		s.mu.Unlock()
		return "", "", err
	}
	s.mu.Unlock()
	if previousClient != nil && previousClient != peer {
		_ = previousClient.Close()
	}
	if err := writePaired(peer, sessionID, clientID, ""); err != nil {
		s.rollbackClientReconnect(sessionID, peer)
		return "", "", err
	}
	if err := notifyClientAttached(agent, sessionID, clientID); err != nil {
		s.rollbackClientReconnect(sessionID, peer)
		return "", "", err
	}
	return sessionID, clientID, nil
}

func (s *Server) canPair(state *sessionState, remote string) bool {
	if state == nil || state.agent == nil || state.client != nil || state.pairingConsumed {
		return false
	}
	if time.Now().After(state.pairingExpiresAt) {
		return false
	}
	return state.pairFailuresByRemote[remote] < s.cfg.MaxPairingFailuresPerIP
}

func (s *Server) canReconnectClient(state *sessionState, clientID string, secret string) bool {
	_, err := s.reconnectableDevice(state, clientID, secret)
	return err == nil
}

func (s *Server) reconnectableDevice(state *sessionState, clientID string, secret string) (*deviceState, error) {
	if state == nil {
		return nil, newCodeError(CodeDeviceUnknown, "client reconnect rejected")
	}
	normalizedID := strings.TrimSpace(clientID)
	if normalizedID == "" {
		return nil, newCodeError(CodeDeviceUnknown, "client reconnect rejected")
	}
	device := state.devices[normalizedID]
	if device == nil {
		return nil, newCodeError(CodeDeviceUnknown, "client reconnect rejected")
	}
	if device.Revoked {
		return nil, newCodeError(CodeDeviceRevoked, "client reconnect rejected")
	}
	if !SecretHashMatches(device.ReconnectHash, secret) {
		return nil, newCodeError(CodeDeviceUnknown, "client reconnect rejected")
	}
	if state.agent == nil {
		if state.agentDisconnectedWithinGrace(s.cfg.AgentGracePeriod) ||
			state.hasReconnectableDevices() {
			return nil, newCodeError(CodeAgentDisconnected, "agent is reconnecting")
		}
		return nil, newCodeError(CodeDeviceUnknown, "client reconnect rejected")
	}
	return device, nil
}

func (s *Server) recordPairingFailure(state *sessionState, remote string) {
	if state != nil {
		state.pairFailuresByRemote[remote]++
	}
}

func (s *Server) rollbackPairing(sessionID string, peer *peerConn, pairingHash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.sessions[sessionID]
	if state == nil || state.client != peer {
		return
	}
	delete(state.devices, state.clientID)
	state.client = nil
	state.clientID = ""
	state.clientReconnectHash = ""
	state.pairingHash = pairingHash
	state.pairingConsumed = false
	_ = s.saveStateLocked()
}

func (s *Server) rollbackClientReconnect(sessionID string, peer *peerConn) {
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

func (s *Server) rollbackAgentReconnect(sessionID string, peer *peerConn, disconnectedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.sessions[sessionID]
	if state == nil || state.agent != peer {
		return
	}
	state.agent = nil
	state.agentDisconnectedAt = disconnectedAt
	_ = s.saveStateLocked()
}

func writeRegistered(peer *peerConn, sessionID string) error {
	return peer.WriteJSON(AgentRegisteredFrame{
		Type: TypeAgentRegistered, Version: Version, SessionID: sessionID,
	})
}

func writePaired(peer *peerConn, sessionID string, clientID string, clientReconnectSecret string) error {
	return peer.WriteJSON(ClientPairedFrame{
		Type:                  TypeClientPaired,
		Version:               Version,
		SessionID:             sessionID,
		ClientID:              clientID,
		ClientReconnectSecret: clientReconnectSecret,
	})
}

func notifyClientAttached(peer *peerConn, sessionID string, clientID string) error {
	return peer.Enqueue(ClientAttachedFrame{
		Type: TypeClientAttached, Version: Version, SessionID: sessionID, ClientID: clientID,
	})
}

func (s *Server) saveStateLocked() error {
	return s.stateStore.save(s.sessions)
}
