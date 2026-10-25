package e2ee

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	DeviceTrustFileName = "trusted_devices.json"
	deviceTrustVersion  = 1
)

type TrustedDevice struct {
	ID              string
	DisplayName     string
	PublicKey       []byte
	Fingerprint     []byte
	CredentialHash  string
	CreatedAt       time.Time
	LastSeenAt      time.Time
	RevokedAt       time.Time
	ActiveSessionID string
}

type DeviceTrustStore struct {
	path string
	mu   sync.Mutex
	file deviceTrustFile
}

type DeviceRegistration struct {
	ID               string
	DisplayName      string
	PublicKey        []byte
	DeviceCredential string
	Now              time.Time
}

type trustedDeviceFile struct {
	ID              string `json:"id"`
	DisplayName     string `json:"displayName"`
	PublicKeyBase64 string `json:"publicKeyBase64"`
	FingerprintHex  string `json:"fingerprintHex"`
	CredentialHash  string `json:"credentialHash"`
	CreatedAt       string `json:"createdAt"`
	LastSeenAt      string `json:"lastSeenAt"`
	RevokedAt       string `json:"revokedAt,omitempty"`
	ActiveSessionID string `json:"activeSessionId,omitempty"`
}

type deviceTrustFile struct {
	Version int                          `json:"version"`
	Devices map[string]trustedDeviceFile `json:"devices"`
}

func DefaultDeviceTrustPath(homeDir string) string {
	return filepath.Join(homeDir, ".mobilevc", "relay", DeviceTrustFileName)
}

