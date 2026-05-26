package trajectory

import (
	"context"
	"fmt"
	"io"
	"os"
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
	case "run.started":
		return fmt.Sprintf("\nRun %s\n  agent: %v\n  task: %v", event.RunID, payload(event, "agent"), payload(event, "input"))
	case "run.completed":
		return fmt.Sprintf("\nRun completed\n  id: %s\n  status: %v\n  dir: %v", event.RunID, payload(event, "status"), payload(event, "run_dir"))
	case "run.failed":
		return fmt.Sprintf("\nRun failed\n  id: %s\n  status: %v\n  dir: %v", event.RunID, payload(event, "status"), payload(event, "run_dir"))
	case "run.cancelled":
		return fmt.Sprintf("\nRun cancelled\n  id: %s\n  dir: %v", event.RunID, payload(event, "run_dir"))
	case "skill.disclosed":
		return fmt.Sprintf("  skills: disclosed count=%v names=%v", payload(event, "count"), payload(event, "names"))
	case "skill.loaded":
		return fmt.Sprintf("  skill loaded: %v", payload(event, "name"))
	case "step.started":
		return fmt.Sprintf("\nStep %d\n  model: %v", event.Step, payload(event, "model"))
	case "step.completed":
		return fmt.Sprintf("  step: completed status=%v", payload(event, "status"))
	case "model.started":
		return fmt.Sprintf("  model: started provider=%v model=%v input=%v", payload(event, "provider"), payload(event, "model"), payload(event, "input_ref"))
	case "model.completed":
		return fmt.Sprintf("  model: completed latency=%vms tokens=%v/%v output=%v", payload(event, "latency_ms"), payload(event, "tokens_in"), payload(event, "tokens_out"), payload(event, "output_ref"))
	case "model.failed":
		return fmt.Sprintf("  model: failed error=%v", payload(event, "error"))
	case "action.parsed":
		return formatAction(event)
	case "action.parse_failed":
		return fmt.Sprintf("  action: parse_failed error=%v", payload(event, "error"))
	case "tool.requested":
		return fmt.Sprintf("  tool: requested name=%v input=%s", payload(event, "tool"), formatAny(payload(event, "input")))
	case "tool.started":
		return fmt.Sprintf("  tool: started name=%v", payload(event, "tool"))
	case "permission.checked":
		return fmt.Sprintf("  permission: checked tool=%v decision=%v risks=%v", payload(event, "tool"), payload(event, "decision"), payload(event, "risks"))
	case "permission.approved":
		return fmt.Sprintf("  permission: approved tool=%v", payload(event, "tool"))
	case "permission.denied":
		return fmt.Sprintf("  permission: denied tool=%v reason=%v", payload(event, "tool"), payload(event, "reason"))
	case "tool.completed":
		return fmt.Sprintf("  tool: completed name=%v latency=%vms output=%v", payload(event, "tool"), payload(event, "latency_ms"), payload(event, "output_ref"))
	case "tool.failed":
		return fmt.Sprintf("  tool: failed name=%v latency=%vms error=%v", payload(event, "tool"), payload(event, "latency_ms"), payload(event, "error"))
	case "artifact.created":
		return ""
	case "user.input.requested":
		return fmt.Sprintf("  user: input requested question=%v", payload(event, "question"))
	case "user.input.received":
		return "  user: input received"
	case "evaluation.started":
		return "\nEvaluation\n  status: started"
	case "evaluation.completed":
		return fmt.Sprintf("  status: completed passed=%v score=%v", payload(event, "passed"), payload(event, "score"))
	case "evaluation.failed":
		return fmt.Sprintf("  status: failed error=%v", payload(event, "error"))
	default:
		return fmt.Sprintf("  event: %s", event.Type)
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
		return fmt.Sprintf("  action: %v tool=%v", actionType, tool)
	}
	return fmt.Sprintf("  action: %v", actionType)
}

func formatAny(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		parts := make([]string, 0, len(typed))
		for key, val := range typed {
			parts = append(parts, fmt.Sprintf("%s=%v", key, val))
		}
		return strings.Join(parts, " ")
	case nil:
		return ""
	default:
		return fmt.Sprint(value)
	}
}
