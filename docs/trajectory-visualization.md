# Trajectory Visualization

Jeju records every meaningful runtime effect in `trajectory.jsonl` and projects
that log into human-readable inspection surfaces.

Use `jeju inspect` when you need a compact terminal summary:

```bash
jeju inspect <run_id>
```

Use `jeju view` to browse run history or open an HTML report:

```bash
jeju view
jeju view <package-ref>
jeju view <run_id>
```

With no selector, `jeju view` lists runs from the configured stores. With a
package selector, it lists runs for that package. With a run ID, it opens the
existing report when it is fresh, and regenerates it first when the report is
missing or older than `trajectory.jsonl`. The report is derived from the
trajectory, not a separate source of truth. It is intended for review,
debugging, demos, and sharing run evidence with another developer or a
higher-level agent.

![Jeju trajectory visualization](trajectory-visualization.png)

The screenshot above is from the
[bug rescue agent](../examples/bug-rescue-agent/README.md), which repairs a
failing local fixture with DeepSeek V4 Flash and records the full read, test,
edit, retest, and write sequence.

The report highlights:

- Run identity, model, loaded skills, and trajectory integrity.
- The original task and final output.
- Step-by-step process events, including model actions and tool calls.
- Tool distribution, artifacts, token counts, and duration.
- Evaluation status, score, and evaluator details when evaluation is enabled.

The canonical evidence remains the run directory:

```text
runs/<run_id>/
  trajectory.jsonl     # canonical append-only run record
  report.html          # derived inspection view
```

Metadata, config snapshots, model inputs and outputs, tool outputs, final
answers, evaluation results, and generated file snapshots are stored as typed
events or inline/chunked artifacts inside `trajectory.jsonl`.

For source-checkout demos, keep generated runs under `.jeju-dev/`:

```bash
jeju run --runs-dir .jeju-dev/runs/<scenario> <agent.yaml> "<task>"
jeju inspect --runs-dir .jeju-dev/runs/<scenario> <run_id>
jeju view --runs-dir .jeju-dev/runs/<scenario> <run_id>
```
