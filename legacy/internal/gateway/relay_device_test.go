package gateway

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mobilevc/internal/protocol"
	"mobilevc/internal/relay"
	"mobilevc/internal/relay/e2ee"
)

func TestRegisterRelayDeviceRequiresE2EE(t *testing.T) {
	h := NewHandler("test", nil)
	store, err := e2ee.LoadOrCreateDeviceTrustStore(filepath.Join(t.TempDir(), e2ee.DeviceTrustFileName))
	if err != nil {
		t.Fatal(err)
	}
	h.DeviceTrust = store

	_, err = h.registerRelayDevice(&staticRelayClientConn{}, testRelayDeviceRegisterRequest(t))
	if err == nil || !strings.Contains(err.Error(), relay.CodeE2EERequired) {
		t.Fatalf("expected e2ee required error, got %v", err)
	}
}

func TestRegisterRelayDevicePersistsTrustedDeviceWithoutPlaintextCredential(t *testing.T) {
	trustPath := filepath.Join(t.TempDir(), e2ee.DeviceTrustFileName)
	store, err := e2ee.LoadOrCreateDeviceTrustStore(trustPath)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler("test", nil)
	h.DeviceTrust = store
	req := testRelayDeviceRegisterRequest(t)

	result, err := h.registerRelayDevice(&staticRelayClientConn{
		info: RelayE2EEInfo{
			Enabled:     true,
			SessionID:   "rs_gateway",
			ClientID:    "rc_attached",
			HandshakeID: "hs_pairing",
		},
	}, req)
	if err != nil {
		t.Fatalf("register relay device: %v", err)
	}
	if result.DeviceID != req.DeviceID || result.Status != "registered" || result.SessionID != "rs_gateway" {
		t.Fatalf("unexpected registration result: %#v", result)
	}
	devices, err := store.ListDevices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].ID != req.DeviceID || devices[0].CredentialHash == req.DeviceCredential {
		t.Fatalf("unexpected trusted device: %#v", devices)
	}
	if strings.Contains(readTestFile(t, trustPath), req.DeviceCredential) {
		t.Fatal("device trust store contains plaintext credential")
	}
}

func TestRelayDeviceListAndRevokeRequireBoundE2EEDevice(t *testing.T) {
	h := NewHandler("test", nil)
	store, err := e2ee.LoadOrCreateDeviceTrustStore(filepath.Join(t.TempDir(), e2ee.DeviceTrustFileName))
	if err != nil {
		t.Fatal(err)
	}
	h.DeviceTrust = store

	if _, err := h.listRelayDevices(&staticRelayClientConn{}); err == nil || !strings.Contains(err.Error(), relay.CodeE2EERequired) {
		t.Fatalf("expected e2ee required for direct list, got %v", err)
	}
	if _, err := h.listRelayDevices(&staticRelayClientConn{
		info: RelayE2EEInfo{
			Enabled: true, SessionID: "rs_gateway", ClientID: "rc_attached", HandshakeID: "hs_pairing",
		},
	}); err == nil || !strings.Contains(err.Error(), relay.CodeDeviceUnknown) {
		t.Fatalf("expected bound device requirement, got %v", err)
	}
}

func TestRelayDeviceListMarksCurrentAndRevokeUpdatesStore(t *testing.T) {
	store, err := e2ee.LoadOrCreateDeviceTrustStore(filepath.Join(t.TempDir(), e2ee.DeviceTrustFileName))
	if err != nil {
		t.Fatal(err)
	}
	active := registerGatewayTestDevice(t, store, "dev_active", "Pixel", "active-credential")
	other := registerGatewayTestDevice(t, store, "dev_lost", "iPhone", "lost-credential")
	if _, _, err := store.MarkDeviceSeen(active.ID, "hs_active", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.MarkDeviceSeen(other.ID, "hs_other", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	h := NewHandler("test", nil)
	h.DeviceTrust = store
	client := &staticRelayClientConn{
		info: RelayE2EEInfo{
			Enabled: true, SessionID: "rs_gateway", ClientID: "rc_attached", HandshakeID: "hs_active", DeviceID: active.ID,
		},
	}

	list, err := h.listRelayDevices(client)
	if err != nil {
		t.Fatalf("list relay devices: %v", err)
	}
	if len(list.Devices) != 2 {
		t.Fatalf("unexpected device count: %#v", list.Devices)
	}
	if !findRelayDevice(list.Devices, active.ID).CurrentDevice || !findRelayDevice(list.Devices, active.ID).Connected {
		t.Fatalf("active device not marked current/connected: %#v", list.Devices)
	}

	revoked, err := h.revokeRelayDevice(client, protocol.RelayDeviceRevokeRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "relay_device_revoke"},
		DeviceID:    other.ID,
	})
	if err != nil {
		t.Fatalf("revoke relay device: %v", err)
	}
	if revoked.DeviceID != other.ID || revoked.Status != "revoked" {
		t.Fatalf("unexpected revoke result: %#v", revoked)
	}
	list, err = h.listRelayDevices(client)
	if err != nil {
		t.Fatalf("list after revoke: %v", err)
	}
	if !findRelayDevice(list.Devices, other.ID).Revoked || findRelayDevice(list.Devices, other.ID).Connected {
		t.Fatalf("revoked device not reflected in list: %#v", list.Devices)
	}
}

