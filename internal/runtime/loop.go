package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"jeju/internal/compiler"
	"jeju/internal/evaluate"
	"jeju/internal/model"
	"jeju/internal/policy"
	"jeju/internal/runs"
	"jeju/internal/tools"
	"jeju/internal/trajectory"
)

func (r *Runtime) Run(ctx context.Context, agent *compiler.CompiledAgent, input string) (*RunResult, error) {
	runDir, err := agent.RunStore.CreateRun(agent.Name, input)
	if err != nil {
		return nil, err
	}
	if err := agent.RunStore.WriteConfigSnapshot(runDir.RunID, agent.ConfigSnapshot); err != nil {
		return nil, err
	}
	recorder, err := trajectory.NewRecorder(agent.Config.Trajectory, runDir.Path)
	if err != nil {
		return nil, err
	}
	defer recorder.Close()

	state := NewRunState(runDir.RunID, agent.Name, input)
	metadata := runs.Metadata{
		RunID:          runDir.RunID,
		Agent:          agent.Name,
		Status:         string(state.Status),
		StartedAt:      state.StartedAt,
		Input:          input,
		ConfigSnapshot: "config.snapshot.yaml",
		Trajectory:     "trajectory.jsonl",
		Final:          "final.md",
	}
	if agent.Config.Evaluate.Enabled {
		metadata.Evaluation = "evaluation.json"
	}
	_ = agent.RunStore.WriteMetadata(runDir.RunID, metadata)

	recorder.Emit(ctx, "run.started", state.RunID, 0, "runtime", map[string]any{
		"agent": agent.Name,
		"input": input,
	})
	r.emitSkillEvents(ctx, recorder, state, agent)

	for !state.IsTerminal() {
		if err := shouldStop(state, agent.Config.Runtime.Limits); err != nil {
			state.AddError("stop", err)
			state.Status = StatusFailed
			break
		}
		state.Step++
		modelName := agent.Config.Runtime.Models.Reasoning
		recorder.Emit(ctx, "step.started", state.RunID, state.Step, "runtime", map[string]any{
			"model": modelName,
		})

		if err := r.runStep(ctx, agent, recorder, state, modelName); err != nil {
			state.AddError("runtime", err)
			if state.ConsecutiveErrors >= agent.Config.Runtime.Limits.MaxConsecutiveErrors {
				state.Status = StatusFailed
			}
		}
		recorder.Emit(ctx, "step.completed", state.RunID, state.Step, "runtime", map[string]any{
			"status": state.Status,
		})
	}

	if state.Status == StatusRunning {
		state.Status = StatusFailed
	}
	if state.Final == "" && state.Status == StatusFailed {
		state.Final = "Run failed before producing a final answer."
	}
	if err := agent.RunStore.WriteFinal(state.RunID, state.Final); err != nil {
		return nil, err
	}

	if agent.Config.Evaluate.Enabled && agent.Config.Evaluate.OnRunComplete {
		r.runEvaluation(ctx, agent, recorder, state)
	}

	now := time.Now()
	state.EndedAt = &now
	eventType := "run.completed"
	if state.Status == StatusFailed {
		eventType = "run.failed"
	}
	if state.Status == StatusCancelled {
		eventType = "run.cancelled"
	}
	recorder.Emit(ctx, eventType, state.RunID, state.Step, "runtime", map[string]any{
		"status":  state.Status,
		"run_dir": runDir.Path,
	})

	metadata.Status = string(state.Status)
	metadata.EndedAt = state.EndedAt
	if err := agent.RunStore.WriteMetadata(runDir.RunID, metadata); err != nil {
		return nil, err
	}
	return &RunResult{RunID: state.RunID, Status: state.Status, Final: state.Final}, nil
}

