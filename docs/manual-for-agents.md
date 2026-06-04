# Manual For Agents

This document is for AI agents that need to author Jeju agents. A Jeju agent is
a narrow, bounded execution unit with a manifest, prompt, tools, permissions,
runtime limits, run evidence, and optional evaluation.

Jeju source repository: https://github.com/cosmtrek/jeju

## When To Create A Jeju Agent

Create a Jeju agent when a repeated workflow benefits from model reasoning plus
explicit tools, permissions, run evidence, or evaluation.

Prefer another artifact when:

- A deterministic script is enough.
- The value is only prompt reuse inside the authoring agent.
- The task needs broad orchestration, distributed workers, remote sandboxes, or
  long-term memory.

Good Jeju agent candidates are narrow specialists: code-review triage, docs
update planning, benchmark failure classification, trajectory inspection,
prompt regression review, release-note drafting, and local repo hygiene checks.

## Design Contract

Before writing files, identify:

- Purpose: one sentence describing the repeated workflow.
- Input contract: what the user or parent agent passes to `jeju run`.
- Output contract: final answer shape, preferably structured JSON or concise
  markdown.
- Workspace binding: which local project directory is inspected or changed.
- Tool needs: read/search/write/edit/shell/http/command tools, with why each is
  needed.
- Permission profile: default to `readOnly`; only enable writes or shell when
  required.
- Model tier: cheap, medium, or strong based on task complexity and risk.
- Runtime limits: max steps, max duration, max tool calls, max consecutive
  errors.
- Evaluation: at least one smoke case or command/rule evaluator when practical.
- Non-goals: what the agent must not attempt.

## Minimal Bundle Shape

Use the smallest bundle that can be validated and run:

```text
agents/<name>.agent.yaml
prompts/<name>.md
workspace/<name>/.gitkeep
eval/<optional-evaluator>.py
README.md
```

Add a runtime skill, schema, or custom tool script only when it removes real
complexity.

## Manifest Reference

Start from a minimal `apiVersion: jeju/v1alpha1`, `kind: Agent` manifest. Keep
behavior in manifest fields, prompt files, tools, skills, permissions, and
evaluators.

Required top-level fields:

- `apiVersion`: must be `jeju/v1alpha1`.
- `kind`: must be `Agent`.
- `metadata.name`: stable agent name.
- `models.providers`: model provider registry.
- `instructions.system`: path to the system prompt file.
- `runtime`: selected model, loop type, and limits.
- `workspace.path`: local workspace boundary.

Optional top-level fields:

- `tools`: built-in, command, or HTTP tools.
- `skills`: skill roots and manually active skill names.
- `permissions`: access and approval policy.
- `evaluate`: post-run evaluators.

Minimal read-only specialist:

```yaml
apiVersion: jeju/v1alpha1
kind: Agent

metadata:
  name: repo-inspector
  description: "Inspect a local repository and produce a structured summary"

models:
  providers:
    primary:
      type: openaiCompatible
      preset: deepseek
      model: deepseek-v4-flash
      envKey: DEEPSEEK_API_KEY
      temperature: 0.1
      thinking:
        type: disabled
      maxOutputTokens: 2048
      contextWindow: 128000
      timeoutSec: 60

instructions:
  system: ../prompts/repo-inspector.md

runtime:
  model: primary
  loop:
    type: react
  compressionThreshold: 0.8
  limits:
    maxSteps: 16
    maxDurationSec: 240
    maxToolCalls: 30
    maxConsecutiveErrors: 2

workspace:
  path: ../workspace/repo-inspector

tools:
  - read
  - search

permissions:
  access: readOnly
  approval: never

evaluate:
  enabled: true
  evaluators:
    - name: basic
      uses: rules
      rules:
        - finalAnswerExists
        - runCompleted
```

## Tools

Built-in tools can be listed as strings:

```yaml
tools:
  - read
  - write
  - edit
  - search
  - shell
```

Custom command tool:

```yaml
tools:
  - name: git_status
    uses: command
    description: "Show current Git working tree status."
    capabilities: [workspaceRead]
    command:
      run: git
      args: ["status", "--short"]
      timeoutSec: 20
```

Command tool with input schema:

