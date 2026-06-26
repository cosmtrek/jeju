package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	agenttool "github.com/cosmtrek/jeju/internal/tools/agent"
	"github.com/cosmtrek/jeju/internal/trajectory"
)

type agentToolParentClient struct {
	requests []model.Request
}

func (c *agentToolParentClient) Generate(ctx context.Context, req model.Request) (model.Response, error) {
	c.requests = append(c.requests, req)
	if len(c.requests) == 1 {
		return model.Response{
			Provider: "openaiCompatible",
			Model:    "parent",
			ToolCalls: []model.ToolCall{{
				ID:        "call_agent_1",
				Name:      "ask_retriever",
				Arguments: json.RawMessage(`{"task":"Summarize the delegated evidence.","context":"Parent run context.","expected_output":"Short markdown."}`),
			}},
			Usage: model.Usage{InputTokens: 10, OutputTokens: 4, TotalTokens: 14},
		}, nil
	}
	return model.Response{
		Provider: "openaiCompatible",
		Model:    "parent",
		Text:     "parent final after child",
		Usage:    model.Usage{InputTokens: 8, OutputTokens: 5, TotalTokens: 13},
	}, nil
}

func TestRuntimeRunsAgentToolAsChildRun(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	childManifest := writeRuntimeAgentToolChild(t, root)

	box, err := sandbox.NewLocal(workspace)
	if err != nil {
		t.Fatal(err)
	}
	registry := tools.NewRegistry()
	if err := registry.Register(agenttool.New(tools.Spec{
		Name:         "ask_retriever",
		Description:  "Run retriever child.",
		Capabilities: []string{"agentRun"},
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"task": map[string]any{"type": "string"}},
			"required":   []string{"task"},
		},
	})); err != nil {
		t.Fatal(err)
	}
	models := model.NewRegistry()
	client := &agentToolParentClient{}
	models.Add("primary", model.ProviderConfig{
		Name:            "primary",
		Model:           "parent",
		ContextWindow:   8000,
		MaxOutputTokens: 512,
		ToolCalling:     true,
	}, client)
	runStore := runs.NewStore(filepath.Join(root, "runs"))
	parent := &compiler.CompiledAgent{
		Name:         "parent",
		Instructions: "Parent test agent.",
		Config: config.AgentManifest{
			Runtime: config.RuntimeConfig{
				Model:                "primary",
				Loop:                 config.LoopConfig{Type: "react"},
				CompressionThreshold: 0.8,
				RecentTokenBudget:    2000,
				Limits:               config.RuntimeLimits{MaxSteps: 4, MaxToolCalls: 2, MaxConsecutiveErrors: 2},
			},
			Workspace:   config.WorkspaceConfig{Path: workspace},
			Permissions: config.PermissionsConfig{Access: "workspace", Approval: "never"},
		},
		Models:     models,
		Tools:      registry,
		AgentTools: map[string]compiler.AgentToolSpec{"ask_retriever": {Name: "ask_retriever", Manifest: childManifest}},
		Skills:     skills.NewRegistry(),
		Memory:     memory.Noop{},
		Sandbox:    box,
		Policy:     policy.NewGate(config.PermissionsConfig{Access: "workspace", Approval: "never"}),
		RunStore:   runStore,
	}

	result, err := New().Run(context.Background(), parent, "delegate to the retriever")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if result.Status != StatusCompleted {
		t.Fatalf("status = %s, want completed", result.Status)
	}
	events, err := trajectory.ReadFile(filepath.Join(runStore.BasePath, result.RunID, runs.TrajectoryFile))
	if err != nil {
		t.Fatal(err)
	}
	record := trajectory.Project(events)
	if record.Stats.ChildRuns != 1 {
		t.Fatalf("child runs = %d, want 1", record.Stats.ChildRuns)
	}
	if record.Stats.ChildModelCalls == 0 {
		t.Fatalf("expected child model calls in stats: %+v", record.Stats)
	}
	if !hasSpanKind(events, trajectory.SpanTool) || !hasSpanKind(events, trajectory.SpanSubagent) {
		t.Fatalf("expected parent trajectory to include tool and subagent spans")
	}
	if _, err := os.Stat(filepath.Join(runStore.BasePath, result.RunID, "child-runs", "ask_retriever", "call_agent_1")); err != nil {
		t.Fatalf("expected child run store under parent run: %v", err)
	}
}

func writeRuntimeAgentToolChild(t *testing.T, root string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(root, "prompts", "child.md")
	if err := os.WriteFile(promptPath, []byte("You are a child retriever."), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "child.agent.yaml")
	data := `apiVersion: jeju/v1alpha1
kind: Agent
metadata:
  name: retriever
models:
  providers:
    primary:
      type: mock
      model: mock-react
instructions:
  system: prompts/child.md
runtime:
  loop:
    type: react
  limits:
    maxSteps: 2
    maxToolCalls: 0
    maxConsecutiveErrors: 2
workspace:
  path: child-workspace
permissions:
  access: workspace
  approval: never
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func hasSpanKind(events []trajectory.Event, kind trajectory.SpanKind) bool {
	for _, event := range events {
		if event.Type != trajectory.EventSpanEnded {
			continue
		}
		if got, _ := event.Payload["kind"].(string); got == string(kind) {
			return true
		}
	}
	return false
}
