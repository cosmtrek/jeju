

# Jeju

> Declarative, local-first runtime for bounded AI agents.
> Define an agent in one manifest, run it headless, and audit every effect.

![Jeju project architecture](docs/jeju-architecture.png)

## What Is Jeju?

Jeju is to an AI agent what a Kubernetes manifest is to a deployment: you
describe the agent declaratively — model provider, instructions, runtime loop,
workspace, tools, skills, permissions, context budget, optional output schema,
and evaluators — and Jeju validates, compiles, and runs that manifest against a
local workspace, recording every meaningful effect in `trajectory.jsonl`. It can
then use evaluation evidence to improve the agent with `jeju evolve`.

Jeju is **not** an interactive coding assistant and **not** a Python agent
framework. It is the layer below those: a way to capture a workflow as a
versioned, permissioned, inspectable agent you can package and re-run headlessly.

|                | Interactive assistants<br/>(Claude Code, Cursor) | Agent frameworks<br/>(LangGraph, CrewAI) | **Jeju** |
|----------------|--------------------------|--------------------------|----------|
| Interface      | interactive chat         | Python code              | **declarative manifest** |
| Runs           | live session             | embedded in your app     | **headless, recorded** |
| Boundaries     | session-level            | hand-coded               | **workspace / tools / permissions / sandbox enforced** |
| Run evidence   | transcript               | your own logging         | **canonical `trajectory.jsonl`** |
| Improvement    | manual                   | manual                   | **`jeju evolve` with evaluation evidence** |
| Distribution   | —                        | pip package              | **content-addressed agent bundle** |

Jeju is experimental. It is strongest when a local workflow can be packaged as a
focused agent with clear tools, permissions, run evidence, and optional
evaluation.

## Quickstart

### Run the showcase

The showcase is a bug rescue workflow. A tiny Python ledger project has failing
rounding tests, and Jeju packages the repair as a bounded agent: DeepSeek V4
Flash reads the fixture, runs the test harness, edits the implementation, reruns
tests, writes `REPAIR.md`, and records the full trajectory. It exercises the
whole positioning end to end — a declarative manifest, write and shell access
fenced inside a sandboxed workspace, and a recorded trajectory you can audit
afterward — and needs no external search services.

Requirements: macOS or Linux, a DeepSeek API key, Python 3 for the fixture
tests, and Go 1.25 or newer only for source installs.

```bash
# Install the latest released CLI on macOS or Linux.
curl -fsSL https://raw.githubusercontent.com/cosmtrek/jeju/master/scripts/install.sh | sh
jeju version

# Run the DeepSeek V4 Flash showcase.
export DEEPSEEK_API_KEY=sk-...
git clone https://github.com/cosmtrek/jeju.git
cd jeju
./scripts/run-bug-rescue-agent.sh

# Inspect the recorded run printed by the script.
jeju inspect --runs-dir .jeju-dev/runs/bug-rescue <run_id>
jeju view --runs-dir .jeju-dev/runs/bug-rescue <run_id>
```

The agent turns the failing ledger tests green by fixing the cent-rounding bug
in a copied workspace, writes `REPAIR.md`, and saves the run:

```text
.jeju-dev/runs/bug-rescue/<run_id>/
├── trajectory.jsonl               # canonical append-only run record
└── report.html                   # derived inspection view
```

![Jeju trajectory visualization](docs/trajectory-visualization.png)

`trajectory.jsonl` is the source of truth; `jeju view` lists run history, opens
the derived report for a run ID, and refreshes it when the trajectory is newer.
For a no-credential mock lifecycle check, use `jeju init <name>` or
`make test-agent`. Windows is not guaranteed yet; install from source with
`go install github.com/cosmtrek/jeju/cmd/jeju@latest`.

### Build your own agent

The fastest path is to install the `jeju-agent-builder` skill in Codex, Claude
Code, or another agent environment, then let that agent author, run, and inspect
Jeju agents for you:

```bash
npx skills add cosmtrek/jeju --skill jeju-agent-builder
```

```text
Use jeju-agent-builder to create and smoke-test a minimal Jeju agent for <workflow>.
```

You can also build manually: `jeju init <name> --dir ~/jeju-agents/<name>`, edit
the manifest and prompt, run `jeju validate`, run a smoke task, and inspect the
trajectory with `jeju inspect` or `jeju view`. See
[Manual For Agents](docs/manual-for-agents.md) for the authoring guide.

