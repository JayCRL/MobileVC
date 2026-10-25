package relay

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAddr                    = ":9000"
	defaultPublicURL               = "ws://127.0.0.1:9000"
	defaultPairingTTL              = 5 * time.Minute
	defaultAgentGracePeriod        = 60 * time.Second
	defaultHandshakeTimeout        = 10 * time.Second
	defaultAgentRegisterTimeout    = 10 * time.Second
	defaultMaxPairingFailuresPerIP = 5
	defaultMaxSessions             = 1000
	defaultMaxAgentConns           = 1000
	defaultMaxClientConns          = 2000
	defaultMaxConnsPerIP           = 20
	defaultPingInterval            = 30 * time.Second
	defaultPongTimeout             = 10 * time.Second
	defaultMaxControlFrameBytes    = 16 * 1024
	defaultMaxPayloadBytes         = 32 * 1024 * 1024
	defaultForwardQueueSize        = 64
	defaultHTTPAllowedRoutes       = "GET:/healthz,GET:/version,GET:/download"
	defaultWSAllowedRoutes         = "GET:/ws"
	defaultRequireE2EE             = true
	defaultPlaintextTestMode       = false
	defaultStateFileName           = "public_relay_state.json"
)

type RouteRule struct {
	Method string
	Path   string
}

type Config struct {
	Addr                    string
	PublicURL               string
	StatePath               string
	PairingTTL              time.Duration
	AgentGracePeriod        time.Duration
	PairingHandshakeTimeout time.Duration
	AgentRegisterTimeout    time.Duration
	MaxPairingFailuresPerIP int
	MaxSessions             int
	MaxAgentConns           int
	MaxClientConns          int
	MaxConnsPerIP           int
	PingInterval            time.Duration
	PongTimeout             time.Duration
	MaxControlFrameBytes    int64
	MaxPayloadBytes         int
	ForwardQueueSize        int
	TrustedProxyCIDRs       string
	HTTPAllowedRoutes       []RouteRule
	WSAllowedRoutes         []RouteRule
	RequireE2EE             bool
	PlaintextTestMode       bool
}

type Overrides struct {
	Addr                    string
	PublicURL               string
	StatePath               string
	PairingTTL              time.Duration
	AgentGracePeriod        time.Duration
	PairingHandshakeTimeout time.Duration
	AgentRegisterTimeout    time.Duration
	MaxPairingFailuresPerIP int
	MaxSessions             int
	MaxAgentConns           int
	MaxClientConns          int
	MaxConnsPerIP           int
	PingInterval            time.Duration
	PongTimeout             time.Duration
	MaxControlFrameBytes    int64
	MaxPayloadBytes         int
	ForwardQueueSize        int
	TrustedProxyCIDRs       string
	HTTPAllowlist           string
	WSAllowlist             string
	RequireE2EE             *bool
	PlaintextTestMode       *bool
}

func LoadConfigFromEnv() (Config, error) {
	return LoadConfig(Overrides{})
}

