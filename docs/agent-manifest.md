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

output:
  name: research_result
  schema:
    type: object
    properties:
      summary: { type: string }
      sources:
        type: array
        items:
          type: object
          properties:
            title: { type: string }
            url: { type: string }
          required: [title, url]
          additionalProperties: false
    required: [summary, sources]
    additionalProperties: false

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
| `output` | no | Optional final answer JSON Schema contract. |
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

For `openaiCompatible` providers, Jeju sends tools as function definitions by default. It asks for final answers with a structured response schema when the provider supports JSON Schema response formats. For providers such as DeepSeek where the documented structured-output surface is JSON mode rather than schema-strict response format, Jeju relies on prompt guidance while tools are available, validates the final answer locally, and uses JSON object mode on the tool-disabled schema retry turn.

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

Known capabilities are `workspaceRead`, `workspaceWrite`, `command`, `networkRead`, `networkWrite`, and `agentRun`. Built-in tools infer capabilities automatically; custom tools can declare them for permission decisions. Agent tools infer `agentRun`.

### Agent Tools

Jeju supports a single-agent delegation primitive where an ordinary
`kind: Agent` calls a statically declared child agent during its own runtime
loop and consumes the child final answer as a tool result. This is different from
`kind: AgentTeam`: agent tools are inline, one-call delegation from a parent
agent, not a lead-worker controller or a new team topology.

Manifest shape:

```yaml
tools:
  - name: ask_retriever
    uses: agent
    description: Run the retriever child agent for one scoped subtask.
    agent:
      manifest: ../agents/retriever.agent.yaml
    input:
      schema:
        type: object
        properties:
          task: { type: string }
          context: { type: string }
          expected_output: { type: string }
        required: [task]
        additionalProperties: false
```

The `agent` block is intentionally small:

| Field | Required | Description |
| --- | --- | --- |
| `manifest` | yes | Child `kind: Agent` manifest, resolved relative to the parent manifest during load. |

Do not put runtime budgets in the parent agent tool. The child agent controls
its own `runtime.limits`, tools, skills, model, output schema, evaluators, and
permissions through its own manifest. The parent tool declaration only says that
the parent may delegate one bounded task to that child agent.

Workspace behavior:

- when the child agent is run directly, it uses its own manifest
  `workspace.path`;
- when the child agent is invoked as an agent tool, it inherits the parent run's
  effective workspace by default.

This keeps reusable child agents independently runnable while making inline
delegation target the same project or workspace as the parent run.

Validation and execution requirements:

- `uses: agent` must be declared as an object tool; scalar shorthand is not
  supported.
- The load phase resolves `agent.manifest` paths from the parent manifest.
- The compiler performs full static validation of the child manifest so protocol
  errors are caught before runtime.
- The child manifest must be `kind: Agent`; `AgentTeam` manifests are not valid
  agent tools.
- Nested agent tools are not supported in the first version. A child agent used
  as a tool must not itself declare `uses: agent`.
- The agent tool capability is `agentRun`. It gates delegation itself; child
  file, shell, and network calls remain governed by the child manifest's own
  permissions and policy.
- Parent `permissions.access` does not automatically narrow child permissions.
  For example, a `readOnly` parent can delegate to a child whose manifest allows
  workspace writes or commands; the parent gates only the `agentRun` delegation,
  and the child gates its own tools.
- A child run failure is returned to the parent as a structured tool result with
  status and run references. Hard integration failures, such as an invalid child
  manifest or failed child compilation, are parent tool errors.

Runtime records both sides of the delegation: a normal tool span for the parent
model's tool call and a nested `subagent` span for the child run. The tool
result includes the child final answer plus metadata such as child agent
name, status, run ID, run path, and child run stats.

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

## Output

```yaml
output:
  name: triage_result
  schema:
    type: object
    properties:
      summary: { type: string }
      severity: { type: string, enum: [low, medium, high] }
    required: [summary, severity]
    additionalProperties: false
```

`output` defines the final answer contract for a run. The schema is inline JSON
Schema; schema files are not supported yet. When configured, Jeju asks providers
that support JSON Schema response formats to use the schema for final assistant
content. Intermediate tool calls still follow each tool's own input schema.

Jeju always validates the final answer locally. If the model returns output that
is not valid JSON or does not match the schema, Jeju retries once with tools
disabled and asks for the final JSON again. If the retry also fails, the run
fails and records the schema validation error in the trajectory.

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

## Run Input

The task string passed to `jeju run` becomes the user input for
`runtime.Run(task)`.

```bash
jeju run agents/research.agent.yaml "Summarize this repository."
```

For longer input, `jeju run --from <source>` can read the task from an external
source. A trailing task argument is optional and is treated as supplemental
instructions for the sourced text:

```bash
jeju run --from clipboard agents/translator.agent.yaml
jeju run --from clipboard agents/translator.agent.yaml "Translate to Chinese."
jeju run --from stdin agents/translator.agent.yaml
jeju run --from notes.md agents/translator.agent.yaml
```

Supported sources:

- `clipboard`: current system clipboard text.
- `stdin` or `-`: standard input.
- any other value: file path, with optional `file:` prefix for compatibility.

When a trailing task string is provided with `--from`, Jeju builds the final task
as the supplemental instructions, a blank line, then the source text. This is
useful for cases like copying foreign-language text to the clipboard and adding
`"Translate to Chinese."` at invocation time. If a file is literally named
`clipboard` or `stdin`, use `./clipboard` or `./stdin` so it is treated as a
path. Jeju preserves the source text as read, including trailing newlines. The
resolved task is recorded in the trajectory like any other run input.

## Run Output

Every run writes to `<runs-dir>/<run_id>/`. CLI commands can override the run
store with `--runs-dir <dir>`, and `JEJU_RUNS_DIR` sets the default when the flag
is omitted. Without either override, local manifest runs use `./runs` and
package-backed refs such as `p:namespace/name`, `package://...`, `github:...`,
`git+...`, and `jeju:...` use `~/.jeju/runs`.

- `trajectory.jsonl`
- `report.html` when a report is generated by `jeju run` or `jeju view`

`trajectory.jsonl` is the canonical run record. Metadata, config snapshots,
model inputs/outputs, tool outputs, final answers, and evaluation results are
stored as typed events or inline/chunked artifacts inside the trajectory. The
HTML report is a projection of the trajectory and can be regenerated.

The run output location is a Jeju runtime convention, not an agent manifest field.
Use `jeju run --output final` when stdout should contain only the final answer;
the run directory and trajectory recording remain unchanged.

In a generated user agent project, `./runs` is the normal local run history for
local manifest runs. In the Jeju source checkout, prefer ignored development
paths such as `.jeju-dev/runs/<scenario>`:

```bash
jeju run --runs-dir .jeju-dev/runs/research agents/research.agent.yaml "Save a note"
jeju view --runs-dir .jeju-dev/runs/research
jeju inspect --runs-dir .jeju-dev/runs/research <run_id>
```

`jeju view` and `jeju inspect` search both `./runs` and `~/.jeju/runs` when no
explicit run store is configured. `jeju view <package-ref>` lists runs for one
package. If a run ID exists in both stores, pass `--runs-dir` to choose one.
