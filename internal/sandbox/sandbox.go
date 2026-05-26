package sandbox

import (
	"context"
	"time"
)

type Sandbox interface {
	Type() string
	Workdir() string
	Exec(ctx context.Context, cmd ExecCommand) (ExecResult, error)
	ReadFile(ctx context.Context, path string) ([]byte, error)
	WriteFile(ctx context.Context, path string, data []byte) error
}

type ExecCommand struct {
	Command    string
	TimeoutSec int
	Env        map[string]string
}

type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}
