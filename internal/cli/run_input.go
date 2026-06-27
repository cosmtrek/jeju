package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
)

const (
	runInputClipboard = "clipboard"
	runInputStdin     = "stdin"
	runInputStdinDash = "-"
)

type runInputReader func(context.Context, string) (string, error)

func resolveRunTask(ctx context.Context, args []string, from string, readSource runInputReader) (string, string, error) {
	if len(args) == 0 {
		return "", "", fmt.Errorf("run requires <agent-ref>")
	}
	agentRef := args[0]
	if strings.TrimSpace(from) == "" {
		if len(args) < 2 {
			return "", "", fmt.Errorf("run requires <agent-ref> and <task> unless --from is set")
		}
		if isMisplacedRunFlag(args[1]) {
			return "", "", fmt.Errorf("run flags must appear before <agent-ref>; use -- before the task if it starts with flag-like text")
		}
		return agentRef, strings.Join(args[1:], " "), nil
	}
	if len(args) > 1 {
		if isMisplacedRunFlag(args[1]) {
			return "", "", fmt.Errorf("run flags must appear before <agent-ref>; use -- before the task if it starts with flag-like text")
		}
	}
	task, err := readSource(ctx, from)
	if err != nil {
		return "", "", err
	}
	if len(args) > 1 {
		task = strings.Join(args[1:], " ") + "\n\n" + task
	}
	return agentRef, task, nil
}

func readRunInputSource(ctx context.Context, source string) (string, error) {
	source = strings.TrimSpace(source)
	switch source {
	case "":
		return "", fmt.Errorf("--from requires a source")
	case runInputClipboard:
		return readClipboard(ctx)
	case runInputStdin, runInputStdinDash:
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(data), nil
	default:
		path := strings.TrimPrefix(source, "file:")
		if path == "" {
			return "", fmt.Errorf("--from file path is empty")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read input file %q: %w", path, err)
		}
		return string(data), nil
	}
}

func readClipboard(ctx context.Context) (string, error) {
	for _, command := range clipboardCommands() {
		if _, err := exec.LookPath(command.name); err != nil {
			continue
		}
		out, err := exec.CommandContext(ctx, command.name, command.args...).Output()
		if err != nil {
			return "", fmt.Errorf("read clipboard with %s: %w", command.name, err)
		}
		return string(out), nil
	}
	return "", fmt.Errorf("clipboard input is not available on %s; install a clipboard command or use --from stdin / --from <path>", goruntime.GOOS)
}

type clipboardCommand struct {
	name string
	args []string
}

func clipboardCommands() []clipboardCommand {
	switch goruntime.GOOS {
	case "darwin":
		return []clipboardCommand{{name: "pbpaste"}}
	case "linux":
		return []clipboardCommand{
			{name: "wl-paste"},
			{name: "xclip", args: []string{"-selection", "clipboard", "-out"}},
			{name: "xsel", args: []string{"--clipboard", "--output"}},
		}
	default:
		return nil
	}
}