func TestRelayDeviceRevokeClosesTrackedTargetConnections(t *testing.T) {
	store, err := e2ee.LoadOrCreateDeviceTrustStore(filepath.Join(t.TempDir(), e2ee.DeviceTrustFileName))
	if err != nil {
		t.Fatal(err)
	}
	active := registerGatewayTestDevice(t, store, "dev_active", "Pixel", "active-credential")
	other := registerGatewayTestDevice(t, store, "dev_lost", "iPhone", "lost-credential")
	h := NewHandler("test", nil)
	h.DeviceTrust = store
	targetConn := &staticRelayClientConn{
		info: RelayE2EEInfo{
			Enabled: true, SessionID: "rs_gateway", ClientID: "rc_lost", HandshakeID: "hs_other", DeviceID: other.ID,
		},
	}
	h.trackRelayE2EEConnection("conn-lost", targetConn)

	_, err = h.revokeRelayDevice(&staticRelayClientConn{
		info: RelayE2EEInfo{
			Enabled: true, SessionID: "rs_gateway", ClientID: "rc_attached", HandshakeID: "hs_active", DeviceID: active.ID,
		},
	}, protocol.RelayDeviceRevokeRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "relay_device_revoke"},
		DeviceID:    other.ID,
	})
	if err != nil {
		t.Fatalf("revoke relay device: %v", err)
	}
	if targetConn.closeCount != 1 {
		t.Fatalf("expected target relay connection to close once, got %d", targetConn.closeCount)
	}
	h.closeRelayDeviceConnections(other.ID)
	if targetConn.closeCount != 1 {
		t.Fatalf("expected revoked device registry entry to be removed, got %d closes", targetConn.closeCount)
	}
}

func TestRelayDeviceRotateReplacesNodeIdentityAndClearsDevices(t *testing.T) {
	store, err := e2ee.LoadOrCreateDeviceTrustStore(filepath.Join(t.TempDir(), e2ee.DeviceTrustFileName))
	if err != nil {
		t.Fatal(err)
	}
	active := registerGatewayTestDevice(t, store, "dev_active", "Pixel", "active-credential")
	other := registerGatewayTestDevice(t, store, "dev_lost", "iPhone", "lost-credential")
	nodeStore, err := e2ee.LoadOrCreateNodeIdentityStore(filepath.Join(t.TempDir(), e2ee.NodeIdentityFileName))
	if err != nil {
		t.Fatal(err)
	}
	before, err := nodeStore.Current()
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler("test", nil)
	h.DeviceTrust = store
	h.NodeIdentity = nodeStore
	activeConn := &rotatingRelayClientConn{
		staticRelayClientConn: staticRelayClientConn{
			info: RelayE2EEInfo{
				Enabled: true, SessionID: "rs_gateway", ClientID: "rc_active", HandshakeID: "hs_active", DeviceID: active.ID,
			},
		},
	}
	otherConn := &rotatingRelayClientConn{
		staticRelayClientConn: staticRelayClientConn{
			info: RelayE2EEInfo{
				Enabled: true, SessionID: "rs_gateway", ClientID: "rc_lost", HandshakeID: "hs_other", DeviceID: other.ID,
			},
		},
	}
	h.trackRelayE2EEConnection("conn-active", activeConn)
	h.trackRelayE2EEConnection("conn-lost", otherConn)

	result, err := h.rotateRelayDevices(activeConn)
	if err != nil {
		t.Fatalf("rotate relay devices: %v", err)
	}
	after, err := nodeStore.Current()
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "rotated" || result.NodeFingerprintHex == "" {
		t.Fatalf("unexpected rotate result: %#v", result)
	}
	if result.NodeFingerprintHex == fmt.Sprintf("%x", before.Fingerprint) ||
		result.NodeFingerprintHex != fmt.Sprintf("%x", after.Fingerprint) {
		t.Fatalf("unexpected node fingerprint before=%x after=%x result=%s", before.Fingerprint, after.Fingerprint, result.NodeFingerprintHex)
	}
	devices, err := store.ListDevices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 0 {
		t.Fatalf("rotation left trusted devices: %#v", devices)
	}
	h.closeAllRelayDeviceConnections()
	if activeConn.rotateCount != 1 || otherConn.rotateCount != 1 {
		t.Fatalf("expected all tracked relay sessions to rotate, got active=%d other=%d", activeConn.rotateCount, otherConn.rotateCount)
	}
	if activeConn.closeCount != 1 || otherConn.closeCount != 1 {
		t.Fatalf("expected all tracked relay connections to close, got active=%d other=%d", activeConn.closeCount, otherConn.closeCount)
	}
}

