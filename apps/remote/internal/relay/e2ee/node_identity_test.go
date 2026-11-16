package e2ee

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateNodeIdentityPersistsStableIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mobilevc", "relay", NodeIdentityFileName)

	first, err := LoadOrCreateNodeIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateNodeIdentity(path)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first.PublicKey, second.PublicKey) {
		t.Fatal("node identity was regenerated instead of loaded")
	}
	if !bytes.Equal(first.Fingerprint, second.Fingerprint) {
		t.Fatal("node fingerprint changed after reload")
	}

	assertMode(t, filepath.Dir(path), 0o700)
	assertMode(t, path, 0o600)
	assertStoredIdentityHasNoPlaintextSecrets(t, path)
}

func TestNodeIdentityStoreRotatePersistsNewIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mobilevc", "relay", NodeIdentityFileName)
	store, err := LoadOrCreateNodeIdentityStore(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Current()
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := store.Rotate()
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.Current()
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadNodeIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.Fingerprint, rotated.Fingerprint) {
		t.Fatal("node identity rotation kept the same fingerprint")
	}
	if !bytes.Equal(rotated.Fingerprint, current.Fingerprint) ||
		!bytes.Equal(rotated.Fingerprint, reloaded.Fingerprint) {
		t.Fatal("rotated node identity was not kept current and persisted")
	}
	assertMode(t, filepath.Dir(path), 0o700)
	assertMode(t, path, 0o600)
}

func TestNodeIdentitySignsAndVerifiesTranscript(t *testing.T) {
	identity, err := GenerateNodeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	transcript := []byte("mobilevc relay e2ee handshake transcript")

	signature, err := identity.SignTranscript(transcript)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyNodeSignature(identity.PublicKey, transcript, signature)
	if err != nil {
		t.Fatal(err)
	}
	if !verified {
		t.Fatal("valid node identity signature was rejected")
	}

	tampered, err := VerifyNodeSignature(identity.PublicKey, []byte("tampered"), signature)
	if err != nil {
		t.Fatal(err)
	}
	if tampered {
		t.Fatal("tampered transcript signature was accepted")
	}
}

func TestLoadNodeIdentityRejectsMetadataMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node_identity.json")
	identity, err := GenerateNodeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveNodeIdentity(path, identity); err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &raw); err != nil {
		t.Fatal(err)
	}
	raw["fingerprintHex"] = "bad"
	tampered, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadNodeIdentity(path); err == nil {
		t.Fatal("loaded node identity with mismatched public metadata")
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode: got %o want %o", path, got, want)
	}
}

func assertStoredIdentityHasNoPlaintextSecrets(t *testing.T, path string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(content, []byte("PRIVATE KEY")) {
		t.Fatal("node identity file contains PEM private key header")
	}
}
