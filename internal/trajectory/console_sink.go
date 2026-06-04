package trajectory

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type ConsoleSink struct {
	out io.Writer
}

func NewConsoleSink() *ConsoleSink {
	return &ConsoleSink{out: os.Stdout}
}

func (s *ConsoleSink) Write(ctx context.Context, event Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	line := formatConsole(event)
	if line != "" {
		fmt.Fprintln(s.out, line)
	}
	return nil
}

func (s *ConsoleSink) Close() error {
	return nil
}

func formatConsole(event Event) string {
	switch event.Type {
	case EventTrajectoryHeader:
		return fmt.Sprintf("\nJeju Run %s\nAgent   %v\nTask    %v", event.RunID, nestedPayload(event, "agent", "name"), payload(event, "input"))
	case EventSpanStarted:
		if payload(event, "kind") == string(SpanStep) {
			return fmt.Sprintf("\nStep %d", event.StepID)
		}
		return ""
	case EventSpanEnded:
		return formatSpanEnded(event)
	case EventActionCreated:
		return formatAction(event)
	case EventPermissionDecided:
		return fmt.Sprintf("  gate   %v  tool=%v %s", payload(event, "decision"), payload(event, "tool"), formatNames(payload(event, "capabilities")))
	case EventArtifactCreated:
		return ""
	case EventMessageCreated, EventArtifactChunk, EventArtifactFinalized:
		return ""
	case EventRunSummary:
		return fmt.Sprintf("\nCompleted\n  run    %s\n  status %v", event.RunID, payload(event, "status"))
	default:
		return fmt.Sprintf("  event  %s", event.Type)
	}
}

func formatModelName(event Event) string {
	attrs, ok := payload(event, "attrs").(map[string]any)
	if !ok {
		return event.Actor
	}
	provider, _ := attrs["provider"].(string)
	model, _ := attrs["model"].(string)
	switch {
	case provider != "" && model != "":
		return provider + "/" + model
	case model != "":
		return model
	case provider != "":
		return provider
	default:
		return event.Actor
	}
}

func formatSpanEnded(event Event) string {
	kind := fmt.Sprint(payload(event, "kind"))
	status := fmt.Sprint(payload(event, "status"))
	if kind == string(SpanRun) || kind == string(SpanStep) {
		if status == "" || status == string(SpanStatusOK) {
			return ""
		}
		return fmt.Sprintf("  status %s", status)
	}
	if kind == string(SpanLLM) {
		metrics, _ := payload(event, "metrics").(map[string]any)
		line := fmt.Sprintf("  model  %s  %s  tokens %v->%v\n         output %v", formatModelName(event), formatLatency(metrics["latency_ms"]), metrics["prompt_tokens"], metrics["completion_tokens"], nestedPayload(event, "output", "content_ref"))
		if ref := nestedPayload(event, "reasoning", "content_ref"); ref != "" {
			line += fmt.Sprintf("\n         thinking %v", ref)
		}
		if status == string(SpanStatusError) {
			line += fmt.Sprintf("\n         error %v", nestedPayload(event, "error", "message"))
		}
		return line
	}
	if kind == string(SpanTool) {
		metrics, _ := payload(event, "metrics").(map[string]any)
		name := event.Actor
		if tool := payload(event, "tool"); tool != "" {
			name = fmt.Sprint(tool)
		}
		if status == string(SpanStatusError) {
			return fmt.Sprintf("  tool   %s  %s  failed\n         error  %v", name, formatLatency(metrics["latency_ms"]), nestedPayload(event, "error", "message"))
		}
		return fmt.Sprintf("  tool   %s  %s  ok\n         output %v", name, formatLatency(metrics["latency_ms"]), nestedPayload(event, "output", "content_ref"))
	}
	if kind == string(SpanEvaluator) {
		if status == string(SpanStatusError) {
			return fmt.Sprintf("\nEvaluation\n  failed error=%v", nestedPayload(event, "error", "message"))
		}
		return fmt.Sprintf("\nEvaluation\n  passed %v  score=%v", nestedPayload(event, "output", "passed"), nestedPayload(event, "output", "score"))
	}
	return ""
}

func payload(event Event, key string) any {
	if event.Payload == nil {
		return ""
	}
	if value, ok := event.Payload[key]; ok {
		return value
	}
	return ""
}

func nestedPayload(event Event, key, nested string) any {
	value, ok := payload(event, key).(map[string]any)
	if !ok {
		return ""
	}
	if nestedValue, ok := value[nested]; ok {
		return nestedValue
	}
	return ""
}

func formatAction(event Event) string {
	actionType := payload(event, "kind")
	tool := payload(event, "function_name")
	if tool != "" {
		return fmt.Sprintf("  action %v  %v", actionType, tool)
	}
	return fmt.Sprintf("  action %v", actionType)
}

func formatAny(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		parts := make([]string, 0, len(typed))
		keys := make([]string, 0, len(typed))
		for key, val := range typed {
			keys = append(keys, key)
			_ = val
		}
		sort.Strings(keys)
		for _, key := range keys {
			val := typed[key]
			parts = append(parts, fmt.Sprintf("%s=%v", key, val))
		}
		return strings.Join(parts, " ")
	case nil:
		return ""
	default:
		return fmt.Sprint(value)
	}
}

func formatLatency(value any) string {
	ms := int64Value(value)
	if ms >= 1000 {
		return fmt.Sprintf("%.2fs", float64(ms)/1000)
	}
	return fmt.Sprintf("%dms", ms)
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case int32:
		return int64(typed)
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	default:
		return 0
	}
}

func formatNames(value any) string {
	switch typed := value.(type) {
	case []string:
		return strings.Join(typed, ", ")
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, ", ")
	case nil:
		return ""
	default:
		return strings.Trim(fmt.Sprint(typed), "[]")
	}
}
