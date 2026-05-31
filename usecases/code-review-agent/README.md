# Code Review Agent

This use case shows Jeju as a repeatable personal/domain agent. The agent
discovers current Git workspace changes through read-only tools, inspects the
current repository, and returns structured findings directly as the final
answer.

It demonstrates:

- fixed task shape: review the current workspace changes
- fixed output contract: JSON review findings
- read-only repository inspection with `git_status`, `git_diff`,
  `git_diff_cached`, `read`, and `search`
- provider swap point: the manifest's `models.providers.primary` block
- auditable output: `runs/<run_id>/metadata.json`, `trajectory.jsonl`,
  `final.md`, and `report.html`

## Run

From the repository you want to review:

```bash
export DEEPSEEK_API_KEY=sk-...

cd /path/to/jeju
./scripts/run-code-review-agent.sh /path/to/project
```

From the Jeju checkout, omitting the argument reviews the current directory:

```bash
./scripts/run-code-review-agent.sh
```

The default model is `deepseek-v4-pro`. To use another provider, edit the
manifest's `models.providers.primary` block.

The JSON review is printed directly by `jeju run` and is also saved to
`runs/<run_id>/final.md`.

The manifest keeps `workspace.path` pointed at this use case's local placeholder
workspace so it can be validated in place. For normal reuse, keep the agent
config anywhere, for example `~/.jeju/ability/code-review/`, and bind it to the
target project at runtime:

```bash
jeju run --workspace /path/to/project ~/.jeju/ability/code-review/agents/code-review.agent.yaml "Review the current repository changes."
```

Inspect the recorded run:

```bash
cd /path/to/project
go run /path/to/jeju/cmd/jeju runs
go run /path/to/jeju/cmd/jeju inspect <run_id>
```
