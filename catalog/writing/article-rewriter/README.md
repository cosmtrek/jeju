# article-rewriter

Rewrites a complete Chinese article from an existing draft plus concrete
editorial suggestions.

## Model And Cost

- Provider preset: DeepSeek
- Model: `deepseek-v4-pro`
- Required environment: `DEEPSEEK_API_KEY`
- Intended use: reserve the entry model for choosing edits, then delegate the
  full rewrite to a focused DeepSeek specialist.

## Permissions

- Workspace access: read-only
- Approval: never
- Tools: `read`, `search`
- The agent returns rewritten Markdown and does not edit files.

## Install

```bash
export JEJU_REGISTRY_INDEX=https://jeju.rickoyu.com/registry/index.yaml
jeju package add jeju:writing/article-rewriter@0.1.0
```

## Run

```bash
jeju run --output final --workspace /path/to/drafts \
  --from /path/to/drafts/draft.md \
  jeju:writing/article-rewriter@0.1.0 \
  "Rewrite the article according to these findings: tighten the thesis, add one caveat, and reduce repetition."
```

## Sample Output

```markdown
# Agent runtime is more than prompt engineering

Prompts still matter most at the moment of delegation: they define the task,
the expected output, and the boundary of judgment. But a reliable agent also
needs a thin execution layer around that prompt.

That layer does not have to be heavy. It can be as small as a fixed workspace,
read-only permissions, structured output validation, and a trajectory that shows
what happened.
```
