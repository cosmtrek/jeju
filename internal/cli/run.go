package cli

import (
	"context"
	"fmt"
	"strings"

	"jeju/internal/compiler"
	"jeju/internal/runtime"
)

func runAgent(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: jeju run <agent.yaml> \"<task>\"")
	}
	manifestPath := args[0]
	task := strings.Join(args[1:], " ")

	agent, err := compiler.Compile(manifestPath)
	if err != nil {
		return err
	}
	rt := runtime.New()
	result, err := rt.Run(ctx, agent, task)
	if err != nil {
		return err
	}
	reportPath, err := writeDefaultRunReport(agent.RunStore, result.RunID)
	if err != nil {
		return fmt.Errorf("write run report: %w", err)
	}
	if result.Final != "" {
		fmt.Printf("\nFinal\n%s\n", strings.TrimRight(result.Final, "\n"))
	}
	fmt.Printf("\nOutputs\n  run_id %s\n  report %s\n", result.RunID, reportPath)
	return nil
}
