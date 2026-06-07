package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var validNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)

var supportedModelTypes = []string{"mock", "openaiCompatible"}

var validModelTypes = boolSet(supportedModelTypes...)

var supportedModelPresets = []string{"deepseek", "mimo"}

var validPresets = boolSet(append([]string{""}, supportedModelPresets...)...)

var validThinkingTypes = map[string]bool{
	"":         true,
	"auto":     true,
	"disabled": true,
	"enabled":  true,
}

var validThinkingEfforts = map[string]bool{
	"":        true,
	"none":    true,
	"minimal": true,
	"low":     true,
	"medium":  true,
	"high":    true,
	"xhigh":   true,
	"max":     true,
}

var validAccess = map[string]bool{
	"readOnly":  true,
	"workspace": true,
	"full":      true,
}

var validApproval = map[string]bool{
	"never":     true,
	"onRequest": true,
	"always":    true,
}

var validCapabilities = map[string]bool{
	"workspaceRead":  true,
	"workspaceWrite": true,
	"command":        true,
	"networkRead":    true,
	"networkWrite":   true,
}

var supportedToolUses = []string{
	"builtin:read",
	"builtin:write",
	"builtin:edit",
	"builtin:search",
	"builtin:shell",
	"command",
	"http",
}

var validToolUses = boolSet(supportedToolUses...)

var supportedEvaluatorUses = []string{"rules", "llm", "command"}

var validEvaluatorUses = boolSet(supportedEvaluatorUses...)

var supportedTrajectoryFormats = []string{"jeju-jsonl"}

var validEvalRules = map[string]bool{
	"finalAnswerExists":       true,
	"noModelError":            true,
	"maxStepsNotExceeded":     true,
	"maxToolCallsNotExceeded": true,
	"noPermissionDenied":      true,
	"runCompleted":            true,
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
	if len(m.Models.Providers) == 0 {
		return fmt.Errorf("models.providers is required")
	}
	for name, provider := range m.Models.Providers {
		if provider.Type == "" {
			return fmt.Errorf("models.providers.%s.type is required", name)
		}
		if !validModelTypes[provider.Type] {
			return fmt.Errorf("models.providers.%s.type %q is not supported", name, provider.Type)
		}
		if !validPresets[provider.Preset] {
			return fmt.Errorf("models.providers.%s.preset %q is not supported", name, provider.Preset)
		}
		if provider.Model == "" {
			return fmt.Errorf("models.providers.%s.model is required", name)
		}
		if !validThinkingTypes[provider.Thinking.Type] {
			return fmt.Errorf("models.providers.%s.thinking.type %q is invalid", name, provider.Thinking.Type)
		}
		if !validThinkingEfforts[provider.Thinking.Effort] {
			return fmt.Errorf("models.providers.%s.thinking.effort %q is invalid", name, provider.Thinking.Effort)
		}
		if provider.ContextWindow < 0 {
			return fmt.Errorf("models.providers.%s.contextWindow must be non-negative", name)
		}
		if provider.Type == "openaiCompatible" && provider.ContextWindow == 0 {
			return fmt.Errorf("models.providers.%s.contextWindow is required for openaiCompatible providers", name)
		}
	}
	if m.Runtime.Model == "" {
		return fmt.Errorf("runtime.model is required when more than one provider is configured")
	}
	if _, ok := m.Models.Providers[m.Runtime.Model]; !ok {
		return fmt.Errorf("runtime.model %q is not defined in models.providers", m.Runtime.Model)
	}
	if m.Runtime.Loop.Type != "react" {
		return fmt.Errorf("runtime.loop.type %q is not supported", m.Runtime.Loop.Type)
	}
	if m.Runtime.CompressionThreshold <= 0 || m.Runtime.CompressionThreshold > 1 {
		return fmt.Errorf("runtime.compressionThreshold must be greater than 0 and at most 1")
	}
	if m.Runtime.RecentTokenBudget <= 0 {
		return fmt.Errorf("runtime.recentTokenBudget must be greater than 0")
	}
	if m.Instructions.System == "" {
		return fmt.Errorf("instructions.system is required")
	}
	if _, err := os.Stat(m.Instructions.System); err != nil {
		return fmt.Errorf("instructions.system %q: %w", m.Instructions.System, err)
	}
	if m.Workspace.Path == "" {
		return fmt.Errorf("workspace.path is required")
	}
	if err := ensureCreatableDir(m.Workspace.Path); err != nil {
		return fmt.Errorf("workspace.path %q is not creatable: %w", m.Workspace.Path, err)
	}
	if !validAccess[m.Permissions.Access] {
		return fmt.Errorf("permissions.access %q is invalid", m.Permissions.Access)
	}
	if !validApproval[m.Permissions.Approval] {
		return fmt.Errorf("permissions.approval %q is invalid", m.Permissions.Approval)
	}
	if err := validateTools(m.Tools); err != nil {
		return err
	}
	if err := validateSkills(m.Skills); err != nil {
		return err
	}
	if err := validateEvaluators(m.Evaluate, m.Models.Providers, m.Runtime.Model); err != nil {
		return err
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
		if tool.Uses == "" {
			return fmt.Errorf("tool %q uses is required", tool.Name)
		}
		if !validToolUses[tool.Uses] {
			return fmt.Errorf("tool %q uses %q is not supported", tool.Name, tool.Uses)
		}
		if tool.Uses == "command" && tool.Command.Run == "" {
			return fmt.Errorf("tool %q command.run is required", tool.Name)
		}
		if tool.Uses == "http" {
			if err := validateHTTPTool(tool); err != nil {
				return err
			}
		}
		for _, capability := range tool.Capabilities {
			if !validCapabilities[capability] {
				return fmt.Errorf("tool %q capability %q is invalid", tool.Name, capability)
			}
		}
		if err := validateSchema(tool.Name, tool.Input.Schema); err != nil {
			return err
		}
	}
	return nil
}

