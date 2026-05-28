# Jeju

![Jeju project architecture](docs/jeju-architecture.png)

> Manifest. Agent. Done.

Jeju is a config-defined agent runtime written in Go. Agents start with a manifest: a declarative spec that describes an agent's model, instructions, runtime loop, workspace, tools, skills, permissions, and evaluation.

Instead of wiring behavior into runtime code, Jeju loads, validates, and compiles a manifest into a runnable agent. The current runtime executes locally with workspace controls and file-backed runs, while the manifest-centered design leaves room for future cloud execution.

DeepSeek setup notes are in [docs/deepseek.md](docs/deepseek.md).

## Features

- **Agent Manifest as the source of truth**: define the agent's model, instructions, runtime limits, tools, skills, permissions, and evaluation in one declarative file.
- **Config-defined behavior**: change agent behavior by editing manifest, prompt, and skill files instead of modifying runtime code.
- **Compiled runtime boundary**: Jeju follows `config.LoadFile -> config.Validate -> compiler.Compile -> runtime.Run`, keeping YAML parsing out of the runtime.
- **Portable execution model**: Jeju runs locally today with filesystem-backed runs and workspaces, while the manifest/runtime boundary keeps the design open for future cloud execution.
- **Auditable trajectories**: every run writes JSONL events and keeps large payloads under run artifacts.
- **Permission-aware tools**: tool calls pass through `policy.Gate`, with capability metadata and approval profiles for sensitive operations.
- **Workspace-constrained file and shell access**: builtin file tools stay inside the configured workspace, and shell commands run there with timeouts.
- **Skill disclosure model**: skills are disclosed first and loaded manually, so the runtime does not inject every skill asset by default.
- **Mock and real model modes**: the scaffolded mock model supports fast local tests without credentials; OpenAI-compatible providers support real API-backed runs.
- **Built-in inspection flow**: `runs` and `inspect` make completed runs easy to list and debug.
- **Rule-based evaluation**: completed runs can be checked for expected lifecycle, model, tool, permission, and output conditions.

## Design Philosophy

Jeju treats an agent as a small, explicit runtime unit instead of an opaque application. The core idea is that the manifest should be the source of truth for agent behavior, and the runtime should execute the compiled result through a narrow path:

```text
Manifest -> Validate -> Compile -> Run -> Gate -> Trace -> Evaluate -> Inspect
```

The runtime does not read YAML directly. Configuration is loaded, validated, and compiled into a `CompiledAgent` before execution. This keeps runtime behavior grounded in the manifest, loaded instructions, tools, skills, permissions, and evaluator configuration rather than hardcoded branches.

Local execution is the first runtime target. Runs are stored on disk, file and shell tools are constrained to a configured workspace, and every run leaves behind enough metadata and trajectory data to inspect what happened.

## Agent Manifest

An Agent Manifest is the source of truth for a Jeju agent. It defines the agent's identity, model, instructions, runtime loop, workspace, tools, skills, permission profile, and optional evaluation rules.

See [docs/agent-manifest.md](docs/agent-manifest.md) for the full field reference, defaults, supported values, and validation rules.

At a high level, a manifest looks like this:

```yaml
apiVersion: jeju/v1alpha1
kind: Agent

metadata:
  name: research
  description: "Local research assistant"

models:
  providers:
    primary:
      type: mock
      model: mock-react

instructions:
  system: ../prompts/research.md

runtime:
  model: primary
  loop:
    type: react
  limits:
    maxSteps: 8
    maxDurationSec: 300

workspace:
  path: ../workspace/research

tools:
  - read
  - write
  - edit
  - search
  - shell

  - name: search_api
    uses: http
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

skills:
  dirs:
    - ../skills
  active:
    - web-research

permissions:
  access: workspace
  approval: onRequest
```

The important sections are:

- `models`: registers model providers. The default scaffold uses `mock`; `openaiCompatible` can point at real Chat Completions-compatible endpoints.
- `instructions`: loads the system prompt from a file so behavior can be reviewed and versioned.
- `runtime`: selects the model, loop type, and execution limits.
- `workspace`: defines where file and shell work is allowed.
- `tools`: declares built-in and custom tools plus capability metadata for permission decisions.
- `skills`: points at skill roots and manually activates the skills that should load instructions.
- `permissions`: gates tool execution before sensitive operations happen.
- Run output records model calls, tool calls, permission decisions, skill events, artifacts, evaluation, and lifecycle events under `./runs`.
- `evaluate`: optionally runs rule-based checks after completion.

## Quick Start

The generated agent uses the `mock` model by default, so the full lifecycle can run without API credentials. The commands below create an isolated Jeju working directory under `.jeju-dev/`, keeping generated agents, workspaces, runs, and skill fixtures separate from the source tree.

```bash
# 1. Check the CLI entrypoint.
go run ./cmd/jeju --help

# 2. Scaffold a new agent into an isolated working directory.
go run ./cmd/jeju init research --dir .jeju-dev

# 3. Enter the generated Jeju working directory.
cd .jeju-dev

# 4. Validate the generated Agent Manifest.
go run ../cmd/jeju validate agents/research.agent.yaml

# 5. Run the agent. The leading "y" approves the scaffolded file write.
printf 'y\n' | go run ../cmd/jeju run agents/research.agent.yaml "写一份关于 AgentOps 的简短分析，并保存到 notes.md"

# 6. List recorded runs.
go run ../cmd/jeju runs

# 7. Inspect a completed run and its trajectory/artifacts.
go run ../cmd/jeju inspect <run_id>
```

Run `validate`, `run`, `runs`, and `inspect` from the generated working directory. To use a real model, change the manifest provider from `mock` to `openaiCompatible` and point it at a Chat Completions-compatible endpoint.

## Tests

```bash
go test ./...
```

The full-path fixture agent is under `tests/fixtures/agent/`. The test copies it into a temporary directory before running, so fixture sources stay clean.

To run the fixture agent locally end to end:

```bash
make test-agent
```

Or run the script directly with an optional task:

```bash
./scripts/run-agent.sh mock "write a brief AgentOps note and save it to notes.md"
```

The script writes local output under `.jeju-dev/<provider>-agent-run/`.

To test with DeepSeek V4 Flash:

```bash
export DEEPSEEK_API_KEY=sk-...
make test-agent PROVIDER=deepseek
```

To test with MiMo V2.5 Pro:

```bash
export MIMO_API_KEY=sk-...
make test-agent PROVIDER=mimo
```

Set `JEJU_MIMO_BASE_URL` if you need to override the MiMo endpoint.