## Key Capabilities

- **Declarative behavior**: the whole agent contract — including optional final
  output schema — lives in one manifest, with prompts and runtime skills as
  adjacent files. No behavior is hidden in code.
- **Enforced boundaries**: every run is constrained by explicit workspace, tool,
  skill, permission, sandbox, timeout, and context-window limits, and every tool
  call passes through a policy gate before it executes.
- **Audited by construction**: one canonical append-only `trajectory.jsonl`
  records lifecycle, model, context, tool, permission, artifact, evaluation, and
  run-summary events, with a derived `report.html` view for review.
- **Evidence-driven improvement**: run task sets, score outcomes, and let
  `jeju evolve` search bounded config-space patches — a GEPA-style search that
  lifted held-out HotpotQA by +3.6pp answer F1 over the unevolved baseline.
- **Portable bundles**: package a focused workflow as a content-addressed agent
  that developers or higher-level AI agents can add and run by a stable
  reference.

Reach for a plain script when deterministic automation is enough. Reach for Jeju
when the workflow needs model reasoning plus explicit tools, permissions, run
evidence, or evaluation — agent experiments, evaluation harnesses, reusable
specialist agents (review, triage, docs, benchmarks), or capturing a
high-frequency local task as a bounded agent another agent can invoke.

## How It Works

Jeju treats an agent as a small, explicit harness unit instead of an opaque
application. The runtime never reads YAML directly; configuration is loaded,
validated, and compiled into a `CompiledAgent` before execution:

```text
Manifest -> Validate -> Compile -> Run -> Gate -> Trace -> Evaluate -> Inspect
```

An agent bundle keeps that contract and its adjacent files together. A minimal
bundle is just enough structure to validate and run:

```text
<name>/
├── agents/
│   └── <name>.agent.yaml          # manifest — source of truth
├── prompts/
│   └── <name>.md                  # system instructions
├── workspace/
│   └── <name>/.gitkeep            # local working directory
├── skills/                        # optional runtime skills
│   └── <skill>/SKILL.md
├── eval/                          # optional evaluators
│   └── <evaluator>.py
└── README.md
```

At a high level, a read-only specialist manifest looks like this:

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

instructions:
  system: ../prompts/repo-inspector.md

runtime:
  model: primary
  loop:
    type: react
  limits:
    maxSteps: 12
    maxDurationSec: 300

workspace:
  path: ../workspace/repo-inspector

tools:
  - read
  - search

permissions:
  access: readOnly
  approval: never

output:
  name: repo_summary
  schema:
    type: object
    required: [summary, findings]
    additionalProperties: false
    properties:
      summary: { type: string }
      findings:
        type: array
        items: { type: string }

evaluate:
  enabled: true
  evaluators:
    - name: basic
      uses: rules
      rules: [finalAnswerExists, runCompleted]
