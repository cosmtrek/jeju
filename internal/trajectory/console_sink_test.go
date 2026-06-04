package trajectory

import (
	"strings"
	"testing"
	"time"
)

func TestFormatConsoleStructuredOutput(t *testing.T) {
	runLine := formatConsole(Event{
		Type:  EventTrajectoryHeader,
		RunID: "20260526-000000-basic",
		TS:    time.Now(),
		Payload: map[string]any{
			"agent": map[string]any{"name": "basic"},
			"input": "write notes",
		},
	})
	if !strings.Contains(runLine, "Run 20260526-000000-basic") ||
		!strings.Contains(runLine, "Agent   basic") ||
		!strings.Contains(runLine, "Task    write notes") {
		t.Fatalf("unexpected run line:\n%s", runLine)
	}

	toolLine := formatConsole(Event{
		Type: EventActionCreated,
		Payload: map[string]any{
			"kind":          "tool_call",
			"function_name": "write",
			"arguments": map[string]any{
				"path": "notes.md",
			},
		},
	})
	if !strings.Contains(toolLine, "action tool_call  write") {
		t.Fatalf("unexpected tool line: %s", toolLine)
	}

	modelLine := formatConsole(Event{
		Type:  EventSpanEnded,
		Actor: "model:primary",
		Payload: map[string]any{
			"kind":   "llm",
			"status": "ok",
			"metrics": map[string]any{
				"latency_ms":        int64(2876),
				"prompt_tokens":     538,
				"completion_tokens": 49,
			},
			"attrs":     map[string]any{"provider": "mock", "model": "mock-react"},
			"output":    map[string]any{"content_ref": "art_model_output"},
			"reasoning": map[string]any{"content_ref": "art_model_reasoning"},
		},
	})
	if !strings.Contains(modelLine, "model  mock/mock-react  2.88s  tokens 538->49") ||
		!strings.Contains(modelLine, "output art_model_output") ||
		!strings.Contains(modelLine, "thinking art_model_reasoning") {
		t.Fatalf("unexpected model line: %s", modelLine)
	}

	if line := formatConsole(Event{Type: EventSpanStarted, Payload: map[string]any{"kind": "llm"}}); line != "" {
		t.Fatalf("llm span.started should be hidden in console output, got %q", line)
	}
	if line := formatConsole(Event{Type: EventPermissionDecided, Payload: map[string]any{"decision": "approved", "tool": "write"}}); !strings.Contains(line, "approved") {
		t.Fatalf("permission.decided should be shown in console output, got %q", line)
	}

	if line := formatConsole(Event{Type: EventArtifactCreated}); line != "" {
		t.Fatalf("artifact.created should be hidden in console output, got %q", line)
	}
}
