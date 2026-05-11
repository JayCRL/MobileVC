package planning

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mobilevc/internal/logx"
)

// Manager handles the planning lifecycle: checking Claude installation,
// managing the API key, and building the planner command.
type Manager struct {
	APIKey string
}

// CheckResult describes whether Claude Code CLI is available.
type CheckResult struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
	InstallHint string `json:"installHint,omitempty"`
}

// CheckClaude verifies that the claude CLI is installed and returns its version.
func (m *Manager) CheckClaude(ctx context.Context) CheckResult {
	which, err := exec.LookPath("claude")
	if err != nil {
		// Check common node paths
		for _, p := range []string{
			os.ExpandEnv("$HOME/.npm-global/bin/claude"),
			"/usr/local/bin/claude",
			"/opt/homebrew/bin/claude",
		} {
			if fi, statErr := os.Stat(p); statErr == nil && !fi.IsDir() {
				which = p
				break
			}
		}
		if which == "" {
			return CheckResult{
				Installed:   false,
				Error:       "claude CLI not found",
				InstallHint: "Run: npm install -g @anthropic-ai/claude-code",
			}
		}
	}

	cmd := exec.CommandContext(ctx, which, "--version")
	out, err := cmd.Output()
	if err != nil {
		return CheckResult{
			Installed:   false,
			Error:       fmt.Sprintf("claude found at %s but failed to run: %v", which, err),
			InstallHint: "Reinstall: npm install -g @anthropic-ai/claude-code",
		}
	}

	return CheckResult{
		Installed: true,
		Version:   strings.TrimSpace(string(out)),
	}
}

// InstallClaude attempts to install the Claude Code CLI via npm.
func (m *Manager) InstallClaude(ctx context.Context) error {
	// Check node/npm first
	if _, err := exec.LookPath("npm"); err != nil {
		return fmt.Errorf("npm is not installed. Please install Node.js first: https://nodejs.org")
	}

	logx.Info("planning", "Installing @anthropic-ai/claude-code via npm...")
	cmd := exec.CommandContext(ctx, "npm", "install", "-g", "@anthropic-ai/claude-code")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// BuildCommand constructs the claude CLI command for a planning session.
// The caller is responsible for setting ANTHROPIC_API_KEY in the environment.
func (m *Manager) BuildCommand(appendPrompt string) []string {
	agentsJSON := AgentsJSON()
	prompt := PlannerPrompt()
	if appendPrompt != "" {
		prompt += "\n\n## Additional Context\n" + appendPrompt
	}

	return []string{
		"claude",
		"--agents", agentsJSON,
		"--append-system-prompt", prompt,
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--print",
		"--verbose",
	}
}

// EnvVars returns the environment variables needed for the planning session.
func (m *Manager) EnvVars() []string {
	return []string{
		"ANTHROPIC_API_KEY=" + m.APIKey,
		"FORCE_COLOR=1",
		"CLICOLOR_FORCE=1",
	}
}

// KeyStore manages API key persistence with encryption.
type KeyStore struct {
	path   string
	secret string
}

// NewKeyStore creates a KeyStore at the given path.
func NewKeyStore(baseDir, secret string) *KeyStore {
	return &KeyStore{
		path:   filepath.Join(baseDir, "planning_key"),
		secret: secret,
	}
}

// Get reads and decrypts the stored API key.
func (ks *KeyStore) Get() (string, error) {
	data, err := os.ReadFile(ks.path)
	if err != nil {
		return "", err
	}
	return Decrypt(strings.TrimSpace(string(data)), ks.secret)
}

// Set encrypts and writes the API key to disk.
func (ks *KeyStore) Set(key string) error {
	encrypted, err := Encrypt(key, ks.secret)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ks.path), 0700); err != nil {
		return err
	}
	return os.WriteFile(ks.path, []byte(encrypted), 0600)
}

// Clear removes the stored API key.
func (ks *KeyStore) Clear() error {
	return os.Remove(ks.path)
}
