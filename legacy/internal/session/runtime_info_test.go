package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mobilevc/internal/engine"
	"mobilevc/internal/protocol"
)

func TestDetectModelValueCodex(t *testing.T) {
	got := detectModelValue(protocol.RuntimeMeta{
		Command: "codex --help",
		Engine:  "codex",
	})
	if got != "codex" {
		t.Fatalf("expected codex, got %q", got)
	}
}

func TestBuildRuntimeInfoResultCodexModels(t *testing.T) {
	previous := fetchCodexModelCatalog
	fetchCodexModelCatalog = func(ctx context.Context, command string, cwd string) ([]engine.CodexModelCatalogEntry, error) {
		return []engine.CodexModelCatalogEntry{
			{
				ID:                     "model-1",
				Model:                  "gpt-5.4",
				DisplayName:            "GPT-5.4",
				Description:            "旗舰推理模型",
				DefaultReasoningEffort: "high",
				SupportedReasoningEfforts: []string{
					"minimal",
					"low",
					"medium",
					"high",
					"xhigh",
				},
				ReasoningEffortOptions: []engine.CodexReasoningEffortOption{
					{ReasoningEffort: "minimal", Description: "最轻"},
					{ReasoningEffort: "low", Description: "较快"},
					{ReasoningEffort: "medium", Description: "平衡"},
					{ReasoningEffort: "high", Description: "深入"},
					{ReasoningEffort: "xhigh", Description: "最强"},
				},
				IsDefault: true,
			},
		}, nil
	}
	defer func() {
		fetchCodexModelCatalog = previous
	}()

	result, err := BuildRuntimeInfoResult("s1", "codex_models", ".", nil)
	if err != nil {
		t.Fatalf("BuildRuntimeInfoResult returned error: %v", err)
	}
	if result.Query != "codex_models" {
		t.Fatalf("expected codex_models query, got %q", result.Query)
	}
	if result.Unavailable {
		t.Fatalf("expected available catalog, got unavailable result: %#v", result)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 catalog item, got %d", len(result.Items))
	}

	item := result.Items[0]
	if item.Label != "gpt-5.4" {
		t.Fatalf("expected model label gpt-5.4, got %q", item.Label)
	}
	if item.Value != "GPT-5.4" {
		t.Fatalf("expected display value GPT-5.4, got %q", item.Value)
	}
	meta, ok := item.Meta.(engine.CodexModelCatalogEntry)
	if !ok {
		t.Fatalf("expected engine.CodexModelCatalogEntry meta, got %T", item.Meta)
	}
	if meta.DefaultReasoningEffort != "high" {
		t.Fatalf("expected default reasoning effort high, got %q", meta.DefaultReasoningEffort)
	}
	if len(meta.SupportedReasoningEfforts) != 5 || meta.SupportedReasoningEfforts[4] != "xhigh" {
		t.Fatalf("expected xhigh in supported efforts, got %#v", meta.SupportedReasoningEfforts)
	}
}

func TestBuildRuntimeInfoResultCodexModelsUnavailableOnFetchFailure(t *testing.T) {
	previous := fetchCodexModelCatalog
	fetchCodexModelCatalog = func(ctx context.Context, command string, cwd string) ([]engine.CodexModelCatalogEntry, error) {
		return nil, errors.New("codex unavailable")
	}
	defer func() {
		fetchCodexModelCatalog = previous
	}()

	result, err := BuildRuntimeInfoResult("s1", "codex_models", ".", nil)
	if err != nil {
		t.Fatalf("BuildRuntimeInfoResult returned error: %v", err)
	}
	if !result.Unavailable {
		t.Fatalf("expected unavailable result when fetch fails")
	}
	if len(result.Items) != 1 || result.Items[0].Detail != "codex unavailable" {
		t.Fatalf("unexpected unavailable payload: %#v", result.Items)
	}
}

