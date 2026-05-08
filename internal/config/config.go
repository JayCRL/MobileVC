package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

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

type Config struct {
	Port      string
	AuthToken string
	Runtime   RuntimeConfig
	TTS       TTSConfig

	// Official server registration (optional)
	OfficialServerURL    string
	OfficialNodeID       string
	OfficialNodeName     string
	OfficialAccessToken  string
	OfficialRefreshToken string
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
	OfficialServerURL      string
	OfficialNodeID         string
}

func Load() (Config, error) {
	cfg := Config{
		Port:      getEnv("PORT", "8001"),
		AuthToken: os.Getenv("AUTH_TOKEN"),
		Runtime: RuntimeConfig{
			DefaultCommand:         getEnv("RUNTIME_DEFAULT_COMMAND", "claude"),
			DefaultMode:            getEnv("RUNTIME_DEFAULT_MODE", "pty"),
			Debug:                  getEnvBool("RUNTIME_DEBUG", false),
			WorkspaceRoot:          strings.TrimSpace(os.Getenv("RUNTIME_WORKSPACE_ROOT")),
			EnhancedProjection:     getEnvBool("RUNTIME_ENHANCED_PROJECTION", true),
			EnableStepProjection:   getEnvBool("RUNTIME_ENABLE_STEP_PROJECTION", true),
			EnableDiffProjection:   getEnvBool("RUNTIME_ENABLE_DIFF_PROJECTION", true),
			EnablePromptProjection: getEnvBool("RUNTIME_ENABLE_PROMPT_PROJECTION", true),
		},
		TTS: TTSConfig{
			Enabled:               getEnvBool("TTS_ENABLED", false),
			Provider:              strings.TrimSpace(getEnv("TTS_PROVIDER", "chattts-http")),
			PythonServiceURL:      strings.TrimSpace(getEnv("TTS_PYTHON_SERVICE_URL", "http://127.0.0.1:9966")),
			RequestTimeoutSeconds: getEnvInt("TTS_REQUEST_TIMEOUT_SECONDS", 30),
			MaxTextLength:         getEnvInt("TTS_MAX_TEXT_LENGTH", 200),
			DefaultFormat:         strings.TrimSpace(getEnv("TTS_DEFAULT_FORMAT", "wav")),
		},
		OfficialServerURL:    strings.TrimSpace(os.Getenv("OFFICIAL_SERVER_URL")),
		OfficialNodeID:       strings.TrimSpace(os.Getenv("OFFICIAL_NODE_ID")),
		OfficialNodeName:     strings.TrimSpace(os.Getenv("OFFICIAL_NODE_NAME")),
		OfficialAccessToken:  strings.TrimSpace(os.Getenv("OFFICIAL_ACCESS_TOKEN")),
		OfficialRefreshToken: strings.TrimSpace(os.Getenv("OFFICIAL_REFRESH_TOKEN")),
	}

	if cfg.AuthToken == "" {
		return Config{}, fmt.Errorf("AUTH_TOKEN is required")
	}
	if err := cfg.validateTTS(); err != nil {
		return Config{}, err
	}

	return cfg, nil
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
		OfficialServerURL:      c.OfficialServerURL,
		OfficialNodeID:         c.OfficialNodeID,
	}
}

func (c Config) validateTTS() error {
	if !c.TTS.Enabled {
		return nil
	}
	if c.TTS.Provider != "chattts-http" {
		return fmt.Errorf("TTS_PROVIDER must be chattts-http")
	}
	if strings.TrimSpace(c.TTS.PythonServiceURL) == "" {
		return fmt.Errorf("TTS_PYTHON_SERVICE_URL is required when TTS is enabled")
	}
	if c.TTS.RequestTimeoutSeconds <= 0 {
		return fmt.Errorf("TTS_REQUEST_TIMEOUT_SECONDS must be greater than 0")
	}
	if c.TTS.MaxTextLength <= 0 {
		return fmt.Errorf("TTS_MAX_TEXT_LENGTH must be greater than 0")
	}
	if strings.ToLower(strings.TrimSpace(c.TTS.DefaultFormat)) != "wav" {
		return fmt.Errorf("TTS_DEFAULT_FORMAT must be wav")
	}
	return nil
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func getEnvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
