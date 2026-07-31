package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cosmtrek/jeju/internal/agentpkg"
	"github.com/cosmtrek/jeju/internal/compiler"
	"github.com/cosmtrek/jeju/internal/runs"
	"github.com/cosmtrek/jeju/internal/runtime"
)

const (
	runOutputLive  = "live"
	runOutputFinal = "final"
)

func runAgent(ctx context.Context, agentRef, task, workspace, runsDir, output, modelID string) error {
	if output != runOutputLive && output != runOutputFinal {
		return fmt.Errorf("run --output must be one of: %s, %s", runOutputLive, runOutputFinal)
	}

	resolved := agentpkg.RunRef{AgentManifestPath: agentRef}
	if agentpkg.IsPackageBackedRef(agentRef) {
		store, err := agentpkg.DefaultStore()
		if err != nil {
			return err
		}
		resolved, err = store.ResolveRunRef(ctx, agentRef, version)
		if err != nil {
			return err
		}
	}

	runStoreDir, err := resolveRunWriteDir(runsDir, agentRef)
	if err != nil {
		return err
	}
	opts := compiler.Options{
		RunStore:      runs.NewStore(runStoreDir),
		ModelOverride: modelID,
	}
	if workspace != "" {
		absWorkspace, err := filepath.Abs(workspace)
		if err != nil {
			return fmt.Errorf("resolve workspace override: %w", err)
		}
		opts.WorkspaceOverride = absWorkspace
	}
	agent, err := compiler.CompileWithOptions(resolved.AgentManifestPath, opts)
	if err != nil {
		return err
	}
	if resolved.Package != nil {
		agent.PackageProvenance = resolved.Package.Map()
		if output == runOutputLive {
			printRunPackageSummary(*resolved.Package, agentpkg.DeriveRisk(agent.Config))
		}
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
		return runStatusError(result)
	}
	if result.Final != "" {
		fmt.Printf("\nFinal\n%s\n", strings.TrimRight(result.Final, "\n"))
	}
	fmt.Printf("\nOutputs\n  run_id %s\n  report %s\n", result.RunID, reportPath)
	return runStatusError(result)
}

func printRunPackageSummary(provenance agentpkg.RunProvenance, risk agentpkg.RiskSummary) {
	fmt.Printf("Package %s@%s\n", provenance.ID, provenance.Version)
	fmt.Printf("  digest %s\n", provenance.Digest)
	if provenance.Source != "" {
		fmt.Printf("  source %s\n", provenance.Source)
	}
	fmt.Printf("  risk %s  access=%s approval=%s\n", risk.Level, risk.Access, risk.Approval)
	if len(risk.Capabilities) > 0 {
		fmt.Printf("  capabilities %s\n", strings.Join(risk.Capabilities, ","))
	}
	fmt.Println()
}

func runStatusError(result *runtime.RunResult) error {
	if result.Status == runtime.StatusCompleted {
		return nil
	}
	status := strings.TrimSpace(string(result.Status))
	if status == "" {
		status = "unknown"
	}
	final := strings.TrimSpace(result.Final)
	if final == "" {
		return fmt.Errorf("run %s (run_id %s)", status, result.RunID)
	}
	return fmt.Errorf("run %s (run_id %s): %s", status, result.RunID, summarizeRunFinal(final))
}

func summarizeRunFinal(final string) string {
	firstLine := strings.TrimSpace(strings.SplitN(final, "\n", 2)[0])
	if firstLine == "" {
		return "no final summary"
	}
	const maxLen = 180
	if len(firstLine) <= maxLen {
		return firstLine
	}
	return firstLine[:maxLen] + "..."
}
