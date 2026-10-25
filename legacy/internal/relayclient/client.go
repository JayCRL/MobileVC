package relayclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"mobilevc/internal/logx"
	"mobilevc/internal/relay"
	"mobilevc/internal/relay/e2ee"
)

type Config struct {
	RelayURL           string
	PairingTTL         time.Duration
	AgentGracePeriod   time.Duration
	PairingEventPath   string
	SessionStatePath   string
	ReconnectBackoff   ReconnectBackoff
	DownloadRoots      []string
	Capabilities       e2ee.CapabilitySet
	NodeFingerprintHex string
	NodeIdentity       *e2ee.NodeIdentity
	NodeIdentityStore  *e2ee.NodeIdentityStore
	DeviceTrust        *e2ee.DeviceTrustStore
	SelectedRoutes     relay.SelectedRoutePolicy
	LANHost            string
	LANPort            string
	LANToken           string
	LANCWD             string
	LANSecureTransport *bool
}

type ReconnectBackoff struct {
	Initial time.Duration
	Max     time.Duration
}

type PairingReadyEvent struct {
	Type               string             `json:"type"`
	RelayURL           string             `json:"relayUrl"`
	SessionID          string             `json:"sessionId"`
	PairingSecret      string             `json:"pairingSecret"`
	ExpiresAt          int64              `json:"expiresAt"`
	Capabilities       e2ee.CapabilitySet `json:"capabilities"`
	NodeFingerprintHex string             `json:"nodeFingerprintHex"`
	LANHost            string             `json:"lanHost,omitempty"`
	LANPort            string             `json:"lanPort,omitempty"`
	LANToken           string             `json:"lanToken,omitempty"`
	LANCWD             string             `json:"lanCwd,omitempty"`
	LANSecureTransport *bool              `json:"lanSecureTransport,omitempty"`
}

type LocalPairingEmitter func(string, PairingReadyEvent) error

const relayControlTimeout = 10 * time.Second

var (
	errRelaySessionRotated    = errors.New("relay session rotated")
	errAgentReconnectRejected = errors.New("relay agent reconnect rejected")
)

type agentRegisterRequest struct {
	SessionID       string
	PairSecret      string
	ReconnectSecret string
	ExpiresAt       time.Time
	Capabilities    e2ee.CapabilitySet
}

type agentReconnectRequest struct {
	SessionID       string
	ReconnectSecret string
}

func Run(ctx context.Context, cfg Config, handler Handler, emit LocalPairingEmitter) error {
	if err := validateConfig(cfg); err != nil {
		return err
	}
	for {
		err := runRegisteredSession(ctx, cfg, handler, emit)
		if errors.Is(err, errRelaySessionRotated) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			logx.Info("relay", "relay session rotated; registering a new relay session")
			continue
		}
		return err
	}
}

func runRegisteredSession(ctx context.Context, cfg Config, handler Handler, emit LocalPairingEmitter) error {
	store := newAgentSessionStore(cfg.SessionStatePath)
	saved, err := store.load()
	if err != nil {
		return err
	}
	if saved.valid() {
		err := runSavedSession(ctx, cfg, handler, saved)
		if errors.Is(err, errRelaySessionRotated) {
			if deleteErr := store.delete(); deleteErr != nil {
				return deleteErr
			}
			return err
		}
		if errors.Is(err, errAgentReconnectRejected) {
			if deleteErr := store.delete(); deleteErr != nil {
				return deleteErr
			}
			logx.Warn("relay", "saved relay session was rejected; registering new relay session")
		} else if err == nil || ctx.Err() != nil {
			return err
		} else {
			return err
		}
	}
	pairingSecret, err := relay.NewSecret()
	if err != nil {
		return err
	}
	reconnectSecret, err := relay.NewSecret()
	if err != nil {
		return err
	}
	sessionID := "rs_" + uuid.NewString()
	expiresAt := time.Now().Add(cfg.PairingTTL)
	req := agentRegisterRequest{
		SessionID:       sessionID,
		PairSecret:      pairingSecret,
		ReconnectSecret: reconnectSecret,
		ExpiresAt:       expiresAt,
		Capabilities:    relayClientCapabilities(cfg),
	}
	conn, err := connectAndRegister(ctx, cfg, req)
	if err != nil {
		return err
	}
	nodeFingerprintHex := currentNodeFingerprintHex(cfg)
	if nodeFingerprintHex == "" {
		_ = conn.Close()
		return fmt.Errorf("relay node fingerprint is required")
	}
	if err := store.save(agentSessionState{
		SessionID:       sessionID,
		ReconnectSecret: reconnectSecret,
	}); err != nil {
		_ = conn.Close()
		return err
	}
	if err := emit(cfg.PairingEventPath, PairingReadyEvent{
		Type:               "mobilevc.relay.pairing_ready",
		RelayURL:           cfg.RelayURL,
		SessionID:          sessionID,
		PairingSecret:      pairingSecret,
		ExpiresAt:          expiresAt.Unix(),
		Capabilities:       req.Capabilities,
		NodeFingerprintHex: nodeFingerprintHex,
		LANHost:            cfg.LANHost,
		LANPort:            cfg.LANPort,
		LANToken:           cfg.LANToken,
		LANCWD:             cfg.LANCWD,
		LANSecureTransport: cfg.LANSecureTransport,
	}); err != nil {
		_ = conn.Close()
		_ = store.delete()
		return err
	}
	defer removePairingEventFile(cfg.PairingEventPath)
	err = serveWithReconnect(ctx, cfg, handler, conn, sessionID, pairingSecret, reconnectSecret)
	if errors.Is(err, errRelaySessionRotated) {
		if deleteErr := store.delete(); deleteErr != nil {
			return deleteErr
		}
	}
	return err
}

