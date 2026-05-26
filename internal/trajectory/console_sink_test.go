package trajectory

import (
	"strings"
	"testing"
	"time"
)

func TestFormatConsoleStructuredOutput(t *testing.T) {
	runLine := formatConsole(Event{
		Type:  "run.started",
		RunID: "20260526-000000-basic",
		TS:    time.Now(),
		Payload: map[string]any{
			"agent": "basic",
			"input": "write notes",
		},
	})
	if !strings.Contains(runLine, "Run 20260526-000000-basic") ||
		!strings.Contains(runLine, "agent: basic") ||
		!strings.Contains(runLine, "task: write notes") {
		t.Fatalf("unexpected run line:\n%s", runLine)
	}

	toolLine := formatConsole(Event{
		Type: "tool.requested",
		Payload: map[string]any{
			"tool": "file_write",
			"input": map[string]any{
				"path": "notes.md",
			},
		},
	})
	if !strings.Contains(toolLine, "tool: requested") ||
		!strings.Contains(toolLine, "name=file_write") ||
		!strings.Contains(toolLine, "path=notes.md") {
		t.Fatalf("unexpected tool line: %s", toolLine)
	}

	if line := formatConsole(Event{Type: "artifact.created"}); line != "" {
		t.Fatalf("artifact.created should be hidden in console output, got %q", line)
	}
}
