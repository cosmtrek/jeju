package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"jeju/internal/compiler"
	"jeju/internal/contextmgr"
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
	req, err := r.prepareModelRequest(ctx, agent, recorder, state, client, cfg)
	if err != nil {
		if contextmgr.IsOverflow(err) {
			state.Status = StatusFailed
		}
		return err
	}
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
	state.TokenCorrectionFactor = contextmgr.UpdateCorrectionFactor(state.TokenCorrectionFactor, state.LastRawTokenEstimate, resp.Usage.InputTokens)
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

func (r *Runtime) prepareModelRequest(ctx context.Context, agent *compiler.CompiledAgent, recorder *trajectory.Recorder, state *RunState, client model.Client, cfg model.ProviderConfig) (model.Request, error) {
	req := r.buildModelRequest(agent, state, cfg)
	contextOpts := contextmgr.Options{
		ContextWindow:    cfg.ContextWindow,
		MaxOutputTokens:  cfg.MaxOutputTokens,
		Threshold:        agent.Config.Runtime.CompressionThreshold,
		CorrectionFactor: state.TokenCorrectionFactor,
	}
	result, err := contextmgr.Prepare(req, state.Messages, state.Summary, contextOpts)
	compressionRequired := result.Report.ContextWindow > 0 && result.Report.BeforeTokens > result.Report.ThresholdTokens
	beforeRef := ""
	afterRef := ""
	if compressionRequired || result.Report.Compressed || err != nil {
		beforeReq := contextmgr.RequestWithSummary(req, state.Messages, state.Summary)
		beforeData, _ := json.MarshalIndent(beforeReq, "", "  ")
		beforeRef, _ = writeArtifact(ctx, agent, recorder, state, stepArtifactName(state.Step, "context_before", "", "json"), beforeData, "context_before")
		if result.PendingSummary == nil {
			afterData, _ := json.MarshalIndent(result.Request, "", "  ")
			afterRef, _ = writeArtifact(ctx, agent, recorder, state, stepArtifactName(state.Step, "context_after", "", "json"), afterData, "context_after")
		}
	}
	reportRef := ""
	writeReport := func() string {
		reportData, _ := json.MarshalIndent(result.Report, "", "  ")
		ref, _ := writeArtifact(ctx, agent, recorder, state, stepArtifactName(state.Step, "context_report", "", "json"), reportData, "context_report")
		return ref
	}
	if result.PendingSummary == nil {
		reportRef = writeReport()
	}
	recorder.Emit(ctx, trajectory.EventContextEstimated, state.RunID, state.Step, "context", map[string]any{
		"estimator":             result.Report.Estimator,
		"estimated_tokens":      result.Report.BeforeTokens,
		"raw_estimated_tokens":  result.Report.BeforeRawTokens,
		"threshold_tokens":      result.Report.ThresholdTokens,
		"context_window":        result.Report.ContextWindow,
		"max_output_tokens":     result.Report.MaxOutputTokens,
		"effective_input_limit": result.Report.EffectiveInputLimit,
		"compression_required":  compressionRequired,
		"correction_factor":     result.Report.CorrectionFactor,
		"report_ref":            reportRef,
		"before_ref":            beforeRef,
	})
	if compressionRequired {
		recorder.Emit(ctx, trajectory.EventContextCompressionStarted, state.RunID, state.Step, "context", map[string]any{
			"before_tokens":    result.Report.BeforeTokens,
			"threshold_tokens": result.Report.ThresholdTokens,
			"before_ref":       beforeRef,
		})
	}
	if err != nil {
		recorder.Emit(ctx, trajectory.EventContextCompressionFailed, state.RunID, state.Step, "context", map[string]any{
			"error":                  err.Error(),
			"before_tokens":          result.Report.BeforeTokens,
			"after_tokens":           result.Report.AfterTokens,
			"effective_input_limit":  result.Report.EffectiveInputLimit,
			"strategies":             result.Report.Strategies,
			"truncated_tool_results": result.Report.TruncatedToolResult,
			"before_ref":             beforeRef,
			"after_ref":              afterRef,
			"report_ref":             reportRef,
		})
		return model.Request{}, err
	}
	if result.PendingSummary != nil {
		summary, summaryErr := r.summarizeContext(ctx, agent, recorder, state, client, cfg, *result.PendingSummary)
		if summaryErr != nil {
			result = contextmgr.DegradeSummary(result, contextOpts)
		} else {
			result = contextmgr.CompleteSummary(result, summary, contextOpts)
		}
		afterData, _ := json.MarshalIndent(result.Request, "", "  ")
		afterRef, _ = writeArtifact(ctx, agent, recorder, state, stepArtifactName(state.Step, "context_after", "", "json"), afterData, "context_after")
		reportRef = writeReport()
	}
	if err := contextmgr.Overflow(result, contextOpts); err != nil {
		recorder.Emit(ctx, trajectory.EventContextCompressionFailed, state.RunID, state.Step, "context", map[string]any{
			"error":                  err.Error(),
			"before_tokens":          result.Report.BeforeTokens,
			"after_tokens":           result.Report.AfterTokens,
			"effective_input_limit":  result.Report.EffectiveInputLimit,
			"strategies":             result.Report.Strategies,
			"truncated_tool_results": result.Report.TruncatedToolResult,
			"before_ref":             beforeRef,
			"after_ref":              afterRef,
			"report_ref":             reportRef,
		})
		return model.Request{}, err
	}
	if result.Report.Compressed {
		if result.Summary != "" {
			summaryRef, _ := writeArtifact(ctx, agent, recorder, state, stepArtifactName(state.Step, "context_summary", "", "md"), []byte(result.Summary), "context_summary")
			recorder.Emit(ctx, trajectory.EventContextCompressionCompleted, state.RunID, state.Step, "context", map[string]any{
				"before_tokens":          result.Report.BeforeTokens,
				"after_tokens":           result.Report.AfterTokens,
				"strategies":             result.Report.Strategies,
				"preserved_blocks":       result.Report.PreservedBlocks,
				"truncated_tool_results": result.Report.TruncatedToolResult,
				"summary_ref":            summaryRef,
				"before_ref":             beforeRef,
				"after_ref":              afterRef,
				"report_ref":             reportRef,
			})
		} else {
			recorder.Emit(ctx, trajectory.EventContextCompressionCompleted, state.RunID, state.Step, "context", map[string]any{
				"before_tokens":          result.Report.BeforeTokens,
				"after_tokens":           result.Report.AfterTokens,
				"strategies":             result.Report.Strategies,
				"preserved_blocks":       result.Report.PreservedBlocks,
				"truncated_tool_results": result.Report.TruncatedToolResult,
				"before_ref":             beforeRef,
				"after_ref":              afterRef,
				"report_ref":             reportRef,
			})
		}
	}
	state.Messages = result.StateMessages
	state.Summary = result.Summary
	state.LastTokenEstimate = result.Report.AfterTokens
	state.LastRawTokenEstimate = result.Report.AfterRawTokens
	return result.Request, nil
}

