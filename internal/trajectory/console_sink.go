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
	case EventRunStarted:
		return fmt.Sprintf("\nJeju Run %s\nAgent   %v\nTask    %v", event.RunID, payload(event, "agent"), payload(event, "input"))
	case EventRunCompleted:
		return fmt.Sprintf("\nCompleted\n  run    %v\n  status %v", payload(event, "run_dir"), payload(event, "status"))
	case EventRunFailed:
		return fmt.Sprintf("\nFailed\n  run    %v\n  status %v", payload(event, "run_dir"), payload(event, "status"))
	case EventRunCancelled:
		return fmt.Sprintf("\nCancelled\n  run    %v", payload(event, "run_dir"))
	case EventSkillDisclosed:
		return fmt.Sprintf("\nSkills\n  loaded  %s", formatNames(payload(event, "names")))
	case EventSkillLoaded:
		return ""
	case EventStepStarted:
		return fmt.Sprintf("\nStep %d", event.Step)
	case EventStepCompleted:
		status := fmt.Sprint(payload(event, "status"))
		if status == "" || status == "running" {
			return ""
		}
		return fmt.Sprintf("  status %s", status)
	case EventModelStarted:
		return ""
	case EventModelCompleted:
		line := fmt.Sprintf("  model  %v/%v  %s  tokens %v->%v\n         output %v", payload(event, "provider"), payload(event, "model"), formatLatency(payload(event, "latency_ms")), payload(event, "tokens_in"), payload(event, "tokens_out"), payload(event, "output_ref"))
		if ref := payload(event, "reasoning_ref"); ref != "" {
			line += fmt.Sprintf("\n         thinking %v", ref)
			if preview := payload(event, "reasoning_preview"); preview != "" {
				line += fmt.Sprintf("\n         thought  %v", preview)
			}
		}
		return line
	case EventModelFailed:
		return fmt.Sprintf("  model  failed  error=%v", payload(event, "error"))
	case EventActionParsed:
		return formatAction(event)
	case EventActionParseFailed:
		return fmt.Sprintf("  action parse_failed  error=%v", payload(event, "error"))
	case EventToolRequested:
		return fmt.Sprintf("  tool   %v\n         input  %s", payload(event, "tool"), formatAny(payload(event, "input")))
	case EventToolStarted:
		return ""
	case EventPermissionChecked:
		return fmt.Sprintf("  gate   %v  %s", payload(event, "decision"), formatNames(payload(event, "capabilities")))
	case EventPermissionApproved:
		return ""
	case EventPermissionDenied:
		return fmt.Sprintf("  gate   deny  tool=%v reason=%v", payload(event, "tool"), payload(event, "reason"))
	case EventToolCompleted:
		return fmt.Sprintf("  tool   %v  %s  ok\n         output %v", payload(event, "tool"), formatLatency(payload(event, "latency_ms")), payload(event, "output_ref"))
	case EventToolFailed:
		return fmt.Sprintf("  tool   %v  %s  failed\n         error  %v", payload(event, "tool"), formatLatency(payload(event, "latency_ms")), payload(event, "error"))
	case EventArtifactCreated:
		return ""
	case EventUserInputRequested:
		return fmt.Sprintf("  user   input requested  question=%v", payload(event, "question"))
	case EventUserInputReceived:
		return "  user   input received"
	case EventEvaluationStarted:
		return "\nEvaluation"
	case EventEvaluationCompleted:
		return fmt.Sprintf("  passed %v  score=%v", payload(event, "passed"), payload(event, "score"))
	case EventEvaluationFailed:
		return fmt.Sprintf("  failed error=%v", payload(event, "error"))
	default:
		return fmt.Sprintf("  event  %s", event.Type)
	}
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

func formatAction(event Event) string {
	actionType := payload(event, "type")
	tool := payload(event, "tool")
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
