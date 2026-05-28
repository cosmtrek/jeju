package command

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
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

	args, err := expandArgs(t.spec.Args, input, schemaDefaults(t.spec.InputSchema))
	if err != nil {
		return tools.Result{}, err
	}

	cmd := exec.CommandContext(ctx, t.command, args...)
	cmd.Dir = t.workdir
	cmd.Env = os.Environ()
	for key, value := range t.env {
		cmd.Env = append(cmd.Env, key+"="+os.ExpandEnv(value))
	}
	cmd.Stdin = bytes.NewReader(nil)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return tools.Result{}, fmt.Errorf("command tool failed: %w: %s", err, stderr.String())
	}

	return parseCommandOutput(stdout.Bytes())
}

func expandArgs(templates []string, input json.RawMessage, defaults map[string]any) ([]string, error) {
	values := map[string]any{}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &values); err != nil {
			return nil, err
		}
	}
	for key, value := range defaults {
		if _, ok := values[key]; !ok {
			values[key] = value
		}
	}
	args := make([]string, 0, len(templates))
	for _, tmpl := range templates {
		arg := tmpl
		for {
			start := strings.Index(arg, "{{")
			if start < 0 {
				break
			}
			end := strings.Index(arg[start:], "}}")
			if end < 0 {
				return nil, fmt.Errorf("unterminated arg template %q", tmpl)
			}
			end += start
			key := strings.TrimSpace(arg[start+2 : end])
			key = strings.TrimPrefix(key, ".")
			value, ok := values[key]
			if !ok {
				return nil, fmt.Errorf("missing input key %q for arg template %q", key, tmpl)
			}
			arg = arg[:start] + fmt.Sprint(value) + arg[end+2:]
		}
		args = append(args, arg)
	}
	return args, nil
}

func schemaDefaults(schema any) map[string]any {
	defaults := map[string]any{}
	root, ok := schema.(map[string]any)
	if !ok {
		return defaults
	}
	props, ok := root["properties"].(map[string]any)
	if !ok {
		return defaults
	}
	for name, raw := range props {
		prop, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if value, ok := prop["default"]; ok {
			defaults[name] = value
		}
	}
	return defaults
}

func parseCommandOutput(data []byte) (tools.Result, error) {
	var envelope struct {
		OK        *bool            `json:"ok"`
		Output    any              `json:"output"`
		Error     any              `json:"error"`
		Artifacts []tools.Artifact `json:"artifacts"`
		Metadata  map[string]any   `json:"metadata"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.OK != nil {
		if !*envelope.OK {
			return tools.Result{}, fmt.Errorf("command tool returned error: %v", envelope.Error)
		}
		out, err := json.Marshal(envelope.Output)
		if err != nil {
			return tools.Result{}, err
		}
		return tools.Result{Output: string(out), Artifacts: envelope.Artifacts, Metadata: envelope.Metadata}, nil
	}
	return tools.Result{Output: strings.TrimSpace(string(data))}, nil
}