```yaml
tools:
  - name: git_diff
    uses: command
    description: "Show a Git diff, optionally restricted to one path."
    capabilities: [workspaceRead]
    command:
      run: git
      args: ["diff", "--", "{{path}}"]
      timeoutSec: 30
    input:
      schema:
        type: object
        properties:
          path:
            type: string
            default: "."
            description: "Repository-relative path to diff."
```

HTTP tool:

```yaml
tools:
  - name: search_api
    uses: http
    description: "Search an external API."
    capabilities: [networkRead]
    http:
      method: POST
      url: https://api.example.com/search
      headers:
        content-type: application/json
        x-api-key: ${SEARCH_API_KEY}
      body:
        json:
          query: "{{query}}"
      timeoutSec: 30
    input:
      schema:
        type: object
        properties:
          query:
            type: string
        required: [query]
```

Declare capabilities accurately: `workspaceRead`, `workspaceWrite`, `command`,
`networkRead`, or `networkWrite`.

## Skills

Skill activation:

```yaml
skills:
  dirs:
    - ../skills
  active:
    - repo-review
```

Each active skill is a directory under a listed `dirs` root and must contain
`SKILL.md`. Only active skill instructions are loaded.

## Evaluators

Common evaluator forms:

```yaml
evaluate:
  enabled: true
  evaluators:
    - name: basic
      uses: rules
      rules:
        - finalAnswerExists
        - runCompleted

    - name: output_shape
      uses: command
      command:
        run: python3
        args: [../eval/check_output.py]
        timeoutSec: 30

    - name: quality
      uses: llm
      model: primary
      prompt: ../eval/quality_judge.md
      threshold: 0.7
```

Every generated agent should have at least a smoke path. For stronger agents,
include a small dataset and consider `jeju evolve` later. Keep evolution
targets narrow: prompts, selected skills, tool descriptions, or evaluator
guidance. Do not let evolve mutate credentials, workspace paths, tool command
bodies, HTTP endpoints, permissions, or environment variables.

## Prompt Guidance

The system prompt should make the specialist boundary obvious:

- State the exact workflow and output format.
- Tell the agent how to inspect before acting.
- Tell it to report residual risk when it cannot inspect enough.
- For write agents, require a brief plan before edits and final summary after
  edits.
- For review agents, prioritize actionable bugs, regressions, security risks,
  and missing tests.
- Avoid broad "be a helpful coding agent" instructions.

## Install Jeju If Needed

Before validating or running a generated agent, check whether Jeju is available:

```bash
jeju version
```

If the command is missing, use the README binary install command on macOS or
Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/cosmtrek/jeju/master/scripts/install.sh | sh
jeju version
```

On Windows, do not run the shell installer. Prefer the matching
`jeju_windows_<arch>.zip` release asset if available, or use the README source
install path when Go is already installed:

```bash
go install github.com/cosmtrek/jeju/cmd/jeju@latest
jeju version
```

Do not install Go automatically. Ask the user before taking the source-install
path when Go is missing.

## Validation

From the Jeju source checkout, write local run artifacts under `.jeju-dev/`:

```bash
jeju validate <bundle>/agents/<name>.agent.yaml
jeju run --runs-dir .jeju-dev/runs/<name> <bundle>/agents/<name>.agent.yaml "<sample input>"
jeju inspect --runs-dir .jeju-dev/runs/<name> <run_id>
```

Run source tests only when changing Jeju code:

```bash
go test ./...
go vet ./...
```

Use real provider smoke runs only when the required API keys are intentionally
set.

## Source Lookup Advice

If repository access is available, inspect source and docs before inventing
fields or behavior. Prefer conceptual search terms over hardcoded paths because
the repository can be reorganized:

```bash
rg -n "AgentManifest|ToolConfig|finalAnswerExists|workspace.path|permissions.access|compressionThreshold"
rg -n "config.LoadFile|config.Validate|compiler.Compile|runtime.Run"
rg -n "uses: command|uses: http|allowed-tools|active skills|evaluate"
```

Look for current docs, schema structs, defaults, validators, compiler behavior,
runtime loop behavior, policy checks, tool implementations, skill loading,
evaluator behavior, evolve behavior, and concrete examples.
