package cli

import (
	"testing"

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
