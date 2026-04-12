package runtime

import (
	"mobilevc/internal/protocol"
	"mobilevc/internal/runner"
)

type ExecuteRequest struct {
	Command        string
	CWD            string
	Mode           runner.Mode
	PermissionMode string
	protocol.RuntimeMeta
}

type InputRequest struct {
	Data string
	protocol.RuntimeMeta
}

type ReviewDecisionRequest struct {
	Decision     string
	IsReviewOnly bool
	protocol.RuntimeMeta
}

type PlanDecisionRequest struct {
	Decision string
	protocol.RuntimeMeta
}

type Dependencies struct {
	NewExecRunner func() runner.Runner
	NewPtyRunner  func() runner.Runner
}
