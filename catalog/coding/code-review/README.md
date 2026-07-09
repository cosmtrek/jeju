# code-review

Reviews current Git working tree changes with read-only repository inspection
and returns machine-readable code review findings.

## Model And Cost

- Provider preset: DeepSeek
- Model: `deepseek-v4-pro`
- Required environment: `DEEPSEEK_API_KEY`
- Intended use: small and medium-small diff reviews, not exhaustive audits.

## Permissions

- Workspace access: read-only
- Approval: never
- Tools: `read`, `search`, and read-only Git diff/status commands
- The agent does not edit files, stage changes, commit, or use network access.

## Install

```bash
export JEJU_REGISTRY_INDEX=https://jeju.rickoyu.com/registry/index.yaml
jeju package add jeju:coding/code-review@0.1.0
```

## Run

From the repository you want to review:

```bash
jeju run --output final \
  --workspace /path/to/project \
  jeju:coding/code-review@0.1.0 \
  "Review the current repository workspace changes."
```

The repository must already have staged or unstaged changes. The package keeps
`workspace.path` pointed at a placeholder directory for validation; bind the
real target repository at runtime with `--workspace`.

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

## Output Contract

The manifest declares the final review shape in `output.schema`. Jeju validates
the final answer locally, retries once if the model returns non-JSON or schema
invalid JSON, and fails the run if the retry still does not match.

The final answer is one JSON object:

```json
{
  "summary": "The change is small and no correctness issues were found.",
  "findings": [],
  "residual_risk": "Only the current staged and unstaged diff was reviewed."
}
```
