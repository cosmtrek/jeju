# Agent Manifest

Jeju loads an agent manifest through:

```text
config.LoadFile -> config.Validate -> compiler.Compile -> runtime.Run
```

The runtime receives a compiled agent, not raw YAML. Relative paths are resolved from the manifest file location.
For reusable agents, `jeju run --workspace <dir>` can override `workspace.path` for a single run while keeping the saved config snapshot auditable.

## Example

```yaml
apiVersion: jeju/v1alpha1
kind: Agent

metadata:
  name: research
  description: "Local research agent"
  labels:
    team: agent-platform

models:
  providers:
    primary:
      type: openaiCompatible
      preset: deepseek
      model: deepseek-v4-flash
      baseUrl: https://api.deepseek.com
      envKey: DEEPSEEK_API_KEY
      temperature: 0.2
      thinking:
        type: disabled
      maxOutputTokens: 2048
      contextWindow: 128000
      timeoutSec: 60

instructions:
  system: ../prompts/research.md

runtime:
  model: primary
  loop:
    type: react
  compressionThreshold: 0.8
  recentTokenBudget: 20000
  limits:
    maxSteps: 20
    maxDurationSec: 900
    maxToolCalls: 50
    maxConsecutiveErrors: 3

workspace:
  path: ../workspace/research

tools:
  - read
  - write
  - edit
  - search
  - shell

  - name: keyword_count
    uses: command
    capabilities: [workspaceRead]
    command:
      run: ../workspace/research/tools/keyword_count.py
      args: ["{{text}}", "{{keyword}}"]
      timeoutSec: 30
    input:
      schema:
        type: object
        properties:
          text: { type: string }
          keyword: { type: string }
        required: [text, keyword]

  - name: search_api
    uses: http
    capabilities: [networkRead]
    http:
      method: POST
      url: https://api.exa.ai/search
      headers:
        content-type: application/json
        x-api-key: ${EXA_API_KEY}
      body:
        json:
          query: "{{query}}"
          numResults: 10
          type: auto
          contents:
            highlights: true
      timeoutSec: 30
    input:
      schema:
        type: object
        properties:
          query: { type: string }
        required: [query]

skills:
  dirs:
    - ../skills
  active:
    - web-research

permissions:
  access: workspace
  approval: onRequest

evaluate:
  enabled: true
  evaluators:
    - name: basic
      uses: rules
      rules:
        - finalAnswerExists
        - runCompleted

    - name: quality
      uses: llm
      model: primary
      prompt: ../eval/quality_judge.md
      threshold: 0.7

    - name: custom_status
      uses: command
      command:
        run: python3
        args: [../eval/status_judge.py]
        timeoutSec: 30
```

## Top-Level Fields

| Field | Required | Description |
| --- | --- | --- |
| `apiVersion` | yes | Must be `jeju/v1alpha1`. |
| `kind` | yes | Must be `Agent`. |
| `metadata` | yes | Agent identity. |
| `models` | yes | Model provider registry. |
| `instructions` | yes | Agent instruction paths. |
| `runtime` | yes | Runtime model, loop, and limits. |
| `workspace` | yes | Local workspace boundary for file and shell tools. |
| `tools` | no | Tool capabilities exposed to the agent. |
| `skills` | no | Skill directories and active skills. |
| `permissions` | no | Access and approval profile. |
| `evaluate` | no | Optional evaluators run after completion. |

## Models

```yaml
models:
  providers:
    primary:
      type: openaiCompatible
      preset: deepseek
      model: deepseek-v4-flash
      baseUrl: https://api.deepseek.com
      envKey: DEEPSEEK_API_KEY
      thinking:
        type: disabled
      maxOutputTokens: 2048
      contextWindow: 128000
```

Provider fields:

