package session

import (
	"fmt"
	"strings"

	"mobilevc/internal/data"
	"mobilevc/internal/engine"
	"mobilevc/internal/protocol"
)

type PendingPermissionPromptLookup interface {
	LatestPendingPermissionPrompt(requestID string) *protocol.PromptRequestEvent
	LatestPendingPrompt() *protocol.PromptRequestEvent
}

func NormalizeClaudePermissionMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "bypassPermissions":
		return "bypassPermissions"
	case "default":
		return "default"
	default:
		return "auto"
	}
}

func RefreshedPermissionPromptEvent(sessionID string, req protocol.PermissionDecisionRequestEvent, service *Service) *protocol.PromptRequestEvent {
	if service == nil {
		return nil
	}
	requestID := strings.TrimSpace(service.CurrentPermissionRequestID(sessionID))
	if requestID == "" {
		return nil
	}
	return RefreshedPermissionPromptEventWithID(sessionID, req, service, requestID)
}

func RefreshedPermissionPromptEventWithID(sessionID string, req protocol.PermissionDecisionRequestEvent, service *Service, requestID string) *protocol.PromptRequestEvent {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil
	}
	meta := protocol.RuntimeMeta{
		Source:              "permission-refresh",
		ResumeSessionID:     strings.TrimSpace(req.ResumeSessionID),
		Command:             strings.TrimSpace(req.FallbackCommand),
		Engine:              strings.TrimSpace(req.FallbackEngine),
		CWD:                 strings.TrimSpace(req.FallbackCWD),
		PermissionMode:      NormalizeClaudePermissionMode(req.PermissionMode),
		PermissionRequestID: requestID,
		BlockingKind:        "permission",
		ContextID:           strings.TrimSpace(req.ContextID),
		ContextTitle:        strings.TrimSpace(req.ContextTitle),
		TargetPath:          strings.TrimSpace(req.TargetPath),
		Target:              strings.TrimSpace(req.FallbackTarget),
		TargetType:          strings.TrimSpace(req.FallbackTargetType),
	}
	if service != nil {
		snapshot := service.RuntimeSnapshot()
		controller := service.ControllerSnapshot()
		currentMeta := protocol.MergeRuntimeMeta(snapshot.ActiveMeta, controller.ActiveMeta)
		meta = protocol.MergeRuntimeMeta(meta, currentMeta)
		meta.Source = "permission-refresh"
		meta.PermissionRequestID = requestID
		meta.BlockingKind = "permission"
		if mode := strings.TrimSpace(req.PermissionMode); mode != "" {
			meta.PermissionMode = NormalizeClaudePermissionMode(mode)
		}
	}
	message := strings.TrimSpace(req.PromptMessage)
	if message == "" {
		message = "当前操作需要你的授权"
	}
	event := protocol.ApplyRuntimeMeta(protocol.NewPromptRequestEvent(sessionID, message, []string{"y", "n"}), meta)
	prompt, ok := event.(protocol.PromptRequestEvent)
	if !ok {
		return nil
	}
	return &prompt
}

func ShouldBlockInputForPendingPermission(
	responder engine.PermissionResponseWriter,
	service *Service,
	projection data.ProjectionSnapshot,
	pending PendingPermissionPromptLookup,
) bool {
	if responder == nil || !responder.HasPendingPermissionRequest() {
		return false
	}
	requestID := strings.TrimSpace(responder.CurrentPermissionRequestID())
	controller := ControllerSnapshot{}
	snapshot := Snapshot{}
	if service != nil {
		controller = service.ControllerSnapshot()
		snapshot = service.RuntimeSnapshot()
	}
	for _, meta := range []protocol.RuntimeMeta{
		controller.ActiveMeta,
		snapshot.ActiveMeta,
		projection.Controller.ActiveMeta,
	} {
		if runtimeMetaBlocksInputForPermission(meta, requestID) {
			return true
		}
	}
	if requestID != "" && requestID != "__text_permission_prompt__" {
		return true
	}
	if pending == nil {
		return false
	}
	if requestID != "" {
		if prompt := pending.LatestPendingPermissionPrompt(requestID); prompt != nil {
			return true
		}
	}
	prompt := pending.LatestPendingPrompt()
	if prompt == nil {
		return false
	}
	if runtimeMetaBlocksInputForPermission(prompt.RuntimeMeta, requestID) {
		return true
	}
	if !snapshot.CanAcceptInteractiveInput {
		return false
	}
	command := firstNonEmptyString(
		prompt.RuntimeMeta.Command,
		snapshot.ActiveMeta.Command,
		controller.CurrentCommand,
		projection.Runtime.Command,
		projection.Controller.CurrentCommand,
	)
	return isAICommand(command) && PromptHasExplicitPermissionIntent(*prompt)
}

func runtimeMetaBlocksInputForPermission(meta protocol.RuntimeMeta, requestID string) bool {
	if strings.TrimSpace(strings.ToLower(meta.BlockingKind)) != "permission" {
		return false
	}
	if requestID == "" || requestID == "__text_permission_prompt__" {
		return true
	}
	return strings.TrimSpace(meta.PermissionRequestID) == "" ||
		strings.TrimSpace(meta.PermissionRequestID) == requestID
}

func PromptHasExplicitPermissionIntent(prompt protocol.PromptRequestEvent) bool {
	lower := strings.ToLower(strings.TrimSpace(prompt.Message))
	if strings.Contains(lower, "permission") ||
		strings.Contains(lower, "authorize") ||
		strings.Contains(lower, "authorization") ||
		strings.Contains(lower, "approval") ||
		strings.Contains(lower, "allow once") ||
		strings.Contains(lower, "allow this time") ||
		strings.Contains(lower, "always allow") ||
		strings.Contains(lower, "always deny") ||
		strings.Contains(lower, "授权") ||
		strings.Contains(lower, "权限") ||
		strings.Contains(lower, "允许") {
		return true
	}
	return false
}

func BuildReviewDecisionPrompt(decision string, req protocol.ReviewDecisionRequestEvent) (string, error) {
	decision = strings.TrimSpace(strings.ToLower(decision))
	if decision == "" {
		return "", fmt.Errorf("review decision is required")
	}
	subject := strings.TrimSpace(req.TargetPath)
	if subject == "" {
		subject = strings.TrimSpace(req.ContextTitle)
	}
	if subject == "" {
		subject = "当前 diff"
	}
	switch decision {
	case "accept":
		return fmt.Sprintf("请接受刚刚展示的 diff 变更，并继续保存当前修改。目标：%s\n", subject), nil
	case "revert":
		return fmt.Sprintf("请撤回刚刚展示的 diff 变更，不要保留这次修改。目标：%s\n", subject), nil
	case "revise":
		return fmt.Sprintf("请基于刚刚展示的 diff 继续调整并重新修改。目标：%s\n", subject), nil
	default:
		return "", fmt.Errorf("review decision must be one of: accept, revert, revise")
	}
}