func TestBuildRuntimeInfoResultClaudeModelsFallsBackToNativeCLI(t *testing.T) {
	previousAPI := fetchClaudeModelsFromAPI
	previousNative := fetchClaudeModelsFromNativeCLI
	fetchClaudeModelsFromAPI = func(baseURL, authToken, currentModel string) ([]protocol.RuntimeInfoItem, error) {
		return nil, errors.New("api unavailable")
	}
	fetchClaudeModelsFromNativeCLI = func(cwd, currentModel string) ([]protocol.RuntimeInfoItem, error) {
		if cwd == "" {
			t.Fatal("expected cwd for native CLI fallback")
		}
		if currentModel != "sonnet" {
			t.Fatalf("expected current model sonnet, got %q", currentModel)
		}
		return []protocol.RuntimeInfoItem{
			{Label: "sonnet", Value: "Sonnet", Available: true, Status: "default"},
			{Label: "Opus Plan", Value: "Opus Plan", Available: true, Status: "ready"},
		}, nil
	}
	defer func() {
		fetchClaudeModelsFromAPI = previousAPI
		fetchClaudeModelsFromNativeCLI = previousNative
	}()

	items, err := fetchClaudeModelCatalogWithSettings("/tmp", claudeSettings{
		Model: "sonnet",
		Env: map[string]string{
			"ANTHROPIC_BASE_URL":   "https://api.example.com",
			"ANTHROPIC_AUTH_TOKEN": "secret",
		},
	})
	if err != nil {
		t.Fatalf("fetchClaudeModelCatalogWithSettings returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 Claude items, got %d", len(items))
	}
	if items[1].Label != "Opus Plan" {
		t.Fatalf("expected native fallback item, got %#v", items)
	}
}

func TestBuildRuntimeInfoResultVoiceAPIConfigs(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("CODEX_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	codexDir := filepath.Join(homeDir, ".codex")
	claudeDir := filepath.Join(homeDir, ".claude")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir claude dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(`
model_provider = "custom"
model = "gpt-5.5"

[model_providers.custom]
base_url = "https://api.example.test/v1"
wire_api = "responses"
env_key = "CODEX_API_KEY"
`), 0o600); err != nil {
		t.Fatalf("write codex config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(`{"CODEX_API_KEY":"codex-key"}`), 0o600); err != nil {
		t.Fatalf("write codex auth: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{
  "model": "claude-sonnet-4-20250514",
  "env": {
    "ANTHROPIC_BASE_URL": "https://anthropic.example.test",
    "ANTHROPIC_AUTH_TOKEN": "claude-key",
    "ANTHROPIC_MODEL": "claude-opus-4-20250514"
  }
}`), 0o600); err != nil {
		t.Fatalf("write claude settings: %v", err)
	}

	result, err := BuildRuntimeInfoResult("s1", "voice_api_configs", ".", nil)
	if err != nil {
		t.Fatalf("BuildRuntimeInfoResult returned error: %v", err)
	}
	if result.Unavailable {
		t.Fatalf("expected voice api configs to be available: %#v", result)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 voice api config items, got %d", len(result.Items))
	}

	codexCandidate, ok := result.Items[0].Meta.(voiceAPIConfigCandidate)
	if !ok {
		t.Fatalf("expected codex candidate meta, got %T", result.Items[0].Meta)
	}
	if codexCandidate.Provider != "codex" ||
		codexCandidate.APIURL != "https://api.example.test/v1/responses" ||
		codexCandidate.APIKey != "codex-key" ||
		codexCandidate.ModelName != "gpt-5.5" ||
		codexCandidate.EndpointType != "responses" {
		t.Fatalf("unexpected codex candidate: %#v", codexCandidate)
	}

	claudeCandidate, ok := result.Items[1].Meta.(voiceAPIConfigCandidate)
	if !ok {
		t.Fatalf("expected claude candidate meta, got %T", result.Items[1].Meta)
	}
	if claudeCandidate.Provider != "claude" ||
		claudeCandidate.APIURL != "https://anthropic.example.test/v1/messages" ||
		claudeCandidate.APIKey != "claude-key" ||
		claudeCandidate.ModelName != "claude-opus-4-20250514" ||
		claudeCandidate.EndpointType != "anthropic_messages" {
		t.Fatalf("unexpected claude candidate: %#v", claudeCandidate)
	}
}

func TestParseClaudeModelCLIOutput(t *testing.T) {
	items, err := parseClaudeModelCLIOutput("Available models:\n- Sonnet\n- Opus Plan (planning)\n- claude-sonnet-4-20250514\n", "Sonnet")
	if err != nil {
		t.Fatalf("parseClaudeModelCLIOutput returned error: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 parsed models, got %d", len(items))
	}
	if items[0].Status != "default" {
		t.Fatalf("expected first item to be default, got %#v", items[0])
	}
	if items[1].Label != "Opus Plan" {
		t.Fatalf("expected Opus Plan label, got %#v", items[1])
	}
	if items[2].Label != "claude-sonnet-4-20250514" {
		t.Fatalf("expected pinned model label, got %#v", items[2])
	}
}
