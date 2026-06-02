package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cosmtrek/jeju/internal/sandbox"
	"github.com/cosmtrek/jeju/internal/tools"
)

const defaultSearchMaxFileBytes = 1 << 20

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
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Glob    string `json:"glob"`
		Mode    string `json:"mode"`
		Limit   int    `json:"limit"`
		Context int    `json:"context"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return tools.Result{}, err
	}
	if strings.TrimSpace(req.Pattern) == "" {
		return tools.Result{}, fmt.Errorf("pattern is required")
	}
	mode := req.Mode
	if mode == "" {
		mode = "literal"
	}
	if mode != "literal" && mode != "regex" {
		return tools.Result{}, fmt.Errorf("mode must be literal or regex")
	}
	limit := req.Limit
	if limit < 0 {
		return tools.Result{}, fmt.Errorf("limit must be >= 0")
	}
	if req.Context < 0 {
		return tools.Result{}, fmt.Errorf("context must be >= 0")
	}
	if limit == 0 {
		limit = 50
	}
	contextBefore := req.Context
	contextAfter := req.Context
	contextBefore = min(contextBefore, 10)
	contextAfter = min(contextAfter, 10)
	root, err := searchRoot(t.box.Workdir(), req.Path)
	if err != nil {
		return tools.Result{}, err
	}
	matcher, err := newLineMatcher(req.Pattern, mode == "regex")
	if err != nil {
		return tools.Result{}, err
	}
	matches := []map[string]any{}
	skippedFiles := []map[string]any{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(t.box.Workdir(), path)
		if relErr != nil {
			return relErr
		}
		if entry.IsDir() {
			if shouldSkipDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if req.Glob != "" && !globMatchAny([]string{req.Glob}, rel, entry.Name()) {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if len(skippedFiles) < 20 {
				skippedFiles = append(skippedFiles, map[string]any{
					"path":   rel,
					"reason": "symlink skipped",
				})
			}
			return nil
		}
		info, err := entry.Info()
		if err == nil && info.Size() > int64(defaultSearchMaxFileBytes) {
			if len(skippedFiles) < 20 {
				skippedFiles = append(skippedFiles, map[string]any{
					"path":         rel,
					"reason":       "file exceeds internal size limit",
					"sizeBytes":    info.Size(),
					"maxFileBytes": defaultSearchMaxFileBytes,
				})
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		data, err := t.box.ReadFile(ctx, rel)
		if err != nil {
			return nil
		}
		lines := splitSearchLines(string(data))
		for i, line := range lines {
			if matcher(line) {
				match := map[string]any{"path": rel, "line": i + 1, "text": line}
				if contextBefore > 0 || contextAfter > 0 {
					match["context"] = contextLines(lines, max(0, i-contextBefore), min(len(lines), i+1+contextAfter))
				}
				matches = append(matches, match)
				if len(matches) >= limit {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil {
		return tools.Result{}, err
	}
	out, err := json.Marshal(map[string]any{
		"matches":      matches,
		"limit":        limit,
		"truncated":    len(matches) >= limit,
		"skippedFiles": skippedFiles,
	})
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Output: string(out)}, nil
}

func searchRoot(workdir, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return workdir, nil
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("absolute search paths are not allowed: %s", path)
	}
	root := filepath.Join(workdir, filepath.Clean(path))
	relRoot, err := filepath.Rel(workdir, root)
	if err != nil || relRoot == ".." || strings.HasPrefix(relRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("search path escapes workspace: %s", path)
	}
	return root, nil
}

func newLineMatcher(query string, regex bool) (func(string) bool, error) {
	if regex {
		expr, err := regexp.Compile(query)
		if err != nil {
			return nil, err
		}
		return expr.MatchString, nil
	}
	return func(line string) bool {
		return strings.Contains(line, query)
	}, nil
}

func shouldSkipDir(rel string) bool {
	if rel == "." {
		return false
	}
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		switch part {
		case ".git", "node_modules", "vendor":
			return true
		case ".jeju-dev", "dist", "build":
			if i == 0 {
				return true
			}
		}
	}
	for i, part := range parts {
		if part == "runs" && (i == 0 || (len(parts) == 3 && parts[0] == "usecases" && i == 2)) {
			return true
		}
	}
	return false
}

func globMatchAny(patterns []string, rel, base string) bool {
	rel = filepath.ToSlash(rel)
	for _, pattern := range patterns {
		pattern = filepath.ToSlash(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		target := base
		if strings.Contains(pattern, "/") {
			target = rel
		}
		if ok, _ := filepath.Match(pattern, target); ok {
			return true
		}
	}
	return false
}

func contextLines(lines []string, start, end int) []map[string]any {
	out := make([]map[string]any, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, map[string]any{"line": i + 1, "text": lines[i]})
	}
	return out
}

func splitSearchLines(content string) []string {
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
