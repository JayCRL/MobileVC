package relayclient

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mobilevc/internal/relay/e2ee"
)

func TestAgentSessionStoreLoadsLegacyExpiryAsNonExpiring(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent_session.json")
	legacy := []byte(`{
  "version": 1,
  "sessionId": "rs_legacy",
  "reconnectSecret": "legacy-reconnect-secret",
  "expiresAt": "2026-05-23T16:09:48Z"
}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatalf("write legacy session state: %v", err)
	}

	state, err := newAgentSessionStore(path).load()
	if err != nil {
		t.Fatalf("load legacy session state: %v", err)
	}

	if !state.valid() {
		t.Fatalf("legacy session state should remain valid: %#v", state)
	}
	if state.SessionID != "rs_legacy" ||
		state.ReconnectSecret != "legacy-reconnect-secret" {
		t.Fatalf("unexpected legacy session state: %#v", state)
	}
}

func TestAgentSessionStoreSaveUsesCompleteJsonFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent_session.json")
	store := newAgentSessionStore(path)

	if err := store.save(agentSessionState{
		SessionID:       "rs_saved",
		ReconnectSecret: "saved-reconnect-secret",
	}); err != nil {
		t.Fatalf("save agent session state: %v", err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatalf("stat agent session state: %v", err)
	} else if info.Size() == 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected agent session file info: size=%d mode=%o", info.Size(), info.Mode().Perm())
	}
	loaded, err := store.load()
	if err != nil {
		t.Fatalf("load agent session state: %v", err)
	}
	if loaded.SessionID != "rs_saved" ||
		loaded.ReconnectSecret != "saved-reconnect-secret" {
		t.Fatalf("unexpected loaded state: %#v", loaded)
	}
}

func TestEmitPairingFileUsesCompleteJsonFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pairing.json")
	event := PairingReadyEvent{
		Type:               "mobilevc.relay.pairing_ready",
		RelayURL:           "wss://relay.example.test",
		SessionID:          "rs_pair",
		PairingSecret:      "pair-secret",
		ExpiresAt:          1770000000,
		Capabilities:       e2ee.ProductionCapabilities(),
		NodeFingerprintHex: "1111111111111111111111111111111111111111111111111111111111111111",
	}

	if err := EmitPairingFile(path, event); err != nil {
		t.Fatalf("emit pairing file: %v", err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatalf("stat pairing file: %v", err)
	} else if info.Size() == 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected pairing file info: size=%d mode=%o", info.Size(), info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pairing file: %v", err)
	}
	var decoded PairingReadyEvent
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode pairing file: %v", err)
	}
	if decoded.SessionID != event.SessionID ||
		decoded.PairingSecret != event.PairingSecret {
		t.Fatalf("unexpected pairing event: %#v", decoded)
	}
}
