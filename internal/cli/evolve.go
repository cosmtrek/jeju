package cli

import (
	"context"
	"fmt"

	"github.com/cosmtrek/jeju/internal/evolve"
)

type evolveCLIOptions struct {
	out           string
	maxIterations int
	dryRun        bool
	baselineOnly  bool
	runTest       bool
}

func runEvolve(ctx context.Context, experimentPath string, opts evolveCLIOptions) error {
	result, err := evolve.Run(ctx, experimentPath, evolve.RunOptions{
		Out:           opts.out,
		MaxIterations: opts.maxIterations,
		DryRun:        opts.dryRun,
		BaselineOnly:  opts.baselineOnly,
		RunTest:       opts.runTest,
	})
	if err != nil {
		return err
	}
	fmt.Printf("\nEvolution\n  experiment %s\n  best       %s\n  output     %s\n  report     %s\n", result.ExperimentID, result.BestID, result.OutputDir, result.ReportPath)
	return nil
}
