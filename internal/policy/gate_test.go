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
