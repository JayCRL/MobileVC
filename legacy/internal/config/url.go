package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

func ValidateRelayURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid relay url %q: %w", raw, err)
	}
	switch parsed.Scheme {
	case "wss":
		return validateRelayURLShape(parsed, raw)
	case "ws":
		return validateInsecureRelayURL(parsed, raw)
	default:
		return fmt.Errorf("relay url must use ws:// or wss://")
	}
}

func validateInsecureRelayURL(parsed *url.URL, raw string) error {
	if err := validateRelayURLShape(parsed, raw); err != nil {
		return err
	}
	if !isInsecureRelayHostAllowed(parsed.Hostname()) {
		return fmt.Errorf("ws:// relay urls are allowed only for loopback or LAN hosts")
	}
	return nil
}

func validateRelayURLShape(parsed *url.URL, raw string) error {
	if parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("invalid relay url %q: expected scheme://host[:port]", raw)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("invalid relay url %q: path is not allowed", raw)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("invalid relay url %q: query and fragment are not allowed", raw)
	}
	return nil
}

func isInsecureRelayHostAllowed(host string) bool {
	normalized := strings.ToLower(strings.Trim(host, "[]"))
	if normalized == "localhost" {
		return true
	}
	ip := net.ParseIP(normalized)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		strings.HasPrefix(strings.ToLower(ip.String()), "fc") ||
		strings.HasPrefix(strings.ToLower(ip.String()), "fd")
}
