package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"jeju/internal/sandbox"
	"jeju/internal/tools"
)

type Shell struct {
	spec tools.Spec
	box  sandbox.Sandbox
	env  map[string]string
}

func NewShell(spec tools.Spec, box sandbox.Sandbox, env map[string]string) *Shell {
	spec.Name = "shell"
	return &Shell{spec: spec, box: box, env: env}
}

func (t *Shell) Name() string {
	return t.spec.Name
}

func (t *Shell) Spec() tools.Spec {
	return t.spec
}

func (t *Shell) Run(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var req struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return tools.Result{}, err
	}
	if err := validateWorkspaceCommand(req.Command); err != nil {
		return tools.Result{}, err
	}
	result, err := t.box.Exec(ctx, sandbox.ExecCommand{
		Command:    req.Command,
		TimeoutSec: t.spec.TimeoutSec,
		Env:        t.env,
	})
	if err != nil {
		return tools.Result{}, err
	}
	out, err := json.Marshal(map[string]any{
		"stdout":      result.Stdout,
		"stderr":      result.Stderr,
		"exit_code":   result.ExitCode,
		"duration_ms": result.Duration.Milliseconds(),
	})
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Output: string(out)}, nil
}

func validateWorkspaceCommand(command string) error {
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("command is required")
	}
	if strings.Contains(command, "$(") || strings.Contains(command, "`") {
		return fmt.Errorf("shell command substitutions are not allowed in workspace sandbox")
	}
	words := splitShellWords(command)
	for _, word := range words {
		if err := validateWorkspaceWord(word); err != nil {
			return err
		}
	}
	return nil
}

func validateWorkspaceWord(word string) error {
	word = strings.TrimSpace(word)
	if word == "" {
		return nil
	}
	word = trimRedirectPrefix(word)
	if word == "" {
		return nil
	}
	if strings.HasPrefix(word, "~") {
		return fmt.Errorf("home-directory paths are not allowed in workspace sandbox: %s", word)
	}
	if strings.HasPrefix(word, "/") && looksLikeAbsolutePath(word) {
		return fmt.Errorf("absolute paths are not allowed in workspace sandbox: %s", word)
	}
	for _, part := range strings.Split(word, "=") {
		if pathEscapesWorkspace(part) {
			return fmt.Errorf("parent directory traversal is not allowed in workspace sandbox: %s", word)
		}
	}
	return nil
}

func trimRedirectPrefix(word string) string {
	word = strings.TrimLeft(word, "0123456789")
	word = strings.TrimLeft(word, "<>&")
	return word
}

func looksLikeAbsolutePath(word string) bool {
	if strings.HasPrefix(word, "/O=") || strings.HasPrefix(word, "/CN=") ||
		strings.Contains(word, "/O=") || strings.Contains(word, "/CN=") {
		return false
	}
	return true
}

func pathEscapesWorkspace(value string) bool {
	if value == "" {
		return false
	}
	cleaned := filepath.Clean(value)
	return cleaned == ".." || strings.HasPrefix(cleaned, "../")
}

func splitShellWords(command string) []string {
	words := []string{}
	var current strings.Builder
	var quote rune
	escaped := false
	for _, ch := range command {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			current.WriteRune(ch)
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
				continue
			}
			current.WriteRune(ch)
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case ' ', '\t', '\n', ';', '|':
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	return words
}
