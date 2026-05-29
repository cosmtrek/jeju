package trajectory

import "time"

type EventType string

const (
	EventRunStarted     EventType = "run.started"
	EventRunCompleted   EventType = "run.completed"
	EventRunFailed      EventType = "run.failed"
	EventRunCancelled   EventType = "run.cancelled"
	EventStepStarted    EventType = "step.started"
	EventStepCompleted  EventType = "step.completed"
	EventModelStarted   EventType = "model.started"
	EventModelCompleted EventType = "model.completed"
	EventModelFailed    EventType = "model.failed"

	EventContextEstimated            EventType = "context.estimated"
	EventContextSummaryStarted       EventType = "context.summary.started"
	EventContextSummaryCompleted     EventType = "context.summary.completed"
	EventContextSummaryFailed        EventType = "context.summary.failed"
	EventContextCompressionStarted   EventType = "context.compression.started"
	EventContextCompressionCompleted EventType = "context.compression.completed"
	EventContextCompressionFailed    EventType = "context.compression.failed"

	EventActionParsed      EventType = "action.parsed"
	EventActionParseFailed EventType = "action.parse_failed"

	EventToolRequested EventType = "tool.requested"
	EventToolStarted   EventType = "tool.started"
	EventToolCompleted EventType = "tool.completed"
	EventToolFailed    EventType = "tool.failed"

	EventPermissionChecked  EventType = "permission.checked"
	EventPermissionApproved EventType = "permission.approved"
	EventPermissionDenied   EventType = "permission.denied"

	EventArtifactCreated     EventType = "artifact.created"
	EventUserInputRequested  EventType = "user.input.requested"
	EventUserInputReceived   EventType = "user.input.received"
	EventEvaluationStarted   EventType = "evaluation.started"
	EventEvaluationCompleted EventType = "evaluation.completed"
	EventEvaluationFailed    EventType = "evaluation.failed"
	EventSkillDisclosed      EventType = "skill.disclosed"
	EventSkillLoaded         EventType = "skill.loaded"
)

type Event struct {
	ID      string         `json:"id"`
	Type    EventType      `json:"type"`
	RunID   string         `json:"run_id"`
	Step    int            `json:"step,omitempty"`
	TS      time.Time      `json:"ts"`
	Actor   string         `json:"actor"`
	Payload map[string]any `json:"payload,omitempty"`
}
