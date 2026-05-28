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
	recorder, err := trajectory.NewRecorder(runDir.Path)
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
		ConfigSnapshot: runs.ConfigSnapshotFile,
		Trajectory:     runs.TrajectoryFile,
		Final:          runs.FinalFile,
	}
	if agent.Config.Evaluate.Enabled {
		metadata.Evaluation = runs.EvaluationFile
	}
	if err := agent.RunStore.WriteMetadata(runDir.RunID, metadata); err != nil {
		return nil, err
	}

	recorder.Emit(ctx, trajectory.EventRunStarted, state.RunID, 0, "runtime", map[string]any{
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
		modelName := agent.Config.Runtime.Model
		recorder.Emit(ctx, trajectory.EventStepStarted, state.RunID, state.Step, "runtime", map[string]any{
			"model": modelName,
		})

		if err := r.runStep(ctx, agent, recorder, state, modelName); err != nil {
			state.AddError("runtime", err)
			if state.ConsecutiveErrors >= agent.Config.Runtime.Limits.MaxConsecutiveErrors {
				state.Status = StatusFailed
			}
		}
		recorder.Emit(ctx, trajectory.EventStepCompleted, state.RunID, state.Step, "runtime", map[string]any{
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

	if agent.Config.Evaluate.Enabled {
		r.runEvaluation(ctx, agent, recorder, state)
	}

	now := time.Now()
	state.EndedAt = &now
	eventType := trajectory.EventRunCompleted
	if state.Status == StatusFailed {
		eventType = trajectory.EventRunFailed
	}
	if state.Status == StatusCancelled {
		eventType = trajectory.EventRunCancelled
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
	inputRef, _ := writeArtifact(ctx, agent, recorder, state, stepArtifactName(state.Step, "model_input", "", "json"), inputData, "model_input")
	recorder.Emit(ctx, trajectory.EventModelStarted, state.RunID, state.Step, "model:"+modelName, map[string]any{
		"provider":  cfg.Provider,
		"model":     cfg.Model,
		"input_ref": inputRef,
	})

	resp, err := client.Generate(ctx, req)
	state.ModelCalls++
	if err != nil {
		state.ModelErrors++
		recorder.Emit(ctx, trajectory.EventModelFailed, state.RunID, state.Step, "model:"+modelName, map[string]any{
			"error": err.Error(),
		})
		return err
	}
	state.ResetErrors()
	reasoningRef := ""
	if resp.ReasoningContent != "" {
		reasoningRef, _ = writeArtifact(ctx, agent, recorder, state, stepArtifactName(state.Step, "model_reasoning", "", "txt"), []byte(resp.ReasoningContent), "model_reasoning")
	}
	outputData := []byte(resp.Text)
	outputExt := "txt"
	if len(outputData) == 0 && len(resp.ToolCalls) > 0 {
		outputData, _ = json.MarshalIndent(resp.ToolCalls, "", "  ")
		outputExt = "json"
	}
	outputRef, _ := writeArtifact(ctx, agent, recorder, state, stepArtifactName(state.Step, "model_output", "", outputExt), outputData, "model_output")
	recorder.Emit(ctx, trajectory.EventModelCompleted, state.RunID, state.Step, "model:"+modelName, map[string]any{
		"provider":          resp.Provider,
		"model":             resp.Model,
		"input_ref":         inputRef,
		"output_ref":        outputRef,
		"latency_ms":        resp.LatencyMS,
		"tokens_in":         resp.Usage.InputTokens,
		"tokens_out":        resp.Usage.OutputTokens,
		"tokens_total":      resp.Usage.TotalTokens,
		"reasoning_ref":     reasoningRef,
		"reasoning_preview": reasoningPreview(resp.ReasoningContent),
	})
	if cfg.ToolCalling {
		return r.handleNativeModelResponse(ctx, agent, recorder, state, resp)
	}
	state.Messages = append(state.Messages, model.Message{
		Role:             "assistant",
		Content:          resp.Text,
		ReasoningContent: resp.ReasoningContent,
	})

	action, err := ParseAction(resp.Text)
	if err != nil {
		state.AddError("action_parse", err)
		recorder.Emit(ctx, trajectory.EventActionParseFailed, state.RunID, state.Step, "runtime", map[string]any{
			"error": err.Error(),
		})
		state.AddObservation("Invalid action JSON. Return only a valid Jeju action JSON object.")
		return nil
	}
	recorder.Emit(ctx, trajectory.EventActionParsed, state.RunID, state.Step, "runtime", map[string]any{
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
	messages := agent.PromptMessages(cfg.ToolCalling)
	var requestTools []model.ToolDefinition
	var responseFormat *model.ResponseFormat
	if cfg.ToolCalling {
		requestTools = nativeToolDefinitions(agent.Tools.Specs(), cfg)
		if len(requestTools) == 0 {
			responseFormat = finalResponseFormat(cfg)
		}
	}
	messages = append(messages, state.Messages...)
	return model.Request{
		Model:          cfg.Model,
		Messages:       messages,
		Temperature:    cfg.Temperature,
		MaxTokens:      cfg.MaxOutputTokens,
		Tools:          requestTools,
		ResponseFormat: responseFormat,
		Metadata: map[string]any{
			"task":         state.Input,
			"step":         state.Step,
			"observations": strings.Join(state.Observations, "\n"),
		},
	}
}

func nativeToolDefinitions(specs []tools.Spec, cfg model.ProviderConfig) []model.ToolDefinition {
	defs := make([]model.ToolDefinition, 0, len(specs)+2)
	for _, spec := range specs {
		defs = append(defs, model.ToolDefinition{
			Name:        spec.Name,
			Description: spec.Description,
			Parameters:  spec.InputSchema,
			Strict:      false,
		})
	}
	defs = append(defs, model.ToolDefinition{
		Name:        "ask_user",
		Description: "Ask the user for required missing information before continuing.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question": map[string]any{"type": "string"},
			},
			"required":             []string{"question"},
			"additionalProperties": false,
		},
		Strict: cfg.JSONSchemaMode,
	})
	defs = append(defs, model.ToolDefinition{
		Name:        "final_answer",
		Description: "Finish the run with the final answer after required tool work is complete.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{"type": "string"},
			},
			"required":             []string{"content"},
			"additionalProperties": false,
		},
		Strict: cfg.JSONSchemaMode,
	})
	return defs
}

func finalResponseFormat(cfg model.ProviderConfig) *model.ResponseFormat {
	return &model.ResponseFormat{
		Type:   "jsonSchema",
		Name:   "jeju_final_response",
		Strict: cfg.JSONSchemaMode,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"thought": map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
			"required":             []string{"content"},
			"additionalProperties": false,
		},
	}
}

func (r *Runtime) handleNativeModelResponse(ctx context.Context, agent *compiler.CompiledAgent, recorder *trajectory.Recorder, state *RunState, resp model.Response) error {
	if len(resp.ToolCalls) > 0 {
		state.Messages = append(state.Messages, model.Message{
			Role:             "assistant",
			Content:          resp.Text,
			ReasoningContent: resp.ReasoningContent,
			ToolCalls:        resp.ToolCalls,
		})
		if len(resp.ToolCalls) == 1 && resp.ToolCalls[0].Name == "ask_user" {
			call := resp.ToolCalls[0]
			action := Action{Type: ActionAskUser}
			var input struct {
				Question string `json:"question"`
			}
			if err := json.Unmarshal(call.Arguments, &input); err != nil {
				state.AddError("action_parse", err)
				recorder.Emit(ctx, trajectory.EventActionParseFailed, state.RunID, state.Step, "runtime", map[string]any{
					"error": err.Error(),
				})
				state.AddObservation("Invalid ask_user tool arguments.")
				return nil
			}
			action.Question = input.Question
			state.Messages[len(state.Messages)-1] = model.Message{Role: "assistant", Content: input.Question, ReasoningContent: resp.ReasoningContent}
			recorder.Emit(ctx, trajectory.EventActionParsed, state.RunID, state.Step, "runtime", map[string]any{
				"type": action.Type,
				"tool": "ask_user",
			})
			r.handleAskUser(ctx, recorder, state, action)
			return nil
		}
		if len(resp.ToolCalls) == 1 && resp.ToolCalls[0].Name == "final_answer" {
			call := resp.ToolCalls[0]
			var input struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal(call.Arguments, &input); err != nil {
				state.AddError("action_parse", err)
				recorder.Emit(ctx, trajectory.EventActionParseFailed, state.RunID, state.Step, "runtime", map[string]any{
					"error": err.Error(),
				})
				state.AddObservation("Invalid final_answer tool arguments.")
				return nil
			}
			if strings.TrimSpace(input.Content) == "" {
				err := fmt.Errorf("final_answer missing content")
				state.AddError("action_parse", err)
				recorder.Emit(ctx, trajectory.EventActionParseFailed, state.RunID, state.Step, "runtime", map[string]any{
					"error": err.Error(),
				})
				state.AddObservation("final_answer requires a non-empty content string.")
				return nil
			}
			state.Messages[len(state.Messages)-1] = model.Message{Role: "assistant", Content: input.Content, ReasoningContent: resp.ReasoningContent}
			recorder.Emit(ctx, trajectory.EventActionParsed, state.RunID, state.Step, "runtime", map[string]any{
				"type": ActionFinal,
				"tool": "final_answer",
			})
			state.Final = input.Content
			state.Status = StatusCompleted
			return nil
		}

		for _, call := range resp.ToolCalls {
			if call.Name == "ask_user" || call.Name == "final_answer" {
				err := fmt.Errorf("control tool %q must be the only native tool call", call.Name)
				state.AddError("action_parse", err)
				recorder.Emit(ctx, trajectory.EventActionParseFailed, state.RunID, state.Step, "runtime", map[string]any{
					"error": err.Error(),
				})
				state.AddObservation("Return ask_user or final_answer as a single function tool call.")
				return nil
			}
		}

		for _, call := range resp.ToolCalls {
			action := Action{
				Type:       ActionToolCall,
				Tool:       call.Name,
				Input:      call.Arguments,
				ToolCallID: call.ID,
			}
			if len(action.Input) == 0 {
				action.Input = json.RawMessage(`{}`)
			}
			recorder.Emit(ctx, trajectory.EventActionParsed, state.RunID, state.Step, "runtime", map[string]any{
				"type": action.Type,
				"tool": action.Tool,
			})
			messageCount := len(state.Messages)
			r.handleToolCall(ctx, agent, recorder, state, action)
			if len(state.Messages) > messageCount {
				state.Messages = state.Messages[:messageCount]
			}
			state.Messages = append(state.Messages, model.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    lastObservation(state),
			})
			if state.IsTerminal() {
				return nil
			}
		}
		return nil
	}

	state.Messages = append(state.Messages, model.Message{Role: "assistant", Content: resp.Text, ReasoningContent: resp.ReasoningContent})
	content, err := parseFinalContent(resp.Text)
	if err != nil {
		state.AddError("action_parse", err)
		recorder.Emit(ctx, trajectory.EventActionParseFailed, state.RunID, state.Step, "runtime", map[string]any{
			"error": err.Error(),
		})
		state.AddObservation("Final response must be a JSON object with a non-empty content string. Use function tools for tool calls; do not simulate tool calls in text.")
		return nil
	}
	if strings.TrimSpace(content) == "" {
		err := fmt.Errorf("native model response missing final content")
		state.AddError("action_parse", err)
		recorder.Emit(ctx, trajectory.EventActionParseFailed, state.RunID, state.Step, "runtime", map[string]any{
			"error": err.Error(),
		})
		state.AddObservation("Return a final response with a non-empty content field.")
		return nil
	}
	recorder.Emit(ctx, trajectory.EventActionParsed, state.RunID, state.Step, "runtime", map[string]any{
		"type": ActionFinal,
	})
	state.Final = content
	state.Status = StatusCompleted
	return nil
}