func LoadConfig(overrides Overrides) (Config, error) {
	cfg, err := loadConfigFromEnv()
	if err != nil {
		return Config{}, err
	}
	if err := applyOverrides(&cfg, overrides); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyOverrides(cfg *Config, overrides Overrides) error {
	if strings.TrimSpace(overrides.Addr) != "" {
		cfg.Addr = strings.TrimSpace(overrides.Addr)
	}
	if strings.TrimSpace(overrides.PublicURL) != "" {
		cfg.PublicURL = strings.TrimSpace(overrides.PublicURL)
	}
	if strings.TrimSpace(overrides.StatePath) != "" {
		cfg.StatePath = strings.TrimSpace(overrides.StatePath)
	}
	applyDurationOverrides(cfg, overrides)
	applyIntegerOverrides(cfg, overrides)
	if strings.TrimSpace(overrides.TrustedProxyCIDRs) != "" {
		cfg.TrustedProxyCIDRs = strings.TrimSpace(overrides.TrustedProxyCIDRs)
	}
	if strings.TrimSpace(overrides.HTTPAllowlist) != "" {
		rules, err := parseRouteRules(overrides.HTTPAllowlist)
		if err != nil {
			return fmt.Errorf("--http-allowlist: %w", err)
		}
		cfg.HTTPAllowedRoutes = rules
	}
	if strings.TrimSpace(overrides.WSAllowlist) != "" {
		rules, err := parseRouteRules(overrides.WSAllowlist)
		if err != nil {
			return fmt.Errorf("--ws-allowlist: %w", err)
		}
		cfg.WSAllowedRoutes = rules
	}
	if overrides.RequireE2EE != nil {
		cfg.RequireE2EE = *overrides.RequireE2EE
	}
	if overrides.PlaintextTestMode != nil {
		cfg.PlaintextTestMode = *overrides.PlaintextTestMode
	}
	return nil
}

func applyDurationOverrides(cfg *Config, overrides Overrides) {
	if overrides.PairingTTL > 0 {
		cfg.PairingTTL = overrides.PairingTTL
	}
	if overrides.AgentGracePeriod > 0 {
		cfg.AgentGracePeriod = overrides.AgentGracePeriod
	}
	if overrides.PairingHandshakeTimeout > 0 {
		cfg.PairingHandshakeTimeout = overrides.PairingHandshakeTimeout
	}
	if overrides.AgentRegisterTimeout > 0 {
		cfg.AgentRegisterTimeout = overrides.AgentRegisterTimeout
	}
	if overrides.PingInterval > 0 {
		cfg.PingInterval = overrides.PingInterval
	}
	if overrides.PongTimeout > 0 {
		cfg.PongTimeout = overrides.PongTimeout
	}
}

func applyIntegerOverrides(cfg *Config, overrides Overrides) {
	if overrides.MaxPairingFailuresPerIP > 0 {
		cfg.MaxPairingFailuresPerIP = overrides.MaxPairingFailuresPerIP
	}
	if overrides.MaxSessions > 0 {
		cfg.MaxSessions = overrides.MaxSessions
	}
	if overrides.MaxAgentConns > 0 {
		cfg.MaxAgentConns = overrides.MaxAgentConns
	}
	if overrides.MaxClientConns > 0 {
		cfg.MaxClientConns = overrides.MaxClientConns
	}
	if overrides.MaxConnsPerIP > 0 {
		cfg.MaxConnsPerIP = overrides.MaxConnsPerIP
	}
	if overrides.MaxControlFrameBytes > 0 {
		cfg.MaxControlFrameBytes = overrides.MaxControlFrameBytes
	}
	if overrides.MaxPayloadBytes > 0 {
		cfg.MaxPayloadBytes = overrides.MaxPayloadBytes
	}
	if overrides.ForwardQueueSize > 0 {
		cfg.ForwardQueueSize = overrides.ForwardQueueSize
	}
}

func loadConfigFromEnv() (Config, error) {
	pairingTTL, err := getEnvDuration("RELAY_PAIRING_TTL", defaultPairingTTL)
	if err != nil {
		return Config{}, err
	}
	grace, err := getEnvDuration("RELAY_AGENT_GRACE_PERIOD", defaultAgentGracePeriod)
	if err != nil {
		return Config{}, err
	}
	limits, err := loadLimitConfigFromEnv()
	if err != nil {
		return Config{}, err
	}
	statePath, err := relayStatePath()
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Addr:              getEnv("RELAY_ADDR", defaultAddr),
		PublicURL:         getEnv("RELAY_PUBLIC_URL", defaultPublicURL),
		StatePath:         statePath,
		PairingTTL:        pairingTTL,
		AgentGracePeriod:  grace,
		TrustedProxyCIDRs: strings.TrimSpace(os.Getenv("RELAY_TRUSTED_PROXY_CIDRS")),
		RequireE2EE:       defaultRequireE2EE,
		PlaintextTestMode: defaultPlaintextTestMode,
	}
	if cfg.RequireE2EE, err = getEnvBool("RELAY_REQUIRE_E2EE", defaultRequireE2EE); err != nil {
		return Config{}, err
	}
	if cfg.PlaintextTestMode, err = getEnvBool("RELAY_PLAINTEXT_TEST_MODE", defaultPlaintextTestMode); err != nil {
		return Config{}, err
	}
	cfg.HTTPAllowedRoutes, err = parseRouteRules(getEnv("RELAY_HTTP_ALLOWLIST", defaultHTTPAllowedRoutes))
	if err != nil {
		return Config{}, fmt.Errorf("RELAY_HTTP_ALLOWLIST: %w", err)
	}
	cfg.WSAllowedRoutes, err = parseRouteRules(getEnv("RELAY_WS_ALLOWLIST", defaultWSAllowedRoutes))
	if err != nil {
		return Config{}, fmt.Errorf("RELAY_WS_ALLOWLIST: %w", err)
	}
	applyLimitConfig(&cfg, limits)
	return cfg, nil
}

func relayStatePath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("RELAY_STATE_PATH")); value != "" {
		return value, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve relay state path: %w", err)
	}
	return filepath.Join(homeDir, ".mobilevc", "relay", defaultStateFileName), nil
}

type limitConfig struct {
	PairingHandshakeTimeout time.Duration
	AgentRegisterTimeout    time.Duration
	MaxPairingFailuresPerIP int
	MaxSessions             int
	MaxAgentConns           int
	MaxClientConns          int
	MaxConnsPerIP           int
	PingInterval            time.Duration
	PongTimeout             time.Duration
	MaxControlFrameBytes    int64
	MaxPayloadBytes         int
	ForwardQueueSize        int
}

func loadLimitConfigFromEnv() (limitConfig, error) {
	cfg := limitConfig{}
	var err error
	cfg.PairingHandshakeTimeout, err = getEnvDuration("RELAY_PAIRING_HANDSHAKE_TIMEOUT", defaultHandshakeTimeout)
	if err != nil {
		return limitConfig{}, err
	}
	return loadRemainingLimitConfig(cfg)
}

