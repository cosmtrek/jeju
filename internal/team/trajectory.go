package team

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/cosmtrek/jeju/internal/runs"
	"github.com/cosmtrek/jeju/internal/runtime"
	"github.com/cosmtrek/jeju/internal/trajectory"
)

const (
	teamActor        = "team-controller"
	teamRunSpanID    = "span_team_run"
	teamKind         = "agent_team"
	teamSummaryRole  = "team_summary"
	teamDecisionRole = "team_decision"
)

func (c *controller) initTrajectory(outputBase string) error {
	store := runs.NewStore(outputBase)
	runDir, err := store.CreateRun(c.manifest.Metadata.Name, c.goal)
	if err != nil {
		return err
	}
	c.id = runDir.RunID
	c.runDir = runDir.Path

	recorder, err := trajectory.NewRecorderWithOptions(c.runDir, trajectory.RecorderOptions{Console: false})
	if err != nil {
		return err
	}
	c.recorder = recorder
	c.emitHeader()
	c.emitArtifact("art_team_config", "config_snapshot", "application/x-yaml", "utf-8", string(c.snapshot), nil)
	c.recorder.EmitSpanStarted(context.Background(), c.id, 0, teamRunSpanID, "", teamActor, trajectory.SpanRun, c.manifest.Metadata.Name, map[string]any{
		"attrs": map[string]any{
			"kind":     teamKind,
			"topology": c.manifest.Runtime.Topology,
		},
	})
	return nil
}

func (c *controller) closeTrajectory() error {
	if c.recorder == nil {
		return nil
	}
	return c.recorder.Close()
}

func (c *controller) emitHeader() {
	c.recorder.Emit(context.Background(), trajectory.EventTrajectoryHeader, c.id, 0, teamActor, map[string]any{
		"agent": map[string]any{
			"name": c.manifest.Metadata.Name,
			"kind": teamKind,
		},
		"input": c.goal,
		"team": map[string]any{
			"topology":     c.manifest.Runtime.Topology,
			"max_rounds":   c.manifest.Runtime.MaxRounds,
			"max_tasks":    c.manifest.Runtime.MaxTasks,
			"max_parallel": c.manifest.Runtime.MaxParallel,
			"workers":      c.workerNames(),
		},
	})
}

func (c *controller) emitArtifact(id, role, mediaType, encoding, text string, value any) string {
	if c.recorder == nil {
		return id
	}
	payload := map[string]any{
		"artifact_id": id,
		"role":        role,
		"media_type":  mediaType,
		"encoding":    encoding,
	}
	if text != "" {
		payload["text"] = text
	}
	if value != nil {
		payload["value"] = value
	}
	c.recorder.Emit(context.Background(), trajectory.EventArtifactCreated, c.id, 0, teamActor, payload)
	return id
}

func (c *controller) emitTeamAction(step int, operation string, fields map[string]any) {
	if c.recorder == nil {
		return
	}
	payload := map[string]any{
		"action_id": c.nextTeamActionID(),
		"kind":      "orchestration",
		"operation": operation,
	}
	for key, value := range fields {
		payload[key] = value
	}
	c.recorder.Emit(context.Background(), trajectory.EventActionCreated, c.id, step, teamActor, payload)
}

func (c *controller) nextTeamActionID() string {
	c.actionMu.Lock()
	defer c.actionMu.Unlock()
	c.actionID++
	return fmt.Sprintf("team_act_%06d", c.actionID)
}

func (c *controller) startRound(round int) {
	c.recorder.EmitSpanStarted(context.Background(), c.id, round, roundSpanID(round), teamRunSpanID, teamActor, trajectory.SpanStep, fmt.Sprintf("round %d", round), map[string]any{
		"attrs": map[string]any{"round": round},
	})
}

func (c *controller) endRound(round int, status trajectory.SpanStatus, addedTasks, dispatchedTasks int, errMsg string) {
	payload := map[string]any{
		"metrics": map[string]any{
			"added_tasks":      addedTasks,
			"dispatched_tasks": dispatchedTasks,
		},
	}
	if errMsg != "" {
		payload["error"] = map[string]any{"message": errMsg}
	}
	c.recorder.EmitSpanEnded(context.Background(), c.id, round, roundSpanID(round), teamRunSpanID, teamActor, trajectory.SpanStep, status, payload)
}

