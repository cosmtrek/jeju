package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"jeju/internal/sandbox"
	"jeju/internal/tools"
)

type Search struct {
	spec tools.Spec
	box  sandbox.Sandbox
}

func NewSearch(spec tools.Spec, box sandbox.Sandbox) *Search {
	if spec.Name == "" {
		spec.Name = "search"
	}
	return &Search{spec: spec, box: box}
}

func (t *Search) Name() string {
	return t.spec.Name
}

func (t *Search) Spec() tools.Spec {
	return t.spec
}

func (t *Search) Run(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var req struct {
		Query string `json:"query"`
		Path  string `json:"path"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return tools.Result{}, err
	}
	if strings.TrimSpace(req.Query) == "" {
		return tools.Result{}, fmt.Errorf("query is required")
	}
	root := t.box.Workdir()
	if req.Path != "" {
		root = filepath.Join(root, filepath.Clean(req.Path))
	}
	relRoot, err := filepath.Rel(t.box.Workdir(), root)
	if err != nil || relRoot == ".." || strings.HasPrefix(relRoot, ".."+string(filepath.Separator)) {
		return tools.Result{}, fmt.Errorf("search path escapes workspace: %s", req.Path)
	}
	matches := []map[string]any{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if strings.Contains(line, req.Query) {
				rel, _ := filepath.Rel(t.box.Workdir(), path)
				matches = append(matches, map[string]any{"path": rel, "line": i + 1, "text": line})
				if len(matches) >= 50 {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil {
		return tools.Result{}, err
	}
	out, err := json.Marshal(map[string]any{"matches": matches})
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Output: string(out)}, nil
}
