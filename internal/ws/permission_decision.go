package ws

import (
	"context"
	"errors"
	"strings"

	"mobilevc/internal/protocol"
	"mobilevc/internal/runner"
	runtimepkg "mobilevc/internal/runtime"
	"mobilevc/internal/session"
	"mobilevc/internal/store"
)

func executePermissionDecision(
	ctx context.Context,
	sessionID string,
	permissionEvent protocol.PermissionDecisionRequestEvent,
	service *runtimepkg.Service,
	projection store.ProjectionSnapshot,
	controller session.ControllerSnapshot,
	grantStore *permissionGrantStore,
	emitAndPersist func(any),
) error {
	decision := strings.TrimSpace(strings.ToLower(permissionEvent.Decision))
	effectivePermissionMode := strings.TrimSpace(permissionEvent.PermissionMode)
	if effectivePermissionMode == "" {
		effectivePermissionMode = strings.TrimSpace(controller.ActiveMeta.PermissionMode)
	}
	if effectivePermissionMode == "" {
		effectivePermissionMode = strings.TrimSpace(projection.Runtime.PermissionMode)
	}
	if effectivePermissionMode != "" {
		service.UpdatePermissionMode(effectivePermissionMode)
	}
	inputMeta := protocol.RuntimeMeta{
		Source:              "permission-decision",
		ResumeSessionID:     firstNonEmptyString(permissionEvent.ResumeSessionID, controller.ResumeSession, controller.ActiveMeta.ResumeSessionID, projection.Runtime.ResumeSessionID),
		ContextID:           firstNonEmptyString(permissionEvent.ContextID, controller.ActiveMeta.ContextID),
		ContextTitle:        firstNonEmptyString(permissionEvent.ContextTitle, controller.ActiveMeta.ContextTitle),
		TargetPath:          firstNonEmptyString(permissionEvent.TargetPath, controller.ActiveMeta.TargetPath),
		TargetText:          decision,
		Command:             firstNonEmptyString(permissionEvent.FallbackCommand, projection.Runtime.Command, controller.CurrentCommand, controller.ActiveMeta.Command),
		Engine:              firstNonEmptyString(permissionEvent.FallbackEngine, controller.ActiveMeta.Engine),
		CWD:                 firstNonEmptyString(permissionEvent.FallbackCWD, controller.ActiveMeta.CWD, projection.Runtime.CWD),
		Target:              firstNonEmptyString(permissionEvent.FallbackTarget, controller.ActiveMeta.Target),
		TargetType:          firstNonEmptyString(permissionEvent.FallbackTargetType, controller.ActiveMeta.TargetType),
		PermissionMode:      effectivePermissionMode,
		PermissionRequestID: strings.TrimSpace(permissionEvent.PermissionRequestID),
	}
	if !isClaudeCommandLike(inputMeta.Command) {
		if err := service.SendPermissionDecision(ctx, sessionID, decision, inputMeta, emitAndPersist); err == nil {
			return nil
		} else if errors.Is(err, runner.ErrNoPendingControlRequest) {
			return runtimepkg.ErrPermissionRequestExpired
		} else {
			return err
		}
	}
	if decision == "deny" {
		if err := service.SendPermissionDecision(ctx, sessionID, decision, inputMeta, emitAndPersist); err == nil {
			return nil
		} else if !errors.Is(err, runner.ErrNoPendingControlRequest) && !errors.Is(err, runner.ErrInputNotSupported) {
			return err
		}
		prompt, err := buildPermissionDecisionPrompt(decision, permissionEvent)
		if err != nil {
			return err
		}
		return service.SendInput(ctx, sessionID, runtimepkg.InputRequest{
			Data:        prompt,
			RuntimeMeta: inputMeta,
		}, emitAndPersist)
	}
	replayInput := hotSwapApproveContinuation(permissionEvent)
	targetPath := normalizePermissionGrantPath(inputMeta.TargetPath)
	permissionRequestID := strings.TrimSpace(inputMeta.PermissionRequestID)
	grantIssued := false
	if grantStore != nil && targetPath != "" {
		grantIssued = grantStore.Issue(sessionID, targetPath, permissionRequestID, temporaryPermissionGrantTTL)
	}
	revokeGrant := func() {
		if grantIssued {
			grantStore.Revoke(sessionID, targetPath)
		}
	}
	req := runtimepkg.ExecuteRequest{
		Command:        inputMeta.Command,
		CWD:            inputMeta.CWD,
		Mode:           runner.ModePTY,
		PermissionMode: inputMeta.PermissionMode,
		RuntimeMeta:    inputMeta,
	}
	currentRunner := service.CurrentRunner()
	if currentRunner != nil {
		if err := service.HotSwapApproveWithTemporaryElevation(ctx, sessionID, req, replayInput, emitAndPersist); err == nil || !errors.Is(err, runtimepkg.ErrNoActiveRunner) {
			if err != nil {
				revokeGrant()
			}
			return err
		}
	}
	if !service.CanHotSwapClaudeSession(req) || !service.HasResumeSession(req) {
		revokeGrant()
		return runtimepkg.ErrNoActiveRunner
	}
	if err := service.HotSwapApproveFromResume(ctx, sessionID, req, replayInput, emitAndPersist); err != nil {
		revokeGrant()
		return err
	}
	return nil
}
