package gateway

import (
	"context"
	"strings"
	"time"

	"mobilevc/internal/data"
	"mobilevc/internal/protocol"
	"mobilevc/internal/session"
)

func emitPermissionRuleList(emit func(any), sessionStore data.Store, ctx context.Context, sessionID string) {
	if sessionStore == nil {
		emit(protocol.NewErrorEvent(sessionID, "session store unavailable", ""))
		return
	}
	sessionEnabled := true
	sessionRules := []protocol.PermissionRule{}
	if strings.TrimSpace(sessionID) != "" {
		record, err := sessionStore.GetSession(ctx, sessionID)
		if err == nil {
			record.Projection = session.NormalizeProjectionSnapshot(record.Projection)
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

func toProtocolPermissionRules(items []data.PermissionRule) []protocol.PermissionRule {
	result := make([]protocol.PermissionRule, 0, len(items))
	for _, item := range items {
		result = append(result, toProtocolPermissionRule(item))
	}
	return result
}

func toProtocolPermissionRule(item data.PermissionRule) protocol.PermissionRule {
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

func fromProtocolPermissionRule(item protocol.PermissionRule) data.PermissionRule {
	rule := data.PermissionRule{
		ID:               strings.TrimSpace(item.ID),
		Scope:            data.PermissionScope(strings.TrimSpace(item.Scope)),
		Enabled:          item.Enabled,
		Engine:           strings.TrimSpace(strings.ToLower(item.Engine)),
		Kind:             data.PermissionKind(strings.TrimSpace(item.Kind)),
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
		rule.Scope = data.PermissionScopeSession
	}
	if rule.Kind == "" {
		rule.Kind = data.PermissionKindGeneric
	}
	return rule
}

func buildPermissionRule(req protocol.PermissionDecisionRequestEvent, scope string, projection data.ProjectionSnapshot, controller session.ControllerSnapshot) data.PermissionRule {
	return session.BuildPermissionRule(req, scope, projection, controller)
}

func upsertPermissionRule(items []data.PermissionRule, rule data.PermissionRule) []data.PermissionRule {
	if strings.TrimSpace(rule.ID) == "" {
		rule.ID = session.PermissionRuleID(rule)
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

func deletePermissionRule(items []data.PermissionRule, id string) []data.PermissionRule {
	out := make([]data.PermissionRule, 0, len(items))
	for _, item := range items {
		if item.ID == id {
			continue
		}
		out = append(out, item)
	}
	return out
}

func togglePermissionRules(items []data.PermissionRule, enabled bool) []data.PermissionRule {
	out := make([]data.PermissionRule, 0, len(items))
	for _, item := range items {
		item.Enabled = enabled && item.Enabled
		out = append(out, item)
	}
	return out
}

func maybeAutoApplyPermissionEvent(
	ctx context.Context,
	sessionStore data.Store,
	sessionID string,
	event any,
	service *session.Service,
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
		if !session.LooksLikePermissionPromptForRule(e) {
			return false, nil
		}
		message = e.Message
		meta = e.RuntimeMeta
	case protocol.InteractionRequestEvent:
		if !session.LooksLikePermissionInteractionForRule(e) {
			return false, nil
		}
		message = e.Message
		meta = e.RuntimeMeta
	default:
		return false, nil
	}
	projection := session.NormalizeProjectionSnapshot(data.ProjectionSnapshot{})
	if strings.TrimSpace(sessionID) != "" {
		if record, err := sessionStore.GetSession(ctx, sessionID); err == nil {
			projection = session.NormalizeProjectionSnapshot(record.Projection)
		}
	}
	controller := service.ControllerSnapshot()
	matchCtx := session.PermissionContextFromPrompt(message, meta, projection, controller)
	req := session.BuildPermissionDecisionFromEvent(sessionID, message, meta, projection, controller)

	if projection.PermissionRulesEnabled {
		if rule, ok := session.MatchPermissionRule(projection.PermissionRules, matchCtx); ok {
			if err := executePermissionDecision(ctx, sessionID, req, service, projection, controller, emitAndPersist); err != nil {
				return false, err
			}
			if strings.TrimSpace(sessionID) != "" {
				if record, err := sessionStore.GetSession(ctx, sessionID); err == nil {
					record.Projection = session.NormalizeProjectionSnapshot(record.Projection)
					record.Projection.PermissionRules = session.MarkPermissionRuleMatched(record.Projection.PermissionRules, rule.ID)
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
	rule, ok := session.MatchPermissionRule(persistentSnapshot.Items, matchCtx)
	if !ok {
		return false, nil
	}
	if err := executePermissionDecision(ctx, sessionID, req, service, projection, controller, emitAndPersist); err != nil {
		return false, err
	}
	persistentSnapshot.Items = session.MarkPermissionRuleMatched(persistentSnapshot.Items, rule.ID)
	_ = sessionStore.SavePermissionRuleSnapshot(ctx, persistentSnapshot)
	emit(protocol.NewPermissionAutoAppliedEvent(sessionID, rule.ID, string(rule.Scope), rule.Summary, "已按长期权限规则自动允许"))
	emitPermissionRuleList(emit, sessionStore, ctx, sessionID)
	return true, nil
}
