package cli

import (
	"fmt"

	"github.com/cosmtrek/jeju/internal/config"
)

func runInfo() error {
	caps := config.SupportedCapabilities()
	fmt.Println("Jeju capabilities")
	fmt.Println()
	printInfoList("Model provider types", caps.ModelProviderTypes)
	printInfoList("Model presets", caps.ModelPresets)
	printInfoList("Tool uses", caps.ToolUses)
	printInfoList("Evaluator uses", caps.EvaluatorUses)
	printInfoList("Trajectory formats", caps.TrajectoryFormats)
	return nil
}

func printInfoList(title string, values []string) {
	fmt.Printf("%s:\n", title)
	for _, value := range values {
		fmt.Printf("  - %s\n", value)
	}
	fmt.Println()
}
