package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var agentNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)

func runInit(args []string) error {
	name, outputDir, err := parseInitArgs(args)
	if err != nil {
		return err
	}
	if !agentNameRe.MatchString(name) {
		return fmt.Errorf("invalid agent name %q", name)
	}
	if outputDir == "" {
		outputDir = "."
	}
	outputDir = filepath.Clean(outputDir)

	dirs := []string{
		filepath.Join(outputDir, "agents"),
		filepath.Join(outputDir, "prompts"),
		filepath.Join(outputDir, "workspace", name),
		filepath.Join(outputDir, "runs"),
		filepath.Join(outputDir, "skills", "web_research"),
		filepath.Join(outputDir, "skills", "report_writer"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	files := map[string]string{
		filepath.Join(outputDir, "agents", name+".agent.yaml"):                 manifestTemplate(name),
		filepath.Join(outputDir, "prompts", name+".md"):                        promptTemplate(name),
		filepath.Join(outputDir, "skills", "web_research", "skill.yaml"):       webResearchSkill(),
		filepath.Join(outputDir, "skills", "web_research", "instructions.md"):  "Collect concise, source-grounded notes. In V0, use available local tools only.\n",
		filepath.Join(outputDir, "skills", "report_writer", "skill.yaml"):      reportWriterSkill(),
		filepath.Join(outputDir, "skills", "report_writer", "instructions.md"): "Write structured Markdown with clear sections and direct conclusions.\n",
		filepath.Join(outputDir, "workspace", ".gitkeep"):                      "",
		filepath.Join(outputDir, "runs", ".gitkeep"):                           "",
	}
	for path, content := range files {
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}

	fmt.Printf("created agent %q\n", name)
	fmt.Printf("root: %s\n", displayPath(outputDir))
	fmt.Printf("manifest: %s\n", displayPath(filepath.Join(outputDir, "agents", name+".agent.yaml")))
	fmt.Printf("workspace: %s\n", displayPath(filepath.Join(outputDir, "workspace", name)))
	if outputDir != "." {
		fmt.Printf("next: cd %s\n", displayPath(outputDir))
	}
	return nil
}

func parseInitArgs(args []string) (string, string, error) {
	if len(args) == 0 {
		return "", "", fmt.Errorf("usage: jeju init <name> [--dir <dir>]")
	}
	name := ""
	outputDir := "."
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--dir" || arg == "-d":
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("%s requires a directory", arg)
			}
			outputDir = args[i+1]
			i++
		case strings.HasPrefix(arg, "--dir="):
			outputDir = strings.TrimPrefix(arg, "--dir=")
		case strings.HasPrefix(arg, "-"):
			return "", "", fmt.Errorf("unknown init option %q", arg)
		case name == "":
			name = arg
		case outputDir == ".":
			outputDir = arg
		default:
			return "", "", fmt.Errorf("usage: jeju init <name> [--dir <dir>]")
		}
	}
	if name == "" {
		return "", "", fmt.Errorf("usage: jeju init <name> [--dir <dir>]")
	}
	return name, outputDir, nil
}

func displayPath(path string) string {
	if path == "." {
		return "."
	}
	return filepath.ToSlash(path)
}

func manifestTemplate(name string) string {
	return fmt.Sprintf(`apiVersion: jeju/v1alpha1
kind: Agent

metadata:
  name: %[1]s
  description: "Local %[1]s agent"

models:
  default: primary
  providers:
    primary:
      provider: mock
      model: mock-react

instructions:
  system: ./prompts/%[1]s.md

runtime:
  mode: react
  limits:
    max_steps: 8
    max_duration_sec: 300
    max_tool_calls: 10
    max_consecutive_errors: 3
  interactive:
    enabled: true
    pause_on:
      - permission_required
      - agent_question

workspace:
  path: ./workspace/%[1]s

tools:
  - name: file_read
    type: builtin
    description: "Read files from workspace"
    permission: allow
    risk: [read]
    side_effect: false

  - name: file_write
    type: builtin
    description: "Write files to workspace"
    permission: ask
    risk: [write]
    side_effect: true

  - name: shell
    type: cli
    description: "Run shell commands in workspace"
    command: bash
    permission: ask
    risk: [execute, write]
    side_effect: true
    timeout_sec: 30

skills:
  mode: disclose
  paths:
    - ./skills/web_research
    - ./skills/report_writer
  disclosure:
    include: [name, description, when_to_use, capabilities, inputs, outputs, requires, risk]
  activation:
    policy: manual
    active:
      - web_research
      - report_writer
    max_active: 3
  loading:
    strategy: lazy

memory:
  enabled: false

sandbox:
  type: local
  workdir: ./workspace/%[1]s
  network: unrestricted

policy:
  default_permission: ask
  sandbox_required_for:
    - execute
  rules:
    - match:
        risk: read
      permission: allow
    - match:
        risk: write
      permission: ask
    - match:
        risk: execute
      permission: ask
    - match:
        risk: destructive
      permission: deny

trajectory:
  enabled: true
  format: jsonl
  store:
    type: file
    path: ./runs
  sinks:
    - type: console
      level: info
    - type: file
      path: ./runs

evaluate:
  enabled: true
  on_run_complete: true
  evaluators:
    - name: basic_trajectory
      type: rule
      rules:
        - final_answer_exists
        - no_model_error
        - max_steps_not_exceeded
        - max_tool_calls_not_exceeded
        - run_completed
  outputs:
    path: ./runs
    file: evaluation.json
`, name)
}

func promptTemplate(name string) string {
	return fmt.Sprintf("You are %s, a concise local research and writing agent.\n", name)
}

func webResearchSkill() string {
	return `apiVersion: jeju/v1alpha1
kind: Skill
metadata:
  name: web_research
  description: "Research a topic and produce grounded notes"
disclosure:
  when_to_use:
    - "Need source-grounded research notes"
  capabilities:
    - source_collection
    - note_synthesis
  inputs:
    topic:
      type: string
      required: true
  outputs:
    notes:
      type: markdown
  requires:
    tools:
      - file_write
  risk:
    level: read
    notes: "Reads available context"
instructions:
  file: ./instructions.md
`
}

func reportWriterSkill() string {
	return `apiVersion: jeju/v1alpha1
kind: Skill
metadata:
  name: report_writer
  description: "Write concise structured Markdown reports"
disclosure:
  when_to_use:
    - "Need a clear written report"
  capabilities:
    - markdown_report
    - structured_summary
  inputs:
    notes:
      type: markdown
      required: false
  outputs:
    report:
      type: markdown
  requires:
    tools:
      - file_write
  risk:
    level: write
    notes: "May write report files"
instructions:
  file: ./instructions.md
`
}
