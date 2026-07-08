---
name: jeju-writing
description: Use Jeju writing packages as specialized workers for article review, Chinese prose polishing, full rewrites, and translation. Use when the user asks to review a draft, improve Chinese writing, rewrite an article, translate text, or delegate writing work to bounded Jeju agents.
metadata:
  short-description: Route writing tasks to Jeju workers
  version: "0.3.0"
---

# Jeju Writing

Use this skill to route writing tasks to first-party Jeju packages. Route simple
requests directly; use an editorial review step only when the user wants
publish-readiness review, diagnosis, or high-impact revision.

## Packages

- `writing/article-editor@0.1.0`: structured editorial review JSON.
- `writing/chinese-expression-polisher@0.1.0`: local Chinese prose polish.
- `writing/article-rewriter@0.1.0`: full article rewrites from constraints.
- `writing/translator@0.1.0`: translation with format preservation.

If package versions here disagree with the registry index, trust the registry
index and update this skill before publishing.

## Setup

Before running a worker:

```bash
jeju --version
test -n "$DEEPSEEK_API_KEY"
```

If the `jeju` CLI is missing, install it first (macOS/Linux):

```bash
curl -fsSL https://raw.githubusercontent.com/cosmtrek/jeju/master/scripts/install.sh | sh
```

or from source with Go 1.25+: `go install github.com/cosmtrek/jeju/cmd/jeju@latest`.
If `DEEPSEEK_API_KEY` is missing, stop and ask the user to provide one; do not
substitute another provider.

Install or refresh all writing packages:

```bash
sh -c 'set -e; export JEJU_REGISTRY_INDEX=${JEJU_REGISTRY_INDEX:-https://jeju.rickoyu.com/registry/index.yaml}; for pkg in writing/article-editor writing/chinese-expression-polisher writing/article-rewriter writing/translator; do jeju package add "jeju:$pkg@0.1.0"; done'
```

Use `jeju:` refs for registry install or one-off remote runs. After installing,
prefer `p:` refs for normal runs. Do not teach GitHub subdirectory refs for
catalog packages.

## Route The Request

Use the user's intent as the routing key:

- Translation request: run `p:writing/translator@0.1.0` directly.
- Light Chinese polish, sentence-level improvement, or final fluency pass: run
  `p:writing/chinese-expression-polisher@0.1.0` directly.
- Explicit full rewrite with style, audience, or structure constraints: run
  `p:writing/article-rewriter@0.1.0` directly.
- Review, critique, publish-readiness, or "tell me what to fix": run
  `p:writing/article-editor@0.1.0` first.
- End-to-end revision: run editor first, show findings, ask which findings to
  apply unless the user already authorized a full rewrite.

Do not make a simple translation or polish request pay the cost of an editorial
review.

## Commands

For review:

```bash
jeju run --workspace /path/to/draft-dir \
  --from /path/to/draft-dir/draft.md \
  p:writing/article-editor@0.1.0 \
  "Review this draft for publish readiness, structure, logic, and prose flow. Return only the configured JSON."
```

Read the JSON and show the `verdict`, `summary`, selected `findings`, and
`jeju view <run_id>` evidence hint.

For Chinese polish:

```bash
jeju run --output final --workspace /path/to/draft-dir \
  --from /path/to/draft-dir/draft.md \
  p:writing/chinese-expression-polisher@0.1.0 \
  "轻改这篇草稿，让中文更自然，保留 Markdown/frontmatter，只输出优化稿。"
```

For full rewrite:

```bash
jeju run --output final --workspace /path/to/draft-dir \
  --from /path/to/draft-dir/draft.md \
  p:writing/article-rewriter@0.1.0 \
  "Rewrite the article using these constraints:

Selected editorial findings:
<paste selected findings>

Rewrite constraints:
- preserve the core thesis and useful examples
- keep Markdown/frontmatter
- only make changes authorized by the user"
```

For translation:

```bash
jeju run --output final --workspace /path/to/draft-dir \
  --from /path/to/draft-dir/draft.md \
  p:writing/translator@0.1.0 \
  "Translate to <target language>. Preserve Markdown/frontmatter."
```

Use the user's requested target language. If unspecified, default to Simplified
Chinese.

## Operating Rules

- Keep `--workspace` pointed at the user's draft directory.
- Use `--from` when the source file or snippet is known.
- Treat `article-editor` JSON as the task envelope for downstream workers.
- Do not invent findings; pass only selected review findings or explicit user
  instructions into rewrite or polish tasks.
- Jeju workers return text; apply returned text to files only after the user
  asks you to write it back.
- In `--output final` mode, stdout is the final answer and stderr contains
  `run_id` plus report path.
