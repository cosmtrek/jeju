package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cosmtrek/jeju/internal/config"
	"github.com/cosmtrek/jeju/internal/evaluate"
	"github.com/cosmtrek/jeju/internal/runs"
	"github.com/cosmtrek/jeju/internal/trajectory"
)

func TestCoreFlowInitValidateRunInspectRuns(t *testing.T) {
	tmp := t.TempDir()
	restoreCWD := chdir(t, tmp)
	defer restoreCWD()

	ctx := context.Background()
	if err := Execute(ctx, []string{"init", "research", "--dir", "jeju-work"}); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "agents", "research.agent.yaml")); !os.IsNotExist(err) {
		t.Fatalf("init --dir wrote agent manifest outside requested output dir")
	}

	restoreWorkCWD := chdir(t, filepath.Join(tmp, "jeju-work"))
	defer restoreWorkCWD()

	if err := Execute(ctx, []string{"validate", "agents/research.agent.yaml"}); err != nil {
		t.Fatalf("validate failed: %v", err)
	}

	runOutput := captureStdout(t, func() {
		if err := Execute(ctx, []string{"run", "agents/research.agent.yaml", "Create a deep research brief on AI agent evaluation methods, compare three approaches, and save the report to notes.md"}); err != nil {
			t.Fatalf("run failed: %v", err)
		}
	})

	store := runs.NewStore("./runs")
	items, err := store.ListRuns()
	if err != nil {
		t.Fatalf("ListRuns failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 run, got %d", len(items))
	}
	runID := items[0].RunID
	runDir := filepath.Join(tmp, "jeju-work", "runs", runID)
	reportPath := filepath.Join(runDir, runs.ReportFile)
	assertFileExists(t, reportPath)
	expectedReportOutput, err := filepath.EvalSymlinks(reportPath)
	if err != nil {
		t.Fatalf("resolve report path failed: %v", err)
	}
	if !strings.Contains(runOutput, "report "+expectedReportOutput) {
		t.Fatalf("run output did not include report path %q:\n%s", expectedReportOutput, runOutput)
	}

	if err := Execute(ctx, []string{"runs"}); err != nil {
		t.Fatalf("runs failed: %v", err)
	}
	if err := Execute(ctx, []string{"inspect", runID}); err != nil {
		t.Fatalf("inspect failed: %v", err)
	}
	if err := Execute(ctx, []string{"view", runID}); err != nil {
		t.Fatalf("view failed: %v", err)
	}
	customReport := filepath.Join(tmp, "jeju-work", "custom-report.html")
	if err := Execute(ctx, []string{"view", runID, "--out", customReport}); err != nil {
		t.Fatalf("view --out failed: %v", err)
	}

	assertFileExists(t, filepath.Join(runDir, runs.MetadataFile))
	assertFileExists(t, filepath.Join(runDir, runs.ConfigSnapshotFile))
	assertFileExists(t, filepath.Join(runDir, runs.TrajectoryFile))
	assertFileExists(t, filepath.Join(runDir, runs.FinalFile))
	assertFileExists(t, filepath.Join(runDir, runs.EvaluationFile))
	assertFileExists(t, filepath.Join(runDir, runs.ReportFile))
	assertFileExists(t, customReport)
	assertFileExists(t, filepath.Join(tmp, "jeju-work", "workspace", "research", "notes.md"))

	events, err := trajectory.ReadFile(filepath.Join(runDir, runs.TrajectoryFile))
	if err != nil {
		t.Fatalf("ReadFile trajectory failed: %v", err)
	}
	requireEventTypes(t, events,
		trajectory.EventRunStarted,
		trajectory.EventSkillDisclosed,
		trajectory.EventSkillLoaded,
		trajectory.EventStepStarted,
		trajectory.EventModelStarted,
		trajectory.EventModelCompleted,
		trajectory.EventActionParsed,
		trajectory.EventToolRequested,
		trajectory.EventPermissionChecked,
		trajectory.EventPermissionApproved,
		trajectory.EventToolStarted,
		trajectory.EventToolCompleted,
		trajectory.EventStepCompleted,
		trajectory.EventEvaluationStarted,
		trajectory.EventEvaluationCompleted,
		trajectory.EventRunCompleted,
	)

	var result evaluate.Result
	data, err := os.ReadFile(filepath.Join(runDir, runs.EvaluationFile))
	if err != nil {
		t.Fatalf("read evaluation failed: %v", err)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal evaluation failed: %v", err)
	}
	if !result.Passed || result.Score != 1 {
		t.Fatalf("expected passing evaluation score=1, got passed=%v score=%v", result.Passed, result.Score)
	}
}

