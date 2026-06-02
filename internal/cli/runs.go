package cli

import (
	"fmt"

	"github.com/cosmtrek/jeju/internal/runs"
)

func runRuns(runsDir string) error {
	store := runs.NewStore(resolveRunsDir(runsDir))
	items, err := store.ListRuns()
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Println("no runs")
		return nil
	}
	for _, item := range items {
		fmt.Printf("%s  %-10s  %-10s  %s\n", item.RunID, item.Agent, item.Status, item.StartedAt.Format("2006-01-02 15:04:05"))
	}
	return nil
}
