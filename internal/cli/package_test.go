package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cosmtrek/jeju/internal/agentpkg"
	"github.com/cosmtrek/jeju/internal/runs"
	"github.com/cosmtrek/jeju/internal/trajectory"
)

func TestPackageLifecycleAndRunRef(t *testing.T) {
	disableReportOpen(t)

	tmp := t.TempDir()
	restoreCWD := chdir(t, tmp)
	defer restoreCWD()
	t.Setenv(agentpkg.StoreEnv, filepath.Join(tmp, "package-store"))

	ctx := context.Background()
	if err := Execute(ctx, []string{"init", "research", "--dir", "jeju-work"}); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	pkgRoot := filepath.Join(tmp, "jeju-work")
	if err := Execute(ctx, []string{
		"package", "init", pkgRoot,
		"--agent", "agents/research.agent.yaml",
		"--id", "research/research",
		"--version", "0.1.0",
	}); err != nil {
		t.Fatalf("package init failed: %v", err)
	}
	assertFileExists(t, filepath.Join(pkgRoot, agentpkg.ManifestFile))

	if err := Execute(ctx, []string{"package", "validate", pkgRoot}); err != nil {
		t.Fatalf("package validate failed: %v", err)
	}
	if err := Execute(ctx, []string{"package", "add", pkgRoot}); err != nil {
		t.Fatalf("package add failed: %v", err)
	}
	listOutput := captureStdout(t, func() {
		if err := Execute(ctx, []string{"package", "ls"}); err != nil {
			t.Fatalf("package ls failed: %v", err)
		}
	})
	if !strings.Contains(listOutput, "research/research 0.1.0 * sha256:") {
		t.Fatalf("package ls missing active package:\n%s", listOutput)
	}
	inspectOutput := captureStdout(t, func() {
		if err := Execute(ctx, []string{"package", "inspect", "research/research"}); err != nil {
			t.Fatalf("package inspect failed: %v", err)
		}
	})
	for _, want := range []string{"id: research/research", "version: 0.1.0", "digest: sha256:", "agent: agents/research.agent.yaml"} {
		if !strings.Contains(inspectOutput, want) {
			t.Fatalf("package inspect missing %q:\n%s", want, inspectOutput)
		}
	}

	targetWorkspace := filepath.Join(tmp, "target-workspace")
	runsDir := filepath.Join(tmp, "package-runs")
	if err := os.MkdirAll(targetWorkspace, 0o755); err != nil {
		t.Fatalf("create target workspace failed: %v", err)
	}
	if err := Execute(ctx, []string{
		"run",
		"--workspace", targetWorkspace,
		"--runs-dir", runsDir,
		"p:research/research@0.1.0",
		"Save a short note to notes.md",
	}); err != nil {
		t.Fatalf("run package ref failed: %v", err)
	}
	assertFileExists(t, filepath.Join(targetWorkspace, "notes.md"))
	assertPackageProvenance(t, runsDir, "research/research", "0.1.0")

	distDir := filepath.Join(tmp, "dist")
	if err := Execute(ctx, []string{"package", "pack", pkgRoot, "--out", distDir}); err != nil {
		t.Fatalf("package pack failed: %v", err)
	}
	artifactPath := filepath.Join(distDir, "research-research-0.1.0.jpkg")
	assertFileExists(t, artifactPath)
	if err := Execute(ctx, []string{"package", "rm", "research/research"}); err != nil {
		t.Fatalf("package rm failed: %v", err)
	}
	if err := Execute(ctx, []string{"package", "add", artifactPath}); err != nil {
		t.Fatalf("package add artifact failed: %v", err)
	}
}

func TestRunDirectGenericGitPackageSource(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	disableReportOpen(t)

	tmp := t.TempDir()
	restoreCWD := chdir(t, tmp)
	defer restoreCWD()
	t.Setenv(agentpkg.StoreEnv, filepath.Join(tmp, "package-store"))

	ctx := context.Background()
	repo := filepath.Join(tmp, "repo.git")
	packageRoot := filepath.Join(repo, "coding", "review")
	if err := Execute(ctx, []string{"init", "research", "--dir", packageRoot}); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if err := Execute(ctx, []string{
		"package", "init", packageRoot,
		"--agent", "agents/research.agent.yaml",
		"--id", "coding/review",
		"--version", "0.1.0",
	}); err != nil {
		t.Fatalf("package init failed: %v", err)
	}
	runGitForCLITest(t, "", "init", repo)
	runGitForCLITest(t, repo, "config", "user.email", "test@example.com")
	runGitForCLITest(t, repo, "config", "user.name", "Test User")
	runGitForCLITest(t, repo, "add", ".")
	runGitForCLITest(t, repo, "commit", "-m", "add package")
	commit := strings.TrimSpace(runGitForCLITest(t, repo, "rev-parse", "HEAD"))

	targetWorkspace := filepath.Join(tmp, "target-workspace")
	runsDir := filepath.Join(tmp, "runs")
	if err := os.MkdirAll(targetWorkspace, 0o755); err != nil {
		t.Fatalf("create target workspace failed: %v", err)
	}
	source := "git+file://" + filepath.ToSlash(repo) + "//coding/review?ref=" + commit
	if err := Execute(ctx, []string{
		"run",
		"--workspace", targetWorkspace,
		"--runs-dir", runsDir,
		source,
		"Save a short note to notes.md",
	}); err != nil {
		t.Fatalf("run direct git package source failed: %v", err)
	}
	assertFileExists(t, filepath.Join(targetWorkspace, "notes.md"))
	assertPackageProvenance(t, runsDir, "coding/review", "0.1.0")
}

func assertPackageProvenance(t *testing.T, runsDir, id, version string) {
	t.Helper()
	store := runs.NewStore(runsDir)
	items, err := store.ListRuns()
	if err != nil {
		t.Fatalf("ListRuns failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 package run, got %d", len(items))
	}
	events, err := trajectory.ReadFile(filepath.Join(runsDir, items[0].RunID, runs.TrajectoryFile))
	if err != nil {
		t.Fatalf("read package trajectory failed: %v", err)
	}
	headerHasPackage := false
	artifactHasPackage := false
	for _, event := range events {
		if event.Type == trajectory.EventTrajectoryHeader {
			agentPayload, _ := event.Payload["agent"].(map[string]any)
			packagePayload, _ := agentPayload["package"].(map[string]any)
			if packagePayload["id"] == id && packagePayload["version"] == version {
				headerHasPackage = true
			}
		}
		if event.Type == trajectory.EventArtifactCreated && event.Payload["role"] == "package_provenance" {
			artifactHasPackage = true
		}
	}
	if !headerHasPackage {
		t.Fatalf("trajectory header missing package provenance")
	}
	if !artifactHasPackage {
		t.Fatalf("trajectory missing package provenance artifact")
	}
}

func runGitForCLITest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}
