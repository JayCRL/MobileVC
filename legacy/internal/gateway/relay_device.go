package gateway

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"mobilevc/internal/protocol"
	"mobilevc/internal/relay"
	"mobilevc/internal/relay/e2ee"
)

func (h *Handler) registerRelayDevice(client ClientConn, req protocol.RelayDeviceRegisterRequestEvent) (protocol.RelayDeviceRegisterResultEvent, error) {
	info, ok := relayE2EEInfo(client)
	if !ok || !info.Enabled {
		return protocol.RelayDeviceRegisterResultEvent{}, fmt.Errorf("%s: relay device registration requires e2ee", relay.CodeE2EERequired)
	}
	if h.DeviceTrust == nil {
		return protocol.RelayDeviceRegisterResultEvent{}, errors.New("relay device trust store is not configured")
	}
	if strings.TrimSpace(req.DeviceID) == "" ||
		strings.TrimSpace(req.DisplayName) == "" ||
		strings.TrimSpace(req.DeviceCredential) == "" {
		return protocol.RelayDeviceRegisterResultEvent{}, fmt.Errorf("%s: missing relay device registration fields", relay.CodeDeviceUnknown)
	}
	publicKey, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(req.DeviceIdentityPublicKey))
	if err != nil {
		return protocol.RelayDeviceRegisterResultEvent{}, fmt.Errorf("%s: invalid relay device identity encoding", relay.CodeE2EEHandshakeFailed)
	}
	device, err := h.DeviceTrust.RegisterDevice(e2ee.DeviceRegistration{
		ID:               req.DeviceID,
		DisplayName:      req.DisplayName,
		PublicKey:        publicKey,
		DeviceCredential: req.DeviceCredential,
		Now:              time.Now().UTC(),
	})
	if err != nil {
		relay.LogAuditEvent(relay.AuditEvent{
			Action: "relay_device_register", Result: "failed",
			SessionID: info.SessionID, ClientID: info.ClientID, DeviceID: req.DeviceID,
			ErrorCode: relayDeviceRegisterErrorCode(err),
		})
		return protocol.RelayDeviceRegisterResultEvent{}, err
	}
	if bound, ok := client.(relayDeviceBoundClientConn); ok {
		bound.SetRelayE2EEDeviceID(device.ID)
	}
	relay.LogAuditEvent(relay.AuditEvent{
		Action: "relay_device_register", Result: "ok",
		SessionID: info.SessionID, ClientID: info.ClientID, DeviceID: device.ID,
	})
	return protocol.NewRelayDeviceRegisterResultEvent(
		info.SessionID,
		device.ID,
		fmt.Sprintf("%x", device.Fingerprint),
		"registered",
	), nil
}

func (h *Handler) listRelayDevices(client ClientConn) (protocol.RelayDeviceListResultEvent, error) {
	info, err := h.relayDeviceManagementInfo(client)
	if err != nil {
		return protocol.RelayDeviceListResultEvent{}, err
	}
	devices, err := h.DeviceTrust.ListDevices()
	if err != nil {
		relay.LogAuditEvent(relay.AuditEvent{
			Action: "relay_device_list", Result: "failed",
			SessionID: info.SessionID, ClientID: info.ClientID, DeviceID: info.DeviceID,
			ErrorCode: relayDeviceRegisterErrorCode(err),
		})
		return protocol.RelayDeviceListResultEvent{}, err
	}
	relay.LogAuditEvent(relay.AuditEvent{
		Action: "relay_device_list", Result: "ok",
		SessionID: info.SessionID, ClientID: info.ClientID, DeviceID: info.DeviceID,
	})
	return protocol.NewRelayDeviceListResultEvent(
		info.SessionID,
		relayTrustedDevices(devices, info.HandshakeID, info.DeviceID),
	), nil
}

func (h *Handler) revokeRelayDevice(client ClientConn, req protocol.RelayDeviceRevokeRequestEvent) (protocol.RelayDeviceRevokeResultEvent, error) {
	info, err := h.relayDeviceManagementInfo(client)
	if err != nil {
		return protocol.RelayDeviceRevokeResultEvent{}, err
	}
	deviceID := strings.TrimSpace(req.DeviceID)
	if deviceID == "" {
		return protocol.RelayDeviceRevokeResultEvent{}, fmt.Errorf("%s: missing relay device id", relay.CodeDeviceUnknown)
	}
	if deviceID == info.DeviceID {
		return protocol.RelayDeviceRevokeResultEvent{}, fmt.Errorf("%s: cannot revoke the active management device", relay.CodeDeviceUnknown)
	}
	if _, err := h.DeviceTrust.RevokeDevice(deviceID, time.Now().UTC()); err != nil {
		relay.LogAuditEvent(relay.AuditEvent{
			Action: "relay_device_revoke", Result: "failed",
			SessionID: info.SessionID, ClientID: info.ClientID, DeviceID: deviceID,
			ErrorCode: relayDeviceRegisterErrorCode(err),
		})
		return protocol.RelayDeviceRevokeResultEvent{}, err
	}
	h.closeRelayDeviceConnections(deviceID)
	relay.LogAuditEvent(relay.AuditEvent{
		Action: "relay_device_revoke", Result: "ok",
		SessionID: info.SessionID, ClientID: info.ClientID, DeviceID: deviceID,
	})
	return protocol.NewRelayDeviceRevokeResultEvent(info.SessionID, deviceID, "revoked"), nil
}

