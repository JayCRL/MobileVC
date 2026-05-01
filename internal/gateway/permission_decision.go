package gateway

import (
	"context"
	"errors"

	"mobilevc/internal/data"
	"mobilevc/internal/engine"
	"mobilevc/internal/protocol"
	"mobilevc/internal/session"
)

func executePermissionDecision(
	ctx context.Context,
	sessionID string,
	permissionEvent protocol.PermissionDecisionRequestEvent,
	service *session.Service,
	projection data.ProjectionSnapshot,
	controller session.ControllerSnapshot,
	emitAndPersist func(any),
) error {
	plan, err := session.BuildPermissionDecisionPlan(permissionEvent, projection, controller)
	if err != nil {
		return err
	}
	if plan.Meta.PermissionMode != "" {
		service.UpdatePermissionMode(plan.Meta.PermissionMode)
	}
	switch plan.Action {
	case session.PermissionDecisionActionDirect:
		if err := service.SendPermissionDecision(ctx, sessionID, plan.Decision, plan.Meta, emitAndPersist); err == nil {
			return nil
		} else if errors.Is(err, engine.ErrNoPendingControlRequest) {
			return session.ErrPermissionRequestExpired
		} else {
			return err
		}
	case session.PermissionDecisionActionDenyThenInput:
		if plan.Meta.PermissionRequestID != "" {
			if err := service.SendPermissionDecision(ctx, sessionID, plan.Decision, plan.Meta, emitAndPersist); err == nil {
				return nil
			} else if !errors.Is(err, engine.ErrNoPendingControlRequest) && !errors.Is(err, engine.ErrInputNotSupported) {
				return err
			}
		}
		return service.SendInput(ctx, sessionID, session.InputRequest{
			Data:        plan.Prompt,
			RuntimeMeta: plan.Meta,
		}, emitAndPersist)
	case session.PermissionDecisionActionAutoThenDirect:
		service.UpdatePermissionMode("auto")
		if err := service.SendPermissionDecision(ctx, sessionID, plan.Decision, plan.Meta, emitAndPersist); err == nil {
			return nil
		} else if errors.Is(err, engine.ErrNoPendingControlRequest) {
			return session.ErrPermissionRequestExpired
		} else {
			return err
		}
	default:
		if err := service.SendPermissionDecision(ctx, sessionID, plan.Decision, plan.Meta, emitAndPersist); err == nil {
			return nil
		} else if errors.Is(err, engine.ErrNoPendingControlRequest) {
			return session.ErrPermissionRequestExpired
		} else {
			return err
		}
	}
}
