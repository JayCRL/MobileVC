package relay

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mobilevc/internal/relay/e2ee"
)

const relayStateVersion = 1

type stateStore struct {
	path string
}

type persistedRelayState struct {
	Version  int                              `json:"version"`
	Sessions map[string]persistedSessionState `json:"sessions"`
}

type persistedSessionState struct {
	ID                 string                 `json:"id"`
	PairingHash        string                 `json:"pairingHash,omitempty"`
	AgentReconnectHash string                 `json:"agentReconnectHash"`
	PairingExpiresAt   string                 `json:"pairingExpiresAt"`
	Capabilities       e2ee.CapabilitySet     `json:"capabilities"`
	PairingConsumed    bool                   `json:"pairingConsumed"`
	Devices            []persistedDeviceState `json:"devices,omitempty"`
}

type persistedDeviceState struct {
	ClientID      string `json:"clientId"`
	Name          string `json:"name"`
	ReconnectHash string `json:"reconnectHash"`
	CreatedAt     string `json:"createdAt"`
	LastSeenAt    string `json:"lastSeenAt"`
	Revoked       bool   `json:"revoked"`
}

func newStateStore(path string) *stateStore {
	normalized := strings.TrimSpace(path)
	if normalized == "" {
		return nil
	}
	return &stateStore{path: normalized}
}

func (s *stateStore) load() (map[string]*sessionState, error) {
	if s == nil {
		return map[string]*sessionState{}, nil
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]*sessionState{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read relay state: %w", err)
	}
	var file persistedRelayState
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse relay state: %w", err)
	}
	if file.Version != relayStateVersion {
		return nil, fmt.Errorf("unsupported relay state version")
	}
	sessions := make(map[string]*sessionState, len(file.Sessions))
	for id, encoded := range file.Sessions {
		state, err := encoded.toSessionState()
		if err != nil {
			return nil, err
		}
		if id != state.id {
			return nil, fmt.Errorf("relay state session id mismatch")
		}
		sessions[id] = state
	}
	return sessions, nil
}

func (s *stateStore) save(sessions map[string]*sessionState) error {
	if s == nil {
		return nil
	}
	file := persistedRelayState{
		Version:  relayStateVersion,
		Sessions: map[string]persistedSessionState{},
	}
	for id, state := range sessions {
		if state == nil {
			continue
		}
		file.Sessions[id] = persistedSessionFromRuntime(state)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create relay state directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("secure relay state directory: %w", err)
	}
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal relay state: %w", err)
	}
	if err := writeFileAtomic(s.path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write relay state: %w", err)
	}
	return nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	return syncDirectory(dir)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func persistedSessionFromRuntime(state *sessionState) persistedSessionState {
	devices := make([]persistedDeviceState, 0, len(state.devices))
	for _, device := range state.devices {
		if device == nil {
			continue
		}
		devices = append(devices, persistedDeviceFromRuntime(device))
	}
	return persistedSessionState{
		ID:                 state.id,
		PairingHash:        state.pairingHash,
		AgentReconnectHash: state.agentReconnectHash,
		PairingExpiresAt:   state.pairingExpiresAt.UTC().Format(time.RFC3339Nano),
		Capabilities:       state.capabilities,
		PairingConsumed:    state.pairingConsumed,
		Devices:            devices,
	}
}

func persistedDeviceFromRuntime(device *deviceState) persistedDeviceState {
	return persistedDeviceState{
		ClientID:      device.ClientID,
		Name:          device.Name,
		ReconnectHash: device.ReconnectHash,
		CreatedAt:     device.CreatedAt.UTC().Format(time.RFC3339Nano),
		LastSeenAt:    device.LastSeenAt.UTC().Format(time.RFC3339Nano),
		Revoked:       device.Revoked,
	}
}

func (p persistedSessionState) toSessionState() (*sessionState, error) {
	id := strings.TrimSpace(p.ID)
	if id == "" || strings.TrimSpace(p.AgentReconnectHash) == "" {
		return nil, fmt.Errorf("invalid relay state session")
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(p.PairingExpiresAt))
	if err != nil {
		return nil, fmt.Errorf("parse relay state pairing expiry: %w", err)
	}
	state := &sessionState{
		id:                   id,
		pairingHash:          strings.TrimSpace(p.PairingHash),
		agentReconnectHash:   strings.TrimSpace(p.AgentReconnectHash),
		pairingExpiresAt:     expiresAt,
		capabilities:         p.Capabilities,
		pairingConsumed:      p.PairingConsumed,
		pairFailuresByRemote: map[string]int{},
		devices:              map[string]*deviceState{},
	}
	for _, encoded := range p.Devices {
		device, err := encoded.toDeviceState()
		if err != nil {
			return nil, err
		}
		state.devices[device.ClientID] = device
	}
	return state, nil
}

func (p persistedDeviceState) toDeviceState() (*deviceState, error) {
	clientID := strings.TrimSpace(p.ClientID)
	if clientID == "" || strings.TrimSpace(p.ReconnectHash) == "" {
		return nil, fmt.Errorf("invalid relay state device")
	}
	createdAt, err := parsePersistedRelayTime(p.CreatedAt, "created")
	if err != nil {
		return nil, err
	}
	lastSeenAt, err := parsePersistedRelayTime(p.LastSeenAt, "last seen")
	if err != nil {
		return nil, err
	}
	return &deviceState{
		ClientID:      clientID,
		Name:          strings.TrimSpace(p.Name),
		ReconnectHash: strings.TrimSpace(p.ReconnectHash),
		CreatedAt:     createdAt,
		LastSeenAt:    lastSeenAt,
		Revoked:       p.Revoked,
	}, nil
}

func parsePersistedRelayTime(raw string, label string) (time.Time, error) {
	value, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, fmt.Errorf("parse relay state %s time: %w", label, err)
	}
	return value, nil
}
