package relayclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const agentSessionStateVersion = 1

type agentSessionStore struct {
	path string
}

type agentSessionState struct {
	SessionID       string
	ReconnectSecret string
}

type persistedAgentSessionState struct {
	Version         int    `json:"version"`
	SessionID       string `json:"sessionId"`
	ReconnectSecret string `json:"reconnectSecret"`
}

func newAgentSessionStore(path string) *agentSessionStore {
	normalized := strings.TrimSpace(path)
	if normalized == "" {
		return nil
	}
	return &agentSessionStore{path: normalized}
}

func (s *agentSessionStore) load() (agentSessionState, error) {
	if s == nil {
		return agentSessionState{}, nil
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return agentSessionState{}, nil
	}
	if err != nil {
		return agentSessionState{}, fmt.Errorf("read relay agent session state: %w", err)
	}
	var persisted persistedAgentSessionState
	if err := json.Unmarshal(raw, &persisted); err != nil {
		return agentSessionState{}, fmt.Errorf("parse relay agent session state: %w", err)
	}
	if persisted.Version != agentSessionStateVersion {
		return agentSessionState{}, fmt.Errorf("unsupported relay agent session state version")
	}
	return agentSessionState{
		SessionID:       strings.TrimSpace(persisted.SessionID),
		ReconnectSecret: strings.TrimSpace(persisted.ReconnectSecret),
	}, nil
}

func (s *agentSessionStore) save(state agentSessionState) error {
	if s == nil {
		return nil
	}
	if !state.complete() {
		return fmt.Errorf("relay agent session state is incomplete")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create relay agent session state directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("secure relay agent session state directory: %w", err)
	}
	persisted := persistedAgentSessionState{
		Version:         agentSessionStateVersion,
		SessionID:       strings.TrimSpace(state.SessionID),
		ReconnectSecret: strings.TrimSpace(state.ReconnectSecret),
	}
	raw, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal relay agent session state: %w", err)
	}
	if err := writeFileAtomic(s.path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write relay agent session state: %w", err)
	}
	return nil
}

func (s *agentSessionStore) delete() error {
	if s == nil {
		return nil
	}
	if err := os.Remove(s.path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("delete relay agent session state: %w", err)
	}
	return nil
}

func (s agentSessionState) valid() bool {
	return s.complete()
}

func (s agentSessionState) complete() bool {
	return strings.TrimSpace(s.SessionID) != "" &&
		strings.TrimSpace(s.ReconnectSecret) != ""
}
