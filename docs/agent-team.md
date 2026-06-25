# Agent Team

Agent Team lets Jeju run a bounded lead-worker collaboration from one user
goal. A team has one controller-facing lead agent and a declared catalog of
worker agents. The lead plans work across rounds, workers execute isolated
normal Jeju agent runs, and the deterministic team controller validates,
schedules, records, and finishes the run.

This document describes the AgentTeam mechanism. The manifest loading,
validation, and child-agent compilation path stays aligned with normal agents:

```text
config.LoadFile -> config.Validate -> compiler.Compile -> runtime
```

Worker child runs use the normal runtime entrypoint. Lead child runs use the
same runtime with controller-managed message history so later lead turns build
on earlier lead decisions.

The only supported team kind and topology are:

```yaml
apiVersion: jeju/v1alpha1
kind: AgentTeam

runtime:
  topology: lead_worker
```

The lead and workers do not chat with each other directly. All communication
flows through controller-maintained task state.

## When To Use It

Agent Team is useful when a task needs several bounded specialist perspectives
but should remain inspectable:

- code review with separate safety, tests, runtime, and static-analysis
  perspectives,
- research comparison across multiple source or framework dimensions,
- debugging with competing hypotheses and a lead that narrows follow-up work,
- implementation planning where repository inspection, risk review, and test
  planning are separate subtasks.

Use a normal `kind: Agent` when one agent can complete the task without
specialist delegation.

## Manifest Shape

A team manifest describes coordination. The lead and every worker still point
to ordinary `kind: Agent` manifests.

```yaml
apiVersion: jeju/v1alpha1
kind: AgentTeam

metadata:
  name: code-review-team
  description: "Lead-worker team for local code review."

lead:
  agent: ../agents/review-lead.agent.yaml
  description: "Plan review tasks, resolve conflicts, and decide when to finish."

workers:
  safety:
    agent: ../agents/safety-review.agent.yaml
    description: "Review permission, sandbox, and path risks."
    maxTasks: 2

  tests:
    agent: ../agents/tests-review.agent.yaml
    description: "Review tests, fixtures, and docs coverage."
    maxTasks: 2

  verifier:
    agent: ../agents/verifier.agent.yaml
    description: "Check worker findings before final output."
    maxTasks: 2

  writer:
    agent: ../agents/final-writer.agent.yaml
    description: "Write final reports from verified task outputs."
    maxTasks: 1

runtime:
  topology: lead_worker
  maxRounds: 4
  maxTasks: 8
  maxParallel: 2
  maxRetriesPerTask: 1
  maxConsecutiveEmptyRounds: 1

verification:
  requireStructuredTaskOutput: false
  requireVerifier: true
  requiredTaskFields:
    - summary
    - findings
    - evidence
    - gaps
    - residual_risk

output:
  dir: ../.jeju-dev/team/code-review-team
```

Relative paths are resolved from the team manifest file.

There is no special `lead.synthesisAgent`. If a team needs a dedicated final
writer or synthesizer, declare it as a normal worker and let the lead create a
task for it. The final answer is selected explicitly with `finish.content` or
`finish.task_id`.

## Running A Team

Run a team with a manifest and a goal:

```bash
jeju team run teams/code-review.team.yaml "Review the current repository changes."
```

Use `--workspace` to bind all lead and worker child runs to a target project:

```bash
jeju team run \
  --workspace /path/to/project \
  --output final \
  teams/code-review.team.yaml \
  "Review the current working tree changes."
```

Console output mode defaults to `live`. `--output final` prints only the final
team answer.

## Execution Model

Agent Team is an outer controller around normal Jeju runs. For each worker
child run, the team controller compiles the referenced agent manifest and calls
the normal runtime. Tool calls, policy gates, workspace restrictions,
trajectories, compression, evaluations, and artifacts remain owned by each
child run.

The lead is also a normal agent, but it has an AgentTeam-specific control
contract. A lead agent is not an arbitrary worker. It is a controller-facing
planner that must return a structured team decision.

High-level flow:

```text
team manifest + user goal
  -> compile lead and worker agents
  -> build round 1 lead input from canonical team state
  -> parse and validate lead decision
  -> accept new tasks, ignoring repeated immutable task ids
  -> dispatch ready workers with maxParallel
  -> verify task output when verification is enabled
  -> append each accepted/rejected result to canonical team state
  -> append a compact controller state-update user message to the lead history
  -> repeat until finish, abort, or limits reached
  -> resolve final answer from finish.content or finish.task_id
  -> write a standard trajectory and derived report
```

The controller is deterministic. It does not call a hidden controller model to
interpret natural language plans. The lead makes control decisions, and the
controller validates and executes them.

## Lead Context

The controller keeps one lead message history across rounds. The lead system
prompt has two sections:

```text
# Jeju AgentTeam Protocol
TeamDecision schema, controller rules, task semantics, finish and abort rules.

# Lead Agent System Prompt
The instructions.system content from the lead agent manifest.
```

Round 1 appends a bootstrap user message with the team goal, runtime limits,
worker catalog, initial state, and a short response reminder. Later rounds
append compact state-update user messages with controller events, hard
directives, changed tasks, finish blockers, and worker budget. The lead's
previous TeamDecision JSON remains in the assistant message history.

The canonical state remains the controller's task table and trajectory, not the
lead's memory. If message history conflicts with a controller update, the lead
must trust the controller update.

Lead input contains:

- round 1 bootstrap: team goal, runtime limits, verifier requirement, declared
  worker catalog, worker budgets, and an authoritative snapshot,
- normal rounds: controller events since the previous turn, controller
  directives, changed task states, finish blockers, and worker budgets,
- repair or periodic rounds: the normal delta plus a compact authoritative
  snapshot.

Lead input should not include full child trajectories by default. Large worker
outputs should be summarized, truncated, or referenced. Workers can receive
fuller selected outputs through `context_refs`.

## Lead Decision

Every lead response is one JSON object. The top-level decision is a tagged
union:

```json
{
  "decision": "continue",
  "round_summary": "Tests and safety findings are verified; ask writer for the final report.",
  "tasks": [
    {
      "id": "final-report",
      "worker": "writer",
      "objective": "Write the final report from verified safety and tests findings.",
      "depends_on": ["safety-review", "tests-review", "verification-check"],
      "context_refs": ["task:safety-review", "task:tests-review", "task:verification-check"],
      "output_contract": {
        "format": "markdown"
      }
    }
  ],
  "finish": null,
  "abort": null
}
```

Supported decisions:

| Decision | Meaning |
| --- | --- |
| `continue` | Add new worker tasks, or intentionally produce an empty planning round. |
| `finish` | Stop planning and resolve the team final answer from `finish`. |
| `abort` | Stop because the lead determines the goal cannot be completed. |

`round_summary` is a short audit/status summary. It stays in the lead's
TeamDecision assistant message history and is recorded in trajectory artifacts,
but it does not control scheduling.

### Continue

For `decision: "continue"`, the controller reads `tasks`.

```json
{
  "decision": "continue",
  "round_summary": "Initial review needs safety and tests coverage.",
  "tasks": [
    {
      "id": "safety-review",
      "worker": "safety",
      "objective": "Review changed files for permission and sandbox risks.",
      "depends_on": [],
      "context_refs": [],
      "output_contract": {
        "format": "json",
        "required_fields": ["summary", "findings", "evidence", "gaps", "residual_risk"]
      }
    }
  ],
  "finish": null,
  "abort": null
}
```

Task rules:

- `id` is required and must be stable.
- `worker` must name a declared worker.
- `objective` is required.
- `depends_on` is the scheduling edge. Every dependency must refer to an
  existing visible task id, and a task becomes ready only after all dependencies
  are verified.
- `context_refs` is the data edge. It controls which task outputs are injected
  into the worker prompt.
- If `context_refs` is omitted, it defaults to `depends_on`. If it is an
  explicit empty array, no task output is injected.
- `output_contract` is optional. Use it only when the task output must be
  machine-checked or consumed by another step.
- Task ids are immutable. If the lead repeats an existing id, the controller
  ignores that task spec instead of updating prior worker, objective,
  dependencies, or output contract.

`continue` may contain an empty task list. Empty rounds count toward
`runtime.maxConsecutiveEmptyRounds`.

### Finish

For `decision: "finish"`, the controller reads `finish`. Exactly one of
`content` or `task_id` must be set.

```json
{
  "decision": "finish",
  "round_summary": "The writer task is verified and ready to become the final answer.",
  "tasks": [],
  "finish": {
    "task_id": "final-report"
  },
  "abort": null
}
```

Or:

```json
{
  "decision": "finish",
  "round_summary": "All required checks are complete.",
  "tasks": [],
  "finish": {
    "content": "Final answer text."
  },
  "abort": null
}
```

Finish rules:

- `finish.content` uses the lead's content as the team final answer.
- `finish.task_id` uses the final output of a verified task as the team final
  answer.
- `finish.task_id` must reference a visible task with status `verified`.
- `finish.content` and `finish.task_id` are mutually exclusive.
- If `verification.requireVerifier` is true, the controller rejects `finish`
  until at least one task from the `verifier` worker is verified.
- If unresolved planned, ready, running, or retry-scheduled tasks remain, the
  controller rejects `finish`.

To synthesize multiple worker results, the lead should create a normal worker
task such as `final-report` that depends on and references the inputs to
synthesize, then finish with `finish.task_id`.

### Abort

For `decision: "abort"`, the controller reads `abort.reason` and marks the team
run failed.