func (c *controller) startChildSpan(round int, label, role, taskID string, attempt int) string {
	spanID := childSpanID(label, attempt)
	attrs := map[string]any{
		"label": label,
		"role":  role,
	}
	if taskID != "" {
		attrs["task_id"] = taskID
	}
	if attempt > 0 {
		attrs["attempt"] = attempt
	}
	c.recorder.EmitSpanStarted(context.Background(), c.id, round, spanID, parentSpanIDForRound(round), actorForChild(role, taskID), trajectory.SpanSubagent, label, map[string]any{"attrs": attrs})
	return spanID
}

func (c *controller) endChildSpan(round int, spanID string, child childRunResult) {
	status := trajectory.SpanStatusOK
	if child.Status == string(runtime.StatusCancelled) {
		status = trajectory.SpanStatusCancelled
	} else if child.Status != string(runtime.StatusCompleted) {
		status = trajectory.SpanStatusError
	}
	attrs := map[string]any{
		"label":          child.Label,
		"role":           child.Role,
		"agent":          child.Agent,
		"child_run_id":   child.RunID,
		"child_run_path": c.relativeRunPath(child.RunDir),
	}
	if child.TaskID != "" {
		attrs["task_id"] = child.TaskID
	}
	c.recorder.EmitSpanEnded(context.Background(), c.id, round, spanID, parentSpanIDForRound(round), actorForChild(child.Role, child.TaskID), trajectory.SpanSubagent, status, map[string]any{
		"attrs":   attrs,
		"metrics": statsPayload(child.Stats),
	})
}

func (c *controller) endChildSpanError(round int, spanID, label, role, taskID string, attempt int, err error) {
	attrs := map[string]any{
		"label": label,
		"role":  role,
	}
	if taskID != "" {
		attrs["task_id"] = taskID
	}
	if attempt > 0 {
		attrs["attempt"] = attempt
	}
	c.recorder.EmitSpanEnded(context.Background(), c.id, round, spanID, parentSpanIDForRound(round), actorForChild(role, taskID), trajectory.SpanSubagent, trajectory.SpanStatusError, map[string]any{
		"attrs": attrs,
		"error": map[string]any{"message": err.Error()},
	})
}

func (c *controller) emitRunSummary(finalRef, summaryRef string, endedAt time.Time) {
	status := StatusCompleted
	if c.summary.Status == StatusFailed {
		status = StatusFailed
	}
	durationMS := endedAt.Sub(c.startedAt).Milliseconds()
	c.recorder.Emit(context.Background(), trajectory.EventRunSummary, c.id, 0, teamActor, map[string]any{
		"status":      status,
		"started_at":  c.startedAt.Format(time.RFC3339Nano),
		"ended_at":    endedAt.Format(time.RFC3339Nano),
		"duration_ms": durationMS,
		"final":       map[string]any{"content_ref": finalRef},
		"stats":       statsPayload(c.summary.Stats),
		"team": map[string]any{
			"status":      c.summary.Status,
			"summary_ref": summaryRef,
			"rounds":      c.summary.RoundCount,
			"tasks":       len(c.summary.Tasks),
		},
	})
}

func (c *controller) relativeRunPath(path string) string {
	rel, err := filepath.Rel(c.runDir, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func roundSpanID(round int) string {
	return fmt.Sprintf("span_round_%03d", round)
}

func childSpanID(label string, attempt int) string {
	if attempt > 0 {
		return fmt.Sprintf("span_subagent_%s_attempt_%03d", sanitizeName(label), attempt)
	}
	return "span_subagent_" + sanitizeName(label)
}

func parentSpanIDForRound(round int) string {
	if round > 0 {
		return roundSpanID(round)
	}
	return teamRunSpanID
}

func actorForChild(role, taskID string) string {
	switch {
	case role == "worker" && taskID != "":
		return "team:worker:" + taskID
	case role != "":
		return "team:" + role
	default:
		return teamActor
	}
}

func statsPayload(stats Stats) map[string]any {
	return map[string]any{
		"child_runs":              stats.ChildRuns,
		"model_calls":             stats.ModelCalls,
		"tool_calls":              stats.ToolCalls,
		"model_errors":            stats.ModelErrors,
		"tool_errors":             stats.ToolErrors,
		"permission_denied":       stats.PermissionDenied,
		"prompt_tokens":           stats.PromptTokens,
		"prompt_cache_hit_tokens": stats.PromptCacheHitTokens,
		"completion_tokens":       stats.CompletionTokens,
		"total_tokens":            stats.TotalTokens,
		"duration_ms":             stats.DurationMS,
	}
}
