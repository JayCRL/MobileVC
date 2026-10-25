package session

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"mobilevc/internal/protocol"
)

type voiceAPIConfigCandidate struct {
	Provider     string `json:"provider"`
	APIURL       string `json:"apiUrl,omitempty"`
	APIKey       string `json:"apiKey,omitempty"`
	ModelName    string `json:"modelName,omitempty"`
	EndpointType string `json:"endpointType,omitempty"`
	SourcePath   string `json:"sourcePath,omitempty"`
	KeySource    string `json:"keySource,omitempty"`
}

type codexVoiceConfig struct {
	ModelProvider       string
	Model               string
	PreferredAuthMethod string
	Providers           map[string]codexVoiceProvider
}

type codexVoiceProvider struct {
	Name                  string
	BaseURL               string
	WireAPI               string
	EnvKey                string
	RequiresOpenAIAuth    bool
	RequiresOpenAIAuthSet bool
}

func buildVoiceAPIConfigItems() ([]protocol.RuntimeInfoItem, int) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return []protocol.RuntimeInfoItem{{
			Label:     "本机配置",
			Value:     "unavailable",
			Available: false,
			Status:    "missing",
			Detail:    fmt.Sprintf("读取用户目录失败：%v", err),
		}}, 0
	}

	items := []protocol.RuntimeInfoItem{
		buildCodexVoiceAPIConfigItem(homeDir),
		buildClaudeVoiceAPIConfigItem(homeDir),
	}
	availableCount := 0
	for _, item := range items {
		if item.Available {
			availableCount++
		}
	}
	return items, availableCount
}

func buildCodexVoiceAPIConfigItem(homeDir string) protocol.RuntimeInfoItem {
	configPath := filepath.Join(homeDir, ".codex", "config.toml")
	authPath := filepath.Join(homeDir, ".codex", "auth.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return unavailableVoiceAPIConfigItem(
			"Codex",
			"missing",
			fmt.Sprintf("未读取到 ~/.codex/config.toml：%v", err),
			voiceAPIConfigCandidate{
				Provider:   "codex",
				SourcePath: configPath,
			},
		)
	}

	config := parseCodexVoiceConfig(string(data))
	providerName := firstNonEmpty(config.ModelProvider, "openai")
	provider := config.Providers[strings.ToLower(providerName)]
	if strings.TrimSpace(provider.BaseURL) == "" && strings.EqualFold(providerName, "openai") {
		provider = codexVoiceProvider{
			Name:    "OpenAI",
			BaseURL: "https://api.openai.com/v1",
			WireAPI: "responses",
			EnvKey:  "OPENAI_API_KEY",
		}
	}
	if strings.TrimSpace(provider.EnvKey) == "" {
		provider.EnvKey = "OPENAI_API_KEY"
	}

	modelName := cleanVoiceAPIModelName(config.Model)
	apiURL := voiceAPIEndpointURL(provider.BaseURL, codexVoiceEndpointType(provider.WireAPI))
	apiKey, keySource := readCodexVoiceAPIKey(authPath, provider.EnvKey)
	candidate := voiceAPIConfigCandidate{
		Provider:     "codex",
		APIURL:       apiURL,
		APIKey:       apiKey,
		ModelName:    modelName,
		EndpointType: codexVoiceEndpointType(provider.WireAPI),
		SourcePath:   configPath,
		KeySource:    keySource,
	}

	missing := voiceAPIConfigMissingReason(apiURL, apiKey, modelName)
	if missing != "" {
		return unavailableVoiceAPIConfigItem(
			"Codex",
			"missing",
			"Codex 配置不完整："+missing,
			candidate,
		)
	}

	return protocol.RuntimeInfoItem{
		Label:     "Codex",
		Value:     modelName + " · " + candidate.EndpointType,
		Available: true,
		Status:    "ready",
		Detail:    "来自 ~/.codex/config.toml 和 ~/.codex/auth.json",
		Meta:      candidate,
	}
}

func buildClaudeVoiceAPIConfigItem(homeDir string) protocol.RuntimeInfoItem {
	settingsPath := filepath.Join(homeDir, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return unavailableVoiceAPIConfigItem(
			"Claude",
			"missing",
			fmt.Sprintf("未读取到 ~/.claude/settings.json：%v", err),
			voiceAPIConfigCandidate{
				Provider:   "claude",
				SourcePath: settingsPath,
			},
		)
	}

	var settings claudeSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return unavailableVoiceAPIConfigItem(
			"Claude",
			"invalid",
			fmt.Sprintf("解析 ~/.claude/settings.json 失败：%v", err),
			voiceAPIConfigCandidate{
				Provider:   "claude",
				SourcePath: settingsPath,
			},
		)
	}

	env := settings.Env
	modelName := cleanVoiceAPIModelName(firstNonEmpty(
		env["ANTHROPIC_MODEL"],
		settings.Model,
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"],
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"],
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"],
	))
	apiKey, keySource := firstAnthropicAPIKey(env)
	apiURL := voiceAPIEndpointURL(firstNonEmpty(env["ANTHROPIC_BASE_URL"], "https://api.anthropic.com"), "anthropic_messages")
	candidate := voiceAPIConfigCandidate{
		Provider:     "claude",
		APIURL:       apiURL,
		APIKey:       apiKey,
		ModelName:    modelName,
		EndpointType: "anthropic_messages",
		SourcePath:   settingsPath,
		KeySource:    keySource,
	}

	missing := voiceAPIConfigMissingReason(apiURL, apiKey, modelName)
	if missing != "" {
		return unavailableVoiceAPIConfigItem(
			"Claude",
			"missing",
			"Claude 配置不完整："+missing,
			candidate,
		)
	}

	return protocol.RuntimeInfoItem{
		Label:     "Claude",
		Value:     modelName + " · anthropic_messages",
		Available: true,
		Status:    "ready",
		Detail:    "来自 ~/.claude/settings.json",
		Meta:      candidate,
	}
}