func (r *Runtime) summarizeContext(ctx context.Context, agent *compiler.CompiledAgent, recorder *trajectory.Recorder, state *RunState, client model.Client, cfg model.ProviderConfig, summaryReq contextmgr.SummaryRequest) (string, error) {
	req := model.Request{
		Model:     cfg.Model,
		Messages:  summaryMessages(summaryReq, cfg),
		MaxTokens: summaryMaxTokens(cfg.MaxOutputTokens),
		ResponseFormat: &model.ResponseFormat{
			Type:   "jsonSchema",
			Name:   "context_summary",
			Strict: cfg.JSONSchemaMode,
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"summary": map[string]any{"type": "string"},
				},
				"required":             []string{"summary"},
				"additionalProperties": false,
			},
		},
		Metadata: map[string]any{
			"task": state.Input,
			"step": state.Step,
			"kind": "context_summary",
		},
	}
	rawTokens := contextmgr.EstimateRequestTokensRaw(req)
	effectiveLimit := cfg.ContextWindow - req.MaxTokens
	inputData, _ := json.MarshalIndent(req, "", "  ")
	inputRef, _ := writeArtifact(ctx, agent, recorder, state, stepArtifactName(state.Step, "context_summary_input", "", "json"), inputData, "context_summary_input")
	recorder.Emit(ctx, trajectory.EventContextSummaryStarted, state.RunID, state.Step, "model:"+cfg.Name, map[string]any{
		"provider":  cfg.Provider,
		"model":     cfg.Model,
		"input_ref": inputRef,
	})
	if cfg.ContextWindow > 0 && rawTokens > effectiveLimit {
		err := fmt.Errorf("context summary request estimate %d exceeds effective input limit %d", rawTokens, effectiveLimit)
		recorder.Emit(ctx, trajectory.EventContextSummaryFailed, state.RunID, state.Step, "model:"+cfg.Name, map[string]any{
			"error":     err.Error(),
			"input_ref": inputRef,
		})
		return "", err
	}
	resp, err := client.Generate(ctx, req)
	state.ModelCalls++
	if err != nil {
		state.ModelErrors++
		recorder.Emit(ctx, trajectory.EventContextSummaryFailed, state.RunID, state.Step, "model:"+cfg.Name, map[string]any{
			"error":     err.Error(),
			"input_ref": inputRef,
		})
		return "", err
	}
	outputData := []byte(resp.Text)
	outputRef, _ := writeArtifact(ctx, agent, recorder, state, stepArtifactName(state.Step, "context_summary_output", "", "txt"), outputData, "context_summary_output")
	summary, err := parseSummaryResponse(resp.Text)
	if err != nil {
		state.ModelErrors++
		recorder.Emit(ctx, trajectory.EventContextSummaryFailed, state.RunID, state.Step, "model:"+cfg.Name, map[string]any{
			"error":      err.Error(),
			"input_ref":  inputRef,
			"output_ref": outputRef,
		})
		return "", err
	}
	recorder.Emit(ctx, trajectory.EventContextSummaryCompleted, state.RunID, state.Step, "model:"+cfg.Name, map[string]any{
		"provider":     resp.Provider,
		"model":        resp.Model,
		"input_ref":    inputRef,
		"output_ref":   outputRef,
		"latency_ms":   resp.LatencyMS,
		"tokens_in":    resp.Usage.InputTokens,
		"tokens_out":   resp.Usage.OutputTokens,
		"tokens_total": resp.Usage.TotalTokens,
	})
	return summary, nil
}

