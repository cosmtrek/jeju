# translator

Translates short text between Chinese and other languages with predictable
format preservation and idiomatic Simplified Chinese output.

## Model And Cost

- Provider preset: DeepSeek
- Model: `deepseek-v4-pro`
- Required environment: `DEEPSEEK_API_KEY`
- Intended use: delegate translation to a narrow specialist while the entry
  model decides when translation is needed.

## Permissions

- Workspace access: read-only
- Approval: never
- Tools: none
- The agent does not inspect files unless the source text is passed through
  `--from`.

## Install

```bash
export JEJU_REGISTRY_INDEX=https://jeju.rickoyu.com/registry/index.yaml
jeju package add jeju:writing/translator@0.1.0
```

## Run

```bash
jeju run --output final --workspace /path/to/drafts \
  --from /path/to/drafts/snippet.txt \
  jeju:writing/translator@0.1.0 \
  "Translate to Simplified Chinese."
```

## Sample Output

```markdown
先交付最小、可验证的改动。
```
