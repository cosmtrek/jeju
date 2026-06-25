package model

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type MockClient struct {
	Config ProviderConfig
}

func NewMockClient(cfg ProviderConfig) *MockClient {
	return &MockClient{Config: cfg}
}

func (c *MockClient) Generate(ctx context.Context, req Request) (Response, error) {
	start := time.Now()
	select {
	case <-ctx.Done():
		return Response{}, ctx.Err()
	default:
	}

	task := metadataString(req.Metadata, "task")
	observations := strings.ToLower(metadataString(req.Metadata, "observations"))
	taskLower := strings.ToLower(task)

	var payload map[string]any
	if strings.Contains(taskLower, "teamdecision") || strings.Contains(taskLower, "team run bootstrap") || strings.Contains(taskLower, "team state update") {
		payload = map[string]any{
			"type":    "final",
			"thought": "The mock lead returns a deterministic team decision.",
			"content": mockTeamDecision(taskLower),
		}
	} else if strings.Contains(taskLower, "jeju team worker task") && strings.Contains(taskLower, "worker: writer") && shouldMockWrite(taskLower, observations) {
		payload = map[string]any{
			"type":    "tool_call",
			"thought": "The worker task asks for a saved file, so I will write the requested report into the workspace.",
			"tool":    "write",
			"input": map[string]any{
				"path":    mockWritePath(taskLower),
				"content": mockReport(task),
			},
		}
	} else if strings.Contains(taskLower, "jeju team worker task") {
		payload = map[string]any{
			"type":    "final",
			"thought": "The mock worker returns structured task output.",
			"content": mockTeamTaskOutput(task),
		}
	} else if shouldMockWrite(taskLower, observations) {
		payload = map[string]any{
			"type":    "tool_call",
			"thought": "The task asks for a saved file, so I will write the requested notes into the workspace.",
			"tool":    "write",
			"input": map[string]any{
				"path":    mockWritePath(taskLower),
				"content": mockReport(task),
			},
		}
	} else {
		payload = map[string]any{
			"type":    "final",
			"thought": "The mock model has enough information to finish.",
			"content": mockReport(task),
		}
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}
	return Response{
		Text:      string(data),
		Raw:       data,
		LatencyMS: time.Since(start).Milliseconds(),
		Model:     c.Config.Model,
		Provider:  c.Config.Provider,
	}, nil
}

func shouldMockWrite(taskLower, observations string) bool {
	if strings.Contains(observations, "tool write completed") || strings.Contains(observations, "write ok") {
		return false
	}
	return strings.Contains(taskLower, "notes.md") ||
		strings.Contains(taskLower, "保存") ||
		strings.Contains(taskLower, "save") ||
		strings.Contains(taskLower, "write") ||
		strings.Contains(taskLower, "写入")
}

func mockWritePath(taskLower string) string {
	if strings.Contains(taskLower, "agent-team-mechanism.md") {
		return "reports/agent-team-mechanism.md"
	}
	if strings.Contains(taskLower, "notes.md") {
		return "notes.md"
	}
	return "notes.md"
}

func mockReport(task string) string {
	if strings.TrimSpace(task) == "" {
		task = "local agent task"
	}
	taskLower := strings.ToLower(task)
	if strings.Contains(taskLower, "jeju team synthesis") || strings.Contains(taskLower, "agent-team-mechanism.md") {
		return `# Agent Team Mechanism Recommendation

## Executive Summary

Use a lead-worker AgentTeam where the lead dynamically plans rounds and workers execute isolated subtasks.

## Recommended Jeju Mechanism

Keep AgentTeam as an outer controller. Each worker remains a normal compiled Jeju Agent run.

## Round Evidence

The team ran lead planning, worker task execution, verification, and final writing.

## Worker Findings

Worker outputs are structured JSON with findings, evidence, gaps, and residual risk.

## Verification Result

Verified task outputs are safe for final output.

## Risks and Deferred Work

Defer peer-to-peer chat, shared mailbox, and file locking.

## Acceptance Checklist

- lead_worker topology
- bounded maxRounds
- declared worker catalog
- child run trajectories
- structured verification
`
	}
	return fmt.Sprintf(`# Jeju Mock Result

Task: %s

This is a deterministic mock response. It demonstrates the full Jeju run lifecycle: model action parsing, permission checking, tool execution, trajectory recording, final output, and rule-based evaluation.
`, task)
}

