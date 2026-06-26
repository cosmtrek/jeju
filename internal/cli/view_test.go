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
	if err := writeTrajectory(filepath.Join(runDir.Path, runs.TrajectoryFile), []trajectory.Event{
		{Type: trajectory.EventTrajectoryHeader, RunID: runDir.RunID, TS: time.Now(), Actor: "runtime", Payload: map[string]any{"agent": map[string]any{"name": "agent"}, "input": "write notes"}},
		{Type: trajectory.EventArtifactCreated, RunID: runDir.RunID, TS: time.Now(), Actor: "runtime", Payload: map[string]any{"artifact_id": "art_config", "role": "config_snapshot", "media_type": "application/x-yaml", "text": "name: agent\n"}},
		{Type: trajectory.EventSpanStarted, RunID: runDir.RunID, StepID: 1, SpanID: "span_step_001", TS: time.Now(), Actor: "runtime", Payload: map[string]any{"kind": "step"}},
		{Type: trajectory.EventArtifactCreated, RunID: runDir.RunID, StepID: 1, TS: time.Now(), Actor: "runtime", Payload: map[string]any{"artifact_id": "art_model_output", "role": "model_output", "media_type": "text/plain", "text": "output"}},
		{Type: trajectory.EventArtifactCreated, RunID: runDir.RunID, StepID: 1, TS: time.Now(), Actor: "runtime", Payload: map[string]any{"artifact_id": "art_model_reasoning", "role": "model_reasoning", "media_type": "text/plain", "text": "thinking through the task"}},
		{Type: trajectory.EventSpanEnded, RunID: runDir.RunID, StepID: 1, SpanID: "span_llm_001", ParentSpanID: "span_step_001", TS: time.Now(), Actor: "model:mock", Payload: map[string]any{
			"kind": "llm", "status": "ok", "output": map[string]any{"content_ref": "art_model_output"}, "reasoning": map[string]any{"content_ref": "art_model_reasoning", "preview": "thinking through the task"},
		}},
		{Type: trajectory.EventArtifactCreated, RunID: runDir.RunID, TS: time.Now(), Actor: "runtime", Payload: map[string]any{"artifact_id": "art_eval", "role": "evaluation", "media_type": "application/json", "text": `{"run_id":"x","passed":true,"score":1,"evaluators":[{"name":"rules","type":"rule","passed":true,"score":1}]}`}},
		{Type: trajectory.EventSpanEnded, RunID: runDir.RunID, SpanID: "span_eval_001", TS: time.Now(), Actor: "evaluate", Payload: map[string]any{"kind": "evaluator", "status": "ok", "output": map[string]any{"content_ref": "art_eval"}}},
		{Type: trajectory.EventArtifactCreated, RunID: runDir.RunID, TS: time.Now(), Actor: "runtime", Payload: map[string]any{"artifact_id": "art_final", "role": "final", "media_type": "text/markdown", "text": "done"}},
		{Type: trajectory.EventRunSummary, RunID: runDir.RunID, TS: endedAt, Actor: "runtime", Payload: map[string]any{"status": "completed", "final": map[string]any{"content_ref": "art_final"}, "ended_at": endedAt.Format(time.RFC3339Nano), "stats": map[string]any{"steps": 1, "model_calls": 1}}},
	}); err != nil {
		t.Fatalf("writeTrajectory failed: %v", err)
	}

	report, err := buildRunReport(store, runDir)
	if err != nil {
		t.Fatalf("buildRunReport failed: %v", err)
	}
	if report.Summary.ModelCompleted != 1 || len(report.Artifacts) < 4 || !report.EvaluationExists {
		t.Fatalf("unexpected report: %#v", report)
	}
	if len(report.Steps) != 1 || report.Steps[0].ReasoningRef != "art_model_reasoning" || report.Steps[0].ReasoningContent == "" {
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
	for _, want := range []string{"Jeju Run", "write notes", "Final Output", "art_model_output", "Thinking", "thinking through the task"} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected html to contain %q", want)
		}
	}
}

func TestEnsureRunReportUsesFreshReport(t *testing.T) {
	tmp := t.TempDir()
	store := runs.NewStore(filepath.Join(tmp, "runs"))
	runDir, err := store.CreateRun("agent", "write notes")
	if err != nil {
		t.Fatalf("CreateRun failed: %v", err)
	}
	trajectoryPath := filepath.Join(runDir.Path, runs.TrajectoryFile)
	if err := writeMinimalReportTrajectory(trajectoryPath, runDir.RunID); err != nil {
		t.Fatalf("writeMinimalReportTrajectory failed: %v", err)
	}
	out := defaultRunReportPath(runDir)
	if err := os.WriteFile(out, []byte("fresh report"), 0o644); err != nil {
		t.Fatalf("write report failed: %v", err)
	}
	base := time.Now().Add(-time.Hour)
	if err := os.Chtimes(trajectoryPath, base, base); err != nil {
		t.Fatalf("touch trajectory failed: %v", err)
	}
	if err := os.Chtimes(out, base.Add(time.Minute), base.Add(time.Minute)); err != nil {
		t.Fatalf("touch report failed: %v", err)
	}

	generated, err := ensureRunReport(store, runDir, out)
	if err != nil {
		t.Fatalf("ensureRunReport failed: %v", err)
	}
	if generated {
		t.Fatal("expected fresh report to be reused")
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read report failed: %v", err)
	}
	if string(data) != "fresh report" {
		t.Fatalf("fresh report was rewritten:\n%s", data)
	}
}