func TestRelayDeviceRevokeRejectsCurrentManagementDevice(t *testing.T) {
	store, err := e2ee.LoadOrCreateDeviceTrustStore(filepath.Join(t.TempDir(), e2ee.DeviceTrustFileName))
	if err != nil {
		t.Fatal(err)
	}
	active := registerGatewayTestDevice(t, store, "dev_active", "Pixel", "active-credential")
	h := NewHandler("test", nil)
	h.DeviceTrust = store

	_, err = h.revokeRelayDevice(&staticRelayClientConn{
		info: RelayE2EEInfo{
			Enabled: true, SessionID: "rs_gateway", ClientID: "rc_attached", HandshakeID: "hs_active", DeviceID: active.ID,
		},
	}, protocol.RelayDeviceRevokeRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "relay_device_revoke"},
		DeviceID:    active.ID,
	})
	if err == nil || !strings.Contains(err.Error(), relay.CodeDeviceUnknown) {
		t.Fatalf("expected current-device revoke rejection, got %v", err)
	}
}

func testRelayDeviceRegisterRequest(t *testing.T) protocol.RelayDeviceRegisterRequestEvent {
	t.Helper()
	identity, err := e2ee.GenerateNodeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return protocol.RelayDeviceRegisterRequestEvent{
		ClientEvent: protocol.ClientEvent{Action: "relay_device_register"},
		DeviceID:    "dev_test",
		DisplayName: "Pixel",
		DeviceIdentityPublicKey: base64.RawURLEncoding.EncodeToString(
			identity.PublicKey,
		),
		DeviceCredential: "device-credential-128-bit-minimum",
	}
}

func registerGatewayTestDevice(t *testing.T, store *e2ee.DeviceTrustStore, id string, name string, credential string) e2ee.TrustedDevice {
	t.Helper()
	identity, err := e2ee.GenerateNodeIdentity()
	if err != nil {
		t.Fatal(err)
	}
	device, err := store.RegisterDevice(e2ee.DeviceRegistration{
		ID: id, DisplayName: name, PublicKey: identity.PublicKey,
		DeviceCredential: credential, Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return device
}

func findRelayDevice(devices []protocol.RelayTrustedDevice, id string) protocol.RelayTrustedDevice {
	for _, device := range devices {
		if device.DeviceID == id {
			return device
		}
	}
	return protocol.RelayTrustedDevice{}
}

type staticRelayClientConn struct {
	info       RelayE2EEInfo
	closeCount int
}

func (c *staticRelayClientConn) ReadJSON(any) error  { return nil }
func (c *staticRelayClientConn) WriteJSON(any) error { return nil }
func (c *staticRelayClientConn) Close() error {
	c.closeCount++
	return nil
}
func (c *staticRelayClientConn) RemoteAddr() string { return "relay:test" }
func (c *staticRelayClientConn) Origin() string     { return "relay" }
func (c *staticRelayClientConn) RelayE2EEInfo() RelayE2EEInfo {
	return c.info
}

type rotatingRelayClientConn struct {
	staticRelayClientConn
	rotateCount int
}

func (c *rotatingRelayClientConn) RotateRelaySession() error {
	c.rotateCount++
	return c.Close()
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
