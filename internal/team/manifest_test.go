package team

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFileDefaultsAndResolvesPaths(t *testing.T) {
	root := t.TempDir()
	agentPath := filepath.Join(root, "agent.yaml")
	if err := os.WriteFile(agentPath, []byte("placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}
	teamPath := filepath.Join(root, "team.yaml")
	if err := os.WriteFile(teamPath, []byte(`apiVersion: jeju/v1alpha1
kind: AgentTeam
metadata:
  name: research-team
lead:
  agent: agent.yaml
workers:
  reviewer:
    agent: agent.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest, _, err := LoadFile(teamPath)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if manifest.Kind != KindAgentTeam {
		t.Fatalf("kind = %q", manifest.Kind)
	}
	if manifest.Runtime.Topology != TopologyLeadWorker {
		t.Fatalf("topology = %q", manifest.Runtime.Topology)
	}
	if manifest.Runtime.MaxRounds != 3 {
		t.Fatalf("maxRounds = %d", manifest.Runtime.MaxRounds)
	}
	if !filepath.IsAbs(manifest.Lead.Agent) {
		t.Fatalf("lead agent path was not resolved: %q", manifest.Lead.Agent)
	}
	if !filepath.IsAbs(manifest.Output.Dir) {
		t.Fatalf("output dir was not resolved: %q", manifest.Output.Dir)
	}
}

func TestLoadFileRequiresVerifierWorkerWhenVerifierGateEnabled(t *testing.T) {
	root := t.TempDir()
	agentPath := filepath.Join(root, "agent.yaml")
	if err := os.WriteFile(agentPath, []byte("placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}
	teamPath := filepath.Join(root, "team.yaml")
	if err := os.WriteFile(teamPath, []byte(`apiVersion: jeju/v1alpha1
kind: AgentTeam
metadata:
  name: research-team
lead:
  agent: agent.yaml
workers:
  quality_gate:
    agent: agent.yaml
verification:
  requireVerifier: true
`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := LoadFile(teamPath)
	if err == nil {
		t.Fatal("LoadFile() error = nil, want missing verifier worker error")
	}
	if !strings.Contains(err.Error(), `worker named "verifier"`) {
		t.Fatalf("error = %q, want verifier worker requirement", err)
	}
}

func TestLoadFilePreservesExplicitZeroRuntimeLimits(t *testing.T) {
	root := t.TempDir()
	agentPath := filepath.Join(root, "agent.yaml")
	if err := os.WriteFile(agentPath, []byte("placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}
	teamPath := filepath.Join(root, "team.yaml")
	if err := os.WriteFile(teamPath, []byte(`apiVersion: jeju/v1alpha1
kind: AgentTeam
metadata:
  name: research-team
lead:
  agent: agent.yaml
workers:
  reviewer:
    agent: agent.yaml
runtime:
  maxRetriesPerTask: 0
  maxConsecutiveEmptyRounds: 0
`), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest, snapshot, err := LoadFile(teamPath)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if manifest.Runtime.MaxRetriesPerTask != 0 {
		t.Fatalf("maxRetriesPerTask = %d, want explicit zero", manifest.Runtime.MaxRetriesPerTask)
	}
	if manifest.Runtime.MaxConsecutiveEmptyRounds != 0 {
		t.Fatalf("maxConsecutiveEmptyRounds = %d, want explicit zero", manifest.Runtime.MaxConsecutiveEmptyRounds)
	}
	if !strings.Contains(string(snapshot), "maxRetriesPerTask: 0") {
		t.Fatalf("snapshot should preserve explicit maxRetriesPerTask zero:\n%s", snapshot)
	}
	if !strings.Contains(string(snapshot), "maxConsecutiveEmptyRounds: 0") {
		t.Fatalf("snapshot should preserve explicit maxConsecutiveEmptyRounds zero:\n%s", snapshot)
	}
}
