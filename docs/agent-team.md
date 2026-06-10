# Agent Team

Agent Team lets Jeju run a bounded lead-worker collaboration from one user
goal. A team has one lead agent and a declared catalog of worker agents. The
lead plans tasks across rounds, workers run as isolated normal Jeju agents, and
the team controller records task state, child run references, verification
results, aggregate stats, and a final synthesis.

The first supported team kind is:

```yaml
apiVersion: jeju/v1alpha1
kind: AgentTeam
```

The only supported topology is `lead_worker`: one lead coordinates multiple
workers through task state. Workers do not chat with each other directly.

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
  synthesisAgent: ../agents/review-synthesis.agent.yaml
  description: "Plan review tasks, resolve conflicts, and synthesize findings."

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
    description: "Check worker findings before synthesis."
    maxTasks: 2

runtime:
  topology: lead_worker
  maxRounds: 4
  maxTasks: 8
  maxParallel: 2
  maxRetriesPerTask: 1
  maxConsecutiveEmptyRounds: 1

verification:
  requireStructuredTaskOutput: true
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
team synthesis.

## Execution Model

Agent Team is an outer controller around normal Jeju runs. The core agent path
does not change:

```text
config.LoadFile -> config.Validate -> compiler.Compile -> runtime.Run
```

For each lead or worker child run, the team controller compiles the referenced
agent manifest and calls the normal runtime. Tool calls, policy gates,
workspace restrictions, trajectories, compression, evaluations, and artifacts
remain owned by each child run.

High-level flow:

```text
team manifest + user goal
  -> compile lead and worker agents
  -> round 1 lead decision
  -> validate lead-created tasks
  -> dispatch ready workers with maxParallel
  -> collect child run results
  -> verify task output
  -> repeat until synthesize, blocked, or limits reached
  -> optional synthesis agent produces final answer
  -> write team summary, events, and report
```

## Lead Decisions

The lead is a normal agent. Its tools come from `lead.agent`; the team
controller does not inject planning tools. In every planning round the lead
must return JSON:

```json
{
  "decision": "continue",
  "round_summary": "Initial review needs safety and tests coverage.",
  "tasks": [
    {
      "id": "safety-review",
      "worker": "safety",
      "objective": "Review changed files for permission and sandbox risks.",
      "context_refs": [],
      "depends_on": [],
      "output_contract": {
        "format": "json",
        "required_fields": ["summary", "findings", "evidence", "gaps", "residual_risk"]
      }
    }
  ],
  "finish": null
}
```

Supported decisions:

| Decision | Meaning |
| --- | --- |
| `continue` | Add tasks and dispatch ready work. |
| `synthesize` | Stop planning and produce the final answer if synthesis gates pass. |
| `blocked` | Stop because required information or capability is missing. |

The controller validates that task workers are declared, task IDs are unique,
dependencies exist, worker and team task limits are respected, and output
contracts are supported. Invalid task specs are recorded as rejected task
states and do not fail the whole team run; valid tasks from the same lead
decision can still proceed.

## Rounds And Task State

A round is one lead decision followed by dispatch of ready worker tasks. The
lead can create follow-up tasks after seeing prior task summaries. `maxRounds`
is the hard cap.

Tasks move through these states:

| Status | Meaning |
| --- | --- |
| `planned` | Accepted from the lead but not ready to run. |
| `ready` | Dependencies are satisfied. |
| `running` | A worker child run is active. |
| `completed` | The worker returned a final answer. |
| `verified` | The task output passed verification. |
| `rejected` | Verification failed and no retry remains. |
| `retry_scheduled` | The task can be retried. |
| `blocked` | The task cannot proceed. |
| `skipped` | The controller skipped the task. |

Worker dispatch is bounded by `runtime.maxParallel`. Failed or invalid task
outputs can be retried up to `runtime.maxRetriesPerTask`.

## Context And Communication

The team does not build a shared group chat. Communication is hub-and-spoke:

```text
lead -> controller -> worker
worker -> controller -> lead
```

Lead checkpoint input includes the user goal, worker catalog, runtime limits,
task table, verified outputs, rejected or blocked summaries, and aggregate
stats. It should not include full child trajectories by default.

Worker input includes the assigned objective, output contract, dependency
state, selected context refs, and run boundaries. The worker then uses only its
own agent prompt, tools, workspace, and policy.

This keeps task ownership clear and prevents the lead context from becoming a
concatenation of all worker histories.

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

`requireStructuredTaskOutput` checks that task finals are parseable JSON and
contain required fields. When `requireVerifier` is true, synthesis is blocked
until at least one task from the `verifier` worker is verified.

The verifier is not special runtime code. It is a normal declared worker whose
prompt and tools define how findings should be checked. Teams that enable
`requireVerifier` must declare a worker named `verifier`.

## Output Files

Each team run creates a run directory under `output.dir`:

```text
.jeju-dev/team/<team_name>/<team_run_id>/
  team.snapshot.yaml
  team.events.jsonl
  team.summary.json
  report.html
  child-runs/
    compile-lead/...
    task-<task_id>/...
    lead-synthesis/...
```

`team.events.jsonl` records team-level events such as `team.started`,
`round.started`, `lead.decision`, `task.created`, `task.started`,
`task.completed`, `task.verified`, `task.rejected`, `round.completed`, and
`team.completed`.

`team.summary.json` is the machine-readable team result. It includes:

- team run ID, status, goal, started and ended timestamps,
- round count and runtime limits,
- declared workers,
- task states and verification results,
- child run IDs and child run directories,
- aggregate model, tool, permission, token, and duration stats,
- final answer and report path.

`report.html` is a derived inspection view for humans. Child runs still keep
their own `trajectory.jsonl` as canonical evidence.

## Runtime Boundaries

Agent Team intentionally stays small:

- no peer-to-peer worker chat,
- no dynamic undeclared worker creation,
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

The fixture validates that a lead can create initial worker tasks, create a
verifier follow-up task in a later round, synthesize through a separate
synthesis agent, and produce team summary/report artifacts without calling a
real model provider.
