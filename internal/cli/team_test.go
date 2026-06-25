package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTeamRunReturnsErrorForFailedStatus(t *testing.T) {
	root := t.TempDir()
	agentPath := filepath.Join(root, "bad.agent.yaml")
	if err := os.WriteFile(agentPath, []byte("apiVersion: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	teamPath := filepath.Join(root, "team.yaml")
	outDir := filepath.Join(root, ".jeju-dev", "team", "failed-team")
	teamYAML := "apiVersion: jeju/v1alpha1\n" +
		"kind: AgentTeam\n" +
		"metadata:\n" +
		"  name: failed-team\n" +
		"lead:\n" +
		"  agent: " + agentPath + "\n" +
		"workers:\n" +
		"  worker:\n" +
		"    agent: " + agentPath + "\n" +
		"output:\n" +
		"  dir: " + outDir + "\n"
	if err := os.WriteFile(teamPath, []byte(teamYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Execute(context.Background(), []string{"team", "run", "--output", "final", teamPath, "trigger compile failure"})
	if err == nil {
		t.Fatal("Execute() error = nil, want failed team run error")
	}
	if !strings.Contains(err.Error(), "team run failed") {
		t.Fatalf("error = %q, want team run failed", err.Error())
	}
}

func TestTeamHelpPrintsRunSubcommandPath(t *testing.T) {
	output := captureStdout(t, func() {
		if err := Execute(context.Background(), []string{"team", "--help"}); err != nil {
			t.Fatalf("team help failed: %v", err)
		}
	})
	want := `jeju team run [--workspace <dir>] [--out <dir>] [--output live|final] <team.yaml> "<goal>"`
	if !strings.Contains(output, want) {
		t.Fatalf("team help output missing %q:\n%s", want, output)
	}
	if strings.Contains(output, `jeju run [--workspace <dir>] [--out <dir>]`) {
		t.Fatalf("team help output points at jeju run:\n%s", output)
	}
}

func TestTeamRejectsRunFlagsOnParentCommand(t *testing.T) {
	err := Execute(context.Background(), []string{"team", "--workspace", "/tmp/project", "team.yaml", "review"})
	if err == nil {
		t.Fatal("expected team parent flag error")
	}
	want := `team flags must appear on the run subcommand; use: jeju team run [--workspace <dir>]`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestTeamRunRejectsRunFlagAfterManifest(t *testing.T) {
	err := Execute(context.Background(), []string{"team", "run", "team.yaml", "--output", "final", "review"})
	if err == nil {
		t.Fatal("expected misplaced team run flag error")
	}
	want := "team run flags must appear before <team.yaml>"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestValidateAcceptsAgentTeamAndExplainsResolvedWiring(t *testing.T) {
	root := t.TempDir()
	leadPath := writeValidAgentManifest(t, root, "lead")
	workerPath := writeValidAgentManifest(t, root, "worker")
	teamPath := filepath.Join(root, "team.yaml")
	if err := os.WriteFile(teamPath, []byte(`apiVersion: jeju/v1alpha1
kind: AgentTeam
metadata:
  name: research-team
  description: "Research team."
lead:
  agent: lead.agent.yaml
workers:
  reviewer:
    agent: worker.agent.yaml
    maxTasks: 2
runtime:
  maxRounds: 2
verification:
  requireStructuredTaskOutput: true
  requiredTaskFields:
    - summary
output:
  dir: `+filepath.Join(root, ".jeju-dev", "team", "research-team")+`
`), 0o644); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := Execute(context.Background(), []string{"validate", "--explain", teamPath}); err != nil {
			t.Fatalf("validate team failed: %v", err)
		}
	})
	for _, expected := range []string{
		"valid: " + teamPath,
		"Manifest: research-team (AgentTeam jeju/v1alpha1)",
		"lead.agent -> " + leadPath,
		"workers.reviewer -> agent=" + workerPath + ", maxTasks=2",
		"runtime.topology -> lead_worker",
		"verification.requiredTaskFields -> [summary]",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("validate --explain output missing %q:\n%s", expected, output)
		}
	}
}

func TestValidateAgentTeamValidatesReferencedWorkerAgents(t *testing.T) {
	root := t.TempDir()
	writeValidAgentManifest(t, root, "lead")
	badWorkerPath := filepath.Join(root, "bad-worker.agent.yaml")
	if err := os.WriteFile(badWorkerPath, []byte("apiVersion: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	teamPath := filepath.Join(root, "team.yaml")
	if err := os.WriteFile(teamPath, []byte(`apiVersion: jeju/v1alpha1
kind: AgentTeam
metadata:
  name: research-team
lead:
  agent: lead.agent.yaml
workers:
  reviewer:
    agent: bad-worker.agent.yaml
output:
  dir: `+filepath.Join(root, ".jeju-dev", "team", "research-team")+`
`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Execute(context.Background(), []string{"validate", teamPath})
	if err == nil {
		t.Fatal("Execute() error = nil, want invalid worker agent error")
	}
	if !strings.Contains(err.Error(), "workers.reviewer.agent") {
		t.Fatalf("error = %q, want worker agent context", err.Error())
	}
}

func writeValidAgentManifest(t *testing.T, root, name string) string {
	t.Helper()
	promptPath := filepath.Join(root, name+".md")
	if err := os.WriteFile(promptPath, []byte("You are a test agent."), 0o644); err != nil {
		t.Fatal(err)
	}
	workspacePath := filepath.Join(root, "workspace", name)
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(root, name+".agent.yaml")
	if err := os.WriteFile(agentPath, []byte(`apiVersion: jeju/v1alpha1
kind: Agent
metadata:
  name: `+name+`
models:
  providers:
    mock:
      type: mock
      model: mock
instructions:
  system: `+promptPath+`
workspace:
  path: `+workspacePath+`
permissions:
  access: readOnly
  approval: never
`), 0o644); err != nil {
		t.Fatal(err)
	}
	return agentPath
}
