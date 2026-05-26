package builtin

import (
	"context"
	"encoding/json"

	"jeju/internal/sandbox"
	"jeju/internal/tools"
)

type FileRead struct {
	spec tools.Spec
	box  sandbox.Sandbox
}

func NewFileRead(spec tools.Spec, box sandbox.Sandbox) *FileRead {
	spec.Name = "file_read"
	return &FileRead{spec: spec, box: box}
}

func (t *FileRead) Name() string {
	return t.spec.Name
}

func (t *FileRead) Spec() tools.Spec {
	return t.spec
}

func (t *FileRead) Run(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return tools.Result{}, err
	}
	data, err := t.box.ReadFile(ctx, req.Path)
	if err != nil {
		return tools.Result{}, err
	}
	out, err := json.Marshal(map[string]any{"path": req.Path, "content": string(data)})
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Output: string(out)}, nil
}
