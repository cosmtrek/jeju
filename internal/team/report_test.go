package team

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteReportRendersTaskAndChildRunDetails(t *testing.T) {
	summary := Summary{
		TeamRunID:       "20260610-000000-team",
		Team:            "review-team",
		Goal:            "Review the change",
		Status:          StatusCompleted,
		StartedAt:       "2026-06-10T14:00:00+08:00",
		EndedAt:         "2026-06-10T14:01:00+08:00",
		RoundCount:      1,
		MaxRounds:       3,
		DeclaredWorkers: []string{"reviewer"},
		Tasks: map[string]TaskState{
			"review-core": {
				ID:             "review-core",
				Worker:         "reviewer",
				Objective:      "Inspect the runtime change for logic errors",
				DependsOn:      []string{"build-packet"},
				OutputContract: OutputContract{Format: "json", RequiredFields: []string{"summary", "findings"}},
				Status:         TaskVerified,
				RoundCreated:   1,
				Attempts:       2,
				RunID:          "20260610-000001-reviewer",
				RunDir:         "child-runs/task-review-core/20260610-000001-reviewer",
				Final:          `{"summary":"clean","findings":[]}`,
				Verification:   VerificationResult{Passed: true},
			},
		},
		ChildRuns: []ChildRunSummary{
			{
				Label:  "task-review-core",
				Agent:  "code-reviewer",
				Role:   "worker",
				TaskID: "review-core",
				RunID:  "20260610-000001-reviewer",
				RunDir: "child-runs/task-review-core/20260610-000001-reviewer",
				Status: "completed",
				Stats:  Stats{ModelCalls: 3, ToolCalls: 5, PromptTokens: 1200, CompletionTokens: 300, TotalTokens: 1500, DurationMS: 4200},
			},
		},
		Final: "All good.",
	}

	out := filepath.Join(t.TempDir(), "report.html")
	if err := writeReport(out, summary); err != nil {
		t.Fatalf("writeReport failed: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read report failed: %v", err)
	}
	html := string(data)
	for _, want := range []string{
		`id="task-review-core"`,
		"Inspect the runtime change for logic errors",
		`href="#task-build-packet"`,
		"2 attempts",
		"&#34;summary&#34;: &#34;clean&#34;",
		"code-reviewer",
		`href="#task-review-core"`,
		"4.2s",
		"child-runs/task-review-core/20260610-000001-reviewer/trajectory.jsonl",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected report html to contain %q", want)
		}
	}
}

func TestTaskFinalHTMLFallsBackToMarkdown(t *testing.T) {
	if got := string(taskFinalHTML(`{"a":1}`)); !strings.Contains(got, "codeblock") {
		t.Fatalf("expected JSON final to render as code block, got %q", got)
	}
	if got := string(taskFinalHTML("## Heading\ntext")); !strings.Contains(got, "<h2") {
		t.Fatalf("expected markdown final to render as markdown, got %q", got)
	}
	if got := string(taskFinalHTML("  ")); got != "" {
		t.Fatalf("expected blank final to render empty, got %q", got)
	}
}