func (r *Runtime) runStep(ctx context.Context, agent *compiler.CompiledAgent, recorder *trajectory.Recorder, state *RunState, modelName string) error {
	client, cfg, ok := agent.Models.Get(modelName)
	if !ok {
		return fmt.Errorf("model %q is not compiled", modelName)
	}
	req := r.buildModelRequest(agent, state, cfg)
	inputData, _ := json.MarshalIndent(req, "", "  ")
	inputRef, _ := writeArtifact(ctx, agent, recorder, state, fmt.Sprintf("model_input_step_%d.json", state.Step), inputData, "model_input")
	recorder.Emit(ctx, "model.started", state.RunID, state.Step, "model:"+modelName, map[string]any{
		"provider":  cfg.Provider,
		"model":     cfg.Model,
		"input_ref": inputRef,
	})

	resp, err := client.Generate(ctx, req)
	state.ModelCalls++
	if err != nil {
		state.ModelErrors++
		recorder.Emit(ctx, "model.failed", state.RunID, state.Step, "model:"+modelName, map[string]any{
			"error": err.Error(),
		})
		return err
	}
	state.ResetErrors()
	outputRef, _ := writeArtifact(ctx, agent, recorder, state, fmt.Sprintf("model_output_step_%d.txt", state.Step), []byte(resp.Text), "model_output")
	recorder.Emit(ctx, "model.completed", state.RunID, state.Step, "model:"+modelName, map[string]any{
		"provider":     resp.Provider,
		"model":        resp.Model,
		"input_ref":    inputRef,
		"output_ref":   outputRef,
		"latency_ms":   resp.LatencyMS,
		"tokens_in":    resp.Usage.InputTokens,
		"tokens_out":   resp.Usage.OutputTokens,
		"tokens_total": resp.Usage.TotalTokens,
	})
	state.Messages = append(state.Messages, model.Message{Role: "assistant", Content: resp.Text})

	action, err := ParseAction(resp.Text)
	if err != nil {
		state.AddError("action_parse", err)
		recorder.Emit(ctx, "action.parse_failed", state.RunID, state.Step, "runtime", map[string]any{
			"error": err.Error(),
		})
		state.AddObservation("Invalid action JSON. Return only a valid Jeju action JSON object.")
		return nil
	}
	recorder.Emit(ctx, "action.parsed", state.RunID, state.Step, "runtime", map[string]any{
		"type":    action.Type,
		"thought": action.Thought,
		"tool":    action.Tool,
	})

	switch action.Type {
	case ActionFinal:
		state.Final = action.Content
		state.Status = StatusCompleted
	case ActionToolCall:
		r.handleToolCall(ctx, agent, recorder, state, action)
	case ActionAskUser:
		r.handleAskUser(ctx, recorder, state, action)
	}
	return nil
}

func (r *Runtime) buildModelRequest(agent *compiler.CompiledAgent, state *RunState, cfg model.ProviderConfig) model.Request {
	messages := []model.Message{{Role: "system", Content: agent.SystemPrompt()}}
	messages = append(messages, state.Messages...)
	return model.Request{
		Model:       cfg.Model,
		Messages:    messages,
		Temperature: cfg.Temperature,
		MaxTokens:   cfg.MaxOutputTokens,
		Metadata: map[string]any{
			"task":         state.Input,
			"step":         state.Step,
			"observations": strings.Join(state.Observations, "\n"),
		},
	}
}

