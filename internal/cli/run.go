package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cosmtrek/jeju/internal/compiler"
	"github.com/cosmtrek/jeju/internal/runtime"
)

const (
	runOutputLive  = "live"
	runOutputFinal = "final"
)

func runAgent(ctx context.Context, manifestPath, task, workspace, output string) error {
	if output != runOutputLive && output != runOutputFinal {
		return fmt.Errorf("run --output must be one of: %s, %s", runOutputLive, runOutputFinal)
	}

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
	rt := runtime.NewWithOptions(runtime.Options{
		SuppressConsoleTrajectory: output == runOutputFinal,
	})
	result, err := rt.Run(ctx, agent, task)
	if err != nil {
		return err
	}
	reportPath, err := writeDefaultRunReport(agent.RunStore, result.RunID)
	if err != nil {
		return fmt.Errorf("write run report: %w", err)
	}
	if output == runOutputFinal {
		if result.Final != "" {
			fmt.Println(strings.TrimRight(result.Final, "\n"))
		}
		return nil
	}
	if result.Final != "" {
		fmt.Printf("\nFinal\n%s\n", strings.TrimRight(result.Final, "\n"))
	}
	fmt.Printf("\nOutputs\n  run_id %s\n  report %s\n", result.RunID, reportPath)
	return nil
}
