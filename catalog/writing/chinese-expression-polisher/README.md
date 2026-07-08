# chinese-expression-polisher

Polishes Chinese prose into more idiomatic, natural, and readable writing while
preserving the author's meaning, facts, and voice.

## Model And Cost

- Provider preset: DeepSeek
- Model: `deepseek-v4-flash`
- Required environment: `DEEPSEEK_API_KEY`
- Intended use: cheap, fast expression polish after the entry model chooses the
  text span and edit strength.

## Permissions

- Workspace access: read-only
- Approval: never
- Tools: `read`, `search`
- The agent does not edit files or use network access.

## Install

```bash
export JEJU_REGISTRY_INDEX=https://jeju.rickoyu.com/registry/index.yaml
jeju package add jeju:writing/chinese-expression-polisher@0.1.0
```

## Run

```bash
jeju run --output final --workspace /path/to/drafts \
  --from /path/to/drafts/paragraph.md \
  jeju:writing/chinese-expression-polisher@0.1.0 \
  "轻改这段文字，让中文更自然，只输出优化稿。"
```

## Sample Output

```markdown
这个功能可以帮助用户更快地创建内容，同时保留必要的编辑空间。它适合处理已有思路明确、但表达还不够顺畅的草稿。
```
