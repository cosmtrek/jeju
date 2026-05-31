# Evolve Triage Fixture

This fixture validates self-evolution effect, not only loop mechanics.

- Baseline target prompt is intentionally weak and usually produces prose.
- The evaluator is deterministic (`eval/triage_judge.py`) and checks JSON triage fields.
- Train failures are visible to the evolver.
- Selection is used for candidate acceptance.
- `datasets/test.jsonl` is reserved for an external holdout audit of baseline vs best.

Run the real-provider effect check:

```bash
export MIMO_API_KEY=...
./scripts/run-evolve-effect-e2e.sh mimo
```
