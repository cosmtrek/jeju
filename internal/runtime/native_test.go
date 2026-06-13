package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cosmtrek/jeju/internal/compiler"
	"github.com/cosmtrek/jeju/internal/config"
	"github.com/cosmtrek/jeju/internal/memory"
	"github.com/cosmtrek/jeju/internal/model"
	"github.com/cosmtrek/jeju/internal/policy"
	"github.com/cosmtrek/jeju/internal/runs"
	"github.com/cosmtrek/jeju/internal/sandbox"
	"github.com/cosmtrek/jeju/internal/skills"
	"github.com/cosmtrek/jeju/internal/tools"
	"github.com/cosmtrek/jeju/internal/tools/builtin"
	"github.com/cosmtrek/jeju/internal/trajectory"
)

type nativeFakeClient struct {
	requests []model.Request
}

func (c *nativeFakeClient) Generate(ctx context.Context, req model.Request) (model.Response, error) {
	c.requests = append(c.requests, req)
	if len(c.requests) == 1 {
		return model.Response{
			Provider:         "openaiCompatible",
			Model:            "fake-native",
			Text:             "I will write the requested note before finishing.",
			ReasoningContent: "I should write the requested note.",
			ToolCalls: []model.ToolCall{{
				ID:        "call_1",
				Name:      "write",
				Arguments: json.RawMessage(`{"path":"notes.md","content":"hello from native tools"}`),
			}},
		}, nil
	}
	return model.Response{
		Provider: "openaiCompatible",
		Model:    "fake-native",
		Text:     "done",
	}, nil
}

type nativeMultiToolClient struct {
	requests []model.Request
}

func (c *nativeMultiToolClient) Generate(ctx context.Context, req model.Request) (model.Response, error) {
	c.requests = append(c.requests, req)
	if len(c.requests) == 1 {
		return model.Response{
			Provider: "openaiCompatible",
			Model:    "fake-native",
			Text:     "parallel writes",
			ToolCalls: []model.ToolCall{
				{
					ID:        "call_1",
					Name:      "write",
					Arguments: json.RawMessage(`{"path":"one.md","content":"one"}`),
				},
				{
					ID:        "call_2",
					Name:      "write",
					Arguments: json.RawMessage(`{"path":"two.md","content":"two"}`),
				},
			},
		}, nil
	}
	return model.Response{
		Provider: "openaiCompatible",
		Model:    "fake-native",
		Text:     "done",
	}, nil
}

type nativeEmptyFinalClient struct {
	requests []model.Request
}

func (c *nativeEmptyFinalClient) Generate(ctx context.Context, req model.Request) (model.Response, error) {
	c.requests = append(c.requests, req)
	if len(c.requests) == 1 {
		return model.Response{
			Provider: "openaiCompatible",
			Model:    "fake-native",
		}, nil
	}
	return model.Response{
		Provider: "openaiCompatible",
		Model:    "fake-native",
		Text:     "done after retry",
	}, nil
}

type nativeToolBudgetClient struct {
	requests []model.Request
}

func (c *nativeToolBudgetClient) Generate(ctx context.Context, req model.Request) (model.Response, error) {
	c.requests = append(c.requests, req)
	if len(c.requests) == 1 {
		return model.Response{
			Provider: "openaiCompatible",
			Model:    "fake-native",
			ToolCalls: []model.ToolCall{{
				ID:        "call_1",
				Name:      "write",
				Arguments: json.RawMessage(`{"path":"notes.md","content":"budget note"}`),
			}},
		}, nil
	}
	return model.Response{
		Provider: "openaiCompatible",
		Model:    "fake-native",
		Text:     "done after tool budget",
	}, nil
}

