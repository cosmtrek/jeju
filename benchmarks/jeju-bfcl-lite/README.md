# Jeju BFCL Lite Benchmark

This benchmark is a small Jeju-specific fixture inspired by BFCL categories. It
does not copy BFCL test data and is not an official BFCL score. The goal is to
exercise Jeju's custom command tool surface with schema-guided tool calls and
file-backed run evidence.

## Tasks

| Task | BFCL-inspired category | Expected behavior |
| --- | --- | --- |
| `simple-single-tool` | `simple_python` | Call one obvious tool with correct arguments. |
| `multiple-tool-choice` | `multiple` | Choose the right tool from several available tools. |
| `irrelevance-no-call` | `irrelevance` | Do not call tools when none are relevant. |

## Tools

The agent exposes three ordinary CLI tools through Jeju command tool adapters:

- `keyword_count`: count keyword occurrences in text.
- `text_stats`: count words and characters.
- `date_shift`: shift an ISO date by a number of days.

Each tool is a normal CLI program. Jeju adapts model JSON input to CLI argv using
the manifest `args` templates. The model sees the JSON Schema from each tool's
`schema` field.

## Acceptance

A BFCL-lite run should check both behavior and artifacts:

- `simple-single-tool`: trajectory contains `tool.requested` for
  `keyword_count`; final answer reports count `3`.
- `multiple-tool-choice`: trajectory contains `tool.requested` for `text_stats`
  and no calls to `keyword_count` or `date_shift`; final answer reports
  `words=7` and `characters=42`.
- `irrelevance-no-call`: trajectory contains no `tool.requested` events; final
  answer explains the concept directly.

These tasks intentionally avoid parallel tool calls because the current Jeju
agent loop supports one tool call per action.
