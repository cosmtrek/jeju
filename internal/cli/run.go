package cli

import (
	"context"
	"fmt"
	"strings"

	"jeju/internal/compiler"
	"jeju/internal/runtime"
)

func runAgent(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: jeju run <agent.yaml> \"<task>\"")
	}
	manifestPath := args[0]
	task := strings.Join(args[1:], " ")

	agent, err := compiler.Compile(manifestPath)
	if err != nil {
		return err
	}
	rt := runtime.New()
	result, err := rt.Run(ctx, agent, task)
	if err != nil {
		return err
	}
	fmt.Printf("run_id: %s\n", result.RunID)
	return nil
}
