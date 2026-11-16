package e2ee

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDeviceTrustStoreRegistersAndPersistsDevice(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mobilevc", "relay", DeviceTrustFileName)
	store, err := LoadOrCreateDeviceTrustStore(path)
	if err != nil {
		t.Fatal(err)
	}
	deviceKey := testDeviceIdentityPublicKey(t)
	credential := "device-credential-128-bit-minimum"
	registeredAt := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)

	device, err := store.RegisterDevice(DeviceRegistration{
		ID: "dev_1", DisplayName: "Pixel", PublicKey: deviceKey,
		DeviceCredential: credential, Now: registeredAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if device.ID != "dev_1" || device.DisplayName != "Pixel" || device.CredentialHash == credential {
		t.Fatalf("unexpected trusted device: %#v", device)
	}
	if !bytes.Equal(device.Fingerprint, Fingerprint(deviceKey)) {
		t.Fatal("device fingerprint mismatch")
	}
	assertMode(t, filepath.Dir(path), 0o700)
	assertMode(t, path, 0o600)
	assertDeviceTrustStoreHasNoPlaintextCredential(t, path, credential)

	reloaded, err := LoadOrCreateDeviceTrustStore(path)
	if err != nil {
		t.Fatal(err)
	}
	devices, err := reloaded.ListDevices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].ID != "dev_1" || !devices[0].CreatedAt.Equal(registeredAt) {
		t.Fatalf("unexpected reloaded devices: %#v", devices)
	}
}

