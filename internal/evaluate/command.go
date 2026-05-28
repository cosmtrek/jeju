package evaluate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

type CommandEvaluator struct {
	name       string
	command    string
	args       []string
	timeoutSec int
}

func NewCommandEvaluator(name, command string, args []string, timeoutSec int) *CommandEvaluator {
	if name == "" {
		name = "command"
	}
	return &CommandEvaluator{name: name, command: command, args: args, timeoutSec: timeoutSec}
}

func (e *CommandEvaluator) Name() string {
	return e.name
}

func (e *CommandEvaluator) Type() string {
	return "command"
}

func (e *CommandEvaluator) Evaluate(ctx context.Context, input Context) (EvaluatorResult, error) {
	timeout := 60 * time.Second
	if e.timeoutSec > 0 {
		timeout = time.Duration(e.timeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	data, err := json.Marshal(input)
	if err != nil {
		return EvaluatorResult{}, err
	}
	cmd := exec.CommandContext(ctx, e.command, e.args...)
	if filepath.IsAbs(e.command) {
		cmd.Dir = filepath.Dir(e.command)
	}
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return EvaluatorResult{}, fmt.Errorf("command evaluator failed: %w: %s", err, stderr.String())
	}
	var parsed struct {
		Score  float64 `json:"score"`
		Passed bool    `json:"passed"`
		Reason string  `json:"reason"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return EvaluatorResult{}, err
	}
	return EvaluatorResult{
		Name:   e.name,
		Type:   "command",
		Passed: parsed.Passed,
		Score:  parsed.Score,
		Results: []RuleResult{{
			Rule:    "commandJudge",
			Passed:  parsed.Passed,
			Message: parsed.Reason,
		}},
	}, nil
}