func mockTeamDecision(taskLower string) string {
	if strings.Contains(taskLower, "blocked decision") {
		return `{
  "decision": "abort",
  "round_summary": "The lead cannot complete this mock task.",
  "tasks": [],
  "finish": null,
  "abort": {"reason": "Team aborted by lead without a final explanation."}
}`
	}
	round := mockTeamRound(taskLower)
	switch round {
	case 1:
		return `{
  "decision": "continue",
  "round_summary": "Plan initial framework and Jeju-fit research tasks.",
  "tasks": [
    {
      "id": "framework-summary",
      "worker": "framework_researcher",
      "objective": "Extract external agent-team mechanism patterns from the local corpus.",
      "context_refs": [],
      "depends_on": [],
      "output_contract": {
        "format": "json",
        "required_fields": ["summary", "findings", "evidence", "gaps", "residual_risk"]
      }
    },
    {
      "id": "jeju-fit-analysis",
      "worker": "jeju_architect",
      "objective": "Map agent-team mechanism patterns onto Jeju runtime constraints.",
      "context_refs": [],
      "depends_on": [],
      "output_contract": {
        "format": "json",
        "required_fields": ["summary", "findings", "evidence", "gaps", "residual_risk"]
      }
    }
	  ],
	  "finish": null,
	  "abort": null
	}`
	case 2:
		return `{
  "decision": "continue",
  "round_summary": "Add a verifier task after initial worker outputs.",
  "tasks": [
    {
      "id": "final-readiness-check",
      "worker": "verifier",
      "objective": "Check whether the worker findings are complete enough for final output.",
      "context_refs": ["task:framework-summary", "task:jeju-fit-analysis"],
      "depends_on": ["framework-summary", "jeju-fit-analysis"],
      "output_contract": {
        "format": "json",
        "required_fields": ["summary", "findings", "evidence", "gaps", "residual_risk", "ready_for_final"]
      }
    }
	  ],
	  "finish": null,
	  "abort": null
	}`
	case 3:
		return `{
  "decision": "continue",
  "round_summary": "Add a normal writer task for the final report.",
  "tasks": [
    {
      "id": "final-report",
      "worker": "writer",
      "objective": "Write final report to reports/agent-team-mechanism.md from verified framework, Jeju-fit, and verifier outputs.",
      "context_refs": ["framework-summary", "jeju-fit-analysis", "final-readiness-check"],
      "depends_on": ["framework-summary", "jeju-fit-analysis", "final-readiness-check"],
      "output_contract": {
        "format": "markdown"
      }
    }
  ],
  "finish": null,
  "abort": null
}`
	default:
		return `{
  "decision": "finish",
  "round_summary": "The final report task is verified; use it as the team final answer.",
  "tasks": [],
  "finish": {"task_id": "final-report"},
  "abort": null
}`
	}
}

func mockTeamRound(taskLower string) int {
	for _, marker := range []string{"round: 1", "round: 2", "round: 3", "round: 4"} {
		if strings.Contains(taskLower, "\n"+marker+"\n") {
			switch marker {
			case "round: 1":
				return 1
			case "round: 2":
				return 2
			case "round: 3":
				return 3
			case "round: 4":
				return 4
			}
		}
	}
	return 4
}

func mockTeamTaskOutput(task string) string {
	lower := strings.ToLower(task)
	worker := "worker"
	if strings.Contains(lower, "framework_researcher") {
		worker = "framework_researcher"
	}
	if strings.Contains(lower, "jeju_architect") {
		worker = "jeju_architect"
	}
	if strings.Contains(lower, "worker: writer") {
		return mockReport(task)
	}
	if strings.Contains(lower, "verifier") {
		return `{
  "summary": "The worker outputs support lead-worker finalization.",
  "findings": ["lead_worker topology is represented", "verification is present", "peer chat is deferred"],
  "evidence": ["framework-summary", "jeju-fit-analysis"],
  "gaps": [],
  "residual_risk": "Mock verification does not judge factual quality.",
	  "ready_for_final": true
	}`
	}
	return fmt.Sprintf(`{
  "summary": "Structured mock output from %s.",
  "findings": ["Use a lead-worker topology", "Keep worker runs isolated", "Use artifacts and task state for communication"],
  "evidence": ["local corpus", "Jeju runtime constraints"],
  "gaps": ["Live external source freshness is not checked in mock mode"],
  "residual_risk": "Mock output validates mechanics, not research quality."
}`, worker)
}

func metadataString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}
