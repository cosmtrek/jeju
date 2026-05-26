package cli

import (
	"context"
	"encoding/json"

	"jeju/internal/sandbox"
	"jeju/internal/tools"
)

type Shell struct {
	spec tools.Spec
	box  sandbox.Sandbox
	env  map[string]string
}

func NewShell(spec tools.Spec, box sandbox.Sandbox, env map[string]string) *Shell {
	spec.Name = "shell"
	return &Shell{spec: spec, box: box, env: env}
}

func (t *Shell) Name() string {
	return t.spec.Name
}

func (t *Shell) Spec() tools.Spec {
	return t.spec
}

func (t *Shell) Run(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var req struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return tools.Result{}, err
	}
	result, err := t.box.Exec(ctx, sandbox.ExecCommand{
		Command:    req.Command,
		TimeoutSec: t.spec.TimeoutSec,
		Env:        t.env,
	})
	if err != nil {
		return tools.Result{}, err
	}
	out, err := json.Marshal(map[string]any{
		"stdout":      result.Stdout,
		"stderr":      result.Stderr,
		"exit_code":   result.ExitCode,
		"duration_ms": result.Duration.Milliseconds(),
	})
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Output: string(out)}, nil
}
