package policy

import (
	"encoding/json"
	"testing"

	"github.com/cosmtrek/jeju/internal/config"
	"github.com/cosmtrek/jeju/internal/tools"
)

func TestGateDenyTakesPrecedence(t *testing.T) {
	gate := NewGate(config.PermissionsConfig{
		Access:   "readOnly",
		Approval: "onRequest",
	})
	decision := gate.Check(PermissionRequest{
		RunID: "run",
		Step:  1,
		Tool:  "danger",
		Input: json.RawMessage(`{}`),
	}, tools.Spec{Name: "danger", Capabilities: []string{"workspaceWrite"}})
	if decision.Action != DecisionDeny {
		t.Fatalf("expected deny, got %s", decision.Action)
	}
}

func TestGateAgentRunRequiresApprovalOnRequest(t *testing.T) {
	gate := NewGate(config.PermissionsConfig{
		Access:   "readOnly",
		Approval: "onRequest",
	})
	decision := gate.Check(PermissionRequest{
		RunID: "run",
		Step:  1,
		Tool:  "ask_child",
		Input: json.RawMessage(`{}`),
	}, tools.Spec{Name: "ask_child", Capabilities: []string{"agentRun"}})
	if decision.Action != DecisionAsk {
		t.Fatalf("expected ask, got %s", decision.Action)
	}
}

func TestGateAgentRunRequiresApprovalAlways(t *testing.T) {
	gate := NewGate(config.PermissionsConfig{
		Access:   "readOnly",
		Approval: "always",
	})
	decision := gate.Check(PermissionRequest{
		RunID: "run",
		Step:  1,
		Tool:  "ask_child",
		Input: json.RawMessage(`{}`),
	}, tools.Spec{Name: "ask_child", Capabilities: []string{"agentRun"}})
	if decision.Action != DecisionAsk {
		t.Fatalf("expected ask, got %s", decision.Action)
	}
}
