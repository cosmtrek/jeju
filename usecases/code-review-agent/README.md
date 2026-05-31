# Code Review Agent

This use case shows Jeju as a repeatable personal/domain agent. The agent reviews
a Git diff, returns structured findings, and runs a local evaluator that checks
whether the review is machine-readable and catches the high-risk issue in the
sample diff.

It demonstrates:

- fixed input shape: a Git diff
- fixed output contract: JSON review findings
- local evaluation: `eval/review_contract.py`
- provider swap point: the manifest's `models.providers.primary` block
- auditable output: `runs/<run_id>/metadata.json`, `trajectory.jsonl`,
  `final.md`, `evaluation.json`, and `report.html`

## Run

From the repository root:

```bash
export DEEPSEEK_API_KEY=sk-...

go run ./cmd/jeju validate usecases/code-review-agent/agents/code-review.agent.yaml

cd usecases/code-review-agent
go run /Users/bytedance/Developer/jeju/cmd/jeju run agents/code-review.agent.yaml "$(cat samples/session-cache.diff)"
```

Inspect the recorded run:

```bash
go run /Users/bytedance/Developer/jeju/cmd/jeju runs
go run /Users/bytedance/Developer/jeju/cmd/jeju inspect <run_id>
```

The local evaluator should pass when the review catches the plaintext-token cache
key regression and the TTL regression in `samples/session-cache.diff`.

