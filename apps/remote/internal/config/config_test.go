package config

import (
	"os"
	"testing"
)

func TestLoadTTSValidation(t *testing.T) {
	preserveConfigEnv(t)

	t.Run("disabled tts passes", func(t *testing.T) {
		os.Setenv("AUTH_TOKEN", "test")
		os.Setenv("TTS_ENABLED", "false")
		if _, err := Load(); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("enabled tts with invalid provider fails", func(t *testing.T) {
		os.Setenv("AUTH_TOKEN", "test")
		os.Setenv("TTS_ENABLED", "true")
		os.Setenv("TTS_PROVIDER", "other")
		os.Setenv("TTS_PYTHON_SERVICE_URL", "http://127.0.0.1:9966")
		os.Setenv("TTS_REQUEST_TIMEOUT_SECONDS", "30")
		os.Setenv("TTS_MAX_TEXT_LENGTH", "200")
		os.Setenv("TTS_DEFAULT_FORMAT", "wav")
		if _, err := Load(); err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("enabled tts with invalid format fails", func(t *testing.T) {
		os.Setenv("AUTH_TOKEN", "test")
		os.Setenv("TTS_ENABLED", "true")
		os.Setenv("TTS_PROVIDER", "chattts-http")
		os.Setenv("TTS_PYTHON_SERVICE_URL", "http://127.0.0.1:9966")
		os.Setenv("TTS_REQUEST_TIMEOUT_SECONDS", "30")
		os.Setenv("TTS_MAX_TEXT_LENGTH", "200")
		os.Setenv("TTS_DEFAULT_FORMAT", "mp3")
		if _, err := Load(); err == nil {
			t.Fatal("expected validation error")
		}
	})
}

func preserveConfigEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"AUTH_TOKEN",
		"NETWORK_EXPOSURE_MODE",
		"TTS_ENABLED",
		"TTS_PROVIDER",
		"TTS_PYTHON_SERVICE_URL",
		"TTS_REQUEST_TIMEOUT_SECONDS",
		"TTS_MAX_TEXT_LENGTH",
		"TTS_DEFAULT_FORMAT",
		"RELAY_MODE",
		"RELAY_URL",
		"RELAY_PAIRING_TTL",
		"RELAY_AGENT_GRACE_PERIOD",
		"RELAY_PAIRING_EVENT_PATH",
		"RELAY_HTTP_ALLOWLIST",
		"RELAY_WS_ALLOWLIST",
	}
	for _, key := range keys {
		preserveEnv(t, key)
	}
}

func preserveEnv(t *testing.T, key string) {
	t.Helper()
	prev, ok := os.LookupEnv(key)
	if ok {
		t.Cleanup(func() { os.Setenv(key, prev) })
		return
	}
	t.Cleanup(func() { os.Unsetenv(key) })
}

func TestLoadNetworkExposureMode(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantMode   string
		wantAddr   string
		wantHealth string
		wantWS     string
	}{
		{
			name:       "default lan",
			wantMode:   ExposureModeLAN,
			wantAddr:   ":8001",
			wantHealth: "http://localhost:8001/healthz",
			wantWS:     "ws://localhost:8001/ws?token=<redacted>",
		},
		{
			name:       "lan alias",
			raw:        "lan-enabled",
			wantMode:   ExposureModeLAN,
			wantAddr:   ":8001",
			wantHealth: "http://localhost:8001/healthz",
			wantWS:     "ws://localhost:8001/ws?token=<redacted>",
		},
		{
			name:       "relay only",
			raw:        "relay-only",
			wantMode:   ExposureModeRelayOnly,
			wantAddr:   "127.0.0.1:8001",
			wantHealth: "http://127.0.0.1:8001/healthz",
			wantWS:     "ws://127.0.0.1:8001/ws?token=<redacted>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := map[string]string{"AUTH_TOKEN": "test"}
			if tt.raw != "" {
				values["NETWORK_EXPOSURE_MODE"] = tt.raw
			}
			withEnv(t, values)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load failed: %v", err)
			}
			if cfg.Network.ExposureMode != tt.wantMode {
				t.Fatalf("exposure mode: got %q want %q", cfg.Network.ExposureMode, tt.wantMode)
			}
			if cfg.ListenAddress() != tt.wantAddr {
				t.Fatalf("listen address: got %q want %q", cfg.ListenAddress(), tt.wantAddr)
			}
			if cfg.HealthURL() != tt.wantHealth {
				t.Fatalf("health url: got %q want %q", cfg.HealthURL(), tt.wantHealth)
			}
			if cfg.WebSocketURL() != tt.wantWS {
				t.Fatalf("websocket url: got %q want %q", cfg.WebSocketURL(), tt.wantWS)
			}
		})
	}
}

func TestLoadRejectsInvalidNetworkExposureMode(t *testing.T) {
	withEnv(t, map[string]string{
		"AUTH_TOKEN":            "test",
		"NETWORK_EXPOSURE_MODE": "public",
	})
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid network exposure mode error")
	}
}

