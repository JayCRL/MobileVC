package main

import (
	"testing"
	"time"

	"mobilevc/internal/config"
)

func TestParseServerFlags(t *testing.T) {
	flags, err := parseServerFlags([]string{
		"--port", "9001",
		"--auth-token", "token",
		"--network-exposure-mode", "relay-only",
		"--relay-mode=true",
		"--relay-url", "wss://relay.example.test",
		"--relay-pairing-event-path", "/tmp/pairing.json",
		"--relay-http-allowlist", "GET:/download",
		"--relay-ws-allowlist", "GET:/ws",
		"--relay-pairing-ttl", "30m",
		"--relay-agent-grace-period", "45s",
	})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if flags.overrides.Port != "9001" ||
		flags.overrides.AuthToken != "token" ||
		flags.overrides.NetworkExposureMode != config.ExposureModeRelayOnly ||
		flags.overrides.RelayURL != "wss://relay.example.test" ||
		flags.overrides.RelayPairingPath != "/tmp/pairing.json" ||
		flags.overrides.RelayHTTPAllowlist != "GET:/download" ||
		flags.overrides.RelayWSAllowlist != "GET:/ws" {
		t.Fatalf("unexpected overrides: %#v", flags.overrides)
	}
	if flags.overrides.RelayMode == nil || !*flags.overrides.RelayMode {
		t.Fatalf("relay mode override not set: %#v", flags.overrides.RelayMode)
	}
	if flags.overrides.RelayPairingTTL != 30*time.Minute ||
		flags.overrides.RelayAgentGrace != 45*time.Second {
		t.Fatalf("unexpected durations: %#v", flags.overrides)
	}
}

func TestParseServerFlagsRejectsInvalidDuration(t *testing.T) {
	if _, err := parseServerFlags([]string{"--relay-pairing-ttl", "bad"}); err == nil {
		t.Fatal("expected invalid duration to fail")
	}
}
