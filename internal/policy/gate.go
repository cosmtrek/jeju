package policy

import (
	"fmt"

	"jeju/internal/config"
	"jeju/internal/tools"
)

type Gate struct {
	defaultPermission DecisionAction
	rules             []Rule
}

func NewGate(cfg config.PolicyConfig) *Gate {
	defaultPermission := DecisionAsk
	if cfg.DefaultPermission != "" {
		defaultPermission = DecisionAction(cfg.DefaultPermission)
	}
	rules := make([]Rule, 0, len(cfg.Rules))
	for _, rule := range cfg.Rules {
		rules = append(rules, Rule{
			Risk:       rule.Match.Risk,
			Tool:       rule.Match.Tool,
			Permission: DecisionAction(rule.Permission),
		})
	}
	return &Gate{defaultPermission: defaultPermission, rules: rules}
}

func (g *Gate) Check(req PermissionRequest, spec tools.Spec) PermissionDecision {
	action := g.defaultPermission
	reason := fmt.Sprintf("default permission is %s", action)
	if spec.Permission != "" {
		action = DecisionAction(spec.Permission)
		reason = fmt.Sprintf("tool %s declares permission %s", spec.Name, action)
	}
	for _, rule := range g.rules {
		if rule.Tool != "" && rule.Tool == req.Tool {
			action = rule.Permission
			reason = fmt.Sprintf("policy rule matched tool %s", rule.Tool)
			if action == DecisionDeny {
				return PermissionDecision{Action: action, Reason: reason}
			}
		}
		if rule.Risk != "" && contains(req.Risks, rule.Risk) {
			action = rule.Permission
			reason = fmt.Sprintf("policy rule matched risk %s", rule.Risk)
			if action == DecisionDeny {
				return PermissionDecision{Action: action, Reason: reason}
			}
		}
	}
	return PermissionDecision{Action: action, Reason: reason}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
