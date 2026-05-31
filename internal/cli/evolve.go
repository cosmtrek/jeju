package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"jeju/internal/evolve"
)

func runEvolve(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("evolve", flag.ContinueOnError)
	out := flags.String("out", "", "override output directory")
	maxIterations := flags.Int("max-iterations", 0, "override search.iterations")
	dryRun := flags.Bool("dry-run", false, "validate spec and compile the baseline bundle without model calls")
	baselineOnly := flags.Bool("baseline-only", false, "run baseline train/selection metrics and report without calling evolver")
	positionals, err := parseEvolveArgs(flags, args)
	if err != nil {
		return err
	}
	if len(positionals) != 1 {
		return fmt.Errorf("usage: jeju evolve [--out <dir>] [--max-iterations N] [--baseline-only] [--dry-run] <experiment.yaml>")
	}
	result, err := evolve.Run(ctx, positionals[0], evolve.RunOptions{
		Out:           *out,
		MaxIterations: *maxIterations,
		DryRun:        *dryRun,
		BaselineOnly:  *baselineOnly,
	})
	if err != nil {
		return err
	}
	fmt.Printf("\nEvolution\n  experiment %s\n  best       %s\n  output     %s\n  report     %s\n", result.ExperimentID, result.BestID, result.OutputDir, result.ReportPath)
	return nil
}

func parseEvolveArgs(flags *flag.FlagSet, args []string) ([]string, error) {
	flagArgs := make([]string, 0, len(args))
	positionals := make([]string, 0, 1)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			flagArgs = append(flagArgs, arg)
			if strings.Contains(arg, "=") {
				continue
			}
			name := strings.TrimLeft(arg, "-")
			if f := flags.Lookup(name); f != nil {
				if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
					continue
				}
				if i+1 >= len(args) {
					return nil, fmt.Errorf("flag needs an argument: -%s", name)
				}
				i++
				flagArgs = append(flagArgs, args[i])
			}
			continue
		}
		positionals = append(positionals, arg)
	}
	if err := flags.Parse(flagArgs); err != nil {
		return nil, err
	}
	return positionals, nil
}