func validateHTTPTool(tool ToolConfig) error {
	if tool.HTTP.Method == "" {
		return fmt.Errorf("tool %q http.method is required", tool.Name)
	}
	if tool.HTTP.URL == "" {
		return fmt.Errorf("tool %q http.url is required", tool.Name)
	}
	if strings.Contains(tool.HTTP.URL, "{{") {
		return fmt.Errorf("tool %q http.url must keep scheme and host static; template query values separately", tool.Name)
	}
	parsed, err := url.Parse(tool.HTTP.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("tool %q http.url must be an absolute URL", tool.Name)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("tool %q http.url scheme %q is not supported", tool.Name, parsed.Scheme)
	}
	return nil
}

func validateSchema(toolName string, schema any) error {
	if schema == nil {
		return nil
	}
	switch raw := schema.(type) {
	case string:
		data, err := os.ReadFile(raw)
		if err != nil {
			return fmt.Errorf("tool %q input.schema %q: %w", toolName, raw, err)
		}
		var parsed any
		if err := json.Unmarshal(data, &parsed); err != nil {
			return fmt.Errorf("tool %q input.schema %q is invalid JSON: %w", toolName, raw, err)
		}
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return fmt.Errorf("tool %q input.schema is invalid: %w", toolName, err)
		}
		var parsed any
		if err := json.Unmarshal(data, &parsed); err != nil {
			return fmt.Errorf("tool %q input.schema is invalid JSON: %w", toolName, err)
		}
	}
	return nil
}

func validateSkills(cfg SkillsConfig) error {
	for _, dir := range cfg.Dirs {
		info, err := os.Stat(dir)
		if err != nil {
			return fmt.Errorf("skills dir %q: %w", dir, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("skills dir %q is not a directory", dir)
		}
	}
	return nil
}

func validateEvaluators(cfg EvaluateConfig, providers map[string]ModelConfig, runtimeModel string) error {
	if !cfg.Enabled {
		return nil
	}
	for _, evaluator := range cfg.Evaluators {
		if evaluator.Name == "" {
			return fmt.Errorf("evaluate evaluator name is required")
		}
		if !validEvaluatorUses[evaluator.Uses] {
			return fmt.Errorf("evaluate evaluator %q uses %q is not supported", evaluator.Name, evaluator.Uses)
		}
		switch evaluator.Uses {
		case "rules":
			for _, rule := range evaluator.Rules {
				if !validEvalRules[rule] {
					return fmt.Errorf("evaluate evaluator %q has unknown rule %q", evaluator.Name, rule)
				}
			}
		case "llm":
			modelName := evaluator.Model
			if modelName == "" {
				modelName = runtimeModel
			}
			if _, ok := providers[modelName]; !ok {
				return fmt.Errorf("evaluate evaluator %q model %q is not defined in models.providers", evaluator.Name, modelName)
			}
			if evaluator.Prompt == "" {
				return fmt.Errorf("evaluate evaluator %q prompt is required for llm", evaluator.Name)
			}
			if _, err := os.Stat(evaluator.Prompt); err != nil {
				return fmt.Errorf("evaluate evaluator %q prompt %q: %w", evaluator.Name, evaluator.Prompt, err)
			}
		case "command":
			if evaluator.Command.Run == "" {
				return fmt.Errorf("evaluate evaluator %q command.run is required", evaluator.Name)
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
