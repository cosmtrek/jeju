---
name: deep-research
description: Conduct web-backed research with Exa search and write a Markdown report. Use when a task asks for current research, market/news analysis, or source-grounded synthesis.
metadata:
  jeju.capabilities: web_search,research_synthesis,markdown_report
allowed-tools: search_api write
---

# Deep Research

## Workflow

1. Call `search_api` at least once before writing the report.
2. Use the search results to identify concrete findings, evidence, and gaps.
3. Write the final Markdown report to `reports/deep-research.md` with the `write` tool.
4. After the write succeeds, return a concise final answer that names the report path.

## Evidence Handling

Use `search_api` to gather current web evidence before writing. Prefer concrete source titles, URLs, highlights, and dates when they appear in the Exa response.

Use 1-3 targeted searches for normal tasks. Do not exceed 5 searches unless the user explicitly asks for broad coverage or the earlier results are unusable.

Do not invent citations. If search results are weak or incomplete, say so in the report.

## Report Requirements

- Title and short executive summary.
- Key findings with supporting source names or URLs when available from search results.
- Open questions or caveats.
- Clear conclusion.

Keep unsupported claims out of the report, and include caveats when search coverage is thin.