func runSavedSession(ctx context.Context, cfg Config, handler Handler, saved agentSessionState) error {
	conn, err := reconnectUntilAccepted(
		ctx,
		cfg,
		saved.SessionID,
		saved.ReconnectSecret,
	)
	if err != nil {
		return err
	}
	return serveWithReconnect(ctx, cfg, handler, conn, saved.SessionID, "", saved.ReconnectSecret)
}

func relayClientCapabilities(cfg Config) e2ee.CapabilitySet {
	if cfg.Capabilities.RelayProtocolVersion != 0 {
		return cfg.Capabilities
	}
	return e2ee.PlaintextTestCapabilities()
}

func validateConfig(cfg Config) error {
	if err := relay.ValidateRelayURL(cfg.RelayURL); err != nil {
		return err
	}
	if cfg.PairingTTL <= 0 || cfg.AgentGracePeriod <= 0 {
		return fmt.Errorf("relay pairing ttl and grace period must be positive")
	}
	if strings.TrimSpace(cfg.PairingEventPath) == "" {
		return fmt.Errorf("relay pairing event path is required")
	}
	if e2eeRequired(relayClientCapabilities(cfg)) {
		roots, err := validateDownloadRoots(cfg.DownloadRoots)
		if err != nil {
			return fmt.Errorf("validate relay download roots: %w", err)
		}
		if len(roots) == 0 {
			return fmt.Errorf("relay download root is required for e2ee mode")
		}
	}
	if currentNodeFingerprintHex(cfg) == "" {
		return fmt.Errorf("relay node fingerprint is required")
	}
	if err := e2ee.ValidateProductionCapabilities(relayClientCapabilities(cfg)); err == nil && cfg.NodeIdentity == nil {
		if cfg.NodeIdentityStore == nil {
			return fmt.Errorf("relay node identity is required for e2ee mode")
		}
		if _, err := cfg.NodeIdentityStore.Current(); err != nil {
			return fmt.Errorf("load relay node identity: %w", err)
		}
	}
	return nil
}

func e2eeRequired(capabilities e2ee.CapabilitySet) bool {
	return e2ee.ValidateProductionCapabilities(capabilities) == nil
}

func selectedRoutePolicy(cfg Config) relay.SelectedRoutePolicy {
	if cfg.SelectedRoutes.IsZero() {
		return relay.DefaultSelectedRoutePolicy()
	}
	return relay.NewSelectedRoutePolicy(cfg.SelectedRoutes.HTTPAllowedRoutes, cfg.SelectedRoutes.WSAllowedRoutes)
}

func DefaultDownloadRoot(workspaceRoot string) (string, error) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	evaluated, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", err
	}
	return evaluated, nil
}

func currentNodeIdentity(cfg Config) (*e2ee.NodeIdentity, error) {
	if cfg.NodeIdentityStore != nil {
		return cfg.NodeIdentityStore.Current()
	}
	if cfg.NodeIdentity != nil {
		return cfg.NodeIdentity, nil
	}
	return nil, fmt.Errorf("relay node identity is required for e2ee mode")
}

