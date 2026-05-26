package policy

import (
	"encoding/json"
	"testing"

	"jeju/internal/config"
	"jeju/internal/tools"
)

func TestGateDenyTakesPrecedence(t *testing.T) {
	gate := NewGate(config.PolicyConfig{
		DefaultPermission: "ask",
		Rules: []config.PolicyRule{
			{Match: config.PolicyMatch{Risk: "write"}, Permission: "ask"},
			{Match: config.PolicyMatch{Risk: "destructive"}, Permission: "deny"},
			{Match: config.PolicyMatch{Tool: "danger"}, Permission: "allow"},
		},
	})
	decision := gate.Check(PermissionRequest{
		RunID: "run",
		Step:  1,
		Tool:  "danger",
		Input: json.RawMessage(`{}`),
		Risks: []string{"write", "destructive"},
	}, tools.Spec{Name: "danger", Permission: "allow", Risks: []string{"write", "destructive"}})
	if decision.Action != DecisionDeny {
		t.Fatalf("expected deny, got %s", decision.Action)
	}
}
