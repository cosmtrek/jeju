# Agent Evolution Manifest

`kind: EvolutionExperiment` defines an evaluation-guided improvement experiment for one target Jeju agent. It is separate from `kind: Agent`: the experiment describes how to search for a better config, while the target agent manifest still describes how a candidate agent runs.

`jeju evolve` loads the experiment, materializes isolated candidate bundles, runs train and selection tasks, asks an evolver agent for structured proposals, applies safe patches, and writes an auditable best candidate. With `--test`, it also runs `data.test` after selection on baseline and the final best.

Relative paths in an evolution manifest are resolved from the manifest file location.

## Example

```yaml
apiVersion: jeju/v1alpha1
kind: EvolutionExperiment

metadata:
  name: evolve-triage
  description: "Improve an operations triage agent."

target:
  agent: ../agents/triage.agent.yaml
  editable:
    - harness:prompt
    - skill:triage
    - tool:lookup

data:
  train: ../datasets/train.jsonl
  selection: ../datasets/selection.jsonl
  test: ../datasets/test.jsonl
  render:
    template: ../prompts/task_input.md.tmpl

objective:
  metric: evaluation.evaluators["triage_judge"].score
  minDelta: 0.2
  guards:
    - "evaluation.passed_rate >= baseline.evaluation.passed_rate"
    - "run.modelErrors <= baseline.run.modelErrors"
  guidance:
    - "Improve general incident triage behavior rather than memorizing examples."

evolver:
  agent: ../agents/evolver.agent.yaml
  proposals: 2

search:
  iterations: 2
  parallelism: 2

output:
  dir: ../.jeju-dev/evolve-triage
```

## Top-Level Fields

| Field | Required | Description |
| --- | --- | --- |
| `apiVersion` | yes | Must be `jeju/v1alpha1`. |
| `kind` | yes | Must be `EvolutionExperiment`. |
| `metadata` | yes | Experiment identity. |
| `target` | yes | The target agent and edit policy. |
| `data` | yes | Train and selection datasets plus optional rendering. |
| `objective` | yes | Primary metric, direction, improvement threshold, guards, and guidance. |
| `evolver` | yes | Jeju agent used to propose patches. |
| `search` | no | Iteration, trial, parallelism, and budget controls. |
| `output` | no | Experiment output directory. |

## Metadata

```yaml
metadata:
  name: evolve-triage
  description: "Improve an operations triage agent."
  labels:
    suite: smoke
```

`metadata.name` is required and is used in the experiment id and default output directory.

## Target

```yaml
target:
  agent: ../agents/triage.agent.yaml
  editable:
    - harness:prompt
    - skill:triage
```

| Field | Required | Description |
| --- | --- | --- |
| `agent` | yes | Target `kind: Agent` manifest, resolved relative to the evolution manifest. |
| `editable` | yes | Harness surfaces the evolver may optimize. Prefer aliases such as `harness:prompt`, `skill:<name>`, and `tool:<name>`. |
| `forbidden` | no | Extra case-specific paths that must never change. Jeju also applies default safety protections. Forbidden paths override editable paths. |

Path syntax:

- `harness:prompt` expands to `instructions.system`.
- `skill:<name>` expands to `skills.active` plus the named skill directory
  under each configured `skills.dirs` root.
- `harness:skills` expands to `skills.active` plus every configured
  `skills.dirs` root.
- `tool:<name>` expands to the named tool's description. This is the default
  safe surface for tool-use strategy.
- `harness:tools` expands the same tool-use strategy surface for every tool.
- Dot paths address object fields, for example `runtime.limits.maxSteps`.
- `[]` matches array elements, for example `tools[].description`.
- `*` matches one map key segment, for example `models.providers.*.temperature`.
- `instructions.system` is special: patches edit the referenced prompt file.
- `file:<relative-path>` edits a referenced bundle file. The path is resolved
  relative to the target agent manifest and must remain inside the candidate
  bundle, for example `file:../skills/triage/SKILL.md`.
- `dir:<relative-path>` exposes files under a referenced bundle directory and
  authorizes `file:` patches inside it, for example `dir:../skills/triage`.

The controller snapshots manifest leaf fields before and after patching. Any changed field outside expanded `editable` or inside effective `forbidden` rejects the candidate.

