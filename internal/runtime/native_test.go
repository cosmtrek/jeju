package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"jeju/internal/compiler"
	"jeju/internal/config"
	"jeju/internal/memory"
	"jeju/internal/model"
	"jeju/internal/policy"
	"jeju/internal/runs"
	"jeju/internal/sandbox"
	"jeju/internal/skills"
	"jeju/internal/tools"
	"jeju/internal/tools/builtin"
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

func hasToolDefinition(defs []model.ToolDefinition, name string) bool {
	for _, def := range defs {
		if def.Name == name {
			return true
		}
	}
	return false
}
