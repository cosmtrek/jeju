# article-editor

Reviews a draft for publish readiness, argument quality, structure, and prose
flow, then returns machine-readable editorial findings.

## Model And Cost

- Provider preset: DeepSeek
- Model: `deepseek-v4-pro`
- Required environment: `DEEPSEEK_API_KEY`
- Intended use: let a higher-level coding agent coordinate the workflow while
  this lower-cost specialist performs the review.

## Permissions

- Workspace access: read-only
- Approval: never
- Tools: `read`, `search`
- The agent does not edit files or use network access.

## Install

```bash
export JEJU_REGISTRY_INDEX=https://jeju.rickoyu.com/registry/index.yaml
jeju package add jeju:writing/article-editor@0.1.0
```

## Run

```bash
jeju run --workspace /path/to/drafts \
  --from /path/to/drafts/draft.md \
  jeju:writing/article-editor@0.1.0 \
  "Review this draft for publish readiness, structure, and prose flow."
```

## Sample Output

```json
{
  "verdict": "needs_revision",
  "summary": "The draft has a clear, provocative thesis, but the argument is asserted rather than built. It needs evidence, caveats, and at least one concrete example before publication.",
  "findings": [
    {
      "severity": "high",
      "location": "opening paragraph",
      "issue": "The central claim is stated as absolute but never defended, so skeptical readers have no reason to accept it.",
      "suggestion": "Either soften the claim or add a concrete comparison showing why the prompt, rather than runtime infrastructure, was decisive."
    }
  ]
}
```
