package unit

import (
	"os"
	"testing"

	"mobilevc/internal/config"
)

func TestLoad_MissingAuthToken(t *testing.T) {
	os.Unsetenv("AUTH_TOKEN")
	os.Setenv("PORT", "9999")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for missing AUTH_TOKEN")
	}
	if err.Error() != "AUTH_TOKEN is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_Defaults(t *testing.T) {
	os.Setenv("AUTH_TOKEN", "test-token")
	defer os.Unsetenv("AUTH_TOKEN")

	// Clear optional envs to test defaults
	for _, k := range []string{"PORT", "RUNTIME_DEFAULT_COMMAND", "RUNTIME_DEFAULT_MODE",
		"RUNTIME_DEBUG", "RUNTIME_WORKSPACE_ROOT", "RUNTIME_ENHANCED_PROJECTION",
		"RUNTIME_ENABLE_STEP_PROJECTION", "RUNTIME_ENABLE_DIFF_PROJECTION",
		"RUNTIME_ENABLE_PROMPT_PROJECTION", "TTS_ENABLED", "TTS_PROVIDER",
		"TTS_PYTHON_SERVICE_URL", "TTS_REQUEST_TIMEOUT_SECONDS", "TTS_MAX_TEXT_LENGTH",
		"TTS_DEFAULT_FORMAT"} {
		os.Unsetenv(k)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Port != "8001" {
		t.Errorf("default Port: got %q, want %q", cfg.Port, "8001")
	}
	if cfg.AuthToken != "test-token" {
		t.Errorf("AuthToken: got %q, want %q", cfg.AuthToken, "test-token")
	}
	if cfg.Runtime.DefaultCommand != "claude" {
		t.Errorf("DefaultCommand: got %q", cfg.Runtime.DefaultCommand)
	}
	if cfg.Runtime.DefaultMode != "pty" {
		t.Errorf("DefaultMode: got %q", cfg.Runtime.DefaultMode)
	}
	if cfg.Runtime.Debug {
		t.Error("Debug should default to false")
	}
	if !cfg.Runtime.EnhancedProjection {
		t.Error("EnhancedProjection should default to true")
	}
	if !cfg.Runtime.EnableStepProjection {
		t.Error("EnableStepProjection should default to true")
	}
	if !cfg.Runtime.EnableDiffProjection {
		t.Error("EnableDiffProjection should default to true")
	}
	if !cfg.Runtime.EnablePromptProjection {
		t.Error("EnablePromptProjection should default to true")
	}
	if cfg.TTS.Enabled {
		t.Error("TTS should be disabled by default")
	}
}

func TestLoad_CustomValues(t *testing.T) {
	os.Setenv("AUTH_TOKEN", "custom-token")
	os.Setenv("PORT", "3000")
	os.Setenv("RUNTIME_DEFAULT_COMMAND", "codex")
	os.Setenv("RUNTIME_DEFAULT_MODE", "exec")
	os.Setenv("RUNTIME_DEBUG", "true")
	os.Setenv("RUNTIME_WORKSPACE_ROOT", "/tmp/work")
	os.Setenv("RUNTIME_ENHANCED_PROJECTION", "false")
	defer func() {
		for _, k := range []string{"AUTH_TOKEN", "PORT", "RUNTIME_DEFAULT_COMMAND",
			"RUNTIME_DEFAULT_MODE", "RUNTIME_DEBUG", "RUNTIME_WORKSPACE_ROOT",
			"RUNTIME_ENHANCED_PROJECTION"} {
			os.Unsetenv(k)
		}
	}()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Port != "3000" {
		t.Errorf("Port: got %q", cfg.Port)
	}
	if cfg.Runtime.DefaultCommand != "codex" {
		t.Errorf("DefaultCommand: got %q", cfg.Runtime.DefaultCommand)
	}
	if cfg.Runtime.DefaultMode != "exec" {
		t.Errorf("DefaultMode: got %q", cfg.Runtime.DefaultMode)
	}
	if !cfg.Runtime.Debug {
		t.Error("Debug should be true")
	}
	if cfg.Runtime.WorkspaceRoot != "/tmp/work" {
		t.Errorf("WorkspaceRoot: got %q", cfg.Runtime.WorkspaceRoot)
	}
	if cfg.Runtime.EnhancedProjection {
		t.Error("EnhancedProjection should be false")
	}
}

