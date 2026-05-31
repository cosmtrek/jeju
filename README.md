# Jeju

![Jeju project architecture](docs/jeju-architecture.png)

> Manifest. Agent. Done.

Jeju is a config-defined agent runtime written in Go. Agents start with a manifest: a declarative spec that describes an agent's model, instructions, runtime loop, workspace, tools, skills, permissions, and evaluation.

Instead of wiring behavior into runtime code, Jeju loads, validates, and compiles a manifest into a runnable agent. The current runtime executes locally with workspace controls and file-backed runs, while the manifest-centered design leaves room for future cloud execution.

Primary docs:

- [Agent manifest reference](docs/agent-manifest.md)
- [Agent evolution manifest reference](docs/agent-evolution-manifest.md)
- [Self-evolution design](docs/self-evolution.md)
- [DeepSeek setup notes](docs/deepseek.md)

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
- **Config-space self-evolution**: `jeju evolve` runs a target agent over train and selection tasks, asks an evolver agent for structured config patches, validates candidates, and writes a better audited config when the objective improves.
- **Task-level effective evaluation**: evolution tasks can provide `expected`, `eval`, and `metadata` so experiment-specific judging can override or complement the target agent's default evaluators.
- **Holdout effect validation fixtures**: the evolve test scripts include both a mechanics fixture and a real-effect triage fixture that can run against MiMo.

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

- `models`: registers model providers. The default scaffold uses `mock`; `openaiCompatible` can point at real Chat Completions-compatible endpoints and must have a known `contextWindow` for request budgeting.
- `instructions`: loads the system prompt from a file so behavior can be reviewed and versioned.
- `runtime`: selects the model, loop type, and execution limits.
- `workspace`: defines where file and shell work is allowed.
- `tools`: declares built-in and custom tools plus capability metadata for permission decisions.
- `skills`: points at skill roots and manually activates the skills that should load instructions.
- `permissions`: gates tool execution before sensitive operations happen.
- Run output records model calls, tool calls, permission decisions, skill events, artifacts, evaluation, and lifecycle events under `./runs`.
- `evaluate`: optionally runs rule-based checks after completion.

## Agent Self-Evolution

Jeju can optimize a config-defined agent with `jeju evolve`. An evolution experiment is a separate `kind: EvolutionExperiment` manifest that points at a target agent, datasets, an objective metric, edit boundaries, an evolver agent, search limits, and an output directory.

The current implementation is an offline optimization loop:

```text
baseline agent
  -> train and selection runs
  -> effective evaluation
  -> feedback digest
  -> evolver proposal
  -> exact-replacement patch
  -> validate and compile candidate
  -> train filter
  -> selection acceptance
  -> best candidate bundle and report
```

`jeju evolve` never edits the source agent in place. It copies the target agent and referenced files into an isolated experiment directory, applies patches only to candidate copies, and writes the accepted best candidate under `best/`.

A minimal experiment looks like this:

```yaml
apiVersion: jeju/v1alpha1
kind: EvolutionExperiment

metadata:
  name: evolve-triage

target:
  agent: ../agents/triage.agent.yaml
  editable:
    - instructions.system
  forbidden:
    - permissions.access
    - permissions.approval
    - workspace.path
    - tools[].command.run
    - tools[].http.url
    - models.providers.*.envKey
    - models.providers.*.baseUrl

data:
  format: jeju.task.v1
  train: ../datasets/train.jsonl
  selection: ../datasets/selection.jsonl
  render:
    template: ../prompts/task_input.md.tmpl

objective:
  metric: evaluation.score
  direction: maximize
  minDelta: 0.1
  guards:
    - "evaluation.passed_rate >= baseline.evaluation.passed_rate"
    - "run.modelErrors <= baseline.run.modelErrors"
  guidance:
    - "Improve general behavior rather than memorizing examples."

evolver:
  agent: ../agents/evolver.agent.yaml
  proposals: 2

search:
  iterations: 3
  trialsPerTask: 1
  parallelism: 2

output:
  dir: ../.jeju-dev/evolve-triage
```

