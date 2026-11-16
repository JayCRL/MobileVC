package main

import (
	"testing"
	"time"
)

func TestParseRelayFlags(t *testing.T) {
	overrides, showHelp, err := parseRelayFlags([]string{
		"--addr", ":9443",
		"--public-url", "wss://relay.example.test",
		"--pairing-ttl", "10m",
		"--agent-grace-period", "90s",
		"--ping-interval", "15s",
		"--pong-timeout", "5s",
		"--max-agent-conns", "7",
		"--max-client-conns", "8",
		"--max-conns-per-ip", "3",
		"--max-control-frame-bytes", "4096",
		"--max-payload-bytes", "8192",
		"--forward-queue-size", "9",
		"--trusted-proxy-cidrs", "127.0.0.1/32",
		"--http-allowlist", "GET:/healthz",
		"--ws-allowlist", "GET:/ws",
	})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if showHelp {
		t.Fatal("showHelp should be false")
	}
	if overrides.Addr != ":9443" ||
		overrides.PublicURL != "wss://relay.example.test" ||
		overrides.MaxAgentConns != 7 ||
		overrides.MaxClientConns != 8 ||
		overrides.MaxConnsPerIP != 3 ||
		overrides.MaxControlFrameBytes != 4096 ||
		overrides.MaxPayloadBytes != 8192 ||
		overrides.ForwardQueueSize != 9 ||
		overrides.TrustedProxyCIDRs != "127.0.0.1/32" ||
		overrides.HTTPAllowlist != "GET:/healthz" ||
		overrides.WSAllowlist != "GET:/ws" {
		t.Fatalf("unexpected overrides: %#v", overrides)
	}
	if overrides.PairingTTL != 10*time.Minute ||
		overrides.AgentGracePeriod != 90*time.Second ||
		overrides.PingInterval != 15*time.Second ||
		overrides.PongTimeout != 5*time.Second {
		t.Fatalf("unexpected durations: %#v", overrides)
	}
}

func TestParseRelayFlagsReadsE2EEModeOnlyWhenSet(t *testing.T) {
	defaultOverrides, _, err := parseRelayFlags(nil)
	if err != nil {
		t.Fatalf("parse default flags: %v", err)
	}
	if defaultOverrides.RequireE2EE != nil || defaultOverrides.PlaintextTestMode != nil {
		t.Fatalf("unset e2ee flags should not override env: %#v", defaultOverrides)
	}

	overrides, _, err := parseRelayFlags([]string{
		"--require-e2ee=false",
		"--plaintext-test-mode",
	})
	if err != nil {
		t.Fatalf("parse e2ee flags: %v", err)
	}
	if overrides.RequireE2EE == nil || *overrides.RequireE2EE {
		t.Fatalf("expected require e2ee false override: %#v", overrides.RequireE2EE)
	}
	if overrides.PlaintextTestMode == nil || !*overrides.PlaintextTestMode {
		t.Fatalf("expected plaintext test mode true override: %#v", overrides.PlaintextTestMode)
	}
}

func TestParseRelayFlagsRejectsInvalidDuration(t *testing.T) {
	if _, _, err := parseRelayFlags([]string{"--ping-interval", "bad"}); err == nil {
		t.Fatal("expected invalid duration to fail")
	}
}