func TestRuntimeUsesNativeToolCalls(t *testing.T) {
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "workspace")
	box, err := sandbox.NewLocal(workspace)
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	registry := tools.NewRegistry()
	if err := registry.Register(builtin.NewFileWrite(tools.Spec{
		Name:         "write",
		Description:  "Write a file",
		Capabilities: []string{"workspaceWrite"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
			"required": []string{"path", "content"},
		},
	}, box)); err != nil {
		t.Fatalf("register write failed: %v", err)
	}

	client := &nativeFakeClient{}
	models := model.NewRegistry()
	models.Add("primary", model.ProviderConfig{
		Name:           "primary",
		Provider:       "openaiCompatible",
		Model:          "fake-native",
		ToolCalling:    true,
		JSONSchemaMode: true,
	}, client)

	agent := &compiler.CompiledAgent{
		Name:         "native",
		Instructions: "Write the requested note.",
		Config: config.AgentManifest{
			Metadata: config.Metadata{Name: "native"},
			Runtime: config.RuntimeConfig{
				Model: "primary",
				Limits: config.RuntimeLimits{
					MaxSteps:             4,
					MaxDurationSec:       30,
					MaxToolCalls:         4,
					MaxConsecutiveErrors: 2,
				},
			},
			Permissions: config.PermissionsConfig{Access: "workspace", Approval: "never"},
		},
		ConfigSnapshot: []byte("apiVersion: jeju/v1alpha1\nkind: Agent\n"),
		Models:         models,
		Tools:          registry,
		Skills:         skills.NewRegistry(),
		Memory:         memory.Noop{},
		Sandbox:        box,
		Policy:         policy.NewGate(config.PermissionsConfig{Access: "workspace", Approval: "never"}),
		RunStore:       runs.NewStore(filepath.Join(tmp, "runs")),
	}

	result, err := New().Run(context.Background(), agent, "save a note")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Status != StatusCompleted || result.Final != "done" {
		t.Fatalf("unexpected result: %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "notes.md"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(data) != "hello from native tools" {
		t.Fatalf("unexpected file content %q", data)
	}
	if len(client.requests) != 2 {
		t.Fatalf("expected two model requests, got %d", len(client.requests))
	}
	if len(client.requests[0].Tools) == 0 {
		t.Fatal("first request did not include function tools")
	}
	if hasToolDefinition(client.requests[0].Tools, "ask_user") || hasToolDefinition(client.requests[0].Tools, "final_answer") {
		t.Fatalf("first request should not include Jeju control tools: %+v", client.requests[0].Tools)
	}
	second := client.requests[1].Messages
	if len(second) < 4 || second[len(second)-1].Role != "tool" || second[len(second)-1].ToolCallID != "call_1" {
		t.Fatalf("second request did not replay tool result correctly: %+v", second)
	}
	if second[len(second)-2].ReasoningContent != "I should write the requested note." {
		t.Fatalf("second request did not replay reasoning content: %+v", second[len(second)-2])
	}
	if second[len(second)-2].Content != "I will write the requested note before finishing." {
		t.Fatalf("second request did not replay assistant content from tool-call turn: %+v", second[len(second)-2])
	}
}

func TestRuntimeGivesNativeFinalChanceAfterToolBudget(t *testing.T) {
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "workspace")
	box, err := sandbox.NewLocal(workspace)
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	registry := tools.NewRegistry()
	if err := registry.Register(builtin.NewFileWrite(tools.Spec{
		Name:         "write",
		Description:  "Write a file",
		Capabilities: []string{"workspaceWrite"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
			"required": []string{"path", "content"},
		},
	}, box)); err != nil {
		t.Fatalf("register write failed: %v", err)
	}

	client := &nativeToolBudgetClient{}
	models := model.NewRegistry()
	models.Add("primary", model.ProviderConfig{
		Name:     "primary",
		Provider: "openaiCompatible",
		Model:    "fake-native",

		JSONMode:    true,
		ToolCalling: true,
	}, client)

	agent := &compiler.CompiledAgent{
		Name:         "native-budget",
		Instructions: "Write once, then finish.",
		Config: config.AgentManifest{
			Metadata: config.Metadata{Name: "native-budget"},
			Runtime: config.RuntimeConfig{
				Model: "primary",
				Limits: config.RuntimeLimits{
					MaxSteps:             3,
					MaxDurationSec:       30,
					MaxToolCalls:         1,
					MaxConsecutiveErrors: 2,
				},
			},
			Permissions: config.PermissionsConfig{Access: "workspace", Approval: "never"},
		},
		ConfigSnapshot: []byte("apiVersion: jeju/v1alpha1\nkind: Agent\n"),
		Models:         models,
		Tools:          registry,
		Skills:         skills.NewRegistry(),
		Memory:         memory.Noop{},
		Sandbox:        box,
		Policy:         policy.NewGate(config.PermissionsConfig{Access: "workspace", Approval: "never"}),
		RunStore:       runs.NewStore(filepath.Join(tmp, "runs")),
	}

	result, err := New().Run(context.Background(), agent, "save a note")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Status != StatusCompleted || result.Final != "done after tool budget" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(client.requests) != 2 {
		t.Fatalf("expected two model requests, got %d", len(client.requests))
	}
	if len(client.requests[0].Tools) == 0 {
		t.Fatal("first request should expose tools")
	}
	if len(client.requests[1].Tools) != 0 {
		t.Fatalf("tool budget final request should not expose tools: %+v", client.requests[1].Tools)
	}
	if client.requests[1].ResponseFormat == nil {
		t.Fatal("tool budget final request should restore JSON response format for JSON-mode providers")
	}
	last := client.requests[1].Messages[len(client.requests[1].Messages)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "Tool budget exhausted") {
		t.Fatalf("tool budget final request missing final instruction: %+v", last)
	}
}