func TestRunWorkspaceOverride(t *testing.T) {
	tmp := t.TempDir()
	restoreCWD := chdir(t, tmp)
	defer restoreCWD()

	ctx := context.Background()
	if err := Execute(ctx, []string{"init", "research", "--dir", "jeju-work"}); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	targetWorkspace := filepath.Join(tmp, "target-repo")
	if err := os.MkdirAll(targetWorkspace, 0o755); err != nil {
		t.Fatalf("create target workspace failed: %v", err)
	}

	restoreWorkCWD := chdir(t, filepath.Join(tmp, "jeju-work"))
	defer restoreWorkCWD()

	if err := Execute(ctx, []string{"run", "--workspace", targetWorkspace, "agents/research.agent.yaml", "Save a short note to notes.md"}); err != nil {
		t.Fatalf("run with workspace override failed: %v", err)
	}

	assertFileExists(t, filepath.Join(targetWorkspace, "notes.md"))
	if _, err := os.Stat(filepath.Join(tmp, "jeju-work", "workspace", "research", "notes.md")); !os.IsNotExist(err) {
		t.Fatalf("run wrote notes.md to the manifest default workspace instead of override")
	}

	store := runs.NewStore("./runs")
	items, err := store.ListRuns()
	if err != nil {
		t.Fatalf("ListRuns failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 run, got %d", len(items))
	}
	snapshot, err := os.ReadFile(filepath.Join("runs", items[0].RunID, runs.ConfigSnapshotFile))
	if err != nil {
		t.Fatalf("read config snapshot failed: %v", err)
	}
	if !strings.Contains(string(snapshot), targetWorkspace) {
		t.Fatalf("config snapshot does not include workspace override %q:\n%s", targetWorkspace, snapshot)
	}
}

func TestRunTaskCanStartWithFlagLikeText(t *testing.T) {
	tmp := t.TempDir()
	restoreCWD := chdir(t, tmp)
	defer restoreCWD()

	ctx := context.Background()
	if err := Execute(ctx, []string{"init", "research", "--dir", "jeju-work"}); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	restoreWorkCWD := chdir(t, filepath.Join(tmp, "jeju-work"))
	defer restoreWorkCWD()

	if err := Execute(ctx, []string{"run", "agents/research.agent.yaml", "--write", "notes.md"}); err != nil {
		t.Fatalf("run should accept flag-like task text after manifest: %v", err)
	}
}

func TestRunRejectsWorkspaceFlagAfterManifest(t *testing.T) {
	tmp := t.TempDir()
	restoreCWD := chdir(t, tmp)
	defer restoreCWD()

	ctx := context.Background()
	if err := Execute(ctx, []string{"init", "research", "--dir", "jeju-work"}); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	restoreWorkCWD := chdir(t, filepath.Join(tmp, "jeju-work"))
	defer restoreWorkCWD()

	err := Execute(ctx, []string{"run", "agents/research.agent.yaml", "--workspace", tmp, "Save a short note"})
	if err == nil {
		t.Fatal("expected misplaced --workspace error")
	}
	if !strings.Contains(err.Error(), "run flags must appear before <agent.yaml>") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteHelpPrintsRootUsage(t *testing.T) {
	output := captureStdout(t, func() {
		if err := Execute(context.Background(), []string{"--help"}); err != nil {
			t.Fatalf("help failed: %v", err)
		}
	})
	for _, want := range []string{
		"Jeju - config-defined local agent runtime",
		"jeju init <name> [<dir>] [--dir <dir>]",
		"Scaffold a local agent bundle",
		"jeju info",
		"List supported providers, tools, evaluators, and trajectory formats",
		"jeju validate [--explain] <agent.yaml>",
		"Validate a manifest and optionally explain resolved wiring",
		"jeju run [--workspace <dir>] <agent.yaml> \"<task>\"",
		"Run an agent against a task",
		"jeju evolve [--dry-run] [--baseline-only] [--max-iterations N] [--out <dir>] <experiment.yaml>",
		"Run an evolution experiment",
		"jeju inspect <run_id>",
		"Print a run summary and artifact paths",
		"jeju view <run_id> [--out <html>]",
		"Render an HTML run report",
		"jeju runs",
		"List local runs",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("help output missing %q:\n%s", want, output)
		}
	}
}

