package evaluate

import "context"

type Evaluator interface {
	Name() string
	Type() string
	Evaluate(ctx context.Context, input Context) (EvaluatorResult, error)
}

type Context struct {
	RunID            string
	Input            string
	Status           string
	Final            string
	Steps            int
	ToolCalls        int
	ModelErrors      int
	ToolErrors       int
	PermissionDenied int
	MaxSteps         int
	MaxToolCalls     int
}
