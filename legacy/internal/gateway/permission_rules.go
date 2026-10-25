package gateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mobilevc/internal/data"
	"mobilevc/internal/protocol"
	"mobilevc/internal/session"
)

func emitPermissionRuleList(emit func(any), sessionStore data.Store, ctx context.Context, sessionID string) {
	events, err := buildPermissionRuleListEvents(sessionStore, ctx, sessionID)
	if err != nil {
		emit(protocol.NewErrorEvent(sessionID, err.Error(), ""))
		return
	}
	for _, event := range events {
		emit(event)
	}
}

func buildPermissionRuleListEvents(sessionStore data.Store, ctx context.Context, sessionID string) ([]any, error) {
	if sessionStore == nil {
		return nil, fmt.Errorf("session store unavailable")
	}
	sessionEnabled := true
	sessionRules := []protocol.PermissionRule{}
	if strings.TrimSpace(sessionID) != "" {
		ruleStore, ok := sessionStore.(data.SessionPermissionRuleStore)
		if !ok {
			return nil, fmt.Errorf("session permission rule store unavailable")
		}
		snapshot, err := ruleStore.GetSessionPermissionRuleSnapshot(ctx, sessionID)
		if err == nil {
			sessionEnabled = snapshot.Enabled
			sessionRules = toProtocolPermissionRules(snapshot.Items)
		} else {
			return nil, err
		}
	}
	persistentSnapshot, err := sessionStore.GetPermissionRuleSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return []any{protocol.NewPermissionRuleListResultEvent(
		sessionID,
		sessionEnabled,
		persistentSnapshot.Enabled,
		sessionRules,
		toProtocolPermissionRules(persistentSnapshot.Items),
	)}, nil
}

func saveSessionPermissionRules(ctx context.Context, sessionStore data.Store, sessionID string, mutate func(data.SessionPermissionRuleSnapshot) data.SessionPermissionRuleSnapshot) error {
	if sessionStore == nil {
		return fmt.Errorf("session store unavailable")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session ID is required")
	}
	ruleStore, ok := sessionStore.(data.SessionPermissionRuleStore)
	if !ok {
		return fmt.Errorf("session permission rule store unavailable")
	}
	snapshot, err := ruleStore.GetSessionPermissionRuleSnapshot(ctx, sessionID)
	if err != nil {
		return err
	}
	snapshot.SessionID = sessionID
	if mutate != nil {
		snapshot = mutate(snapshot)
	}
	snapshot.SessionID = sessionID
	_, err = ruleStore.SaveSessionPermissionRuleSnapshot(ctx, snapshot)
	return err
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
	var ruleStore data.SessionPermissionRuleStore
	if strings.TrimSpace(sessionID) != "" {
		store, ok := sessionStore.(data.SessionPermissionRuleStore)
		if !ok {
			return false, fmt.Errorf("session permission rule store unavailable")
		}
		ruleStore = store
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
		snapshot, err := ruleStore.GetSessionPermissionRuleSnapshot(ctx, sessionID)
		if err != nil {
			return false, err
		}
		projection.PermissionRulesEnabled = snapshot.Enabled
		projection.PermissionRules = append([]data.PermissionRule(nil), snapshot.Items...)
		projection = session.NormalizeProjectionSnapshot(projection)
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
				projection.PermissionRules = session.MarkPermissionRuleMatched(projection.PermissionRules, rule.ID)
				snapshot := data.SessionPermissionRuleSnapshot{
					SessionID: sessionID,
					Enabled:   projection.PermissionRulesEnabled,
					Items:     projection.PermissionRules,
				}
				if _, err := ruleStore.SaveSessionPermissionRuleSnapshot(ctx, snapshot); err != nil {
					return false, err
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
	if err := sessionStore.SavePermissionRuleSnapshot(ctx, persistentSnapshot); err != nil {
		return false, err
	}
	emit(protocol.NewPermissionAutoAppliedEvent(sessionID, rule.ID, string(rule.Scope), rule.Summary, "已按长期权限规则自动允许"))
	emitPermissionRuleList(emit, sessionStore, ctx, sessionID)
	return true, nil
}
