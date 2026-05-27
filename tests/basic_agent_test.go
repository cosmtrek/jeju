package tests

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"jeju/internal/cli"
	"jeju/internal/evaluate"
	"jeju/internal/runs"
	"jeju/internal/trajectory"
)

func TestBasicAgentFixtureFullRun(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "basic-agent")
	copyDir(t, fixturePath(t, "basic-agent"), workdir)

	restoreCWD := chdir(t, workdir)
	defer restoreCWD()

	ctx := context.Background()
	if err := cli.Execute(ctx, []string{"validate", "agents/basic.agent.yaml"}); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	withStdin(t, "y\n", func() {
		if err := cli.Execute(ctx, []string{"run", "agents/basic.agent.yaml", "write a brief AgentOps note and save it to notes.md"}); err != nil {
			t.Fatalf("run failed: %v", err)
		}
	})

	store := runs.NewStore("./runs")
	runList, err := store.ListRuns()
	if err != nil {
		t.Fatalf("list runs failed: %v", err)
	}
	if len(runList) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runList))
	}
	runID := runList[0].RunID
	if err := cli.Execute(ctx, []string{"runs"}); err != nil {
		t.Fatalf("runs command failed: %v", err)
	}
	if err := cli.Execute(ctx, []string{"inspect", runID}); err != nil {
		t.Fatalf("inspect command failed: %v", err)
	}

	runDir := filepath.Join(workdir, "runs", runID)
	requireFile(t, filepath.Join(runDir, runs.MetadataFile))
	requireFile(t, filepath.Join(runDir, runs.ConfigSnapshotFile))
	requireFile(t, filepath.Join(runDir, runs.TrajectoryFile))
	requireFile(t, filepath.Join(runDir, runs.FinalFile))
	requireFile(t, filepath.Join(runDir, runs.EvaluationFile))
	requireFile(t, filepath.Join(workdir, "workspace", "basic", "notes.md"))

	events, err := trajectory.ReadFile(filepath.Join(runDir, runs.TrajectoryFile))
	if err != nil {
		t.Fatalf("read trajectory failed: %v", err)
	}
	requireEventTypes(t, events,
		trajectory.EventRunStarted,
		trajectory.EventSkillDisclosed,
		trajectory.EventSkillLoaded,
		trajectory.EventStepStarted,
		trajectory.EventModelStarted,
		trajectory.EventModelCompleted,
		trajectory.EventActionParsed,
		trajectory.EventToolRequested,
		trajectory.EventPermissionChecked,
		trajectory.EventPermissionApproved,
		trajectory.EventToolStarted,
		trajectory.EventToolCompleted,
		trajectory.EventEvaluationStarted,
		trajectory.EventEvaluationCompleted,
		trajectory.EventRunCompleted,
	)

	var result evaluate.Result
	data, err := os.ReadFile(filepath.Join(runDir, runs.EvaluationFile))
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

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "fixtures", name)
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	if err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	}); err != nil {
		t.Fatalf("copy fixture failed: %v", err)
	}
}

func chdir(t *testing.T, dir string) func() {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir failed: %v", err)
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

func requireFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected file %s: %v", path, err)
	}
	if info.IsDir() {
		t.Fatalf("expected %s to be a file", path)
	}
}

func requireEventTypes(t *testing.T, events []trajectory.Event, types ...trajectory.EventType) {
	t.Helper()
	seen := map[trajectory.EventType]bool{}
	for _, event := range events {
		seen[event.Type] = true
	}
	for _, typ := range types {
		if !seen[typ] {
			t.Fatalf("missing trajectory event %q", typ)
		}
	}
}
