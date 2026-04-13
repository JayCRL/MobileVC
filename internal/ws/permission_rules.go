package ws

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mobilevc/internal/protocol"
	runtimepkg "mobilevc/internal/runtime"
	"mobilevc/internal/session"
	"mobilevc/internal/store"
)

type permissionMatchContext struct {
	Engine      string
	Kind        store.PermissionKind
	CommandHead string
	TargetPath  string
}

func emitPermissionRuleList(emit func(any), sessionStore store.Store, ctx context.Context, sessionID string) {
	if sessionStore == nil {
		emit(protocol.NewErrorEvent(sessionID, "session store unavailable", ""))
		return
	}
	sessionEnabled := true
	sessionRules := []protocol.PermissionRule{}
	if strings.TrimSpace(sessionID) != "" {
		record, err := sessionStore.GetSession(ctx, sessionID)
		if err == nil {
			record.Projection = normalizeProjectionSnapshot(record.Projection)
			sessionEnabled = record.Projection.PermissionRulesEnabled
			sessionRules = toProtocolPermissionRules(record.Projection.PermissionRules)
		}
	}
	persistentSnapshot, err := sessionStore.GetPermissionRuleSnapshot(ctx)
	if err != nil {
		emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
		return
	}
	emit(protocol.NewPermissionRuleListResultEvent(
		sessionID,
		sessionEnabled,
		persistentSnapshot.Enabled,
		sessionRules,
		toProtocolPermissionRules(persistentSnapshot.Items),
	))
}

func toProtocolPermissionRules(items []store.PermissionRule) []protocol.PermissionRule {
	result := make([]protocol.PermissionRule, 0, len(items))
	for _, item := range items {
		result = append(result, toProtocolPermissionRule(item))
	}
	return result
}

func toProtocolPermissionRule(item store.PermissionRule) protocol.PermissionRule {
	createdAt := ""
	if !item.CreatedAt.IsZero() {
		createdAt = item.CreatedAt.Format(time.RFC3339)
	}
	lastMatchedAt := ""
	if !item.LastMatchedAt.IsZero() {
		lastMatchedAt = item.LastMatchedAt.Format(time.RFC3339)
	}
	return protocol.PermissionRule{
		ID:               item.ID,
		Scope:            string(item.Scope),
		Enabled:          item.Enabled,
		Engine:           item.Engine,
		Kind:             string(item.Kind),
		CommandHead:      item.CommandHead,
		TargetPathPrefix: item.TargetPathPrefix,
		Summary:          item.Summary,
		CreatedAt:        createdAt,
		LastMatchedAt:    lastMatchedAt,
		MatchCount:       item.MatchCount,
	}
}

func fromProtocolPermissionRule(item protocol.PermissionRule) store.PermissionRule {
	rule := store.PermissionRule{
		ID:               strings.TrimSpace(item.ID),
		Scope:            store.PermissionScope(strings.TrimSpace(item.Scope)),
		Enabled:          item.Enabled,
		Engine:           strings.TrimSpace(strings.ToLower(item.Engine)),
		Kind:             store.PermissionKind(strings.TrimSpace(item.Kind)),
		CommandHead:      strings.TrimSpace(strings.ToLower(item.CommandHead)),
		TargetPathPrefix: strings.TrimSpace(item.TargetPathPrefix),
		Summary:          strings.TrimSpace(item.Summary),
		MatchCount:       item.MatchCount,
	}
	if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(item.CreatedAt)); err == nil {
		rule.CreatedAt = ts
	}
	if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(item.LastMatchedAt)); err == nil {
		rule.LastMatchedAt = ts
	}
	if rule.Scope == "" {
		rule.Scope = store.PermissionScopeSession
	}
	if rule.Kind == "" {
		rule.Kind = store.PermissionKindGeneric
	}
	return rule
}

func permissionContextFromDecision(req protocol.PermissionDecisionRequestEvent, projection store.ProjectionSnapshot, controller session.ControllerSnapshot) permissionMatchContext {
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
	return permissionMatchContext{
		Engine:      engine,
		Kind:        classifyPermissionKind(req.PromptMessage, targetPath, command),
		CommandHead: permissionCommandHead(command),
		TargetPath:  targetPath,
	}
}

