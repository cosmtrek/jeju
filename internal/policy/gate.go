package policy

import (
	"fmt"

	"github.com/cosmtrek/jeju/internal/config"
	"github.com/cosmtrek/jeju/internal/tools"
)

type Gate struct {
	access   string
	approval string
}

func NewGate(cfg config.PermissionsConfig) *Gate {
	return &Gate{access: cfg.Access, approval: cfg.Approval}
}

func (g *Gate) Check(req PermissionRequest, spec tools.Spec) PermissionDecision {
	if denied := g.deniedByAccess(spec); denied != "" {
		return PermissionDecision{Action: DecisionDeny, Reason: denied}
	}
	if g.approval == "never" {
		return PermissionDecision{Action: DecisionAllow, Reason: "approval policy is never"}
	}
	if g.approval == "always" && hasSideEffect(spec.Capabilities) {
		return PermissionDecision{Action: DecisionAsk, Reason: "approval policy requires approval for side effects"}
	}
	if g.approval == "onRequest" && requiresApproval(spec.Capabilities) {
		return PermissionDecision{Action: DecisionAsk, Reason: fmt.Sprintf("tool %s requires approval", spec.Name)}
	}
	return PermissionDecision{Action: DecisionAllow, Reason: "allowed by permissions"}
}

func (g *Gate) deniedByAccess(spec tools.Spec) string {
	switch g.access {
	case "readOnly":
		if hasAnyCapability(spec.Capabilities, "workspaceWrite", "command", "networkRead", "networkWrite") {
			return fmt.Sprintf("permissions.access readOnly blocks tool %s", spec.Name)
		}
	case "workspace":
		return ""
	case "full":
		return ""
	}
	return ""
}

func requiresApproval(capabilities []string) bool {
	return hasAnyCapability(capabilities, "workspaceWrite", "command", "networkRead", "networkWrite", "agentRun")
}

func hasSideEffect(capabilities []string) bool {
	return hasAnyCapability(capabilities, "workspaceWrite", "command", "networkWrite", "agentRun")
}

func hasAnyCapability(capabilities []string, wants ...string) bool {
	for _, capability := range capabilities {
		for _, want := range wants {
			if capability == want {
				return true
			}
		}
	}
	return false
}
