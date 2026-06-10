package team

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/cosmtrek/jeju/internal/trajectory"
)

func IsRecord(record trajectory.RunRecord) bool {
	if artifactByRole(record, teamSummaryRole).ID != "" {
		return true
	}
	for _, event := range record.Events {
		if event.Type != trajectory.EventTrajectoryHeader {
			continue
		}
		if agent, ok := event.Payload["agent"].(map[string]any); ok && stringMapValue(agent, "kind") == teamKind {
			return true
		}
	}
	return false
}

func ProjectSummary(record trajectory.RunRecord) (Summary, bool) {
	if !IsRecord(record) {
		return Summary{}, false
	}
	if artifact := artifactByRole(record, teamSummaryRole); artifact.ID != "" {
		var summary Summary
		if err := json.Unmarshal([]byte(artifact.Content()), &summary); err == nil {
			fillSummaryDefaults(&summary, record)
			return summary, true
		}
	}
	summary := replaySummary(record)
	fillSummaryDefaults(&summary, record)
	return summary, true
}

func artifactByRole(record trajectory.RunRecord, role string) trajectory.Artifact {
	for _, artifact := range record.Artifacts {
		if artifact.Role == role {
			return artifact
		}
	}
	return trajectory.Artifact{}
}

func replaySummary(record trajectory.RunRecord) Summary {
	summary := Summary{
		TeamRunID: record.RunID,
		Team:      record.Agent,
		Goal:      record.Input,
		Status:    record.Status,
		StartedAt: formatTime(record.StartedAt),
		Tasks:     map[string]TaskState{},
	}
	if record.EndedAt != nil {
		summary.EndedAt = formatTime(*record.EndedAt)
	}
	for _, event := range record.Events {
		if event.Type == trajectory.EventTrajectoryHeader {
			applyHeader(&summary, event.Payload)
		}
		if event.Type == trajectory.EventActionCreated {
			applyAction(&summary, event.StepID, event.Payload)
		}
		if event.Type == trajectory.EventSpanEnded && stringMapValue(event.Payload, "kind") == string(trajectory.SpanSubagent) {
			applyChildSpan(&summary, event.Payload)
		}
	}
	if summary.RoundCount == 0 {
		for _, task := range summary.Tasks {
			if task.RoundCreated > summary.RoundCount {
				summary.RoundCount = task.RoundCreated
			}
		}
	}
	if summary.Final == "" && record.FinalRef != "" {
		summary.Final = record.ArtifactContent(record.FinalRef)
	}
	return summary
}

func fillSummaryDefaults(summary *Summary, record trajectory.RunRecord) {
	if summary.TeamRunID == "" {
		summary.TeamRunID = record.RunID
	}
	if summary.Team == "" {
		summary.Team = record.Agent
	}
	if summary.Goal == "" {
		summary.Goal = record.Input
	}
	if summary.StartedAt == "" && !record.StartedAt.IsZero() {
		summary.StartedAt = formatTime(record.StartedAt)
	}
	if summary.EndedAt == "" && record.EndedAt != nil {
		summary.EndedAt = formatTime(*record.EndedAt)
	}
	if summary.Status == "" {
		summary.Status = record.Status
	}
	if summary.Tasks == nil {
		summary.Tasks = map[string]TaskState{}
	}
	if summary.Final == "" && record.FinalRef != "" {
		summary.Final = record.ArtifactContent(record.FinalRef)
	}
	sort.Strings(summary.DeclaredWorkers)
}

func applyHeader(summary *Summary, payload map[string]any) {
	team, _ := payload["team"].(map[string]any)
	summary.MaxRounds = intMapValue(team, "max_rounds")
	summary.MaxTasks = intMapValue(team, "max_tasks")
	if workers, ok := team["workers"].([]any); ok {
		for _, worker := range workers {
			if text, ok := worker.(string); ok {
				summary.DeclaredWorkers = append(summary.DeclaredWorkers, text)
			}
		}
		sort.Strings(summary.DeclaredWorkers)
	}
}

