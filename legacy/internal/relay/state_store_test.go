package relay

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mobilevc/internal/relay/e2ee"
)

func TestStateStoreSaveUsesCompleteJsonFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "public_relay_state.json")
	store := newStateStore(path)
	sessions := map[string]*sessionState{
		"rs_state": {
			id:                   "rs_state",
			pairingHash:          SecretHash("pair-secret"),
			agentReconnectHash:   SecretHash("agent-secret"),
			pairingExpiresAt:     time.Now().Add(time.Minute).UTC(),
			capabilities:         e2ee.ProductionCapabilities(),
			pairFailuresByRemote: map[string]int{},
			devices:              map[string]*deviceState{},
		},
	}

	if err := store.save(sessions); err != nil {
		t.Fatalf("save relay state: %v", err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatalf("stat relay state: %v", err)
	} else if info.Size() == 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected relay state file info: size=%d mode=%o", info.Size(), info.Mode().Perm())
	}
	loaded, err := store.load()
	if err != nil {
		t.Fatalf("load relay state: %v", err)
	}
	if loaded["rs_state"] == nil {
		t.Fatalf("saved session missing after reload: %#v", loaded)
	}
}
