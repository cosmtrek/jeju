package command

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"jeju/internal/tools"
)

type Tool struct {
	name    string
	command string
	workdir string
	spec    tools.Spec
	env     map[string]string
}

func New(name, command, workdir string, spec tools.Spec, env map[string]string) *Tool {
	spec.Name = name
	return &Tool{name: name, command: command, workdir: workdir, spec: spec, env: env}
}

func (t *Tool) Name() string {
	return t.name
}

func (t *Tool) Spec() tools.Spec {
	return t.spec
}

func (t *Tool) Run(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	timeout := 60 * time.Second
	if t.spec.TimeoutSec > 0 {
		timeout = time.Duration(t.spec.TimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	envelope, err := json.Marshal(map[string]any{
		"tool":  t.name,
		"input": json.RawMessage(input),
		"context": map[string]any{
			"workspace": t.workdir,
		},
	})
	if err != nil {
		return tools.Result{}, err
	}

	cmd := exec.CommandContext(ctx, t.command)
	cmd.Dir = t.workdir
	cmd.Env = os.Environ()
	for key, value := range t.env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Stdin = bytes.NewReader(envelope)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return tools.Result{}, fmt.Errorf("command tool failed: %w: %s", err, stderr.String())
	}

	var parsed struct {
		OK        bool             `json:"ok"`
		Output    any              `json:"output"`
		Error     any              `json:"error"`
		Artifacts []tools.Artifact `json:"artifacts"`
		Metadata  map[string]any   `json:"metadata"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return tools.Result{}, err
	}
	if !parsed.OK {
		return tools.Result{}, fmt.Errorf("command tool returned error: %v", parsed.Error)
	}
	out, err := json.Marshal(parsed.Output)
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Output: string(out), Artifacts: parsed.Artifacts, Metadata: parsed.Metadata}, nil
}