Jeju applies these default forbidden paths to every evolution run:

```yaml
forbidden:
  - permissions
  - workspace
  - models.providers.*.envKey
  - models.providers.*.baseUrl
  - evaluate.evaluators[].command
  - tools[].command
  - tools[].http
  - tools[].env
  - tools[].capabilities
  - skills.dirs
```

Only add `target.forbidden` for case-specific constraints beyond those defaults.

`tool:<name>` intentionally does not grant schema files, implementation files,
command, HTTP, environment, or capability changes. Those can alter parameter
contracts or execution boundaries and must be listed explicitly when an
experiment really needs them:

```yaml
target:
  editable:
    - tool:search
    - file:../schemas/search.schema.json
  forbidden:
    - runtime.limits.maxSteps
```

## Data

```yaml
data:
  train: ../datasets/train.jsonl
  selection: ../datasets/selection.jsonl
  test: ../datasets/test.jsonl
  render:
    template: ../prompts/task_input.md.tmpl
```

| Field | Required | Description |
| --- | --- | --- |
| `train` | yes | JSONL tasks used for proposal feedback and train filtering. |
| `selection` | yes | JSONL tasks used for candidate acceptance. |
| `test` | no | Final holdout tasks run only when `jeju evolve --test` is specified. |
| `render.template` | no | Go template that renders each task into the string passed to `runtime.Run`. |

### Task JSONL

Each non-empty line is one task:

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
    "rubric": "Return strict JSON and choose the right severity, route, and action."
  },
  "metadata": {
    "category": "auth"
  },
  "weight": 1
}
```

| Field | Required | Description |
| --- | --- | --- |
| `id` | yes | Unique task id within the split. |
| `input` | no | User-defined task payload. If no template is configured and `input` is a string, it is used directly; otherwise it is pretty-printed JSON. |
| `expected` | no | Task-level expected data passed to effective evaluation. Built-in task checks support `mustInclude`/`must_include` and `mustNotInclude`/`must_not_include`. |
| `eval` | no | Task-level evaluator hints or rubric passed to evaluators. |
| `metadata` | no | Task metadata passed to evaluators and stored in results. |
| `weight` | no | Metric weight. Defaults to `1`. |

Template context is the full task object. Example:

```gotemplate
Ticket:
{{ .Input.ticket }}

Customer tier: {{ .Input.customer_tier }}
```

## Objective

```yaml
objective:
  metric: evaluation.score
  minDelta: 0.02
  guards:
    - "evaluation.passed_rate >= baseline.evaluation.passed_rate"
    - "run.tokens <= 1.5 * baseline.run.tokens"
  guidance:
    - "Do not trade away factual accuracy for lower token cost."
```

| Field | Required | Description |
| --- | --- | --- |
| `metric` | yes | Primary metric source used for candidate selection. |
| `direction` | no | `maximize` or `minimize`. Defaults to `maximize`. |
| `minDelta` | no | Minimum improvement over the incumbent best. Defaults to `0`. |
| `guards` | no | Hard constraints. A candidate that fails a guard is rejected. |
| `guidance` | no | Qualitative instructions passed to the evolver. It does not affect scoring directly. |

Supported metric sources:

| Metric | Meaning |
| --- | --- |
| `evaluation.score` | Average effective evaluation score. |
| `evaluation.passed_rate` | Fraction of trials whose effective evaluation passed. |
| `evaluation.evaluators["name"].score` | Average score for one evaluator. |
| `evaluation.evaluators["name"].passed` | Pass rate for one evaluator. |
| `evaluation.evaluators["name"].rules["rule"].passed` | Pass rate for one named rule result. |
| `run.steps` | Average runtime steps. |
| `run.toolCalls` | Average tool calls. |
| `run.modelCalls` | Average model calls. |
| `run.modelErrors` | Average model errors. |
| `run.toolErrors` | Average tool errors. |
| `run.permissionDenied` | Average denied permission checks. |
| `run.tokens` / `run.totalTokens` | Average total model tokens. |
| `run.promptTokens` | Average prompt tokens. |
| `run.completionTokens` | Average completion tokens. |
| `run.durationSec` | Average run duration in seconds. |

Guard expressions support:

- metric variables, including `baseline.<metric>`
- numbers
- `+`, `-`, `*`, `/`
- parentheses
- `>=`, `<=`, `>`, `<`, `==`, `!=`

Baseline metrics are split-local. During train evaluation, `baseline.*` means train baseline metrics; during selection evaluation, it means selection baseline metrics.

## Evolver

```yaml
evolver:
  agent: ../agents/evolver.agent.yaml
  proposals: 2
