# Commit Plan Agent

This example defines a read-only Jeju agent that clusters large Git worktree
changes into coherent commit themes.

It is intended to run before code review or committing. The agent does not stage
files, create commits, edit files, or judge code correctness. It returns a
strict JSON plan that a supervising agent or human can verify.

It demonstrates:

- large-diff inventory with `git_status`, diffstat, name-only, and untracked
  file tools
- path-scoped diff inspection only when a commit boundary is unclear
- commit themes organized by intent rather than raw directory grouping
- Conventional Commits message suggestions
- explicit outlier and residual-risk reporting

## Run

Set a DeepSeek API key:

```bash
export DEEPSEEK_API_KEY=sk-...
```

From the Jeju checkout, pass the repository you want to plan. Omitting the
argument uses the current directory:

```bash
./scripts/run-commit-plan-agent.sh /path/to/project
```

The default model is `deepseek-v4-flash`. To use another provider, edit the
manifest's `models.providers.primary` block.

The script builds the local Jeju binary and runs:

```bash
jeju run --output final --runs-dir .jeju-dev/runs/commit-plan ...
```

stdout contains only the JSON commit plan. The full trajectory is saved under
`.jeju-dev/runs/commit-plan/<run_id>/trajectory.jsonl`.

## Output Contract

The final answer is one JSON object with:

- `summary`: overall worktree theme
- `scale`: changed/staged/unstaged/untracked file counts and large-change flag
- `commit_plan`: ordered commit groups with message, files, rationale, and
  verification suggestions
- `outliers`: files that do not fit the main plan
- `needs_decision`: boundaries that require human intent
- `residual_risk`: what the agent did not inspect deeply

Use this as a planning aid. Apply staging manually after verifying the file
lists.
