# Code Review Team

This is a reusable `kind: AgentTeam` bundle for reviewing a repository's
current Git working tree changes. It uses one lead, a packet-builder worker,
five specialist reviewers, and a verifier. The final answer is a synthesized
Markdown code review with findings first.

Use this team when a single reviewer agent is too shallow and you want separate
coverage for diff scope, runtime correctness, safety/policy, tests/docs, and
static command results.

## Requirements

- Jeju with `team run` support.
- Python 3 for the bundled packet tool.
- A Git repository as the target workspace.
- `DEEPSEEK_API_KEY` set, unless you edit the manifests to use another
  OpenAI-compatible provider.

This team may call the configured model many times. It is intended for
substantive code review, not as a cheap pre-commit hook.

Execution model:

1. `packet_builder` runs the bundled `tools/cr-packet.py` command, creates a
   unique packet `run_id`, and writes packet artifacts under
   `.jeju-dev/code-review-team-packets/<run_id>` in the target workspace.
2. Specialist reviewers read that `run_id` from the packet-builder task context
   and call only their fixed `get_review_packet` tool.
3. The verifier receives reviewer outputs through `context_refs`, lists compact
   packet evidence ids, then fetches targeted evidence bodies for high-risk
   findings before marking them verified, rejected, downgraded, or uncertain.
4. The synthesis agent writes the final Markdown answer from verified team
   state.

## Run From This Repository

From the Jeju checkout:

```bash
export DEEPSEEK_API_KEY=sk-...

go run ./cmd/jeju team run \
  --workspace /path/to/project \
  --output final \
  examples/code-review-team/teams/code-review.team.yaml \
  "Review the current working tree changes. Focus on actionable correctness, safety, tests, docs, and reviewability issues."
```

When reviewing the Jeju checkout itself, use:

```bash
go run ./cmd/jeju team run \
  --workspace . \
  --output final \
  examples/code-review-team/teams/code-review.team.yaml \
  "Review the current working tree changes."
```

Team artifacts are written under repo-root `.jeju-dev/team/code-review-team/`.
Packet artifacts are written under the target workspace's
`.jeju-dev/code-review-team-packets/<run_id>/`; a
`.jeju-dev/code-review-team-packets/current.json` pointer records the most recent
packet run for manual inspection.

## Install As A Reusable Local Team

Copy the bundle somewhere stable, then run it against any local project:

```bash
mkdir -p ~/jeju-agents
cp -R examples/code-review-team ~/jeju-agents/code-review-team

cd /path/to/project
jeju team run \
  --workspace . \
  --out .jeju-dev/team/code-review-team \
  --output final \
  ~/jeju-agents/code-review-team/teams/code-review.team.yaml \
  "Review the current working tree changes."
```

`--workspace` binds every lead and worker child run to the project being
reviewed. `--out` keeps team-level artifacts in the reviewed project. Packet
artifacts are written under the reviewed project's
`.jeju-dev/code-review-team-packets/<run_id>/`.

## Output

The final answer is Markdown. It should include:

- actionable findings first, ordered by severity,
- severity, file, line, impact, evidence, confidence, and concrete fix for each
  finding,
- rejected or downgraded findings when the verifier identifies them,
- residual risks, missing dimensions, failed tasks, and test gaps.

Team artifacts include:

```text
.jeju-dev/team/code-review-team/<team_run_id>/
  team.snapshot.yaml
  team.events.jsonl
  team.summary.json
  report.html
  child-runs/
```

## Boundaries

- The lead and reviewer agents do not have generic repository read/search
  tools. Reviewers inspect only the packet returned by their
  `get_review_packet` tool.
- `packet_builder` writes packet JSON files under `.jeju-dev/` in the target
  workspace. The specialist reviewers and verifier are read-only.
- Findings must cite packet evidence ids. Unsupported suspicions belong in
  `gaps` or `residual_risk`.
- The diff-context packet intentionally omits full file excerpts. It focuses on
  changed-file inventory, diffstat, diff hunks, scope, and reviewability.
- The static-analysis packet runs lightweight deterministic commands. For Go
  projects this includes `git diff --check`, `go test ./...`, and
  `go vet ./...`; for Node/Python projects it uses the available package test
  conventions in the packet tool. When commands fail, the packet includes
  changed diff hunks only for files mentioned by the command output so the
  static reviewer can triage whether the failure is attributable to this diff.
- Verifier gating is content-aware: it lists compact evidence ids first, then
  fetches targeted evidence bodies for P0/P1 and suspicious findings. It should
  reject unsupported claims and downgrade overstated severity instead of relying
  only on evidence id existence.

## Validation

Validate the agent manifests from the Jeju checkout:

```bash
for f in examples/code-review-team/agents/*.agent.yaml; do
  go run ./cmd/jeju validate "$f"
done
```

Validate the packet tool:

```bash
python3 -m py_compile examples/code-review-team/tools/cr-packet.py
examples/code-review-team/tools/cr-packet.py build --run-id smoke
examples/code-review-team/tools/cr-packet.py list --run-id smoke
examples/code-review-team/tools/cr-packet.py evidence-index --run-id smoke
examples/code-review-team/tools/cr-packet.py evidence --run-id smoke --dimension diff_context --id diff_context.scope
```

## What This Shows

- dynamic lead-worker planning with `runtime.topology: lead_worker`,
- packet-first evidence collection implemented as a normal worker tool,
- reviewer agents constrained to packet-scoped evidence,
- verifier gating through a declared `verifier` worker with targeted evidence
  body checks,
- child run trajectories and a team-level `team.summary.json` / `report.html`.
