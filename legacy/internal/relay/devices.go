package relay

import (
	"strings"
	"time"
)

type DeviceInfo struct {
	ClientID   string
	Name       string
	CreatedAt  time.Time
	LastSeenAt time.Time
	Connected  bool
	Revoked    bool
}

func (s *Server) Devices(sessionID string) []DeviceInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.sessions[strings.TrimSpace(sessionID)]
	if state == nil {
		return nil
	}
	out := make([]DeviceInfo, 0, len(state.devices))
	for _, device := range state.devices {
		out = append(out, device.info(state.clientID, state.client != nil))
	}
	return out
}

func (s *Server) RevokeDevice(sessionID string, clientID string) bool {
	s.mu.Lock()
	state := s.sessions[strings.TrimSpace(sessionID)]
	if state == nil {
		s.mu.Unlock()
		return false
	}
	device := state.devices[strings.TrimSpace(clientID)]
	if device == nil {
		s.mu.Unlock()
		return false
	}
	device.Revoked = true
	connected := state.clientID == device.ClientID
	client := state.client
	if connected {
		state.client = nil
		state.clientID = ""
	}
	if err := s.saveStateLocked(); err != nil {
		if connected {
			state.client = client
			state.clientID = device.ClientID
		}
		device.Revoked = false
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()
	if connected && client != nil {
		_ = client.Close()
	}
	return true
}

func (s *Server) RotateSessionCredentials(sessionID string) bool {
	s.mu.Lock()
	state := s.sessions[strings.TrimSpace(sessionID)]
	if state == nil {
		s.mu.Unlock()
		return false
	}
	client := state.client
	state.client = nil
	state.clientID = ""
	state.clientReconnectHash = ""
	state.devices = map[string]*deviceState{}
	state.pairingHash = ""
	state.pairingConsumed = true
	if err := s.saveStateLocked(); err != nil {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
	return true
}

func (s *Server) rememberDeviceLocked(state *sessionState, clientID string, name string, reconnectHash string) {
	if state.devices == nil {
		state.devices = map[string]*deviceState{}
	}
	now := time.Now().UTC()
	normalizedID := strings.TrimSpace(clientID)
	device := state.devices[normalizedID]
	if device == nil {
		device = &deviceState{
			ClientID:  normalizedID,
			Name:      strings.TrimSpace(name),
			CreatedAt: now,
		}
		state.devices[normalizedID] = device
	}
	if strings.TrimSpace(name) != "" {
		device.Name = strings.TrimSpace(name)
	}
	device.ReconnectHash = reconnectHash
	device.LastSeenAt = now
	device.Revoked = false
}

func (d *deviceState) info(activeClientID string, activeConnected bool) DeviceInfo {
	return DeviceInfo{
		ClientID:   d.ClientID,
		Name:       d.Name,
		CreatedAt:  d.CreatedAt,
		LastSeenAt: d.LastSeenAt,
		Connected:  activeConnected && d.ClientID == activeClientID,
		Revoked:    d.Revoked,
	}
}
