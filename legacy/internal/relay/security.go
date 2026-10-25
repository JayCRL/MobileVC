package relay

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strings"
)

const secretBytes = 32

func NewSecret() (string, error) {
	bytes := make([]byte, secretBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate relay secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func SecretHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func SecretHashMatches(hash string, secret string) bool {
	actual := SecretHash(secret)
	if len(hash) != len(actual) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hash), []byte(actual)) == 1
}

func ValidateRelayURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid relay url: %w", err)
	}
	if parsed.Scheme == "wss" {
		return validateRelayURLShape(parsed)
	}
	if parsed.Scheme != "ws" {
		return fmt.Errorf("relay url must use ws:// or wss://")
	}
	if err := validateRelayURLShape(parsed); err != nil {
		return err
	}
	if !isDevelopmentHost(parsed.Hostname()) {
		return fmt.Errorf("ws:// relay urls are allowed only for loopback or LAN hosts")
	}
	return nil
}

func validateRelayURLShape(parsed *url.URL) error {
	if parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("invalid relay url: expected scheme://host[:port]")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("invalid relay url: path is not allowed")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("invalid relay url: query and fragment are not allowed")
	}
	return nil
}

func isDevelopmentHost(host string) bool {
	normalized := strings.ToLower(strings.Trim(host, "[]"))
	if normalized == "localhost" {
		return true
	}
	ip := net.ParseIP(normalized)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || isIPv6Development(ip)
}

func isIPv6Development(ip net.IP) bool {
	return ip.IsLinkLocalUnicast() || strings.HasPrefix(strings.ToLower(ip.String()), "fc") ||
		strings.HasPrefix(strings.ToLower(ip.String()), "fd")
}