func currentNodeFingerprintHex(cfg Config) string {
	if identity, err := currentNodeIdentity(cfg); err == nil && identity != nil {
		return fmt.Sprintf("%x", identity.Fingerprint)
	}
	if isFingerprintHex(cfg.NodeFingerprintHex) {
		return strings.TrimSpace(cfg.NodeFingerprintHex)
	}
	return ""
}

func isFingerprintHex(value string) bool {
	normalized := strings.TrimSpace(value)
	if len(normalized) != 64 {
		return false
	}
	for _, char := range normalized {
		if (char >= '0' && char <= '9') ||
			(char >= 'a' && char <= 'f') ||
			(char >= 'A' && char <= 'F') {
			continue
		}
		return false
	}
	return true
}

func connectAndRegister(ctx context.Context, cfg Config, req agentRegisterRequest) (*websocket.Conn, error) {
	conn, err := dialAgent(ctx, cfg.RelayURL)
	if err != nil {
		return nil, err
	}
	if err := registerAgent(conn, req); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func dialAgent(ctx context.Context, rawURL string) (*websocket.Conn, error) {
	endpoint, err := relayEndpoint(rawURL, "/relay/agent")
	if err != nil {
		return nil, err
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("connect relay agent websocket: %w", err)
	}
	return conn, nil
}

func relayEndpoint(rawURL string, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", err
	}
	parsed.Path = path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func registerAgent(conn *websocket.Conn, req agentRegisterRequest) error {
	frame := relay.AgentRegisterFrame{
		Type:                     relay.TypeAgentRegister,
		Version:                  relay.Version,
		SessionID:                req.SessionID,
		PairingSecretHash:        relay.SecretHash(req.PairSecret),
		AgentReconnectSecretHash: relay.SecretHash(req.ReconnectSecret),
		PairingExpiresAt:         req.ExpiresAt.Unix(),
		AgentName:                localAgentName(),
		AgentSystem:              runtime.GOOS,
		Capabilities:             &req.Capabilities,
	}
	if err := writeControlJSON(conn, frame); err != nil {
		return fmt.Errorf("send relay agent registration: %w", err)
	}
	var registered relay.AgentRegisteredFrame
	if err := readControlJSON(conn, &registered); err != nil {
		return fmt.Errorf("read relay agent registration response: %w", err)
	}
	if registered.Type != relay.TypeAgentRegistered || registered.SessionID != req.SessionID {
		return fmt.Errorf("relay agent registration rejected")
	}
	return nil
}

func reconnectAgent(conn *websocket.Conn, req agentReconnectRequest) error {
	frame := relay.AgentReconnectFrame{
		Type:                 relay.TypeAgentReconnect,
		Version:              relay.Version,
		SessionID:            req.SessionID,
		AgentReconnectSecret: req.ReconnectSecret,
		AgentName:            localAgentName(),
		AgentSystem:          runtime.GOOS,
	}
	if err := writeControlJSON(conn, frame); err != nil {
		return fmt.Errorf("send relay agent reconnect: %w", err)
	}
	var registered relay.AgentRegisteredFrame
	if err := readControlJSON(conn, &registered); err != nil {
		return fmt.Errorf("read relay agent reconnect response: %w", err)
	}
	if registered.Type != relay.TypeAgentRegistered || registered.SessionID != req.SessionID {
		return errAgentReconnectRejected
	}
	return nil
}

func localAgentName() string {
	return "MobileVC Backend"
}

func writeControlJSON(conn *websocket.Conn, frame any) error {
	if err := conn.SetWriteDeadline(time.Now().Add(relayControlTimeout)); err != nil {
		return err
	}
	if err := conn.WriteJSON(frame); err != nil {
		return err
	}
	return conn.SetWriteDeadline(time.Time{})
}

func readControlJSON(conn *websocket.Conn, frame any) error {
	if err := conn.SetReadDeadline(time.Now().Add(relayControlTimeout)); err != nil {
		return err
	}
	if err := conn.ReadJSON(frame); err != nil {
		return err
	}
	return conn.SetReadDeadline(time.Time{})
}

func EmitPairingFile(path string, event PairingReadyEvent) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal pairing event: %w", err)
	}
	if err := writeFileAtomic(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write pairing event file: %w", err)
	}
	return nil
}

func removePairingEventFile(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		logx.Warn("relay", "remove pairing event file failed: %v", err)
	}
}
