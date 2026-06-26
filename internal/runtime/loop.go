package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/cosmtrek/jeju/internal/compiler"
	"github.com/cosmtrek/jeju/internal/contextmgr"
	"github.com/cosmtrek/jeju/internal/evaluate"
	"github.com/cosmtrek/jeju/internal/jsonschemautil"
	"github.com/cosmtrek/jeju/internal/model"
	"github.com/cosmtrek/jeju/internal/policy"
	"github.com/cosmtrek/jeju/internal/runs"
	"github.com/cosmtrek/jeju/internal/tools"
	"github.com/cosmtrek/jeju/internal/trajectory"
)

func (r *Runtime) Run(ctx context.Context, agent *compiler.CompiledAgent, input string) (*RunResult, error) {
	runDir, err := agent.RunStore.CreateRun(agent.Name, input)
	if err != nil {
		return nil, err
	}
	state := NewRunState(runDir.RunID, agent.Name, input)
	return r.runWithInitialState(ctx, agent, runDir, state, input)
}

func (r *Runtime) RunWithMessages(ctx context.Context, agent *compiler.CompiledAgent, input string, prefixMessages []model.Message, messages []model.Message) (*RunResult, error) {
	runDir, err := agent.RunStore.CreateRun(agent.Name, input)
	if err != nil {
		return nil, err
	}
	state := NewRunStateWithMessages(runDir.RunID, agent.Name, input, prefixMessages, messages)
	return r.runWithInitialState(ctx, agent, runDir, state, input)
}

func (r *Runtime) runWithInitialState(ctx context.Context, agent *compiler.CompiledAgent, runDir *runs.RunDir, state *RunState, input string) (*RunResult, error) {
	state.RunDir = runDir.Path
	recorder, err := trajectory.NewRecorderWithOptions(runDir.Path, trajectory.RecorderOptions{
		Console: !r.suppressConsoleTrajectory,
	})
	if err != nil {
		return nil, err
	}
	defer recorder.Close()

	agentPayload := map[string]any{"name": agent.Name}
	if len(agent.PackageProvenance) > 0 {
		agentPayload["package"] = agent.PackageProvenance
	}
	recorder.Emit(ctx, trajectory.EventTrajectoryHeader, state.RunID, 0, "runtime", map[string]any{
		"agent": agentPayload,
		"input": input,
	})
	writeArtifact(ctx, recorder, state, "config_snapshot", "", []byte(agent.ConfigSnapshot), "config_snapshot", "application/x-yaml")
	if len(agent.PackageProvenance) > 0 {
		data, _ := json.MarshalIndent(agent.PackageProvenance, "", "  ")
		writeArtifact(ctx, recorder, state, "package_provenance", "", data, "package_provenance", "application/json")
	}
	recorder.EmitSpanStarted(ctx, state.RunID, 0, runSpanID(), "", "runtime", trajectory.SpanRun, agent.Name, nil)
	r.emitSkillEvents(ctx, recorder, state, agent)

	for !state.IsTerminal() {
		if err := shouldStop(state, agent.Config.Runtime.Limits); err != nil {
			state.AddError("stop", err)
			state.Status = StatusFailed
			break
		}
		state.Step++
		modelName := agent.Config.Runtime.Model
		recorder.EmitSpanStarted(ctx, state.RunID, state.Step, stepSpanID(state.Step), runSpanID(), "runtime", trajectory.SpanStep, fmt.Sprintf("step %d", state.Step), map[string]any{
			"model": modelName,
		})

		if err := r.runStep(ctx, agent, recorder, state, modelName); err != nil {
			state.AddError("runtime", err)
			if state.ConsecutiveErrors >= agent.Config.Runtime.Limits.MaxConsecutiveErrors {
				state.Status = StatusFailed
			}
		}
		recorder.EmitSpanEnded(ctx, state.RunID, state.Step, stepSpanID(state.Step), runSpanID(), "runtime", trajectory.SpanStep, spanStatusForRun(state.Status), nil)
	}

	if state.Status == StatusRunning {
		state.Status = StatusFailed
	}
	if state.Final == "" && state.Status == StatusFailed {
		state.Final = "Run failed before producing a final answer."
	}
	finalRef := state.FinalRef
	if finalRef == "" {
		finalRef = writeArtifact(ctx, recorder, state, "final", "", []byte(state.Final), "final", "text/markdown")
		state.FinalRef = finalRef
	}

	var evalResult *evaluate.Result
	if agent.Config.Evaluate.Enabled {
		evalResult = r.runEvaluation(ctx, agent, recorder, state)
	}

	now := time.Now()
	state.EndedAt = &now
	recorder.EmitSpanEnded(ctx, state.RunID, 0, runSpanID(), "", "runtime", trajectory.SpanRun, spanStatusForRun(state.Status), nil)
	summary := map[string]any{
		"status":      string(state.Status),
		"started_at":  state.StartedAt.Format(time.RFC3339Nano),
		"ended_at":    now.Format(time.RFC3339Nano),
		"duration_ms": now.Sub(state.StartedAt).Milliseconds(),
		"final":       map[string]any{"content_ref": finalRef},
		"stats":       runStatsPayload(state),
	}
	if evalResult != nil {
		summary["evaluation"] = map[string]any{"passed": evalResult.Passed, "score": evalResult.Score}
	}
	recorder.Emit(ctx, trajectory.EventRunSummary, state.RunID, 0, "runtime", summary)
	return &RunResult{RunID: state.RunID, Status: state.Status, Final: state.Final}, nil
}

