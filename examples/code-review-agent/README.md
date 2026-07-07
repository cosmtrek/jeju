# Code Review Agent

This example shows how Jeju turns a narrow developer workflow into a
config-defined, bounded, inspectable agent. The agent discovers current Git
workspace changes through read-only tools, inspects the current repository, and
returns structured findings directly as the final answer.

It demonstrates:

- fixed task shape: review the current workspace changes
- fixed output contract: manifest `output.schema` for JSON review findings
- read-only repository inspection with `git_status`, diffstat/name-only tools,
  path-scoped `git_diff` / `git_diff_cached`, `read`, and `search`
- provider swap point: the manifest's `models.providers.primary` block
- auditable output: `runs/<run_id>/trajectory.jsonl` plus the derived
  `report.html` inspection view

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

The manifest declares the final review shape in `output.schema`. Jeju validates
the final answer locally, retries once if the model returns non-JSON or schema
invalid JSON, and fails the run if the retry still does not match. The prompt
therefore describes workflow and field semantics, while the manifest owns the
machine-readable output contract.

For larger diffs, the agent first inspects status, diffstat, and changed file
lists before deciding whether to continue. It is intended for small and
medium-small reviews, not full audits. If the inventory shows a broad,
unrelated, or full-repository change that cannot be reviewed well with a bounded
pass, it should return an empty findings array and recommend using a broader
review tool or splitting the patch. For in-scope diffs, it loads the full
unstaged and staged diff first, generates internal candidate findings from that
context, then uses targeted `read` or `search` calls only to validate or reject
specific candidates. Any lower-risk clusters it did not inspect are reported as
residual risk in the final JSON.

This is a bounded risk review, not an exhaustive audit. It favors high-signal
diff evidence, small source reads, and early final output over broad repository
exploration. Cross-file issues that require deep caller/callee chains or
lower-risk clusters may be omitted and should be treated as residual risk.

The script runs `jeju run --output final --runs-dir .jeju-dev/runs/code-review`,
so stdout contains only the JSON review after a direct JSON parse. The full
trajectory is still saved under
`.jeju-dev/runs/code-review/<run_id>/trajectory.jsonl`; the final answer is
recorded as a JSON `final` artifact inside that log.

The manifest keeps `workspace.path` pointed at this example's local placeholder
workspace so it can be validated in place. For normal reuse, keep the agent
config anywhere, for example `~/.jeju/ability/code-review/`, and bind it to the
target project at runtime:

```bash
jeju run --output final --runs-dir .jeju-dev/runs/code-review --workspace /path/to/project ~/.jeju/ability/code-review/agents/code-review.agent.yaml "Review the current repository changes."
```

Inspect the recorded run:

```bash
cd /path/to/project
go run /path/to/jeju/cmd/jeju view --runs-dir /path/to/jeju/.jeju-dev/runs/code-review
go run /path/to/jeju/cmd/jeju inspect --runs-dir /path/to/jeju/.jeju-dev/runs/code-review <run_id>
```
