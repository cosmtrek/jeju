package agentpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRejectsUnknownPackageFields(t *testing.T) {
	root := writeValidPackage(t)
	appendFile(t, filepath.Join(root, ManifestFile), "\nusage: forbidden\n")

	_, err := Validate(root, ValidateOptions{})
	if err == nil {
		t.Fatal("expected unknown field error")
	}
	if !strings.Contains(err.Error(), "field usage not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsDeclaredPathsOutsideRootBeforeReading(t *testing.T) {
	root := writeValidPackage(t)
	writeFile(t, filepath.Join(root, "agent.yaml"), strings.Replace(validAgentYAML(), "system: instructions.md", "system: /tmp/outside-instructions.md", 1))

	_, err := Validate(root, ValidateOptions{})
	if err == nil {
		t.Fatal("expected path escape error")
	}
	if !strings.Contains(err.Error(), "instructions.system") || !strings.Contains(err.Error(), "path escapes package root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateResolvesRelativePathsFromPackageRoot(t *testing.T) {
	root := writeValidPackage(t)
	otherCWD := t.TempDir()
	restoreCWD := chdirForAgentpkgTest(t, otherCWD)
	defer restoreCWD()

	if _, err := Validate(root, ValidateOptions{}); err != nil {
		t.Fatalf("Validate should resolve agent relative paths from package root, got: %v", err)
	}
}

func TestValidateRejectsRelativePathEscapingPackageRoot(t *testing.T) {
	root := writeValidPackage(t)
	outside := filepath.Join(filepath.Dir(root), "outside.md")
	writeFile(t, outside, "outside package\n")
	writeFile(t, filepath.Join(root, "agent.yaml"), strings.Replace(validAgentYAML(), "system: instructions.md", "system: ../outside.md", 1))

	_, err := Validate(root, ValidateOptions{})
	if err == nil {
		t.Fatal("expected path escape error")
	}
	if !strings.Contains(err.Error(), "instructions.system") || !strings.Contains(err.Error(), "path escapes package root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateChecksCompatibilityRange(t *testing.T) {
	root := writeValidPackage(t)
	appendFile(t, filepath.Join(root, ManifestFile), "\ncompatibility:\n  jeju: \">=0.4.0 <0.6.0\"\n")

	if _, err := Validate(root, ValidateOptions{JejuVersion: "0.5.0"}); err != nil {
		t.Fatalf("expected compatible version: %v", err)
	}
	_, err := Validate(root, ValidateOptions{JejuVersion: "0.6.0"})
	if err == nil {
		t.Fatal("expected incompatible version error")
	}
	if !strings.Contains(err.Error(), "does not allow current version") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeValidPackage(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeValidPackageAt(t, root)
	return root
}

func writeValidPackageAt(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "workspace"), 0o755); err != nil {
		t.Fatalf("mkdir workspace failed: %v", err)
	}
	writeFile(t, filepath.Join(root, ManifestFile), `apiVersion: jeju/v1alpha1
kind: AgentPackage

metadata:
  id: test/review
  version: 0.1.0
  description: "Review a repository."

agent:
  manifest: agent.yaml
`)
	writeFile(t, filepath.Join(root, "agent.yaml"), validAgentYAML())
	writeFile(t, filepath.Join(root, "instructions.md"), "You are a concise reviewer.\n")
}

func validAgentYAML() string {
	return `apiVersion: jeju/v1alpha1
kind: Agent

metadata:
  name: review
  description: "Review agent"

models:
  providers:
    primary:
      type: mock
      model: mock-react
      contextWindow: 128000

instructions:
  system: instructions.md

runtime:
  model: primary
  loop:
    type: react
  compressionThreshold: 0.8
  recentTokenBudget: 20000
  limits:
    maxSteps: 2
    maxDurationSec: 30
    maxToolCalls: 1
    maxConsecutiveErrors: 1

workspace:
  path: workspace

permissions:
  access: readOnly
  approval: never
`
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s failed: %v", path, err)
	}
}

func appendFile(t *testing.T, path string, content string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open %s failed: %v", path, err)
	}
	defer file.Close()
	if _, err := file.WriteString(content); err != nil {
		t.Fatalf("append %s failed: %v", path, err)
	}
}

func chdirForAgentpkgTest(t *testing.T, dir string) func() {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s failed: %v", dir, err)
	}
	return func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restore cwd failed: %v", err)
		}
	}
}
