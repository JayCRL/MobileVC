package session

import (
	"fmt"
	"strings"
	"time"

	"mobilevc/internal/data"
	"mobilevc/internal/protocol"
)

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type PermissionMatchContext struct {
	Engine      string
	Kind        data.PermissionKind
	CommandHead string
	TargetPath  string
}

func PermissionContextFromDecision(req protocol.PermissionDecisionRequestEvent, projection data.ProjectionSnapshot, controller ControllerSnapshot) PermissionMatchContext {
	command := firstNonEmptyString(
		req.FallbackCommand,
		projection.Runtime.Command,
		controller.CurrentCommand,
		controller.ActiveMeta.Command,
	)
	targetPath := firstNonEmptyString(
		req.TargetPath,
		controller.ActiveMeta.TargetPath,
		projection.Runtime.CWD,
	)
	engine := strings.TrimSpace(strings.ToLower(firstNonEmptyString(
		req.FallbackEngine,
		controller.ActiveMeta.Engine,
		projection.Runtime.Engine,
	)))
	if engine == "" {
		engine = "any"
	}
	return PermissionMatchContext{
		Engine:      engine,
		Kind:        ClassifyPermissionKind(req.PromptMessage, targetPath, command),
		CommandHead: PermissionCommandHead(command),
		TargetPath:  targetPath,
	}
}

func PermissionContextFromPrompt(promptMessage string, meta protocol.RuntimeMeta, projection data.ProjectionSnapshot, controller ControllerSnapshot) PermissionMatchContext {
	command := firstNonEmptyString(meta.Command, projection.Runtime.Command, controller.CurrentCommand, controller.ActiveMeta.Command)
	targetPath := firstNonEmptyString(meta.TargetPath, controller.ActiveMeta.TargetPath)
	engine := strings.TrimSpace(strings.ToLower(firstNonEmptyString(meta.Engine, controller.ActiveMeta.Engine, projection.Runtime.Engine)))
	if engine == "" {
		engine = "any"
	}
	return PermissionMatchContext{
		Engine:      engine,
		Kind:        ClassifyPermissionKind(promptMessage, targetPath, command),
		CommandHead: PermissionCommandHead(command),
		TargetPath:  targetPath,
	}
}

func ClassifyPermissionKind(promptMessage, targetPath, command string) data.PermissionKind {
	lowerPrompt := strings.ToLower(strings.TrimSpace(promptMessage))
	lowerCommand := strings.ToLower(strings.TrimSpace(command))
	switch {
	case strings.Contains(lowerPrompt, "network"),
		strings.Contains(lowerPrompt, "联网"),
		strings.Contains(lowerPrompt, "网络"):
		return data.PermissionKindNetwork
	case strings.Contains(lowerPrompt, "command"),
		strings.Contains(lowerPrompt, "命令"),
		(strings.TrimSpace(lowerCommand) != "" && targetPath == ""):
		return data.PermissionKindShell
	case strings.Contains(lowerPrompt, "修改文件"),
		strings.Contains(lowerPrompt, "write"),
		strings.Contains(lowerPrompt, "edit"),
		strings.Contains(lowerPrompt, "文件"):
		return data.PermissionKindWrite
	default:
		return data.PermissionKindGeneric
	}
}

