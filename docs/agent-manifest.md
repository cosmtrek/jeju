# Agent Manifest Reference

The Agent Manifest is Jeju's source of truth for an agent. It declares what the agent is, which model it uses, where instructions live, what runtime limits apply, which tools and skills are available, how tool calls are gated, where run data is stored, and how completed runs are evaluated.

Jeju loads a manifest through this path:

```text
config.LoadFile -> config.Validate -> compiler.Compile -> runtime.Run
```

The runtime receives a compiled agent. It does not read YAML directly.

## Minimal Shape

```yaml
apiVersion: jeju/v1alpha1
kind: Agent

metadata:
  name: research

models:
  default: primary
  providers:
    primary:
      provider: mock
      model: mock-react

instructions:
  system: ./prompts/research.md

runtime:
  mode: react

workspace:
  path: ./workspace/research
```

Defaults fill in runtime limits, React settings, skill loading policy, local sandbox settings, trajectory storage, and default permissions.

## Top-Level Fields

| Field | Required | Description |
| --- | --- | --- |
| `apiVersion` | yes | Manifest API version. Must be `jeju/v1alpha1`. |
| `kind` | yes | Resource kind. Must be `Agent`. |
| `metadata` | yes | Agent identity and labels. |
| `models` | yes | Model providers and default provider selection. |
| `model_roles` | no | Optional role-to-provider mapping. |
| `instructions` | yes | System instruction file path. |
| `runtime` | no | Runtime mode, limits, model roles, ReAct settings, and interactivity. Defaults to ReAct settings. |
| `workspace` | yes | Workspace path for file-oriented agent work. |
| `tools` | no | Tools exposed to the runtime. |
| `skills` | no | Skill discovery, disclosure, activation, and loading config. |
| `memory` | no | Memory toggle. Currently disabled in scaffolded agents. |
| `sandbox` | no | Execution sandbox config. Defaults to local workspace sandbox. |
| `policy` | no | Permission defaults and risk/tool-specific rules. |
| `trajectory` | no | JSONL trajectory storage and sinks. |
| `evaluate` | no | Optional rule-based evaluation after run completion. |

## `metadata`

```yaml
metadata:
  name: research
  description: "Local research agent"
  labels:
    team: agent-platform
```

| Field | Required | Description |
| --- | --- | --- |
| `name` | yes | Agent name. Must match `^[a-zA-Z][a-zA-Z0-9_-]*$`. |
| `description` | no | Human-readable agent description. |
| `labels` | no | String key/value labels for organization. |

## `models`

```yaml
models:
  default: primary
  providers:
    primary:
      provider: openai_compatible
      model: deepseek-chat
      base_url: https://api.deepseek.com
      env_key: DEEPSEEK_API_KEY
      temperature: 0.2
      max_output_tokens: 2048
      timeout_sec: 60
  fallback:
    - backup
```

| Field | Required | Description |
| --- | --- | --- |
| `default` | yes | Provider key used as the default model. Must exist under `providers`. |
| `providers` | yes | Map of provider names to provider config. |
| `fallback` | no | Ordered fallback provider names. |

Provider fields:

| Field | Required | Description |
| --- | --- | --- |
| `provider` | yes | Supported values: `mock`, `openai_compatible`, `deepseek`, `mimo`. |
| `model` | yes | Provider model name. Supports `${ENV_VAR}` expansion. |
| `base_url` | no | API base URL. Supports `${ENV_VAR}` expansion. |
| `env_key` | no | Environment variable name that contains the API key. Supports `${ENV_VAR}` expansion. |
| `temperature` | no | Sampling temperature. |
| `max_output_tokens` | no | Maximum model output tokens. |
| `timeout_sec` | no | Model call timeout in seconds. |

`deepseek` defaults:

- `base_url`: `https://api.deepseek.com`
- `env_key`: `DEEPSEEK_API_KEY`

`mimo` defaults:

- `base_url`: `https://api.xiaomimimo.com/v1`
- `env_key`: `MIMO_API_KEY`

## `model_roles`

```yaml
model_roles:
  reasoning: primary
  utility: primary
  evaluation: primary
```

Optional map from logical role names to configured provider names. Every referenced provider must exist in `models.providers`.

Runtime-specific model roles can also be set under `runtime.models`.

## `instructions`

```yaml
instructions:
  system: ./prompts/research.md
```

| Field | Required | Description |
| --- | --- | --- |
| `system` | yes | Path to the system instruction file. The file must exist when the manifest is validated. |

## `runtime`

