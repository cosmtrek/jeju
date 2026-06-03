# SkillsBench Lite Agent

This example is a compact SkillsBench-style evolution case inspired by
`Harness Updating Is Not Harness Benefit: Disentangling Evolution Capabilities
in Self-Evolving LLM Agents` (`arXiv:2605.30621`).

It focuses on the harness surface that maps cleanly to Jeju: a baseline solver
sees a weak skill directory but does not actively load it. `jeju evolve` can then
improve the agent by editing the system prompt, activating the skill, and
rewriting the skill file. The default solver and evolver model is
`deepseek-v4-flash`.

The local dataset has 28 JSONL tasks across 12 SkillsBench-like task families.
It is intentionally small and deterministic so it can run during Jeju
development without SkillsBench containers.

## Run

From the Jeju checkout:

```bash
export DEEPSEEK_API_KEY=sk-...

go run ./cmd/jeju validate --explain examples/skillsbench-lite-agent/agents/solver.agent.yaml
go run ./cmd/jeju evolve --baseline-only examples/skillsbench-lite-agent/experiments/skillsbench-lite-evolve.yaml
go run ./cmd/jeju evolve --test --max-iterations 2 examples/skillsbench-lite-agent/experiments/skillsbench-lite-evolve.yaml
```

Outputs are written under
`.jeju-dev/evolve/skillsbench-lite/<experiment_id>/`. Inspect
`leaderboard.json`, `report.md`, and `best/` for the accepted candidate and its
patches.

The experiment grants:

- `harness:prompt`
- `skill:skillsbench-lite`

Jeju expands these to the system prompt, `skills.active`, and the
`skillsbench-lite` skill directory. Protected fields such as model credentials,
permissions, workspace, evaluator commands, tools, and skill directory bindings
remain fixed by default.

## Recorded Result

One recorded DeepSeek run:

```text
experiment: 20260603-175417-evolve-skillsbench-lite
best: candidate-001-01

split       candidate          skillsbench_lite_judge  pass_rate
train       baseline           0.0750                  0.0000
train       candidate-001-01   0.9417                  0.9167
selection   baseline           0.0562                  0.0000
selection   candidate-001-01   0.9500                  1.0000
test        baseline           0.0375                  0.0000
test        candidate-001-01   0.9625                  0.8750
```

The accepted candidate changed only the prompt, `skills.active`, and the
`skillsbench-lite` skill file. The held-out test split still catches residual
errors, so this is useful as a reliability check rather than only a happy-path
demo.

## Mechanism Comparison

Compare Jeju's structured patch path with a paper-style direct workspace edit:

```bash
examples/skillsbench-lite-agent/experiments/compare_mechanisms.py \
  --jeju-run-dir .jeju-dev/evolve/skillsbench-lite/20260603-175417-evolve-skillsbench-lite
```

One recorded comparison:

```text
mechanism             train score/pass   selection score/pass   test score/pass
baseline              0.0750 / 0.0000    0.0562 / 0.0000        0.0375 / 0.0000
Jeju structured patch 0.9417 / 0.9167    0.9500 / 1.0000        0.9625 / 0.8750
paper-style direct    0.8250 / 0.7500    0.9250 / 1.0000        0.8250 / 0.7500
```

For this local case, Jeju's structured patch mechanism scored higher on the
held-out test split. The paper-style candidate activated the skill and improved
formatting, but introduced domain labels outside the local judge taxonomy. This
is a case-level mechanism check, not a claim about the full paper benchmark.

## Paper Mapping

- Paper harness component: skills.
- Jeju harness component: `skills.dirs` plus `skills.active`.
- Paper baseline: empty or unused skill harness.
- Jeju baseline: disclosed weak skill directory with no active skill.
- Paper update: evolver creates or updates reusable skills.
- Jeju update: evolver rewrites the reusable skill, activates it, and revises
  prompt policy.
- Paper metric style: pass rate plus activation/adherence analysis.
- Jeju metric style: deterministic pass rate and score from
  `skillsbench_lite_judge`.
