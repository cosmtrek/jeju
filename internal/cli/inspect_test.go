package cli

import (
	"os"
	"path/filepath"
	"testing"

	"jeju/internal/trajectory"
)

func TestSummarizeInspect(t *testing.T) {
	summary := summarizeInspect([]trajectory.Event{
		{Type: "step.started"},
		{Type: "model.started"},
		{Type: "model.completed"},
		{Type: "tool.started"},
		{Type: "tool.completed"},
		{Type: "permission.checked"},
		{Type: "permission.approved"},
		{Type: "skill.disclosed"},
		{Type: "skill.loaded"},
		{Type: "artifact.created"},
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

func TestReadEvaluationSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evaluation.json")
	if err := os.WriteFile(path, []byte(`{"passed":true,"score":0.75,"evaluators":[{"name":"basic"}]}`), 0o644); err != nil {
		t.Fatalf("write evaluation failed: %v", err)
	}
	summary := readEvaluationSummary(path)
	if !summary.Exists || !summary.Passed || summary.Score != 0.75 || summary.Evaluators != 1 {
		t.Fatalf("unexpected evaluation summary: %#v", summary)
	}
}
