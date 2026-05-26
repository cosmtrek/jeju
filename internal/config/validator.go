package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var validNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)

var validPermissions = map[string]bool{
	"allow":   true,
	"ask":     true,
	"deny":    true,
	"dry_run": true,
}

var validRisks = map[string]bool{
	"read":        true,
	"write":       true,
	"execute":     true,
	"network":     true,
	"credential":  true,
	"external":    true,
	"destructive": true,
	"production":  true,
	"payment":     true,
	"message":     true,
}

var validEvalRules = map[string]bool{
	"final_answer_exists":         true,
	"no_model_error":              true,
	"no_tool_error":               true,
	"max_steps_not_exceeded":      true,
	"max_tool_calls_not_exceeded": true,
	"no_permission_denied":        true,
	"run_completed":               true,
}

func Validate(m *AgentManifest) error {
	if m.APIVersion != "jeju/v1alpha1" {
		return fmt.Errorf("unsupported apiVersion %q", m.APIVersion)
	}
	if m.Kind != "Agent" {
		return fmt.Errorf("unsupported kind %q", m.Kind)
	}
	if !validNameRe.MatchString(m.Metadata.Name) {
		return fmt.Errorf("metadata.name must match %s", validNameRe.String())
	}
	if m.Models.Default == "" {
		return fmt.Errorf("models.default is required")
	}
	if len(m.Models.Providers) == 0 {
		return fmt.Errorf("models.providers is required")
	}
	if _, ok := m.Models.Providers[m.Models.Default]; !ok {
		return fmt.Errorf("models.default %q is not defined in providers", m.Models.Default)
	}
	for name, provider := range m.Models.Providers {
		if provider.Provider == "" {
			return fmt.Errorf("models.providers.%s.provider is required", name)
		}
		if provider.Model == "" {
			return fmt.Errorf("models.providers.%s.model is required", name)
		}
		if provider.Provider != "mock" && provider.Provider != "openai_compatible" && provider.Provider != "deepseek" {
			return fmt.Errorf("models.providers.%s.provider %q is not supported in V0", name, provider.Provider)
		}
	}
	for role, modelName := range m.ModelRoles {
		if _, ok := m.Models.Providers[modelName]; !ok {
			return fmt.Errorf("model_roles.%s references unknown provider %q", role, modelName)
		}
	}
	for role, modelName := range map[string]string{
		"runtime.models.reasoning":  m.Runtime.Models.Reasoning,
		"runtime.models.utility":    m.Runtime.Models.Utility,
		"runtime.models.evaluation": m.Runtime.Models.Evaluation,
	} {
		if modelName != "" {
			if _, ok := m.Models.Providers[modelName]; !ok {
				return fmt.Errorf("%s references unknown provider %q", role, modelName)
			}
		}
	}
	if m.Instructions.System == "" {
		return fmt.Errorf("instructions.system is required")
	}
	if _, err := os.Stat(m.Instructions.System); err != nil {
		return fmt.Errorf("instructions.system %q: %w", m.Instructions.System, err)
	}
	if m.Runtime.Mode != "react" {
		return fmt.Errorf("runtime.mode %q is not supported in V0", m.Runtime.Mode)
	}
	if m.Runtime.React.ActionMode != "combined" {
		return fmt.Errorf("runtime.react.action_mode %q is not supported in V0", m.Runtime.React.ActionMode)
	}
	if m.Workspace.Path == "" {
		return fmt.Errorf("workspace.path is required")
	}
	if err := ensureCreatableDir(m.Workspace.Path); err != nil {
		return fmt.Errorf("workspace.path %q is not creatable: %w", m.Workspace.Path, err)
	}
	if m.Sandbox.Type != "local" {
		return fmt.Errorf("sandbox.type %q is not supported in V0", m.Sandbox.Type)
	}
	if m.Trajectory.Format != "jsonl" {
		return fmt.Errorf("trajectory.format %q is not supported in V0", m.Trajectory.Format)
	}
	if m.Trajectory.Store.Type != "file" {
		return fmt.Errorf("trajectory.store.type %q is not supported in V0", m.Trajectory.Store.Type)
	}
	if err := ensureCreatableDir(m.Trajectory.Store.Path); err != nil {
		return fmt.Errorf("trajectory.store.path %q is not creatable: %w", m.Trajectory.Store.Path, err)
	}
	if err := validateTools(m.Tools); err != nil {
		return err
	}
	for _, path := range m.Skills.Paths {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("skills path %q: %w", path, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("skills path %q is not a directory", path)
		}
		if _, err := os.Stat(filepath.Join(path, "skill.yaml")); err != nil {
			return fmt.Errorf("skill %q missing skill.yaml: %w", path, err)
		}
	}
	for _, evaluator := range m.Evaluate.Evaluators {
		if evaluator.Type != "rule" {
			return fmt.Errorf("evaluate evaluator %q type %q is not supported in V0", evaluator.Name, evaluator.Type)
		}
		for _, rule := range evaluator.Rules {
			if !validEvalRules[rule] {
				return fmt.Errorf("evaluate evaluator %q has unknown rule %q", evaluator.Name, rule)
			}
		}
	}
	return nil
}

func validateTools(tools []ToolConfig) error {
	seen := map[string]bool{}
	for _, tool := range tools {
		if tool.Name == "" {
			return fmt.Errorf("tools.name is required")
		}
		if seen[tool.Name] {
			return fmt.Errorf("duplicate tool name %q", tool.Name)
		}
		seen[tool.Name] = true
		switch tool.Type {
		case "builtin", "cli", "command":
		default:
			return fmt.Errorf("tool %q type %q is not supported in V0", tool.Name, tool.Type)
		}
		if tool.Type == "cli" || tool.Type == "command" {
			if tool.Command == "" {
				return fmt.Errorf("tool %q command is required", tool.Name)
			}
		}
		if tool.Permission != "" && !validPermissions[tool.Permission] {
			return fmt.Errorf("tool %q permission %q is invalid", tool.Name, tool.Permission)
		}
		for _, risk := range tool.Risk {
			if !validRisks[risk] {
				return fmt.Errorf("tool %q risk %q is invalid", tool.Name, risk)
			}
		}
	}
	return nil
}

func ensureCreatableDir(path string) error {
	if info, err := os.Stat(path); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("not a directory")
		}
		return nil
	}
	ancestor := filepath.Dir(path)
	for ancestor != "" && ancestor != "." && ancestor != string(filepath.Separator) {
		info, err := os.Stat(ancestor)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("ancestor %q is not a directory", ancestor)
			}
			return nil
		}
		ancestor = filepath.Dir(ancestor)
	}
	info, err := os.Stat(".")
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("current path is not a directory")
	}
	return nil
}
