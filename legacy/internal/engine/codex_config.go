package engine

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type codexConfigDefaults struct {
	model           string
	reasoningEffort string
	approvalPolicy  string
	sandboxMode     string
}

func loadCodexConfigDefaults() (codexConfigDefaults, error) {
	path, ok, err := codexConfigPath()
	if err != nil {
		return codexConfigDefaults{}, err
	}
	if !ok {
		return codexConfigDefaults{}, nil
	}
	return readCodexConfigDefaults(path)
}

func codexConfigPath() (string, bool, error) {
	if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
		return filepath.Join(codexHome, "config.toml"), true, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, fmt.Errorf("resolve Codex config home: %w", err)
	}
	if strings.TrimSpace(home) == "" {
		return "", false, nil
	}
	return filepath.Join(home, ".codex", "config.toml"), true, nil
}

func readCodexConfigDefaults(path string) (codexConfigDefaults, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return codexConfigDefaults{}, nil
		}
		return codexConfigDefaults{}, fmt.Errorf("read Codex config %s: %w", path, err)
	}
	defer file.Close()

	var defaults codexConfigDefaults
	inTopLevel := true
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(stripTomlComment(scanner.Text()))
		if strings.HasPrefix(line, "[") {
			inTopLevel = false
			continue
		}
		if !inTopLevel {
			continue
		}
		key, value, ok := parseCodexConfigAssignment(line)
		if !ok {
			continue
		}
		switch key {
		case "model":
			defaults.model = value
		case "model_reasoning_effort":
			defaults.reasoningEffort = strings.ToLower(value)
		case "approval_policy":
			defaults.approvalPolicy = value
		case "sandbox_mode":
			defaults.sandboxMode = value
		}
	}
	if err := scanner.Err(); err != nil {
		return codexConfigDefaults{}, fmt.Errorf("scan Codex config %s: %w", path, err)
	}
	return defaults, nil
}

func parseCodexConfigAssignment(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(stripTomlComment(line))
	if trimmed == "" || strings.HasPrefix(trimmed, "[") {
		return "", "", false
	}
	index := strings.Index(trimmed, "=")
	if index <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(trimmed[:index])
	value := strings.TrimSpace(trimmed[index+1:])
	value = strings.Trim(value, `"'`)
	if key == "" || value == "" {
		return "", "", false
	}
	return strings.ToLower(key), value, true
}

func stripTomlComment(line string) string {
	inSingleQuote := false
	inDoubleQuote := false
	escaped := false
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && inDoubleQuote {
			escaped = true
			continue
		}
		switch r {
		case '\'':
			if !inDoubleQuote {
				inSingleQuote = !inSingleQuote
			}
		case '"':
			if !inSingleQuote {
				inDoubleQuote = !inDoubleQuote
			}
		case '#':
			if !inSingleQuote && !inDoubleQuote {
				return line[:i]
			}
		}
	}
	return line
}
