package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFileResolvesInputSchemaRelativeToManifest(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agents")
	schemaDir := filepath.Join(dir, "schemas")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll agents failed: %v", err)
	}
	if err := os.MkdirAll(schemaDir, 0o755); err != nil {
		t.Fatalf("MkdirAll schemas failed: %v", err)
	}
	schemaPath := filepath.Join(schemaDir, "tool.json")
	if err := os.WriteFile(schemaPath, []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatalf("WriteFile schema failed: %v", err)
	}
	manifestPath := filepath.Join(agentDir, "agent.yaml")
	if err := os.WriteFile(manifestPath, []byte(`apiVersion: jeju/v1alpha1
kind: Agent
metadata:
  name: test
models:
  providers:
    primary:
      type: mock
      model: mock-react
instructions:
  system: ../prompts/system.md
runtime:
  loop:
    type: react
workspace:
  path: ../workspace
tools:
  - name: custom
    uses: command
    command:
      run: ../tools/custom
    input:
      schema: ../schemas/tool.json
`), 0o644); err != nil {
		t.Fatalf("WriteFile manifest failed: %v", err)
	}

	manifest, _, err := LoadFile(manifestPath)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if got := manifest.Tools[0].Input.Schema; got != schemaPath {
		t.Fatalf("expected schema path %q, got %#v", schemaPath, got)
	}
}