func (r *Runtime) runStep(ctx context.Context, agent *compiler.CompiledAgent, recorder *trajectory.Recorder, state *RunState, modelName string) error {
	client, cfg, ok := agent.Models.Get(modelName)
	if !ok {
		state.FinalValidationRetryPending = false
		return fmt.Errorf("model %q is not compiled", modelName)
	}
	req, err := r.prepareModelRequest(ctx, agent, recorder, state, client, cfg)
	if err != nil {
		state.FinalValidationRetryPending = false
		if contextmgr.IsOverflow(err) {
			state.Status = StatusFailed
		}
		return err
	}
	state.FinalValidationRetryPending = false
	inputData, _ := json.MarshalIndent(req, "", "  ")
	inputRef := writeArtifact(ctx, recorder, state, "model_input", "", inputData, "model_input", "application/json")
	modelSpanID := llmSpanID(state.Step, "primary")
	recorder.EmitSpanStarted(ctx, state.RunID, state.Step, modelSpanID, stepSpanID(state.Step), "model:"+modelName, trajectory.SpanLLM, modelName, map[string]any{
		"input": map[string]any{"content_ref": inputRef},
		"attrs": map[string]any{
			"provider": cfg.Provider,
			"model":    cfg.Model,
		},
	})

	resp, err := client.Generate(ctx, req)
	state.ModelCalls++
	if err != nil {
		state.ModelErrors++
		recorder.EmitSpanEnded(ctx, state.RunID, state.Step, modelSpanID, stepSpanID(state.Step), "model:"+modelName, trajectory.SpanLLM, trajectory.SpanStatusError, map[string]any{
			"input": map[string]any{"content_ref": inputRef},
			"error": map[string]any{"message": err.Error()},
		})
		return err
	}
	state.TokenCorrectionFactor = contextmgr.UpdateCorrectionFactor(state.TokenCorrectionFactor, state.LastRawTokenEstimate, resp.Usage.InputTokens)
	state.RecordUsage(resp.Usage)
	state.ResetErrors()
	reasoningRef := ""
	if resp.ReasoningContent != "" {
		reasoningRef = writeArtifact(ctx, recorder, state, "model_reasoning", "", []byte(resp.ReasoningContent), "model_reasoning", "text/plain")
	}
	outputData := []byte(resp.Text)
	outputExt := "txt"
	if len(outputData) == 0 && len(resp.ToolCalls) > 0 {
		outputData, _ = json.MarshalIndent(resp.ToolCalls, "", "  ")
		outputExt = "json"
	}
	outputMediaType := "text/plain"
	if outputExt == "json" {
		outputMediaType = "application/json"
	}
	outputRef := writeArtifact(ctx, recorder, state, "model_output", "", outputData, "model_output", outputMediaType)
	modelEnd := map[string]any{
		"input":  map[string]any{"content_ref": inputRef},
		"output": map[string]any{"content_ref": outputRef},
		"attrs": map[string]any{
			"provider": resp.Provider,
			"model":    resp.Model,
		},
		"metrics": map[string]any{
			"latency_ms":              resp.LatencyMS,
			"prompt_tokens":           resp.Usage.InputTokens,
			"completion_tokens":       resp.Usage.OutputTokens,
			"total_tokens":            resp.Usage.TotalTokens,
			"prompt_cache_hit_tokens": resp.Usage.CacheHitTokens,
		},
	}
	if reasoningRef != "" {
		modelEnd["reasoning"] = map[string]any{
			"content_ref": reasoningRef,
			"preview":     reasoningPreview(resp.ReasoningContent),
		}
	}
	recorder.EmitSpanEnded(ctx, state.RunID, state.Step, modelSpanID, stepSpanID(state.Step), "model:"+modelName, trajectory.SpanLLM, trajectory.SpanStatusOK, modelEnd)
	recorder.Emit(ctx, trajectory.EventMessageCreated, state.RunID, state.Step, "runtime", map[string]any{
		"message_id":    fmt.Sprintf("msg_assistant_%03d", state.Step),
		"role":          "assistant",
		"source":        "agent",
		"content":       []map[string]any{{"type": "text", "text": resp.Text}},
		"reasoning_ref": reasoningRef,
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
		recorder.Emit(ctx, trajectory.EventActionCreated, state.RunID, state.Step, "runtime", map[string]any{
			"action_id": fmt.Sprintf("act_%03d_parse_failed", state.Step),
			"kind":      "parse_failed",
			"error":     map[string]any{"message": err.Error()},
		})
		state.AddObservation("Invalid action JSON. Return only a valid Jeju action JSON object.")
		return nil
	}
	if action.Type == ActionToolCall && action.ToolCallID == "" {
		action.ToolCallID = fmt.Sprintf("call_%03d_%s", state.Step, sanitizeArtifactSuffix(action.Tool))
	}
	if action.Type != ActionFinal {
		emitAction(ctx, recorder, state, action)
	}

	switch action.Type {
	case ActionFinal:
		r.handleFinal(ctx, agent, recorder, state, action.Content, action.Thought)
	case ActionToolCall:
		r.handleToolCall(ctx, agent, recorder, state, action)
	case ActionAskUser:
		r.handleAskUser(ctx, recorder, state, action)
	}
	return nil
}

func (r *Runtime) buildModelRequest(agent *compiler.CompiledAgent, state *RunState, cfg model.ProviderConfig) model.Request {
	messages := agent.PromptMessages(cfg.ToolCalling)
	if state.PrefixMessages != nil {
		messages = append([]model.Message(nil), state.PrefixMessages...)
	}
	var requestTools []model.ToolDefinition
	var responseFormat *model.ResponseFormat
	toolBudgetExhausted := agent.Config.Runtime.Limits.MaxToolCalls > 0 && state.ToolCalls >= agent.Config.Runtime.Limits.MaxToolCalls
	if cfg.ToolCalling && !toolBudgetExhausted && !state.FinalValidationRetryPending {
		requestTools = nativeToolDefinitions(agent.Tools.Specs())
	}
	if agent.Output.CompiledSchema != nil && cfg.JSONSchemaMode {
		responseFormat = &model.ResponseFormat{
			Type:   "jsonSchema",
			Name:   agent.Output.Name,
			Schema: agent.Output.Schema,
			Strict: true,
		}
	} else if agent.Output.CompiledSchema != nil && cfg.JSONMode && len(requestTools) == 0 {
		responseFormat = &model.ResponseFormat{Type: "jsonObject"}
	} else if toolBudgetExhausted && cfg.JSONMode {
		responseFormat = &model.ResponseFormat{Type: "jsonObject"}
	}
	if toolBudgetExhausted && !state.ToolBudgetFinalTried {
		state.Messages = append(state.Messages, model.Message{
			Role: "user",
			Content: fmt.Sprintf(
				"Tool budget exhausted after %d completed tool calls. Do not call tools. Return the best final answer now using the available evidence.",
				state.ToolCalls,
			),
		})
		state.ToolBudgetFinalTried = true
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
		ContextWindow:     cfg.ContextWindow,
		MaxOutputTokens:   cfg.MaxOutputTokens,
		Threshold:         agent.Config.Runtime.CompressionThreshold,
		RecentTokenBudget: agent.Config.Runtime.RecentTokenBudget,
		CorrectionFactor:  state.TokenCorrectionFactor,
	}
	result, err := contextmgr.Prepare(req, state.Messages, state.Summary, contextOpts)
	compressionRequired := result.Report.ContextWindow > 0 && result.Report.BeforeTokens > result.Report.ThresholdTokens
	beforeRef := ""
	afterRef := ""
	if compressionRequired || result.Report.Compressed || err != nil {
		beforeReq := contextmgr.RequestWithSummary(req, state.Messages, state.Summary)
		beforeData, _ := json.MarshalIndent(beforeReq, "", "  ")
		beforeRef = writeArtifact(ctx, recorder, state, "context_before", "", beforeData, "context_before", "application/json")
		if result.PendingSummary == nil {
			afterData, _ := json.MarshalIndent(result.Request, "", "  ")
			afterRef = writeArtifact(ctx, recorder, state, "context_after", "", afterData, "context_after", "application/json")
		}
	}
	reportRef := ""
	writeReport := func() string {
		reportData, _ := json.MarshalIndent(result.Report, "", "  ")
		ref := writeArtifact(ctx, recorder, state, "context_report", "", reportData, "context_report", "application/json")
		return ref
	}
	if result.PendingSummary == nil {
		reportRef = writeReport()
	}
	contextSpan := contextSpanID(state.Step, "estimate")
	recorder.EmitSpanStarted(ctx, state.RunID, state.Step, contextSpan, stepSpanID(state.Step), "context", trajectory.SpanContext, "context estimate", map[string]any{
		"operation": "estimate",
	})
	recorder.EmitSpanEnded(ctx, state.RunID, state.Step, contextSpan, stepSpanID(state.Step), "context", trajectory.SpanContext, trajectory.SpanStatusOK, map[string]any{
		"operation": "estimate",
		"metrics": map[string]any{
			"estimated_tokens":      result.Report.BeforeTokens,
			"raw_estimated_tokens":  result.Report.BeforeRawTokens,
			"threshold_tokens":      result.Report.ThresholdTokens,
			"recent_token_budget":   result.Report.RecentTokenBudget,
			"context_window":        result.Report.ContextWindow,
			"max_output_tokens":     result.Report.MaxOutputTokens,
			"effective_input_limit": result.Report.EffectiveInputLimit,
		},
		"attrs": map[string]any{
			"estimator":            result.Report.Estimator,
			"compression_required": compressionRequired,
			"correction_factor":    result.Report.CorrectionFactor,
			"report_ref":           reportRef,
			"before_ref":           beforeRef,
		},
	})
	if compressionRequired {
		recorder.EmitSpanStarted(ctx, state.RunID, state.Step, contextSpanID(state.Step, "compression"), stepSpanID(state.Step), "context", trajectory.SpanContext, "context compression", map[string]any{
			"operation": "compaction",
			"before":    map[string]any{"tokens": result.Report.BeforeTokens, "content_ref": beforeRef},
		})
	}
	if err != nil {
		recorder.EmitSpanEnded(ctx, state.RunID, state.Step, contextSpanID(state.Step, "compression"), stepSpanID(state.Step), "context", trajectory.SpanContext, trajectory.SpanStatusError, map[string]any{
			"operation": "compaction",
			"error":     map[string]any{"message": err.Error()},
			"before":    map[string]any{"tokens": result.Report.BeforeTokens, "content_ref": beforeRef},
			"after":     map[string]any{"tokens": result.Report.AfterTokens, "content_ref": afterRef},
			"metrics": map[string]any{
				"effective_input_limit":  result.Report.EffectiveInputLimit,
				"truncated_tool_results": result.Report.TruncatedToolResult,
			},
			"attrs": map[string]any{"strategies": result.Report.Strategies, "report_ref": reportRef},
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
		afterRef = writeArtifact(ctx, recorder, state, "context_after", "", afterData, "context_after", "application/json")
		reportRef = writeReport()
	}
	if err := contextmgr.Overflow(result, contextOpts); err != nil {
		recorder.EmitSpanEnded(ctx, state.RunID, state.Step, contextSpanID(state.Step, "compression"), stepSpanID(state.Step), "context", trajectory.SpanContext, trajectory.SpanStatusError, map[string]any{
			"operation": "compaction",
			"error":     map[string]any{"message": err.Error()},
			"before":    map[string]any{"tokens": result.Report.BeforeTokens, "content_ref": beforeRef},
			"after":     map[string]any{"tokens": result.Report.AfterTokens, "content_ref": afterRef},
			"metrics": map[string]any{
				"effective_input_limit":  result.Report.EffectiveInputLimit,
				"truncated_tool_results": result.Report.TruncatedToolResult,
			},
			"attrs": map[string]any{"strategies": result.Report.Strategies, "report_ref": reportRef},
		})
		return model.Request{}, err
	}
	if result.Report.Compressed {
		summaryRef := ""
		if result.Summary != "" {
			summaryRef = writeArtifact(ctx, recorder, state, "context_summary", "", []byte(result.Summary), "context_summary", "text/markdown")
		}
		recorder.EmitSpanEnded(ctx, state.RunID, state.Step, contextSpanID(state.Step, "compression"), stepSpanID(state.Step), "context", trajectory.SpanContext, trajectory.SpanStatusOK, map[string]any{
			"operation":   "compaction",
			"boundary":    "replace",
			"before":      map[string]any{"tokens": result.Report.BeforeTokens, "content_ref": beforeRef},
			"after":       map[string]any{"tokens": result.Report.AfterTokens, "content_ref": afterRef},
			"summary_ref": summaryRef,
			"metrics": map[string]any{
				"before_tokens":          result.Report.BeforeTokens,
				"after_tokens":           result.Report.AfterTokens,
				"preserved_blocks":       result.Report.PreservedBlocks,
				"truncated_tool_results": result.Report.TruncatedToolResult,
			},
			"attrs": map[string]any{"strategies": result.Report.Strategies, "report_ref": reportRef},
		})
	} else if compressionRequired {
		recorder.EmitSpanEnded(ctx, state.RunID, state.Step, contextSpanID(state.Step, "compression"), stepSpanID(state.Step), "context", trajectory.SpanContext, trajectory.SpanStatusOK, map[string]any{
			"operation": "compaction",
			"boundary":  "noop",
			"before":    map[string]any{"tokens": result.Report.BeforeTokens, "content_ref": beforeRef},
			"after":     map[string]any{"tokens": result.Report.AfterTokens, "content_ref": afterRef},
			"metrics": map[string]any{
				"before_tokens":          result.Report.BeforeTokens,
				"after_tokens":           result.Report.AfterTokens,
				"preserved_blocks":       result.Report.PreservedBlocks,
				"truncated_tool_results": result.Report.TruncatedToolResult,
			},
			"attrs": map[string]any{"strategies": result.Report.Strategies, "report_ref": reportRef},
		})
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
	inputRef := writeArtifact(ctx, recorder, state, "context_summary_input", "", inputData, "context_summary_input", "application/json")
	spanID := llmSpanID(state.Step, "context_summary")
	recorder.EmitSpanStarted(ctx, state.RunID, state.Step, spanID, contextSpanID(state.Step, "compression"), "model:"+cfg.Name, trajectory.SpanLLM, "context summary", map[string]any{
		"input": map[string]any{"content_ref": inputRef},
		"attrs": map[string]any{"provider": cfg.Provider, "model": cfg.Model, "operation": "context_summary"},
	})
	if cfg.ContextWindow > 0 && rawTokens > effectiveLimit {
		err := fmt.Errorf("context summary request estimate %d exceeds effective input limit %d", rawTokens, effectiveLimit)
		recorder.EmitSpanEnded(ctx, state.RunID, state.Step, spanID, contextSpanID(state.Step, "compression"), "model:"+cfg.Name, trajectory.SpanLLM, trajectory.SpanStatusError, map[string]any{
			"input": map[string]any{"content_ref": inputRef},
			"error": map[string]any{"message": err.Error()},
		})
		return "", err
	}
	resp, err := client.Generate(ctx, req)
	state.ModelCalls++
	if err != nil {
		state.ModelErrors++
		recorder.EmitSpanEnded(ctx, state.RunID, state.Step, spanID, contextSpanID(state.Step, "compression"), "model:"+cfg.Name, trajectory.SpanLLM, trajectory.SpanStatusError, map[string]any{
			"input": map[string]any{"content_ref": inputRef},
			"error": map[string]any{"message": err.Error()},
		})
		return "", err
	}
	outputData := []byte(resp.Text)
	outputRef := writeArtifact(ctx, recorder, state, "context_summary_output", "", outputData, "context_summary_output", "text/plain")
	summary, err := parseSummaryResponse(resp.Text)
	if err != nil {
		state.ModelErrors++
		recorder.EmitSpanEnded(ctx, state.RunID, state.Step, spanID, contextSpanID(state.Step, "compression"), "model:"+cfg.Name, trajectory.SpanLLM, trajectory.SpanStatusError, map[string]any{
			"input":  map[string]any{"content_ref": inputRef},
			"output": map[string]any{"content_ref": outputRef},
			"error":  map[string]any{"message": err.Error()},
		})
		return "", err
	}
	state.RecordUsage(resp.Usage)
	recorder.EmitSpanEnded(ctx, state.RunID, state.Step, spanID, contextSpanID(state.Step, "compression"), "model:"+cfg.Name, trajectory.SpanLLM, trajectory.SpanStatusOK, map[string]any{
		"input":  map[string]any{"content_ref": inputRef},
		"output": map[string]any{"content_ref": outputRef},
		"attrs":  map[string]any{"provider": resp.Provider, "model": resp.Model, "operation": "context_summary"},
		"metrics": map[string]any{
			"latency_ms":              resp.LatencyMS,
			"prompt_tokens":           resp.Usage.InputTokens,
			"completion_tokens":       resp.Usage.OutputTokens,
			"total_tokens":            resp.Usage.TotalTokens,
			"prompt_cache_hit_tokens": resp.Usage.CacheHitTokens,
		},
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

func nativeToolDefinitions(specs []tools.Spec) []model.ToolDefinition {
	defs := make([]model.ToolDefinition, 0, len(specs))
	for _, spec := range specs {
		defs = append(defs, model.ToolDefinition{
			Name:        spec.Name,
			Description: spec.Description,
			Parameters:  spec.InputSchema,
			Strict:      false,
		})
	}
	return defs
}

func (r *Runtime) handleNativeModelResponse(ctx context.Context, agent *compiler.CompiledAgent, recorder *trajectory.Recorder, state *RunState, resp model.Response) error {
	if len(resp.ToolCalls) > 0 {
		state.Messages = append(state.Messages, model.Message{
			Role:             "assistant",
			Content:          resp.Text,
			ReasoningContent: resp.ReasoningContent,
			ToolCalls:        resp.ToolCalls,
		})

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
			emitAction(ctx, recorder, state, action)
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
	content := strings.TrimSpace(resp.Text)
	if content == "" {
		err := fmt.Errorf("native model response missing final content")
		state.AddError("action_parse", err)
		emitActionParseFailed(ctx, recorder, state, err)
		state.AddObservation("Return a non-empty final response, or call one of the provided function tools.")
		return nil
	}
	r.handleFinal(ctx, agent, recorder, state, content, "")
	return nil
}

func (r *Runtime) handleFinal(ctx context.Context, agent *compiler.CompiledAgent, recorder *trajectory.Recorder, state *RunState, content string, thought string) {
	content = strings.TrimSpace(content)
	if err := validateFinalOutput(agent, content); err != nil {
		if state.FinalValidationRetries < maxFinalValidationRetries(agent) {
			state.FinalValidationRetries++
			state.FinalValidationRetryPending = true
			state.AddError("final_schema", err)
			emitFinalValidationFailed(ctx, recorder, state, err)
			state.AddObservation(fmt.Sprintf(
				"Final answer did not match output schema %q: %s. Return only a JSON value that matches the schema. Do not call tools.",
				agent.Output.Name,
				err.Error(),
			))
			return
		}
		state.AddError("final_schema", err)
		emitFinalValidationFailed(ctx, recorder, state, err)
		state.Final = fmt.Sprintf("Run failed because final answer did not match output schema %q after %d retry.", agent.Output.Name, state.FinalValidationRetries)
		state.Status = StatusFailed
		return
	}
	emitActionWithFinalMediaType(ctx, recorder, state, Action{Type: ActionFinal, Thought: thought, Content: content}, finalMediaType(agent))
	state.Final = content
	state.Status = StatusCompleted
}

func validateFinalOutput(agent *compiler.CompiledAgent, content string) error {
	if agent.Output.CompiledSchema == nil {
		return nil
	}
	return jsonschemautil.ValidateJSON(agent.Output.CompiledSchema, content)
}

func maxFinalValidationRetries(agent *compiler.CompiledAgent) int {
	if agent.Output.CompiledSchema == nil {
		return 0
	}
	if agent.Output.MaxRetries <= 0 {
		return 1
	}
	return agent.Output.MaxRetries
}

func finalMediaType(agent *compiler.CompiledAgent) string {
	if agent.Output.CompiledSchema != nil {
		return "application/json"
	}
	return "text/markdown"
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
	if max := agent.Config.Runtime.Limits.MaxToolCalls; max > 0 && state.ToolCalls >= max {
		err := fmt.Errorf("max tool calls exceeded: %d", max)
		state.AddError("tool", err)
		state.AddObservation(fmt.Sprintf("Tool %s was not run because the tool budget is exhausted. Return a final answer using the available evidence.", action.Tool))
		return
	}
	if action.ToolCallID == "" {
		action.ToolCallID = fmt.Sprintf("call_%03d_%s", state.Step, sanitizeArtifactSuffix(action.Tool))
	}
	toolSpan := toolSpanID(state.Step, action.ToolCallID)
	tool, ok := agent.Tools.Get(action.Tool)
	if !ok {
		err := fmt.Errorf("unknown tool %q", action.Tool)
		state.ToolErrors++
		state.AddError("tool", err)
		recorder.EmitSpanStarted(ctx, state.RunID, state.Step, toolSpan, stepSpanID(state.Step), "tool:"+action.Tool, trajectory.SpanTool, action.Tool, map[string]any{
			"tool":         action.Tool,
			"tool_call_id": action.ToolCallID,
			"input":        map[string]any{"value": compactToolInput(action.Input)},
		})
		recorder.EmitSpanEnded(ctx, state.RunID, state.Step, toolSpan, stepSpanID(state.Step), "tool:"+action.Tool, trajectory.SpanTool, trajectory.SpanStatusError, map[string]any{
			"tool":         action.Tool,
			"tool_call_id": action.ToolCallID,
			"error":        map[string]any{"message": err.Error()},
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
	permissionPayload := map[string]any{
		"tool_call_id": action.ToolCallID,
		"tool":         action.Tool,
		"capabilities": spec.Capabilities,
		"decision":     decisionName(decision.Action),
		"reason":       decision.Reason,
	}
	approvalReason := ""
	if decision.Action == policy.DecisionDeny {
		state.PermissionDenied++
		recorder.Emit(ctx, trajectory.EventPermissionDecided, state.RunID, state.Step, "policy", permissionPayload)
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
				permissionPayload["decision"] = "denied"
				permissionPayload["reason"] = "user denied"
				recorder.Emit(ctx, trajectory.EventPermissionDecided, state.RunID, state.Step, "policy", permissionPayload)
				state.AddObservation(fmt.Sprintf("Tool %s denied by user.", action.Tool))
				return
			}
		}
	}
	if approvalReason != "" {
		permissionPayload["decision"] = "auto_approved"
		permissionPayload["reason"] = approvalReason
	} else {
		permissionPayload["decision"] = "approved"
	}
	recorder.Emit(ctx, trajectory.EventPermissionDecided, state.RunID, state.Step, "policy", permissionPayload)

	start := time.Now()
	recorder.EmitSpanStarted(ctx, state.RunID, state.Step, toolSpan, stepSpanID(state.Step), "tool:"+action.Tool, trajectory.SpanTool, action.Tool, map[string]any{
		"tool":         action.Tool,
		"tool_call_id": action.ToolCallID,
		"input":        map[string]any{"value": compactToolInput(action.Input)},
	})
	var result tools.Result
	var err error
	if agentTool, ok := agent.AgentTools[action.Tool]; ok {
		result, err = r.runAgentTool(ctx, agent, recorder, state, action, toolSpan, agentTool)
	} else {
		result, err = runToolWithTimeout(ctx, tool, spec, action.Input)
	}
	latency := time.Since(start).Milliseconds()
	if err != nil {
		state.ToolErrors++
		state.AddError("tool", err)
		recorder.EmitSpanEnded(ctx, state.RunID, state.Step, toolSpan, stepSpanID(state.Step), "tool:"+action.Tool, trajectory.SpanTool, trajectory.SpanStatusError, map[string]any{
			"tool":         action.Tool,
			"tool_call_id": action.ToolCallID,
			"error":        map[string]any{"message": err.Error()},
			"metrics":      map[string]any{"latency_ms": latency},
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
	outputRef := writeArtifact(ctx, recorder, state, "tool_output", artifactSuffix, outputData, "tool_output", "application/json")
	recorder.EmitSpanEnded(ctx, state.RunID, state.Step, toolSpan, stepSpanID(state.Step), "tool:"+action.Tool, trajectory.SpanTool, trajectory.SpanStatusOK, map[string]any{
		"tool":         action.Tool,
		"tool_call_id": action.ToolCallID,
		"input":        map[string]any{"value": compactToolInput(action.Input)},
		"output":       map[string]any{"content_ref": outputRef},
		"metrics":      map[string]any{"latency_ms": latency},
	})
	for _, artifact := range result.Artifacts {
		writeArtifact(ctx, recorder, state, "workspace_file", artifact.Name, []byte(artifact.Path), "workspace_file", "text/plain")
	}
	state.AddObservation(fmt.Sprintf("Tool %s completed: %s", action.Tool, result.Output))
}

func (r *Runtime) handleAskUser(ctx context.Context, recorder *trajectory.Recorder, state *RunState, action Action) {
	state.Status = StatusWaitingUser
	if r.autoUserInput != nil {
		answer := *r.autoUserInput
		recorder.Emit(ctx, trajectory.EventMessageCreated, state.RunID, state.Step, "user", map[string]any{
			"message_id": fmt.Sprintf("msg_user_%03d", state.Step),
			"role":       "user",
			"source":     "user",
			"content":    []map[string]any{{"type": "text", "text": answer}},
			"reason":     "auto_answered_by_evolve",
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
	recorder.Emit(ctx, trajectory.EventMessageCreated, state.RunID, state.Step, "user", map[string]any{
		"message_id": fmt.Sprintf("msg_user_%03d", state.Step),
		"role":       "user",
		"source":     "user",
		"content":    []map[string]any{{"type": "text", "text": answer}},
	})
	state.Messages = append(state.Messages, model.Message{Role: "user", Content: answer})
	state.Status = StatusRunning
}

func (r *Runtime) runEvaluation(ctx context.Context, agent *compiler.CompiledAgent, recorder *trajectory.Recorder, state *RunState) *evaluate.Result {
	spanID := "span_eval_001"
	recorder.EmitSpanStarted(ctx, state.RunID, state.Step, spanID, runSpanID(), "evaluate", trajectory.SpanEvaluator, "evaluation", nil)
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
		recorder.EmitSpanEnded(ctx, state.RunID, state.Step, spanID, runSpanID(), "evaluate", trajectory.SpanEvaluator, trajectory.SpanStatusError, map[string]any{
			"error": map[string]any{"message": err.Error()},
		})
		return nil
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	evalRef := writeArtifact(ctx, recorder, state, "evaluation", "", data, "evaluation", "application/json")
	recorder.EmitSpanEnded(ctx, state.RunID, state.Step, spanID, runSpanID(), "evaluate", trajectory.SpanEvaluator, trajectory.SpanStatusOK, map[string]any{
		"output": map[string]any{
			"content_ref": evalRef,
			"passed":      result.Passed,
			"score":       result.Score,
			"evaluators":  result.Evaluators,
		},
	})
	return &result
}

func (r *Runtime) emitSkillEvents(ctx context.Context, recorder *trajectory.Recorder, state *RunState, agent *compiler.CompiledAgent) {
	all := agent.Skills.All()
	names := make([]string, 0, len(all))
	for _, skill := range all {
		names = append(names, skill.Manifest.Metadata.Name)
	}
	disclosureSpan := "span_skill_disclosure"
	recorder.EmitSpanStarted(ctx, state.RunID, 0, disclosureSpan, runSpanID(), "skills", trajectory.SpanSkill, "skill disclosure", map[string]any{
		"operation": "disclosure",
	})
	recorder.EmitSpanEnded(ctx, state.RunID, 0, disclosureSpan, runSpanID(), "skills", trajectory.SpanSkill, trajectory.SpanStatusOK, map[string]any{
		"operation": "disclosure",
		"output":    map[string]any{"count": len(names), "names": names},
	})
	for _, skill := range agent.Skills.Active() {
		spanID := "span_skill_" + sanitizeArtifactSuffix(skill.Manifest.Metadata.Name)
		recorder.EmitSpanStarted(ctx, state.RunID, 0, spanID, runSpanID(), "skills", trajectory.SpanSkill, skill.Manifest.Metadata.Name, map[string]any{
			"operation": "load",
		})
		recorder.EmitSpanEnded(ctx, state.RunID, 0, spanID, runSpanID(), "skills", trajectory.SpanSkill, trajectory.SpanStatusOK, map[string]any{
			"operation": "load",
			"output":    map[string]any{"name": skill.Manifest.Metadata.Name},
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

func (r *Runtime) runAgentTool(ctx context.Context, parent *compiler.CompiledAgent, recorder *trajectory.Recorder, state *RunState, action Action, toolSpan string, spec compiler.AgentToolSpec) (tools.Result, error) {
	spanID := subagentSpanID(state.Step, action.ToolCallID)
	attrs := map[string]any{
		"tool":         action.Tool,
		"tool_call_id": action.ToolCallID,
		"manifest":     spec.Manifest,
	}
	recorder.EmitSpanStarted(ctx, state.RunID, state.Step, spanID, toolSpan, "subagent:"+action.Tool, trajectory.SpanSubagent, action.Tool, map[string]any{
		"tool_call_id": action.ToolCallID,
		"attrs":        attrs,
	})

	childStore := runs.NewStore(filepath.Join(state.RunDir, "child-runs", sanitizeArtifactSuffix(action.Tool), sanitizeArtifactSuffix(action.ToolCallID)))
	child, err := compiler.CompileWithOptions(spec.Manifest, compiler.Options{
		RunStore:          childStore,
		WorkspaceOverride: parent.Config.Workspace.Path,
	})
	if err != nil {
		recorder.EmitSpanEnded(ctx, state.RunID, state.Step, spanID, toolSpan, "subagent:"+action.Tool, trajectory.SpanSubagent, trajectory.SpanStatusError, map[string]any{
			"tool_call_id": action.ToolCallID,
			"attrs":        attrs,
			"error":        map[string]any{"message": err.Error()},
		})
		return tools.Result{}, err
	}

	input := renderAgentToolInput(parent.Name, action.Tool, action.Input)
	autoUserInput := "Use only the delegated tool task and available tools. Do not ask the user during an agent tool child run; return a final answer with any gaps or residual risk."
	rt := NewWithOptions(Options{
		AutoUserInput:             &autoUserInput,
		SuppressConsoleTrajectory: true,
	})
	result, err := rt.Run(ctx, child, input)
	if err != nil {
		recorder.EmitSpanEnded(ctx, state.RunID, state.Step, spanID, toolSpan, "subagent:"+action.Tool, trajectory.SpanSubagent, trajectory.SpanStatusError, map[string]any{
			"tool_call_id": action.ToolCallID,
			"attrs":        attrs,
			"error":        map[string]any{"message": err.Error()},
		})
		return tools.Result{}, err
	}

	record, err := child.RunStore.ReadRunRecord(result.RunID)
	if err != nil {
		recorder.EmitSpanEnded(ctx, state.RunID, state.Step, spanID, toolSpan, "subagent:"+action.Tool, trajectory.SpanSubagent, trajectory.SpanStatusError, map[string]any{
			"tool_call_id": action.ToolCallID,
			"attrs":        attrs,
			"error":        map[string]any{"message": err.Error()},
		})
		return tools.Result{}, err
	}
	childRunDir := filepath.Join(child.RunStore.BasePath, result.RunID)
	childRunPath := relativeRunPath(state.RunDir, childRunDir)
	attrs["agent"] = record.Agent
	attrs["child_run_id"] = result.RunID
	attrs["child_run_path"] = childRunPath
	attrs["status"] = string(result.Status)
	metrics := statsMetrics(record.Stats, record.DurationMS)
	recorder.EmitSpanEnded(ctx, state.RunID, state.Step, spanID, toolSpan, "subagent:"+action.Tool, trajectory.SpanSubagent, spanStatusForRun(result.Status), map[string]any{
		"tool_call_id": action.ToolCallID,
		"attrs":        attrs,
		"metrics":      metrics,
	})
	addChildStats(state, record)

	return tools.Result{
		Output: strings.TrimSpace(result.Final),
		Metadata: map[string]any{
			"kind":     "agent_run",
			"agent":    record.Agent,
			"status":   string(result.Status),
			"run_id":   result.RunID,
			"run_path": childRunPath,
			"stats":    metrics,
		},
	}, nil
}

func renderAgentToolInput(parentAgent, tool string, input json.RawMessage) string {
	var payload map[string]any
	if err := json.Unmarshal(input, &payload); err != nil {
		return fmt.Sprintf("# Jeju Agent Tool Task\n\nparent_agent: %s\ntool: %s\n\n# Tool Input\n\n%s", parentAgent, tool, strings.TrimSpace(string(input)))
	}
	var b strings.Builder
	b.WriteString("# Jeju Agent Tool Task\n\n")
	b.WriteString(fmt.Sprintf("parent_agent: %s\n", parentAgent))
	b.WriteString(fmt.Sprintf("tool: %s\n", tool))
	if task, ok := payload["task"]; ok {
		b.WriteString("\n# Task\n\n")
		b.WriteString(fmt.Sprint(task))
		b.WriteString("\n")
	}
	if contextValue, ok := payload["context"]; ok {
		b.WriteString("\n# Context\n\n")
		b.WriteString(fmt.Sprint(contextValue))
		b.WriteString("\n")
	}
	if expected, ok := payload["expected_output"]; ok {
		b.WriteString("\n# Expected Output\n\n")
		b.WriteString(fmt.Sprint(expected))
		b.WriteString("\n")
	}
	if _, ok := payload["task"]; !ok {
		data, _ := json.MarshalIndent(payload, "", "  ")
		b.WriteString("\n# Tool Input JSON\n\n")
		b.Write(data)
		b.WriteString("\n")
	}
	return b.String()
}

func addChildStats(state *RunState, record trajectory.RunRecord) {
	state.ChildRuns++
	if record.Status != string(StatusCompleted) {
		state.ChildRunErrors++
	}
	state.ChildModelCalls += record.Stats.ModelCalls
	state.ChildToolCalls += record.Stats.ToolCalls
	state.ChildModelErrors += record.Stats.ModelErrors
	state.ChildToolErrors += record.Stats.ToolErrors
	state.ChildPermissionDenied += record.Stats.PermissionDenied
	state.ChildPromptTokens += record.Stats.PromptTokens
	state.ChildPromptCacheHitTokens += record.Stats.PromptCacheHitTokens
	state.ChildCompletionTokens += record.Stats.CompletionTokens
	state.ChildTotalTokens += record.Stats.TotalTokens
}

func statsMetrics(stats trajectory.RunStats, durationMS int64) map[string]any {
	return map[string]any{
		"model_calls":             stats.ModelCalls,
		"tool_calls":              stats.ToolCalls,
		"model_errors":            stats.ModelErrors,
		"tool_errors":             stats.ToolErrors,
		"permission_denied":       stats.PermissionDenied,
		"prompt_tokens":           stats.PromptTokens,
		"prompt_cache_hit_tokens": stats.PromptCacheHitTokens,
		"completion_tokens":       stats.CompletionTokens,
		"total_tokens":            stats.TotalTokens,
		"duration_ms":             durationMS,
	}
}

func relativeRunPath(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
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

func writeArtifact(ctx context.Context, recorder *trajectory.Recorder, state *RunState, typ string, suffix string, data []byte, role string, mediaType string) string {
	id := artifactID(state.Step, typ, suffix)
	const inlineLimit = 64 * 1024
	payload := map[string]any{
		"artifact_id": id,
		"role":        role,
		"media_type":  mediaType,
	}
	if len(data) <= inlineLimit {
		if mediaType == "application/json" {
			var value any
			if err := json.Unmarshal(data, &value); err == nil {
				payload["encoding"] = "json"
				payload["value"] = value
			} else {
				payload["encoding"] = "utf-8"
				payload["text"] = string(data)
			}
		} else {
			payload["encoding"] = "utf-8"
			payload["text"] = string(data)
		}
		recorder.Emit(ctx, trajectory.EventArtifactCreated, state.RunID, state.Step, "runtime", payload)
		return id
	}
	payload["chunked"] = true
	recorder.Emit(ctx, trajectory.EventArtifactCreated, state.RunID, state.Step, "runtime", payload)
	for index, start := 0, 0; start < len(data); index, start = index+1, start+inlineLimit {
		end := start + inlineLimit
		if end > len(data) {
			end = len(data)
		}
		recorder.Emit(ctx, trajectory.EventArtifactChunk, state.RunID, state.Step, "runtime", map[string]any{
			"artifact_id": id,
			"index":       index,
			"encoding":    "base64",
			"data":        base64.StdEncoding.EncodeToString(data[start:end]),
		})
	}
	sum := sha256.Sum256(data)
	recorder.Emit(ctx, trajectory.EventArtifactFinalized, state.RunID, state.Step, "runtime", map[string]any{
		"artifact_id": id,
		"bytes":       len(data),
		"sha256":      fmt.Sprintf("%x", sum[:]),
	})
	return id
}

func artifactID(step int, typ string, suffix string) string {
	if step <= 0 {
		if suffix != "" {
			return "art_" + typ + "_" + sanitizeArtifactSuffix(suffix)
		}
		return "art_" + typ
	}
	name := fmt.Sprintf("art_step%03d_%s", step, typ)
	if suffix != "" {
		name += "_" + sanitizeArtifactSuffix(suffix)
	}
	return name
}

func runSpanID() string { return "span_run" }

func stepSpanID(step int) string { return fmt.Sprintf("span_step_%03d", step) }

func llmSpanID(step int, suffix string) string {
	return fmt.Sprintf("span_llm_%03d_%s", step, sanitizeArtifactSuffix(suffix))
}

func toolSpanID(step int, callID string) string {
	return fmt.Sprintf("span_tool_%03d_%s", step, sanitizeArtifactSuffix(callID))
}

func subagentSpanID(step int, callID string) string {
	return fmt.Sprintf("span_subagent_%03d_%s", step, sanitizeArtifactSuffix(callID))
}

func contextSpanID(step int, suffix string) string {
	return fmt.Sprintf("span_context_%03d_%s", step, sanitizeArtifactSuffix(suffix))
}

func spanStatusForRun(status RunStatus) trajectory.SpanStatus {
	switch status {
	case StatusCompleted, StatusRunning, StatusWaitingUser, StatusWaitingApproval, StatusPaused:
		return trajectory.SpanStatusOK
	case StatusCancelled:
		return trajectory.SpanStatusCancelled
	default:
		return trajectory.SpanStatusError
	}
}

func runStatsPayload(state *RunState) map[string]any {
	return map[string]any{
		"steps":                         state.Step,
		"model_calls":                   state.ModelCalls,
		"tool_calls":                    state.ToolCalls,
		"permission_denied":             state.PermissionDenied,
		"model_errors":                  state.ModelErrors,
		"tool_errors":                   state.ToolErrors,
		"child_runs":                    state.ChildRuns,
		"child_run_errors":              state.ChildRunErrors,
		"child_model_calls":             state.ChildModelCalls,
		"child_tool_calls":              state.ChildToolCalls,
		"child_model_errors":            state.ChildModelErrors,
		"child_tool_errors":             state.ChildToolErrors,
		"child_permission_denied":       state.ChildPermissionDenied,
		"child_prompt_tokens":           state.ChildPromptTokens,
		"child_prompt_cache_hit_tokens": state.ChildPromptCacheHitTokens,
		"child_completion_tokens":       state.ChildCompletionTokens,
		"child_total_tokens":            state.ChildTotalTokens,
		"prompt_tokens":                 state.PromptTokens,
		"prompt_cache_hit_tokens":       state.PromptCacheHitTokens,
		"completion_tokens":             state.CompletionTokens,
		"total_tokens":                  state.TotalTokens,
	}
}

func emitAction(ctx context.Context, recorder *trajectory.Recorder, state *RunState, action Action) {
	emitActionWithFinalMediaType(ctx, recorder, state, action, "text/markdown")
}

func emitActionWithFinalMediaType(ctx context.Context, recorder *trajectory.Recorder, state *RunState, action Action, finalMediaType string) {
	payload := map[string]any{
		"action_id": fmt.Sprintf("act_%03d_%s", state.Step, sanitizeArtifactSuffix(string(action.Type))),
		"kind":      string(action.Type),
	}
	if action.Thought != "" {
		payload["thought"] = action.Thought
	}
	if action.Tool != "" {
		payload["tool_call_id"] = action.ToolCallID
		payload["function_name"] = action.Tool
		payload["arguments"] = compactToolInput(action.Input)
	}
	if action.Question != "" {
		payload["question"] = action.Question
	}
	if action.Content != "" {
		ref := writeArtifact(ctx, recorder, state, "final", "", []byte(action.Content), "final", finalMediaType)
		state.FinalRef = ref
		payload["final"] = map[string]any{"content_ref": ref}
	}
	recorder.Emit(ctx, trajectory.EventActionCreated, state.RunID, state.Step, "runtime", payload)
}

func emitFinalValidationFailed(ctx context.Context, recorder *trajectory.Recorder, state *RunState, err error) {
	recorder.Emit(ctx, trajectory.EventActionCreated, state.RunID, state.Step, "runtime", map[string]any{
		"action_id": fmt.Sprintf("act_%03d_final_validation_failed_%d", state.Step, state.FinalValidationRetries),
		"kind":      "final_validation_failed",
		"error":     map[string]any{"message": err.Error()},
	})
}

func emitActionParseFailed(ctx context.Context, recorder *trajectory.Recorder, state *RunState, err error) {
	recorder.Emit(ctx, trajectory.EventActionCreated, state.RunID, state.Step, "runtime", map[string]any{
		"action_id": fmt.Sprintf("act_%03d_parse_failed", state.Step),
		"kind":      "parse_failed",
		"error":     map[string]any{"message": err.Error()},
	})
}

func decisionName(decision policy.DecisionAction) string {
	switch decision {
	case policy.DecisionAllow:
		return "approved"
	case policy.DecisionAsk:
		return "ask"
	case policy.DecisionDeny:
		return "denied"
	default:
		return string(decision)
	}
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
