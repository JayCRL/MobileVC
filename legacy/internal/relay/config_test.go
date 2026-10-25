package relay

import (
	"path/filepath"
	"testing"
)

func TestLoadConfigFromEnvRejectsInvalidDuration(t *testing.T) {
	withRelayEnv(t, map[string]string{
		"RELAY_PAIRING_TTL": "invalid",
	})

	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected invalid relay duration to fail")
	}
}

func TestLoadConfigFromEnvRejectsInvalidLimit(t *testing.T) {
	withRelayEnv(t, map[string]string{
		"RELAY_MAX_CONNS_PER_IP": "invalid",
	})

	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected invalid relay connection limit to fail")
	}
}

func TestLoadConfigFromEnvRejectsInvalidByteLimit(t *testing.T) {
	withRelayEnv(t, map[string]string{
		"RELAY_MAX_CONTROL_FRAME_BYTES": "invalid",
	})

	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected invalid relay byte limit to fail")
	}
}

func TestLoadConfigFromEnvUsesDefaultPayloadByteLimit(t *testing.T) {
	withRelayEnv(t, map[string]string{})

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.MaxPayloadBytes != 32*1024*1024 {
		t.Fatalf("payload bytes: got %d", cfg.MaxPayloadBytes)
	}
}

func TestLoadConfigFromEnvReadsPayloadByteLimit(t *testing.T) {
	withRelayEnv(t, map[string]string{
		"RELAY_MAX_PAYLOAD_BYTES": "12KiB",
	})

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.MaxPayloadBytes != 12*1024 {
		t.Fatalf("payload bytes: got %d", cfg.MaxPayloadBytes)
	}
}

func TestLoadConfigFromEnvUsesDefaultStatePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	withRelayEnv(t, map[string]string{})

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	want := filepath.Join(home, ".mobilevc", "relay", "public_relay_state.json")
	if cfg.StatePath != want {
		t.Fatalf("state path: got %q want %q", cfg.StatePath, want)
	}
}

func TestLoadConfigReadsStatePathOverride(t *testing.T) {
	withRelayEnv(t, map[string]string{
		"RELAY_STATE_PATH": "/tmp/mobilevc-relay-state.json",
	})

	cfg, err := LoadConfig(Overrides{StatePath: "/tmp/override-relay-state.json"})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.StatePath != "/tmp/override-relay-state.json" {
		t.Fatalf("state path override not applied: %#v", cfg)
	}
}

func TestLoadConfigFromEnvReadsE2EEMode(t *testing.T) {
	withRelayEnv(t, map[string]string{
		"RELAY_REQUIRE_E2EE":        "false",
		"RELAY_PLAINTEXT_TEST_MODE": "true",
	})

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.RequireE2EE || !cfg.PlaintextTestMode {
		t.Fatalf("unexpected e2ee config: %#v", cfg)
	}
}

func TestLoadConfigRejectsConflictingE2EEMode(t *testing.T) {
	withRelayEnv(t, map[string]string{
		"RELAY_REQUIRE_E2EE":        "true",
		"RELAY_PLAINTEXT_TEST_MODE": "true",
	})

	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected conflicting e2ee config to fail")
	}
}

func TestLoadConfigFromEnvReadsDefaultRouteAllowlists(t *testing.T) {
	withRelayEnv(t, map[string]string{})

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !routeAllowed(cfg.HTTPAllowedRoutes, "GET", "/healthz") {
		t.Fatal("default HTTP allowlist should include GET /healthz")
	}
	if !routeAllowed(cfg.WSAllowedRoutes, "GET", "/ws") {
		t.Fatal("default WS allowlist should include GET /ws")
	}
	if routeAllowed(cfg.HTTPAllowedRoutes, "POST", "/healthz") {
		t.Fatal("default HTTP allowlist should reject wrong method")
	}
}

