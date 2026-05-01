package session

import (
	"fmt"
	"strings"

	"mobilevc/internal/data"
	"mobilevc/internal/protocol"
)

type PermissionDecisionAction string

const (
	PermissionDecisionActionDirect         PermissionDecisionAction = "direct"
	PermissionDecisionActionDenyThenInput  PermissionDecisionAction = "deny_then_input"
	PermissionDecisionActionAutoThenDirect PermissionDecisionAction = "auto_then_direct"
)

type PermissionDecisionPlan struct {
	Action   PermissionDecisionAction
	Decision string
	Meta     protocol.RuntimeMeta
	Prompt   string
}

func BuildPermissionDecisionPlan(req protocol.PermissionDecisionRequestEvent, projection data.ProjectionSnapshot, controller ControllerSnapshot) (PermissionDecisionPlan, error) {
	decision := strings.TrimSpace(strings.ToLower(req.Decision))
	effectivePermissionMode := strings.TrimSpace(req.PermissionMode)
	if effectivePermissionMode == "" {
		effectivePermissionMode = strings.TrimSpace(controller.ActiveMeta.PermissionMode)
	}
	if effectivePermissionMode == "" {
		effectivePermissionMode = strings.TrimSpace(projection.Runtime.PermissionMode)
	}
	meta := protocol.RuntimeMeta{
		Source:              "permission-decision",
		ResumeSessionID:     firstNonEmptyString(req.ResumeSessionID, controller.ResumeSession, controller.ActiveMeta.ResumeSessionID, projection.Runtime.ResumeSessionID),
		ContextID:           firstNonEmptyString(req.ContextID, controller.ActiveMeta.ContextID),
		ContextTitle:        firstNonEmptyString(req.ContextTitle, controller.ActiveMeta.ContextTitle),
		TargetPath:          firstNonEmptyString(req.TargetPath, controller.ActiveMeta.TargetPath),
		TargetText:          decision,
		Command:             firstNonEmptyString(req.FallbackCommand, projection.Runtime.Command, controller.CurrentCommand, controller.ActiveMeta.Command),
		Engine:              firstNonEmptyString(req.FallbackEngine, controller.ActiveMeta.Engine),
		CWD:                 firstNonEmptyString(req.FallbackCWD, controller.ActiveMeta.CWD, projection.Runtime.CWD),
		Target:              firstNonEmptyString(req.FallbackTarget, controller.ActiveMeta.Target),
		TargetType:          firstNonEmptyString(req.FallbackTargetType, controller.ActiveMeta.TargetType),
		PermissionMode:      effectivePermissionMode,
		PermissionRequestID: strings.TrimSpace(req.PermissionRequestID),
	}
	if !IsClaudeCommandLike(meta.Command) {
		return PermissionDecisionPlan{Action: PermissionDecisionActionDirect, Decision: decision, Meta: meta}, nil
	}
	if decision == "deny" {
		prompt, err := BuildPermissionDecisionPrompt(decision, req)
		if err != nil {
			return PermissionDecisionPlan{}, err
		}
		return PermissionDecisionPlan{Action: PermissionDecisionActionDenyThenInput, Decision: decision, Meta: meta, Prompt: prompt}, nil
	}
	if strings.TrimSpace(meta.PermissionMode) != "auto" {
		meta.PermissionMode = "auto"
		return PermissionDecisionPlan{Action: PermissionDecisionActionAutoThenDirect, Decision: decision, Meta: meta}, nil
	}
	return PermissionDecisionPlan{Action: PermissionDecisionActionDirect, Decision: decision, Meta: meta}, nil
}

func BuildPermissionDecisionPrompt(decision string, req protocol.PermissionDecisionRequestEvent) (string, error) {
	decision = strings.TrimSpace(strings.ToLower(decision))
	if decision == "" {
		return "", fmt.Errorf("permission decision is required")
	}
	subject := strings.TrimSpace(req.TargetPath)
	if subject == "" {
		subject = strings.TrimSpace(req.ContextTitle)
	}
	if subject == "" {
		subject = "刚才请求的操作"
	}
	lines := []string{}
	if req.PermissionRequestID != "" {
		lines = append(lines, fmt.Sprintf("PermissionRequestID: %s", req.PermissionRequestID))
	}
	if req.ResumeSessionID != "" {
		lines = append(lines, fmt.Sprintf("ResumeSessionID: %s", req.ResumeSessionID))
	}
	if req.TargetPath != "" {
		lines = append(lines, fmt.Sprintf("TargetPath: %s", req.TargetPath))
	}
	if req.ContextID != "" {
		lines = append(lines, fmt.Sprintf("ContextID: %s", req.ContextID))
	}
	if req.ContextTitle != "" {
		lines = append(lines, fmt.Sprintf("ContextTitle: %s", req.ContextTitle))
	}
	if req.PermissionMode != "" {
		lines = append(lines, fmt.Sprintf("PermissionMode: %s", req.PermissionMode))
	}
	if strings.TrimSpace(req.PromptMessage) != "" {
		lines = append(lines, fmt.Sprintf("OriginalPrompt: %s", strings.TrimSpace(req.PromptMessage)))
	}
	switch decision {
	case "approve":
		switch PermissionDecisionIntent(req) {
		case "read":
			return fmt.Sprintf("用户已批准刚才请求的只读/查看权限。请在当前已保存的会话上下文中继续刚才被权限拦截的读取或查看操作，不要执行编辑、写入或其它副作用动作。目标：%s\n%s\n", subject, strings.Join(lines, "\n")), nil
		case data.PermissionKindShell:
			return fmt.Sprintf("用户已批准刚才请求的命令执行权限。请在当前已保存的会话上下文中继续刚才被权限拦截的命令执行；请重新发起新的命令调用，不要把该权限解释为文件写入任务。目标：%s\n%s\n", subject, strings.Join(lines, "\n")), nil
		default:
			return fmt.Sprintf("用户已批准刚才请求的文件修改/写入权限。请在当前已保存的会话上下文中继续刚才被权限拦截的任务。执行要求：先重新读取目标文件的当前内容，再基于最新内容继续刚才的修改；只有在重新读取完成后，才重试刚才被拦截的编辑或写入工具调用。不要再次向用户请求同一权限，直接继续完成即可。目标：%s\n%s\n", subject, strings.Join(lines, "\n")), nil
		}
	case "deny":
		return fmt.Sprintf("用户拒绝了刚才请求的文件修改/写入权限。请不要继续写入或编辑该目标，并基于当前上下文给出不写文件的替代方案或下一步建议。目标：%s\n%s\n", subject, strings.Join(lines, "\n")), nil
	default:
		return "", fmt.Errorf("permission decision must be one of: approve, deny")
	}
}

func PermissionDecisionIntent(req protocol.PermissionDecisionRequestEvent) data.PermissionKind {
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		req.PromptMessage,
		req.FallbackTargetType,
		req.FallbackCommand,
	}, " ")))
	switch {
	case strings.Contains(text, "read"), strings.Contains(text, "view"), strings.Contains(text, "查看"), strings.Contains(text, "读取"):
		return "read"
	case strings.Contains(text, "bash"), strings.Contains(text, "command"), strings.Contains(text, "命令"):
		return data.PermissionKindShell
	default:
		return data.PermissionKindWrite
	}
}

func IsClaudeCommandLike(command string) bool {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return false
	}
	head := strings.ToLower(strings.TrimSpace(fields[0]))
	return head == "claude" || strings.HasSuffix(head, "/claude") || strings.HasSuffix(head, `\\claude`) || head == "claude.exe"
}
