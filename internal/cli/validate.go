package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/cosmtrek/jeju/internal/config"
	teamrunner "github.com/cosmtrek/jeju/internal/team"

	"gopkg.in/yaml.v3"
)

type manifestHeader struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

func runValidate(manifestPath string, explain bool) error {
	header, err := readManifestHeader(manifestPath)
	if err != nil {
		return err
	}
	switch header.Kind {
	case "Agent":
		return runValidateAgent(manifestPath, explain)
	case teamrunner.KindAgentTeam:
		return runValidateAgentTeam(manifestPath, explain)
	default:
		return fmt.Errorf("unsupported kind %q", header.Kind)
	}
}

func readManifestHeader(manifestPath string) (manifestHeader, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return manifestHeader{}, err
	}
	var header manifestHeader
	if err := yaml.Unmarshal(data, &header); err != nil {
		return manifestHeader{}, err
	}
	return header, nil
}

func runValidateAgent(manifestPath string, explain bool) error {
	manifest, _, err := config.LoadFile(manifestPath)
	if err != nil {
		return err
	}
	if err := config.Validate(manifest); err != nil {
		return err
	}
	fmt.Printf("valid: %s\n", manifestPath)
	if explain {
		printManifestExplanation(manifest)
	}
	return nil
}

func runValidateAgentTeam(manifestPath string, explain bool) error {
	manifest, _, err := teamrunner.LoadFile(manifestPath)
	if err != nil {
		return err
	}
	if err := validateTeamAgentRefs(manifest); err != nil {
		return err
	}
	fmt.Printf("valid: %s\n", manifestPath)
	if explain {
		printTeamManifestExplanation(manifest)
	}
	return nil
}

func validateTeamAgentRefs(manifest *teamrunner.AgentTeamManifest) error {
	if err := validateAgentRef("lead.agent", manifest.Lead.Agent); err != nil {
		return err
	}
	for _, name := range sortedWorkerNames(manifest.Workers) {
		if err := validateAgentRef("workers."+name+".agent", manifest.Workers[name].Agent); err != nil {
			return err
		}
	}
	return nil
}

func validateAgentRef(label, path string) error {
	manifest, _, err := config.LoadFile(path)
	if err != nil {
		return fmt.Errorf("%s %q: %w", label, path, err)
	}
	if err := config.Validate(manifest); err != nil {
		return fmt.Errorf("%s %q: %w", label, path, err)
	}
	return nil
}

func printManifestExplanation(manifest *config.AgentManifest) {
	fmt.Println()
	fmt.Printf("Manifest: %s (%s %s)\n", manifest.Metadata.Name, manifest.Kind, manifest.APIVersion)
	if manifest.Metadata.Description != "" {
		fmt.Printf("Description: %s\n", manifest.Metadata.Description)
	}

	providerNames := sortedProviderNames(manifest.Models.Providers)
	fmt.Println("Models:")
	for _, name := range providerNames {
		provider := manifest.Models.Providers[name]
		markers := []string{
			fmt.Sprintf("type=%s", provider.Type),
			fmt.Sprintf("model=%s", provider.Model),
		}
		if provider.Preset != "" {
			markers = append(markers, "preset="+provider.Preset)
		}
		if provider.ContextWindow > 0 {
			markers = append(markers, fmt.Sprintf("contextWindow=%d", provider.ContextWindow))
		}
		prefix := "  "
		if name == manifest.Runtime.Model {
			prefix = "  runtime.model -> "
		}
		fmt.Printf("%smodels.providers.%s (%s)\n", prefix, name, strings.Join(markers, ", "))
	}

	fmt.Println("Runtime:")
	fmt.Printf("  runtime.loop.type -> %s\n", manifest.Runtime.Loop.Type)
	fmt.Printf("  runtime.compressionThreshold -> %.2f\n", manifest.Runtime.CompressionThreshold)
	fmt.Printf("  runtime.recentTokenBudget -> %d\n", manifest.Runtime.RecentTokenBudget)
	fmt.Printf("  runtime.limits -> maxSteps=%d, maxDurationSec=%d, maxToolCalls=%d, maxConsecutiveErrors=%d\n",
		manifest.Runtime.Limits.MaxSteps,
		manifest.Runtime.Limits.MaxDurationSec,
		manifest.Runtime.Limits.MaxToolCalls,
		manifest.Runtime.Limits.MaxConsecutiveErrors,
	)

	fmt.Println("Instructions:")
	fmt.Printf("  instructions.system -> %s\n", manifest.Instructions.System)

	fmt.Println("Workspace:")
	fmt.Printf("  workspace.path -> %s\n", manifest.Workspace.Path)

	fmt.Println("Policy:")
	fmt.Printf("  permissions.access -> %s\n", manifest.Permissions.Access)
	fmt.Printf("  permissions.approval -> %s\n", manifest.Permissions.Approval)

	fmt.Println("Output:")
	if manifest.Output.Name == "" && manifest.Output.Schema == nil {
		fmt.Println("  (none)")
	} else {
		fmt.Printf("  output.name -> %s\n", manifest.Output.Name)
		fmt.Println("  output.schema -> inline")
	}

	fmt.Println("Tools:")
	if len(manifest.Tools) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, tool := range manifest.Tools {
			details := []string{"uses=" + tool.Uses}
			if len(tool.Capabilities) > 0 {
				details = append(details, "capabilities=["+strings.Join(tool.Capabilities, ",")+"]")
			}
			if tool.Command.Run != "" {
				details = append(details, "command.run="+tool.Command.Run)
			}
			if tool.HTTP.URL != "" {
				details = append(details, "http="+tool.HTTP.Method+" "+tool.HTTP.URL)
			}
			fmt.Printf("  tools.%s -> %s\n", tool.Name, strings.Join(details, ", "))
		}
	}

	fmt.Println("Skills:")
	if len(manifest.Skills.Dirs) == 0 && len(manifest.Skills.Active) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, dir := range manifest.Skills.Dirs {
			fmt.Printf("  skills.dirs -> %s\n", dir)
		}
		for _, active := range manifest.Skills.Active {
			fmt.Printf("  skills.active -> %s\n", active)
		}
	}

	fmt.Println("Evaluators:")
	if !manifest.Evaluate.Enabled {
		fmt.Println("  (disabled)")
	} else if len(manifest.Evaluate.Evaluators) == 0 {
		fmt.Println("  (enabled, none)")
	} else {
		for _, evaluator := range manifest.Evaluate.Evaluators {
			details := []string{"uses=" + evaluator.Uses}
			if evaluator.Uses == "llm" {
				modelName := evaluator.Model
				if modelName == "" {
					modelName = manifest.Runtime.Model
				}
				details = append(details, "model=models.providers."+modelName)
				details = append(details, "prompt="+evaluator.Prompt)
			}
			if len(evaluator.Rules) > 0 {
				details = append(details, "rules=["+strings.Join(evaluator.Rules, ",")+"]")
			}
			if evaluator.Command.Run != "" {
				details = append(details, "command.run="+evaluator.Command.Run)
			}
			fmt.Printf("  evaluate.evaluators.%s -> %s\n", evaluator.Name, strings.Join(details, ", "))
		}
	}
}

