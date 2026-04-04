package runtime

import (
	"context"
	"errors"
	"testing"

	"mobilevc/internal/protocol"
	"mobilevc/internal/runner"
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
	fetchCodexModelCatalog = func(ctx context.Context, command string, cwd string) ([]runner.CodexModelCatalogEntry, error) {
		return []runner.CodexModelCatalogEntry{
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
				ReasoningEffortOptions: []runner.CodexReasoningEffortOption{
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
	meta, ok := item.Meta.(runner.CodexModelCatalogEntry)
	if !ok {
		t.Fatalf("expected runner.CodexModelCatalogEntry meta, got %T", item.Meta)
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
	fetchCodexModelCatalog = func(ctx context.Context, command string, cwd string) ([]runner.CodexModelCatalogEntry, error) {
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