func LoadOrCreateDeviceTrustStore(path string) (*DeviceTrustStore, error) {
	normalized := strings.TrimSpace(path)
	if normalized == "" {
		return nil, errors.New("device trust path is required")
	}
	store := &DeviceTrustStore{path: normalized, file: deviceTrustFile{
		Version: deviceTrustVersion, Devices: map[string]trustedDeviceFile{},
	}}
	if _, err := os.Stat(normalized); errors.Is(err, os.ErrNotExist) {
		if err := store.save(); err != nil {
			return nil, err
		}
		return store, nil
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func NewDeviceCredential() (string, error) {
	bytes := make([]byte, KeyLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate device credential: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func DeviceCredentialHash(credential string) string {
	sum := sha256.Sum256([]byte(credential))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func DeviceCredentialMatches(hash string, credential string) bool {
	actual := DeviceCredentialHash(credential)
	if len(hash) != len(actual) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hash), []byte(actual)) == 1
}

func DeviceProofMatchesCredentialHash(hash string, transcript []byte, expected []byte) bool {
	secretHash, err := base64.RawURLEncoding.DecodeString(hash)
	if err != nil {
		return false
	}
	actual, err := proofFromSecretHash(ProofPurposeDevice, secretHash, transcript)
	if err != nil {
		return false
	}
	return len(actual) == len(expected) && subtle.ConstantTimeCompare(actual, expected) == 1
}

func (s *DeviceTrustStore) RegisterDevice(reg DeviceRegistration) (TrustedDevice, error) {
	if s == nil {
		return TrustedDevice{}, errors.New("device trust store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := reg.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	normalized, err := normalizeDeviceRegistration(reg)
	if err != nil {
		return TrustedDevice{}, err
	}
	if existing, exists := s.file.Devices[normalized.ID]; exists {
		return s.rebindDeviceLocked(existing, normalized, now)
	}
	device := trustedDeviceFile{
		ID: normalized.ID, DisplayName: normalized.DisplayName,
		PublicKeyBase64: base64.RawStdEncoding.EncodeToString(normalized.PublicKey),
		FingerprintHex:  fmt.Sprintf("%x", Fingerprint(normalized.PublicKey)),
		CredentialHash:  DeviceCredentialHash(normalized.DeviceCredential),
		CreatedAt:       now.Format(time.RFC3339Nano),
		LastSeenAt:      now.Format(time.RFC3339Nano),
	}
	s.file.Devices[device.ID] = device
	if err := s.save(); err != nil {
		return TrustedDevice{}, err
	}
	return device.toTrusted()
}

func (s *DeviceTrustStore) rebindDeviceLocked(existing trustedDeviceFile, reg DeviceRegistration, now time.Time) (TrustedDevice, error) {
	if strings.TrimSpace(existing.RevokedAt) != "" {
		return TrustedDevice{}, errors.New("device_already_bound")
	}
	if !DeviceCredentialMatches(existing.CredentialHash, reg.DeviceCredential) {
		return TrustedDevice{}, errors.New("device_already_bound")
	}
	if encoded := base64.RawStdEncoding.EncodeToString(reg.PublicKey); encoded != existing.PublicKeyBase64 {
		return TrustedDevice{}, errors.New("device_already_bound")
	}
	if strings.TrimSpace(reg.DisplayName) != "" {
		existing.DisplayName = reg.DisplayName
	}
	existing.LastSeenAt = now.Format(time.RFC3339Nano)
	existing.ActiveSessionID = ""
	s.file.Devices[existing.ID] = existing
	if err := s.save(); err != nil {
		return TrustedDevice{}, err
	}
	return existing.toTrusted()
}

func (s *DeviceTrustStore) ListDevices() ([]TrustedDevice, error) {
	if s == nil {
		return nil, errors.New("device trust store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	devices := make([]TrustedDevice, 0, len(s.file.Devices))
	for _, encoded := range s.file.Devices {
		device, err := encoded.toTrusted()
		if err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].CreatedAt.Before(devices[j].CreatedAt)
	})
	return devices, nil
}

func (s *DeviceTrustStore) VerifyDeviceCredential(deviceID string, credential string) (TrustedDevice, error) {
	if s == nil {
		return TrustedDevice{}, errors.New("device trust store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	encoded, ok := s.file.Devices[strings.TrimSpace(deviceID)]
	if !ok {
		return TrustedDevice{}, errors.New("device_unknown")
	}
	if strings.TrimSpace(encoded.RevokedAt) != "" {
		return TrustedDevice{}, errors.New("device_revoked")
	}
	if !DeviceCredentialMatches(encoded.CredentialHash, credential) {
		return TrustedDevice{}, errors.New("device_unknown")
	}
	return encoded.toTrusted()
}

func (s *DeviceTrustStore) VerifyDeviceProof(deviceID string, publicKey []byte, transcript []byte, deviceProof []byte, deviceSignature []byte) (TrustedDevice, error) {
	if s == nil {
		return TrustedDevice{}, errors.New("device trust store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	device, err := s.trustedDeviceLocked(deviceID)
	if err != nil {
		return TrustedDevice{}, err
	}
	if subtle.ConstantTimeCompare(device.PublicKey, publicKey) != 1 {
		return TrustedDevice{}, errors.New("device_unknown")
	}
	verified, err := VerifyNodeSignature(device.PublicKey, transcript, deviceSignature)
	if err != nil {
		return TrustedDevice{}, errors.New("device_unknown")
	}
	if !verified || !DeviceProofMatchesCredentialHash(device.CredentialHash, transcript, deviceProof) {
		return TrustedDevice{}, errors.New("device_unknown")
	}
	return device, nil
}

func (s *DeviceTrustStore) MarkDeviceSeen(deviceID string, sessionID string, now time.Time) (TrustedDevice, string, error) {
	if s == nil {
		return TrustedDevice{}, "", errors.New("device trust store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	encoded, ok := s.file.Devices[strings.TrimSpace(deviceID)]
	if !ok {
		return TrustedDevice{}, "", errors.New("device_unknown")
	}
	if strings.TrimSpace(encoded.RevokedAt) != "" {
		return TrustedDevice{}, "", errors.New("device_revoked")
	}
	previous := encoded.ActiveSessionID
	if now.IsZero() {
		now = time.Now().UTC()
	}
	encoded.LastSeenAt = now.UTC().Format(time.RFC3339Nano)
	encoded.ActiveSessionID = strings.TrimSpace(sessionID)
	s.file.Devices[encoded.ID] = encoded
	if err := s.save(); err != nil {
		return TrustedDevice{}, "", err
	}
	device, err := encoded.toTrusted()
	return device, previous, err
}

func (s *DeviceTrustStore) trustedDeviceLocked(deviceID string) (TrustedDevice, error) {
	encoded, ok := s.file.Devices[strings.TrimSpace(deviceID)]
	if !ok {
		return TrustedDevice{}, errors.New("device_unknown")
	}
	if strings.TrimSpace(encoded.RevokedAt) != "" {
		return TrustedDevice{}, errors.New("device_revoked")
	}
	return encoded.toTrusted()
}

func (s *DeviceTrustStore) RevokeDevice(deviceID string, now time.Time) (TrustedDevice, error) {
	if s == nil {
		return TrustedDevice{}, errors.New("device trust store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	encoded, ok := s.file.Devices[strings.TrimSpace(deviceID)]
	if !ok {
		return TrustedDevice{}, errors.New("device_unknown")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	encoded.RevokedAt = now.UTC().Format(time.RFC3339Nano)
	encoded.ActiveSessionID = ""
	s.file.Devices[encoded.ID] = encoded
	if err := s.save(); err != nil {
		return TrustedDevice{}, err
	}
	return encoded.toTrusted()
}

func (s *DeviceTrustStore) ClearTrustedDevicesForNodeRotation() error {
	if s == nil {
		return errors.New("device trust store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.file.Devices = map[string]trustedDeviceFile{}
	return s.save()
}

func (s *DeviceTrustStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("read device trust store: %w", err)
	}
	var encoded deviceTrustFile
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return fmt.Errorf("parse device trust store: %w", err)
	}
	if encoded.Version != deviceTrustVersion {
		return fmt.Errorf("unsupported device trust store format")
	}
	if encoded.Devices == nil {
		encoded.Devices = map[string]trustedDeviceFile{}
	}
	for id, device := range encoded.Devices {
		if id != device.ID {
			return fmt.Errorf("device trust id mismatch")
		}
		if _, err := device.toTrusted(); err != nil {
			return err
		}
	}
	s.file = encoded
	return nil
}

func (s *DeviceTrustStore) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create device trust directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("secure device trust directory: %w", err)
	}
	raw, err := json.MarshalIndent(s.file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal device trust store: %w", err)
	}
	if err := os.WriteFile(s.path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write device trust store: %w", err)
	}
	return os.Chmod(s.path, 0o600)
}

func normalizeDeviceRegistration(reg DeviceRegistration) (DeviceRegistration, error) {
	reg.ID = strings.TrimSpace(reg.ID)
	reg.DisplayName = strings.TrimSpace(reg.DisplayName)
	reg.DeviceCredential = strings.TrimSpace(reg.DeviceCredential)
	if reg.ID == "" || reg.DisplayName == "" || reg.DeviceCredential == "" {
		return DeviceRegistration{}, errors.New("device id, display name, and credential are required")
	}
	if _, err := ecdh.P256().NewPublicKey(reg.PublicKey); err != nil {
		return DeviceRegistration{}, fmt.Errorf("invalid device public key: %w", err)
	}
	reg.PublicKey = append([]byte(nil), reg.PublicKey...)
	return reg, nil
}

func (d trustedDeviceFile) toTrusted() (TrustedDevice, error) {
	publicKey, err := base64.RawStdEncoding.DecodeString(d.PublicKeyBase64)
	if err != nil {
		return TrustedDevice{}, fmt.Errorf("decode device public key: %w", err)
	}
	if _, err := ecdh.P256().NewPublicKey(publicKey); err != nil {
		return TrustedDevice{}, fmt.Errorf("invalid device public key: %w", err)
	}
	fingerprint := Fingerprint(publicKey)
	if fmt.Sprintf("%x", fingerprint) != d.FingerprintHex {
		return TrustedDevice{}, fmt.Errorf("device fingerprint mismatch")
	}
	createdAt, err := parseDeviceTrustTime(d.CreatedAt)
	if err != nil {
		return TrustedDevice{}, err
	}
	lastSeenAt, err := parseDeviceTrustTime(d.LastSeenAt)
	if err != nil {
		return TrustedDevice{}, err
	}
	revokedAt, err := parseOptionalDeviceTrustTime(d.RevokedAt)
	if err != nil {
		return TrustedDevice{}, err
	}
	return TrustedDevice{
		ID: d.ID, DisplayName: d.DisplayName, PublicKey: publicKey,
		Fingerprint: fingerprint, CredentialHash: d.CredentialHash,
		CreatedAt: createdAt, LastSeenAt: lastSeenAt, RevokedAt: revokedAt,
		ActiveSessionID: d.ActiveSessionID,
	}, nil
}

func parseDeviceTrustTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse device trust timestamp: %w", err)
	}
	return parsed, nil
}

func parseOptionalDeviceTrustTime(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	return parseDeviceTrustTime(value)
}
