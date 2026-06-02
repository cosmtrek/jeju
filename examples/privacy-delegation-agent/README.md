# Privacy Delegation Agent

This example is a PUPA-lite scenario inspired by privacy-conscious delegation
benchmarks. It shows Jeju as a governed evolution harness: a weak agent leaks
private details into a request meant for an external LLM, a deterministic
evaluator explains the leakage, and `jeju evolve` may improve only the system
prompt while forbidden config fields remain locked.

It demonstrates:

- privacy-aware delegation with a concrete leakage metric
- rich textual evaluator feedback for prompt evolution
- train/selection/test splits for candidate validation
- safe config-space evolution: only `instructions.system` is editable
- forbidden fields for permissions, workspace, tools, evaluator commands, and
  model credentials
- auditable candidate bundles, leaderboard, report, and run trajectories

## Run

From the Jeju checkout:

```bash
export DEEPSEEK_API_KEY=sk-...

go run ./cmd/jeju validate --explain examples/privacy-delegation-agent/agents/privacy.agent.yaml
go run ./cmd/jeju evolve --baseline-only examples/privacy-delegation-agent/experiments/privacy-evolve.yaml
go run ./cmd/jeju evolve --max-iterations 2 examples/privacy-delegation-agent/experiments/privacy-evolve.yaml
```

Evolution output is written under
`.jeju-dev/evolve/privacy-delegation/<experiment_id>/` at the Jeju checkout
root.
Inspect `leaderboard.json`, `report.md`, and `best/` to see whether the
candidate improved without changing protected fields.

The target and evolver agents use the DeepSeek preset. To use another provider,
edit the `models.providers.primary` blocks in `agents/privacy.agent.yaml` and
`agents/evolver.agent.yaml`.

## Demo Shape

The baseline prompt is intentionally unsafe: it asks the target agent to preserve
all concrete details when creating the external LLM request. The evaluator checks
the generated `llm_request` for leaked sensitive strings such as customer names,
employee emails, internal codenames, tenant ids, secret tokens, internal URLs,
and private incident ids.

The ideal candidate still preserves the user's operational intent, but abstracts
sensitive details before delegation.