| Field | Required | Description |
| --- | --- | --- |
| `type` | yes | `mock` or `openaiCompatible`. |
| `preset` | no | `deepseek` or `mimo`; fills default `baseUrl`, `envKey`, and JSON mode. |
| `model` | yes | Provider model name. |
| `baseUrl` | no | API base URL. |
| `envKey` | no | Environment variable name that contains the API key. |
| `temperature` | no | Sampling temperature. |
| `thinking.type` | no | `auto`, `disabled`, or `enabled`. DeepSeek and MiMo presets default to `disabled`; when enabled, Jeju records provider reasoning content and replays it on later tool turns. |
| `thinking.effort` | no | Provider-specific reasoning effort: `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, or `max`. |
| `maxOutputTokens` | no | Maximum output tokens. Jeju reserves this amount when budgeting input context. |
| `contextWindow` | no | Total model context window used for request-time token budgeting. Presets fill common defaults; custom `openaiCompatible` providers must set it explicitly. |
| `timeoutSec` | no | Model request timeout. |

`runtime.model` selects the provider used by the agent loop. If omitted, Jeju uses the single configured provider; with multiple providers it must be explicit.

Jeju keeps the manifest model fields provider-neutral and maps them in the model adapter. OpenAI-style structured output and function calling are protocol capabilities, not user-facing loop-format knobs: the runtime prefers native tool/function calling when the provider supports it, and uses structured outputs or JSON mode for final/evaluator outputs. Provider presets are where API spelling differences live, such as OpenAI `reasoning_effort`, DeepSeek/MiMo `thinking.type`, and MiMo `max_completion_tokens`.

For `openaiCompatible` providers, Jeju sends tools as function definitions by default. It asks for final answers with a structured response schema when the provider supports JSON Schema response formats, and falls back to JSON object mode for providers such as DeepSeek where the documented structured-output surface is JSON mode rather than schema-strict response format.

When `thinking.type: enabled` is used with providers that return `reasoning_content` such as DeepSeek and MiMo, Jeju writes the full thinking text as a trajectory artifact, shows a short console preview, and keeps the assistant `reasoning_content` in message history so the next tool-call turn can pass it back to the API. OpenAI Responses reasoning items and Gemini thought signatures follow the same preservation principle, but require provider-specific adapters.

## Runtime

```yaml
runtime:
  model: primary
  loop:
    type: react
  compressionThreshold: 0.8
  recentTokenBudget: 20000
  limits:
    maxSteps: 20
    maxDurationSec: 900
    maxToolCalls: 50
    maxConsecutiveErrors: 3
```

`loop.type` currently supports `react`. The action protocol is selected internally from the model provider and capabilities; it is not a manifest field.

`compressionThreshold` defaults to `0.8`, and `recentTokenBudget` defaults to `20000`. Before each model request, Jeju estimates input tokens for prompt layers, message history, tools, and response schema. The effective input budget is `contextWindow - maxOutputTokens`; compression starts when the estimate exceeds `effectiveInputBudget * compressionThreshold`, and the run fails before the provider call if the compressed request still exceeds the effective budget. Jeju first truncates older tool results, then keeps a token-budgeted recent window of complete message blocks and asks the configured runtime model to update a rolling summary from the previous summary plus newly evicted blocks. The configured recent budget is an upper bound; small context windows cap the effective recent budget at the compression threshold. Jeju then applies emergency truncation to recent tool results if needed. Summary inputs are capped before the summary model call; if the summary call fails, Jeju degrades by dropping the evicted blocks and preserving the previous summary plus recent raw messages instead of retrying the same failing summary call. The previous summary is stored separately from recent raw messages, so later compression summarizes only newly evicted raw messages together with the prior summary rather than re-summarizing the original messages. Context estimates, before/after snapshots, summary model calls, summaries, and compression decisions are recorded in the trajectory.

## Tools

Built-in tools can be listed as strings:

```yaml
tools:
  - read
  - write
  - edit
  - search
  - shell
```

String entries are shorthand for `uses: builtin:<name>`.

Custom command tools:

```yaml
tools:
  - name: keyword_count
    uses: command
    capabilities: [workspaceRead]
    command:
      run: ../workspace/agent/tools/keyword_count.py
      args: ["{{text}}", "{{keyword}}"]
      timeoutSec: 30
    input:
      schema: ../schemas/keyword_count.json
