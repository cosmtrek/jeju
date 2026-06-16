package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cosmtrek/jeju/internal/agentpkg"
	"github.com/cosmtrek/jeju/internal/runs"
)

const runsDirEnv = "JEJU_RUNS_DIR"
const localRunsDir = "./runs"

func resolveRunWriteDir(value, agentRef string) (string, error) {
	if resolved, ok := explicitRunsDir(value); ok {
		return resolved, nil
	}
	if agentpkg.IsPackageBackedRef(agentRef) {
		return defaultGlobalRunsDir()
	}
	return filepath.Clean(localRunsDir), nil
}

func explicitRunsDir(value string) (string, bool) {
	if value == "" {
		value = os.Getenv(runsDirEnv)
	}
	if value == "" {
		return "", false
	}
	return filepath.Clean(value), true
}

func defaultGlobalRunsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".jeju", "runs"), nil
}

type runStoreCandidate struct {
	Label string
	Path  string
}

func readRunStoreCandidates(value string) ([]runStoreCandidate, error) {
	if resolved, ok := explicitRunsDir(value); ok {
		return []runStoreCandidate{{Label: "configured", Path: resolved}}, nil
	}
	global, err := defaultGlobalRunsDir()
	if err != nil {
		return nil, err
	}
	return dedupeRunStoreCandidates([]runStoreCandidate{
		{Label: "local", Path: filepath.Clean(localRunsDir)},
		{Label: "global", Path: global},
	}), nil
}

func dedupeRunStoreCandidates(candidates []runStoreCandidate) []runStoreCandidate {
	seen := map[string]bool{}
	out := []runStoreCandidate{}
	for _, candidate := range candidates {
		key := candidate.Path
		if abs, err := filepath.Abs(candidate.Path); err == nil {
			key = abs
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, candidate)
	}
	return out
}

type loadedRun struct {
	Store      *runs.Store
	RunDir     *runs.RunDir
	StoreLabel string
	StorePath  string
}

func loadRunFromCandidateStores(runID, runsDir string) (loadedRun, error) {
	candidates, err := readRunStoreCandidates(runsDir)
	if err != nil {
		return loadedRun{}, err
	}
	matches := []loadedRun{}
	for _, candidate := range candidates {
		store := runs.NewStore(candidate.Path)
		runDir, err := store.LoadRun(runID)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return loadedRun{}, err
		}
		matches = append(matches, loadedRun{
			Store:      store,
			RunDir:     runDir,
			StoreLabel: candidate.Label,
			StorePath:  candidate.Path,
		})
	}
	if len(matches) == 0 {
		paths := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			paths = append(paths, candidate.Path)
		}
		return loadedRun{}, fmt.Errorf("run %q not found in run stores: %s", runID, strings.Join(paths, ", "))
	}
	if len(matches) > 1 {
		paths := make([]string, 0, len(matches))
		for _, match := range matches {
			paths = append(paths, match.StorePath)
		}
		return loadedRun{}, fmt.Errorf("run %q exists in multiple run stores (%s); use --runs-dir to choose one", runID, strings.Join(paths, ", "))
	}
	return matches[0], nil
}