func TestExecuteSubcommandHelpPrintsFlags(t *testing.T) {
	output := captureStdout(t, func() {
		if err := Execute(context.Background(), []string{"run", "--help"}); err != nil {
			t.Fatalf("run help failed: %v", err)
		}
	})
	for _, want := range []string{
		"Jeju - Run an agent against a task",
		`jeju run [--workspace <dir>] <agent.yaml> "<task>" [flags]`,
		"--workspace string",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("run help output missing %q:\n%s", want, output)
		}
	}
}

func TestEvolveHelpPrintsFlags(t *testing.T) {
	output := captureStdout(t, func() {
		if err := Execute(context.Background(), []string{"evolve", "--help"}); err != nil {
			t.Fatalf("evolve help failed: %v", err)
		}
	})
	for _, want := range []string{
		"Jeju - Run an evolution experiment",
		"jeju evolve [--dry-run] [--baseline-only] [--max-iterations N] [--out <dir>] <experiment.yaml> [flags]",
		"--dry-run",
		"--baseline-only",
		"--max-iterations int",
		"--out string",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("evolve help output missing %q:\n%s", want, output)
		}
	}
}

func TestInitKeepsLegacyPositionalOutputDir(t *testing.T) {
	tmp := t.TempDir()
	restoreCWD := chdir(t, tmp)
	defer restoreCWD()

	if err := Execute(context.Background(), []string{"init", "research", "jeju-work"}); err != nil {
		t.Fatalf("init with positional output dir failed: %v", err)
	}
	assertFileExists(t, filepath.Join(tmp, "jeju-work", "agents", "research.agent.yaml"))
}

func TestInitDirFlagWinsOverLegacyPositionalOutputDir(t *testing.T) {
	tmp := t.TempDir()
	restoreCWD := chdir(t, tmp)
	defer restoreCWD()

	if err := Execute(context.Background(), []string{"init", "research", "legacy-dir", "--dir", "flag-dir"}); err != nil {
		t.Fatalf("init with positional and --dir output dirs failed: %v", err)
	}
	assertFileExists(t, filepath.Join(tmp, "flag-dir", "agents", "research.agent.yaml"))
	if _, err := os.Stat(filepath.Join(tmp, "legacy-dir", "agents", "research.agent.yaml")); !os.IsNotExist(err) {
		t.Fatalf("init used legacy positional output dir even though --dir was set")
	}
}

func TestInfoPrintsSupportedCapabilities(t *testing.T) {
	output := captureStdout(t, func() {
		if err := Execute(context.Background(), []string{"info"}); err != nil {
			t.Fatalf("info failed: %v", err)
		}
	})
	for _, want := range []string{
		"Model provider types:",
		"openaiCompatible",
		"Tool uses:",
		"builtin:search",
		"Evaluator uses:",
		"Trajectory formats:",
		"jeju-jsonl",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("info output missing %q:\n%s", want, output)
		}
	}
}

func TestValidateExplainPrintsManifestConnections(t *testing.T) {
	tmp := t.TempDir()
	restoreCWD := chdir(t, tmp)
	defer restoreCWD()

	ctx := context.Background()
	if err := Execute(ctx, []string{"init", "research", "--dir", "jeju-work"}); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	restoreWorkCWD := chdir(t, filepath.Join(tmp, "jeju-work"))
	defer restoreWorkCWD()

	output := captureStdout(t, func() {
		if err := Execute(ctx, []string{"validate", "--explain", "agents/research.agent.yaml"}); err != nil {
			t.Fatalf("validate --explain failed: %v", err)
		}
	})
	expectedWorkspacePath, err := filepath.Abs(filepath.Join("workspace", "research"))
	if err != nil {
		t.Fatalf("resolve expected workspace path failed: %v", err)
	}
	for _, want := range []string{
		"valid: agents/research.agent.yaml",
		"Manifest: research (Agent jeju/v1alpha1)",
		"runtime.model -> models.providers.primary",
		"workspace.path -> " + expectedWorkspacePath,
		"permissions.approval -> never",
		"tools.search_api -> uses=http",
		"skills.active -> web-research",
		"evaluate.evaluators.basic_trajectory -> uses=rules",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("validate --explain output missing %q:\n%s", want, output)
		}
	}
}

