# Jeju

> Define in config. Execute with boundaries. Trace everything. Improve with evidence.

![Jeju project architecture](docs/jeju-architecture.png)

Jeju is a config-defined agent runtime written in Go. It turns declarative manifests into runnable agents through a strict load, validate, compile, and run boundary.

A manifest describes the agent's model, instructions, runtime loop, workspace, tools, skills, permissions, context budget, and evaluation rules. The runtime then executes the compiled result with workspace controls, permission gates, auditable trajectories, file-backed artifacts, inspection views, and optional config-space self-evolution.

## Features

- **Config-defined agents**: define models, instructions, tools, skills, permissions, context budget, and evaluation in one manifest.
- **Compiled runtime boundary**: Jeju follows `config.LoadFile -> config.Validate -> compiler.Compile -> runtime.Run`, so execution never depends on raw YAML.
- **Bounded execution**: work happens inside explicit workspace, tool, skill, permission, sandbox, timeout, and context-window limits.
- **Auditable trajectories**: every run records lifecycle, model, context, tool, permission, artifact, and evaluation events, with large payloads stored separately.
- **Evidence-driven improvement**: inspect completed runs, evaluate outcomes, and use `jeju evolve` to search config-space patches against train and selection tasks.

## Quick Start

The generated agent bundle includes a manifest, prompt, workspace, skills, run store, and a `mock` model, so the full lifecycle runs without API credentials or approval prompts. Install the CLI, then choose any local directory for the generated agent project.

```bash
# Install the latest released CLI.
curl -fsSL https://raw.githubusercontent.com/cosmtrek/jeju/master/scripts/install.sh | sh
jeju version

# Scaffold a new agent project wherever you want to keep it.
jeju init research --dir ~/jeju-agents/research-agent

# Run the generated agent bundle.
cd ~/jeju-agents/research-agent
jeju validate agents/research.agent.yaml
jeju run agents/research.agent.yaml "Create a deep research brief on AI agent evaluation methods, compare three approaches, and save the report to notes.md"

# Inspect the recorded run.
jeju runs
jeju inspect <run_id>
```

The run writes metadata, a config snapshot, trajectory JSONL, artifacts, and `final.md` under `runs/<run_id>/`. The first inspect view should show the full loop: skill loading, model calls, permission checking, a workspace write, artifacts, and evaluation.

The default `mock` provider is deterministic, so this first run demonstrates Jeju's execution lifecycle rather than live web research.

When running demos or fixture scenarios from the Jeju source checkout, keep
generated artifacts under the ignored `.jeju-dev/` directory:

```bash
jeju run --runs-dir .jeju-dev/runs/research agents/research.agent.yaml "Save a short note to notes.md"
jeju runs --runs-dir .jeju-dev/runs/research
jeju inspect --runs-dir .jeju-dev/runs/research <run_id>
```

Inside a generated agent project, the default `./runs` store remains the normal
user-facing run history.

### Install From Source

Developers can also install Jeju from the Go module:

```bash
go install github.com/cosmtrek/jeju/cmd/jeju@latest
```

### Run With DeepSeek

To run the same generated agent with a real model, edit `agents/research.agent.yaml` and replace the generated `models.providers.primary` block:

```yaml
models:
  providers:
    primary:
      type: openaiCompatible
      preset: deepseek
      model: deepseek-v4-flash
      envKey: DEEPSEEK_API_KEY
      thinking:
        type: disabled
```

Then set credentials and run the agent again:

```bash
export DEEPSEEK_API_KEY=sk-...

# Optional: required only if the agent should call the scaffolded search_api tool.
export EXA_API_KEY=...

jeju validate agents/research.agent.yaml
jeju run agents/research.agent.yaml "Create a deep research brief on AI agent evaluation methods, compare three approaches, and save the report to notes.md"
```

`preset: deepseek` fills the DeepSeek base URL and context window defaults. See [DeepSeek setup notes](docs/deepseek.md) for provider details.

## Design Philosophy

Jeju treats an agent as a small, explicit runtime unit instead of an opaque application. The manifest is the source of truth for behavior, the compiler fixes the executable boundary, and the runtime records each meaningful effect:

```text
Manifest -> Validate -> Compile -> Run -> Gate -> Trace -> Evaluate -> Inspect
```

The runtime does not read YAML directly. Configuration is loaded, validated, and compiled into a `CompiledAgent` before execution. This keeps runtime behavior grounded in the manifest, loaded instructions, tools, skills, permissions, and evaluator configuration rather than hardcoded branches.

The current runtime stores runs on disk, constrains file and shell tools to a configured workspace, and leaves behind enough metadata and trajectory data to inspect what happened.

Self-evolution is built on top of the same contract. An evolution experiment does not mutate the source agent in place; it runs candidate bundles, scores them with task-level evaluation, applies only allowed exact patches, and materializes the best accepted config separately.

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

## Examples

Runnable recommended scenarios live under [examples](examples/README.md).
These are not test fixtures; they are example agent bundles for repeatable,
evaluable workflows such as code review, privacy-aware delegation, research,
model comparison, and agent-as-skill integration.

The first example is a [code review agent](examples/code-review-agent/README.md)
that reviews the current Git workspace diff with read-only tools and prints
structured findings directly.

## Tests

Run the normal code checks first:

```bash
go test ./...
go vet ./...
```

Run the mock fixture agent end to end without credentials:

```bash
make test-agent
```

Provider-backed and heavier smoke runs are opt-in and may call real model APIs:

```bash
# DeepSeek fixture run.
export DEEPSEEK_API_KEY=sk-...
make test-agent PROVIDER=deepseek

# MiMo fixture run.
export MIMO_API_KEY=sk-...
make test-agent PROVIDER=mimo

# Long-horizon context compression run.
make test-long-horizon-agent PROVIDER=mimo

# Evolution mechanics.
make test-evolve-e2e PROVIDER=mock
make test-evolve-e2e PROVIDER=mimo

# Evolution effect smoke and real-provider holdout check.
make test-evolve-effect-e2e PROVIDER=mock
make test-evolve-effect-e2e PROVIDER=mimo
```

Fixture scripts copy sources into temporary or `.jeju-dev/<scenario>/` workdirs before running, so fixture sources stay clean. Set `JEJU_MIMO_BASE_URL` if you need to override the MiMo endpoint.

For source-checkout development, avoid writing generated runs to repo-root
`runs/` or example-local `runs/` directories. Prefer `--runs-dir
.jeju-dev/runs/<scenario>` for demo runs and `.jeju-dev/evolve/<scenario>` for
evolution output.