```

| Field | Required | Description |
| --- | --- | --- |
| `agent` | yes | A normal Jeju `kind: Agent` manifest used to produce proposal JSON. |
| `proposals` | no | Maximum proposals to accept per iteration. Defaults to `2`; valid range is `1..8`. |

The evolver receives a feedback digest as its task input. It should return either a single proposal or a wrapper with `proposals`.

Beyond objective, editable targets, train results, history, and editable
content, the digest carries reflection material for trace-grounded proposals:

| Digest field | Description |
| --- | --- |
| `reflection` | The lowest-scoring train trials of the parent candidate, each with the rendered task input, the final output, the score, and evaluator feedback messages (truncated excerpts). |
| `rejected_proposals` | The most recent rejected proposal hypotheses with their rejection reasons, so the evolver does not resubmit failed ideas. |
| `pool` | `pareto` strategy only: the current candidate pool with train metrics and instance win counts. |

Selection and test split details are always withheld from the digest.

Single proposal:

```json
{
  "id": "proposal-001",
  "hypothesis": "The prompt does not enforce the required JSON shape.",
  "changes": [
    {
      "target": "instructions.system",
      "find": "You are a support assistant.\n",
      "replace": "You are a support assistant. Return only strict JSON.\n"
    },
    {
      "target": "file:../skills/triage/SKILL.md",
      "find": "Answer carefully.\n",
      "replace": "Answer carefully. Follow the triage rubric before final output.\n"
    },
    {
      "target": "file:../skills/triage/examples.md",
      "op": "write",
      "content": "Use the triage rubric before responding.\n"
    }
  ],
  "confidence": 0.8
}
```

Wrapper:

```json
{
  "proposals": [
    {
      "hypothesis": "Make the output format explicit.",
      "changes": [
        {
          "target": "instructions.system",
          "find": "old text",
          "replace": "new text"
        }
      ]
    }
  ]
}
```

Proposal fields:

| Field | Required | Description |
| --- | --- | --- |
| `id` | no | Proposal id. Jeju fills one if omitted. |
| `parent_candidate` | no | Filled by Jeju when materializing a candidate. |
| `hypothesis` | yes | Why the change should improve the metric. |
| `changes` | yes | Patch operations. |
| `confidence` | no | Optional evolver confidence. |

Patch operation fields:

| Field | Required | Description |
| --- | --- | --- |
| `target` | yes | Editable path. |
| `op` | no | `replace` or `write`. Defaults to `replace`. |
| `find` | for `replace` | Text that must match exactly once. |
| `replace` | for `replace` | Replacement text. |
| `content` | for `write` | Full file content to write. |

## Search

```yaml
search:
  strategy: pareto
  iterations: 10
  parallelism: 2
  minibatch: 10
  pool: 8
  seed: 42
