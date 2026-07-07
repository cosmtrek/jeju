package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cosmtrek/jeju/internal/runs"
)

type listedRun struct {
	runs.Metadata
	StoreLabel string
}

func runListRuns(runsDir, packageSelector string) error {
	candidates, err := readRunStoreCandidates(runsDir)
	if err != nil {
		return err
	}
	items := []listedRun{}
	for _, candidate := range candidates {
		store := runs.NewStore(candidate.Path)
		runItems, err := store.ListRuns()
		if err != nil {
			return err
		}
		for _, item := range runItems {
			if packageSelector != "" && !matchesPackageSelector(item, packageSelector) {
				continue
			}
			items = append(items, listedRun{
				Metadata:   item,
				StoreLabel: candidate.Label,
			})
		}
	}
	if len(items) == 0 {
		if packageSelector != "" {
			fmt.Printf("no runs for package %s\n", packageSelector)
		} else {
			fmt.Println("no runs")
		}
		return nil
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].StartedAt.After(items[j].StartedAt)
	})
	if duplicates := duplicateRunIDs(items); len(duplicates) > 0 {
		fmt.Printf("warning: run IDs in multiple run stores: %s; use --runs-dir to choose one\n", strings.Join(duplicates, ", "))
	}
	fmt.Printf("%-28s  %-12s  %-30s  %-10s  %-10s  %s\n", "RUN_ID", "AGENT", "PACKAGE", "STATUS", "STORE", "STARTED")
	for _, item := range items {
		fmt.Printf("%-28s  %-12s  %-30s  %-10s  %-10s  %s\n",
			item.RunID,
			item.Agent,
			packageLabel(item.PackageID, item.PackageVersion),
			item.Status,
			item.StoreLabel,
			item.StartedAt.Format("2006-01-02 15:04:05"),
		)
	}
	return nil
}

func matchesPackageSelector(item runs.Metadata, selector string) bool {
	id, version := splitPackageSelector(selector)
	if item.PackageID != id {
		return false
	}
	if version == "" {
		return true
	}
	return item.PackageVersion == version
}

func splitPackageSelector(selector string) (string, string) {
	selector = normalizePackageSelector(selector)
	id, version, ok := strings.Cut(selector, "@")
	if !ok {
		return id, ""
	}
	return id, version
}

func isPackageSelector(selector string) bool {
	selector = normalizePackageSelector(selector)
	id, _, _ := strings.Cut(selector, "@")
	return strings.Contains(id, "/")
}

func normalizePackageSelector(selector string) string {
	for _, prefix := range []string{"package://", "p:", "jeju:"} {
		selector = strings.TrimPrefix(selector, prefix)
	}
	return selector
}

func duplicateRunIDs(items []listedRun) []string {
	counts := map[string]int{}
	for _, item := range items {
		counts[item.RunID]++
	}
	duplicates := []string{}
	for runID, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, runID)
		}
	}
	sort.Strings(duplicates)
	return duplicates
}

func packageLabel(id, version string) string {
	if id == "" {
		return "-"
	}
	if version == "" {
		return id
	}
	return id + "@" + version
}
