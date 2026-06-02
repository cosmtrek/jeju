package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cosmtrek/jeju/internal/runs"
	"github.com/cosmtrek/jeju/internal/trajectory"
)

func TestBuildRunReportAndWriteHTML(t *testing.T) {
	tmp := t.TempDir()
	store := runs.NewStore(filepath.Join(tmp, "runs"))
	runDir, err := store.CreateRun("agent", "write notes")
	if err != nil {
		t.Fatalf("CreateRun failed: %v", err)
	}
	endedAt := time.Now().Add(time.Second)
	if err := store.WriteMetadata(runDir.RunID, runs.Metadata{
		RunID:          runDir.RunID,
		Agent:          "agent",
		Status:         "completed",
		StartedAt:      time.Now(),
		EndedAt:        &endedAt,
		Input:          "write notes",
		ConfigSnapshot: runs.ConfigSnapshotFile,
		Trajectory:     runs.TrajectoryFile,
		Final:          runs.FinalFile,
		Evaluation:     runs.EvaluationFile,
	}); err != nil {
		t.Fatalf("WriteMetadata failed: %v", err)
	}
	if err := store.WriteConfigSnapshot(runDir.RunID, []byte("name: agent\n")); err != nil {
		t.Fatalf("WriteConfigSnapshot failed: %v", err)
	}
	if err := store.WriteFinal(runDir.RunID, "done"); err != nil {
		t.Fatalf("WriteFinal failed: %v", err)
	}
	if err := store.WriteEvaluation(runDir.RunID, []byte(`{"run_id":"x","passed":true,"score":1,"evaluators":[{"name":"rules","type":"rule","passed":true,"score":1}]}`)); err != nil {
		t.Fatalf("WriteEvaluation failed: %v", err)
	}
	if _, err := store.WriteArtifact(runDir.RunID, "step001_model_output.txt", []byte("output")); err != nil {
		t.Fatalf("WriteArtifact failed: %v", err)
	}
	if _, err := store.WriteArtifact(runDir.RunID, "step001_model_reasoning.txt", []byte("thinking through the task")); err != nil {
		t.Fatalf("WriteArtifact reasoning failed: %v", err)
	}
	if err := writeTrajectory(filepath.Join(runDir.Path, runs.TrajectoryFile), []trajectory.Event{
		{ID: "evt_000001", Type: trajectory.EventRunStarted, RunID: runDir.RunID, TS: time.Now(), Actor: "runtime", Payload: map[string]any{"agent": "agent"}},
		{ID: "evt_000002", Type: trajectory.EventModelCompleted, RunID: runDir.RunID, Step: 1, TS: time.Now(), Actor: "model:mock", Payload: map[string]any{
			"output_ref":        "artifacts/step001_model_output.txt",
			"reasoning_ref":     "artifacts/step001_model_reasoning.txt",
			"reasoning_preview": "thinking through the task",
		}},
	}); err != nil {
		t.Fatalf("writeTrajectory failed: %v", err)
	}

	report, err := buildRunReport(store, runDir)
	if err != nil {
		t.Fatalf("buildRunReport failed: %v", err)
	}
	if report.Summary.ModelCompleted != 1 || len(report.Artifacts) != 2 || !report.EvaluationExists {
		t.Fatalf("unexpected report: %#v", report)
	}
	if len(report.Steps) != 1 || report.Steps[0].ReasoningRef != "artifacts/step001_model_reasoning.txt" || report.Steps[0].ReasoningContent == "" {
		t.Fatalf("reasoning was not attached to step: %#v", report.Steps)
	}

	out := filepath.Join(tmp, "report.html")
	if err := writeRunReportHTML(out, report); err != nil {
		t.Fatalf("writeRunReportHTML failed: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read report failed: %v", err)
	}
	html := string(data)
	for _, want := range []string{"Jeju Run", "write notes", "Final Output", "step001_model_output.txt", "Thinking", "thinking through the task"} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected html to contain %q", want)
		}
	}
}

func TestBuildStepViewsMultipleToolCalls(t *testing.T) {
	runID := "run-multi"
	events := []trajectory.Event{
		{ID: "e1", Type: trajectory.EventActionParsed, RunID: runID, Step: 1, Actor: "runtime", Payload: map[string]any{"type": "tool_call", "tool": "search_api"}},
		{ID: "e2", Type: trajectory.EventToolRequested, RunID: runID, Step: 1, Actor: "model", Payload: map[string]any{"tool": "search_api", "input": map[string]any{"query": "first query"}}},
		{ID: "e3", Type: trajectory.EventToolCompleted, RunID: runID, Step: 1, Actor: "tool:search_api", Payload: map[string]any{"tool": "search_api", "status": "ok"}},
		{ID: "e4", Type: trajectory.EventActionParsed, RunID: runID, Step: 1, Actor: "runtime", Payload: map[string]any{"type": "tool_call", "tool": "search_api"}},
		{ID: "e5", Type: trajectory.EventToolRequested, RunID: runID, Step: 1, Actor: "model", Payload: map[string]any{"tool": "search_api", "input": map[string]any{"query": "second query"}}},
		{ID: "e6", Type: trajectory.EventToolCompleted, RunID: runID, Step: 1, Actor: "tool:search_api", Payload: map[string]any{"tool": "search_api", "status": "ok"}},
		{ID: "e7", Type: trajectory.EventStepCompleted, RunID: runID, Step: 1, Actor: "runtime", Payload: map[string]any{"status": "running"}},
	}

	steps := buildStepViews(events, map[string]artifactView{}, "")
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	step := steps[0]
	if step.Kind != "tool" {
		t.Fatalf("expected step kind tool, got %q", step.Kind)
	}
	if len(step.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(step.ToolCalls))
	}
	if step.Title != "search_api × 2" {
		t.Fatalf("unexpected step title: %q", step.Title)
	}
	if step.Status != "completed" {
		t.Fatalf("expected aggregate status completed, got %q", step.Status)
	}
	if !strings.Contains(step.ToolCalls[0].Input, "first query") || !strings.Contains(step.ToolCalls[1].Input, "second query") {
		t.Fatalf("tool call inputs not captured: %+v", step.ToolCalls)
	}
}

