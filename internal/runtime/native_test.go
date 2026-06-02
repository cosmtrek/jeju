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
		ToolCalls: []model.ToolCall{{
			ID:        "call_2",
			Name:      "final_answer",
			Arguments: json.RawMessage(`{"content":"done"}`),
		}},
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
		ToolCalls: []model.ToolCall{{
			ID:        "call_3",
			Name:      "final_answer",
			Arguments: json.RawMessage(`{"content":"done"}`),
		}},
	}, nil
}

type nativeInvalidFinalClient struct {
	requests []model.Request
}

func (c *nativeInvalidFinalClient) Generate(ctx context.Context, req model.Request) (model.Response, error) {
	c.requests = append(c.requests, req)
	if len(c.requests) == 1 {
		return model.Response{
			Provider: "openaiCompatible",
			Model:    "fake-native",
			ToolCalls: []model.ToolCall{{
				ID:        "call_bad_final",
				Name:      "final_answer",
				Arguments: json.RawMessage(`{"content":"unterminated`),
			}},
		}, nil
	}
	return model.Response{
		Provider: "openaiCompatible",
		Model:    "fake-native",
		ToolCalls: []model.ToolCall{{
			ID:        "call_good_final",
			Name:      "final_answer",
			Arguments: json.RawMessage(`{"content":"done after retry"}`),
		}},
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
	if !hasToolDefinition(client.requests[0].Tools, "final_answer") {
		t.Fatalf("first request did not include final_answer tool: %+v", client.requests[0].Tools)
	}
	second := client.requests[1].Messages
	if len(second) < 4 || second[len(second)-1].Role != "tool" || second[len(second)-1].ToolCallID != "call_1" {
		t.Fatalf("second request did not replay tool result correctly: %+v", second)
	}
	if second[len(second)-2].ReasoningContent != "I should write the requested note." {
		t.Fatalf("second request did not replay reasoning content: %+v", second[len(second)-2])
	}
}

func TestRuntimeReturnsNativeToolFeedbackForInvalidFinalAnswer(t *testing.T) {
	tmp := t.TempDir()
	box, err := sandbox.NewLocal(filepath.Join(tmp, "workspace"))
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	client := &nativeInvalidFinalClient{}
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
		t.Fatalf("expected retry context to include invalid tool call feedback, got %+v", second)
	}
	assistant := second[len(second)-2]
	feedback := second[len(second)-1]
	if assistant.Role != "assistant" || len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "call_bad_final" {
		t.Fatalf("invalid final_answer call was not replayed: %+v", assistant)
	}
	if feedback.Role != "tool" || feedback.ToolCallID != "call_bad_final" {
		t.Fatalf("invalid final_answer feedback was not a tool response: %+v", feedback)
	}
	if !strings.Contains(feedback.Content, "invalid JSON arguments") {
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
	if !hasRuntimeEventType(events, trajectory.EventPermissionDenied) {
		t.Fatalf("expected permission.denied event, got %+v", events)
	}
	if hasRuntimeEventType(events, trajectory.EventPermissionApproved) {
		t.Fatalf("did not expect permission.approved for denied tool call: %+v", events)
	}
	if hasRuntimeEventType(events, trajectory.EventToolCompleted) {
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
	if got := second[len(second)-3]; got.Role != "assistant" || len(got.ToolCalls) != 2 {
		t.Fatalf("assistant multi tool call was not replayed: %+v", got)
	}
	for _, name := range []string{
		"step001_tool_output_write_call_1.json",
		"step001_tool_output_write_call_2.json",
	} {
		if _, err := os.Stat(filepath.Join(tmp, "runs", result.RunID, "artifacts", name)); err != nil {
			t.Fatalf("expected distinct tool output artifact %s: %v", name, err)
		}
	}
}

func hasToolDefinition(defs []model.ToolDefinition, name string) bool {
	for _, def := range defs {
		if def.Name == name {
			return true
		}
	}
	return false
}