func permissionContextFromPrompt(promptMessage string, meta protocol.RuntimeMeta, projection store.ProjectionSnapshot, controller session.ControllerSnapshot) permissionMatchContext {
	command := firstNonEmptyString(meta.Command, projection.Runtime.Command, controller.CurrentCommand, controller.ActiveMeta.Command)
	targetPath := firstNonEmptyString(meta.TargetPath, controller.ActiveMeta.TargetPath)
	engine := strings.TrimSpace(strings.ToLower(firstNonEmptyString(meta.Engine, controller.ActiveMeta.Engine, projection.Runtime.Engine)))
	if engine == "" {
		engine = "any"
	}
	return permissionMatchContext{
		Engine:      engine,
		Kind:        classifyPermissionKind(promptMessage, targetPath, command),
		CommandHead: permissionCommandHead(command),
		TargetPath:  targetPath,
	}
}

func classifyPermissionKind(promptMessage, targetPath, command string) store.PermissionKind {
	lowerPrompt := strings.ToLower(strings.TrimSpace(promptMessage))
	lowerCommand := strings.ToLower(strings.TrimSpace(command))
	switch {
	case strings.Contains(lowerPrompt, "network"),
		strings.Contains(lowerPrompt, "联网"),
		strings.Contains(lowerPrompt, "网络"):
		return store.PermissionKindNetwork
	case strings.Contains(lowerPrompt, "command"),
		strings.Contains(lowerPrompt, "命令"),
		(strings.TrimSpace(lowerCommand) != "" && targetPath == ""):
		return store.PermissionKindShell
	case strings.Contains(lowerPrompt, "修改文件"),
		strings.Contains(lowerPrompt, "write"),
		strings.Contains(lowerPrompt, "edit"),
		strings.Contains(lowerPrompt, "文件"):
		return store.PermissionKindWrite
	default:
		return store.PermissionKindGeneric
	}
}

