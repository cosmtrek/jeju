# Code Review With Agent Tools

This example is a normal `kind: Agent`, not an `AgentTeam`. It demonstrates
`uses: agent`: the parent review agent calls declared child agents as tools
inside its own runtime loop and consumes each child final answer as a tool
result.

It is intentionally adjacent to
[`examples/code-review-team`](../code-review-team/README.md):

- `code-review-team` uses a bounded lead-worker controller with rounds, task
  state, worker scheduling, verification, and final task selection.
- `code-review-with-agent-tools` uses one parent agent with normal tool calls.
  The only delegation mechanism is `tools[].uses: agent`, and the flow stays
  intentionally small: one packet builder, one reviewer by default, and one
  verifier.

## Shape

```text
code-review-with-agent-tools.agent.yaml  kind=Agent
  build_review_packet  uses=agent -> packet-builder.agent.yaml
  ask_reviewer         uses=agent -> reviewer.agent.yaml
  verify_findings      uses=agent -> verifier.agent.yaml
```

The child agents are ordinary `kind: Agent` manifests. When run directly, they
use their own `workspace.path`. When invoked as agent tools, they inherit the
parent run's effective workspace.

## Requirements

- A Git repository as the target workspace.
- `DEEPSEEK_API_KEY` set, unless you edit the manifests to use another
  OpenAI-compatible provider.
- Python 3 for the bundled packet tool.

## Run

From the Jeju checkout:

```bash
export DEEPSEEK_API_KEY=sk-...

go run ./cmd/jeju run \
  --workspace /path/to/project \
  --runs-dir .jeju-dev/runs/code-review-with-agent-tools \
  --output final \
  examples/code-review-with-agent-tools/agents/code-review-with-agent-tools.agent.yaml \
  "Review the current working tree changes. Focus on actionable correctness, safety, tests, and reviewability issues."
```

When reviewing the Jeju checkout itself:

```bash
go run ./cmd/jeju run \
  --workspace . \
  --runs-dir .jeju-dev/runs/code-review-with-agent-tools \
  --output final \
  examples/code-review-with-agent-tools/agents/code-review-with-agent-tools.agent.yaml \
  "Review the current working tree changes."
```

Inspect the report:

```bash
go run ./cmd/jeju view \
  --runs-dir .jeju-dev/runs/code-review-with-agent-tools \
  <run_id>
```

The parent trajectory contains a normal `tool` span for each agent tool call,
with a nested `subagent` span for the child run. Child trajectories are stored
under:

```text
.jeju-dev/runs/code-review-with-agent-tools/<run_id>/child-runs/
```

Packet artifacts are written in the target workspace under:

```text
.jeju-dev/code-review-agent-tools-packets/
```

## What This Shows

- `uses: agent` is a single-agent delegation surface, not a team topology.
- Parent policy gates delegation through `agentRun`.
- Child agents keep their own manifests, tools, prompts, limits, and policies.
- Child runs inherit the parent workspace when invoked as tools.
- The report can display agent tool calls as tool spans with nested subagent
  run metadata.

Use `AgentTeam` instead when the problem needs explicit task state, rounds,
parallel worker scheduling, verification dependencies, or controller-level
finish selection.
