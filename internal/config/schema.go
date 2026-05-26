package config

type AgentManifest struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`

	Models     ModelsConfig      `yaml:"models"`
	ModelRoles map[string]string `yaml:"model_roles,omitempty"`

	Instructions InstructionsConfig `yaml:"instructions"`
	Runtime      RuntimeConfig      `yaml:"runtime"`
	Workspace    WorkspaceConfig    `yaml:"workspace"`

	Tools      []ToolConfig     `yaml:"tools,omitempty"`
	Skills     SkillsConfig     `yaml:"skills,omitempty"`
	Memory     MemoryConfig     `yaml:"memory,omitempty"`
	Sandbox    SandboxConfig    `yaml:"sandbox,omitempty"`
	Policy     PolicyConfig     `yaml:"policy,omitempty"`
	Trajectory TrajectoryConfig `yaml:"trajectory,omitempty"`
	Evaluate   EvaluateConfig   `yaml:"evaluate,omitempty"`
}

type Metadata struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
}

type ModelsConfig struct {
	Default   string                 `yaml:"default"`
	Providers map[string]ModelConfig `yaml:"providers"`
	Fallback  []string               `yaml:"fallback,omitempty"`
}

type ModelConfig struct {
	Provider        string   `yaml:"provider"`
	Model           string   `yaml:"model"`
	BaseURL         string   `yaml:"base_url,omitempty"`
	EnvKey          string   `yaml:"env_key,omitempty"`
	APIKeyEnv       string   `yaml:"api_key_env,omitempty"`
	Temperature     *float64 `yaml:"temperature,omitempty"`
	MaxOutputTokens int      `yaml:"max_output_tokens,omitempty"`
	TimeoutSec      int      `yaml:"timeout_sec,omitempty"`
}

type InstructionsConfig struct {
	System string `yaml:"system"`
}

type RuntimeConfig struct {
	Mode        string            `yaml:"mode"`
	MaxSteps    int               `yaml:"max_steps,omitempty"`
	Limits      RuntimeLimits     `yaml:"limits,omitempty"`
	Models      RuntimeModels     `yaml:"models,omitempty"`
	React       ReactConfig       `yaml:"react,omitempty"`
	Interactive InteractiveConfig `yaml:"interactive,omitempty"`
}

type RuntimeLimits struct {
	MaxSteps             int `yaml:"max_steps,omitempty"`
	MaxDurationSec       int `yaml:"max_duration_sec,omitempty"`
	MaxToolCalls         int `yaml:"max_tool_calls,omitempty"`
	MaxConsecutiveErrors int `yaml:"max_consecutive_errors,omitempty"`
}

type RuntimeModels struct {
	Reasoning  string `yaml:"reasoning,omitempty"`
	Utility    string `yaml:"utility,omitempty"`
	Evaluation string `yaml:"evaluation,omitempty"`
}

type ReactConfig struct {
	ActionMode string `yaml:"action_mode,omitempty"`
	Reflection string `yaml:"reflection,omitempty"`
	Compaction string `yaml:"compaction,omitempty"`
}

type InteractiveConfig struct {
	Enabled bool     `yaml:"enabled,omitempty"`
	PauseOn []string `yaml:"pause_on,omitempty"`
}

type WorkspaceConfig struct {
	Path string `yaml:"path"`
}

type ToolConfig struct {
	Name            string            `yaml:"name"`
	Type            string            `yaml:"type"`
	Description     string            `yaml:"description,omitempty"`
	Command         string            `yaml:"command,omitempty"`
	Args            []string          `yaml:"args,omitempty"`
	Schema          string            `yaml:"schema,omitempty"`
	Permission      string            `yaml:"permission,omitempty"`
	Risk            []string          `yaml:"risk,omitempty"`
	TimeoutSec      int               `yaml:"timeout_sec,omitempty"`
	SandboxRequired bool              `yaml:"sandbox_required,omitempty"`
	SideEffect      bool              `yaml:"side_effect,omitempty"`
	Env             map[string]string `yaml:"env,omitempty"`
}

type SkillsConfig struct {
	Mode       string                `yaml:"mode,omitempty"`
	Paths      []string              `yaml:"paths,omitempty"`
	Disclosure SkillDisclosureConfig `yaml:"disclosure,omitempty"`
	Activation SkillActivationConfig `yaml:"activation,omitempty"`
	Loading    SkillLoadingConfig    `yaml:"loading,omitempty"`
}

type SkillDisclosureConfig struct {
	Include []string `yaml:"include,omitempty"`
}

type SkillActivationConfig struct {
	Policy    string   `yaml:"policy,omitempty"`
	Active    []string `yaml:"active,omitempty"`
	MaxActive int      `yaml:"max_active,omitempty"`
}

type SkillLoadingConfig struct {
	Strategy string `yaml:"strategy,omitempty"`
}

type MemoryConfig struct {
	Enabled bool `yaml:"enabled,omitempty"`
}

type SandboxConfig struct {
	Type       string         `yaml:"type,omitempty"`
	Workdir    string         `yaml:"workdir,omitempty"`
	Network    string         `yaml:"network,omitempty"`
	Image      string         `yaml:"image,omitempty"`
	Endpoint   string         `yaml:"endpoint,omitempty"`
	APIKeyEnv  string         `yaml:"api_key_env,omitempty"`
	TimeoutSec int            `yaml:"timeout_sec,omitempty"`
	Mounts     []SandboxMount `yaml:"mounts,omitempty"`
}

type SandboxMount struct {
	Source string `yaml:"source"`
	Target string `yaml:"target"`
}

type PolicyConfig struct {
	DefaultPermission  string       `yaml:"default_permission,omitempty"`
	SandboxRequiredFor []string     `yaml:"sandbox_required_for,omitempty"`
	Rules              []PolicyRule `yaml:"rules,omitempty"`
}

type PolicyRule struct {
	Match      PolicyMatch `yaml:"match"`
	Permission string      `yaml:"permission"`
}

type PolicyMatch struct {
	Risk string `yaml:"risk,omitempty"`
	Tool string `yaml:"tool,omitempty"`
}

type TrajectoryConfig struct {
	Enabled         bool         `yaml:"enabled,omitempty"`
	Format          string       `yaml:"format,omitempty"`
	Store           StoreConfig  `yaml:"store,omitempty"`
	Sinks           []SinkConfig `yaml:"sinks,omitempty"`
	FailOnSinkError bool         `yaml:"fail_on_sink_error,omitempty"`
}

type StoreConfig struct {
	Type string `yaml:"type,omitempty"`
	Path string `yaml:"path,omitempty"`
}

type SinkConfig struct {
	Type      string `yaml:"type"`
	Level     string `yaml:"level,omitempty"`
	Path      string `yaml:"path,omitempty"`
	Endpoint  string `yaml:"endpoint,omitempty"`
	APIKeyEnv string `yaml:"api_key_env,omitempty"`
}

type EvaluateConfig struct {
	Enabled       bool              `yaml:"enabled,omitempty"`
	OnRunComplete bool              `yaml:"on_run_complete,omitempty"`
	Evaluators    []EvaluatorConfig `yaml:"evaluators,omitempty"`
	Outputs       EvalOutputConfig  `yaml:"outputs,omitempty"`
}

type EvaluatorConfig struct {
	Name   string   `yaml:"name"`
	Type   string   `yaml:"type"`
	Rules  []string `yaml:"rules,omitempty"`
	Model  string   `yaml:"model,omitempty"`
	Rubric string   `yaml:"rubric,omitempty"`
}

type EvalOutputConfig struct {
	Path string `yaml:"path,omitempty"`
	File string `yaml:"file,omitempty"`
}