Dataset files are JSONL. Each row is a task:

```json
{
  "id": "triage-001",
  "input": {
    "ticket": "OAuth callback validation fails after a redirect-domain config release.",
    "customer_tier": "enterprise"
  },
  "expected": {
    "mustInclude": ["P1", "auth"]
  },
  "eval": {
    "rubric": "Return strict JSON and choose the right route."
  },
  "metadata": {
    "category": "auth"
  },
  "weight": 1
}
```

The `input` is rendered into the runtime task string with the optional Go template in `data.render.template`. `expected`, `eval`, and `metadata` are passed to effective evaluation. If a task provides expected or eval data, that task-level signal takes precedence over the target agent's default evaluator assumptions.

The evolver is also a normal Jeju agent. It receives a deterministic feedback digest and must return proposal JSON:

```json
{
  "hypothesis": "The prompt does not enforce the required output format.",
  "changes": [
    {
      "target": "instructions.system",
      "find": "You are a support assistant.\n",
      "replace": "You are a support assistant. Return only strict JSON.\n"
    }
  ]
}
```

Patch safety is intentionally narrow. The patch target must be listed in `target.editable`, must not match `target.forbidden`, and `find` must match exactly once inside the candidate bundle. `instructions.system` patches update the referenced prompt file. After patching, Jeju validates that no forbidden or uneditable manifest leaf field changed, then compiles the candidate before running it.

Common commands:

```bash
# Validate the baseline bundle and compile without model calls.
jeju evolve --dry-run experiments/evolve.yaml

# Measure baseline train/selection metrics without calling the evolver.
jeju evolve --baseline-only experiments/evolve.yaml

# Run the full evolution loop.
jeju evolve experiments/evolve.yaml

# Limit real-provider cost while testing.
jeju evolve --max-iterations 2 --out .jeju-dev/evolve/triage experiments/evolve.yaml
```

Evolution output is written under `output.dir/<experiment_id>/`:

- `experiment.snapshot.json`: resolved experiment config.
- `events.jsonl`: controller lifecycle events.
- `baseline/results.json`: baseline train and selection results.
- `iterations/<n>/feedback_digest.json`: digest passed to the evolver.
- `iterations/<n>/proposals.json`: parsed structured proposals.
- `iterations/<n>/candidate-*/patch.json`: applied candidate patch.
- `leaderboard.json`: all candidates, metrics, and rejection reasons.
- `best/`: materialized best candidate bundle.
- `report.md`: human-readable result summary.

See [docs/agent-evolution-manifest.md](docs/agent-evolution-manifest.md) for the full schema and [docs/self-evolution.md](docs/self-evolution.md) for the design details.

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

Run `validate`, `run`, `runs`, and `inspect` from the generated working directory. To use a real model, change the manifest provider from `mock` to `openaiCompatible`, point it at a Chat Completions-compatible endpoint, and set `contextWindow` unless you use a preset that fills it.

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

To stress context compression with a long-horizon native tool-calling run:

```bash
export MIMO_API_KEY=sk-...
make test-long-horizon-agent PROVIDER=mimo
```

Set `JEJU_MIMO_BASE_URL` if you need to override the MiMo endpoint.

To validate the evolution mechanics fixture:

```bash
make test-evolve-e2e PROVIDER=mock
```

To run the same evolution mechanics fixture with MiMo:

```bash
export MIMO_API_KEY=sk-...
make test-evolve-e2e PROVIDER=mimo
```

To smoke the effect fixture without credentials, run the mock baseline path:

```bash
make test-evolve-effect-e2e PROVIDER=mock
```

To validate that self-evolution actually improves behavior on held-out triage tasks, run the real MiMo path:

```bash
export MIMO_API_KEY=sk-...
make test-evolve-effect-e2e PROVIDER=mimo
```

The real-provider effect script writes `.jeju-dev/evolve-effect-e2e-mimo/.jeju-dev/evolve-triage-effect-summary.json`, including baseline and best-candidate holdout scores.