func summaryMessages(req contextmgr.SummaryRequest, cfg model.ProviderConfig) []model.Message {
	var b strings.Builder
	b.WriteString("Update the conversation summary for an agent run.\n")
	b.WriteString("Return only a JSON object with a string field named summary.\n")
	b.WriteString("Preserve user requirements, decisions, tool findings, file paths, errors, and unresolved work. Do not invent facts.\n")
	if strings.TrimSpace(req.PreviousSummary) != "" {
		b.WriteString("\n# Previous Summary\n")
		b.WriteString(strings.TrimSpace(req.PreviousSummary))
		b.WriteString("\n")
	}
	b.WriteString("\n# Newly Evicted Messages\n")
	messageBudget := summaryMessageBudget(cfg)
	for i, message := range req.Messages {
		b.WriteString(fmt.Sprintf("\n## Message %d role=%s\n", i+1, message.Role))
		if message.ToolCallID != "" {
			b.WriteString("tool_call_id: ")
			b.WriteString(message.ToolCallID)
			b.WriteString("\n")
		}
		if len(message.ToolCalls) > 0 {
			calls, _ := json.Marshal(message.ToolCalls)
			b.WriteString("tool_calls: ")
			b.Write(calls)
			b.WriteString("\n")
		}
		if strings.TrimSpace(message.Content) != "" {
			b.WriteString(truncateSummaryContent(message.Content, messageBudget))
			b.WriteString("\n")
		}
	}
	return []model.Message{
		{Role: "system", Content: "You summarize context for Jeju, a local agent runtime."},
		{Role: "user", Content: b.String()},
	}
}

func summaryMaxTokens(maxOutputTokens int) int {
	if maxOutputTokens <= 0 || maxOutputTokens > 1024 {
		return 1024
	}
	return maxOutputTokens
}

func summaryMessageBudget(cfg model.ProviderConfig) int {
	limit := cfg.ContextWindow - summaryMaxTokens(cfg.MaxOutputTokens)
	if limit <= 0 {
		return 512
	}
	chars := limit * 3 / 8
	if chars < 512 {
		return 512
	}
	if chars > 6000 {
		return 6000
	}
	return chars
}

func truncateSummaryContent(text string, maxChars int) string {
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	head := maxChars * 2 / 3
	tail := maxChars / 3
	if head+tail >= len(runes) {
		return text
	}
	return string(runes[:head]) + fmt.Sprintf("\n[Jeju truncated summary input: omitted approximately %d characters]\n", len(runes)-head-tail) + string(runes[len(runes)-tail:])
}

func parseSummaryResponse(text string) (string, error) {
	var envelope struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		return "", fmt.Errorf("context summary response is not valid JSON: %w", err)
	}
	summary := strings.TrimSpace(envelope.Summary)
	if summary == "" {
		return "", fmt.Errorf("context summary response missing non-empty summary")
	}
	return summary, nil
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
	approvalReason := ""
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
		if r.autoApprove {
			approvalReason = "auto_approved_by_evolve"
		} else {
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
	}
	approvalPayload := map[string]any{
		"tool": action.Tool,
	}
	if approvalReason != "" {
		approvalPayload["reason"] = approvalReason
	}
	recorder.Emit(ctx, trajectory.EventPermissionApproved, state.RunID, state.Step, "policy", approvalPayload)

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
	if r.autoUserInput != nil {
		answer := *r.autoUserInput
		recorder.Emit(ctx, trajectory.EventUserInputReceived, state.RunID, state.Step, "user", map[string]any{
			"input":  answer,
			"reason": "auto_answered_by_evolve",
		})
		state.Messages = append(state.Messages, model.Message{Role: "user", Content: answer})
		state.Status = StatusRunning
		return
	}
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
