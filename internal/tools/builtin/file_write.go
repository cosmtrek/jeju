package builtin

import (
	"context"
	"encoding/json"

	"github.com/cosmtrek/jeju/internal/sandbox"
	"github.com/cosmtrek/jeju/internal/tools"
)

type FileWrite struct {
	spec tools.Spec
	box  sandbox.Sandbox
}

func NewFileWrite(spec tools.Spec, box sandbox.Sandbox) *FileWrite {
	if spec.Name == "" {
		spec.Name = "write"
	}
	return &FileWrite{spec: spec, box: box}
}

func (t *FileWrite) Name() string {
	return t.spec.Name
}

func (t *FileWrite) Spec() tools.Spec {
	return t.spec
}

func (t *FileWrite) Run(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return tools.Result{}, err
	}
	if err := t.box.WriteFile(ctx, req.Path, []byte(req.Content)); err != nil {
		return tools.Result{}, err
	}
	out, err := json.Marshal(map[string]any{"path": req.Path, "bytes": len([]byte(req.Content))})
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{
		Output: string(out),
		Artifacts: []tools.Artifact{
			{Name: req.Path, Path: req.Path, Type: "file"},
		},
	}, nil
}