func (h *Handler) rotateRelayDevices(client ClientConn) (protocol.RelayDeviceRotateResultEvent, error) {
	info, err := h.relayDeviceManagementInfo(client)
	if err != nil {
		return protocol.RelayDeviceRotateResultEvent{}, err
	}
	if h.NodeIdentity == nil {
		return protocol.RelayDeviceRotateResultEvent{}, errors.New("relay node identity store is not configured")
	}
	identity, err := h.NodeIdentity.Rotate()
	if err != nil {
		relay.LogAuditEvent(relay.AuditEvent{
			Action: "relay_device_rotate", Result: "failed",
			SessionID: info.SessionID, ClientID: info.ClientID, DeviceID: info.DeviceID,
			ErrorCode: relayDeviceRegisterErrorCode(err),
		})
		return protocol.RelayDeviceRotateResultEvent{}, err
	}
	if err := h.DeviceTrust.ClearTrustedDevicesForNodeRotation(); err != nil {
		relay.LogAuditEvent(relay.AuditEvent{
			Action: "relay_device_rotate", Result: "failed",
			SessionID: info.SessionID, ClientID: info.ClientID, DeviceID: info.DeviceID,
			ErrorCode: relayDeviceRegisterErrorCode(err),
		})
		return protocol.RelayDeviceRotateResultEvent{}, err
	}
	relay.LogAuditEvent(relay.AuditEvent{
		Action: "relay_device_rotate", Result: "ok",
		SessionID: info.SessionID, ClientID: info.ClientID, DeviceID: info.DeviceID,
	})
	return protocol.NewRelayDeviceRotateResultEvent(
		info.SessionID,
		fmt.Sprintf("%x", identity.Fingerprint),
		"rotated",
	), nil
}

func (h *Handler) relayDeviceManagementInfo(client ClientConn) (RelayE2EEInfo, error) {
	info, ok := relayE2EEInfo(client)
	if !ok || !info.Enabled {
		return RelayE2EEInfo{}, fmt.Errorf("%s: relay device management requires e2ee", relay.CodeE2EERequired)
	}
	if h.DeviceTrust == nil {
		return RelayE2EEInfo{}, errors.New("relay device trust store is not configured")
	}
	if strings.TrimSpace(info.DeviceID) == "" {
		return RelayE2EEInfo{}, fmt.Errorf("%s: relay device identity is not bound to this e2ee session", relay.CodeDeviceUnknown)
	}
	return info, nil
}

func relayTrustedDevices(devices []e2ee.TrustedDevice, currentHandshakeID string, currentDeviceID string) []protocol.RelayTrustedDevice {
	out := make([]protocol.RelayTrustedDevice, 0, len(devices))
	for _, device := range devices {
		revoked := !device.RevokedAt.IsZero()
		activeSessionID := strings.TrimSpace(device.ActiveSessionID)
		out = append(out, protocol.RelayTrustedDevice{
			DeviceID:        device.ID,
			DisplayName:     device.DisplayName,
			FingerprintHex:  fmt.Sprintf("%x", device.Fingerprint),
			CreatedAt:       device.CreatedAt.UTC().Format(time.RFC3339Nano),
			LastSeenAt:      device.LastSeenAt.UTC().Format(time.RFC3339Nano),
			RevokedAt:       relayDeviceOptionalTime(device.RevokedAt),
			ActiveSessionID: activeSessionID,
			Connected:       !revoked && activeSessionID != "",
			CurrentDevice:   device.ID == currentDeviceID || activeSessionID == strings.TrimSpace(currentHandshakeID),
			Revoked:         revoked,
		})
	}
	return out
}

func relayDeviceOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func relayE2EEInfo(client ClientConn) (RelayE2EEInfo, bool) {
	relayClient, ok := client.(relayE2EEClientConn)
	if !ok {
		return RelayE2EEInfo{}, false
	}
	info := relayClient.RelayE2EEInfo()
	return info, info.SessionID != "" && info.ClientID != "" && info.HandshakeID != ""
}

type relayDeviceBoundClientConn interface {
	SetRelayE2EEDeviceID(string)
}

type relaySessionRotator interface {
	RotateRelaySession() error
}

