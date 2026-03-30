package skills

import (
	"fmt"
	"strings"

	"mobilevc/internal/runner"
	"mobilevc/internal/runtime"
	"mobilevc/internal/store"
)

type Launcher struct {
	registry *Registry
	store    store.Store
}

func NewLauncher(skillStore store.Store) *Launcher {
	return &Launcher{registry: NewRegistry(skillStore), store: skillStore}
}

func (l *Launcher) BuildRequest(name, engine, cwd, targetType, targetPath, targetTitle, targetDiff, contextID, contextTitle, targetText, targetStack string, sessionContext store.SessionContext) (runtime.ExecuteRequest, error) {
	def, ok, err := l.registry.GetSkill(strings.TrimSpace(name))
	if err != nil {
		return runtime.ExecuteRequest{}, err
	}
	if !ok {
		return runtime.ExecuteRequest{}, fmt.Errorf("unknown skill: %s", name)
	}
	if !isSkillEnabled(sessionContext, def.Name) {
		return runtime.ExecuteRequest{}, fmt.Errorf("当前 skill 未在本会话启用，请先到 Skill 管理界面勾选")
	}

	resolvedTargetType := strings.TrimSpace(targetType)
	if resolvedTargetType == "" {
		resolvedTargetType = def.TargetType
	}
	resolvedContextTitle := strings.TrimSpace(contextTitle)
	if resolvedContextTitle == "" {
		resolvedContextTitle = strings.TrimSpace(targetTitle)
	}
	if resolvedContextTitle == "" {
		resolvedContextTitle = resolvedTargetType
	}

	prompt, err := l.buildPrompt(def, resolvedTargetType, targetPath, resolvedContextTitle, targetDiff, targetText, targetStack)
	if err != nil {
		return runtime.ExecuteRequest{}, err
	}
	memoryItems, err := l.loadEnabledMemory(sessionContext)
	if err != nil {
		return runtime.ExecuteRequest{}, err
	}
	memoryPrefix := BuildMemoryPrefix(sessionContext, memoryItems)
	if memoryPrefix != "" {
		prompt = memoryPrefix + "\n\n" + prompt
	}

	aiCmd := "claude"
	switch strings.TrimSpace(strings.ToLower(engine)) {
	case "gemini":
		aiCmd = "gemini"
	case "codex":
		aiCmd = "codex"
	}

	return runtime.ExecuteRequest{
		Command: aiCmd + " " + quotePrompt(prompt),
		CWD:     cwd,
		Mode:    runner.ModeExec,
		RuntimeMeta: MetaForSkill(
			def,
			resolvedContextTitle,
			targetPath,
			contextID,
			resolvedContextTitle,
			strings.TrimSpace(targetText),
		),
	}, nil
}

func (l *Launcher) buildPrompt(def Definition, targetType, targetPath, contextTitle, targetDiff, targetText, targetStack string) (string, error) {
	switch normalizeContextType(targetType) {
	case "diff":
		return buildDiffPrompt(def, targetPath, contextTitle, targetDiff)
	case "step":
		return buildStepPrompt(def, targetPath, contextTitle, targetText)
	case "error":
		return buildErrorPrompt(def, targetPath, contextTitle, targetText, targetStack)
	case "context":
		return buildContextPrompt(def, targetPath, contextTitle, targetText)
	default:
		return "", fmt.Errorf("unsupported skill context: %s", targetType)
	}
}

func normalizeContextType(targetType string) string {
	switch strings.TrimSpace(targetType) {
	case "current-diff", "diff":
		return "diff"
	case "current-step", "step":
		return "step"
	case "current-error", "error":
		return "error"
	case "current-context", "context":
		return "context"
	default:
		return strings.TrimSpace(targetType)
	}
}

func buildDiffPrompt(def Definition, targetPath, contextTitle, targetDiff string) (string, error) {
	body := strings.TrimSpace(targetDiff)
	if body == "" {
		return "", fmt.Errorf("target diff is required for %s", def.Name)
	}
	prompt := strings.TrimSpace(def.Prompt)
	if contextTitle != "" {
		prompt += "\n\n上下文标题：" + contextTitle
	}
	if targetPath != "" {
		prompt += "\n目标路径：" + targetPath
	}
	prompt += "\n\n```diff\n" + body + "\n```"
	return prompt, nil
}

