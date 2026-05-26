package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"jeju/internal/evaluate"
	"jeju/internal/runs"
	"jeju/internal/trajectory"
)

func TestCoreFlowInitValidateRunInspectRuns(t *testing.T) {
	tmp := t.TempDir()
	restoreCWD := chdir(t, tmp)
	defer restoreCWD()

	ctx := context.Background()
	if err := Execute(ctx, []string{"init", "research", "--dir", "jeju-work"}); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "agents", "research.agent.yaml")); !os.IsNotExist(err) {
		t.Fatalf("init --dir wrote agent manifest outside requested output dir")
	}

	restoreWorkCWD := chdir(t, filepath.Join(tmp, "jeju-work"))
	defer restoreWorkCWD()

	if err := Execute(ctx, []string{"validate", "agents/research.agent.yaml"}); err != nil {
		t.Fatalf("validate failed: %v", err)
	}

	withStdin(t, "y\n", func() {
		if err := Execute(ctx, []string{"run", "agents/research.agent.yaml", "写一份关于 AgentOps 的简短分析，并保存到 notes.md"}); err != nil {
			t.Fatalf("run failed: %v", err)
		}
	})

	store := runs.NewStore("./runs")
	items, err := store.ListRuns()
	if err != nil {
		t.Fatalf("ListRuns failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 run, got %d", len(items))
	}
	runID := items[0].RunID

	if err := Execute(ctx, []string{"runs"}); err != nil {
		t.Fatalf("runs failed: %v", err)
	}
	if err := Execute(ctx, []string{"inspect", runID}); err != nil {
		t.Fatalf("inspect failed: %v", err)
	}
	if err := Execute(ctx, []string{"view", runID}); err != nil {
		t.Fatalf("view failed: %v", err)
	}

	runDir := filepath.Join(tmp, "jeju-work", "runs", runID)
	assertFileExists(t, filepath.Join(runDir, "metadata.json"))
	assertFileExists(t, filepath.Join(runDir, "config.snapshot.yaml"))
	assertFileExists(t, filepath.Join(runDir, "trajectory.jsonl"))
	assertFileExists(t, filepath.Join(runDir, "final.md"))
	assertFileExists(t, filepath.Join(runDir, "evaluation.json"))
	assertFileExists(t, filepath.Join(runDir, "report.html"))
	assertFileExists(t, filepath.Join(tmp, "jeju-work", "workspace", "research", "notes.md"))

	events, err := trajectory.ReadFile(filepath.Join(runDir, "trajectory.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile trajectory failed: %v", err)
	}
	requireEventTypes(t, events,
		"run.started",
		"skill.disclosed",
		"skill.loaded",
		"step.started",
		"model.started",
		"model.completed",
		"action.parsed",
		"tool.requested",
		"permission.checked",
		"permission.approved",
		"tool.started",
		"tool.completed",
		"step.completed",
		"evaluation.started",
		"evaluation.completed",
		"run.completed",
	)

	var result evaluate.Result
	data, err := os.ReadFile(filepath.Join(runDir, "evaluation.json"))
	if err != nil {
		t.Fatalf("read evaluation failed: %v", err)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal evaluation failed: %v", err)
	}
	if !result.Passed || result.Score != 1 {
		t.Fatalf("expected passing evaluation score=1, got passed=%v score=%v", result.Passed, result.Score)
	}
}

func chdir(t *testing.T, dir string) func() {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	return func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("restore cwd failed: %v", err)
		}
	}
}

func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	old := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe failed: %v", err)
	}
	if _, err := writer.WriteString(input); err != nil {
		t.Fatalf("write stdin pipe failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdin writer failed: %v", err)
	}
	os.Stdin = reader
	defer func() {
		os.Stdin = old
		_ = reader.Close()
	}()
	fn()
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.IsDir() {
		t.Fatalf("expected %s to be a file", path)
	}
}

func requireEventTypes(t *testing.T, events []trajectory.Event, types ...string) {
	t.Helper()
	seen := map[string]bool{}
	for _, event := range events {
		seen[event.Type] = true
	}
	for _, typ := range types {
		if !seen[typ] {
			t.Fatalf("missing trajectory event type %q", typ)
		}
	}
}