func parseFinalContent(text string) (string, error) {
	var envelope struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(text), &envelope); err == nil && envelope.Content != "" {
		return envelope.Content, nil
	}
	return "", fmt.Errorf("native final response is not valid structured JSON")
}

func lastObservation(state *RunState) string {
	if len(state.Observations) == 0 {
		return ""
	}
	return state.Observations[len(state.Observations)-1]
}

func reasoningPreview(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if len(text) <= 320 {
		return text
	}
	return text[:320] + "..."
}

func (r *Runtime) handleToolCall(ctx context.Context, agent *compiler.CompiledAgent, recorder *trajectory.Recorder, state *RunState, action Action) {
	recorder.Emit(ctx, trajectory.EventToolRequested, state.RunID, state.Step, "model", map[string]any{
		"tool":  action.Tool,
		"input": compactToolInput(action.Input),
	})
	tool, ok := agent.Tools.Get(action.Tool)
	if !ok {
		err := fmt.Errorf("unknown tool %q", action.Tool)
		state.ToolErrors++
		state.AddError("tool", err)
		recorder.Emit(ctx, trajectory.EventToolFailed, state.RunID, state.Step, "tool:"+action.Tool, map[string]any{
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
	}
	decision := agent.Policy.Check(req, spec)
	recorder.Emit(ctx, trajectory.EventPermissionChecked, state.RunID, state.Step, "policy", map[string]any{
		"tool":         action.Tool,
		"capabilities": spec.Capabilities,
		"decision":     decision.Action,
		"reason":       decision.Reason,
	})
	if decision.Action == policy.DecisionDeny {
		state.PermissionDenied++
		recorder.Emit(ctx, trajectory.EventPermissionDenied, state.RunID, state.Step, "policy", map[string]any{
			"tool":   action.Tool,
			"reason": decision.Reason,
		})
		state.AddObservation(fmt.Sprintf("Tool %s denied by policy: %s", action.Tool, decision.Reason))
		return
	}
	if decision.Action == policy.DecisionAsk {
		approved, stop := r.askApproval(action.Tool, spec.Capabilities)
		if stop {
			state.Status = StatusCancelled
			state.AddObservation("User stopped the run.")
			return
		}
		if !approved {
			state.PermissionDenied++
			recorder.Emit(ctx, trajectory.EventPermissionDenied, state.RunID, state.Step, "policy", map[string]any{
				"tool":   action.Tool,
				"reason": "user denied",
			})
			state.AddObservation(fmt.Sprintf("Tool %s denied by user.", action.Tool))
			return
		}
	}
	recorder.Emit(ctx, trajectory.EventPermissionApproved, state.RunID, state.Step, "policy", map[string]any{
		"tool": action.Tool,
	})

	start := time.Now()
	recorder.Emit(ctx, trajectory.EventToolStarted, state.RunID, state.Step, "tool:"+action.Tool, map[string]any{
		"tool":  action.Tool,
		"input": compactToolInput(action.Input),
	})
	result, err := runToolWithTimeout(ctx, tool, spec, action.Input)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		state.ToolErrors++
		state.AddError("tool", err)
		recorder.Emit(ctx, trajectory.EventToolFailed, state.RunID, state.Step, "tool:"+action.Tool, map[string]any{
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
	artifactSuffix := action.Tool
	if action.ToolCallID != "" {
		artifactSuffix += "_" + sanitizeArtifactSuffix(action.ToolCallID)
	}
	outputRef, _ := writeArtifact(ctx, agent, recorder, state, stepArtifactName(state.Step, "tool_output", artifactSuffix, "json"), outputData, "tool_output")
	recorder.Emit(ctx, trajectory.EventToolCompleted, state.RunID, state.Step, "tool:"+action.Tool, map[string]any{
		"tool":       action.Tool,
		"input":      compactToolInput(action.Input),
		"output_ref": outputRef,
		"latency_ms": latency,
		"status":     "ok",
	})
	for _, artifact := range result.Artifacts {
		recorder.Emit(ctx, trajectory.EventArtifactCreated, state.RunID, state.Step, "tool:"+action.Tool, map[string]any{
			"name": artifact.Name,
			"path": artifact.Path,
			"type": artifact.Type,
		})
	}
	state.AddObservation(fmt.Sprintf("Tool %s completed: %s", action.Tool, result.Output))
}

func (r *Runtime) handleAskUser(ctx context.Context, recorder *trajectory.Recorder, state *RunState, action Action) {
	state.Status = StatusWaitingUser
	recorder.Emit(ctx, trajectory.EventUserInputRequested, state.RunID, state.Step, "runtime", map[string]any{
		"question": action.Question,
	})
	fmt.Printf("? %s\n> ", action.Question)
	answer := r.readLine()
	if strings.TrimSpace(answer) == "/stop" {
		state.Status = StatusCancelled
		return
	}
	recorder.Emit(ctx, trajectory.EventUserInputReceived, state.RunID, state.Step, "user", map[string]any{
		"input": answer,
	})
	state.Messages = append(state.Messages, model.Message{Role: "user", Content: answer})
	state.Status = StatusRunning
}

func (r *Runtime) runEvaluation(ctx context.Context, agent *compiler.CompiledAgent, recorder *trajectory.Recorder, state *RunState) {
	recorder.Emit(ctx, trajectory.EventEvaluationStarted, state.RunID, state.Step, "evaluate", nil)
	result, err := evaluate.Run(ctx, state.RunID, agent.Evaluators, evaluate.Context{
		RunID:            state.RunID,
		Input:            state.Input,
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
		recorder.Emit(ctx, trajectory.EventEvaluationFailed, state.RunID, state.Step, "evaluate", map[string]any{
			"error": err.Error(),
		})
		return
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	if err := agent.RunStore.WriteEvaluation(state.RunID, data); err != nil {
		recorder.Emit(ctx, trajectory.EventEvaluationFailed, state.RunID, state.Step, "evaluate", map[string]any{
			"error": err.Error(),
		})
		return
	}
	recorder.Emit(ctx, trajectory.EventEvaluationCompleted, state.RunID, state.Step, "evaluate", map[string]any{
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
	recorder.Emit(ctx, trajectory.EventSkillDisclosed, state.RunID, 0, "skills", map[string]any{
		"count": len(names),
		"names": names,
	})
	for _, skill := range agent.Skills.Active() {
		recorder.Emit(ctx, trajectory.EventSkillLoaded, state.RunID, 0, "skills", map[string]any{
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

func (r *Runtime) askApproval(tool string, capabilities []string) (approved bool, stop bool) {
	fmt.Printf("? Permission required  tool=%s capabilities=%v approve? [y/N] ", tool, capabilities)
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
	recorder.Emit(ctx, trajectory.EventArtifactCreated, state.RunID, state.Step, "runtime", map[string]any{
		"name": name,
		"path": ref,
		"type": typ,
	})
	return ref, nil
}

func stepArtifactName(step int, typ string, suffix string, ext string) string {
	name := fmt.Sprintf("step%03d_%s", step, typ)
	if suffix != "" {
		name += "_" + suffix
	}
	if ext != "" {
		name += "." + ext
	}
	return name
}

func sanitizeArtifactSuffix(text string) string {
	var b strings.Builder
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	if b.Len() == 0 {
		return "call"
	}
	return b.String()
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
