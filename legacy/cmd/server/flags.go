package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"mobilevc/internal/config"
)

type serverFlags struct {
	overrides config.Overrides
	showHelp  bool
}

func parseServerFlags(args []string) (serverFlags, error) {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var flags serverFlags
	var relayMode bool
	var relayModeSet bool
	var relayTTL string
	var relayGrace string
	fs.StringVar(&flags.overrides.Port, "port", "", "HTTP port")
	fs.StringVar(&flags.overrides.AuthToken, "auth-token", "", "AUTH_TOKEN value")
	fs.StringVar(&flags.overrides.NetworkExposureMode, "network-exposure-mode", "", "lan or relay-only")
	fs.BoolFunc("relay-mode", "enable or disable relay mode", func(value string) error {
		parsed, err := parseFlagBool(value)
		if err != nil {
			return err
		}
		relayMode = parsed
		relayModeSet = true
		return nil
	})
	fs.StringVar(&flags.overrides.RelayURL, "relay-url", "", "relay ws:// or wss:// URL")
	fs.StringVar(&flags.overrides.RelayPairingPath, "relay-pairing-event-path", "", "owner-only relay pairing event JSON path")
	fs.StringVar(&flags.overrides.RelaySessionPath, "relay-agent-session-state-path", "", "owner-only relay agent session state JSON path")
	fs.StringVar(&flags.overrides.RelayHTTPAllowlist, "relay-http-allowlist", "", "comma-separated METHOD:/path selected HTTP routes")
	fs.StringVar(&flags.overrides.RelayWSAllowlist, "relay-ws-allowlist", "", "comma-separated METHOD:/path selected websocket routes")
	fs.StringVar(&relayTTL, "relay-pairing-ttl", "", "relay pairing TTL, e.g. 30m")
	fs.StringVar(&relayGrace, "relay-agent-grace-period", "", "relay agent reconnect grace period, e.g. 60s")
	fs.BoolVar(&flags.showHelp, "help", false, "show help")
	if err := fs.Parse(args); err != nil {
		return serverFlags{}, err
	}
	if flags.showHelp {
		fs.Usage()
		return flags, nil
	}
	if relayModeSet {
		flags.overrides.RelayMode = &relayMode
	}
	var err error
	if relayTTL != "" {
		flags.overrides.RelayPairingTTL, err = time.ParseDuration(relayTTL)
		if err != nil {
			return serverFlags{}, fmt.Errorf("--relay-pairing-ttl must be a valid duration: %w", err)
		}
	}
	if relayGrace != "" {
		flags.overrides.RelayAgentGrace, err = time.ParseDuration(relayGrace)
		if err != nil {
			return serverFlags{}, fmt.Errorf("--relay-agent-grace-period must be a valid duration: %w", err)
		}
	}
	return flags, nil
}
