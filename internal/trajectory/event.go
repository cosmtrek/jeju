package trajectory

import "time"

const SchemaVersion = "jeju.trajectory.v1"

type EventType string

const (
	EventTrajectoryHeader  EventType = "trajectory.header"
	EventSpanStarted       EventType = "span.started"
	EventSpanEnded         EventType = "span.ended"
	EventMessageCreated    EventType = "message.created"
	EventActionCreated     EventType = "action.created"
	EventPermissionDecided EventType = "permission.decided"
	EventArtifactCreated   EventType = "artifact.created"
	EventArtifactChunk     EventType = "artifact.chunk"
	EventArtifactFinalized EventType = "artifact.finalized"
	EventRunSummary        EventType = "run.summary"
)

type SpanKind string

const (
	SpanRun       SpanKind = "run"
	SpanStep      SpanKind = "step"
	SpanLLM       SpanKind = "llm"
	SpanTool      SpanKind = "tool"
	SpanPolicy    SpanKind = "policy"
	SpanContext   SpanKind = "context"
	SpanEvaluator SpanKind = "evaluator"
	SpanSkill     SpanKind = "skill"
	SpanSubagent  SpanKind = "subagent"
	SpanShell     SpanKind = "shell"
)

type SpanStatus string

const (
	SpanStatusOK        SpanStatus = "ok"
	SpanStatusError     SpanStatus = "error"
	SpanStatusCancelled SpanStatus = "cancelled"
	SpanStatusSkipped   SpanStatus = "skipped"
)

type Event struct {
	SchemaVersion string         `json:"schema_version"`
	Seq           uint64         `json:"seq"`
	EventID       string         `json:"event_id"`
	Type          EventType      `json:"type"`
	TS            time.Time      `json:"ts"`
	TrajectoryID  string         `json:"trajectory_id"`
	SessionID     string         `json:"session_id"`
	RunID         string         `json:"run_id"`
	StepID        int            `json:"step_id,omitempty"`
	SpanID        string         `json:"span_id,omitempty"`
	ParentSpanID  string         `json:"parent_span_id,omitempty"`
	Actor         string         `json:"actor"`
	Payload       map[string]any `json:"payload"`
}
