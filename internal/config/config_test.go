package config

import (
	"os"
	"testing"
)

func TestLoadTTSValidation(t *testing.T) {
	oldEnv := os.Environ()
	_ = oldEnv
	keys := []string{
		"AUTH_TOKEN",
		"TTS_ENABLED",
		"TTS_PROVIDER",
		"TTS_PYTHON_SERVICE_URL",
		"TTS_REQUEST_TIMEOUT_SECONDS",
		"TTS_MAX_TEXT_LENGTH",
		"TTS_DEFAULT_FORMAT",
	}
	for _, key := range keys {
		prev, ok := os.LookupEnv(key)
		if ok {
			defer os.Setenv(key, prev)
		} else {
			defer os.Unsetenv(key)
		}
	}

	t.Run("disabled tts passes", func(t *testing.T) {
		os.Setenv("AUTH_TOKEN", "test")
		os.Setenv("TTS_ENABLED", "false")
		if _, err := Load(); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("enabled tts with invalid provider fails", func(t *testing.T) {
		os.Setenv("AUTH_TOKEN", "test")
		os.Setenv("TTS_ENABLED", "true")
		os.Setenv("TTS_PROVIDER", "other")
		os.Setenv("TTS_PYTHON_SERVICE_URL", "http://127.0.0.1:9966")
		os.Setenv("TTS_REQUEST_TIMEOUT_SECONDS", "30")
		os.Setenv("TTS_MAX_TEXT_LENGTH", "200")
		os.Setenv("TTS_DEFAULT_FORMAT", "wav")
		if _, err := Load(); err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("enabled tts with invalid format fails", func(t *testing.T) {
		os.Setenv("AUTH_TOKEN", "test")
		os.Setenv("TTS_ENABLED", "true")
		os.Setenv("TTS_PROVIDER", "chattts-http")
		os.Setenv("TTS_PYTHON_SERVICE_URL", "http://127.0.0.1:9966")
		os.Setenv("TTS_REQUEST_TIMEOUT_SECONDS", "30")
		os.Setenv("TTS_MAX_TEXT_LENGTH", "200")
		os.Setenv("TTS_DEFAULT_FORMAT", "mp3")
		if _, err := Load(); err == nil {
			t.Fatal("expected validation error")
		}
	})
}
