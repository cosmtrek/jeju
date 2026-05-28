package evaluate

import "context"

type RuleEvaluator struct {
	name  string
	rules []string
}

func NewRuleEvaluator(name string, rules []string) *RuleEvaluator {
	if name == "" {
		name = "rule"
	}
	return &RuleEvaluator{name: name, rules: rules}
}

func (e *RuleEvaluator) Name() string {
	return e.name
}

func (e *RuleEvaluator) Type() string {
	return "rule"
}

func (e *RuleEvaluator) Evaluate(ctx context.Context, input Context) (EvaluatorResult, error) {
	select {
	case <-ctx.Done():
		return EvaluatorResult{}, ctx.Err()
	default:
	}
	results := make([]RuleResult, 0, len(e.rules))
	passedCount := 0
	for _, rule := range e.rules {
		result := evalRule(rule, input)
		if result.Passed {
			passedCount++
		}
		results = append(results, result)
	}
	score := 1.0
	passed := true
	if len(results) > 0 {
		score = float64(passedCount) / float64(len(results))
		passed = passedCount == len(results)
	}
	return EvaluatorResult{
		Name:    e.name,
		Type:    "rule",
		Passed:  passed,
		Score:   score,
		Results: results,
	}, nil
}

func evalRule(rule string, input Context) RuleResult {
	result := RuleResult{Rule: rule, Passed: true}
	switch rule {
	case "finalAnswerExists":
		result.Passed = input.Final != ""
	case "noModelError":
		result.Passed = input.ModelErrors == 0
	case "maxStepsNotExceeded":
		result.Passed = input.MaxSteps == 0 || input.Steps <= input.MaxSteps
	case "maxToolCallsNotExceeded":
		result.Passed = input.MaxToolCalls == 0 || input.ToolCalls <= input.MaxToolCalls
	case "noPermissionDenied":
		result.Passed = input.PermissionDenied == 0
	case "runCompleted":
		result.Passed = input.Status == "completed"
	default:
		result.Passed = false
		result.Message = "unknown rule"
	}
	return result
}
