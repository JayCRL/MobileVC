package relay

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"mobilevc/internal/relay/e2ee"
)

func newTestRelayServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newLimitedTestRelayServer(t, Config{})
}

func newLimitedTestRelayServer(t *testing.T, overrides Config) *httptest.Server {
	t.Helper()
	_, server := newInspectableRelayServer(t, overrides)
	return server
}

func newInspectableRelayServer(t *testing.T, overrides Config) (*Server, *httptest.Server) {
	t.Helper()
	cfg := baseTestRelayConfig()
	applyTestOverrides(&cfg, overrides)
	relayServer, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return relayServer, httptest.NewServer(relayServer.Handler("test"))
}

func baseTestRelayConfig() Config {
	return Config{
		Addr:                    ":0",
		PublicURL:               "ws://127.0.0.1:9000",
		PairingTTL:              time.Minute,
		AgentGracePeriod:        time.Minute,
		PairingHandshakeTimeout: time.Second,
		AgentRegisterTimeout:    time.Second,
		MaxPairingFailuresPerIP: 5,
		MaxSessions:             10,
		MaxAgentConns:           10,
		MaxClientConns:          10,
		MaxConnsPerIP:           10,
		PingInterval:            time.Minute,
		PongTimeout:             time.Minute,
		MaxControlFrameBytes:    16 * 1024,
		MaxPayloadBytes:         defaultMaxPayloadBytes,
		ForwardQueueSize:        4,
		PlaintextTestMode:       true,
		HTTPAllowedRoutes: []RouteRule{
			{Method: http.MethodGet, Path: "/healthz"},
			{Method: http.MethodGet, Path: "/version"},
			{Method: http.MethodGet, Path: "/download"},
		},
		WSAllowedRoutes: []RouteRule{
			{Method: http.MethodGet, Path: "/ws"},
		},
	}
}

func applyTestOverrides(cfg *Config, overrides Config) {
	if overrides.MaxConnsPerIP > 0 {
		cfg.MaxConnsPerIP = overrides.MaxConnsPerIP
	}
	if overrides.AgentGracePeriod > 0 {
		cfg.AgentGracePeriod = overrides.AgentGracePeriod
	}
	if overrides.MaxPayloadBytes > 0 {
		cfg.MaxPayloadBytes = overrides.MaxPayloadBytes
	}
	if overrides.TrustedProxyCIDRs != "" {
		cfg.TrustedProxyCIDRs = overrides.TrustedProxyCIDRs
	}
	if overrides.StatePath != "" {
		cfg.StatePath = overrides.StatePath
	}
	if overrides.RequireE2EE {
		cfg.RequireE2EE = true
		cfg.PlaintextTestMode = false
	}
}

func dialRelay(t *testing.T, baseURL string, path string) *websocket.Conn {
	t.Helper()
	return dialRelayWithHeader(t, baseURL, path, nil)
}

func dialRelayWithHeader(t *testing.T, baseURL string, path string, header http.Header) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + path
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	return conn
}

func registerAgent(t *testing.T, conn *websocket.Conn, sessionID string, pairSecret string, reconnectSecret string, expiresAt time.Time) {
	t.Helper()
	registerAgentWithCapabilities(t, conn, sessionID, pairSecret, reconnectSecret, expiresAt, testAgentCapabilities())
}

func registerAgentWithCapabilities(
	t *testing.T,
	conn *websocket.Conn,
	sessionID string,
	pairSecret string,
	reconnectSecret string,
	expiresAt time.Time,
	capabilities *e2ee.CapabilitySet,
) {
	t.Helper()
	err := conn.WriteJSON(AgentRegisterFrame{
		Type:                     TypeAgentRegister,
		Version:                  Version,
		SessionID:                sessionID,
		PairingSecretHash:        SecretHash(pairSecret),
		AgentReconnectSecretHash: SecretHash(reconnectSecret),
		PairingExpiresAt:         expiresAt.Unix(),
		Capabilities:             capabilities,
	})
	if err != nil {
		t.Fatalf("register agent: %v", err)
	}
	var registered AgentRegisteredFrame
	if err := conn.ReadJSON(&registered); err != nil {
		t.Fatalf("read registered: %v", err)
	}
}

func testAgentCapabilities() *e2ee.CapabilitySet {
	capabilities := e2ee.PlaintextTestCapabilities()
	return &capabilities
}

func productionAgentCapabilities() *e2ee.CapabilitySet {
	capabilities := e2ee.ProductionCapabilities()
	return &capabilities
}

func pairClient(t *testing.T, conn *websocket.Conn, sessionID string, secret string) string {
	t.Helper()
	paired := pairClientWithFrame(t, conn, sessionID, secret)
	return paired.ClientID
}

func pairClientWithReconnectSecret(t *testing.T, conn *websocket.Conn, sessionID string, secret string) (string, string) {
	t.Helper()
	paired := pairClientWithFrame(t, conn, sessionID, secret)
	if paired.ClientReconnectSecret == "" {
		t.Fatal("missing client reconnect secret")
	}
	return paired.ClientID, paired.ClientReconnectSecret
}

func pairClientWithFrame(t *testing.T, conn *websocket.Conn, sessionID string, secret string) ClientPairedFrame {
	t.Helper()
	if err := conn.WriteJSON(ClientPairFrame{
		Type: TypeClientPair, Version: Version, SessionID: sessionID, PairingSecret: secret,
	}); err != nil {
		t.Fatalf("pair client: %v", err)
	}
	var paired ClientPairedFrame
	if err := conn.ReadJSON(&paired); err != nil {
		t.Fatalf("read paired: %v", err)
	}
	return paired
}

func testEnvelope(sessionID string, clientID string, direction string, payload []byte) ForwardEnvelope {
	return ForwardEnvelope{
		Type:            TypeRelayForward,
		Version:         Version,
		SessionID:       sessionID,
		ClientID:        clientID,
		Direction:       direction,
		MessageID:       "msg-1",
		ContentType:     ContentTypeMobileVC,
		Encryption:      EncryptionNone,
		PayloadEncoding: PayloadBase64URL,
		Payload:         base64.RawURLEncoding.EncodeToString(payload),
	}
}