```yaml
runtime:
  mode: react
  limits:
    max_steps: 8
    max_duration_sec: 300
    max_tool_calls: 10
    max_consecutive_errors: 3
  models:
    reasoning: primary
    utility: primary
    evaluation: primary
  react:
    action_mode: combined
    reflection: off
    compaction: off
  interactive:
    enabled: true
    pause_on:
      - permission_required
      - agent_question
```

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `mode` | no | `react` | Runtime mode. Only `react` is currently supported. |
| `max_steps` | no | unset | Legacy top-level max step field. Used only when `limits.max_steps` is omitted. |
| `limits.max_steps` | no | `20` | Maximum ReAct loop steps. |
| `limits.max_duration_sec` | no | `900` | Maximum run duration in seconds. |
| `limits.max_tool_calls` | no | `50` | Maximum tool calls in one run. |
| `limits.max_consecutive_errors` | no | `3` | Maximum consecutive runtime errors before stopping. |
| `models.reasoning` | no | `models.default` | Provider used for reasoning. |
| `models.utility` | no | `models.default` | Provider used for utility work. |
| `models.evaluation` | no | `models.default` | Provider used for evaluation. |
| `react.action_mode` | no | `combined` | ReAct action format. Only `combined` is currently supported. |
| `react.reflection` | no | `off` | Reflection mode. |
| `react.compaction` | no | `off` | Context compaction mode. |
| `interactive.enabled` | no | `false` | Whether interactive pauses are enabled. |
| `interactive.pause_on` | no | `permission_required`, `agent_question` | Pause reasons. |

## `workspace`

```yaml
workspace:
  path: ./workspace/research
```

| Field | Required | Description |
| --- | --- | --- |
| `path` | yes | Local workspace path. It must be an existing directory or have a creatable ancestor. |

File tools stay inside this workspace. The default local sandbox workdir also points here.

## `tools`

```yaml
tools:
  - name: file_write
    type: builtin
    description: "Write files to workspace"
    permission: ask
    risk: [write]
    side_effect: true

  - name: shell
    type: cli
    description: "Run shell commands in workspace"
    command: bash
    args: []
    permission: ask
    risk: [execute, write]
    timeout_sec: 30
    sandbox_required: true
    side_effect: true
    env:
      EXAMPLE: value
```

| Field | Required | Description |
| --- | --- | --- |
| `name` | yes | Unique tool name. |
| `type` | yes | Supported values: `builtin`, `cli`, `command`. |
| `description` | no | Tool description shown to the model/runtime. |
| `command` | required for `cli` and `command` | Command executable. |
| `args` | no | Static command arguments. |
| `schema` | no | Tool input schema reference or inline schema string. |
| `permission` | no | Tool permission override. Supported values: `allow`, `ask`, `deny`, `dry_run`. |
| `risk` | no | Risk labels used by policy rules. |
| `timeout_sec` | no | Tool timeout in seconds. |
| `sandbox_required` | no | Whether sandboxing is required for this tool. |
| `side_effect` | no | Whether the tool can change external or workspace state. |
| `env` | no | Environment variables for command tools. |

Supported risk labels:

- `read`
- `write`
- `execute`
- `network`
- `credential`
- `external`
- `destructive`
- `production`
- `payment`
- `message`

Tools used by the scaffold are:

- `file_read` (`builtin`)
- `file_write` (`builtin`)
- `shell` (`cli`)

All tool calls pass through `policy.Gate` before execution.

## `skills`

```yaml
skills:
  mode: disclose
  paths:
    - ./skills/web_research
    - ./skills/report_writer
  disclosure:
    include: [name, description, when_to_use, capabilities, inputs, outputs, requires, risk]
  activation:
    policy: manual
    active:
      - web_research
      - report_writer
    max_active: 3
  loading:
    strategy: lazy
```

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `mode` | no | `disclose` | Skill mode. The current implementation uses disclosure plus manual activation. |
| `paths` | no | empty | Skill directories. Each path must exist and contain `skill.yaml`. |
| `disclosure.include` | no | empty | Skill metadata fields to disclose. |
| `activation.policy` | no | `manual` | Skill activation policy. |
| `activation.active` | no | empty | Skill names to load as active skills. |
| `activation.max_active` | no | `3` | Maximum active skills. |
| `loading.strategy` | no | `lazy` | Skill instruction loading strategy. |

Skills are disclosed first. Active skills load instructions manually; Jeju does not inject all skill assets by default.

## `memory`

```yaml
memory:
  enabled: false
```

| Field | Required | Description |
| --- | --- | --- |
| `enabled` | no | Memory toggle. Scaffolded agents set this to `false`. |

## `sandbox`

```yaml
sandbox:
  type: local
  workdir: ./workspace/research
  network: unrestricted
  timeout_sec: 300
```

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `type` | no | `local` | Sandbox type. Only `local` is currently supported. |
| `workdir` | no | `workspace.path` | Working directory for shell execution. |
| `network` | no | unset | Network policy marker. |
| `image` | no | unset | Reserved for non-local sandbox modes. |
| `endpoint` | no | unset | Reserved for remote sandbox modes. |
| `api_key_env` | no | unset | Reserved API key environment variable for remote sandbox modes. |
| `timeout_sec` | no | unset | Sandbox timeout in seconds. |
| `mounts` | no | empty | Reserved mount config. |

Mount shape:

```yaml
mounts:
  - source: ./data
    target: /data
```