func TestValidateExplainDistinguishesEnabledEmptyEvaluators(t *testing.T) {
	manifest := minimalExplainManifest(t, true)
	output := captureStdout(t, func() {
		printManifestExplanation(manifest)
	})
	if !strings.Contains(output, "Evaluators:\n  (enabled, none)") {
		t.Fatalf("expected enabled empty evaluators explanation, got:\n%s", output)
	}

	manifest.Evaluate.Enabled = false
	output = captureStdout(t, func() {
		printManifestExplanation(manifest)
	})
	if !strings.Contains(output, "Evaluators:\n  (disabled)") {
		t.Fatalf("expected disabled evaluators explanation, got:\n%s", output)
	}
}

func TestValidateRejectsUnknownOption(t *testing.T) {
	err := Execute(context.Background(), []string{"validate", "--unknown", "agent.yaml"})
	if err == nil {
		t.Fatal("expected validate unknown option error")
	}
	if !strings.Contains(err.Error(), `unknown flag: --unknown`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func minimalExplainManifest(t *testing.T, evaluateEnabled bool) *config.AgentManifest {
	t.Helper()
	tmp := t.TempDir()
	promptPath := filepath.Join(tmp, "prompt.md")
	if err := os.WriteFile(promptPath, []byte("test prompt"), 0o644); err != nil {
		t.Fatalf("write prompt failed: %v", err)
	}
	workspacePath := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("create workspace failed: %v", err)
	}
	manifest := &config.AgentManifest{
		APIVersion: "jeju/v1alpha1",
		Kind:       "Agent",
		Metadata: config.Metadata{
			Name: "agent",
		},
		Models: config.ModelsConfig{
			Providers: map[string]config.ModelConfig{
				"primary": {
					Type:          "mock",
					Model:         "mock-react",
					ContextWindow: 128000,
				},
			},
		},
		Instructions: config.InstructionsConfig{System: promptPath},
		Runtime: config.RuntimeConfig{
			Model:                "primary",
			Loop:                 config.LoopConfig{Type: "react"},
			CompressionThreshold: 0.8,
			Limits: config.RuntimeLimits{
				MaxSteps:             1,
				MaxDurationSec:       30,
				MaxToolCalls:         1,
				MaxConsecutiveErrors: 1,
			},
		},
		Workspace:   config.WorkspaceConfig{Path: workspacePath},
		Permissions: config.PermissionsConfig{Access: "workspace", Approval: "never"},
		Evaluate:    config.EvaluateConfig{Enabled: evaluateEnabled},
	}
	if err := config.Validate(manifest); err != nil {
		t.Fatalf("minimal manifest should validate: %v", err)
	}
	return manifest
}

func TestExecuteUnknownCommandReturnsError(t *testing.T) {
	err := Execute(context.Background(), []string{"unknown"})
	if err == nil {
		t.Fatal("expected unknown command error")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func chdir(t *testing.T, dir string) func() {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	return func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("restore cwd failed: %v", err)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe failed: %v", err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = old
		_ = reader.Close()
	}()
	fn()
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer failed: %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout failed: %v", err)
	}
	return string(data)
}

func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	old := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe failed: %v", err)
	}
	if _, err := writer.WriteString(input); err != nil {
		t.Fatalf("write stdin pipe failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdin writer failed: %v", err)
	}
	os.Stdin = reader
	defer func() {
		os.Stdin = old
		_ = reader.Close()
	}()
	fn()
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.IsDir() {
		t.Fatalf("expected %s to be a file", path)
	}
}

func requireEventTypes(t *testing.T, events []trajectory.Event, types ...trajectory.EventType) {
	t.Helper()
	seen := map[trajectory.EventType]bool{}
	for _, event := range events {
		seen[event.Type] = true
	}
	for _, typ := range types {
		if !seen[typ] {
			t.Fatalf("missing trajectory event type %q", typ)
		}
	}
}