func TestLoad_TTSValidation_Disabled(t *testing.T) {
	os.Setenv("AUTH_TOKEN", "t")
	os.Unsetenv("TTS_ENABLED")
	os.Setenv("TTS_PYTHON_SERVICE_URL", "")
	defer func() {
		os.Unsetenv("AUTH_TOKEN")
		os.Unsetenv("TTS_PYTHON_SERVICE_URL")
	}()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load with TTS disabled should not fail validation: %v", err)
	}
	if cfg.TTS.Enabled {
		t.Error("TTS should be disabled")
	}
}

func TestLoad_TTSValidation_Enabled_BadFormat(t *testing.T) {
	os.Setenv("AUTH_TOKEN", "t")
	os.Setenv("TTS_ENABLED", "true")
	os.Setenv("TTS_DEFAULT_FORMAT", "mp3")
	defer func() {
		for _, k := range []string{"AUTH_TOKEN", "TTS_ENABLED", "TTS_DEFAULT_FORMAT"} {
			os.Unsetenv(k)
		}
	}()

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for invalid TTS format")
	}
}

func TestLoad_TTSValidation_Enabled_BadProvider(t *testing.T) {
	os.Setenv("AUTH_TOKEN", "t")
	os.Setenv("TTS_ENABLED", "true")
	os.Setenv("TTS_PROVIDER", "unknown-provider")
	defer func() {
		for _, k := range []string{"AUTH_TOKEN", "TTS_ENABLED", "TTS_PROVIDER"} {
			os.Unsetenv(k)
		}
	}()

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for invalid TTS provider")
	}
}

func TestLoad_TTSValidation_Enabled_NegativeTimeout(t *testing.T) {
	os.Setenv("AUTH_TOKEN", "t")
	os.Setenv("TTS_ENABLED", "true")
	os.Setenv("TTS_REQUEST_TIMEOUT_SECONDS", "-1")
	defer func() {
		for _, k := range []string{"AUTH_TOKEN", "TTS_ENABLED", "TTS_REQUEST_TIMEOUT_SECONDS"} {
			os.Unsetenv(k)
		}
	}()

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for negative TTS timeout")
	}
}

func TestLoad_TTSValidation_Enabled_Valid(t *testing.T) {
	os.Setenv("AUTH_TOKEN", "t")
	os.Setenv("TTS_ENABLED", "true")
	os.Setenv("TTS_PROVIDER", "chattts-http")
	defer func() {
		for _, k := range []string{"AUTH_TOKEN", "TTS_ENABLED", "TTS_PROVIDER"} {
			os.Unsetenv(k)
		}
	}()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected valid TTS config: %v", err)
	}
	if !cfg.TTS.Enabled {
		t.Error("TTS should be enabled")
	}
}

func TestSummary(t *testing.T) {
	os.Setenv("AUTH_TOKEN", "s3cret")
	defer os.Unsetenv("AUTH_TOKEN")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	s := cfg.Summary()
	if !s.AuthTokenConfigured {
		t.Error("AuthTokenConfigured should be true")
	}
	if s.Port != "8001" {
		t.Errorf("Port: got %q", s.Port)
	}
	if s.DefaultCommand != "claude" {
		t.Errorf("DefaultCommand: got %q", s.DefaultCommand)
	}
}

func TestGetEnvBool_Variants(t *testing.T) {
	// Test via Load since getEnvBool is unexported
	os.Setenv("AUTH_TOKEN", "t")

	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		{"true_lower", "true", true},
		{"true_upper", "TRUE", true},
		{"one", "1", true},
		{"yes", "yes", true},
		{"on", "on", true},
		{"false_lower", "false", false},
		{"zero", "0", false},
		{"no", "no", false},
		{"off", "off", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("RUNTIME_DEBUG", tt.value)
			defer os.Unsetenv("RUNTIME_DEBUG")

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Runtime.Debug != tt.expected {
				t.Errorf("Debug: got %v, want %v (input=%q)", cfg.Runtime.Debug, tt.expected, tt.value)
			}
		})
	}

	os.Unsetenv("AUTH_TOKEN")
}
