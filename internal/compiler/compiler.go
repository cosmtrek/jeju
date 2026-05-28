package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"jeju/internal/config"
	"jeju/internal/evaluate"
	"jeju/internal/memory"
	"jeju/internal/model"
	"jeju/internal/policy"
	"jeju/internal/runs"
	"jeju/internal/sandbox"
	"jeju/internal/skills"
	"jeju/internal/tools"
	"jeju/internal/tools/builtin"
	toolcli "jeju/internal/tools/cli"
	"jeju/internal/tools/command"
	toolhttp "jeju/internal/tools/http"
)

type CompiledAgent struct {
	Name           string
	Description    string
	Config         config.AgentManifest
	ConfigSnapshot []byte
	Instructions   string
	Models         *model.Registry
	Tools          *tools.Registry
	Skills         *skills.Registry
	Memory         memory.Store
	Sandbox        sandbox.Sandbox
	Policy         *policy.Gate
	Evaluators     []evaluate.Evaluator
	RunStore       *runs.Store
	systemPrompt   string
}

func Compile(manifestPath string) (*CompiledAgent, error) {
	manifest, snapshot, err := config.LoadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	if err := config.Validate(manifest); err != nil {
		return nil, err
	}

	instructions, err := os.ReadFile(manifest.Instructions.System)
	if err != nil {
		return nil, err
	}
	box, err := sandbox.NewLocal(manifest.Workspace.Path)
	if err != nil {
		return nil, err
	}
	toolRegistry, err := compileTools(manifest.Tools, box)
	if err != nil {
		return nil, err
	}
	skillRegistry, err := skills.LoadRegistry(manifest.Skills, toolRegistry.Names())
	if err != nil {
		return nil, err
	}
	modelRegistry, err := compileModels(manifest.Models)
	if err != nil {
		return nil, err
	}
	evaluators, err := compileEvaluators(manifest.Evaluate, modelRegistry, manifest.Runtime.Model)
	if err != nil {
		return nil, err
	}

	agent := &CompiledAgent{
		Name:           manifest.Metadata.Name,
		Description:    manifest.Metadata.Description,
		Config:         *manifest,
		ConfigSnapshot: snapshot,
		Instructions:   string(instructions),
		Models:         modelRegistry,
		Tools:          toolRegistry,
		Skills:         skillRegistry,
		Memory:         memory.Noop{},
		Sandbox:        box,
		Policy:         policy.NewGate(manifest.Permissions),
		Evaluators:     evaluators,
		RunStore:       runs.NewStore("./runs"),
	}
	agent.systemPrompt = agent.renderSystemPrompt()
	return agent, nil
}

func compileModels(cfg config.ModelsConfig) (*model.Registry, error) {
	registry := model.NewRegistry()
	for name, item := range cfg.Providers {
		providerCfg := model.ProviderConfig{
			Name:        name,
			Provider:    item.Type,
			Model:       item.Model,
			BaseURL:     item.BaseURL,
			EnvKey:      item.EnvKey,
			Temperature: item.Temperature,
			Thinking: model.ThinkingConfig{
				Type:   item.Thinking.Type,
				Effort: item.Thinking.Effort,
			},
			MaxOutputTokens: item.MaxOutputTokens,
			TimeoutSec:      item.TimeoutSec,
		}
		switch item.Type {
		case "mock":
			registry.Add(name, providerCfg, model.NewMockClient(providerCfg))
		case "openaiCompatible":
			providerCfg.ToolCalling = true
			providerCfg.JSONSchemaMode = true
			if item.Preset == "deepseek" || item.Preset == "mimo" {
				providerCfg.Provider = item.Preset
				providerCfg.JSONMode = true
			}
			if item.Preset == "deepseek" {
				providerCfg.JSONSchemaMode = false
			}
			registry.Add(name, providerCfg, model.NewOpenAICompatibleClient(providerCfg))
		default:
			return nil, fmt.Errorf("unsupported model provider type %q", item.Type)
		}
	}
	return registry, nil
}

func compileTools(configs []config.ToolConfig, box sandbox.Sandbox) (*tools.Registry, error) {
	registry := tools.NewRegistry()
	for _, cfg := range configs {
		spec, err := compileToolSpec(cfg)
		if err != nil {
			return nil, err
		}
		switch cfg.Uses {
		case "builtin:read":
			if err := registry.Register(builtin.NewFileRead(spec, box)); err != nil {
				return nil, err
			}
		case "builtin:write":
			if err := registry.Register(builtin.NewFileWrite(spec, box)); err != nil {
				return nil, err
			}
		case "builtin:edit":
			if err := registry.Register(builtin.NewEdit(spec, box)); err != nil {
				return nil, err
			}
		case "builtin:search":
			if err := registry.Register(builtin.NewSearch(spec, box)); err != nil {
				return nil, err
			}
		case "builtin:shell":
			if spec.TimeoutSec == 0 {
				spec.TimeoutSec = 30
			}
			if err := registry.Register(toolcli.NewShell(spec, box, cfg.Env)); err != nil {
				return nil, err
			}
		case "command":
			if spec.TimeoutSec == 0 {
				spec.TimeoutSec = 60
			}
			if err := registry.Register(command.New(cfg.Name, cfg.Command.Run, box.Workdir(), spec, cfg.Env)); err != nil {
				return nil, err
			}
		case "http":
			if spec.TimeoutSec == 0 {
				spec.TimeoutSec = 30
			}
			if err := registry.Register(toolhttp.New(toolhttp.Config{
				Name:       cfg.Name,
				Spec:       spec,
				Method:     cfg.HTTP.Method,
				URL:        cfg.HTTP.URL,
				Query:      cfg.HTTP.Query,
				Headers:    cfg.HTTP.Headers,
				Body:       cfg.HTTP.Body.JSON,
				TimeoutSec: cfg.HTTP.TimeoutSec,
			})); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported tool uses %q", cfg.Uses)
		}
	}
	return registry, nil
}

