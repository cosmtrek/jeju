# Catalog Cold Start Notes

Date: 2026-07-08

This records the Phase 0 cold-start check for the first writing catalog
packages. The check used a clean temporary package store, a copied source
bundle, a small draft workspace, and a real DeepSeek run for
`writing/article-editor`.

## Friction Points

| Observation | New-user interpretation | Repair layer |
|---|---|---|
| Source package README files contained developer-home absolute paths, so `jeju package validate` rejected the copied bundle. | The package looks broken before the user reaches the agent behavior. | Package README. Catalog READMEs now use neutral `/path/to/...` examples and `jeju:` references only. |
| A missing `DEEPSEEK_API_KEY` produced a failed run with a report, but `jeju run` originally returned a successful process status. | Scripts or CI could treat a failed agent run as success. | CLI. `jeju run` now returns an error for failed or cancelled run statuses. |
| `article-editor` sometimes tried to search the workspace even when the draft text was already passed through `--from`. | The worker appears confused about whether it should inspect files or review the provided text. | Package prompt. The input contract now says direct draft text should be reviewed directly unless nearby context is requested. |
| `--output final` kept stdout clean but hid the run id and report path. | The evidence trail is harder to find after downstream polish, rewrite, or translation runs. | CLI. Final-output mode now writes run evidence to stderr while preserving stdout as the final answer. |

## Verified Path

The completed smoke path was:

```bash
jeju package validate catalog/writing/article-editor
jeju package add catalog/writing/article-editor --replace
jeju run --output final \
  --runs-dir .jeju-dev/runs/catalog-smoke \
  --workspace .jeju-dev/catalog-smoke/workspace \
  --from .jeju-dev/catalog-smoke/workspace/draft.md \
  p:writing/article-editor@0.1.0 \
  "Review this draft for publish readiness. Return only the configured JSON."
```

The run completed with structured JSON matching `output.schema`, and the report
was generated under `.jeju-dev/runs/catalog-smoke/`.
