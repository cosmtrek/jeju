package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

type AgentManifest struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`

	Models       ModelsConfig       `yaml:"models"`
	Instructions InstructionsConfig `yaml:"instructions"`
	Runtime      RuntimeConfig      `yaml:"runtime"`
	Workspace    WorkspaceConfig    `yaml:"workspace"`
	Tools        []ToolConfig       `yaml:"tools,omitempty"`
	Skills       SkillsConfig       `yaml:"skills,omitempty"`
	Permissions  PermissionsConfig  `yaml:"permissions,omitempty"`
	Evaluate     EvaluateConfig     `yaml:"evaluate,omitempty"`
}

type Metadata struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
}

type ModelsConfig struct {
	Providers map[string]ModelConfig `yaml:"providers"`
}

type ModelConfig struct {
	Type            string         `yaml:"type"`
	Preset          string         `yaml:"preset,omitempty"`
	Model           string         `yaml:"model"`
	BaseURL         string         `yaml:"baseUrl,omitempty"`
	EnvKey          string         `yaml:"envKey,omitempty"`
	Temperature     *float64       `yaml:"temperature,omitempty"`
	Thinking        ThinkingConfig `yaml:"thinking,omitempty"`
	MaxOutputTokens int            `yaml:"maxOutputTokens,omitempty"`
	TimeoutSec      int            `yaml:"timeoutSec,omitempty"`
}

type ThinkingConfig struct {
	Type   string `yaml:"type,omitempty"`
	Effort string `yaml:"effort,omitempty"`
}

type InstructionsConfig struct {
	System string `yaml:"system"`
}

type RuntimeConfig struct {
	Model  string        `yaml:"model,omitempty"`
	Loop   LoopConfig    `yaml:"loop,omitempty"`
	Limits RuntimeLimits `yaml:"limits,omitempty"`
}

type LoopConfig struct {
	Type string `yaml:"type,omitempty"`
}

type RuntimeLimits struct {
	MaxSteps             int `yaml:"maxSteps,omitempty"`
	MaxDurationSec       int `yaml:"maxDurationSec,omitempty"`
	MaxToolCalls         int `yaml:"maxToolCalls,omitempty"`
	MaxConsecutiveErrors int `yaml:"maxConsecutiveErrors,omitempty"`
}

type WorkspaceConfig struct {
	Path string `yaml:"path"`
}

type ToolConfig struct {
	Name         string            `yaml:"name,omitempty"`
	Uses         string            `yaml:"uses,omitempty"`
	Description  string            `yaml:"description,omitempty"`
	Capabilities []string          `yaml:"capabilities,omitempty"`
	Command      CommandConfig     `yaml:"command,omitempty"`
	HTTP         HTTPConfig        `yaml:"http,omitempty"`
	Input        ToolInputConfig   `yaml:"input,omitempty"`
	Env          map[string]string `yaml:"env,omitempty"`
}

func (t *ToolConfig) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var name string
		if err := value.Decode(&name); err != nil {
			return err
		}
		t.Name = name
		t.Uses = "builtin:" + name
		return nil
	case yaml.MappingNode:
		type rawToolConfig ToolConfig
		var raw rawToolConfig
		if err := value.Decode(&raw); err != nil {
			return err
		}
		*t = ToolConfig(raw)
		return nil
	default:
		return fmt.Errorf("tool must be a string or object")
	}
}

type CommandConfig struct {
	Run        string   `yaml:"run,omitempty"`
	Args       []string `yaml:"args,omitempty"`
	TimeoutSec int      `yaml:"timeoutSec,omitempty"`
}

type HTTPConfig struct {
	Method     string            `yaml:"method,omitempty"`
	URL        string            `yaml:"url,omitempty"`
	Query      map[string]string `yaml:"query,omitempty"`
	Headers    map[string]string `yaml:"headers,omitempty"`
	Body       HTTPBodyConfig    `yaml:"body,omitempty"`
	TimeoutSec int               `yaml:"timeoutSec,omitempty"`
}

type HTTPBodyConfig struct {
	JSON any    `yaml:"json,omitempty"`
	Text string `yaml:"text,omitempty"`
}

type ToolInputConfig struct {
	Schema any `yaml:"schema,omitempty"`
}

type SkillsConfig struct {
	Dirs   []string `yaml:"dirs,omitempty"`
	Active []string `yaml:"active,omitempty"`
}

type PermissionsConfig struct {
	Access   string `yaml:"access,omitempty"`
	Approval string `yaml:"approval,omitempty"`
}

type EvaluateConfig struct {
	Enabled    bool              `yaml:"enabled,omitempty"`
	Evaluators []EvaluatorConfig `yaml:"evaluators,omitempty"`
}

type EvaluatorConfig struct {
	Name      string        `yaml:"name"`
	Uses      string        `yaml:"uses"`
	Rules     []string      `yaml:"rules,omitempty"`
	Model     string        `yaml:"model,omitempty"`
	Prompt    string        `yaml:"prompt,omitempty"`
	Threshold *float64      `yaml:"threshold,omitempty"`
	Command   CommandConfig `yaml:"command,omitempty"`
}