type relayDeviceConnectionRegistry struct {
	mu             sync.Mutex
	connectionByID map[string]relayDeviceConnection
	idsByDevice    map[string]map[string]struct{}
}

type relayDeviceConnection struct {
	deviceID string
	client   ClientConn
}

func newRelayDeviceConnectionRegistry() *relayDeviceConnectionRegistry {
	return &relayDeviceConnectionRegistry{
		connectionByID: map[string]relayDeviceConnection{},
		idsByDevice:    map[string]map[string]struct{}{},
	}
}

func (h *Handler) trackRelayE2EEConnection(connectionID string, client ClientConn) {
	if h.relayDeviceConns == nil {
		h.relayDeviceConns = newRelayDeviceConnectionRegistry()
	}
	info, ok := relayE2EEInfo(client)
	if !ok || !info.Enabled {
		return
	}
	deviceID := strings.TrimSpace(info.DeviceID)
	if deviceID == "" {
		return
	}
	h.relayDeviceConns.track(connectionID, deviceID, client)
}

func (h *Handler) forgetRelayE2EEConnection(connectionID string) {
	if h.relayDeviceConns == nil {
		return
	}
	h.relayDeviceConns.forget(connectionID)
}

func (h *Handler) closeRelayDeviceConnections(deviceID string) {
	if h.relayDeviceConns == nil {
		return
	}
	for _, client := range h.relayDeviceConns.takeDevice(deviceID) {
		_ = client.Close()
	}
}

func (h *Handler) closeAllRelayDeviceConnections() {
	if h.relayDeviceConns == nil {
		return
	}
	for _, client := range h.relayDeviceConns.takeAll() {
		if rotator, ok := client.(relaySessionRotator); ok {
			_ = rotator.RotateRelaySession()
			continue
		}
		_ = client.Close()
	}
}

func (r *relayDeviceConnectionRegistry) track(connectionID string, deviceID string, client ClientConn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	trimmedConnectionID := strings.TrimSpace(connectionID)
	trimmedDeviceID := strings.TrimSpace(deviceID)
	if trimmedConnectionID == "" || trimmedDeviceID == "" || client == nil {
		return
	}
	r.forgetLocked(trimmedConnectionID)
	r.connectionByID[trimmedConnectionID] = relayDeviceConnection{
		deviceID: trimmedDeviceID,
		client:   client,
	}
	if r.idsByDevice[trimmedDeviceID] == nil {
		r.idsByDevice[trimmedDeviceID] = map[string]struct{}{}
	}
	r.idsByDevice[trimmedDeviceID][trimmedConnectionID] = struct{}{}
}

func (r *relayDeviceConnectionRegistry) forget(connectionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forgetLocked(strings.TrimSpace(connectionID))
}

func (r *relayDeviceConnectionRegistry) takeDevice(deviceID string) []ClientConn {
	r.mu.Lock()
	defer r.mu.Unlock()
	trimmedDeviceID := strings.TrimSpace(deviceID)
	if trimmedDeviceID == "" {
		return nil
	}
	ids := r.idsByDevice[trimmedDeviceID]
	if len(ids) == 0 {
		return nil
	}
	clients := make([]ClientConn, 0, len(ids))
	for connectionID := range ids {
		if tracked, ok := r.connectionByID[connectionID]; ok && tracked.client != nil {
			clients = append(clients, tracked.client)
		}
		delete(r.connectionByID, connectionID)
	}
	delete(r.idsByDevice, trimmedDeviceID)
	return clients
}

func (r *relayDeviceConnectionRegistry) takeAll() []ClientConn {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.connectionByID) == 0 {
		return nil
	}
	clients := make([]ClientConn, 0, len(r.connectionByID))
	for _, tracked := range r.connectionByID {
		if tracked.client != nil {
			clients = append(clients, tracked.client)
		}
	}
	r.connectionByID = map[string]relayDeviceConnection{}
	r.idsByDevice = map[string]map[string]struct{}{}
	return clients
}

func (r *relayDeviceConnectionRegistry) forgetLocked(connectionID string) {
	if connectionID == "" {
		return
	}
	tracked, ok := r.connectionByID[connectionID]
	if !ok {
		return
	}
	delete(r.connectionByID, connectionID)
	ids := r.idsByDevice[tracked.deviceID]
	delete(ids, connectionID)
	if len(ids) == 0 {
		delete(r.idsByDevice, tracked.deviceID)
	}
}

func relayDeviceRegisterErrorCode(err error) string {
	message := err.Error()
	for _, code := range []string{
		relay.CodeE2EERequired,
		relay.CodeE2EEHandshakeFailed,
		relay.CodeDeviceRevoked,
		relay.CodeDeviceUnknown,
	} {
		if strings.Contains(message, code) {
			return code
		}
	}
	return relay.CodeE2EEHandshakeFailed
}
