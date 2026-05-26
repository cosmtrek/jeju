package cli

import (
	"context"
	"fmt"
)

func Execute(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printHelp()
		return nil
	}

	switch args[0] {
	case "-h", "--help", "help":
		printHelp()
		return nil
	case "init":
		return runInit(args[1:])
	case "validate":
		return runValidate(args[1:])
	case "run":
		return runAgent(ctx, args[1:])
	case "inspect":
		return runInspect(args[1:])
	case "runs":
		return runRuns(args[1:])
	case "view":
		return runView(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printHelp() {
	fmt.Println(`Jeju - config-defined local agent runtime

Usage:
  jeju init <name> [--dir <dir>]
  jeju validate <agent.yaml>
  jeju run <agent.yaml> "<task>"
  jeju inspect <run_id>
  jeju view <run_id> [--out <html>]
  jeju runs

Examples:
  jeju init research --dir .jeju-dev
  cd .jeju-dev
  jeju validate agents/research.agent.yaml
  jeju run agents/research.agent.yaml "写一份关于 AgentOps 的简短分析，并保存到 notes.md"
  jeju view 20260526-120000-research`)
}