func TestLoadWithOverridesUsesCLIValues(t *testing.T) {
	withEnv(t, map[string]string{
		"AUTH_TOKEN":               "env-token",
		"NETWORK_EXPOSURE_MODE":    "lan",
		"RELAY_MODE":               "false",
		"RELAY_URL":                "",
		"RELAY_PAIRING_EVENT_PATH": "",
		"RELAY_HTTP_ALLOWLIST":     "",
		"RELAY_WS_ALLOWLIST":       "",
	})
	relayMode := true
	cfg, err := LoadWithOverrides(Overrides{
		Port:                "9001",
		AuthToken:           "cli-token",
		NetworkExposureMode: "relay-only",
		RelayMode:           &relayMode,
		RelayURL:            "wss://relay.example.test",
		RelayPairingPath:    "/tmp/mobilevc-pairing.json",
		RelayHTTPAllowlist:  "GET:/download",
		RelayWSAllowlist:    "GET:/ws",
	})
	if err != nil {
		t.Fatalf("LoadWithOverrides failed: %v", err)
	}
	if cfg.Port != "9001" ||
		cfg.AuthToken != "cli-token" ||
		cfg.Network.ExposureMode != ExposureModeRelayOnly ||
		cfg.ListenAddress() != "127.0.0.1:9001" ||
		cfg.HealthURL() != "http://127.0.0.1:9001/healthz" ||
		!cfg.Relay.Enabled ||
		cfg.Relay.URL != "wss://relay.example.test" ||
		cfg.Relay.PairingEventPath != "/tmp/mobilevc-pairing.json" ||
		cfg.Relay.HTTPAllowlist != "GET:/download" ||
		cfg.Relay.WSAllowlist != "GET:/ws" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestLoadRelaySelectedRouteAllowlists(t *testing.T) {
	withEnv(t, map[string]string{
		"AUTH_TOKEN":               "test",
		"RELAY_MODE":               "true",
		"RELAY_URL":                "wss://relay.example.com",
		"RELAY_PAIRING_EVENT_PATH": "/tmp/mobilevc-pairing.json",
		"RELAY_HTTP_ALLOWLIST":     "GET:/download",
		"RELAY_WS_ALLOWLIST":       "GET:/ws",
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Relay.HTTPAllowlist != "GET:/download" || cfg.Relay.WSAllowlist != "GET:/ws" {
		t.Fatalf("unexpected relay allowlists: %#v", cfg.Relay)
	}
}

func TestLoadRelayModeValidatesURL(t *testing.T) {
	valid := []string{
		"wss://relay.example.com",
		"ws://127.0.0.1:9000",
		"ws://localhost:9000",
		"ws://192.168.1.10:9000",
	}
	for _, relayURL := range valid {
		t.Run(relayURL, func(t *testing.T) {
			withEnv(t, map[string]string{
				"AUTH_TOKEN":               "test",
				"RELAY_MODE":               "true",
				"RELAY_URL":                relayURL,
				"RELAY_PAIRING_EVENT_PATH": "/tmp/mobilevc-pairing.json",
			})
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load failed: %v", err)
			}
			if !cfg.Relay.Enabled || cfg.Relay.URL != relayURL {
				t.Fatalf("Relay config mismatch: %#v", cfg.Relay)
			}
		})
	}
}

func TestLoadRelayModeRejectsUnsafeURL(t *testing.T) {
	invalid := []string{
		"",
		"http://relay.example.com",
		"https://relay.example.com",
		"ws://relay.example.com",
		"wss://relay.example.com/path",
	}
	for _, relayURL := range invalid {
		t.Run(relayURL, func(t *testing.T) {
			withEnv(t, map[string]string{
				"AUTH_TOKEN":               "test",
				"RELAY_MODE":               "true",
				"RELAY_URL":                relayURL,
				"RELAY_PAIRING_EVENT_PATH": "/tmp/mobilevc-pairing.json",
			})
			if _, err := Load(); err == nil {
				t.Fatal("expected relay validation error")
			}
		})
	}
}

func TestLoadRelayModeRequiresPairingEventPath(t *testing.T) {
	withEnv(t, map[string]string{
		"AUTH_TOKEN": "test",
		"RELAY_MODE": "true",
		"RELAY_URL":  "wss://relay.example.com",
	})

	if _, err := Load(); err == nil {
		t.Fatal("expected relay pairing event path validation error")
	}
}

func TestLoadRelayModeRejectsInvalidDuration(t *testing.T) {
	withEnv(t, map[string]string{
		"AUTH_TOKEN":               "test",
		"RELAY_MODE":               "true",
		"RELAY_URL":                "wss://relay.example.com",
		"RELAY_PAIRING_TTL":        "not-a-duration",
		"RELAY_PAIRING_EVENT_PATH": "/tmp/mobilevc-pairing.json",
	})

	if _, err := Load(); err == nil {
		t.Fatal("expected relay duration validation error")
	}
}

func withEnv(t *testing.T, values map[string]string) {
	t.Helper()
	for key, value := range values {
		prev, ok := os.LookupEnv(key)
		if ok {
			t.Cleanup(func() { os.Setenv(key, prev) })
		} else {
			t.Cleanup(func() { os.Unsetenv(key) })
		}
		os.Setenv(key, value)
	}
}
