package cli

import (
	"os"
	"path/filepath"
)

const runsDirEnv = "JEJU_RUNS_DIR"

func resolveRunsDir(value string) string {
	if value == "" {
		value = os.Getenv(runsDirEnv)
	}
	if value == "" {
		value = "./runs"
	}
	return filepath.Clean(value)
}
