package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"jeju/internal/compiler"
	"jeju/internal/runtime"
)

func runAgent(ctx context.Context, manifestPath, task, workspace string) error {
	opts := compiler.Options{}
	if workspace != "" {
		absWorkspace, err := filepath.Abs(workspace)
		if err != nil {
			return fmt.Errorf("resolve workspace override: %w", err)
		}
		opts.WorkspaceOverride = absWorkspace
	}
	agent, err := compiler.CompileWithOptions(manifestPath, opts)
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
