package cli

import (
	"fmt"
	"sort"
	"strings"

	"jeju/internal/config"
)

func runValidate(args []string) error {
	manifestPath, explain, err := parseValidateArgs(args)
	if err != nil {
		return err
	}
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

func parseValidateArgs(args []string) (string, bool, error) {
	explain := false
	manifestPath := ""
	for _, arg := range args {
		switch {
		case arg == "--explain":
			explain = true
		case strings.HasPrefix(arg, "-"):
			return "", false, fmt.Errorf("unknown validate option %q", arg)
		case manifestPath == "":
			manifestPath = arg
		default:
			return "", false, fmt.Errorf("usage: jeju validate [--explain] <agent.yaml>")
		}
	}
	if manifestPath == "" {
		return "", false, fmt.Errorf("usage: jeju validate [--explain] <agent.yaml>")
	}
	return manifestPath, explain, nil
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

func sortedProviderNames(providers map[string]config.ModelConfig) []string {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
