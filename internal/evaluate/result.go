package evaluate

type Result struct {
	RunID      string            `json:"run_id"`
	Passed     bool              `json:"passed"`
	Score      float64           `json:"score"`
	Evaluators []EvaluatorResult `json:"evaluators"`
}

type EvaluatorResult struct {
	Name    string       `json:"name"`
	Type    string       `json:"type"`
	Passed  bool         `json:"passed"`
	Score   float64      `json:"score"`
	Results []RuleResult `json:"results,omitempty"`
}

type RuleResult struct {
	Rule    string `json:"rule"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}