func TestEnsureRunReportRefreshesStaleReport(t *testing.T) {
	tmp := t.TempDir()
	store := runs.NewStore(filepath.Join(tmp, "runs"))
	runDir, err := store.CreateRun("agent", "write notes")
	if err != nil {
		t.Fatalf("CreateRun failed: %v", err)
	}
	trajectoryPath := filepath.Join(runDir.Path, runs.TrajectoryFile)
	if err := writeMinimalReportTrajectory(trajectoryPath, runDir.RunID); err != nil {
		t.Fatalf("writeMinimalReportTrajectory failed: %v", err)
	}
	out := defaultRunReportPath(runDir)
	if err := os.WriteFile(out, []byte("stale report"), 0o644); err != nil {
		t.Fatalf("write report failed: %v", err)
	}
	base := time.Now().Add(-time.Hour)
	if err := os.Chtimes(out, base, base); err != nil {
		t.Fatalf("touch report failed: %v", err)
	}
	if err := os.Chtimes(trajectoryPath, base.Add(time.Minute), base.Add(time.Minute)); err != nil {
		t.Fatalf("touch trajectory failed: %v", err)
	}

	generated, err := ensureRunReport(store, runDir, out)
	if err != nil {
		t.Fatalf("ensureRunReport failed: %v", err)
	}
	if !generated {
		t.Fatal("expected stale report to be regenerated")
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read report failed: %v", err)
	}
	if !strings.Contains(string(data), "Jeju Run") {
		t.Fatalf("report was not regenerated:\n%s", data)
	}
}

