package config

import (
	"fmt"
	"os"
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

type Config struct {
	Port      string
	AuthToken string
	Runtime   RuntimeConfig
}

func Load() (Config, error) {
	cfg := Config{
		Port:      getEnv("PORT", "8080"),
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
	}

	if cfg.AuthToken == "" {
		return Config{}, fmt.Errorf("AUTH_TOKEN is required")
	}

	return cfg, nil
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