```

HTTP tools:

```yaml
tools:
  - name: search_api
    uses: http
    capabilities: [networkRead]
    http:
      method: POST
      url: https://api.exa.ai/search
      headers:
        content-type: application/json
        x-api-key: ${EXA_API_KEY}
      body:
        json:
          query: "{{query}}"
          numResults: 10
          type: auto
          contents:
            highlights: true
```

HTTP scheme and host must be static. Template values use `{{field}}` and are intended for query, path-like values, headers, and body fields.

Known capabilities are `workspaceRead`, `workspaceWrite`, `command`, `networkRead`, and `networkWrite`. Built-in tools infer capabilities automatically; custom tools can declare them for permission decisions.

## Skills

```yaml
skills:
  dirs:
    - ../skills
  active:
    - web-research
```

Each skill is a directory under a listed `dirs` root and must contain `SKILL.md`.
Jeju follows the Agent Skills `SKILL.md` format: required `name` and
`description`, optional `license`, `compatibility`, `metadata`, and
`allowed-tools`. Store skill versions in `metadata.version`; the Agent Skills
spec does not define a top-level `version` field. The skill `name` must match
the parent directory. Jeju reads frontmatter for disclosure and injects the full
`SKILL.md` only for active skills.

Example:

```markdown
---
name: web-research
description: Research web sources and summarize findings. Use when current external information is needed.
metadata:
  jeju.capabilities: source_collection,summarization
  version: "0.1.0"
allowed-tools: search_api
---

# Web Research

Use `search_api` for web search. It calls Exa with `EXA_API_KEY` from the environment and returns highlighted results. Use primary sources and summarize with links.
```

## Permissions

```yaml
permissions:
  access: workspace
  approval: onRequest
```

`access` controls the resource boundary:

- `readOnly`: read-only workspace access.
- `workspace`: workspace read/write plus configured tools, with approval policy applied.
- `full`: full local access.

`approval` controls when Jeju asks the user:

- `never`: allowed calls run, blocked calls fail.
- `onRequest`: sensitive calls such as writes, command execution, and network access ask first.
- `always`: all side-effecting calls ask first.

## Evaluate

```yaml
evaluate:
  enabled: true
  evaluators:
    - name: basic
      uses: rules
      rules: [finalAnswerExists, runCompleted]
```

Evaluator backends:

- `rules`: deterministic built-in rules.
- `llm`: model-based judge. Requires `prompt`; `model` defaults to `runtime.model`.
- `command`: local command judge. Jeju runs `command.run` with `command.args`, sends evaluation context as JSON on stdin, and expects JSON with `score` and `passed`.

Built-in rules:

- `finalAnswerExists`
- `noModelError`
- `maxStepsNotExceeded`
- `maxToolCallsNotExceeded`
- `noPermissionDenied`
- `runCompleted`

Evaluation results are recorded in `<runs-dir>/<run_id>/trajectory.jsonl` as an
`evaluator` span plus an `evaluation` artifact.

## Run Output

Every run writes to `<runs-dir>/<run_id>/`. The default run store is `./runs`,
or `JEJU_RUNS_DIR` when that environment variable is set. CLI commands can
override it with `--runs-dir <dir>`.

- `trajectory.jsonl`
- `report.html` when a report is generated by `jeju run` or `jeju view`

`trajectory.jsonl` is the canonical run record. Metadata, config snapshots,
model inputs/outputs, tool outputs, final answers, and evaluation results are
stored as typed events or inline/chunked artifacts inside the trajectory. The
HTML report is a projection of the trajectory and can be regenerated.

The run output location is a Jeju runtime convention, not an agent manifest field.
Use `jeju run --output final` when stdout should contain only the final answer;
the run directory and trajectory recording remain unchanged.

In a generated user agent project, `./runs` is the normal local run history. In
the Jeju source checkout, prefer ignored development paths such as
`.jeju-dev/runs/<scenario>`:

```bash
jeju run --runs-dir .jeju-dev/runs/research agents/research.agent.yaml "Save a note"
jeju runs --runs-dir .jeju-dev/runs/research
jeju inspect --runs-dir .jeju-dev/runs/research <run_id>
```
