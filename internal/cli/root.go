package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func Execute(ctx context.Context, args []string) error {
	cmd := newRootCommand(ctx)
	cmd.SetArgs(args)
	return cmd.ExecuteContext(ctx)
}

func newRootCommand(ctx context.Context) *cobra.Command {
	root := &cobra.Command{
		Use:           "jeju",
		Short:         "Config-defined local agent runtime",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			printHelp(cmd)
			return nil
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printHelp(cmd)
	})

	root.AddCommand(
		rawArgsCommand("init", func(args []string) error {
			return runInit(args)
		}),
		rawArgsCommand("info", func(args []string) error {
			return runInfo(args)
		}),
		rawArgsCommand("validate", func(args []string) error {
			return runValidate(args)
		}),
		rawArgsCommand("run", func(args []string) error {
			return runAgent(ctx, args)
		}),
		rawArgsCommand("inspect", func(args []string) error {
			return runInspect(args)
		}),
		rawArgsCommand("runs", func(args []string) error {
			return runRuns(args)
		}),
		rawArgsCommand("view", func(args []string) error {
			return runView(args)
		}),
		rawArgsCommand("evolve", func(args []string) error {
			return runEvolve(ctx, args)
		}),
	)
	return root
}

func rawArgsCommand(use string, run func([]string) error) *cobra.Command {
	return &cobra.Command{
		Use:                use,
		DisableFlagParsing: true,
		SilenceUsage:       true,
		SilenceErrors:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(args)
		},
	}
}

func printHelp(cmd *cobra.Command) {
	fmt.Fprintln(cmd.OutOrStdout(), `Jeju - config-defined local agent runtime

Usage:
  jeju init <name> [--dir <dir>]
  jeju info
  jeju validate [--explain] <agent.yaml>
  jeju run [--workspace <dir>] <agent.yaml> "<task>"
  jeju evolve [--baseline-only] [--max-iterations N] [--out <dir>] <experiment.yaml>
  jeju inspect <run_id>
  jeju view <run_id> [--out <html>]
  jeju runs

Examples:
  jeju init research --dir ~/jeju-agents/research-agent
  cd ~/jeju-agents/research-agent
  jeju info
  jeju validate agents/research.agent.yaml
  jeju validate --explain agents/research.agent.yaml
  jeju run agents/research.agent.yaml "Create a deep research brief on AI agent evaluation methods, compare three approaches, and save the report to notes.md"
  jeju run --workspace /path/to/project agents/code-review.agent.yaml "Review the current repository changes."
  jeju evolve --baseline-only experiments/research-evolve.yaml
  jeju view 20260526-120000-research`)
}