func (r *Runtime) handleToolCall(ctx context.Context, agent *compiler.CompiledAgent, recorder *trajectory.Recorder, state *RunState, action Action) {
	recorder.Emit(ctx, "tool.requested", state.RunID, state.Step, "model", map[string]any{
		"tool":  action.Tool,
		"input": compactToolInput(action.Input),
	})
	tool, ok := agent.Tools.Get(action.Tool)
	if !ok {
		err := fmt.Errorf("unknown tool %q", action.Tool)
		state.ToolErrors++
		state.AddError("tool", err)
		recorder.Emit(ctx, "tool.failed", state.RunID, state.Step, "tool:"+action.Tool, map[string]any{
			"tool":  action.Tool,
			"error": err.Error(),
		})
		state.AddObservation(err.Error())
		return
	}
	spec := tool.Spec()
	req := policy.PermissionRequest{
		RunID: state.RunID,
		Step:  state.Step,
		Tool:  action.Tool,
		Input: action.Input,
		Risks: spec.Risks,
	}
	decision := agent.Policy.Check(req, spec)
	recorder.Emit(ctx, "permission.checked", state.RunID, state.Step, "policy", map[string]any{
		"tool":     action.Tool,
		"risks":    spec.Risks,
		"decision": decision.Action,
		"reason":   decision.Reason,
	})
	if decision.Action == policy.DecisionDeny {
		state.PermissionDenied++
		recorder.Emit(ctx, "permission.denied", state.RunID, state.Step, "policy", map[string]any{
			"tool":   action.Tool,
			"reason": decision.Reason,
		})
		state.AddObservation(fmt.Sprintf("Tool %s denied by policy: %s", action.Tool, decision.Reason))
		return
	}
	if decision.Action == policy.DecisionDryRun {
		recorder.Emit(ctx, "permission.denied", state.RunID, state.Step, "policy", map[string]any{
			"tool":   action.Tool,
			"reason": "dry_run is reserved but not implemented in V0",
		})
		state.AddObservation(fmt.Sprintf("Tool %s skipped: dry_run is not implemented in V0.", action.Tool))
		return
	}
	if decision.Action == policy.DecisionAsk {
		approved, stop := r.askApproval(action.Tool, spec.Risks)
		if stop {
			state.Status = StatusCancelled
			state.AddObservation("User stopped the run.")
			return
		}
		if !approved {
			state.PermissionDenied++
			recorder.Emit(ctx, "permission.denied", state.RunID, state.Step, "policy", map[string]any{
				"tool":   action.Tool,
				"reason": "user denied",
			})
			state.AddObservation(fmt.Sprintf("Tool %s denied by user.", action.Tool))
			return
		}
	}
	recorder.Emit(ctx, "permission.approved", state.RunID, state.Step, "policy", map[string]any{
		"tool": action.Tool,
	})

	start := time.Now()
	recorder.Emit(ctx, "tool.started", state.RunID, state.Step, "tool:"+action.Tool, map[string]any{
		"tool":  action.Tool,
		"input": compactToolInput(action.Input),
	})
	result, err := runToolWithTimeout(ctx, tool, spec, action.Input)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		state.ToolErrors++
		state.AddError("tool", err)
		recorder.Emit(ctx, "tool.failed", state.RunID, state.Step, "tool:"+action.Tool, map[string]any{
			"tool":       action.Tool,
			"error":      err.Error(),
			"latency_ms": latency,
		})
		state.AddObservation(fmt.Sprintf("Tool %s failed: %s", action.Tool, err.Error()))
		return
	}
	state.ResetErrors()
	state.ToolCalls++
	outputData, _ := json.MarshalIndent(result, "", "  ")
	outputRef, _ := writeArtifact(ctx, agent, recorder, state, fmt.Sprintf("tool_output_step_%d_%s.json", state.Step, action.Tool), outputData, "tool_output")
	recorder.Emit(ctx, "tool.completed", state.RunID, state.Step, "tool:"+action.Tool, map[string]any{
		"tool":       action.Tool,
		"input":      compactToolInput(action.Input),
		"output_ref": outputRef,
		"latency_ms": latency,
		"status":     "ok",
	})
	for _, artifact := range result.Artifacts {
		recorder.Emit(ctx, "artifact.created", state.RunID, state.Step, "tool:"+action.Tool, map[string]any{
			"name": artifact.Name,
			"path": artifact.Path,
			"type": artifact.Type,
		})
	}
	state.AddObservation(fmt.Sprintf("Tool %s completed: %s", action.Tool, result.Output))
}

func (r *Runtime) handleAskUser(ctx context.Context, recorder *trajectory.Recorder, state *RunState, action Action) {
	state.Status = StatusWaitingUser
	recorder.Emit(ctx, "user.input.requested", state.RunID, state.Step, "runtime", map[string]any{
		"question": action.Question,
	})
	fmt.Printf("? %s\n> ", action.Question)
	answer := r.readLine()
	if strings.TrimSpace(answer) == "/stop" {
		state.Status = StatusCancelled
		return
	}
	recorder.Emit(ctx, "user.input.received", state.RunID, state.Step, "user", map[string]any{
		"input": answer,
	})
	state.Messages = append(state.Messages, model.Message{Role: "user", Content: answer})
	state.Status = StatusRunning
}