func TestRuntimeReturnsNativeFeedbackForEmptyFinalResponse(t *testing.T) {
	tmp := t.TempDir()
	box, err := sandbox.NewLocal(filepath.Join(tmp, "workspace"))
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	client := &nativeEmptyFinalClient{}
	models := model.NewRegistry()
	models.Add("primary", model.ProviderConfig{
		Name:           "primary",
		Provider:       "openaiCompatible",
		Model:          "fake-native",
		ToolCalling:    true,
		JSONSchemaMode: true,
	}, client)

	agent := &compiler.CompiledAgent{
		Name:         "native-invalid-final",
		Instructions: "Review the changes.",
		Config: config.AgentManifest{
			Metadata: config.Metadata{Name: "native-invalid-final"},
			Runtime: config.RuntimeConfig{
				Model: "primary",
				Limits: config.RuntimeLimits{
					MaxSteps:             3,
					MaxDurationSec:       30,
					MaxToolCalls:         2,
					MaxConsecutiveErrors: 2,
				},
			},
			Permissions: config.PermissionsConfig{Access: "readOnly", Approval: "never"},
		},
		ConfigSnapshot: []byte("apiVersion: jeju/v1alpha1\nkind: Agent\n"),
		Models:         models,
		Tools:          tools.NewRegistry(),
		Skills:         skills.NewRegistry(),
		Memory:         memory.Noop{},
		Sandbox:        box,
		Policy:         policy.NewGate(config.PermissionsConfig{Access: "readOnly", Approval: "never"}),
		RunStore:       runs.NewStore(filepath.Join(tmp, "runs")),
	}

	result, err := New().Run(context.Background(), agent, "finish")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Status != StatusCompleted || result.Final != "done after retry" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(client.requests) != 2 {
		t.Fatalf("expected two model requests, got %d", len(client.requests))
	}
	second := client.requests[1].Messages
	if len(second) < 3 {
		t.Fatalf("expected retry context to include empty final feedback, got %+v", second)
	}
	assistant := second[len(second)-2]
	feedback := second[len(second)-1]
	if assistant.Role != "assistant" || assistant.Content != "" || len(assistant.ToolCalls) != 0 {
		t.Fatalf("empty native final response was not replayed: %+v", assistant)
	}
	if feedback.Role != "user" {
		t.Fatalf("empty final feedback should be a user observation: %+v", feedback)
	}
	if !strings.Contains(feedback.Content, "non-empty final response") {
		t.Fatalf("feedback did not explain parse failure: %q", feedback.Content)
	}
}

func TestRuntimeAutoApproveDoesNotBypassPolicyDeny(t *testing.T) {
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "workspace")
	box, err := sandbox.NewLocal(workspace)
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	registry := tools.NewRegistry()
	if err := registry.Register(builtin.NewFileWrite(tools.Spec{
		Name:         "write",
		Description:  "Write a file",
		Capabilities: []string{"workspaceWrite"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
			"required": []string{"path", "content"},
		},
	}, box)); err != nil {
		t.Fatalf("register write failed: %v", err)
	}

	client := &nativeFakeClient{}
	models := model.NewRegistry()
	models.Add("primary", model.ProviderConfig{
		Name:           "primary",
		Provider:       "openaiCompatible",
		Model:          "fake-native",
		ToolCalling:    true,
		JSONSchemaMode: true,
	}, client)

	permissions := config.PermissionsConfig{Access: "readOnly", Approval: "never"}
	agent := &compiler.CompiledAgent{
		Name:         "native",
		Instructions: "Write the requested note.",
		Config: config.AgentManifest{
			Metadata: config.Metadata{Name: "native"},
			Runtime: config.RuntimeConfig{
				Model: "primary",
				Limits: config.RuntimeLimits{
					MaxSteps:             4,
					MaxDurationSec:       30,
					MaxToolCalls:         4,
					MaxConsecutiveErrors: 2,
				},
			},
			Permissions: permissions,
		},
		ConfigSnapshot: []byte("apiVersion: jeju/v1alpha1\nkind: Agent\n"),
		Models:         models,
		Tools:          registry,
		Skills:         skills.NewRegistry(),
		Memory:         memory.Noop{},
		Sandbox:        box,
		Policy:         policy.NewGate(permissions),
		RunStore:       runs.NewStore(filepath.Join(tmp, "runs")),
	}

	result, err := NewWithOptions(Options{AutoApprove: true}).Run(context.Background(), agent, "save a note")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Status != StatusCompleted || result.Final != "done" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(workspace, "notes.md")); !os.IsNotExist(err) {
		t.Fatalf("policy-denied write should not create notes.md, stat error: %v", err)
	}
	events, err := trajectory.ReadFile(filepath.Join(tmp, "runs", result.RunID, runs.TrajectoryFile))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !hasPermissionDecision(events, "denied") {
		t.Fatalf("expected permission.denied event, got %+v", events)
	}
	if hasPermissionDecision(events, "approved") {
		t.Fatalf("did not expect permission.approved for denied tool call: %+v", events)
	}
	if hasToolSpanStatus(events, string(trajectory.SpanStatusOK)) {
		t.Fatalf("did not expect tool.completed for denied tool call: %+v", events)
	}
}

