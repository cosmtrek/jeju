# Jeju Use Cases

This directory contains recommended, runnable Jeju scenarios. Each use case is a
self-contained agent bundle. A bundle can be as small as one manifest plus one
prompt, and can add sample inputs, evaluators, tools, or a local workspace only
when the scenario needs them.

Use cases are different from test fixtures: they are intended to show where Jeju
is useful as a repeatable, evaluable, local-first agent runtime.

## Available Use Cases

- [Translate agent](translate-agent/translator.agent.yaml): a minimal flat agent
  with only `translator.agent.yaml` and `prompt.md`.
- [Code review agent](code-review-agent/README.md): reviews a Git diff and
  returns structured findings checked by a local evaluator.

## Minimal Translate Agent

The translate agent is intentionally flat:

```text
usecases/translate-agent/
  translator.agent.yaml
  prompt.md
```

Validate it from the Jeju checkout:

```bash
go run ./cmd/jeju validate usecases/translate-agent/translator.agent.yaml
```

Run it from the use case directory so local run artifacts stay under
`usecases/translate-agent/runs/`:

```bash
export DEEPSEEK_API_KEY=sk-...
cd usecases/translate-agent
go run ../../cmd/jeju run translator.agent.yaml "Translate to English: 你好，欢迎使用 Jeju。"
```

The manifest uses the DeepSeek preset by default. To use another provider, edit
the single `models.providers.primary` block in `translator.agent.yaml`.
