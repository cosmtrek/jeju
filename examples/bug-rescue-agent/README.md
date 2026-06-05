# Bug Rescue Agent

This showcase example gives Jeju a small broken Python project and asks a
DeepSeek V4 Flash agent to repair it.

It demonstrates a full bounded workflow:

- inspect a local codebase
- run a deterministic test harness
- make a minimal source edit
- rerun tests
- write a short repair note
- inspect the complete trajectory and HTML report

The committed fixture is never edited directly. The run script copies it to
`.jeju-dev/workspaces/bug-rescue` and binds the agent to that temporary
workspace.

## Run

Set a DeepSeek API key:

```bash
export DEEPSEEK_API_KEY=sk-...
```

From the Jeju checkout:

```bash
./scripts/run-bug-rescue-agent.sh
```

The script builds the local Jeju binary, copies the broken ledger fixture, shows
the initial failing test result, validates the agent, and runs:

```bash
jeju run --runs-dir .jeju-dev/runs/bug-rescue --workspace .jeju-dev/workspaces/bug-rescue ...
```

After the run, inspect the report:

```bash
jeju view --runs-dir .jeju-dev/runs/bug-rescue <run_id>
```

## Expected Result

The agent should fix the ledger rounding bug, make the tests pass, and write
`REPAIR.md` in the copied workspace. The run evidence is saved under
`.jeju-dev/runs/bug-rescue/<run_id>/trajectory.jsonl` with a derived
`report.html`.
