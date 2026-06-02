package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cosmtrek/jeju/internal/sandbox"
	"github.com/cosmtrek/jeju/internal/tools"
)

type Edit struct {
	spec tools.Spec
	box  sandbox.Sandbox
}

func NewEdit(spec tools.Spec, box sandbox.Sandbox) *Edit {
	if spec.Name == "" {
		spec.Name = "edit"
	}
	return &Edit{spec: spec, box: box}
}

func (t *Edit) Name() string {
	return t.spec.Name
}

func (t *Edit) Spec() tools.Spec {
	return t.spec
}

func (t *Edit) Run(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var req struct {
		Path    string `json:"path"`
		OldText string `json:"oldText"`
		NewText string `json:"newText"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return tools.Result{}, err
	}
	if req.OldText == "" {
		return tools.Result{}, fmt.Errorf("oldText is required")
	}
	data, err := t.box.ReadFile(ctx, req.Path)
	if err != nil {
		return tools.Result{}, err
	}
	content := string(data)
	count := strings.Count(content, req.OldText)
	if count == 0 {
		return tools.Result{}, fmt.Errorf("oldText not found in %s", req.Path)
	}
	if count > 1 {
		return tools.Result{}, fmt.Errorf("oldText is ambiguous in %s: %d matches", req.Path, count)
	}
	updated := strings.Replace(content, req.OldText, req.NewText, 1)
	if err := t.box.WriteFile(ctx, req.Path, []byte(updated)); err != nil {
		return tools.Result{}, err
	}
	out, err := json.Marshal(map[string]any{"path": req.Path, "replacements": 1})
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{
		Output:    string(out),
		Artifacts: []tools.Artifact{{Name: req.Path, Path: req.Path, Type: "file"}},
	}, nil
}
