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