func TestSelectedRoutePolicyValidatesEncryptedRoutes(t *testing.T) {
	policy := NewSelectedRoutePolicy(
		[]RouteRule{{Method: "GET", Path: "/download"}},
		[]RouteRule{{Method: "GET", Path: "/ws"}},
	)
	if err := policy.ValidateStream("file.download"); err != nil {
		t.Fatalf("file.download selected route rejected: %v", err)
	}
	if err := policy.ValidateStream("mobilevc.ws"); err != nil {
		t.Fatalf("mobilevc.ws selected route rejected: %v", err)
	}
	if policy.HTTPRouteAllowed("POST", "/download") {
		t.Fatal("selected route policy accepted wrong method")
	}
	trailingSlashPolicy := NewSelectedRoutePolicy(
		[]RouteRule{{Method: " get ", Path: "/download/"}},
		[]RouteRule{{Method: " get ", Path: "/ws/"}},
	)
	if err := trailingSlashPolicy.ValidateStream("file.download"); err != nil {
		t.Fatalf("file.download selected route should accept normalized allowlist path: %v", err)
	}
	if err := trailingSlashPolicy.ValidateStream("mobilevc.ws"); err != nil {
		t.Fatalf("mobilevc.ws selected route should accept normalized allowlist path: %v", err)
	}
	if err := policy.ValidateStream("shell.exec"); err == nil {
		t.Fatal("selected route policy accepted unsupported stream type")
	}
}

func TestSelectedRoutePolicyDeniesMissingRoutes(t *testing.T) {
	policy := NewSelectedRoutePolicy(
		[]RouteRule{{Method: "GET", Path: "/healthz"}},
		[]RouteRule{{Method: "GET", Path: "/events"}},
	)
	if err := policy.ValidateStream("file.download"); err == nil {
		t.Fatal("selected route policy accepted missing GET /download")
	}
	if err := policy.ValidateStream("mobilevc.ws"); err == nil {
		t.Fatal("selected route policy accepted missing GET /ws")
	}
}

func TestLoadConfigFromEnvRejectsInvalidRouteAllowlist(t *testing.T) {
	withRelayEnv(t, map[string]string{
		"RELAY_HTTP_ALLOWLIST": "GET:healthz",
	})

	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected invalid route allowlist to fail")
	}
}

func TestValidateRejectsEmptyRouteAllowlist(t *testing.T) {
	cfg := baseTestRelayConfig()
	cfg.HTTPAllowedRoutes = nil
	cfg.WSAllowedRoutes = nil

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected empty route allowlist validation error")
	}
}

func TestLoadConfigAppliesOverrides(t *testing.T) {
	withRelayEnv(t, map[string]string{
		"RELAY_ADDR":       ":9000",
		"RELAY_PUBLIC_URL": "ws://127.0.0.1:9000",
	})

	cfg, err := LoadConfig(Overrides{
		Addr:              ":9443",
		PublicURL:         "wss://relay.example.test",
		MaxAgentConns:     7,
		MaxClientConns:    8,
		MaxConnsPerIP:     3,
		ForwardQueueSize:  9,
		HTTPAllowlist:     "GET:/healthz",
		WSAllowlist:       "GET:/ws",
		TrustedProxyCIDRs: "127.0.0.1/32",
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Addr != ":9443" ||
		cfg.PublicURL != "wss://relay.example.test" ||
		cfg.MaxAgentConns != 7 ||
		cfg.MaxClientConns != 8 ||
		cfg.MaxConnsPerIP != 3 ||
		cfg.ForwardQueueSize != 9 ||
		cfg.TrustedProxyCIDRs != "127.0.0.1/32" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	if !routeAllowed(cfg.HTTPAllowedRoutes, "GET", "/healthz") ||
		routeAllowed(cfg.HTTPAllowedRoutes, "GET", "/version") {
		t.Fatalf("unexpected HTTP allowlist: %#v", cfg.HTTPAllowedRoutes)
	}
}

func withRelayEnv(t *testing.T, values map[string]string) {
	t.Helper()
	keys := []string{
		"RELAY_ADDR",
		"RELAY_PUBLIC_URL",
		"RELAY_PAIRING_TTL",
		"RELAY_MAX_CONNS_PER_IP",
		"RELAY_MAX_CONTROL_FRAME_BYTES",
		"RELAY_MAX_PAYLOAD_BYTES",
		"RELAY_HTTP_ALLOWLIST",
		"RELAY_WS_ALLOWLIST",
		"RELAY_STATE_PATH",
		"RELAY_REQUIRE_E2EE",
		"RELAY_PLAINTEXT_TEST_MODE",
	}
	for _, key := range keys {
		t.Setenv(key, "")
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
}
