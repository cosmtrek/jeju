package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cosmtrek/jeju/internal/sandbox"
	"github.com/cosmtrek/jeju/internal/tools"
)

const defaultReadLimit = 200

type FileRead struct {
	spec tools.Spec
	box  sandbox.Sandbox
}

func NewFileRead(spec tools.Spec, box sandbox.Sandbox) *FileRead {
	if spec.Name == "" {
		spec.Name = "read"
	}
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
		Path   string `json:"path"`
		Offset *int   `json:"offset"`
		Limit  *int   `json:"limit"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return tools.Result{}, err
	}
	if req.Offset != nil && *req.Offset <= 0 {
		return tools.Result{}, fmt.Errorf("offset must be a positive 1-based line number")
	}
	if req.Limit != nil && *req.Limit <= 0 {
		return tools.Result{}, fmt.Errorf("limit must be a positive line count")
	}
	data, err := t.box.ReadFile(ctx, req.Path)
	if err != nil {
		return tools.Result{}, err
	}
	lines := splitLines(string(data))
	totalLines := len(lines)
	offset, limit := requestedPage(req.Offset, req.Limit)
	endLine := pageEndLine(offset, limit, totalLines)
	content := ""
	if offset <= totalLines {
		content = strings.Join(lines[offset-1:endLine], "")
	}
	nextOffset := 0
	truncated := endLine < totalLines
	if truncated {
		nextOffset = endLine + 1
	}
	out, err := json.Marshal(map[string]any{
		"path":       req.Path,
		"content":    content,
		"offset":     offset,
		"limit":      limit,
		"totalLines": totalLines,
		"nextOffset": nextOffset,
		"truncated":  truncated,
	})
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Output: string(out)}, nil
}

func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.SplitAfter(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func requestedPage(offset, limit *int) (int, int) {
	lineOffset := 1
	if offset != nil {
		lineOffset = *offset
	}
	lineLimit := defaultReadLimit
	if limit != nil {
		lineLimit = *limit
	}
	return lineOffset, lineLimit
}

func pageEndLine(offset, limit, totalLines int) int {
	if totalLines == 0 {
		return 0
	}
	if offset > totalLines {
		return totalLines
	}
	endLine := offset + limit - 1
	if endLine > totalLines {
		return totalLines
	}
	return endLine
}
