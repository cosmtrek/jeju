# Jeju Examples

This directory contains recommended, runnable Jeju scenarios. Each example is a
self-contained agent bundle. A bundle can be as small as one manifest plus one
prompt, and can add sample inputs, evaluators, tools, or a local workspace when
the scenario needs them.

Examples are different from test fixtures: they show how Jeju can define agent
behavior in config, run it with boundaries, inspect effects, and improve it with
evaluation evidence.

## Available Examples

- [Code review agent](code-review-agent/README.md): reviews a Git diff and
  returns structured findings checked by a local evaluator.
- [Privacy delegation agent](privacy-delegation-agent/README.md): improves a
  weak privacy-preserving delegation prompt using deterministic leakage
  evidence.

## Local Artifacts

When running examples from the Jeju source checkout, keep generated artifacts
under repo-root `.jeju-dev/`:

```bash
jeju run --runs-dir .jeju-dev/runs/<scenario> <agent.yaml> "<task>"
jeju evolve --out .jeju-dev/evolve/<scenario> <experiment.yaml>
```

Do not leave example-local `runs/`, example-local `.jeju-dev/`, or repo-root
`runs/` directories in the source checkout unless a task explicitly asks for a
source fixture.