func TestDeviceTrustStoreVerifiesCredentialAndRejectsRevokedDevice(t *testing.T) {
	store := testDeviceTrustStore(t)
	device := registerTestTrustedDevice(t, store, "dev_1", "Pixel", "credential-one")

	if _, err := store.VerifyDeviceCredential(device.ID, "credential-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyDeviceCredential(device.ID, "wrong"); err == nil || !strings.Contains(err.Error(), "device_unknown") {
		t.Fatalf("expected wrong credential failure, got %v", err)
	}

	revoked, err := store.RevokeDevice(device.ID, time.Date(2026, 5, 21, 11, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if revoked.RevokedAt.IsZero() {
		t.Fatal("revoked device has no revoked timestamp")
	}
	if _, err := store.VerifyDeviceCredential(device.ID, "credential-one"); err == nil || !strings.Contains(err.Error(), "device_revoked") {
		t.Fatalf("expected revoked credential failure, got %v", err)
	}
	if _, err := store.RegisterDevice(DeviceRegistration{
		ID: device.ID, DisplayName: "Pixel rebound", PublicKey: testDeviceIdentityPublicKey(t),
		DeviceCredential: "credential-rebound", Now: time.Date(2026, 5, 21, 11, 1, 0, 0, time.UTC),
	}); err == nil || !strings.Contains(err.Error(), "device_already_bound") {
		t.Fatalf("expected duplicate revoked registration failure, got %v", err)
	}
}

func TestDeviceTrustStoreRegisterDeviceIsIdempotentForSameDeviceCredential(t *testing.T) {
	store := testDeviceTrustStore(t)
	publicKey := testDeviceIdentityPublicKey(t)
	first, err := store.RegisterDevice(DeviceRegistration{
		ID: "dev_1", DisplayName: "Pixel", PublicKey: publicKey,
		DeviceCredential: "credential-one", Now: time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	second, err := store.RegisterDevice(DeviceRegistration{
		ID: "dev_1", DisplayName: "Pixel renamed", PublicKey: publicKey,
		DeviceCredential: "credential-one", Now: time.Date(2026, 5, 21, 10, 2, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.CreatedAt != first.CreatedAt {
		t.Fatalf("idempotent registration changed identity: first=%#v second=%#v", first, second)
	}
	if second.DisplayName != "Pixel renamed" || !second.LastSeenAt.After(first.LastSeenAt) {
		t.Fatalf("idempotent registration did not refresh metadata: first=%#v second=%#v", first, second)
	}
}

func TestDeviceTrustStoreRegisterDeviceRejectsDuplicateWithDifferentCredential(t *testing.T) {
	store := testDeviceTrustStore(t)
	publicKey := testDeviceIdentityPublicKey(t)
	if _, err := store.RegisterDevice(DeviceRegistration{
		ID: "dev_1", DisplayName: "Pixel", PublicKey: publicKey,
		DeviceCredential: "credential-one", Now: time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	_, err := store.RegisterDevice(DeviceRegistration{
		ID: "dev_1", DisplayName: "Pixel", PublicKey: publicKey,
		DeviceCredential: "credential-two", Now: time.Date(2026, 5, 21, 10, 2, 0, 0, time.UTC),
	})
	if err == nil || !strings.Contains(err.Error(), "device_already_bound") {
		t.Fatalf("expected duplicate device rejection, got %v", err)
	}
}

func TestDeviceTrustStoreVerifiesReconnectDeviceProof(t *testing.T) {
	store := testDeviceTrustStore(t)
	deviceIdentity := testNodeIdentity(t)
	nodeIdentity := testNodeIdentity(t)
	credential := "credential-one"
	_, err := store.RegisterDevice(DeviceRegistration{
		ID: "dev_1", DisplayName: "Pixel", PublicKey: deviceIdentity.PublicKey,
		DeviceCredential: credential, Now: time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	input := ProductionCapabilities().ApplyToHandshake(HandshakeInput{
		Kind:                     HandshakeKindReconnect,
		SessionID:                "rs_1",
		ClientID:                 "rc_1",
		HandshakeID:              "hs_1",
		ClientEphemeralPublicKey: testDeviceIdentityPublicKey(t),
		NodeEphemeralPublicKey:   testDeviceIdentityPublicKey(t),
		NodeIdentityPublicKey:    nodeIdentity.PublicKey,
		DeviceIdentityPublicKey:  deviceIdentity.PublicKey,
	})
	transcript, err := HandshakeTranscript(input)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := deviceIdentity.SignTranscript(transcript)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyDeviceProof(
		"dev_1",
		deviceIdentity.PublicKey,
		transcript,
		DeviceProof(credential, transcript),
		signature,
	); err != nil {
		t.Fatalf("verify reconnect proof: %v", err)
	}
	if _, err := store.VerifyDeviceProof(
		"dev_1",
		deviceIdentity.PublicKey,
		transcript,
		DeviceProof("wrong", transcript),
		signature,
	); err == nil || !strings.Contains(err.Error(), "device_unknown") {
		t.Fatalf("expected wrong proof to be unknown, got %v", err)
	}
}

func TestDeviceTrustStoreMarksSeenAndReturnsReplacedSession(t *testing.T) {
	store := testDeviceTrustStore(t)
	device := registerTestTrustedDevice(t, store, "dev_1", "Pixel", "credential-one")

	seen, previous, err := store.MarkDeviceSeen(device.ID, "hs_1", time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if previous != "" || seen.ActiveSessionID != "hs_1" {
		t.Fatalf("unexpected first active session: previous=%q device=%#v", previous, seen)
	}
	seen, previous, err = store.MarkDeviceSeen(device.ID, "hs_2", time.Date(2026, 5, 21, 12, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if previous != "hs_1" || seen.ActiveSessionID != "hs_2" {
		t.Fatalf("unexpected replaced active session: previous=%q device=%#v", previous, seen)
	}
}

func TestDeviceTrustStoreClearTrustedDevicesForNodeRotation(t *testing.T) {
	store := testDeviceTrustStore(t)
	registerTestTrustedDevice(t, store, "dev_1", "Pixel", "credential-one")
	registerTestTrustedDevice(t, store, "dev_2", "iPhone", "credential-two")

	if err := store.ClearTrustedDevicesForNodeRotation(); err != nil {
		t.Fatal(err)
	}
	devices, err := store.ListDevices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 0 {
		t.Fatalf("global rotate left devices: %#v", devices)
	}
	if _, err := store.VerifyDeviceCredential("dev_1", "credential-one"); err == nil || !strings.Contains(err.Error(), "device_unknown") {
		t.Fatalf("expected rotated device failure, got %v", err)
	}
}

func TestDeviceTrustStoreSerializesConcurrentUpdates(t *testing.T) {
	store := testDeviceTrustStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := store.RegisterDevice(DeviceRegistration{
				ID: "dev_" + string(rune('a'+i)), DisplayName: "Device",
				PublicKey: testDeviceIdentityPublicKey(t), DeviceCredential: "credential",
				Now: time.Date(2026, 5, 21, 10, i, 0, 0, time.UTC),
			})
			if err != nil {
				t.Errorf("register device %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	devices, err := store.ListDevices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 20 {
		t.Fatalf("device count: got %d want 20", len(devices))
	}
}

func TestDeviceTrustStoreRejectsMetadataTamper(t *testing.T) {
	path := filepath.Join(t.TempDir(), DeviceTrustFileName)
	store, err := LoadOrCreateDeviceTrustStore(path)
	if err != nil {
		t.Fatal(err)
	}
	registerTestTrustedDevice(t, store, "dev_1", "Pixel", "credential-one")

	var raw map[string]any
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &raw); err != nil {
		t.Fatal(err)
	}
	devices := raw["devices"].(map[string]any)
	device := devices["dev_1"].(map[string]any)
	device["fingerprintHex"] = "bad"
	tampered, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateDeviceTrustStore(path); err == nil {
		t.Fatal("loaded tampered device trust store")
	}
}

func testDeviceTrustStore(t *testing.T) *DeviceTrustStore {
	t.Helper()
	store, err := LoadOrCreateDeviceTrustStore(filepath.Join(t.TempDir(), DeviceTrustFileName))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func registerTestTrustedDevice(t *testing.T, store *DeviceTrustStore, id string, name string, credential string) TrustedDevice {
	t.Helper()
	device, err := store.RegisterDevice(DeviceRegistration{
		ID: id, DisplayName: name, PublicKey: testDeviceIdentityPublicKey(t),
		DeviceCredential: credential, Now: time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return device
}

func testDeviceIdentityPublicKey(t *testing.T) []byte {
	t.Helper()
	identity, err := GenerateNodeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return identity.PublicKey
}

func assertDeviceTrustStoreHasNoPlaintextCredential(t *testing.T, path string, credential string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(content, []byte(credential)) {
		t.Fatal("device trust store contains plaintext credential")
	}
}
