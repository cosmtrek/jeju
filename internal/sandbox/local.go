package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Local struct {
	workdir string
}

func NewLocal(workdir string) (*Local, error) {
	abs, err := filepath.Abs(workdir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	return &Local{workdir: abs}, nil
}

func (s *Local) Type() string {
	return "local"
}

func (s *Local) Workdir() string {
	return s.workdir
}

func (s *Local) Exec(ctx context.Context, cmd ExecCommand) (ExecResult, error) {
	timeout := 30 * time.Second
	if cmd.TimeoutSec > 0 {
		timeout = time.Duration(cmd.TimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	shell := "bash"
	args := []string{"-lc", cmd.Command}
	if runtime.GOOS == "windows" {
		shell = "cmd"
		args = []string{"/C", cmd.Command}
	}
	proc := exec.CommandContext(ctx, shell, args...)
	proc.Dir = s.workdir
	proc.Env = os.Environ()
	for key, value := range cmd.Env {
		proc.Env = append(proc.Env, key+"="+value)
	}
	var stdout, stderr bytes.Buffer
	proc.Stdout = &stdout
	proc.Stderr = &stderr
	err := proc.Run()
	result := ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
		Duration: time.Since(start),
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else if ctx.Err() != nil {
			result.ExitCode = -1
			return result, ctx.Err()
		} else {
			result.ExitCode = -1
			return result, err
		}
	}
	return result, nil
}

func (s *Local) ReadFile(ctx context.Context, path string) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	full, err := s.safePath(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

func (s *Local) WriteFile(ctx context.Context, path string, data []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	full, err := s.safePath(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0o644)
}

func (s *Local) safePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("absolute paths are not allowed: %s", path)
	}
	cleaned := filepath.Clean(path)
	full := filepath.Join(s.workdir, cleaned)
	rel, err := filepath.Rel(s.workdir, full)
	if err != nil {
		return "", err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return "", fmt.Errorf("path escapes workspace: %s", path)
	}
	return full, nil
}
