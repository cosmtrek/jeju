package compiler

import (
	"fmt"
	"os"

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
	box, err := sandbox.NewLocal(manifest.Sandbox.Workdir)
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
	evaluators, err := compileEvaluators(manifest.Evaluate)
	if err != nil {
		return nil, err
	}

	return &CompiledAgent{
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
		Policy:         policy.NewGate(manifest.Policy),
		Evaluators:     evaluators,
		RunStore:       runs.NewStore(manifest.Trajectory.Store.Path),
	}, nil
}

func compileModels(cfg config.ModelsConfig) (*model.Registry, error) {
	registry := model.NewRegistry()
	for name, item := range cfg.Providers {
		providerCfg := model.ProviderConfig{
			Name:            name,
			Provider:        item.Provider,
			Model:           item.Model,
			BaseURL:         item.BaseURL,
			EnvKey:          item.EnvKey,
			APIKeyEnv:       item.APIKeyEnv,
			Temperature:     item.Temperature,
			MaxOutputTokens: item.MaxOutputTokens,
			TimeoutSec:      item.TimeoutSec,
		}
		switch item.Provider {
		case "mock":
			registry.Add(name, providerCfg, model.NewMockClient(providerCfg))
		case "openai_compatible":
			registry.Add(name, providerCfg, model.NewOpenAICompatibleClient(providerCfg))
		case "deepseek":
			providerCfg.Provider = "deepseek"
			providerCfg.JSONMode = true
			registry.Add(name, providerCfg, model.NewOpenAICompatibleClient(providerCfg))
		default:
			return nil, fmt.Errorf("unsupported model provider %q", item.Provider)
		}
	}
	return registry, nil
}

func compileTools(configs []config.ToolConfig, box sandbox.Sandbox) (*tools.Registry, error) {
	registry := tools.NewRegistry()
	for _, cfg := range configs {
		spec := tools.Spec{
			Name:            cfg.Name,
			Description:     cfg.Description,
			Permission:      cfg.Permission,
			Risks:           cfg.Risk,
			TimeoutSec:      cfg.TimeoutSec,
			SideEffect:      cfg.SideEffect,
			SandboxRequired: cfg.SandboxRequired,
		}
		switch cfg.Type {
		case "builtin":
			switch cfg.Name {
			case "file_read":
				if err := registry.Register(builtin.NewFileRead(spec, box)); err != nil {
					return nil, err
				}
			case "file_write":
				if err := registry.Register(builtin.NewFileWrite(spec, box)); err != nil {
					return nil, err
				}
			default:
				return nil, fmt.Errorf("unknown builtin tool %q", cfg.Name)
			}
		case "cli":
			if cfg.Name != "shell" {
				return nil, fmt.Errorf("unknown cli tool %q", cfg.Name)
			}
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
			if err := registry.Register(command.New(cfg.Name, cfg.Command, box.Workdir(), spec, cfg.Env)); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported tool type %q", cfg.Type)
		}
	}
	return registry, nil
}

func compileEvaluators(cfg config.EvaluateConfig) ([]evaluate.Evaluator, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	evaluators := make([]evaluate.Evaluator, 0, len(cfg.Evaluators))
	for _, item := range cfg.Evaluators {
		if item.Type != "rule" {
			return nil, fmt.Errorf("unsupported evaluator type %q", item.Type)
		}
		evaluators = append(evaluators, evaluate.NewRuleEvaluator(item.Name, item.Rules))
	}
	return evaluators, nil
}