func PermissionCommandHead(command string) string {
	fields := strings.Fields(strings.TrimSpace(strings.ToLower(command)))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func BuildPermissionRule(req protocol.PermissionDecisionRequestEvent, scope string, projection data.ProjectionSnapshot, controller ControllerSnapshot) data.PermissionRule {
	ctx := PermissionContextFromDecision(req, projection, controller)
	now := time.Now().UTC()
	rule := data.PermissionRule{
		Scope:            data.PermissionScope(strings.TrimSpace(scope)),
		Enabled:          true,
		Engine:           ctx.Engine,
		Kind:             ctx.Kind,
		CommandHead:      ctx.CommandHead,
		TargetPathPrefix: strings.TrimSpace(req.TargetPath),
		CreatedAt:        now,
		Summary:          SummarizePermissionRule(ctx),
	}
	if rule.Scope == "" {
		rule.Scope = data.PermissionScopeSession
	}
	rule.ID = PermissionRuleID(rule)
	return rule
}

func SummarizePermissionRule(ctx PermissionMatchContext) string {
	parts := []string{}
	if ctx.Engine != "" && ctx.Engine != "any" {
		parts = append(parts, strings.ToUpper(ctx.Engine[:1])+ctx.Engine[1:])
	}
	if ctx.Kind != "" {
		parts = append(parts, string(ctx.Kind))
	}
	if ctx.CommandHead != "" {
		parts = append(parts, ctx.CommandHead)
	}
	if strings.TrimSpace(ctx.TargetPath) != "" {
		parts = append(parts, ctx.TargetPath)
	}
	if len(parts) == 0 {
		return "自动允许规则"
	}
	return strings.Join(parts, " · ")
}

func PermissionRuleID(rule data.PermissionRule) string {
	return fmt.Sprintf(
		"%s|%s|%s|%s|%s",
		strings.TrimSpace(string(rule.Scope)),
		strings.TrimSpace(rule.Engine),
		strings.TrimSpace(string(rule.Kind)),
		strings.TrimSpace(rule.CommandHead),
		strings.TrimSpace(rule.TargetPathPrefix),
	)
}

func MatchPermissionRule(items []data.PermissionRule, ctx PermissionMatchContext) (data.PermissionRule, bool) {
	for _, item := range items {
		if !item.Enabled {
			continue
		}
		if item.Engine != "" && item.Engine != "any" && item.Engine != ctx.Engine {
			continue
		}
		if item.Kind != "" && item.Kind != data.PermissionKindGeneric && item.Kind != ctx.Kind {
			continue
		}
		if item.CommandHead != "" && item.CommandHead != ctx.CommandHead {
			continue
		}
		if item.TargetPathPrefix != "" && !strings.HasPrefix(ctx.TargetPath, item.TargetPathPrefix) {
			continue
		}
		return item, true
	}
	return data.PermissionRule{}, false
}

func MarkPermissionRuleMatched(items []data.PermissionRule, id string) []data.PermissionRule {
	now := time.Now().UTC()
	out := make([]data.PermissionRule, 0, len(items))
	for _, item := range items {
		if item.ID == id {
			item.MatchCount++
			item.LastMatchedAt = now
		}
		out = append(out, item)
	}
	return out
}

func BuildPermissionDecisionFromEvent(
	sessionID string,
	message string,
	meta protocol.RuntimeMeta,
	projection data.ProjectionSnapshot,
	controller ControllerSnapshot,
) protocol.PermissionDecisionRequestEvent {
	return protocol.PermissionDecisionRequestEvent{
		ClientEvent:         protocol.ClientEvent{Action: "permission_decision"},
		Decision:            "approve",
		PermissionMode:      firstNonEmptyString(meta.PermissionMode, controller.ActiveMeta.PermissionMode, projection.Runtime.PermissionMode),
		PermissionRequestID: strings.TrimSpace(meta.PermissionRequestID),
		ResumeSessionID:     firstNonEmptyString(meta.ResumeSessionID, controller.ResumeSession, controller.ActiveMeta.ResumeSessionID, projection.Runtime.ResumeSessionID),
		TargetPath:          strings.TrimSpace(meta.TargetPath),
		ContextID:           strings.TrimSpace(meta.ContextID),
		ContextTitle:        strings.TrimSpace(meta.ContextTitle),
		PromptMessage:       strings.TrimSpace(message),
		FallbackCommand:     firstNonEmptyString(meta.Command, projection.Runtime.Command, controller.CurrentCommand, controller.ActiveMeta.Command),
		FallbackCWD:         firstNonEmptyString(meta.CWD, controller.ActiveMeta.CWD, projection.Runtime.CWD),
		FallbackEngine:      firstNonEmptyString(meta.Engine, controller.ActiveMeta.Engine, projection.Runtime.Engine),
		FallbackTarget:      strings.TrimSpace(meta.Target),
		FallbackTargetType:  strings.TrimSpace(meta.TargetType),
	}
}

func LooksLikePermissionPromptForRule(event protocol.PromptRequestEvent) bool {
	if len(event.Options) >= 2 {
		first := strings.ToLower(strings.TrimSpace(event.Options[0]))
		second := strings.ToLower(strings.TrimSpace(event.Options[1]))
		if (first == "y" || first == "yes" || first == "approve") && (second == "n" || second == "no" || second == "deny") {
			return true
		}
	}
	kind := ClassifyPermissionKind(event.Message, strings.TrimSpace(event.RuntimeMeta.TargetPath), strings.TrimSpace(event.RuntimeMeta.Command))
	return kind != "" && kind != data.PermissionKindGeneric
}

func LooksLikePermissionInteractionForRule(event protocol.InteractionRequestEvent) bool {
	kind := strings.ToLower(strings.TrimSpace(event.Kind))
	if strings.Contains(kind, "permission") {
		return true
	}
	hasApprove := false
	hasDeny := false
	for _, action := range event.Actions {
		value := strings.ToLower(strings.TrimSpace(firstNonEmptyString(action.Decision, action.Value, action.Label)))
		switch value {
		case "approve", "allow", "accept", "yes", "y":
			hasApprove = true
		case "deny", "reject", "no", "n":
			hasDeny = true
		}
	}
	if hasApprove && hasDeny {
		return true
	}
	message := firstNonEmptyString(event.Message, event.Title)
	targetPath := firstNonEmptyString(event.TargetPath, event.RuntimeMeta.TargetPath)
	derivedKind := ClassifyPermissionKind(message, strings.TrimSpace(targetPath), strings.TrimSpace(event.RuntimeMeta.Command))
	return derivedKind != "" && derivedKind != data.PermissionKindGeneric
}
