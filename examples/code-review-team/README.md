# Code Review Team

This is a reusable `kind: AgentTeam` bundle for reviewing a repository's
current Git working tree changes. It uses one lead and three workers: a packet
builder, a generalist reviewer that the lead dispatches 1-3 times with
different focuses, and a judge verifier. The final answer is a synthesized
Markdown code review with verified findings first.

The design follows the converged shape of current AI code review systems:
a small pool of generalist finders with dynamically assigned focuses, one
strong judge that deduplicates and confidence-gates candidate findings, and
hard output caps with an explicit "clean diff" outcome.

## Requirements

- Jeju with `team run` support.
- Python 3 for the bundled packet tool.
- A Git repository as the target workspace.
- `DEEPSEEK_API_KEY` set, unless you edit the manifests to use another
  OpenAI-compatible provider. The lead, reviewer, verifier, and synthesis
  agents default to `deepseek-v4-pro`; the packet builder uses
  `deepseek-v4-flash`.

This team may call the configured model many times. It is intended for
substantive code review, not as a cheap pre-commit hook.

## Execution Model

1. Round 1: the lead creates one `build-review-packets` task. The
   `packet_builder` worker runs the bundled `tools/cr-packet.py build`
   command, which writes a single evidence packet under
   `.jeju-dev/code-review-team-packets/<run_id>/` in the target workspace and
   reports the `run_id`, changed files, extension histogram, scope flags
   (binary files, very large changes, generated content, truncation), and
   which deterministic checks are available. Heavy checks do not run at build
   time.
2. Round 2: the lead reads that report and chooses 1-3 review focuses for
   this specific diff — correctness and runtime behavior, security and data
   flow, tests and change completeness, conventions — sizing the plan to the
   diff (one focus for a docs-only change, three for a large risky change).
   It creates one `reviewer` task per focus with a tailored objective, plus
   one `verifier` task depending on all of them, in the same round.
3. Each reviewer task loads the packet once, reviews the diff through its
   assigned focus, and may call `expand_evidence` (bounded file excerpts, at
   most 5 calls) to chase context beyond the packet — callers, full function
   bodies, related files. A correctness-focused task may also run the
   detected deterministic checks (`go test`, `go vet`, `npm test`, `pytest`)
   through `run_static_check` and must attribute failures to the diff. The
   worker limit leaves headroom for the final answer after those tool calls.
   Reviewer prompts carry an explicit false-positive doctrine: no
   pre-existing issues, no linter-catchable issues, no nitpicks, no
   speculation without evidence.
4. The verifier is the judge: it merges and deduplicates candidates across
   reviewer tasks (independent agreement raises confidence), re-fetches cited
   evidence and expanded context, scores every candidate on a 0-100 rubric,
   drops everything below 80, and caps output at 8 findings. Zero verified
   findings is a valid result.
5. The lead synthesizes when the verifier output is clean. If the verifier
   marked a P0/P1 finding `uncertain`, the lead may run one escalation round:
   a follow-up reviewer task gathers the missing evidence, and a second
   verifier task re-checks. At most one escalation round.
6. The synthesis agent writes the final Markdown answer from verified team
   state.

## Run From This Repository

From the Jeju checkout:

```bash
export DEEPSEEK_API_KEY=sk-...

go run ./cmd/jeju team run \
  --workspace /path/to/project \
  --output final \
  examples/code-review-team/teams/code-review.team.yaml \
  "Review the current working tree changes. Focus on actionable correctness, safety, tests, and reviewability issues."
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
`.jeju-dev/code-review-team-packets/current.json` pointer records the most
recent packet run for manual inspection.

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
reviewed. `--out` keeps team-level artifacts in the reviewed project.

## Output

The final answer is Markdown. It should include:

- verified findings first, ordered by severity, each with severity, file,
  line, impact, evidence, confidence score, and a concrete fix,
- an explicit statement when the diff is clean,
- rejected or downgraded findings with the verifier's reason,
- a coverage section: dispatched and skipped focuses, scope flags, checks
  status, failed tasks,
- residual risks and test gaps.

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

- Reviewer and verifier agents have no generic repository read/search tools.
  They see the packet plus bounded `expand_evidence` excerpts (workspace
  paths only, line-capped, binary-rejected); all agents except the packet
  builder are read-only.
- Findings must cite packet evidence ids. Context fetched through
  `expand_evidence` is recorded as `{path, start, end}` ranges so the
  verifier can re-fetch and check the same content. Unsupported suspicions
  belong in `gaps` or `residual_risk`.
- The packet build is fast and deterministic: diff hunks, file inventory,
  scope flags, and `git diff --check` only. Heavy checks run on demand inside
  a reviewer task via `run_static_check`, and only when the lead asks for
  them.
- The verifier gates on content, not metadata: candidates scoring below 80 on
  the confidence rubric are dropped, output is capped at 8 findings, and an
  empty findings list is a legitimate outcome.
- Worker names (`packet_builder`, `reviewer`, `verifier`) are referenced by
  the lead prompt; keep the team manifest and `prompts/review-lead.md` in
  sync if you rename them.

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
examples/code-review-team/tools/cr-packet.py packet --run-id smoke | head -c 400
examples/code-review-team/tools/cr-packet.py evidence-index --run-id smoke
examples/code-review-team/tools/cr-packet.py evidence --run-id smoke --id scope.flags
examples/code-review-team/tools/cr-packet.py expand --path README.md --start 1 --end 20
examples/code-review-team/tools/cr-packet.py check --name go_vet
```

## What This Shows

- dynamic lead-worker planning with `runtime.topology: lead_worker`, where
  the lead sizes the review to the diff instead of running a fixed pipeline,
- one generalist worker type dispatched multiple times with different
  focuses, instead of one worker type per review dimension,
- packet-first evidence collection plus bounded agentic retrieval
  (`expand_evidence`) implemented as normal worker tools,
- a judge-style verifier with cross-reviewer dedup, a 0-100 confidence
  rubric, a hard threshold, and capped output,
- child run trajectories and a team-level `team.summary.json` / `report.html`.