```json
{
  "decision": "abort",
  "round_summary": "The requested review needs a worker that is not declared.",
  "tasks": [],
  "finish": null,
  "abort": {
    "reason": "No declared worker can inspect the required external service."
  }
}
```

`abort.reason` is required. Use `abort` when the goal cannot be completed with
the declared workers, available tools, policy, or workspace.

## Worker Tasks

Workers are ordinary Jeju agents. A worker is not required to return JSON unless
the team or task explicitly asks for structured output.

Worker input contains:

- the team goal,
- the assigned task objective,
- dependency and context information,
- selected task outputs from `context_refs`,
- any task-level `output_contract`,
- run boundaries.

Task-level `output_contract` overrides team-level defaults for that task. JSON
field verification should only apply when the active contract format is `json`.
Text or Markdown outputs are valid for tasks such as final writing.

## Rounds And Task State

A round is one lead decision followed by controller validation and dispatch of
ready worker tasks. The lead can create follow-up tasks after seeing prior task
summaries and controller results. `runtime.maxRounds` is the hard cap.

Tasks move through these states:

| Status | Meaning |
| --- | --- |
| `planned` | Accepted from the lead but not ready to run. |
| `ready` | Dependencies are verified. |
| `running` | A worker child run is active. |
| `completed` | The worker returned a final answer. |
| `verified` | The task output passed verification. |
| `rejected` | Verification failed and no retry remains. |
| `retry_scheduled` | The task can be retried. |
| `blocked` | The task cannot proceed. |
| `skipped` | The controller skipped the task. |

Worker dispatch is bounded by `runtime.maxParallel`. Failed or invalid task
outputs can be retried up to `runtime.maxRetriesPerTask`.

## Verification

Verification is configured in the team manifest:

```yaml
verification:
  requireStructuredTaskOutput: true
  requireVerifier: true
  requiredTaskFields:
    - summary
    - findings
    - evidence
    - gaps
    - residual_risk
```

`requireStructuredTaskOutput` asks the controller to verify worker task output
against the active output contract. When the active contract format is `json`,
the controller checks that the worker final is parseable JSON and contains the
required fields. When the active contract format is `text` or `markdown`, the
controller should check completion and non-empty output without JSON field
validation.

When `requireVerifier` is true, `finish` is blocked until at least one task from
the `verifier` worker is verified. The verifier is not special runtime code. It
is a normal declared worker whose prompt and tools define how findings should
be checked.

## Output Files

Each team run creates a run directory under `output.dir`:

```text
.jeju-dev/team/<team_name>/<team_run_id>/
  trajectory.jsonl
  report.html
  child-runs/
    lead-round-001/...
    lead-round-002/...
    task-<task_id>/...
```

`trajectory.jsonl` is the canonical team run record. Team runs use the same
Jeju trajectory envelope as normal agent runs:

- `trajectory.header` declares `agent.kind: agent_team`, the team name, goal,
  topology, limits, and worker catalog.
- Each team round is a `span` with `kind: step`.
- Lead decisions and worker tasks are `span` events with `kind: subagent`. They
  reference child run IDs and relative child run paths instead of copying child
  trajectories into the parent log.
- Task lifecycle changes are `action.created` events with `kind:
  orchestration` and an operation such as `task.created`, `task.completed`,
  `task.verified`, `task.rejected`, or `task.blocked`.
- Controller validation results, ignored repeated task ids, rejected finish
  decisions, abort reasons, final answer, and projected team summary are
  trajectory artifacts or actions.
- `run.summary` closes the parent team run and points at the final and summary
  artifacts.

`report.html` is a derived inspection view for humans. Tasks and child runs
render as expandable cards: collapsed rows show the status overview, and
expanding a card reveals the task objective, dependencies, output contract,
verification result, error, and final output, or the child run agent, per-run
stats, and task link. Child runs still keep their own `trajectory.jsonl` as
canonical evidence and are linked from the parent report.

## Runtime Boundaries

Agent Team intentionally stays small:

- no peer-to-peer worker chat,
- no dynamic undeclared worker creation,
- no hidden controller model,
- no implicit synthesis agent,
- no shared mutable long-term memory,
- no distributed workers,
- no Docker or remote sandbox,
- no MCP client behavior added by the team controller.

Agent behavior should come from the lead and worker manifests, prompts, tools,
skills, policy, workspace, trajectory, and evaluator configuration rather than
hardcoded team-controller branches.

## Fixture

The repository includes a mock-provider fixture at:

```text
tests/fixtures/agent-team-deep-research/
```

The fixture validates that a persistent lead session can create initial worker
tasks, create a verifier follow-up task in a later round, create an ordinary
writer task for the final answer, finish with `finish.task_id`, and produce
team summary/report artifacts without calling a real model provider.
