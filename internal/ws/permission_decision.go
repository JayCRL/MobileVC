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
	if strings.TrimSpace(inputMeta.PermissionMode) != "auto" {
		inputMeta.PermissionMode = "auto"
		service.UpdatePermissionMode("auto")
	}
	if err := service.SendPermissionDecision(ctx, sessionID, decision, inputMeta, emitAndPersist); err == nil {
		return nil
	} else if errors.Is(err, runner.ErrNoPendingControlRequest) {
		return runtimepkg.ErrPermissionRequestExpired
	} else {
		return err
	}
}
