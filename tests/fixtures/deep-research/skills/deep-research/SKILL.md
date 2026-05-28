---
name: deep-research
description: Conduct web-backed research with Exa search and write a Markdown report. Use when a task asks for current research, market/news analysis, or source-grounded synthesis.
metadata:
  jeju.capabilities: web_search,research_synthesis,markdown_report
allowed-tools: search_api write
---

# Deep Research

Use `search_api` to gather current web evidence before writing. Prefer concrete source titles, URLs, highlights, and dates when they appear in the Exa response.

Write a structured Markdown report to `reports/deep-research.md`. Keep unsupported claims out of the report, and include caveats when search coverage is thin.
