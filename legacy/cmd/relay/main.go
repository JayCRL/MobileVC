package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"mobilevc/internal/logx"
	"mobilevc/internal/relay"
)

var version = "dev"

func main() {
	logx.Info("relay", "starting MobileVC relay")
	overrides, showHelp, err := parseRelayFlags(os.Args[1:])
	if err != nil {
		logx.Error("relay", "parse flags failed: %v", err)
		panic(err)
	}
	if showHelp {
		return
	}
	cfg, err := relay.LoadConfig(overrides)
	if err != nil {
		logx.Error("relay", "load relay config failed: %v", err)
		panic(err)
	}
	server, err := relay.NewServer(cfg)
	if err != nil {
		logx.Error("relay", "initialize relay failed: %v", err)
		panic(err)
	}
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		logx.Error("relay", "listen failed: %v", err)
		panic(fmt.Errorf("listen tcp %s: %w", cfg.Addr, err))
	}
	logx.Info("relay", "ready: addr=%s publicURL=%s", cfg.Addr, cfg.PublicURL)
	if err := http.Serve(listener, server.Handler(version)); err != nil {
		logx.Error("relay", "server stopped unexpectedly: %v", err)
		panic(err)
	}
}

func parseRelayFlags(args []string) (relay.Overrides, bool, error) {
	fs := flag.NewFlagSet("relay-server", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var overrides relay.Overrides
	var showHelp bool
	var pairingTTL, gracePeriod, handshakeTimeout, registerTimeout, pingInterval, pongTimeout string
	fs.StringVar(&overrides.Addr, "addr", "", "listen address, e.g. :9000")
	fs.StringVar(&overrides.PublicURL, "public-url", "", "public relay ws:// or wss:// URL")
	fs.StringVar(&overrides.StatePath, "state-path", "", "owner-only relay state JSON path for device reconnect across relay restarts")
	fs.StringVar(&pairingTTL, "pairing-ttl", "", "pairing TTL, e.g. 5m")
	fs.StringVar(&gracePeriod, "agent-grace-period", "", "agent reconnect grace period, e.g. 60s")
	fs.StringVar(&handshakeTimeout, "pairing-handshake-timeout", "", "client pairing handshake timeout, e.g. 10s")
	fs.StringVar(&registerTimeout, "agent-register-timeout", "", "agent register timeout, e.g. 10s")
	fs.IntVar(&overrides.MaxPairingFailuresPerIP, "max-pairing-failures-per-ip", 0, "max pairing failures per remote IP")
	fs.IntVar(&overrides.MaxSessions, "max-sessions", 0, "max relay sessions")
	fs.IntVar(&overrides.MaxAgentConns, "max-agent-conns", 0, "max agent websocket connections")
	fs.IntVar(&overrides.MaxClientConns, "max-client-conns", 0, "max client websocket connections")
	fs.IntVar(&overrides.MaxConnsPerIP, "max-conns-per-ip", 0, "max websocket connections per IP")
	fs.StringVar(&pingInterval, "ping-interval", "", "websocket ping interval, e.g. 30s")
	fs.StringVar(&pongTimeout, "pong-timeout", "", "websocket pong timeout, e.g. 10s")
	fs.Int64Var(&overrides.MaxControlFrameBytes, "max-control-frame-bytes", 0, "max control frame bytes")
	fs.IntVar(&overrides.MaxPayloadBytes, "max-payload-bytes", 0, "max decoded forward payload bytes")
	fs.IntVar(&overrides.ForwardQueueSize, "forward-queue-size", 0, "bounded forward queue size per peer")
	fs.StringVar(&overrides.TrustedProxyCIDRs, "trusted-proxy-cidrs", "", "comma-separated trusted proxy CIDRs")
	fs.StringVar(&overrides.HTTPAllowlist, "http-allowlist", "", "comma-separated METHOD:/path HTTP allowlist")
	fs.StringVar(&overrides.WSAllowlist, "ws-allowlist", "", "comma-separated METHOD:/path websocket allowlist")
	requireE2EE := optionalBoolFlag(fs, "require-e2ee", "reject plaintext relay.forward frames")
	plaintextTestMode := optionalBoolFlag(fs, "plaintext-test-mode", "allow plaintext relay.forward frames for local test mode only")
	fs.BoolVar(&showHelp, "help", false, "show help")
	if err := fs.Parse(args); err != nil {
		return relay.Overrides{}, false, err
	}
	if showHelp {
		fs.Usage()
		return overrides, true, nil
	}
	durations := []struct {
		raw string
		set func(time.Duration)
		key string
	}{
		{pairingTTL, func(v time.Duration) { overrides.PairingTTL = v }, "--pairing-ttl"},
		{gracePeriod, func(v time.Duration) { overrides.AgentGracePeriod = v }, "--agent-grace-period"},
		{handshakeTimeout, func(v time.Duration) { overrides.PairingHandshakeTimeout = v }, "--pairing-handshake-timeout"},
		{registerTimeout, func(v time.Duration) { overrides.AgentRegisterTimeout = v }, "--agent-register-timeout"},
		{pingInterval, func(v time.Duration) { overrides.PingInterval = v }, "--ping-interval"},
		{pongTimeout, func(v time.Duration) { overrides.PongTimeout = v }, "--pong-timeout"},
	}
	for _, duration := range durations {
		if duration.raw == "" {
			continue
		}
		parsed, err := time.ParseDuration(duration.raw)
		if err != nil {
			return relay.Overrides{}, false, fmt.Errorf("%s must be a valid duration: %w", duration.key, err)
		}
		duration.set(parsed)
	}
	if requireE2EE.IsSet() {
		value := requireE2EE.Value()
		overrides.RequireE2EE = &value
	}
	if plaintextTestMode.IsSet() {
		value := plaintextTestMode.Value()
		overrides.PlaintextTestMode = &value
	}
	return overrides, false, nil
}

type optionalBool struct {
	value bool
	set   bool
}

func optionalBoolFlag(fs *flag.FlagSet, name string, usage string) *optionalBool {
	flagValue := &optionalBool{}
	fs.Var(flagValue, name, usage)
	return flagValue
}

func (b *optionalBool) Set(raw string) error {
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return fmt.Errorf("must be a valid boolean: %w", err)
	}
	b.value = parsed
	b.set = true
	return nil
}

func (b *optionalBool) String() string {
	return strconv.FormatBool(b.value)
}

func (b *optionalBool) IsBoolFlag() bool {
	return true
}

func (b *optionalBool) IsSet() bool {
	return b.set
}

func (b *optionalBool) Value() bool {
	return b.value
}