```

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `strategy` | no | `pareto` | Search strategy: `pareto` (default) or `hillclimb`. |
| `iterations` | no | `3` | Maximum proposal iterations. |
| `trialsPerTask` | no | `1` | Repeated runs per candidate/task. |
| `parallelism` | no | `1` | Concurrent trial workers. |
| `minibatch` | no | `8` | `pareto` only: train tasks per cascade gate batch. |
| `pool` | no | `8` | `pareto` only: maximum candidate pool size. |
| `seed` | no | `1` | `pareto` only: RNG seed for parent and mini-batch sampling. |
| `budget.maxRuns` | no | unset | Stop when recorded candidate/evolver run count reaches this value. |
| `budget.maxModelTokens` | no | unset | Stop when recorded model token usage reaches this value. |

`pareto` is the default: on the public HotpotQA benchmark it more than
doubled the held-out test gain of `hillclimb` under the same budget
(+3.6pp F1 / +7pp EM vs +1.7pp / +4pp; see
`examples/hotpotqa-agent/README.md`). Use `hillclimb` when you want the
cheapest possible single-lineage search.

### hillclimb

`hillclimb` is batch hill-climbing on a single lineage:

1. Run baseline train and selection.
2. Ask the evolver for up to `proposals` proposals against the current best.
3. Evaluate each candidate on train.
4. If train guards pass, evaluate on selection.
5. Accept a candidate only if it improves the current best selection metric by `minDelta` and passes guards.
6. Stop after the iteration limit, budget exhaustion, or three consecutive no-improvement iterations.

### pareto

`pareto` is a GEPA-style instance-wise Pareto search that avoids the greedy
single-lineage local optimum:

1. Run baseline train and selection; the baseline seeds the candidate pool.
2. Each iteration samples a parent from the pool with probability
   proportional to the number of train tasks the candidate currently wins
   (its membership count on the instance-wise Pareto frontier).
3. The evolver proposes patches against that parent. The digest additionally
   carries a `pool` summary.
4. Each candidate first runs a cheap train mini-batch cascade gate
   (`minibatch` tasks); candidates that score worse than the parent on the
   same tasks are rejected before full evaluation.
5. Survivors are evaluated on full train and selection with the same guard
   rules as `hillclimb`.
6. A candidate joins the pool if it wins at least one train task or improves
   the parent's train aggregate. The pool keeps frontier members plus the
   current best, capped at `pool` by train aggregate.
7. The final best is the pool candidate with the strongest selection metric.
8. Stop after the iteration limit, budget exhaustion, or three consecutive
   iterations without pool additions.

## Output

```yaml
output:
  dir: ../.jeju-dev/evolve-triage
```

`output.dir` defaults to `.jeju-dev/evolve/<metadata.name>` relative to the evolution manifest.
For source-checkout demos and examples, prefer a repo-root ignored path such as
`.jeju-dev/evolve/<scenario>` so generated experiment artifacts do not accumulate
inside source fixture directories.

Each run creates:

```text
<output.dir>/<experiment_id>/
  experiment.snapshot.json
  events.jsonl
  baseline/
  iterations/
  best/
  leaderboard.json
  report.md
```

Important files:

| File | Description |
| --- | --- |
| `experiment.snapshot.json` | Fully defaulted experiment snapshot. |
| `events.jsonl` | Controller lifecycle events. |
| `baseline/results.json` | Baseline train and selection results. |
| `iterations/<n>/feedback_digest.json` | Digest sent to the evolver. |
| `iterations/<n>/proposals.json` | Parsed proposals from the evolver. |
| `iterations/<n>/candidate-*/patch.json` | Proposal applied to a candidate. |
| `leaderboard.json` | All candidates, results, and rejection reasons. |
| `best/` | Materialized best candidate bundle. |
| `report.md` | Human-readable summary. |

## CLI

```bash
jeju evolve experiments/evolve.yaml
jeju evolve --baseline-only experiments/evolve.yaml
jeju evolve --dry-run experiments/evolve.yaml
jeju evolve --max-iterations 2 experiments/evolve.yaml
jeju evolve --out .jeju-dev/evolve/triage experiments/evolve.yaml
```

Options:

| Option | Description |
| --- | --- |
| `--baseline-only` | Run baseline train/selection and write a report without calling the evolver. |
| `--test` | Run `data.test` after selection on baseline and final best; test metrics do not affect candidate acceptance. |
| `--dry-run` | Validate the experiment and compile the baseline bundle without model calls. |
| `--max-iterations N` | Override `search.iterations`. |
| `--out DIR` | Override `output.dir`. |

## Current Limitations

- `data.test` is opt-in with `--test`; it runs only after candidate selection and does not affect candidate acceptance.
- Non-interactive evolution auto-approves runtime prompts and auto-answers `ask_user` with an empty string. Hard policy denials still block execution.
- Patch operations are exact text replacements, not YAML AST patches.
- Proposal parsing is JSON-based. When the evolver output is not valid
  proposal JSON, the controller retries the evolver once with the parse error
  appended to the digest input, then fails the iteration.
- Search strategies are limited to `hillclimb` and `pareto`; beam or
  successive-halving strategies are not exposed in the manifest.
