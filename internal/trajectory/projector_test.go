package trajectory

import (
	"slices"
	"testing"
	"time"
)

func TestProjectDerivesCompletedPartialWithoutSummary(t *testing.T) {
	events := []Event{
		{Seq: 1, Type: EventTrajectoryHeader, RunID: "run", TS: time.Now(), Payload: map[string]any{"agent": map[string]any{"name": "agent"}, "input": "task"}},
		{Seq: 2, Type: EventArtifactCreated, RunID: "run", TS: time.Now(), Payload: map[string]any{"artifact_id": "art_final", "role": "final", "media_type": "text/markdown", "text": "done"}},
	}
	record := Project(events)
	if record.Status != "completed" {
		t.Fatalf("expected completed from final artifact, got %q", record.Status)
	}
	if record.Integrity != IntegrityPartial {
		t.Fatalf("expected partial integrity, got %q", record.Integrity)
	}
	if !slices.Contains(record.IntegrityIssues, "partial:missing_run_summary") {
		t.Fatalf("expected missing run summary issue, got %#v", record.IntegrityIssues)
	}
}

func TestProjectSummaryDoesNotOverrideEventStats(t *testing.T) {
	events := []Event{
		{Seq: 1, Type: EventTrajectoryHeader, RunID: "run", TS: time.Now(), Payload: map[string]any{"agent": map[string]any{"name": "agent"}}},
		{Seq: 2, Type: EventSpanStarted, RunID: "run", TS: time.Now(), SpanID: "span_llm_001", Actor: "model:mock", Payload: map[string]any{"kind": "llm"}},
		{Seq: 3, Type: EventSpanEnded, RunID: "run", TS: time.Now(), SpanID: "span_llm_001", Actor: "model:mock", Payload: map[string]any{"kind": "llm", "status": "ok", "metrics": map[string]any{"prompt_tokens": 10, "completion_tokens": 3, "total_tokens": 13}}},
		{Seq: 4, Type: EventRunSummary, RunID: "run", TS: time.Now(), Payload: map[string]any{"status": "completed", "stats": map[string]any{"model_calls": 99, "total_tokens": 999}}},
	}
	record := Project(events)
	if record.Stats.ModelCalls != 1 || record.Stats.TotalTokens != 13 {
		t.Fatalf("summary should not override event stats: %#v", record.Stats)
	}
}

func TestProjectLaterSummaryDoesNotClearRunSnapshotFields(t *testing.T) {
	started := time.Date(2026, 6, 4, 10, 0, 0, 0, time.UTC)
	ended := started.Add(5 * time.Second)
	later := ended.Add(2 * time.Second)
	events := []Event{
		{Seq: 1, Type: EventTrajectoryHeader, RunID: "run", TS: started, Payload: map[string]any{"agent": map[string]any{"name": "agent"}}},
		{Seq: 2, Type: EventArtifactCreated, RunID: "run", TS: started, Payload: map[string]any{"artifact_id": "art_final", "role": "final", "media_type": "text/markdown", "text": "done"}},
		{Seq: 3, Type: EventRunSummary, RunID: "run", TS: ended, Payload: map[string]any{"status": "completed", "ended_at": ended.Format(time.RFC3339Nano), "duration_ms": int64(5000), "final": map[string]any{"content_ref": "art_final"}}},
		{Seq: 4, Type: EventRunSummary, RunID: "run", TS: later, Payload: map[string]any{"evaluation": map[string]any{"passed": true, "score": 1}}},
	}
	record := Project(events)
	if record.Status != "completed" || record.FinalRef != "art_final" || record.DurationMS != 5000 {
		t.Fatalf("later summary should not clear run snapshot fields: status=%q final=%q duration=%d", record.Status, record.FinalRef, record.DurationMS)
	}
	if record.EndedAt == nil || !record.EndedAt.Equal(ended) {
		t.Fatalf("later summary should not overwrite ended_at: got %v want %v", record.EndedAt, ended)
	}
}

func TestProjectCompleteIntegrity(t *testing.T) {
	now := time.Now()
	events := []Event{
		{Seq: 1, Type: EventTrajectoryHeader, RunID: "run", TS: now, Payload: map[string]any{"agent": map[string]any{"name": "agent"}}},
		{Seq: 2, Type: EventSpanStarted, RunID: "run", TS: now, SpanID: "span_run", Actor: "runtime", Payload: map[string]any{"kind": "run"}},
		{Seq: 3, Type: EventArtifactCreated, RunID: "run", TS: now, Payload: map[string]any{"artifact_id": "art_final", "role": "final", "media_type": "text/markdown", "text": "done"}},
		{Seq: 4, Type: EventSpanEnded, RunID: "run", TS: now, SpanID: "span_run", Actor: "runtime", Payload: map[string]any{"kind": "run", "status": "ok"}},
		{Seq: 5, Type: EventRunSummary, RunID: "run", TS: now, Payload: map[string]any{"status": "completed", "final": map[string]any{"content_ref": "art_final"}}},
	}
	record := Project(events)
	if record.Integrity != IntegrityComplete || len(record.IntegrityIssues) != 0 {
		t.Fatalf("expected complete integrity, got %q %#v", record.Integrity, record.IntegrityIssues)
	}
}
