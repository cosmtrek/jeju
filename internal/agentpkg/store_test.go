package agentpkg

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreAddGenericGitSource(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo.git")
	runCommand(t, "", "git", "init", repo)
	runCommand(t, repo, "git", "config", "user.email", "test@example.com")
	runCommand(t, repo, "git", "config", "user.name", "Test User")
	packageRoot := filepath.Join(repo, "agents", "review")
	writeValidPackageAt(t, packageRoot)
	runCommand(t, repo, "git", "add", ".")
	runCommand(t, repo, "git", "commit", "-m", "add package")
	commit := strings.TrimSpace(runCommand(t, repo, "git", "rev-parse", "HEAD"))

	store := NewStore(filepath.Join(tmp, "store"))
	source := "git+file://" + filepath.ToSlash(repo) + "//agents/review?ref=" + commit
	result, err := store.Add(context.Background(), source, true, "dev")
	if err != nil {
		t.Fatalf("Add git source failed: %v", err)
	}
	if result.ID != "test/review" || result.Version != "0.1.0" {
		t.Fatalf("unexpected package %s@%s", result.ID, result.Version)
	}
	if result.Resolved.Type != "git" || result.Resolved.Commit != commit || result.Resolved.Subdir != "agents/review" {
		t.Fatalf("unexpected resolved source: %+v", result.Resolved)
	}
	resolved, err := store.ResolveRunRef(context.Background(), "package://test/review@0.1.0", "dev")
	if err != nil {
		t.Fatalf("ResolveRunRef failed: %v", err)
	}
	if !strings.HasPrefix(resolved.AgentManifestPath, result.StorePath) {
		t.Fatalf("agent manifest %q is not inside store path %q", resolved.AgentManifestPath, result.StorePath)
	}
}

func TestStoreUpdateRequiresReplaceForSameVersionDigestChange(t *testing.T) {
	tmp := t.TempDir()
	packageRoot := filepath.Join(tmp, "package")
	writeValidPackageAt(t, packageRoot)
	store := NewStore(filepath.Join(tmp, "store"))

	first, err := store.Add(context.Background(), packageRoot, true, "dev")
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	appendFile(t, filepath.Join(packageRoot, "instructions.md"), "New package content.\n")

	_, err = store.Update(context.Background(), "test/review", "0.1.0", "dev", false)
	if err == nil {
		t.Fatal("expected update without replace to fail")
	}
	if !strings.Contains(err.Error(), "--replace") {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := store.Update(context.Background(), "test/review", "0.1.0", "dev", true)
	if err != nil {
		t.Fatalf("Update with replace failed: %v", err)
	}
	if second.Digest == first.Digest {
		t.Fatalf("expected digest to change after content update, got %s", second.Digest)
	}
	inspect, err := store.Inspect("test/review@0.1.0", "dev")
	if err != nil {
		t.Fatalf("Inspect failed: %v", err)
	}
	if inspect.Digest != second.Digest {
		t.Fatalf("active digest = %s, want %s", inspect.Digest, second.Digest)
	}
	if inspect.AgentManifestPath != filepath.Join(inspect.StorePath, "agent.yaml") {
		t.Fatalf("agent manifest path = %q, want it inside %q", inspect.AgentManifestPath, inspect.StorePath)
	}

	appendFile(t, filepath.Join(packageRoot, "instructions.md"), "More package content.\n")
	_, err = store.Add(context.Background(), packageRoot, true, "dev")
	if err == nil {
		t.Fatal("expected add without replace to fail")
	}
	third, err := store.AddWithOptions(context.Background(), packageRoot, AddOptions{
		Activate:    true,
		JejuVersion: "dev",
		Replace:     true,
	})
	if err != nil {
		t.Fatalf("AddWithOptions with replace failed: %v", err)
	}
	if third.Digest == second.Digest {
		t.Fatalf("expected digest to change after second content update, got %s", third.Digest)
	}
}

func TestFirstVersionUsesSemanticOrdering(t *testing.T) {
	got := firstVersion(map[string]InstalledVersion{
		"0.9.0":  {},
		"0.10.0": {},
		"0.2.0":  {},
	})
	if got != "0.10.0" {
		t.Fatalf("firstVersion() = %q, want 0.10.0", got)
	}
}

func TestFirstVersionPrefersReleaseOverPrerelease(t *testing.T) {
	got := firstVersion(map[string]InstalledVersion{
		"1.0.0-rc.1": {},
		"1.0.0":      {},
		"1.0.0-beta": {},
	})
	if got != "1.0.0" {
		t.Fatalf("firstVersion() = %q, want 1.0.0", got)
	}
}

func TestResolveInstalledRunRefUsesStoredPackageMetadata(t *testing.T) {
	tmp := t.TempDir()
	packageRoot := filepath.Join(tmp, "package")
	writeValidPackageAt(t, packageRoot)
	store := NewStore(filepath.Join(tmp, "store"))
	result, err := store.Add(context.Background(), packageRoot, true, "dev")
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("user home unavailable")
	}
	if err := os.WriteFile(filepath.Join(result.StorePath, "leak.txt"), []byte(home), 0o644); err != nil {
		t.Fatalf("write extra store file failed: %v", err)
	}
	resolved, err := store.ResolveRunRef(context.Background(), "package://test/review@0.1.0", "dev")
	if err != nil {
		t.Fatalf("ResolveRunRef should not run full package validation: %v", err)
	}
	if resolved.AgentManifestPath == "" || resolved.Package == nil {
		t.Fatalf("ResolveRunRef returned incomplete ref: %+v", resolved)
	}
}

func TestResolveInstalledRunRefSupportsShortPackageRef(t *testing.T) {
	tmp := t.TempDir()
	packageRoot := filepath.Join(tmp, "package")
	writeValidPackageAt(t, packageRoot)
	store := NewStore(filepath.Join(tmp, "store"))
	result, err := store.Add(context.Background(), packageRoot, true, "dev")
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	for _, ref := range []string{"p:test/review@0.1.0", "p:test/review"} {
		resolved, err := store.ResolveRunRef(context.Background(), ref, "dev")
		if err != nil {
			t.Fatalf("ResolveRunRef(%q) failed: %v", ref, err)
		}
		if resolved.Package == nil {
			t.Fatalf("ResolveRunRef(%q) missing package provenance", ref)
		}
		if resolved.Package.ID != "test/review" || resolved.Package.Version != "0.1.0" {
			t.Fatalf("ResolveRunRef(%q) package = %s@%s, want test/review@0.1.0", ref, resolved.Package.ID, resolved.Package.Version)
		}
		if !strings.HasPrefix(resolved.AgentManifestPath, result.StorePath) {
			t.Fatalf("ResolveRunRef(%q) manifest %q is not inside store path %q", ref, resolved.AgentManifestPath, result.StorePath)
		}
	}
}

func runCommand(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, string(out))
	}
	return string(out)
}