File tools and shell execution must remain inside the configured local workspace/sandbox boundaries.

## `policy`

```yaml
policy:
  default_permission: ask
  sandbox_required_for:
    - execute
  rules:
    - match:
        risk: read
      permission: allow
    - match:
        risk: write
      permission: ask
    - match:
        tool: shell
      permission: ask
    - match:
        risk: destructive
      permission: deny
```

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `default_permission` | no | `ask` | Fallback permission when no rule matches. |
| `sandbox_required_for` | no | empty | Risk labels that require sandbox execution. |
| `rules` | no | empty | Ordered permission rules. |

Rule fields:

| Field | Required | Description |
| --- | --- | --- |
| `match.risk` | no | Match tool calls by risk label. |
| `match.tool` | no | Match tool calls by tool name. |
| `permission` | yes | Supported values: `allow`, `ask`, `deny`, `dry_run`. |

## `trajectory`

```yaml
trajectory:
  enabled: true
  format: jsonl
  store:
    type: file
    path: ./runs
  sinks:
    - type: console
      level: info
    - type: file
      path: ./runs
  fail_on_sink_error: false
```

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `enabled` | no | `true` | Trajectory is always enabled by defaults. |
| `format` | no | `jsonl` | Trajectory format. Only `jsonl` is currently supported. |
| `store.type` | no | `file` | Run store type. Only `file` is currently supported. |
| `store.path` | no | `./runs` | Run directory root. Must be creatable. |
| `sinks` | no | console and file sinks | Additional event sinks. |
| `fail_on_sink_error` | no | `false` | Whether sink failures fail the run. |

Sink fields:

| Field | Required | Description |
| --- | --- | --- |
| `type` | yes | Sink type, for example `console` or `file`. |
| `level` | no | Console verbosity level. |
| `path` | no | File sink path. |
| `endpoint` | no | Reserved endpoint for external sinks. |
| `api_key_env` | no | Reserved API key environment variable for external sinks. |

Every run creates a run directory and writes:

- `metadata.json`
- `config.snapshot.yaml`
- `trajectory.jsonl`
- `final.md`
- `evaluation.json` when evaluation is enabled
- `artifacts/` for large model/tool payloads

## `evaluate`

```yaml
evaluate:
  enabled: true
  on_run_complete: true
  evaluators:
    - name: basic_trajectory
      type: rule
      rules:
        - final_answer_exists
        - no_model_error
        - no_tool_error
        - max_steps_not_exceeded
        - max_tool_calls_not_exceeded
        - no_permission_denied
        - run_completed
  outputs:
    path: ./runs
    file: evaluation.json
```

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `enabled` | no | `false` | Enables evaluation. |
| `on_run_complete` | no | `true` when enabled | Runs evaluation after completion. |
| `evaluators` | no | empty | Evaluation configs. |
| `outputs.path` | no | `trajectory.store.path` when enabled | Evaluation output root. |
| `outputs.file` | no | `evaluation.json` when enabled | Evaluation output file name. |

Evaluator fields:

| Field | Required | Description |
| --- | --- | --- |
| `name` | yes | Evaluator name. |
| `type` | yes | Only `rule` is currently supported. |
| `rules` | no | Rule names for rule evaluators. |
| `model` | no | Reserved model field. |
| `rubric` | no | Reserved rubric field. |

Supported rule names:

- `final_answer_exists`
- `no_model_error`
- `no_tool_error`
- `max_steps_not_exceeded`
- `max_tool_calls_not_exceeded`
- `no_permission_denied`
- `run_completed`

## Environment Expansion

`ResolveEnv` expands `${ENV_VAR}` references in these model provider fields:

- `models.providers.<name>.base_url`
- `models.providers.<name>.model`
- `models.providers.<name>.env_key`

Example:

```yaml
models:
  default: primary
  providers:
    primary:
      provider: openai_compatible
      model: ${JEJU_MODEL}
      base_url: ${JEJU_BASE_URL}
      env_key: JEJU_API_KEY
```

## Validation Summary

`jeju validate <agent.yaml>` checks the current manifest constraints, including:

- `apiVersion` is `jeju/v1alpha1`.
- `kind` is `Agent`.
- `metadata.name` matches `^[a-zA-Z][a-zA-Z0-9_-]*$`.
- `models.default` exists in `models.providers`.
- Model providers are `mock`, `openai_compatible`, `deepseek`, or `mimo`.
- Referenced model roles point at known providers.
- `instructions.system` exists.
- `runtime.mode` is `react`.
- `runtime.react.action_mode` is `combined`.
- `workspace.path` is creatable.
- `sandbox.type` is `local`.
- `trajectory.format` is `jsonl`.
- `trajectory.store.type` is `file`.
- Tool names are unique.
- `cli` and `command` tools define `command`.
- Tool permissions and risks use supported values.
- Skill paths exist and contain `skill.yaml`.
- Evaluators use `type: rule` and known rule names.
