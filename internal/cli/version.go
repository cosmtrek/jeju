package cli

import (
	"fmt"
	"runtime/debug"
)

var (
	version = "dev"
	commit  = "none"
	branch  = "unknown"
	date    = "unknown"
)

func runVersion() error {
	info := buildVersionInfo()
	fmt.Printf("jeju %s\n", version)
	fmt.Printf("commit: %s\n", info.commit)
	fmt.Printf("branch: %s\n", info.branch)
	fmt.Printf("built: %s\n", info.date)
	return nil
}

type versionInfo struct {
	commit string
	branch string
	date   string
}

func buildVersionInfo() versionInfo {
	info := versionInfo{
		commit: commit,
		branch: branch,
		date:   date,
	}
	if build, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range build.Settings {
			switch setting.Key {
			case "vcs.revision":
				if info.commit == "none" && setting.Value != "" {
					info.commit = shortCommit(setting.Value)
				}
			case "vcs.time":
				if info.date == "unknown" && setting.Value != "" {
					info.date = setting.Value
				}
			case "vcs.modified":
				if setting.Value == "true" && info.commit != "none" && info.commit != "" {
					info.commit += "-dirty"
				}
			}
		}
	}
	return info
}

func shortCommit(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}