func (r *Runtime) runEvaluation(ctx context.Context, agent *compiler.CompiledAgent, recorder *trajectory.Recorder, state *RunState) {
	recorder.Emit(ctx, "evaluation.started", state.RunID, state.Step, "evaluate", nil)
	result, err := evaluate.Run(ctx, state.RunID, agent.Evaluators, evaluate.Context{
		RunID:            state.RunID,
		Status:           string(state.Status),
		Final:            state.Final,
		Steps:            state.Step,
		ToolCalls:        state.ToolCalls,
		ModelErrors:      state.ModelErrors,
		ToolErrors:       state.ToolErrors,
		PermissionDenied: state.PermissionDenied,
		MaxSteps:         agent.Config.Runtime.Limits.MaxSteps,
		MaxToolCalls:     agent.Config.Runtime.Limits.MaxToolCalls,
	})
	if err != nil {
		recorder.Emit(ctx, "evaluation.failed", state.RunID, state.Step, "evaluate", map[string]any{
			"error": err.Error(),
		})
		return
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	if err := agent.RunStore.WriteEvaluation(state.RunID, data); err != nil {
		recorder.Emit(ctx, "evaluation.failed", state.RunID, state.Step, "evaluate", map[string]any{
			"error": err.Error(),
		})
		return
	}
	recorder.Emit(ctx, "evaluation.completed", state.RunID, state.Step, "evaluate", map[string]any{
		"passed": result.Passed,
		"score":  result.Score,
	})
}

func (r *Runtime) emitSkillEvents(ctx context.Context, recorder *trajectory.Recorder, state *RunState, agent *compiler.CompiledAgent) {
	all := agent.Skills.All()
	names := make([]string, 0, len(all))
	for _, skill := range all {
		names = append(names, skill.Manifest.Metadata.Name)
	}
	recorder.Emit(ctx, "skill.disclosed", state.RunID, 0, "skills", map[string]any{
		"count": len(names),
		"names": names,
	})
	for _, skill := range agent.Skills.Active() {
		recorder.Emit(ctx, "skill.loaded", state.RunID, 0, "skills", map[string]any{
			"name": skill.Manifest.Metadata.Name,
		})
	}
}

func runToolWithTimeout(ctx context.Context, tool tools.Tool, spec tools.Spec, input json.RawMessage) (tools.Result, error) {
	if spec.TimeoutSec <= 0 {
		return tool.Run(ctx, input)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(spec.TimeoutSec)*time.Second)
	defer cancel()
	return tool.Run(ctx, input)
}

func (r *Runtime) askApproval(tool string, risks []string) (approved bool, stop bool) {
	fmt.Printf("? Permission required  tool=%s risk=%v approve? [y/N] ", tool, risks)
	answer := strings.TrimSpace(strings.ToLower(r.readLine()))
	fmt.Println()
	switch answer {
	case "y", "yes", "approve", "/approve":
		return true, false
	case "/stop":
		return false, true
	default:
		return false, false
	}
}

func (r *Runtime) readLine() string {
	text, _ := r.input.ReadString('\n')
	return strings.TrimRight(text, "\r\n")
}

func writeArtifact(ctx context.Context, agent *compiler.CompiledAgent, recorder *trajectory.Recorder, state *RunState, name string, data []byte, typ string) (string, error) {
	ref, err := agent.RunStore.WriteArtifact(state.RunID, name, data)
	if err != nil {
		return "", err
	}
	recorder.Emit(ctx, "artifact.created", state.RunID, state.Step, "runtime", map[string]any{
		"name": name,
		"path": ref,
		"type": typ,
	})
	return ref, nil
}

func compactToolInput(input json.RawMessage) map[string]any {
	var raw map[string]any
	if err := json.Unmarshal(input, &raw); err != nil {
		return map[string]any{"raw": string(input)}
	}
	result := map[string]any{}
	for _, key := range []string{"path", "command", "query"} {
		if value, ok := raw[key]; ok {
			result[key] = value
		}
	}
	if len(result) == 0 {
		result["keys"] = keys(raw)
	}
	return result
}

func keys(values map[string]any) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	return out
}