func compileToolSpec(cfg config.ToolConfig) (tools.Spec, error) {
	spec := tools.Spec{
		Name:         cfg.Name,
		Description:  cfg.Description,
		Args:         cfg.Command.Args,
		Capabilities: toolCapabilities(cfg),
		TimeoutSec:   cfg.Command.TimeoutSec,
	}
	if cfg.Uses == "http" {
		spec.TimeoutSec = cfg.HTTP.TimeoutSec
	}
	schema, err := compileInputSchema(cfg.Input.Schema)
	if err != nil {
		return tools.Spec{}, fmt.Errorf("tool %q input.schema: %w", cfg.Name, err)
	}
	if schema == nil {
		schema = defaultInputSchema(cfg.Uses)
	}
	spec.InputSchema = schema
	return spec, nil
}

func defaultInputSchema(uses string) any {
	switch uses {
	case "builtin:read":
		return objectSchema(map[string]any{
			"path": map[string]any{"type": "string", "description": "Workspace-relative file path to read."},
		}, []string{"path"})
	case "builtin:write":
		return objectSchema(map[string]any{
			"path":    map[string]any{"type": "string", "description": "Workspace-relative file path to write."},
			"content": map[string]any{"type": "string", "description": "Complete file content to write."},
		}, []string{"path", "content"})
	case "builtin:edit":
		return objectSchema(map[string]any{
			"path":    map[string]any{"type": "string", "description": "Workspace-relative file path to edit."},
			"oldText": map[string]any{"type": "string", "description": "Exact text to replace. Must match exactly once."},
			"newText": map[string]any{"type": "string", "description": "Replacement text."},
		}, []string{"path", "oldText", "newText"})
	case "builtin:search":
		return objectSchema(map[string]any{
			"query": map[string]any{"type": "string", "description": "Literal text to search for."},
			"path":  map[string]any{"type": "string", "description": "Optional workspace-relative directory to search."},
		}, []string{"query"})
	case "builtin:shell":
		return objectSchema(map[string]any{
			"command": map[string]any{"type": "string", "description": "Shell command to run in the workspace."},
		}, []string{"command"})
	default:
		return nil
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func toolCapabilities(cfg config.ToolConfig) []string {
	if len(cfg.Capabilities) > 0 {
		return cfg.Capabilities
	}
	switch cfg.Uses {
	case "builtin:read", "builtin:search":
		return []string{"workspaceRead"}
	case "builtin:write", "builtin:edit":
		return []string{"workspaceWrite"}
	case "builtin:shell", "command":
		return []string{"command"}
	case "http":
		if isWriteHTTPMethod(cfg.HTTP.Method) {
			return []string{"networkWrite"}
		}
		return []string{"networkRead"}
	default:
		return nil
	}
}

func isWriteHTTPMethod(method string) bool {
	switch strings.ToUpper(method) {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}

func compileInputSchema(schema any) (any, error) {
	if schema == nil {
		return nil, nil
	}
	if path, ok := schema.(string); ok {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var parsed any
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil, err
		}
		return parsed, nil
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func compileEvaluators(cfg config.EvaluateConfig, models *model.Registry, runtimeModel string) ([]evaluate.Evaluator, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	evaluators := make([]evaluate.Evaluator, 0, len(cfg.Evaluators))
	for _, item := range cfg.Evaluators {
		switch item.Uses {
		case "rules":
			evaluators = append(evaluators, evaluate.NewRuleEvaluator(item.Name, item.Rules))
		case "llm":
			modelName := item.Model
			if modelName == "" {
				modelName = runtimeModel
			}
			client, provider, ok := models.Get(modelName)
			if !ok {
				return nil, fmt.Errorf("evaluator %q model %q is not compiled", item.Name, modelName)
			}
			prompt, err := os.ReadFile(item.Prompt)
			if err != nil {
				return nil, err
			}
			evaluators = append(evaluators, evaluate.NewLLMEvaluator(item.Name, client, provider, string(prompt), item.Threshold))
		case "command":
			evaluators = append(evaluators, evaluate.NewCommandEvaluator(item.Name, item.Command.Run, item.Command.Args, item.Command.TimeoutSec))
		default:
			return nil, fmt.Errorf("unsupported evaluator uses %q", item.Uses)
		}
	}
	return evaluators, nil
}
