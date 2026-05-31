package cli

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"jeju/internal/compiler"
	"jeju/internal/runtime"
)

func runAgent(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	workspace := flags.String("workspace", "", "override workspace.path for this run")
	positionals, err := parseRunArgs(flags, args)
	if err != nil {
		return err
	}
	if len(positionals) < 2 {
		return fmt.Errorf("usage: jeju run [--workspace <dir>] <agent.yaml> \"<task>\"")
	}
	manifestPath := positionals[0]
	task := strings.Join(positionals[1:], " ")

	opts := compiler.Options{}
	if *workspace != "" {
		absWorkspace, err := filepath.Abs(*workspace)
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

func parseRunArgs(flags *flag.FlagSet, args []string) ([]string, error) {
	flagArgs := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if err := flags.Parse(flagArgs); err != nil {
				return nil, err
			}
			return args[i+1:], nil
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
		if err := flags.Parse(flagArgs); err != nil {
			return nil, err
		}
		return args[i:], nil
	}
	if err := flags.Parse(flagArgs); err != nil {
		return nil, err
	}
	return nil, nil
}
