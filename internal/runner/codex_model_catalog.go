package runner

import (
	"context"
	"fmt"
	"strings"
)

type CodexReasoningEffortOption struct {
	ReasoningEffort string `json:"reasoningEffort"`
	Description     string `json:"description,omitempty"`
}

type CodexModelCatalogEntry struct {
	ID                        string                       `json:"id,omitempty"`
	Model                     string                       `json:"model"`
	DisplayName               string                       `json:"displayName,omitempty"`
	Description               string                       `json:"description,omitempty"`
	DefaultReasoningEffort    string                       `json:"defaultReasoningEffort,omitempty"`
	SupportedReasoningEfforts []string                     `json:"supportedReasoningEfforts,omitempty"`
	ReasoningEffortOptions    []CodexReasoningEffortOption `json:"reasoningEffortOptions,omitempty"`
	IsDefault                 bool                         `json:"isDefault,omitempty"`
	Hidden                    bool                         `json:"hidden,omitempty"`
}

type codexModelListResponse struct {
	Data       []codexModelListItem `json:"data"`
	NextCursor *string              `json:"nextCursor"`
}

type codexModelListItem struct {
	ID                        string                            `json:"id"`
	Model                     string                            `json:"model"`
	DisplayName               string                            `json:"displayName"`
	Description               string                            `json:"description"`
	DefaultReasoningEffort    string                            `json:"defaultReasoningEffort"`
	SupportedReasoningEfforts []codexModelReasoningEffortOption `json:"supportedReasoningEfforts"`
	IsDefault                 bool                              `json:"isDefault"`
	Hidden                    bool                              `json:"hidden"`
}

type codexModelReasoningEffortOption struct {
	ReasoningEffort string `json:"reasoningEffort"`
	Description     string `json:"description"`
}

func FetchCodexModelCatalog(ctx context.Context, command string, cwd string) ([]CodexModelCatalogEntry, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cmd := newCodexAppServerCommand(ctx, command)
	if dir := strings.TrimSpace(cwd); dir != "" {
		cmd.Dir = dir
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create codex app-server stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("create codex app-server stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("create codex app-server stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}

	app := &codexAppSession{
		cwd:     strings.TrimSpace(cwd),
		cmd:     cmd,
		stdin:   stdin,
		stderr:  stderr,
		pending: make(map[string]chan codexRPCResponse),
	}
	go app.readLoop(ctx, stdout)
	go app.readStderr(ctx)
	defer func() {
		_ = app.Close()
		_ = cmd.Wait()
	}()

	if err := app.initialize(ctx); err != nil {
		return nil, err
	}

	var entries []CodexModelCatalogEntry
	cursor := ""
	for {
		params := map[string]any{
			"includeHidden": false,
			"limit":         100,
		}
		if cursor != "" {
			params["cursor"] = cursor
		}

		var resp codexModelListResponse
		if err := app.call(ctx, "model/list", params, &resp); err != nil {
			return nil, err
		}
		entries = append(entries, mapCodexModelCatalogEntries(resp.Data)...)
		if resp.NextCursor == nil || strings.TrimSpace(*resp.NextCursor) == "" {
			break
		}
		cursor = strings.TrimSpace(*resp.NextCursor)
	}

	return entries, nil
}

func mapCodexModelCatalogEntries(items []codexModelListItem) []CodexModelCatalogEntry {
	entries := make([]CodexModelCatalogEntry, 0, len(items))
	for _, item := range items {
		model := strings.TrimSpace(firstNonEmptyString(item.Model, item.ID))
		if model == "" {
			continue
		}

		entry := CodexModelCatalogEntry{
			ID:                     strings.TrimSpace(item.ID),
			Model:                  model,
			DisplayName:            strings.TrimSpace(item.DisplayName),
			Description:            strings.TrimSpace(item.Description),
			DefaultReasoningEffort: strings.ToLower(strings.TrimSpace(item.DefaultReasoningEffort)),
			IsDefault:              item.IsDefault,
			Hidden:                 item.Hidden,
		}
		for _, option := range item.SupportedReasoningEfforts {
			effort := strings.ToLower(strings.TrimSpace(option.ReasoningEffort))
			if effort == "" {
				continue
			}
			entry.SupportedReasoningEfforts = append(entry.SupportedReasoningEfforts, effort)
			entry.ReasoningEffortOptions = append(entry.ReasoningEffortOptions, CodexReasoningEffortOption{
				ReasoningEffort: effort,
				Description:     strings.TrimSpace(option.Description),
			})
		}
		if entry.DefaultReasoningEffort == "" && len(entry.SupportedReasoningEfforts) > 0 {
			entry.DefaultReasoningEffort = entry.SupportedReasoningEfforts[0]
		}
		entries = append(entries, entry)
	}
	return entries
}
