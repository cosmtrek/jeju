# Jeju Examples

This directory contains recommended, runnable Jeju scenarios. Each example is a
self-contained agent bundle. A bundle can be as small as one manifest plus one
prompt, and can add sample inputs, evaluators, tools, or a local workspace when
the scenario needs them.

Examples are different from test fixtures: they show how Jeju can define agent
behavior in config, run it with boundaries, inspect effects, and improve it with
evaluation evidence.

## Available Examples

- [Bug rescue agent](bug-rescue-agent/README.md): repairs a failing Python
  ledger fixture with DeepSeek V4 Flash, reruns tests, and writes a repair note.
- [Code review agent](code-review-agent/README.md): reviews a Git diff with
  read-only Git tools and returns structured findings.
- [Code review with agent tools](code-review-with-agent-tools/README.md): runs
  one parent agent that calls packet-builder, reviewer, and verifier child
  agents through `uses: agent`.
- [Code review team](code-review-team/README.md): runs a lead-worker
  packet-first review with specialist reviewers and a verifier.
- [Commit plan agent](commit-plan-agent/README.md): clusters large Git changes
  into reviewable commit themes before staging or committing.
- [HotpotQA evolve benchmark](hotpotqa-agent/README.md): evolves a multi-hop
  QA solver prompt against official HotpotQA answer EM/F1 and documents the
  study that validated the default `pareto` search strategy.
- [Privacy delegation agent](privacy-delegation-agent/README.md): improves a
  weak privacy-preserving delegation prompt using deterministic leakage
  evidence.
- [SkillsBench Lite agent](skillsbench-lite-agent/README.md): replicates a
  small SkillsBench-style harness activation and adherence experiment.

## Local Artifacts

When running examples from the Jeju source checkout, keep generated artifacts
under repo-root `.jeju-dev/`:

```bash
jeju run --runs-dir .jeju-dev/runs/<scenario> <agent.yaml> "<task>"
jeju view --runs-dir .jeju-dev/runs/<scenario>
jeju evolve --out .jeju-dev/evolve/<scenario> <experiment.yaml>
```

Do not leave example-local `runs/`, example-local `.jeju-dev/`, or repo-root
`runs/` directories in the source checkout unless a task explicitly asks for a
source fixture.
