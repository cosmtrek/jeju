package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cosmtrek/jeju/internal/sandbox"
	"github.com/cosmtrek/jeju/internal/tools"
)

func TestSearchRejectsEmptyQuery(t *testing.T) {
	box, err := sandbox.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	if err := box.WriteFile(context.Background(), "notes.txt", []byte("hello\n")); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	search := NewSearch(tools.Spec{}, box)
	_, err = search.Run(context.Background(), json.RawMessage(`{"pattern":" "}`))
	if err == nil || !strings.Contains(err.Error(), "pattern is required") {
		t.Fatalf("expected empty query error, got %v", err)
	}
}

func TestSearchSupportsRegexGlobAndContext(t *testing.T) {
	box, err := sandbox.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	writeSearchFile(t, box, "internal/cli/run.go", "package cli\nfunc runAgent() {}\nfunc runInspect() {}\n")
	writeSearchFile(t, box, "internal/cli/readme.txt", "func runIgnored() {}\n")
	writeSearchFile(t, box, "internal/cli/view.go", "package cli\nfunc viewOnly() {}\n")

	search := NewSearch(tools.Spec{}, box)
	result, err := search.Run(context.Background(), json.RawMessage(`{
		"path":"internal/cli",
		"pattern":"func run(Agent|Inspect)",
		"mode":"regex",
		"glob":"*.go",
		"context":1
	}`))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var out struct {
		Matches []struct {
			Path    string `json:"path"`
			Line    int    `json:"line"`
			Text    string `json:"text"`
			Context []struct {
				Line int    `json:"line"`
				Text string `json:"text"`
			} `json:"context"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(result.Output), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(out.Matches) != 2 {
		t.Fatalf("expected 2 matches, got %+v", out.Matches)
	}
	if out.Matches[0].Path != filepath.ToSlash("internal/cli/run.go") || out.Matches[0].Line != 2 {
		t.Fatalf("unexpected first match: %+v", out.Matches[0])
	}
	if len(out.Matches[0].Context) != 3 {
		t.Fatalf("missing context: %+v", out.Matches[0].Context)
	}
	if out.Matches[0].Context[0].Text != "package cli" || out.Matches[0].Context[2].Text != "func runInspect() {}" {
		t.Fatalf("unexpected context: %+v", out.Matches[0].Context)
	}
}

func TestSearchDefaultSkipsGeneratedRunDirs(t *testing.T) {
	box, err := sandbox.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	writeSearchFile(t, box, "src/main.go", "needle\n")
	writeSearchFile(t, box, "internal/runs/store.go", "needle\n")
	writeSearchFile(t, box, "src/dist/source.go", "needle\n")
	writeSearchFile(t, box, "pkg/build/source.go", "needle\n")
	writeSearchFile(t, box, "usecases/code-review-agent/.jeju-dev/config.yaml", "needle\n")
	writeSearchFile(t, box, "runs/20260531/artifact.txt", "needle\n")
	writeSearchFile(t, box, "usecases/code-review-agent/runs/20260531/artifact.txt", "needle\n")
	writeSearchFile(t, box, "usecases/code-review-agent/subdir/runs/source.txt", "needle\n")
	writeSearchFile(t, box, ".jeju-dev/tmp.txt", "needle\n")
	writeSearchFile(t, box, "dist/generated.txt", "needle\n")
	writeSearchFile(t, box, "build/generated.txt", "needle\n")
	writeSearchFile(t, box, "pkg/node_modules/generated.txt", "needle\n")

	search := NewSearch(tools.Spec{}, box)
	result, err := search.Run(context.Background(), json.RawMessage(`{"pattern":"needle","limit":10}`))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var out struct {
		Matches []struct {
			Path string `json:"path"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(result.Output), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(out.Matches) != 6 {
		t.Fatalf("unexpected matches: %+v", out.Matches)
	}
	paths := map[string]bool{}
	for _, match := range out.Matches {
		paths[match.Path] = true
	}
	for _, path := range []string{
		"src/main.go",
		"internal/runs/store.go",
		"src/dist/source.go",
		"pkg/build/source.go",
		"usecases/code-review-agent/.jeju-dev/config.yaml",
		"usecases/code-review-agent/subdir/runs/source.txt",
	} {
		if !paths[filepath.ToSlash(path)] {
			t.Fatalf("expected match for %s, got %+v", path, out.Matches)
		}
	}
}

func TestSearchSkipsSymlinkFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on windows")
	}
	workdir := t.TempDir()
	box, err := sandbox.NewLocal(workdir)
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("needle\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(workdir, "linked.txt")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	writeSearchFile(t, box, "normal.txt", "needle\n")

	search := NewSearch(tools.Spec{}, box)
	result, err := search.Run(context.Background(), json.RawMessage(`{"pattern":"needle"}`))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var out struct {
		Matches []struct {
			Path string `json:"path"`
		} `json:"matches"`
		SkippedFiles []struct {
			Path   string `json:"path"`
			Reason string `json:"reason"`
		} `json:"skippedFiles"`
	}
	if err := json.Unmarshal([]byte(result.Output), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(out.Matches) != 1 || out.Matches[0].Path != "normal.txt" {
		t.Fatalf("unexpected matches: %+v", out.Matches)
	}
	if len(out.SkippedFiles) != 1 || out.SkippedFiles[0].Path != "linked.txt" || out.SkippedFiles[0].Reason != "symlink skipped" {
		t.Fatalf("unexpected skipped files: %+v", out.SkippedFiles)
	}
}

func TestSearchDoesNotReturnPhantomTrailingContextLine(t *testing.T) {
	box, err := sandbox.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	writeSearchFile(t, box, "notes.txt", "first\nneedle\n")

	search := NewSearch(tools.Spec{}, box)
	result, err := search.Run(context.Background(), json.RawMessage(`{"pattern":"needle","context":1}`))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var out struct {
		Matches []struct {
			Context []struct {
				Line int    `json:"line"`
				Text string `json:"text"`
			} `json:"context"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(result.Output), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(out.Matches) != 1 {
		t.Fatalf("expected one match, got %+v", out.Matches)
	}
	if len(out.Matches[0].Context) != 2 {
		t.Fatalf("unexpected context: %+v", out.Matches[0].Context)
	}
	if out.Matches[0].Context[0].Text != "first" || out.Matches[0].Context[1].Text != "needle" {
		t.Fatalf("unexpected trailing context: %+v", out.Matches[0].Context)
	}
}

func TestSearchSkipsFilesOverMaxFileBytes(t *testing.T) {
	box, err := sandbox.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	writeSearchFile(t, box, "small.txt", "needle\n")
	writeSearchFile(t, box, "large.txt", strings.Repeat("x", defaultSearchMaxFileBytes+1)+"needle\n")

	search := NewSearch(tools.Spec{}, box)
	result, err := search.Run(context.Background(), json.RawMessage(`{"pattern":"needle"}`))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var out struct {
		Matches []struct {
			Path string `json:"path"`
		} `json:"matches"`
		SkippedFiles []struct {
			Path         string `json:"path"`
			Reason       string `json:"reason"`
			MaxFileBytes int    `json:"maxFileBytes"`
		} `json:"skippedFiles"`
	}
	if err := json.Unmarshal([]byte(result.Output), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(out.Matches) != 1 || out.Matches[0].Path != "small.txt" {
		t.Fatalf("unexpected matches: %+v", out.Matches)
	}
	if len(out.SkippedFiles) != 1 || out.SkippedFiles[0].Path != "large.txt" || out.SkippedFiles[0].MaxFileBytes != defaultSearchMaxFileBytes {
		t.Fatalf("unexpected skipped files: %+v", out.SkippedFiles)
	}
}

func TestSearchRejectsInvalidMode(t *testing.T) {
	box, err := sandbox.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	writeSearchFile(t, box, "notes.txt", "Alpha\n")

	search := NewSearch(tools.Spec{}, box)
	_, err = search.Run(context.Background(), json.RawMessage(`{"pattern":"alpha","mode":"glob"}`))
	if err == nil || !strings.Contains(err.Error(), "mode must be literal or regex") {
		t.Fatalf("expected mode error, got %v", err)
	}
}

func writeSearchFile(t *testing.T, box *sandbox.Local, path, content string) {
	t.Helper()
	if err := box.WriteFile(context.Background(), path, []byte(content)); err != nil {
		t.Fatalf("WriteFile(%s) failed: %v", path, err)
	}
}
