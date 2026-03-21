package skills

import (
	"fmt"
	"strings"

	"mobilevc/internal/runner"
	"mobilevc/internal/runtime"
)

type Launcher struct {
	registry map[string]Definition
}

func NewLauncher() *Launcher {
	return &Launcher{registry: Builtins()}
}

func (l *Launcher) BuildRequest(name, engine, cwd, targetType, targetPath, targetTitle, targetDiff, contextID, contextTitle, targetText, targetStack string) (runtime.ExecuteRequest, error) {
	def, ok := l.registry[strings.TrimSpace(name)]
	if !ok {
		return runtime.ExecuteRequest{}, fmt.Errorf("unknown skill: %s", name)
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

	aiCmd := "claude"
	if strings.TrimSpace(engine) == "gemini" {
		aiCmd = "gemini"
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
	// Find the first quoted segment
	idx := strings.IndexByte(trimmed, '"')
	if idx < 0 {
		return ""
	}
	rest := trimmed[idx+1:]
	// Find closing quote (handle escaped quotes)
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
