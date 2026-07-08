# Jeju Catalog

First-party Jeju agent packages that can be installed by registry reference.

Set the registry index once:

```bash
export JEJU_REGISTRY_INDEX=https://jeju.rickoyu.com/registry/index.yaml
```

| Package | Use | Model | Permissions | Install |
|---|---|---|---|---|
| `writing/article-editor` | Review drafts and return structured editorial findings. | `deepseek-v4-pro` | read-only | `jeju package add jeju:writing/article-editor@0.1.0` |
| `writing/article-rewriter` | Rewrite a full Chinese article from concrete editorial findings. | `deepseek-v4-pro` | read-only | `jeju package add jeju:writing/article-rewriter@0.1.0` |
| `writing/chinese-expression-polisher` | Polish Chinese expression while preserving meaning and voice. | `deepseek-v4-flash` | read-only | `jeju package add jeju:writing/chinese-expression-polisher@0.1.0` |
| `writing/translator` | Translate short text with format preservation and idiomatic Chinese. | `deepseek-v4-pro` | read-only | `jeju package add jeju:writing/translator@0.1.0` |
