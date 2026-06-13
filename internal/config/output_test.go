package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateOutputSchema(t *testing.T) {
	dir := t.TempDir()
	prompt := filepath.Join(dir, "system.md")
	if err := os.WriteFile(prompt, []byte("Return structured output."), 0o644); err != nil {
		t.Fatalf("WriteFile prompt failed: %v", err)
	}

	manifest := minimalValidManifest(prompt, filepath.Join(dir, "workspace"))
	manifest.Output = OutputConfig{
		Name: "task_result",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"summary": map[string]any{"type": "string"},
			},
			"required": []string{"summary"},
		},
	}
	if err := Validate(&manifest); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
}

func TestValidateOutputRequiresNameAndSchema(t *testing.T) {
	dir := t.TempDir()
	prompt := filepath.Join(dir, "system.md")
	if err := os.WriteFile(prompt, []byte("Return structured output."), 0o644); err != nil {
		t.Fatalf("WriteFile prompt failed: %v", err)
	}

	for _, tc := range []struct {
		name       string
		output     OutputConfig
		wantErrSub string
	}{
		{
			name:       "missing name",
			output:     OutputConfig{Schema: map[string]any{"type": "object"}},
			wantErrSub: "output.name is required",
		},
		{
			name:       "missing schema",
			output:     OutputConfig{Name: "task_result"},
			wantErrSub: "output.schema is required",
		},
		{
			name:       "invalid schema",
			output:     OutputConfig{Name: "task_result", Schema: map[string]any{"type": "not-a-json-schema-type"}},
			wantErrSub: "output.schema is invalid",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := minimalValidManifest(prompt, filepath.Join(dir, "workspace-"+tc.name))
			manifest.Output = tc.output
			err := Validate(&manifest)
			if err == nil || !strings.Contains(err.Error(), tc.wantErrSub) {
				t.Fatalf("Validate error got %v, want containing %q", err, tc.wantErrSub)
			}
		})
	}
}

func minimalValidManifest(prompt string, workspace string) AgentManifest {
	return AgentManifest{
		APIVersion: "jeju/v1alpha1",
		Kind:       "Agent",
		Metadata:   Metadata{Name: "test"},
		Models: ModelsConfig{Providers: map[string]ModelConfig{
			"primary": {Type: "mock", Model: "mock"},
		}},
		Instructions: InstructionsConfig{System: prompt},
		Runtime: RuntimeConfig{
			Model:                "primary",
			Loop:                 LoopConfig{Type: "react"},
			CompressionThreshold: 0.8,
			RecentTokenBudget:    20000,
			Limits: RuntimeLimits{
				MaxSteps:             20,
				MaxDurationSec:       900,
				MaxToolCalls:         50,
				MaxConsecutiveErrors: 3,
			},
		},
		Workspace:   WorkspaceConfig{Path: workspace},
		Permissions: PermissionsConfig{Access: "workspace", Approval: "never"},
	}
}