func TestBuildStepViewsMultipleToolCalls(t *testing.T) {
	runID := "run-multi"
	events := []trajectory.Event{
		{Type: trajectory.EventSpanStarted, RunID: runID, StepID: 1, SpanID: "span_step_001", Actor: "runtime", Payload: map[string]any{"kind": "step"}},
		{Type: trajectory.EventActionCreated, RunID: runID, StepID: 1, Actor: "runtime", Payload: map[string]any{"kind": "tool_call", "tool_call_id": "call_1", "function_name": "search_api", "arguments": map[string]any{"query": "first query"}}},
		{Type: trajectory.EventSpanEnded, RunID: runID, StepID: 1, SpanID: "span_tool_001", Actor: "tool:search_api", Payload: map[string]any{"kind": "tool", "status": "ok", "tool_call_id": "call_1"}},
		{Type: trajectory.EventActionCreated, RunID: runID, StepID: 1, Actor: "runtime", Payload: map[string]any{"kind": "tool_call", "tool_call_id": "call_2", "function_name": "search_api", "arguments": map[string]any{"query": "second query"}}},
		{Type: trajectory.EventSpanEnded, RunID: runID, StepID: 1, SpanID: "span_tool_002", Actor: "tool:search_api", Payload: map[string]any{"kind": "tool", "status": "ok", "tool_call_id": "call_2"}},
		{Type: trajectory.EventSpanEnded, RunID: runID, StepID: 1, SpanID: "span_step_001", Actor: "runtime", Payload: map[string]any{"kind": "step", "status": "ok"}},
	}

	steps := buildStepViews(trajectory.Project(events), "")
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

func TestBuildStepViewsAgentToolSubagent(t *testing.T) {
	runID := "run-agent-tool"
	events := []trajectory.Event{
		{Type: trajectory.EventSpanStarted, RunID: runID, StepID: 1, SpanID: "span_step_001", Actor: "runtime", Payload: map[string]any{"kind": "step"}},
		{Type: trajectory.EventActionCreated, RunID: runID, StepID: 1, Actor: "runtime", Payload: map[string]any{"kind": "tool_call", "tool_call_id": "call_agent", "function_name": "ask_retriever", "arguments": map[string]any{"task": "summarize"}}},
		{Type: trajectory.EventSpanStarted, RunID: runID, StepID: 1, SpanID: "span_tool_001", Actor: "tool:ask_retriever", Payload: map[string]any{"kind": "tool", "tool_call_id": "call_agent"}},
		{Type: trajectory.EventSpanStarted, RunID: runID, StepID: 1, SpanID: "span_subagent_001", ParentSpanID: "span_tool_001", Actor: "subagent:ask_retriever", Payload: map[string]any{"kind": "subagent", "tool_call_id": "call_agent", "attrs": map[string]any{"tool": "ask_retriever"}}},
		{Type: trajectory.EventSpanEnded, RunID: runID, StepID: 1, SpanID: "span_subagent_001", ParentSpanID: "span_tool_001", Actor: "subagent:ask_retriever", Payload: map[string]any{"kind": "subagent", "status": "ok", "tool_call_id": "call_agent", "attrs": map[string]any{"tool": "ask_retriever", "agent": "retriever", "child_run_id": "child-1", "child_run_path": "child-runs/ask_retriever/call_agent/child-1", "status": "completed"}, "metrics": map[string]any{"model_calls": 1, "tool_calls": 2, "total_tokens": 42}}},
		{Type: trajectory.EventSpanEnded, RunID: runID, StepID: 1, SpanID: "span_tool_001", Actor: "tool:ask_retriever", Payload: map[string]any{"kind": "tool", "status": "ok", "tool_call_id": "call_agent"}},
		{Type: trajectory.EventSpanEnded, RunID: runID, StepID: 1, SpanID: "span_step_001", Actor: "runtime", Payload: map[string]any{"kind": "step", "status": "ok"}},
	}

	steps := buildStepViews(trajectory.Project(events), "")
	if len(steps) != 1 || len(steps[0].ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %+v", steps)
	}
	tc := steps[0].ToolCalls[0]
	if tc.Kind != "agent" || !tc.AgentTool {
		t.Fatalf("expected agent tool view, got %+v", tc)
	}
	if tc.AgentName != "retriever" || tc.AgentRunID != "child-1" || tc.AgentTotalTokens != 42 {
		t.Fatalf("agent tool details not projected: %+v", tc)
	}
}

func TestBuildStepViewsContextCompression(t *testing.T) {
	runID := "run-compress"
	events := []trajectory.Event{
		{Type: trajectory.EventSpanEnded, RunID: runID, StepID: 6, SpanID: "span_context_006_estimate", Actor: "context", Payload: map[string]any{
			"kind": "context", "status": "ok", "operation": "estimate",
			"metrics": map[string]any{"estimated_tokens": float64(6650), "threshold_tokens": float64(5990), "context_window": float64(16000), "effective_input_limit": float64(14976)},
			"attrs":   map[string]any{"compression_required": true},
		}},
		{Type: trajectory.EventSpanEnded, RunID: runID, StepID: 6, SpanID: "span_context_006_compression", Actor: "context", Payload: map[string]any{
			"kind": "context", "status": "ok", "operation": "compaction", "summary_ref": "art_context_summary", "attrs": map[string]any{"strategies": []any{"tool_result_truncate", "summary"}, "report_ref": "art_context_report"},
			"metrics": map[string]any{"before_tokens": float64(6650), "after_tokens": float64(5691), "preserved_blocks": float64(4), "truncated_tool_results": float64(1)},
		}},
	}

	steps := buildStepViews(trajectory.Project(events), "")
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
}

func TestBuildStepViewsContextEstimateOnlyNoPanel(t *testing.T) {
	runID := "run-noop"
	events := []trajectory.Event{
		{Type: trajectory.EventSpanEnded, RunID: runID, StepID: 1, SpanID: "span_context_001_estimate", Actor: "context", Payload: map[string]any{
			"kind": "context", "status": "ok", "operation": "estimate",
			"metrics": map[string]any{"estimated_tokens": float64(1200), "threshold_tokens": float64(5990), "context_window": float64(16000)},
			"attrs":   map[string]any{"compression_required": false},
		}},
	}
	steps := buildStepViews(trajectory.Project(events), "")
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

func writeMinimalReportTrajectory(path, runID string) error {
	endedAt := time.Now()
	return writeTrajectory(path, []trajectory.Event{
		{Type: trajectory.EventTrajectoryHeader, RunID: runID, TS: endedAt.Add(-time.Second), Actor: "runtime", Payload: map[string]any{"agent": map[string]any{"name": "agent"}, "input": "write notes"}},
		{Type: trajectory.EventArtifactCreated, RunID: runID, TS: endedAt, Actor: "runtime", Payload: map[string]any{"artifact_id": "art_final", "role": "final", "media_type": "text/markdown", "text": "done"}},
		{Type: trajectory.EventRunSummary, RunID: runID, TS: endedAt, Actor: "runtime", Payload: map[string]any{"status": "completed", "final": map[string]any{"content_ref": "art_final"}, "ended_at": endedAt.Format(time.RFC3339Nano)}},
	})
}

func disableReportOpen(t *testing.T) {
	t.Helper()
	old := openReportFile
	openReportFile = func(string) error { return nil }
	t.Cleanup(func() {
		openReportFile = old
	})
}
