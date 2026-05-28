package policy

import "encoding/json"

type DecisionAction string

const (
	DecisionAllow DecisionAction = "allow"
	DecisionAsk   DecisionAction = "ask"
	DecisionDeny  DecisionAction = "deny"
)

type PermissionRequest struct {
	RunID string
	Step  int
	Tool  string
	Input json.RawMessage
}

type PermissionDecision struct {
	Action DecisionAction
	Reason string
}