func applyAction(summary *Summary, stepID int, payload map[string]any) {
	if stringMapValue(payload, "kind") != "orchestration" {
		return
	}
	operation := stringMapValue(payload, "operation")
	taskID := stringMapValue(payload, "task_id")
	switch operation {
	case "lead.decision":
		if stepID > summary.RoundCount {
			summary.RoundCount = stepID
		}
	case "task.created":
		if taskID == "" {
			return
		}
		task := summary.Tasks[taskID]
		task.ID = taskID
		task.Worker = stringMapValue(payload, "worker")
		task.Objective = stringMapValue(payload, "objective")
		task.ContextRefs = stringSliceMapValue(payload, "context_refs")
		task.DependsOn = stringSliceMapValue(payload, "depends_on")
		task.Status = TaskPlanned
		task.RoundCreated = intMapValue(payload, "round")
		summary.Tasks[taskID] = task
	case "task.started":
		updateTask(summary, taskID, func(task *TaskState) {
			task.Status = TaskRunning
			task.Attempts = intMapValue(payload, "attempt")
		})
	case "task.completed":
		updateTask(summary, taskID, func(task *TaskState) {
			task.Status = TaskCompleted
			task.RunID = stringMapValue(payload, "run_id")
			task.RunDir = stringMapValue(payload, "run_dir")
		})
	case "task.verified":
		updateTask(summary, taskID, func(task *TaskState) {
			task.Status = TaskVerified
			task.Verification = VerificationResult{Passed: true}
		})
	case "task.retry_scheduled":
		updateTask(summary, taskID, func(task *TaskState) {
			task.Status = TaskRetryScheduled
			task.Verification = VerificationResult{Passed: false, Reasons: stringSliceMapValue(payload, "reasons")}
		})
	case "task.rejected":
		updateTask(summary, taskID, func(task *TaskState) {
			task.Status = TaskRejected
			task.Error = stringMapValue(payload, "reason")
			task.Verification = VerificationResult{Passed: false, Reasons: stringSliceMapValue(payload, "reasons")}
			if task.Error != "" && len(task.Verification.Reasons) == 0 {
				task.Verification.Reasons = []string{task.Error}
			}
		})
	case "task.blocked":
		updateTask(summary, taskID, func(task *TaskState) {
			task.Status = TaskBlocked
			task.Error = stringMapValue(payload, "reason")
			task.Verification = VerificationResult{Passed: false, Reasons: []string{task.Error}}
		})
	}
}

func applyChildSpan(summary *Summary, payload map[string]any) {
	attrs, _ := payload["attrs"].(map[string]any)
	childRunID := stringMapValue(attrs, "child_run_id")
	if childRunID == "" {
		return
	}
	stats := statsFromMap(mapMapValue(payload, "metrics"))
	child := ChildRunSummary{
		Label:  stringMapValue(attrs, "label"),
		Agent:  stringMapValue(attrs, "agent"),
		Role:   stringMapValue(attrs, "role"),
		TaskID: stringMapValue(attrs, "task_id"),
		RunID:  childRunID,
		RunDir: stringMapValue(attrs, "child_run_path"),
		Status: childStatusFromSpan(payload),
		Stats:  stats,
	}
	summary.ChildRuns = append(summary.ChildRuns, child)
	summary.Stats.add(stats)
	summary.Stats.ChildRuns++
}

func updateTask(summary *Summary, taskID string, fn func(*TaskState)) {
	if taskID == "" {
		return
	}
	task := summary.Tasks[taskID]
	task.ID = taskID
	fn(&task)
	summary.Tasks[taskID] = task
}

func childStatusFromSpan(payload map[string]any) string {
	switch stringMapValue(payload, "status") {
	case string(trajectory.SpanStatusOK):
		return "completed"
	case string(trajectory.SpanStatusCancelled):
		return "cancelled"
	default:
		return "failed"
	}
}

func statsFromMap(payload map[string]any) Stats {
	return Stats{
		ChildRuns:            intMapValue(payload, "child_runs"),
		ModelCalls:           intMapValue(payload, "model_calls"),
		ToolCalls:            intMapValue(payload, "tool_calls"),
		ModelErrors:          intMapValue(payload, "model_errors"),
		ToolErrors:           intMapValue(payload, "tool_errors"),
		PermissionDenied:     intMapValue(payload, "permission_denied"),
		PromptTokens:         intMapValue(payload, "prompt_tokens"),
		PromptCacheHitTokens: intMapValue(payload, "prompt_cache_hit_tokens"),
		CompletionTokens:     intMapValue(payload, "completion_tokens"),
		TotalTokens:          intMapValue(payload, "total_tokens"),
		DurationMS:           int64MapValue(payload, "duration_ms"),
	}
}

func mapMapValue(payload map[string]any, key string) map[string]any {
	value, _ := payload[key].(map[string]any)
	return value
}

func stringMapValue(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, _ := payload[key].(string)
	return value
}

func stringSliceMapValue(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	switch value := payload[key].(type) {
	case []string:
		return value
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func intMapValue(payload map[string]any, key string) int {
	if payload == nil {
		return 0
	}
	switch value := payload[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	default:
		return 0
	}
}

func int64MapValue(payload map[string]any, key string) int64 {
	if payload == nil {
		return 0
	}
	switch value := payload[key].(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	case json.Number:
		n, _ := value.Int64()
		return n
	default:
		return 0
	}
}

func formatTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Format(time.RFC3339Nano)
}
