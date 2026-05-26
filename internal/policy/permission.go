package policy

import "encoding/json"

type DecisionAction string

const (
	DecisionAllow  DecisionAction = "allow"
	DecisionAsk    DecisionAction = "ask"
	DecisionDeny   DecisionAction = "deny"
	DecisionDryRun DecisionAction = "dry_run"
)

type PermissionRequest struct {
	RunID string
	Step  int
	Tool  string
	Input json.RawMessage
	Risks []string
}

type PermissionDecision struct {
	Action DecisionAction
	Reason string
}