func TestBuildStepViewsContextCompression(t *testing.T) {
	runID := "run-compress"
	events := []trajectory.Event{
		{ID: "e1", Type: trajectory.EventContextEstimated, RunID: runID, Step: 6, Actor: "context", Payload: map[string]any{
			"estimated_tokens": float64(6650), "threshold_tokens": float64(5990), "context_window": float64(16000),
			"effective_input_limit": float64(14976), "compression_required": true,
		}},
		{ID: "e2", Type: trajectory.EventContextCompressionStarted, RunID: runID, Step: 6, Actor: "context", Payload: map[string]any{"before_tokens": float64(6650), "threshold_tokens": float64(5990)}},
		{ID: "e3", Type: trajectory.EventContextSummaryStarted, RunID: runID, Step: 6, Actor: "model:primary", Payload: map[string]any{}},
		{ID: "e4", Type: trajectory.EventContextSummaryCompleted, RunID: runID, Step: 6, Actor: "model:primary", Payload: map[string]any{"tokens_in": float64(565), "tokens_out": float64(136)}},
		{ID: "e5", Type: trajectory.EventContextCompressionCompleted, RunID: runID, Step: 6, Actor: "context", Payload: map[string]any{
			"before_tokens": float64(6650), "after_tokens": float64(5691), "preserved_blocks": float64(4),
			"truncated_tool_results": float64(1), "strategies": []any{"tool_result_truncate", "summary"},
			"summary_ref": "artifacts/step006_context_summary.md", "report_ref": "artifacts/step006_context_report.json",
		}},
		{ID: "e6", Type: trajectory.EventModelStarted, RunID: runID, Step: 6, Actor: "model:primary", Payload: map[string]any{"input_ref": "artifacts/step006_model_input.json"}},
		{ID: "e7", Type: trajectory.EventStepCompleted, RunID: runID, Step: 6, Actor: "runtime", Payload: map[string]any{"status": "running"}},
	}

	steps := buildStepViews(events, map[string]artifactView{}, "")
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	c := steps[0].Compression
	if c == nil {
		t.Fatal("expected compression view to be populated")
	}
	if !c.Triggered {
		t.Fatal("expected compression to be marked triggered")
	}
	if c.BeforeTokens != 6650 || c.AfterTokens != 5691 || c.ThresholdTokens != 5990 {
		t.Fatalf("unexpected token figures: %+v", c)
	}
	if c.PreservedBlocks != 4 || c.TruncatedToolResults != 1 {
		t.Fatalf("unexpected block/truncation counts: %+v", c)
	}
	if !c.Summarized || c.SummaryTokensIn != 565 || c.SummaryTokensOut != 136 {
		t.Fatalf("summary not captured: %+v", c)
	}
	if c.StrategiesLabel != "tool_result_truncate, summary" {
		t.Fatalf("unexpected strategies label: %q", c.StrategiesLabel)
	}
	if c.SummaryRef == "" || c.ReportRef == "" {
		t.Fatalf("artifact refs not captured: %+v", c)
	}
}

func TestBuildStepViewsContextEstimateOnlyNoPanel(t *testing.T) {
	runID := "run-noop"
	events := []trajectory.Event{
		{ID: "e1", Type: trajectory.EventContextEstimated, RunID: runID, Step: 1, Actor: "context", Payload: map[string]any{
			"estimated_tokens": float64(1200), "threshold_tokens": float64(5990), "context_window": float64(16000), "compression_required": false,
		}},
		{ID: "e2", Type: trajectory.EventStepCompleted, RunID: runID, Step: 1, Actor: "runtime", Payload: map[string]any{"status": "running"}},
	}
	steps := buildStepViews(events, map[string]artifactView{}, "")
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	if c := steps[0].Compression; c != nil && c.Triggered {
		t.Fatalf("expected no triggered compression panel for sub-threshold estimate: %+v", c)
	}
}

func writeTrajectory(path string, events []trajectory.Event) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if _, err := file.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	return nil
}