func permissionCommandHead(command string) string {
	fields := strings.Fields(strings.TrimSpace(strings.ToLower(command)))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func buildPermissionRule(req protocol.PermissionDecisionRequestEvent, scope string, projection store.ProjectionSnapshot, controller session.ControllerSnapshot) store.PermissionRule {
	ctx := permissionContextFromDecision(req, projection, controller)
	now := time.Now().UTC()
	rule := store.PermissionRule{
		Scope:            store.PermissionScope(strings.TrimSpace(scope)),
		Enabled:          true,
		Engine:           ctx.Engine,
		Kind:             ctx.Kind,
		CommandHead:      ctx.CommandHead,
		TargetPathPrefix: strings.TrimSpace(req.TargetPath),
		CreatedAt:        now,
		Summary:          summarizePermissionRule(ctx),
	}
	if rule.Scope == "" {
		rule.Scope = store.PermissionScopeSession
	}
	rule.ID = permissionRuleID(rule)
	return rule
}

func summarizePermissionRule(ctx permissionMatchContext) string {
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

func permissionRuleID(rule store.PermissionRule) string {
	return fmt.Sprintf(
		"%s|%s|%s|%s|%s",
		strings.TrimSpace(string(rule.Scope)),
		strings.TrimSpace(rule.Engine),
		strings.TrimSpace(string(rule.Kind)),
		strings.TrimSpace(rule.CommandHead),
		strings.TrimSpace(rule.TargetPathPrefix),
	)
}

func upsertPermissionRule(items []store.PermissionRule, rule store.PermissionRule) []store.PermissionRule {
	if strings.TrimSpace(rule.ID) == "" {
		rule.ID = permissionRuleID(rule)
	}
	for index := range items {
		if items[index].ID == rule.ID {
			rule.CreatedAt = items[index].CreatedAt
			rule.MatchCount = items[index].MatchCount
			rule.LastMatchedAt = items[index].LastMatchedAt
			items[index] = rule
			return items
		}
	}
	return append(items, rule)
}

func deletePermissionRule(items []store.PermissionRule, id string) []store.PermissionRule {
	out := make([]store.PermissionRule, 0, len(items))
	for _, item := range items {
		if item.ID == id {
			continue
		}
		out = append(out, item)
	}
	return out
}

func togglePermissionRules(items []store.PermissionRule, enabled bool) []store.PermissionRule {
	out := make([]store.PermissionRule, 0, len(items))
	for _, item := range items {
		item.Enabled = enabled && item.Enabled
		out = append(out, item)
	}
	return out
}

func matchPermissionRule(items []store.PermissionRule, ctx permissionMatchContext) (store.PermissionRule, bool) {
	for _, item := range items {
		if !item.Enabled {
			continue
		}
		if item.Engine != "" && item.Engine != "any" && item.Engine != ctx.Engine {
			continue
		}
		if item.Kind != "" && item.Kind != store.PermissionKindGeneric && item.Kind != ctx.Kind {
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
	return store.PermissionRule{}, false
}

func markPermissionRuleMatched(items []store.PermissionRule, id string) []store.PermissionRule {
	now := time.Now().UTC()
	out := make([]store.PermissionRule, 0, len(items))
	for _, item := range items {
		if item.ID == id {
			item.MatchCount++
			item.LastMatchedAt = now
		}
		out = append(out, item)
	}
	return out
}

func buildPermissionDecisionFromEvent(
	sessionID string,
	message string,
	meta protocol.RuntimeMeta,
	projection store.ProjectionSnapshot,
	controller session.ControllerSnapshot,
) protocol.PermissionDecisionRequestEvent {
	return protocol.PermissionDecisionRequestEvent{
		ClientEvent:        protocol.ClientEvent{Action: "permission_decision"},
		Decision:           "approve",
		PermissionMode:     firstNonEmptyString(meta.PermissionMode, controller.ActiveMeta.PermissionMode, projection.Runtime.PermissionMode),
		PermissionRequestID: strings.TrimSpace(meta.PermissionRequestID),
		ResumeSessionID:    firstNonEmptyString(meta.ResumeSessionID, controller.ResumeSession, controller.ActiveMeta.ResumeSessionID, projection.Runtime.ResumeSessionID),
		TargetPath:         firstNonEmptyString(meta.TargetPath, controller.ActiveMeta.TargetPath),
		ContextID:          firstNonEmptyString(meta.ContextID, controller.ActiveMeta.ContextID),
		ContextTitle:       firstNonEmptyString(meta.ContextTitle, controller.ActiveMeta.ContextTitle),
		PromptMessage:      strings.TrimSpace(message),
		FallbackCommand:    firstNonEmptyString(meta.Command, projection.Runtime.Command, controller.CurrentCommand, controller.ActiveMeta.Command),
		FallbackCWD:        firstNonEmptyString(meta.CWD, controller.ActiveMeta.CWD, projection.Runtime.CWD),
		FallbackEngine:     firstNonEmptyString(meta.Engine, controller.ActiveMeta.Engine, projection.Runtime.Engine),
		FallbackTarget:     firstNonEmptyString(meta.Target, controller.ActiveMeta.Target),
		FallbackTargetType: firstNonEmptyString(meta.TargetType, controller.ActiveMeta.TargetType),
	}
}

func looksLikePermissionPromptForRule(event protocol.PromptRequestEvent) bool {
	if len(event.Options) >= 2 {
		first := strings.ToLower(strings.TrimSpace(event.Options[0]))
		second := strings.ToLower(strings.TrimSpace(event.Options[1]))
		if (first == "y" || first == "yes" || first == "approve") && (second == "n" || second == "no" || second == "deny") {
			return true
		}
	}
	kind := classifyPermissionKind(event.Message, strings.TrimSpace(event.RuntimeMeta.TargetPath), strings.TrimSpace(event.RuntimeMeta.Command))
	return kind != "" && kind != store.PermissionKindGeneric
}

func looksLikePermissionInteractionForRule(event protocol.InteractionRequestEvent) bool {
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
	derivedKind := classifyPermissionKind(message, strings.TrimSpace(targetPath), strings.TrimSpace(event.RuntimeMeta.Command))
	return derivedKind != "" && derivedKind != store.PermissionKindGeneric
}

func maybeConsumeTemporaryPermissionGrant(
	ctx context.Context,
	sessionStore store.Store,
	sessionID string,
	event any,
	service *runtimepkg.Service,
	grantStore *permissionGrantStore,
	emitAndPersist func(any),
) (bool, error) {
	if grantStore == nil {
		return false, nil
	}
	var (
		message string
		meta    protocol.RuntimeMeta
	)
	switch e := event.(type) {
	case protocol.PromptRequestEvent:
		if !looksLikePermissionPromptForRule(e) {
			return false, nil
		}
		message = e.Message
		meta = e.RuntimeMeta
	case protocol.InteractionRequestEvent:
		if !looksLikePermissionInteractionForRule(e) {
			return false, nil
		}
		message = e.Message
		meta = protocol.MergeRuntimeMeta(e.RuntimeMeta, protocol.RuntimeMeta{TargetPath: firstNonEmptyString(e.TargetPath, e.RuntimeMeta.TargetPath)})
	default:
		return false, nil
	}
	targetPath := normalizePermissionGrantPath(meta.TargetPath)
	if targetPath == "" || !grantStore.ConsumeIfValid(sessionID, targetPath, strings.TrimSpace(meta.PermissionRequestID)) {
		return false, nil
	}
	projection := normalizeProjectionSnapshot(store.ProjectionSnapshot{})
	if strings.TrimSpace(sessionID) != "" && sessionStore != nil {
		if record, err := sessionStore.GetSession(ctx, sessionID); err == nil {
			projection = normalizeProjectionSnapshot(record.Projection)
		}
	}
	controller := service.ControllerSnapshot()
	req := buildPermissionDecisionFromEvent(sessionID, message, meta, projection, controller)
	if req.TargetPath == "" {
		req.TargetPath = targetPath
	}
	if err := executePermissionDecision(ctx, sessionID, req, service, projection, controller, grantStore, emitAndPersist); err != nil {
		return false, err
	}
	return true, nil
}

func maybeAutoApplyPermissionEvent(
	ctx context.Context,
	sessionStore store.Store,
	sessionID string,
	event any,
	service *runtimepkg.Service,
	emit func(any),
	emitAndPersist func(any),
) (bool, error) {
	if sessionStore == nil {
		return false, nil
	}
	var (
		message string
		meta    protocol.RuntimeMeta
	)
	switch e := event.(type) {
	case protocol.PromptRequestEvent:
		if !looksLikePermissionPromptForRule(e) {
			return false, nil
		}
		message = e.Message
		meta = e.RuntimeMeta
	case protocol.InteractionRequestEvent:
		if !looksLikePermissionInteractionForRule(e) {
			return false, nil
		}
		message = e.Message
		meta = e.RuntimeMeta
	default:
		return false, nil
	}
	projection := normalizeProjectionSnapshot(store.ProjectionSnapshot{})
	if strings.TrimSpace(sessionID) != "" {
		if record, err := sessionStore.GetSession(ctx, sessionID); err == nil {
			projection = normalizeProjectionSnapshot(record.Projection)
		}
	}
	controller := service.ControllerSnapshot()
	matchCtx := permissionContextFromPrompt(message, meta, projection, controller)
	req := buildPermissionDecisionFromEvent(sessionID, message, meta, projection, controller)

	if projection.PermissionRulesEnabled {
		if rule, ok := matchPermissionRule(projection.PermissionRules, matchCtx); ok {
			if err := executePermissionDecision(ctx, sessionID, req, service, projection, controller, nil, emitAndPersist); err != nil {
				return false, err
			}
			if strings.TrimSpace(sessionID) != "" {
				if record, err := sessionStore.GetSession(ctx, sessionID); err == nil {
					record.Projection = normalizeProjectionSnapshot(record.Projection)
					record.Projection.PermissionRules = markPermissionRuleMatched(record.Projection.PermissionRules, rule.ID)
					_, _ = sessionStore.SaveProjection(ctx, sessionID, record.Projection)
				}
			}
			emit(protocol.NewPermissionAutoAppliedEvent(sessionID, rule.ID, string(rule.Scope), rule.Summary, "已按会话权限规则自动允许"))
			emitPermissionRuleList(emit, sessionStore, ctx, sessionID)
			return true, nil
		}
	}

	persistentSnapshot, err := sessionStore.GetPermissionRuleSnapshot(ctx)
	if err != nil {
		return false, err
	}
	if !persistentSnapshot.Enabled {
		return false, nil
	}
	rule, ok := matchPermissionRule(persistentSnapshot.Items, matchCtx)
	if !ok {
		return false, nil
	}
	if err := executePermissionDecision(ctx, sessionID, req, service, projection, controller, nil, emitAndPersist); err != nil {
		return false, err
	}
	persistentSnapshot.Items = markPermissionRuleMatched(persistentSnapshot.Items, rule.ID)
	_ = sessionStore.SavePermissionRuleSnapshot(ctx, persistentSnapshot)
	emit(protocol.NewPermissionAutoAppliedEvent(sessionID, rule.ID, string(rule.Scope), rule.Summary, "已按长期权限规则自动允许"))
	emitPermissionRuleList(emit, sessionStore, ctx, sessionID)
	return true, nil
}
