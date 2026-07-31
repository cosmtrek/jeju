package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cosmtrek/jeju/internal/config"
)

func TestCompileToolSpecDefaultsBuiltinDescriptions(t *testing.T) {
	for _, tc := range []struct {
		name string
		uses string
	}{
		{name: "read", uses: "builtin:read"},
		{name: "write", uses: "builtin:write"},
		{name: "edit", uses: "builtin:edit"},
		{name: "search", uses: "builtin:search"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := compileToolSpec(config.ToolConfig{Name: tc.name, Uses: tc.uses})
			if err != nil {
				t.Fatalf("compileToolSpec failed: %v", err)
			}
			if strings.TrimSpace(spec.Description) == "" {
				t.Fatalf("%s default description is empty", tc.uses)
			}
		})
	}
}

func TestCompileAgentToolValidatesChildAndRegistersVirtualTool(t *testing.T) {
	root := t.TempDir()
	parentPath := writeCompilerTestAgent(t, root, "parent", "child.yaml", false)
	writeCompilerTestAgent(t, root, "child", "", false)

	agent, err := CompileWithOptions(parentPath, Options{WorkspaceOverride: filepath.Join(root, "workspace")})
	if err != nil {
		t.Fatalf("CompileWithOptions failed: %v", err)
	}
	if _, ok := agent.AgentTools["ask_child"]; !ok {
		t.Fatalf("compiled agent missing ask_child agent tool: %+v", agent.AgentTools)
	}
	tool, ok := agent.Tools.Get("ask_child")
	if !ok {
		t.Fatal("virtual agent tool was not registered")
	}
	if got := tool.Spec().Capabilities; len(got) != 1 || got[0] != "agentRun" {
		t.Fatalf("agent tool capabilities = %v, want [agentRun]", got)
	}
}

func TestCompileAgentToolRejectsNestedAgentTools(t *testing.T) {
	root := t.TempDir()
	parentPath := writeCompilerTestAgent(t, root, "parent", "child.yaml", false)
	writeCompilerTestAgent(t, root, "child", "grandchild.yaml", false)
	writeCompilerTestAgent(t, root, "grandchild", "", false)

	_, err := CompileWithOptions(parentPath, Options{WorkspaceOverride: filepath.Join(root, "workspace")})
	if err == nil || !strings.Contains(err.Error(), "must not declare nested agent tool") {
		t.Fatalf("CompileWithOptions error = %v, want nested agent tool rejection", err)
	}
}

func TestCompileAgentToolRejectsTeamChild(t *testing.T) {
	root := t.TempDir()
	parentPath := writeCompilerTestAgent(t, root, "parent", "child.yaml", false)
	writeCompilerTestAgent(t, root, "child", "", true)

	_, err := CompileWithOptions(parentPath, Options{WorkspaceOverride: filepath.Join(root, "workspace")})
	if err == nil || !strings.Contains(err.Error(), "must be kind Agent") {
		t.Fatalf("CompileWithOptions error = %v, want kind Agent rejection", err)
	}
}

func TestCompileWithOptionsOverridesActiveModel(t *testing.T) {
	root := t.TempDir()
	manifestPath := writeCompilerTestAgent(t, root, "model-override", "", false)
	original, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}

	agent, err := CompileWithOptions(manifestPath, Options{ModelOverride: "mock-candidate"})
	if err != nil {
		t.Fatalf("CompileWithOptions failed: %v", err)
	}
	if got := agent.Config.Models.Providers["primary"].Model; got != "mock-candidate" {
		t.Fatalf("effective model = %q, want mock-candidate", got)
	}
	_, provider, ok := agent.Models.Get("primary")
	if !ok {
		t.Fatal("compiled model registry missing primary")
	}
	if provider.Model != "mock-candidate" {
		t.Fatalf("compiled provider model = %q, want mock-candidate", provider.Model)
	}
	if !strings.Contains(string(agent.ConfigSnapshot), "model: mock-candidate") {
		t.Fatalf("config snapshot missing model override:\n%s", agent.ConfigSnapshot)
	}
	after, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatal("model override modified the source manifest")
	}
}

func TestCompileWithOptionsOverridesOnlyRuntimeProvider(t *testing.T) {
	root := t.TempDir()
	manifestPath := writeCompilerTestAgent(t, root, "multi-model", "", false)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data),
		`    primary:
      type: mock
      model: mock-react`,
		`    primary:
      type: mock
      model: mock-react
    judge:
      type: mock
      model: mock-judge`,
		1,
	)
	updated = strings.Replace(updated,
		`runtime:
  loop:`,
		`runtime:
  model: primary
  loop:`,
		1,
	)
	if err := os.WriteFile(manifestPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}

	agent, err := CompileWithOptions(manifestPath, Options{ModelOverride: "mock-candidate"})
	if err != nil {
		t.Fatalf("CompileWithOptions failed: %v", err)
	}
	if got := agent.Config.Models.Providers["primary"].Model; got != "mock-candidate" {
		t.Fatalf("primary model = %q, want mock-candidate", got)
	}
	if got := agent.Config.Models.Providers["judge"].Model; got != "mock-judge" {
		t.Fatalf("judge model = %q, want mock-judge", got)
	}
}

func writeCompilerTestAgent(t *testing.T, root, name, child string, teamKind bool) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(root, "prompts", name+".md")
	if err := os.WriteFile(promptPath, []byte("You are "+name+"."), 0o644); err != nil {
		t.Fatal(err)
	}
	kind := "Agent"
	if teamKind {
		kind = "AgentTeam"
	}
	tools := ""
	if child != "" {
		tools = `
tools:
  - name: ask_child
    uses: agent
    agent:
      manifest: ` + child + `
`
	}
	path := filepath.Join(root, name+".yaml")
	data := `apiVersion: jeju/v1alpha1
kind: ` + kind + `
metadata:
  name: ` + name + `
models:
  providers:
    primary:
      type: mock
      model: mock-react
instructions:
  system: prompts/` + name + `.md
runtime:
  loop:
    type: react
workspace:
  path: workspace
permissions:
  access: workspace
  approval: never
` + tools
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCompileToolSpecKeepsExplicitDescription(t *testing.T) {
	spec, err := compileToolSpec(config.ToolConfig{
		Name:        "write",
		Uses:        "builtin:write",
		Description: "Project-specific write guidance.",
	})
	if err != nil {
		t.Fatalf("compileToolSpec failed: %v", err)
	}
	if spec.Description != "Project-specific write guidance." {
		t.Fatalf("description = %q, want explicit manifest description", spec.Description)
	}
}
