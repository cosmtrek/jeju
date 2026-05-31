package evaluate

import (
	"context"
	"encoding/json"
	"fmt"

	"jeju/internal/model"
)

type LLMEvaluator struct {
	name      string
	client    model.Client
	provider  model.ProviderConfig
	prompt    string
	threshold float64
}

func NewLLMEvaluator(name string, client model.Client, provider model.ProviderConfig, prompt string, threshold *float64) *LLMEvaluator {
	if name == "" {
		name = "llm"
	}
	value := 0.7
	if threshold != nil {
		value = *threshold
	}
	return &LLMEvaluator{name: name, client: client, provider: provider, prompt: prompt, threshold: value}
}

func (e *LLMEvaluator) Name() string {
	return e.name
}

func (e *LLMEvaluator) Type() string {
	return "llm"
}

func (e *LLMEvaluator) Evaluate(ctx context.Context, input Context) (EvaluatorResult, error) {
	userContent := fmt.Sprintf("Task:\n%s\n\nFinal answer:\n%s", input.Input, input.Final)
	if input.Expected != nil {
		data, _ := json.MarshalIndent(input.Expected, "", "  ")
		userContent += fmt.Sprintf("\n\nExpected:\n%s", string(data))
	}
	if input.Eval != nil {
		data, _ := json.MarshalIndent(input.Eval, "", "  ")
		userContent += fmt.Sprintf("\n\nTask evaluation instructions:\n%s", string(data))
	}
	if input.Metadata != nil {
		data, _ := json.MarshalIndent(input.Metadata, "", "  ")
		userContent += fmt.Sprintf("\n\nTask metadata:\n%s", string(data))
	}
	req := model.Request{
		Model: e.provider.Model,
		Messages: []model.Message{
			{Role: "system", Content: e.prompt},
			{Role: "user", Content: userContent},
		},
		Temperature: e.provider.Temperature,
		MaxTokens:   e.provider.MaxOutputTokens,
	}
	resp, err := e.client.Generate(ctx, req)
	if err != nil {
		return EvaluatorResult{}, err
	}
	var parsed struct {
		Score  float64 `json:"score"`
		Passed *bool   `json:"passed"`
		Reason string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(resp.Text), &parsed); err != nil {
		return EvaluatorResult{}, fmt.Errorf("llm evaluator %q returned invalid JSON: %w", e.name, err)
	}
	passed := parsed.Score >= e.threshold
	if parsed.Passed != nil {
		passed = *parsed.Passed
	}
	return EvaluatorResult{
		Name:   e.name,
		Type:   "llm",
		Passed: passed,
		Score:  parsed.Score,
		Results: []RuleResult{{
			Rule:    "llmJudge",
			Passed:  passed,
			Message: parsed.Reason,
		}},
	}, nil
}
