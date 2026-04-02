package skills

import (
	"strings"

	"mobilevc/internal/store"
)

const enabledSkillsPrefixHeader = "[MobileVC Enabled Skills]"
const enabledMemoryPrefixHeader = "[MobileVC Memory]"

// BuildEnabledSkillsPrefix renders the currently enabled MobileVC skills as
// injected conversation context for normal AI chat turns.
func BuildEnabledSkillsPrefix(skillStore store.Store, sessionContext store.SessionContext) (string, error) {
	if len(sessionContext.EnabledSkillNames) == 0 {
		return "", nil
	}

	registry := NewRegistry(skillStore)
	items, err := registry.ListSkills()
	if err != nil {
		return "", err
	}

	defsByName := make(map[string]Definition, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		defsByName[name] = item
	}

	parts := []string{
		enabledSkillsPrefixHeader,
		"以下内容由 MobileVC 会话上下文注入，表示当前会话在 UI 中显式启用的 skills。它们不是 Codex/Claude CLI 自动发现结果；如果用户询问当前启用了哪些 skill，请以这里为准。",
		"已启用 skills：",
	}

	seen := make(map[string]struct{}, len(sessionContext.EnabledSkillNames))
	for _, rawName := range sessionContext.EnabledSkillNames {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}

		body := "- " + name
		if def, ok := defsByName[name]; ok {
			if desc := strings.TrimSpace(def.Description); desc != "" {
				body += "\n  描述：" + desc
			}
			if targetType := strings.TrimSpace(def.TargetType); targetType != "" {
				body += "\n  适用上下文：" + targetType
			}
			if prompt := strings.TrimSpace(def.Prompt); prompt != "" {
				body += "\n  使用要求：" + prompt
			}
		}
		parts = append(parts, body)
	}

	if len(parts) <= 3 {
		return "", nil
	}
	parts = append(parts, "在后续回答中，优先复用这些已启用 skills；如果用户直接询问当前启用了哪些 skill，请直接列出上面的项目，不要回答“没有启用”。")
	return strings.Join(parts, "\n\n"), nil
}

func BuildEnabledMemoryPrefix(memoryStore store.Store, sessionContext store.SessionContext) (string, error) {
	items, err := loadEnabledMemoryItems(memoryStore, sessionContext)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", nil
	}

	parts := make([]string, 0, len(items)+3)
	parts = append(parts, enabledMemoryPrefixHeader)
	parts = append(parts, "以下内容来自当前会话启用且已同步的 MobileVC memory 镜像。它们是当前会话显式启用的记忆，不是模型自行回忆出的内容；如果用户询问当前启用了哪些 memory，请以这里为准。")
	parts = append(parts, "已启用 memories：")
	for _, item := range items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = item.ID
		}
		parts = append(parts, "- "+title+"\n"+strings.TrimSpace(item.Content))
	}
	parts = append(parts, "在后续回答中，优先参考这些 memories；如果用户直接询问当前启用了哪些 memory，请直接列出上面的项目，不要回答“没有启用”。")
	return strings.Join(parts, "\n\n"), nil
}

func InjectEnabledSkillsPrefix(input, prefix string) string {
	return InjectConversationPrefixes(input, prefix)
}

func InjectConversationPrefixes(input string, prefixes ...string) string {
	trimmedPrefixes := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		trimmed := strings.TrimSpace(prefix)
		if trimmed == "" {
			continue
		}
		trimmedPrefixes = append(trimmedPrefixes, trimmed)
	}
	if len(trimmedPrefixes) == 0 {
		return input
	}
	if strings.Contains(input, enabledSkillsPrefixHeader) || strings.Contains(input, enabledMemoryPrefixHeader) {
		return input
	}
	return strings.Join(trimmedPrefixes, "\n\n") + "\n\n[User Input]\n" + input
}

func loadEnabledMemoryItems(memoryStore store.Store, sessionContext store.SessionContext) ([]store.MemoryItem, error) {
	if memoryStore == nil || len(sessionContext.EnabledMemoryIDs) == 0 {
		return nil, nil
	}
	items, err := memoryStore.ListMemoryCatalog(contextBackground())
	if err != nil {
		return nil, err
	}
	enabled := make(map[string]struct{}, len(sessionContext.EnabledMemoryIDs))
	for _, id := range sessionContext.EnabledMemoryIDs {
		enabled[strings.TrimSpace(id)] = struct{}{}
	}
	result := make([]store.MemoryItem, 0, len(enabled))
	for _, item := range items {
		if _, ok := enabled[strings.TrimSpace(item.ID)]; !ok {
			continue
		}
		if item.SyncState != store.CatalogSyncStateSynced {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}
