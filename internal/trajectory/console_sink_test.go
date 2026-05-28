package trajectory

import (
	"strings"
	"testing"
	"time"
)

func TestFormatConsoleStructuredOutput(t *testing.T) {
	runLine := formatConsole(Event{
		Type:  EventRunStarted,
		RunID: "20260526-000000-basic",
		TS:    time.Now(),
		Payload: map[string]any{
			"agent": "basic",
			"input": "write notes",
		},
	})
	if !strings.Contains(runLine, "Run 20260526-000000-basic") ||
		!strings.Contains(runLine, "Agent   basic") ||
		!strings.Contains(runLine, "Task    write notes") {
		t.Fatalf("unexpected run line:\n%s", runLine)
	}

	toolLine := formatConsole(Event{
		Type: EventToolRequested,
		Payload: map[string]any{
			"tool": "write",
			"input": map[string]any{
				"path": "notes.md",
			},
		},
	})
	if !strings.Contains(toolLine, "tool   write") ||
		!strings.Contains(toolLine, "path=notes.md") {
		t.Fatalf("unexpected tool line: %s", toolLine)
	}

	modelLine := formatConsole(Event{
		Type: EventModelCompleted,
		Payload: map[string]any{
			"provider":          "mimo",
			"model":             "mimo-v2.5-pro",
			"latency_ms":        int64(2876),
			"tokens_in":         538,
			"tokens_out":        49,
			"output_ref":        "artifacts/step001_model_output.txt",
			"reasoning_ref":     "artifacts/step001_model_reasoning.txt",
			"reasoning_preview": "I need to inspect the task.",
		},
	})
	if !strings.Contains(modelLine, "model  mimo/mimo-v2.5-pro  2.88s  tokens 538->49") ||
		!strings.Contains(modelLine, "output artifacts/step001_model_output.txt") ||
		!strings.Contains(modelLine, "thinking artifacts/step001_model_reasoning.txt") ||
		!strings.Contains(modelLine, "thought  I need to inspect the task.") {
		t.Fatalf("unexpected model line: %s", modelLine)
	}

	if line := formatConsole(Event{Type: EventModelStarted}); line != "" {
		t.Fatalf("model.started should be hidden in console output, got %q", line)
	}
	if line := formatConsole(Event{Type: EventPermissionApproved}); line != "" {
		t.Fatalf("permission.approved should be hidden in console output, got %q", line)
	}

	if line := formatConsole(Event{Type: EventArtifactCreated}); line != "" {
		t.Fatalf("artifact.created should be hidden in console output, got %q", line)
	}
}
