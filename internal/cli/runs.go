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

func runRuns(runsDir string) error {
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
			items = append(items, listedRun{
				Metadata:   item,
				StoreLabel: candidate.Label,
			})
		}
	}
	if len(items) == 0 {
		fmt.Println("no runs")
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
