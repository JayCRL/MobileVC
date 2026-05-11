package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port            string
	JWTSecret       string
	DBPath          string
	BaseURL         string
	GitHubClientID  string
	GitHubSecret    string
	GoogleClientID  string
	GoogleSecret    string
	TurnHost        string
	TurnPort        string
	TurnUser        string
	TurnPass        string
	AdminToken      string
	AccessTokenTTL  int // minutes
	RefreshTokenTTL int // days
}

func (c Config) Summary() map[string]any {
	return map[string]any{
		"port":              c.Port,
		"jwt_secret_set":    c.JWTSecret != "",
		"db_path":           c.DBPath,
		"base_url":          c.BaseURL,
		"github_oauth_set":  c.GitHubClientID != "",
		"google_oauth_set":  c.GoogleClientID != "",
		"turn_configured":   c.TurnHost != "",
		"admin_token_set":   c.AdminToken != "",
	}
}

func (c Config) Validate() error {
	if c.JWTSecret == "" {
		return fmt.Errorf("OFFICIAL_JWT_SECRET is required")
	}
	if c.BaseURL == "" {
		return fmt.Errorf("OFFICIAL_BASE_URL is required")
	}
	if c.GitHubClientID == "" && c.GoogleClientID == "" {
		return fmt.Errorf("at least one OAuth provider (GITHUB_CLIENT_ID or GOOGLE_CLIENT_ID) must be configured")
	}
	if c.GitHubClientID != "" && c.GitHubSecret == "" {
		return fmt.Errorf("GITHUB_CLIENT_SECRET is required when GITHUB_CLIENT_ID is set")
	}
	if c.GoogleClientID != "" && c.GoogleSecret == "" {
		return fmt.Errorf("GOOGLE_CLIENT_SECRET is required when GOOGLE_CLIENT_ID is set")
	}
	return nil
}

func Load() (Config, error) {
	cfg := Config{
		Port:            getEnv("OFFICIAL_PORT", "8003"),
		JWTSecret:       os.Getenv("OFFICIAL_JWT_SECRET"),
		DBPath:          getEnv("OFFICIAL_DB_PATH", ""),
		BaseURL:         os.Getenv("OFFICIAL_BASE_URL"),
		GitHubClientID:  os.Getenv("GITHUB_CLIENT_ID"),
		GitHubSecret:    os.Getenv("GITHUB_CLIENT_SECRET"),
		GoogleClientID:  os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleSecret:    os.Getenv("GOOGLE_CLIENT_SECRET"),
		TurnHost:        os.Getenv("OFFICIAL_TURN_HOST"),
		TurnPort:        getEnv("OFFICIAL_TURN_PORT", "3478"),
		TurnUser:        os.Getenv("OFFICIAL_TURN_USER"),
		TurnPass:        os.Getenv("OFFICIAL_TURN_PASS"),
		AdminToken:      os.Getenv("ADMIN_TOKEN"),
		AccessTokenTTL:  getEnvInt("OFFICIAL_ACCESS_TOKEN_TTL_MINUTES", 15),
		RefreshTokenTTL: getEnvInt("OFFICIAL_REFRESH_TOKEN_TTL_DAYS", 30),
	}
	if cfg.DBPath == "" {
		cfg.DBPath = os.ExpandEnv("$HOME/.mobilevc/official.db")
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
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
