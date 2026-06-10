package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	teamrunner "github.com/cosmtrek/jeju/internal/team"

	"github.com/spf13/cobra"
)

func newTeamCommand(ctx context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "team",
		Short:        "Run lead-worker agent teams",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newTeamRunCommand(ctx))
	return cmd
}

func newTeamRunCommand(ctx context.Context) *cobra.Command {
	var workspace string
	var output string
	var outDir string
	cmd := &cobra.Command{
		Use:          `run [--workspace <dir>] [--out <dir>] [--output live|final] <team.yaml> "<goal>"`,
		Short:        "Run an AgentTeam against a goal",
		Args:         cobra.MinimumNArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTeam(ctx, args[0], strings.Join(args[1:], " "), workspace, outDir, output)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "override workspace.path for lead and worker child runs")
	cmd.Flags().StringVar(&outDir, "out", "", "team output directory")
	cmd.Flags().StringVar(&output, "output", runOutputLive, "console output mode: live or final")
	cmd.Flags().SetInterspersed(false)
	return cmd
}

func runTeam(ctx context.Context, teamPath, goal, workspace, outDir, output string) error {
	if output != runOutputLive && output != runOutputFinal {
		return fmt.Errorf("team run --output must be one of: %s, %s", runOutputLive, runOutputFinal)
	}
	opts := teamrunner.Options{OutputDir: outDir}
	if workspace != "" {
		absWorkspace, err := filepath.Abs(workspace)
		if err != nil {
			return fmt.Errorf("resolve workspace override: %w", err)
		}
		opts.WorkspaceOverride = absWorkspace
	}
	result, err := teamrunner.Run(ctx, teamPath, goal, opts)
	if err != nil {
		return err
	}
	if output == runOutputFinal {
		if result.Final != "" {
			fmt.Println(strings.TrimRight(result.Final, "\n"))
		}
		return teamRunStatusError(result)
	}
	if result.Final != "" {
		fmt.Printf("\nFinal\n%s\n", strings.TrimRight(result.Final, "\n"))
	}
	fmt.Printf("\nOutputs\n  team_run_id %s\n  status %s\n  report %s\n", result.TeamRunID, result.Status, result.Report)
	return teamRunStatusError(result)
}

func teamRunStatusError(result *teamrunner.Result) error {
	if result.Status != teamrunner.StatusFailed {
		return nil
	}
	if strings.TrimSpace(result.Final) == "" {
		return fmt.Errorf("team run failed")
	}
	return fmt.Errorf("team run failed: %s", strings.TrimSpace(result.Final))
}