func loadRemainingLimitConfig(cfg limitConfig) (limitConfig, error) {
	loaders := []func(*limitConfig) error{
		loadRegisterTimeout,
		loadConnectionLimits,
		loadPingSettings,
		loadFrameLimits,
	}
	for _, load := range loaders {
		if err := load(&cfg); err != nil {
			return limitConfig{}, err
		}
	}
	return cfg, nil
}

func applyLimitConfig(cfg *Config, limits limitConfig) {
	cfg.PairingHandshakeTimeout = limits.PairingHandshakeTimeout
	cfg.AgentRegisterTimeout = limits.AgentRegisterTimeout
	cfg.MaxPairingFailuresPerIP = limits.MaxPairingFailuresPerIP
	cfg.MaxSessions = limits.MaxSessions
	cfg.MaxAgentConns = limits.MaxAgentConns
	cfg.MaxClientConns = limits.MaxClientConns
	cfg.MaxConnsPerIP = limits.MaxConnsPerIP
	cfg.PingInterval = limits.PingInterval
	cfg.PongTimeout = limits.PongTimeout
	cfg.MaxControlFrameBytes = limits.MaxControlFrameBytes
	cfg.MaxPayloadBytes = limits.MaxPayloadBytes
	cfg.ForwardQueueSize = limits.ForwardQueueSize
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Addr) == "" {
		return fmt.Errorf("RELAY_ADDR is required")
	}
	if err := ValidateRelayURL(c.PublicURL); err != nil {
		return fmt.Errorf("RELAY_PUBLIC_URL: %w", err)
	}
	if c.PairingTTL <= 0 || c.AgentGracePeriod <= 0 {
		return fmt.Errorf("relay ttl and grace period must be positive")
	}
	if c.ForwardQueueSize <= 0 || c.MaxControlFrameBytes <= 0 || c.MaxPayloadBytes <= 0 {
		return fmt.Errorf("relay frame limits must be positive")
	}
	if c.MaxSessions <= 0 || c.MaxAgentConns <= 0 || c.MaxClientConns <= 0 || c.MaxConnsPerIP <= 0 {
		return fmt.Errorf("relay connection limits must be positive")
	}
	if c.PingInterval <= 0 || c.PongTimeout <= 0 {
		return fmt.Errorf("relay ping settings must be positive")
	}
	if len(c.HTTPAllowedRoutes) == 0 && len(c.WSAllowedRoutes) == 0 {
		return fmt.Errorf("relay route allowlist cannot be empty")
	}
	if c.RequireE2EE && c.PlaintextTestMode {
		return fmt.Errorf("RELAY_REQUIRE_E2EE and RELAY_PLAINTEXT_TEST_MODE cannot both be true")
	}
	return nil
}

func parseRouteRules(raw string) ([]RouteRule, error) {
	parts := strings.Split(raw, ",")
	rules := make([]RouteRule, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		rule, err := parseRouteRule(part)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func parseRouteRule(raw string) (RouteRule, error) {
	method, path, ok := strings.Cut(strings.TrimSpace(raw), ":")
	if !ok {
		return RouteRule{}, fmt.Errorf("route %q must use METHOD:/path", raw)
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	if method == "" || path == "" || !strings.HasPrefix(path, "/") {
		return RouteRule{}, fmt.Errorf("route %q must use METHOD:/path", raw)
	}
	if method != http.MethodGet && method != http.MethodPost {
		return RouteRule{}, fmt.Errorf("route %q uses unsupported method %s", raw, method)
	}
	return normalizeRouteRule(RouteRule{Method: method, Path: path}), nil
}

func getEnv(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) (time.Duration, error) {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return 0, fmt.Errorf("%s must be a valid duration: %w", key, err)
		}
		return parsed, nil
	}
	return fallback, nil
}

func getEnvInt(key string, fallback int) (int, error) {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return 0, fmt.Errorf("%s must be a valid integer: %w", key, err)
		}
		return parsed, nil
	}
	return fallback, nil
}

func getEnvBool(key string, fallback bool) (bool, error) {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return false, fmt.Errorf("%s must be a valid boolean: %w", key, err)
		}
		return parsed, nil
	}
	return fallback, nil
}

func getEnvBytes(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSuffix(value, "B"))
	if err == nil {
		return parsed, nil
	}
	if strings.HasSuffix(strings.ToLower(value), "kib") {
		return parseByteUnit(value, "KiB", 1024)
	}
	return 0, fmt.Errorf("%s must be bytes or KiB", key)
}

func parseByteUnit(value string, suffix string, unit int) (int, error) {
	number := strings.TrimSpace(strings.TrimSuffix(value, suffix))
	parsed, err := strconv.Atoi(number)
	if err != nil {
		return 0, err
	}
	return parsed * unit, nil
}
