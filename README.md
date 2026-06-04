# Jeju

> Define behavior in config. Run with boundaries. Inspect every effect. Improve with evidence.

![Jeju project architecture](docs/jeju-architecture.png)

Jeju is an experimental local-first agent harness for developers who want to define agent behavior in config, run it with strict boundaries, inspect every effect, and improve it with evaluation evidence.

A manifest describes the agent's model, instructions, runtime loop, workspace, tools, skills, permissions, context budget, and evaluation rules. Jeju compiles that manifest, runs it against a local workspace, records every meaningful effect in an append-only trajectory, and can improve the agent with evaluation evidence.

The same harness supports several local agent workflows: quick agent experiments, source-backed evaluations, portable specialist agent bundles, and repeated high-frequency tasks that developers or higher-level AI agents want to turn into reusable Jeju agents.

## Features

- **Config-defined behavior**: define models, instructions, tools, skills, permissions, context budget, and evaluation in one manifest.
- **Strict execution boundaries**: work happens inside explicit workspace, tool, skill, permission, sandbox, timeout, and context-window limits.
- **Effect-level inspection**: every run records lifecycle, model, context, tool, permission, artifact, and evaluation events.
- **File-backed evidence**: each run writes one append-only `trajectory.jsonl`; config snapshots, final answers, evaluation results, and generated artifacts are recorded as trajectory artifacts.
- **Evaluation-guided improvement**: inspect completed runs, evaluate outcomes, and use `jeju evolve` to search config-space patches against train and selection tasks.
- **Portable agent bundles**: package focused agent workflows so developers or higher-level AI agents can reuse them in local workspaces.

## Quick Start

The generated agent bundle includes a manifest, prompt, workspace, skills, run store, and a `mock` model, so the full lifecycle runs without API credentials or approval prompts. Install the CLI, then choose any local directory for the generated agent project.

```bash
# Install the latest released CLI on macOS or Linux.
curl -fsSL https://raw.githubusercontent.com/cosmtrek/jeju/master/scripts/install.sh | sh
jeju version

# Scaffold a new agent project wherever you want to keep it.
mkdir -p ~/jeju-agents
jeju init research --dir ~/jeju-agents/research-agent

# Run the generated agent bundle.
cd ~/jeju-agents/research-agent
jeju validate agents/research.agent.yaml
jeju run agents/research.agent.yaml "Create a deep research brief on AI agent evaluation methods, compare three approaches, and save the report to notes.md"

# Inspect the recorded run.
jeju runs
jeju inspect <run_id>
```

The run writes an append-only `trajectory.jsonl` under `runs/<run_id>/`; `report.html` is a derived inspection view generated from that log. The first inspect view should show the full loop: skill loading, model calls, permission checking, a workspace write, artifacts, and evaluation.

The default `mock` provider is deterministic, so this first run demonstrates Jeju's execution lifecycle rather than live web research.

The install script targets macOS and Linux. Windows release archives are
published as zip assets; download the matching `jeju_windows_<arch>.zip` from
GitHub Releases and put `jeju.exe` on `PATH`, or use the source install below.

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

Developers can also install Jeju from the Go module. This is a fallback path
when a platform-specific installer is unavailable:

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

## Use Cases

Jeju is a harness, not a broad multi-agent platform. The current implementation
is strongest when the work can be expressed as a focused local agent with clear
tools, permissions, run evidence, and optional evaluation.

- **Agent experiments**: prototype a local agent by changing manifest fields,
  prompts, skills, tools, model providers, and runtime limits.
- **Evaluation harnesses**: run a fixed agent against task cases, inspect
  trajectory evidence, and compare outcomes with rule, command, or LLM
  evaluators.
- **Reusable specialist agents**: package review, triage, research, docs, or
  benchmark workflows into portable agent bundles.
- **High-frequency workflow capture**: turn repeated local tasks into bounded
  agents that can be run by a developer or invoked by a higher-level AI agent.

Prefer a script for deterministic automation. Prefer an authoring-agent skill
when the value is only reusable instructions or prompt guidance. Use Jeju when
the workflow needs model reasoning plus explicit tools, permissions, run
evidence, or evaluation.

### Agent Authoring Skill

