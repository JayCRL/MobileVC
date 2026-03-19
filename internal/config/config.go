package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port      string
	AuthToken string
}

func Load() (Config, error) {
	cfg := Config{
		Port:      getEnv("PORT", "8080"),
		AuthToken: os.Getenv("AUTH_TOKEN"),
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