func buildStepPrompt(def Definition, targetPath, contextTitle, targetText string) (string, error) {
	body := strings.TrimSpace(targetText)
	if body == "" {
		return "", fmt.Errorf("target text is required for %s", def.Name)
	}
	prompt := strings.TrimSpace(def.Prompt)
	if contextTitle != "" {
		prompt += "\n\n上下文标题：" + contextTitle
	}
	if targetPath != "" {
		prompt += "\n目标路径：" + targetPath
	}
	prompt += "\n\n步骤上下文：\n" + body
	return prompt, nil
}

func buildErrorPrompt(def Definition, targetPath, contextTitle, targetText, targetStack string) (string, error) {
	message := strings.TrimSpace(targetText)
	stack := strings.TrimSpace(targetStack)
	if message == "" && stack == "" {
		return "", fmt.Errorf("target text or stack is required for %s", def.Name)
	}
	prompt := strings.TrimSpace(def.Prompt)
	if contextTitle != "" {
		prompt += "\n\n上下文标题：" + contextTitle
	}
	if targetPath != "" {
		prompt += "\n目标路径：" + targetPath
	}
	if message != "" {
		prompt += "\n\n错误信息：\n" + message
	}
	if stack != "" {
		prompt += "\n\n错误堆栈：\n```text\n" + stack + "\n```"
	}
	return prompt, nil
}

func buildContextPrompt(def Definition, targetPath, contextTitle, targetText string) (string, error) {
	prompt := strings.TrimSpace(def.Prompt)
	if contextTitle != "" {
		prompt += "\n\n上下文标题：" + contextTitle
	}
	if targetPath != "" {
		prompt += "\n目标路径：" + targetPath
	}
	body := strings.TrimSpace(targetText)
	if body != "" {
		prompt += "\n\n当前补充上下文：\n" + body
	}
	return prompt, nil
}

func BuildMemoryPrefix(sessionContext store.SessionContext, items []store.MemoryItem) string {
	if len(sessionContext.EnabledMemoryIDs) == 0 || len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items)+2)
	parts = append(parts, "[MobileVC Memory]")
	parts = append(parts, "以下内容来自当前会话启用且已同步的 MobileVC memory 镜像，请在回答与执行 skill 时一并参考：")
	for _, item := range items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = item.ID
		}
		parts = append(parts, "- "+title+"\n"+strings.TrimSpace(item.Content))
	}
	return strings.Join(parts, "\n\n")
}

func (l *Launcher) loadEnabledMemory(sessionContext store.SessionContext) ([]store.MemoryItem, error) {
	if l == nil || l.store == nil || len(sessionContext.EnabledMemoryIDs) == 0 {
		return nil, nil
	}
	items, err := l.store.ListMemoryCatalog(contextBackground())
	if err != nil {
		return nil, err
	}
	enabled := make(map[string]struct{}, len(sessionContext.EnabledMemoryIDs))
	for _, id := range sessionContext.EnabledMemoryIDs {
		enabled[strings.TrimSpace(id)] = struct{}{}
	}
	result := make([]store.MemoryItem, 0, len(enabled))
	for _, item := range items {
		if _, ok := enabled[item.ID]; !ok {
			continue
		}
		if item.SyncState != store.CatalogSyncStateSynced {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}

func isSkillEnabled(sessionContext store.SessionContext, skillName string) bool {
	needle := strings.TrimSpace(skillName)
	for _, item := range sessionContext.EnabledSkillNames {
		if strings.TrimSpace(item) == needle {
			return true
		}
	}
	return false
}

func quotePrompt(prompt string) string {
	escaped := strings.ReplaceAll(prompt, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	escaped = strings.ReplaceAll(escaped, "\n", `\n`)
	return `"` + escaped + `"`
}

// ExtractPrompt extracts the prompt text from a command string built by BuildRequest.
// The command is in the form: `claude "escaped prompt"` — we extract and unescape the quoted part.
func (l *Launcher) ExtractPrompt(command string) string {
	trimmed := strings.TrimSpace(command)
	idx := strings.IndexByte(trimmed, '"')
	if idx < 0 {
		return ""
	}
	rest := trimmed[idx+1:]
	var result strings.Builder
	i := 0
	for i < len(rest) {
		if rest[i] == '\\' && i+1 < len(rest) {
			switch rest[i+1] {
			case '"':
				result.WriteByte('"')
			case 'n':
				result.WriteByte('\n')
			case '\\':
				result.WriteByte('\\')
			default:
				result.WriteByte(rest[i])
				result.WriteByte(rest[i+1])
			}
			i += 2
			continue
		}
		if rest[i] == '"' {
			break
		}
		result.WriteByte(rest[i])
		i++
	}
	return result.String()
}
