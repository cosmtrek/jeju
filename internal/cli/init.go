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
		filepath.Join(outputDir, "skills", "web-research"),
		filepath.Join(outputDir, "skills", "report-writer"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	files := map[string]string{
		filepath.Join(outputDir, "agents", name+".agent.yaml"):          manifestTemplate(name),
		filepath.Join(outputDir, "prompts", name+".md"):                 promptTemplate(name),
		filepath.Join(outputDir, "skills", "web-research", "SKILL.md"):  webResearchSkill(),
		filepath.Join(outputDir, "skills", "report-writer", "SKILL.md"): reportWriterSkill(),
		filepath.Join(outputDir, "workspace", ".gitkeep"):               "",
		filepath.Join(outputDir, "runs", ".gitkeep"):                    "",
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
  providers:
    primary:
      type: mock
      model: mock-react
      contextWindow: 128000

instructions:
  system: ../prompts/%[1]s.md

runtime:
  model: primary
  loop:
    type: react
  compressionThreshold: 0.8
  limits:
    maxSteps: 8
    maxDurationSec: 300
    maxToolCalls: 10
    maxConsecutiveErrors: 3

workspace:
  path: ../workspace/%[1]s

tools:
  - read
  - write
  - edit
  - search
  - shell

  - name: search_api
    uses: http
    description: "Search the web with Exa"
    capabilities: [networkRead]
    http:
      method: POST
      url: https://api.exa.ai/search
      headers:
        content-type: application/json
        x-api-key: ${EXA_API_KEY}
      body:
        json:
          query: "{{query}}"
          numResults: 10
          type: auto
          contents:
            highlights: true
      timeoutSec: 30
    input:
      schema:
        type: object
        required: [query]
        properties:
          query:
            type: string
            description: "Search query"

skills:
  dirs:
    - ../skills
  active:
    - web-research
    - report-writer

permissions:
  access: workspace
  approval: onRequest

evaluate:
  enabled: true
  evaluators:
    - name: basic_trajectory
      uses: rules
      rules:
        - finalAnswerExists
        - noModelError
        - maxStepsNotExceeded
        - maxToolCallsNotExceeded
        - runCompleted
`, name)
}

func promptTemplate(name string) string {
	return fmt.Sprintf("You are %s, a concise local research and writing agent.\n", name)
}

func webResearchSkill() string {
	return `---
name: web-research
description: Research a topic and produce grounded notes. Use when source-grounded research notes are needed.
metadata:
  jeju.capabilities: source_collection,note_synthesis
allowed-tools: search_api write
---

# Web Research

Use search_api for web search. It calls Exa with EXA_API_KEY from the environment and returns highlighted results. Collect concise, source-grounded notes and write results when the task asks for saved output.
`
}

func reportWriterSkill() string {
	return `---
name: report-writer
description: Write concise structured Markdown reports. Use when the task needs a clear written report.
metadata:
  jeju.capabilities: markdown_report,structured_summary
allowed-tools: write
---

# Report Writer

Write structured Markdown with clear sections and direct conclusions.
`
}
