package cli

import (
	"context"
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
	cobra.AddTemplateFunc("commandUsage", commandUsage)

	root := &cobra.Command{
		Use:   "jeju",
		Short: "Local-first agent harness",
		Long:  rootLongDescription(),
		Example: `  jeju init research --dir ./research-agent
  jeju validate ./research-agent/agents/research.agent.yaml
  jeju run ./research-agent/agents/research.agent.yaml "Create a short note explaining this agent run lifecycle."
  jeju view
  jeju inspect <run_id>
  jeju view <run_id>`,
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
	root.AddCommand(newPackageCommand(ctx))
	root.AddCommand(newRunCommand(ctx))
	root.AddCommand(newTeamCommand(ctx))
	root.AddCommand(newEvolveCommand(ctx))
	root.AddCommand(newInspectCommand())
	root.AddCommand(newViewCommand())
	root.AddCommand(newVersionCommand())
	return root
}

func rootLongDescription() string {
	return `Jeju - Local-first agent harness

Define behavior in config, run with boundaries, inspect every effect,
and improve with evaluation evidence.

Version:
` + indentLines(formatVersionInfo(), "  ")
}

func indentLines(text, prefix string) string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func commandUsage(cmd *cobra.Command) string {
	use := strings.TrimSpace(cmd.Use)
	if use == "" {
		return cmd.CommandPath()
	}
	rest := strings.TrimSpace(strings.TrimPrefix(use, cmd.Name()))
	if rest == "" {
		return cmd.CommandPath()
	}
	return cmd.CommandPath() + " " + rest
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
		Use:          "validate [--explain] <manifest.yaml>",
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
	var runsDir string
	var inputFrom string
	cmd := &cobra.Command{
		Use:          `run [--workspace <dir>] [--runs-dir <dir>] [--output live|final] [--from clipboard|stdin|<path>] <agent-ref> ["<task>"]`,
		Short:        "Run an agent against a task",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			agentRef, task, err := resolveRunTask(ctx, args, inputFrom, readRunInputSource)
			if err != nil {
				return cmd.FlagErrorFunc()(cmd, err)
			}
			return runAgent(ctx, agentRef, task, workspace, runsDir, output)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "override workspace.path for this run")
	cmd.Flags().StringVar(&runsDir, "runs-dir", "", "run store directory (default: JEJU_RUNS_DIR, ~/.jeju/runs for package refs, or ./runs for local manifests)")
	cmd.Flags().StringVar(&output, "output", runOutputLive, "console output mode: live or final")
	cmd.Flags().StringVar(&inputFrom, "from", "", "read task input from source: clipboard, stdin, -, or file path")
	cmd.Flags().SetInterspersed(false)
	return cmd
}

func isMisplacedRunFlag(arg string) bool {
	return arg == "--workspace" || strings.HasPrefix(arg, "--workspace=") ||
		arg == "--runs-dir" || strings.HasPrefix(arg, "--runs-dir=") ||
		arg == "--output" || strings.HasPrefix(arg, "--output=") ||
		arg == "--from" || strings.HasPrefix(arg, "--from=")
}

func newEvolveCommand(ctx context.Context) *cobra.Command {
	var out string
	var maxIterations int
	var dryRun bool
	var baselineOnly bool
	var runTest bool
	cmd := &cobra.Command{
		Use:          "evolve [--dry-run] [--baseline-only] [--test] [--max-iterations N] [--out <dir>] <experiment.yaml>",
		Short:        "Run an evolution experiment",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEvolve(ctx, args[0], evolveCLIOptions{
				out:           out,
				maxIterations: maxIterations,
				dryRun:        dryRun,
				baselineOnly:  baselineOnly,
				runTest:       runTest,
			})
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "override output directory")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 0, "override search.iterations")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate spec and compile the baseline bundle without model calls")
	cmd.Flags().BoolVar(&baselineOnly, "baseline-only", false, "run baseline train/selection metrics and report without calling evolver")
	cmd.Flags().BoolVar(&runTest, "test", false, "run data.test on baseline and final best after selection")
	return cmd
}

func newInspectCommand() *cobra.Command {
	var runsDir string
	cmd := &cobra.Command{
		Use:          "inspect <run_id>",
		Short:        "Print a run summary and artifact paths",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInspect(args[0], runsDir)
		},
	}
	cmd.Flags().StringVar(&runsDir, "runs-dir", "", "run store directory (default: JEJU_RUNS_DIR or local/global run stores)")
	return cmd
}

func newViewCommand() *cobra.Command {
	var out string
	var runsDir string
	cmd := &cobra.Command{
		Use:          "view [<run_id>|<package-ref>] [--out <html>]",
		Short:        "List runs or open an HTML run report",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			selector := ""
			if len(args) > 0 {
				selector = args[0]
			}
			return runView(selector, out, runsDir)
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "output HTML path")
	cmd.Flags().StringVar(&runsDir, "runs-dir", "", "run store directory (default: JEJU_RUNS_DIR or local/global run stores)")
	return cmd
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

const rootHelpTemplate = `{{if .Long}}{{.Long}}{{else}}Jeju - {{.Short}}{{end}}

Usage:
{{- if .HasAvailableSubCommands}}
{{- range .Commands}}{{if .IsAvailableCommand}}
  {{printf "%-88s" (commandUsage .)}} {{.Short}}{{end}}{{end}}
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