func TestRuntimeReplaysAllNativeToolCalls(t *testing.T) {
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "workspace")
	box, err := sandbox.NewLocal(workspace)
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	registry := tools.NewRegistry()
	if err := registry.Register(builtin.NewFileWrite(tools.Spec{
		Name:         "write",
		Description:  "Write a file",
		Capabilities: []string{"workspaceWrite"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
			"required": []string{"path", "content"},
		},
	}, box)); err != nil {
		t.Fatalf("register write failed: %v", err)
	}

	client := &nativeMultiToolClient{}
	models := model.NewRegistry()
	models.Add("primary", model.ProviderConfig{
		Name:           "primary",
		Provider:       "openaiCompatible",
		Model:          "fake-native",
		ToolCalling:    true,
		JSONSchemaMode: true,
	}, client)

	agent := &compiler.CompiledAgent{
		Name:         "native-multi",
		Instructions: "Write the requested notes.",
		Config: config.AgentManifest{
			Metadata: config.Metadata{Name: "native-multi"},
			Runtime: config.RuntimeConfig{
				Model: "primary",
				Limits: config.RuntimeLimits{
					MaxSteps:             4,
					MaxDurationSec:       30,
					MaxToolCalls:         4,
					MaxConsecutiveErrors: 2,
				},
			},
			Permissions: config.PermissionsConfig{Access: "workspace", Approval: "never"},
		},
		ConfigSnapshot: []byte("apiVersion: jeju/v1alpha1\nkind: Agent\n"),
		Models:         models,
		Tools:          registry,
		Skills:         skills.NewRegistry(),
		Memory:         memory.Noop{},
		Sandbox:        box,
		Policy:         policy.NewGate(config.PermissionsConfig{Access: "workspace", Approval: "never"}),
		RunStore:       runs.NewStore(filepath.Join(tmp, "runs")),
	}

	result, err := New().Run(context.Background(), agent, "save two notes")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Status != StatusCompleted || result.Final != "done" {
		t.Fatalf("unexpected result: %+v", result)
	}
	for name, want := range map[string]string{"one.md": "one", "two.md": "two"} {
		data, err := os.ReadFile(filepath.Join(workspace, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(data) != want {
			t.Fatalf("unexpected %s content %q", name, data)
		}
	}
	if len(client.requests) != 2 {
		t.Fatalf("expected two model requests, got %d", len(client.requests))
	}
	second := client.requests[1].Messages
	if len(second) < 5 {
		t.Fatalf("expected replayed assistant and tool messages, got %+v", second)
	}
	lastTwo := second[len(second)-2:]
	if lastTwo[0].Role != "tool" || lastTwo[0].ToolCallID != "call_1" {
		t.Fatalf("first tool result was not replayed: %+v", lastTwo[0])
	}
	if lastTwo[1].Role != "tool" || lastTwo[1].ToolCallID != "call_2" {
		t.Fatalf("second tool result was not replayed: %+v", lastTwo[1])
	}
	if got := second[len(second)-3]; got.Role != "assistant" || got.Content != "parallel writes" || len(got.ToolCalls) != 2 {
		t.Fatalf("assistant multi tool call was not replayed: %+v", got)
	}
	events, err := trajectory.ReadFile(filepath.Join(tmp, "runs", result.RunID, runs.TrajectoryFile))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	record := trajectory.Project(events)
	for _, id := range []string{
		"art_step001_tool_output_write_call_1",
		"art_step001_tool_output_write_call_2",
	} {
		if _, ok := record.Artifacts[id]; !ok {
			t.Fatalf("expected distinct tool output artifact %s", id)
		}
	}
}

func hasPermissionDecision(events []trajectory.Event, decision string) bool {
	for _, event := range events {
		if event.Type == trajectory.EventPermissionDecided {
			if value, ok := event.Payload["decision"].(string); ok && value == decision {
				return true
			}
		}
	}
	return false
}

func hasToolSpanStatus(events []trajectory.Event, status string) bool {
	for _, event := range events {
		if event.Type != trajectory.EventSpanEnded {
			continue
		}
		if kind, _ := event.Payload["kind"].(string); kind != string(trajectory.SpanTool) {
			continue
		}
		if value, _ := event.Payload["status"].(string); value == status {
			return true
		}
	}
	return false
}

func hasToolDefinition(defs []model.ToolDefinition, name string) bool {
	for _, def := range defs {
		if def.Name == name {
			return true
		}
	}
	return false
}
