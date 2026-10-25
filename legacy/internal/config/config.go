package config

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

const (
	ExposureModeLAN       = "lan"
	ExposureModeRelayOnly = "relay-only"
)

type NetworkConfig struct {
	ExposureMode string
}

type RuntimeConfig struct {
	DefaultCommand         string
	DefaultMode            string
	Debug                  bool
	WorkspaceRoot          string
	EnhancedProjection     bool
	EnableStepProjection   bool
	EnableDiffProjection   bool
	EnablePromptProjection bool
}

type TTSConfig struct {
	Enabled               bool
	Provider              string
	PythonServiceURL      string
	RequestTimeoutSeconds int
	MaxTextLength         int
	DefaultFormat         string
}

type RelayConfig struct {
	Enabled          bool
	URL              string
	PairingTTL       time.Duration
	AgentGracePeriod time.Duration
	PairingEventPath string
	SessionStatePath string
	HTTPAllowlist    string
	WSAllowlist      string
}

type Overrides struct {
	Port                string
	AuthToken           string
	NetworkExposureMode string
	RelayMode           *bool
	RelayURL            string
	RelayPairingTTL     time.Duration
	RelayAgentGrace     time.Duration
	RelayPairingPath    string
	RelaySessionPath    string
	RelayHTTPAllowlist  string
	RelayWSAllowlist    string
}

type Config struct {
	Port      string
	AuthToken string
	Network   NetworkConfig
	Runtime   RuntimeConfig
	TTS       TTSConfig
	Relay     RelayConfig
}

type Summary struct {
	Port                   string
	AuthTokenConfigured    bool
	DefaultCommand         string
	DefaultMode            string
	Debug                  bool
	WorkspaceRoot          string
	EnhancedProjection     bool
	EnableStepProjection   bool
	EnableDiffProjection   bool
	EnablePromptProjection bool
	TTSEnabled             bool
	TTSProvider            string
	TTSPythonServiceURL    string
	TTSRequestTimeout      int
	TTSMaxTextLength       int
	TTSDefaultFormat       string
	RelayMode              bool
	RelayURL               string
	ExposureMode           string
	ListenAddress          string
	HealthURL              string
	VersionURL             string
	WebSocketURL           string
}

func Load() (Config, error) {
	return LoadWithOverrides(Overrides{})
}

func LoadWithOverrides(overrides Overrides) (Config, error) {
	relayCfg, err := loadRelayConfig()
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Port:      getEnv("PORT", "8001"),
		AuthToken: os.Getenv("AUTH_TOKEN"),
		Network:   loadNetworkConfig(),
		Runtime:   loadRuntimeConfig(),
		TTS:       loadTTSConfig(),
		Relay:     relayCfg,
	}
	applyOverrides(&cfg, overrides)

	if cfg.AuthToken == "" {
		return Config{}, fmt.Errorf("AUTH_TOKEN is required")
	}
	if err := cfg.validateTTS(); err != nil {
		return Config{}, err
	}
	if err := cfg.validateRelay(); err != nil {
		return Config{}, err
	}
	if err := cfg.validateNetwork(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func applyOverrides(cfg *Config, overrides Overrides) {
	if strings.TrimSpace(overrides.Port) != "" {
		cfg.Port = strings.TrimSpace(overrides.Port)
	}
	if strings.TrimSpace(overrides.AuthToken) != "" {
		cfg.AuthToken = overrides.AuthToken
	}
	if strings.TrimSpace(overrides.NetworkExposureMode) != "" {
		cfg.Network.ExposureMode = normalizeExposureMode(overrides.NetworkExposureMode)
	}
	if overrides.RelayMode != nil {
		cfg.Relay.Enabled = *overrides.RelayMode
	}
	if strings.TrimSpace(overrides.RelayURL) != "" {
		cfg.Relay.URL = strings.TrimSpace(overrides.RelayURL)
	}
	if overrides.RelayPairingTTL > 0 {
		cfg.Relay.PairingTTL = overrides.RelayPairingTTL
	}
	if overrides.RelayAgentGrace > 0 {
		cfg.Relay.AgentGracePeriod = overrides.RelayAgentGrace
	}
	if strings.TrimSpace(overrides.RelayPairingPath) != "" {
		cfg.Relay.PairingEventPath = strings.TrimSpace(overrides.RelayPairingPath)
	}
	if strings.TrimSpace(overrides.RelaySessionPath) != "" {
		cfg.Relay.SessionStatePath = strings.TrimSpace(overrides.RelaySessionPath)
	}
	if strings.TrimSpace(overrides.RelayHTTPAllowlist) != "" {
		cfg.Relay.HTTPAllowlist = strings.TrimSpace(overrides.RelayHTTPAllowlist)
	}
	if strings.TrimSpace(overrides.RelayWSAllowlist) != "" {
		cfg.Relay.WSAllowlist = strings.TrimSpace(overrides.RelayWSAllowlist)
	}
}

func (c Config) ListenAddress() string {
	host := ""
	if c.Network.ExposureMode == ExposureModeRelayOnly {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, c.Port)
}

func (c Config) LocalEndpointHostPort() string {
	if c.Network.ExposureMode == ExposureModeRelayOnly {
		return c.ListenAddress()
	}
	return net.JoinHostPort("localhost", c.Port)
}

func (c Config) HealthURL() string {
	return "http://" + c.LocalEndpointHostPort() + "/healthz"
}

func (c Config) VersionURL() string {
	return "http://" + c.LocalEndpointHostPort() + "/version"
}

func (c Config) WebSocketURL() string {
	return "ws://" + c.LocalEndpointHostPort() + "/ws?token=<redacted>"
}

func (c Config) Summary() Summary {
	return Summary{
		Port:                   c.Port,
		AuthTokenConfigured:    strings.TrimSpace(c.AuthToken) != "",
		DefaultCommand:         c.Runtime.DefaultCommand,
		DefaultMode:            c.Runtime.DefaultMode,
		Debug:                  c.Runtime.Debug,
		WorkspaceRoot:          c.Runtime.WorkspaceRoot,
		EnhancedProjection:     c.Runtime.EnhancedProjection,
		EnableStepProjection:   c.Runtime.EnableStepProjection,
		EnableDiffProjection:   c.Runtime.EnableDiffProjection,
		EnablePromptProjection: c.Runtime.EnablePromptProjection,
		TTSEnabled:             c.TTS.Enabled,
		TTSProvider:            c.TTS.Provider,
		TTSPythonServiceURL:    c.TTS.PythonServiceURL,
		TTSRequestTimeout:      c.TTS.RequestTimeoutSeconds,
		TTSMaxTextLength:       c.TTS.MaxTextLength,
		TTSDefaultFormat:       c.TTS.DefaultFormat,
		RelayMode:              c.Relay.Enabled,
		RelayURL:               c.Relay.URL,
		ExposureMode:           c.Network.ExposureMode,
		ListenAddress:          c.ListenAddress(),
		HealthURL:              c.HealthURL(),
		VersionURL:             c.VersionURL(),
		WebSocketURL:           c.WebSocketURL(),
	}
}