func printTeamManifestExplanation(manifest *teamrunner.AgentTeamManifest) {
	fmt.Println()
	fmt.Printf("Manifest: %s (%s %s)\n", manifest.Metadata.Name, manifest.Kind, manifest.APIVersion)
	if manifest.Metadata.Description != "" {
		fmt.Printf("Description: %s\n", manifest.Metadata.Description)
	}

	fmt.Println("Lead:")
	fmt.Printf("  lead.agent -> %s\n", manifest.Lead.Agent)

	fmt.Println("Workers:")
	for _, name := range sortedWorkerNames(manifest.Workers) {
		worker := manifest.Workers[name]
		details := []string{"agent=" + worker.Agent}
		if worker.MaxTasks > 0 {
			details = append(details, fmt.Sprintf("maxTasks=%d", worker.MaxTasks))
		}
		if worker.Description != "" {
			details = append(details, "description="+worker.Description)
		}
		fmt.Printf("  workers.%s -> %s\n", name, strings.Join(details, ", "))
	}

	fmt.Println("Runtime:")
	fmt.Printf("  runtime.topology -> %s\n", manifest.Runtime.Topology)
	fmt.Printf("  runtime.limits -> maxRounds=%d, maxTasks=%d, maxParallel=%d, maxRetriesPerTask=%d, maxConsecutiveEmptyRounds=%d\n",
		manifest.Runtime.MaxRounds,
		manifest.Runtime.MaxTasks,
		manifest.Runtime.MaxParallel,
		manifest.Runtime.MaxRetriesPerTask,
		manifest.Runtime.MaxConsecutiveEmptyRounds,
	)

	fmt.Println("Verification:")
	fmt.Printf("  verification.requireStructuredTaskOutput -> %t\n", manifest.Verification.RequireStructuredTaskOutput)
	fmt.Printf("  verification.requireVerifier -> %t\n", manifest.Verification.RequireVerifier)
	if len(manifest.Verification.RequiredTaskFields) == 0 {
		fmt.Println("  verification.requiredTaskFields -> []")
	} else {
		fmt.Printf("  verification.requiredTaskFields -> [%s]\n", strings.Join(manifest.Verification.RequiredTaskFields, ", "))
	}

	fmt.Println("Output:")
	fmt.Printf("  output.dir -> %s\n", manifest.Output.Dir)
}

func sortedProviderNames(providers map[string]config.ModelConfig) []string {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedWorkerNames(workers map[string]teamrunner.Worker) []string {
	names := make([]string, 0, len(workers))
	for name := range workers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
