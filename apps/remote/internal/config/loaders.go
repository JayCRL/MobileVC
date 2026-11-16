package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

func loadNetworkConfig() NetworkConfig {
	return NetworkConfig{
		ExposureMode: normalizeExposureMode(os.Getenv("NETWORK_EXPOSURE_MODE")),
	}
}

func loadRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		DefaultCommand:         getEnv("RUNTIME_DEFAULT_COMMAND", "claude"),
		DefaultMode:            getEnv("RUNTIME_DEFAULT_MODE", "pty"),
		Debug:                  getEnvBool("RUNTIME_DEBUG", false),
		WorkspaceRoot:          strings.TrimSpace(os.Getenv("RUNTIME_WORKSPACE_ROOT")),
		EnhancedProjection:     getEnvBool("RUNTIME_ENHANCED_PROJECTION", true),
		EnableStepProjection:   getEnvBool("RUNTIME_ENABLE_STEP_PROJECTION", true),
		EnableDiffProjection:   getEnvBool("RUNTIME_ENABLE_DIFF_PROJECTION", true),
		EnablePromptProjection: getEnvBool("RUNTIME_ENABLE_PROMPT_PROJECTION", true),
	}
}

func normalizeExposureMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "lan", "lan-enabled":
		return ExposureModeLAN
	case "relay-only", "relay":
		return ExposureModeRelayOnly
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func loadTTSConfig() TTSConfig {
	return TTSConfig{
		Enabled:               getEnvBool("TTS_ENABLED", false),
		Provider:              strings.TrimSpace(getEnv("TTS_PROVIDER", "chattts-http")),
		PythonServiceURL:      strings.TrimSpace(getEnv("TTS_PYTHON_SERVICE_URL", "http://127.0.0.1:9966")),
		RequestTimeoutSeconds: getEnvInt("TTS_REQUEST_TIMEOUT_SECONDS", 30),
		MaxTextLength:         getEnvInt("TTS_MAX_TEXT_LENGTH", 200),
		DefaultFormat:         strings.TrimSpace(getEnv("TTS_DEFAULT_FORMAT", "wav")),
	}
}

func loadRelayConfig() (RelayConfig, error) {
	pairingTTL, err := getEnvDurationStrict("RELAY_PAIRING_TTL", 5*time.Minute)
	if err != nil {
		return RelayConfig{}, err
	}
	grace, err := getEnvDurationStrict("RELAY_AGENT_GRACE_PERIOD", 60*time.Second)
	if err != nil {
		return RelayConfig{}, err
	}
	sessionStatePath, err := relayAgentSessionStatePath()
	if err != nil {
		return RelayConfig{}, err
	}
	return RelayConfig{
		Enabled:          getEnvBool("RELAY_MODE", false),
		URL:              strings.TrimSpace(os.Getenv("RELAY_URL")),
		PairingTTL:       pairingTTL,
		AgentGracePeriod: grace,
		PairingEventPath: strings.TrimSpace(os.Getenv("RELAY_PAIRING_EVENT_PATH")),
		SessionStatePath: sessionStatePath,
		HTTPAllowlist:    strings.TrimSpace(os.Getenv("RELAY_HTTP_ALLOWLIST")),
		WSAllowlist:      strings.TrimSpace(os.Getenv("RELAY_WS_ALLOWLIST")),
	}, nil
}

func relayAgentSessionStatePath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("RELAY_AGENT_SESSION_STATE_PATH")); value != "" {
		return value, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".mobilevc", "relay", "agent_session.json"), nil
}
