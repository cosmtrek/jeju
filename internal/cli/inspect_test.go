package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cosmtrek/jeju/internal/runs"
	"github.com/cosmtrek/jeju/internal/trajectory"
)

func TestSummarizeInspect(t *testing.T) {
	summary := summarizeInspect([]trajectory.Event{
		{Type: trajectory.EventSpanStarted, Payload: map[string]any{"kind": "step"}},
		{Type: trajectory.EventSpanStarted, Payload: map[string]any{"kind": "llm"}},
		{Type: trajectory.EventSpanEnded, Payload: map[string]any{"kind": "llm", "status": "ok"}},
		{Type: trajectory.EventSpanStarted, Payload: map[string]any{"kind": "tool"}},
		{Type: trajectory.EventSpanEnded, Payload: map[string]any{"kind": "tool", "status": "ok"}},
		{Type: trajectory.EventPermissionDecided, Payload: map[string]any{"decision": "approved"}},
		{Type: trajectory.EventSpanEnded, Payload: map[string]any{"kind": "skill", "status": "ok", "output": map[string]any{"count": 1}}},
		{Type: trajectory.EventSpanEnded, Payload: map[string]any{"kind": "skill", "status": "ok", "output": map[string]any{"name": "x"}}},
		{Type: trajectory.EventArtifactCreated},
	})
	if summary.Steps != 1 ||
		summary.ModelStarted != 1 ||
		summary.ModelCompleted != 1 ||
		summary.ToolStarted != 1 ||
		summary.ToolCompleted != 1 ||
		summary.PermissionChecked != 1 ||
		summary.PermissionApproved != 1 ||
		summary.SkillDisclosed != 1 ||
		summary.SkillLoaded != 1 ||
		summary.Artifacts != 1 {
		t.Fatalf("unexpected inspect summary: %#v", summary)
	}
}

func TestInspectAmbiguousRunRequiresRunsDir(t *testing.T) {
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

	err := runInspect(runID, "")
	if err == nil {
		t.Fatal("expected ambiguous run error")
	}
	if !strings.Contains(err.Error(), "exists in multiple run stores") ||
		!strings.Contains(err.Error(), "--runs-dir") {
		t.Fatalf("unexpected ambiguous run error: %v", err)
	}
}
