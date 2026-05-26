package evaluate

import "context"

func Run(ctx context.Context, runID string, evaluators []Evaluator, input Context) (Result, error) {
	result := Result{RunID: runID, Passed: true}
	if len(evaluators) == 0 {
		result.Score = 1
		return result, nil
	}
	total := 0.0
	for _, evaluator := range evaluators {
		evalResult, err := evaluator.Evaluate(ctx, input)
		if err != nil {
			return Result{}, err
		}
		result.Evaluators = append(result.Evaluators, evalResult)
		total += evalResult.Score
		if !evalResult.Passed {
			result.Passed = false
		}
	}
	result.Score = total / float64(len(evaluators))
	return result, nil
}
