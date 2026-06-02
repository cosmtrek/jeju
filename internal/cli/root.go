package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func Execute(ctx context.Context, args []string) error {
	cmd := newRootCommand(ctx)
	cmd.SetArgs(args)
	return cmd.ExecuteContext(ctx)
}

func newRootCommand(ctx context.Context) *cobra.Command {
	// Keep help output in onboarding order instead of alphabetical order.
	cobra.EnableCommandSorting = false

	root := &cobra.Command{
		Use:   "jeju",
		Short: "config-defined local agent runtime",
		Example: `  jeju init research --dir ~/jeju-agents/research-agent
  cd ~/jeju-agents/research-agent
  jeju info
  jeju validate agents/research.agent.yaml
  jeju validate --explain agents/research.agent.yaml
  jeju run agents/research.agent.yaml "Create a deep research brief on AI agent evaluation methods, compare three approaches, and save the report to notes.md"
  jeju run --workspace /path/to/project agents/code-review.agent.yaml "Review the current repository changes."
  jeju evolve --baseline-only experiments/research-evolve.yaml
  jeju view 20260526-120000-research`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetHelpTemplate(rootHelpTemplate)

	root.AddCommand(newInitCommand())
	root.AddCommand(newInfoCommand())
	root.AddCommand(newValidateCommand())
	root.AddCommand(newRunCommand(ctx))
	root.AddCommand(newEvolveCommand(ctx))
	root.AddCommand(newInspectCommand())
	root.AddCommand(newViewCommand())
	root.AddCommand(newRunsCommand())
	root.AddCommand(newVersionCommand())
	return root
}

func newInitCommand() *cobra.Command {
	var outputDir string
	cmd := &cobra.Command{
		Use:          "init <name> [<dir>] [--dir <dir>]",
		Short:        "Scaffold a local agent bundle",
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 2 && !cmd.Flags().Changed("dir") {
				outputDir = args[1]
			}
			return runInit(args[0], outputDir)
		},
	}
	cmd.Flags().StringVarP(&outputDir, "dir", "d", ".", "directory to scaffold into")
	return cmd
}

func newInfoCommand() *cobra.Command {
	return &cobra.Command{
		Use:          "info",
		Short:        "List supported providers, tools, evaluators, and trajectory formats",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInfo()
		},
	}
}

func newValidateCommand() *cobra.Command {
	var explain bool
	cmd := &cobra.Command{
		Use:          "validate [--explain] <agent.yaml>",
		Short:        "Validate a manifest and optionally explain resolved wiring",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidate(args[0], explain)
		},
	}
	cmd.Flags().BoolVar(&explain, "explain", false, "explain resolved manifest wiring")
	return cmd
}

func newRunCommand(ctx context.Context) *cobra.Command {
	var workspace string
	var output string
	cmd := &cobra.Command{
		Use:          `run [--workspace <dir>] [--output live|final] <agent.yaml> "<task>"`,
		Short:        "Run an agent against a task",
		Args:         cobra.MinimumNArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if isMisplacedRunFlag(args[1]) {
				return cmd.FlagErrorFunc()(cmd, fmt.Errorf("run flags must appear before <agent.yaml>; use -- before the task if it starts with flag-like text"))
			}
			return runAgent(ctx, args[0], strings.Join(args[1:], " "), workspace, output)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "override workspace.path for this run")
	cmd.Flags().StringVar(&output, "output", runOutputLive, "console output mode: live or final")
	cmd.Flags().SetInterspersed(false)
	return cmd
}

func isMisplacedRunFlag(arg string) bool {
	return arg == "--workspace" || strings.HasPrefix(arg, "--workspace=") ||
		arg == "--output" || strings.HasPrefix(arg, "--output=")
}

func newEvolveCommand(ctx context.Context) *cobra.Command {
	var out string
	var maxIterations int
	var dryRun bool
	var baselineOnly bool
	cmd := &cobra.Command{
		Use:          "evolve [--dry-run] [--baseline-only] [--max-iterations N] [--out <dir>] <experiment.yaml>",
		Short:        "Run an evolution experiment",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEvolve(ctx, args[0], evolveCLIOptions{
				out:           out,
				maxIterations: maxIterations,
				dryRun:        dryRun,
				baselineOnly:  baselineOnly,
			})
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "override output directory")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 0, "override search.iterations")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate spec and compile the baseline bundle without model calls")
	cmd.Flags().BoolVar(&baselineOnly, "baseline-only", false, "run baseline train/selection metrics and report without calling evolver")
	return cmd
}

func newInspectCommand() *cobra.Command {
	return &cobra.Command{
		Use:          "inspect <run_id>",
		Short:        "Print a run summary and artifact paths",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInspect(args[0])
		},
	}
}

func newViewCommand() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:          "view <run_id> [--out <html>]",
		Short:        "Render an HTML run report",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runView(args[0], out)
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "output HTML path")
	return cmd
}

func newRunsCommand() *cobra.Command {
	return &cobra.Command{
		Use:          "runs",
		Short:        "List local runs",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRuns()
		},
	}
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:          "version",
		Short:        "Print build version information",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVersion()
		},
	}
}

const rootHelpTemplate = `Jeju - {{.Short}}

Usage:
{{- if .HasAvailableSubCommands}}
{{- range .Commands}}{{if .IsAvailableCommand}}
  {{printf "%-88s" (printf "jeju %s" .Use)}} {{.Short}}{{end}}{{end}}
{{- else}}
  {{.UseLine}}
{{- end}}
{{- if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}
{{- end}}
{{- if .Example}}

Examples:
{{.Example}}
{{- end}}
`