```

See [Agent Manifest](docs/agent-manifest.md) for the full field reference,
defaults, supported values, and validation rules.

## Packaging & Teams

### Agent Package

Agent Package is the distribution layer for one reusable `kind: Agent`. A small
`jeju.package.yaml` manifest wraps an existing bundle so the agent can be packed,
added to a local content-addressed store, and run by a stable reference:

```bash
jeju package pack ./agents/code-review --out dist/
jeju package add dist/coding-code-review-0.1.0.jpkg
jeju run package://coding/code-review@0.1.0 "Review current diff."
jeju run p:coding/code-review "Review current diff."
jeju run --model deepseek-v4-flash p:coding/code-review "Review current diff."
```

The package layer only adds distribution metadata, source provenance,
digest-based storage, and stable refs; runs still take the normal
`LoadFile -> Validate -> Compile -> Run` path. Sources can be local artifacts,
GitHub or generic Git subdirectories, or `jeju:` registry refs. See
[Agent Package](docs/agent-package.md) for the manifest fields, source syntax,
and full command set.

`jeju run --model <model-id>` overrides the active runtime provider's model ID
for one run without modifying the package or its digest. The provider type,
endpoint, credentials, context window, token limits, and other model settings
still come from the agent manifest.

Package-backed runs default to `~/.jeju/runs` unless `--runs-dir` or
`JEJU_RUNS_DIR` is set, so invoking `p:...` from an arbitrary directory does not
create a local `./runs` folder.

### Agent Team

For work that needs several bounded perspectives but should stay inspectable,
`kind: AgentTeam` runs a lead-worker collaboration from a single goal. One lead
agent plans tasks across rounds; workers run as ordinary, isolated `kind: Agent`
runs; the lead keeps message history across rounds; and the controller records
task state, child run references, verifier gates, and the selected final answer.
Workers never chat peer-to-peer — this is a bounded outer controller, not a
multi-agent platform.

```bash
jeju team run examples/code-review-team/teams/code-review.team.yaml "Review the current diff."
```

See [Agent Team](docs/agent-team.md) for the manifest shape and topology.

## Evaluation & Evolution

`jeju evolve` improves a declarative agent without mutating the source in place.
An experiment points at a target agent, datasets, an objective metric, edit
boundaries, an evolver agent, and search limits; the loop is offline and
auditable, ending in a best-candidate bundle and report.

Two search strategies are built in: `pareto` (default), a GEPA-style search over
an instance-wise Pareto frontier with a cheap mini-batch cascade gate, and
`hillclimb`, greedy single-lineage search. Both are validated on a public
benchmark: on HotpotQA (distractor) with DeepSeek V4 Flash, evolving only the
solver prompt improved the held-out test split by **+3.6pp answer F1 and +7pp
exact match** over the unevolved baseline, while `hillclimb` under the same
budget managed only +1.7pp F1 / +4pp EM before stalling in a local optimum.

```bash
jeju evolve --dry-run experiments/evolve.yaml      # validate the experiment
jeju evolve --baseline-only experiments/evolve.yaml # score the baseline
jeju evolve experiments/evolve.yaml                # run the search
jeju evolve --test experiments/evolve.yaml         # evaluate on the test split
```

See [Evolution Manifest](docs/agent-evolution-manifest.md),
[Self Evolution](docs/self-evolution.md), and the
[HotpotQA evolve benchmark](examples/hotpotqa-agent/README.md) for the schema,
design notes, and full study.

## Examples & Documentation

Runnable example agent bundles live under [examples](examples/README.md) — not
test fixtures, but recommended scenarios showing declarative behavior, explicit
boundaries, run evidence, and evaluation or evolution where useful:

- [Bug rescue agent](examples/bug-rescue-agent/README.md)
- [Code review with agent tools](examples/code-review-with-agent-tools/README.md)
- [Code review team](examples/code-review-team/README.md)
- [Commit plan agent](examples/commit-plan-agent/README.md)
- [HotpotQA evolve benchmark](examples/hotpotqa-agent/README.md)
- [Privacy delegation agent](examples/privacy-delegation-agent/README.md)
- [SkillsBench Lite agent](examples/skillsbench-lite-agent/README.md)

The high-frequency single-agent code review workflow is maintained as the
installable [coding/code-review](catalog/coding/code-review/README.md) catalog
package.

Reference docs:

- [Agent Manifest](docs/agent-manifest.md) ·
  [Trajectory Format](docs/trajectory-format.md) ·
  [Trajectory Visualization](docs/trajectory-visualization.md)
- [Agent Package](docs/agent-package.md) · [Agent Team](docs/agent-team.md)
- [Evolution Manifest](docs/agent-evolution-manifest.md) ·
  [Self Evolution](docs/self-evolution.md)
- [Manual For Agents](docs/manual-for-agents.md) ·
  [DeepSeek Setup](docs/deepseek.md)

## Development

```bash
go test ./...          # run the test suite
go vet ./...           # static checks for runtime/compiler/tooling changes
make test-agent        # mock fixture agent, end to end, no credentials
```

Provider-backed smoke runs are opt-in and may call real model APIs:

```bash
export DEEPSEEK_API_KEY=sk-...
make test-agent PROVIDER=deepseek

export MIMO_API_KEY=sk-...
make test-agent PROVIDER=mimo
```

For source-checkout development, keep generated runs out of repo-root `runs/` by
pointing at `.jeju-dev/`, for example
`jeju run --runs-dir .jeju-dev/runs/<scenario> <agent.yaml> "<task>"`, then list
them with `jeju view --runs-dir .jeju-dev/runs/<scenario>`. Inside a generated
user agent project, the default `./runs` store remains the normal local run
history for manifest-based runs.

## License

Jeju is released under the MIT License. See [LICENSE](LICENSE) for details.
