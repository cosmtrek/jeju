package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cosmtrek/jeju/internal/runs"
)

func TestRunsWarnsForDuplicateRunIDsAcrossDefaultStores(t *testing.T) {
	tmp := t.TempDir()
	restoreCWD := chdir(t, tmp)
	defer restoreCWD()
	home := filepath.Join(tmp, "home")
	t.Setenv("HOME", home)
	t.Setenv(runsDirEnv, "")

	runID := "20260616-120000-agent"
	localRunDir := filepath.Join(tmp, "runs", runID)
	globalRunDir := filepath.Join(home, ".jeju", "runs", runID)
	for _, dir := range []string{localRunDir, globalRunDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create run dir failed: %v", err)
		}
		if err := writeMinimalReportTrajectory(filepath.Join(dir, runs.TrajectoryFile), runID); err != nil {
			t.Fatalf("write trajectory failed: %v", err)
		}
	}

	output := captureStdout(t, func() {
		if err := runRuns(""); err != nil {
			t.Fatalf("runs failed: %v", err)
		}
	})
	for _, want := range []string{
		"warning: run IDs in multiple run stores: " + runID,
		"use --runs-dir to choose one",
		"local",
		"global",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("runs output missing %q:\n%s", want, output)
		}
	}
}