func unavailableVoiceAPIConfigItem(label, status, detail string, candidate voiceAPIConfigCandidate) protocol.RuntimeInfoItem {
	candidate.APIKey = ""
	candidate.KeySource = ""
	return protocol.RuntimeInfoItem{
		Label:     label,
		Value:     "unavailable",
		Available: false,
		Status:    status,
		Detail:    detail,
		Meta:      candidate,
	}
}

func voiceAPIConfigMissingReason(apiURL, apiKey, modelName string) string {
	missing := make([]string, 0, 3)
	if strings.TrimSpace(apiURL) == "" {
		missing = append(missing, "API URL")
	}
	if strings.TrimSpace(apiKey) == "" {
		missing = append(missing, "API Key")
	}
	if strings.TrimSpace(modelName) == "" {
		missing = append(missing, "Model Name")
	}
	return strings.Join(missing, "、")
}

func parseCodexVoiceConfig(raw string) codexVoiceConfig {
	config := codexVoiceConfig{
		Providers: map[string]codexVoiceProvider{},
	}
	section := ""
	for _, rawLine := range strings.Split(raw, "\n") {
		line := strings.TrimSpace(stripTomlComment(rawLine))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(strings.Trim(line, "[]"))
			continue
		}
		key, value, ok := splitTomlAssignment(line)
		if !ok {
			continue
		}
		if strings.HasPrefix(section, "model_providers.") {
			name := strings.Trim(strings.TrimPrefix(section, "model_providers."), `"'`)
			if name == "" {
				continue
			}
			normalizedName := strings.ToLower(name)
			provider := config.Providers[normalizedName]
			switch key {
			case "name":
				provider.Name = parseTomlStringValue(value)
			case "base_url":
				provider.BaseURL = parseTomlStringValue(value)
			case "wire_api":
				provider.WireAPI = parseTomlStringValue(value)
			case "env_key":
				provider.EnvKey = parseTomlStringValue(value)
			case "requires_openai_auth":
				provider.RequiresOpenAIAuth = parseTomlBoolValue(value)
				provider.RequiresOpenAIAuthSet = true
			}
			config.Providers[normalizedName] = provider
			continue
		}
		if section != "" {
			continue
		}
		switch key {
		case "model_provider":
			config.ModelProvider = parseTomlStringValue(value)
		case "model":
			config.Model = parseTomlStringValue(value)
		case "preferred_auth_method":
			config.PreferredAuthMethod = parseTomlStringValue(value)
		}
	}
	return config
}

func splitTomlAssignment(line string) (string, string, bool) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

func parseTomlStringValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= 2 {
		quote := trimmed[0]
		if (quote == '"' || quote == '\'') && trimmed[len(trimmed)-1] == quote {
			return strings.TrimSpace(trimmed[1 : len(trimmed)-1])
		}
	}
	return strings.TrimSpace(trimmed)
}

func parseTomlBoolValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

func stripTomlComment(line string) string {
	inSingle := false
	inDouble := false
	escaped := false
	for index, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && inDouble {
			escaped = true
			continue
		}
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return line[:index]
			}
		}
	}
	return line
}

func codexVoiceEndpointType(wireAPI string) string {
	normalized := strings.TrimSpace(strings.ToLower(wireAPI))
	switch normalized {
	case "chat", "chat_completions", "chat-completions":
		return "chat_completions"
	case "responses", "response":
		return "responses"
	default:
		return "responses"
	}
}

func voiceAPIEndpointURL(rawBaseURL, endpointType string) string {
	baseURL := strings.TrimSpace(rawBaseURL)
	if baseURL == "" {
		return ""
	}
	suffix := "chat/completions"
	switch endpointType {
	case "responses":
		suffix = "responses"
	case "anthropic_messages":
		suffix = "messages"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return baseURL
	}
	path := strings.TrimRight(parsed.Path, "/")
	if path == "" {
		path = "/v1/" + suffix
	} else if strings.HasSuffix(path, "/"+suffix) {
		parsed.Path = path
		return parsed.String()
	} else if strings.HasSuffix(path, "/v1") {
		path += "/" + suffix
	} else {
		path += "/" + suffix
	}
	parsed.Path = path
	return parsed.String()
}

func readCodexVoiceAPIKey(authPath, envKey string) (string, string) {
	auth := map[string]string{}
	if data, err := os.ReadFile(authPath); err == nil {
		_ = json.Unmarshal(data, &auth)
	}
	candidates := []string{
		strings.TrimSpace(envKey),
		"CODEX_API_KEY",
		"OPENAI_API_KEY",
	}
	for _, keyName := range uniqueNonEmptyStrings(candidates) {
		if value := strings.TrimSpace(auth[keyName]); value != "" {
			return value, "~/.codex/auth.json:" + keyName
		}
		if value := strings.TrimSpace(os.Getenv(keyName)); value != "" {
			return value, "env:" + keyName
		}
	}
	return "", ""
}

func firstAnthropicAPIKey(env map[string]string) (string, string) {
	for _, keyName := range []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY"} {
		if value := strings.TrimSpace(env[keyName]); value != "" {
			return value, "~/.claude/settings.json:" + keyName
		}
		if value := strings.TrimSpace(os.Getenv(keyName)); value != "" {
			return value, "env:" + keyName
		}
	}
	return "", ""
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToUpper(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*[[:alpha:]]|\[[0-9;]*m\]?`)

func cleanVoiceAPIModelName(value string) string {
	return strings.TrimSpace(ansiEscapePattern.ReplaceAllString(value, ""))
}