Higher-level AI agents can use the authoring skill to create Jeju agent bundles.
The skill helps them decide whether Jeju is the right artifact, choose a narrow
task boundary, generate a minimal manifest and prompt, set permissions and
runtime limits, add smoke evaluation, and validate the result.

Install the skill with `npx skills`:

```bash
npx skills add cosmtrek/jeju --skill jeju-agent-builder
```

Or ask Codex to install it for you:

```text
Use skill-installer to install the jeju-agent-builder skill from cosmtrek/jeju.
```

The skill is [skills/jeju-agent-builder/SKILL.md](skills/jeju-agent-builder/SKILL.md).
The full authoring manual is [docs/manual-for-agents.md](docs/manual-for-agents.md).

## Design Philosophy

Jeju treats an agent as a small, explicit harness unit instead of an opaque application. The manifest is the source of truth for behavior, the compiler fixes the executable boundary, and the runtime records each meaningful effect:

```text
Manifest -> Validate -> Compile -> Run -> Gate -> Trace -> Evaluate -> Inspect
```

The runtime does not read YAML directly. Configuration is loaded, validated, and compiled into a `CompiledAgent` before execution. This keeps runtime behavior grounded in the manifest, loaded instructions, tools, skills, permissions, and evaluator configuration rather than hardcoded branches.

The current runtime stores runs on disk, constrains file and shell tools to a configured workspace, and leaves behind enough metadata and trajectory data to inspect what happened.

Evaluation-guided improvement is built on top of the same contract. An evolution experiment does not mutate the source agent in place; it runs candidate bundles, scores them with task-level evaluation, applies only allowed exact patches, and materializes the best accepted config separately.

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
- Run output records model calls, tool calls, permission decisions, skill events, artifacts, evaluation, and lifecycle events under the configured run store, defaulting to `./runs`.
- `evaluate`: optionally runs rule-based checks after completion.

## Evaluation-Guided Improvement

Jeju can improve a config-defined agent with `jeju evolve`. An evolution experiment is a separate `kind: EvolutionExperiment` manifest that points at a target agent, datasets, an objective metric, edit boundaries, an evolver agent, search limits, and an output directory.

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
    - harness:prompt

data:
  train: ../datasets/train.jsonl
  selection: ../datasets/selection.jsonl
  render:
    template: ../prompts/task_input.md.tmpl

objective:
  metric: evaluation.score
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

Patch safety is intentionally narrow. The patch target must be listed in expanded `target.editable`, must not match `target.forbidden`, and `replace` patches require `find` to match exactly once inside the candidate bundle. Harness aliases such as `harness:prompt`, `skill:<name>`, and `tool:<name>` expand to concrete editable paths. `file:<relative-path>` and `dir:<relative-path>` authorize explicitly scoped harness files, and `op: "write"` can write full content only to editable file targets. After patching, Jeju validates that no forbidden or uneditable manifest leaf field changed, then compiles the candidate before running it.

Common commands:

```bash
# Validate the baseline bundle and compile without model calls.
jeju evolve --dry-run experiments/evolve.yaml

# Measure baseline train/selection metrics without calling the evolver.
jeju evolve --baseline-only experiments/evolve.yaml

# Run the full evolution loop.
jeju evolve experiments/evolve.yaml

# Run final holdout test metrics after selecting the best candidate.
jeju evolve --test experiments/evolve.yaml

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

If `data.test` is configured, add `--test` to run the test split only after
candidate selection, on baseline and the final best. Test metrics are written to
the report but do not affect candidate acceptance.

See [docs/agent-evolution-manifest.md](docs/agent-evolution-manifest.md) for the full schema and [docs/self-evolution.md](docs/self-evolution.md) for the design details.

## Examples

Runnable recommended scenarios live under [examples](examples/README.md). These
are example agent bundles, not test fixtures.

Current examples cover a [code review agent](examples/code-review-agent/README.md),
a [privacy delegation agent](examples/privacy-delegation-agent/README.md), and a
[SkillsBench Lite agent](examples/skillsbench-lite-agent/README.md). They show
how Jeju defines behavior in config, runs with explicit boundaries, records
effects, and uses evaluation evidence to improve an agent.

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

## License

Jeju is released under the MIT License. See [LICENSE](LICENSE) for details.
