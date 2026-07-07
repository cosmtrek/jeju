---
name: jeju-agent-builder
description: Use when converting a repeated workflow, local developer task, specialist agent idea, inline child-agent delegation idea, or bounded lead-worker team idea into a minimal Jeju agent, agent-tool bundle, or AgentTeam bundle with manifests, prompts, tools, permissions, evaluation, and smoke validation. This is for any higher-level AI agent that needs to author Jeju agents or teams.
metadata:
  short-description: Create focused Jeju agents from repeated workflows
  version: "0.4.1"
---

# Jeju Agent Builder

Use this skill when the user wants an AI agent to turn a repeated workflow into
a Jeju agent, an agent with declared child-agent tools, or a bounded AgentTeam.
Jeju agents should be narrow, bounded execution units that another higher-level
agent can create, run, inspect, and improve; AgentTeams should stay limited to
one lead-worker controller over ordinary Jeju agents.

This is an authoring skill stored in the Jeju repository. It is not a Jeju
runtime skill loaded by an agent manifest unless someone explicitly copies or
references it as one.

For the full authoring manual, read `docs/manual-for-agents.md` in the Jeju
repository when available. For distributable agents, also read
`docs/agent-package.md`. Jeju source repository:
https://github.com/cosmtrek/jeju

## Decide If Jeju Is Appropriate

Create a Jeju agent only when the workflow benefits from model reasoning plus
explicit tools, permissions, run evidence, or evaluation.

Prefer another artifact when:

- A deterministic script is enough.
- The value is only prompt reuse inside the authoring agent.
- The task needs broad orchestration, distributed workers, remote sandboxes, or
  long-term memory.

Good Jeju agent candidates are narrow specialists such as code-review triage,
docs update planning, benchmark failure classification, trajectory inspection,
prompt regression review, release-note drafting, and local repo hygiene checks.
Use `tools[].uses: agent` when one ordinary parent agent should call one or more
declared specialist child agents inline and consume their final answers as tool
results.
Use `kind: AgentTeam` only when the workflow genuinely needs a bounded
lead-worker controller over several ordinary child agents.

## Minimal Design Contract

Before writing files, identify:

- Purpose: one sentence describing the repeated workflow.
- Input contract: what the user or parent agent passes to `jeju run`.
- Output contract: final answer shape. Prefer manifest `output` with inline
  JSON Schema for structured JSON; use concise markdown only when free-form
  prose is the intended product.
- Workspace binding: which local project directory is inspected or changed.
- Tool needs: read/search/write/edit/shell/http/command tools, with why each is
  needed.
- Permission profile: default to `readOnly`; only enable writes or shell when
  required.
- Model tier: cheap, medium, or strong based on task complexity and risk.
- Runtime limits: max steps, max duration, max tool calls, max consecutive
  errors.
- Evaluation: at least one smoke case or command/rule evaluator when practical.
- Packaging: whether the finished agent should be local-only or published as an
  Agent Package, including package id, version, and intended source.
- Non-goals: what the agent must not attempt.

## Minimal Bundle Shape

Use the smallest bundle that can be validated and run:

```text
agents/<name>.agent.yaml
prompts/<name>.md
workspace/<name>/.gitkeep
eval/<optional-evaluator>.py
README.md
```

Add a runtime skill, schema, or custom tool script only when it removes real
complexity.

When generating a runtime skill, follow the Agent Skills `SKILL.md` frontmatter
format. The Agent Skills spec does not define a top-level `version` field; put
the semantic version in `metadata.version` so bundles can be distributed and
upgraded intentionally:

```markdown
---
name: repo-review
description: Review a local repository diff and return actionable findings. Use when repo change review is needed.
metadata:
  short-description: Review local repository diffs
  version: "0.1.0"
allowed-tools: read search git_diff
---
```

Start new skills at `0.1.0`. Increment the version when behavior, required
tools, output format, compatibility, or validation expectations change.

## Package For Distribution

Use an Agent Package when the finished bundle should be reused by stable
reference, shared as a local artifact, pulled from Git, or listed in a registry.
Do not package early scratch agents unless the user asks for a distributable
output.

Agent Package is a distribution layer for one reusable `kind: Agent`; it is not
a second runtime manifest. The package should point at the existing agent
manifest and leave runtime behavior in `agent.yaml`:

```yaml
apiVersion: jeju/v1alpha1
kind: AgentPackage

metadata:
  id: coding/code-review
  name: Code Review
  version: 0.1.0
  description: "Review a local repository change and return structured findings."

compatibility:
  jeju: ">=0.4.0"

agent:
  manifest: agents/code-review.agent.yaml
```

Package rules for generated bundles:

- The package root must contain `jeju.package.yaml`; other directory names are
  conventions, not protocol.
- Generate at most one `AgentPackage` for one `kind: Agent`. Do not package
  `AgentTeam` in V1.
- Use package ids in `namespace/name` form and semantic versions.
- Prefer bumping `metadata.version` when behavior, required tools, output
  contract, compatibility, or validation expectations change.
- Do not put `usage`, `quality`, `risk`, or `publish.source` in
  `jeju.package.yaml`. Put usage in `README.md`; let risk be derived from the
  agent manifest; keep source and digest in registry/store provenance.
- Keep `workspace.path` as a placeholder inside the bundle. Consumers can run
  package-backed agents with `jeju run --workspace /path/to/project ...`.

Useful package commands:

```bash
jeju package init <bundle> --agent agents/<name>.agent.yaml --id <namespace/name> --version 0.1.0 --description "<capability>"
jeju package validate <bundle>
jeju package pack <bundle> --out dist/
jeju package add dist/<namespace-name>-0.1.0.jpkg
jeju run package://<namespace/name>@0.1.0 "<sample input>"
```

Remote package sources can be added or run by Git subdirectory reference:

```bash
jeju package add github:owner/repo//namespace/name?ref=<commit>
jeju run github:owner/repo//namespace/name?ref=<commit> "<sample input>"
```

If a mutable source or local directory changes without a version bump, `jeju
package add` and `jeju package update` should fail by default when the digest
changes. Use `--replace` only when the user explicitly wants to move the local
`id@version` ref to new content.

## Agent Tools Authoring

Use `tools[].uses: agent` for inline delegation inside one ordinary `kind:
Agent` loop. This is the right fit when a parent agent needs a small number of
static specialists and can decide when to call them as normal tools.

Do not use agent tools as a general workflow graph, dynamic worker pool, or
replacement for AgentTeam. Use `kind: AgentTeam` instead when the task needs
explicit task state, dependencies, retries, verification, parallel scheduling,
or controller-level final selection.

Minimal agent-tool bundle shape:

```text
agents/<parent>.agent.yaml
agents/<child>.agent.yaml
prompts/<parent>.md
prompts/<child>.md
workspace/<name>/.gitkeep
README.md
```

Manifest shape:

```yaml
tools:
  - name: ask_retriever
    uses: agent
    description: Run the retriever child agent for one scoped subtask.
    agent:
      manifest: child.agent.yaml
    input:
      schema:
        type: object
        required: [task]
        additionalProperties: false
        properties:
          task:
            type: string
          context:
            type: string
          expected_output:
            type: string
```

Agent tool rules:

- The child manifest must be `kind: Agent`; do not point at `AgentTeam`.
- The compiler validates and compiles the child manifest before runtime.
- Nested agent tools are not supported. A child agent used as a tool must not
  itself declare `uses: agent`.
- Do not put parent-side runtime budgets such as timeout, max rounds, or max
  depth on the agent tool. The child controls its own `runtime.limits`.
- When the child runs directly, it uses its own `workspace.path`. When invoked
  as an agent tool, it inherits the parent run's effective workspace.
- The parent policy gates delegation through the `agentRun` capability. The
  child still enforces its own manifest permissions for file, shell, command,
  HTTP, and network behavior.
- `readOnly` parent access does not block `agentRun` by itself. If delegation
  should require a human approval step, use `permissions.approval: onRequest`.
- A child run failure is returned to the parent as an agent tool result with
  child status, run id, path, and stats. Hard integration failures such as an
  invalid child manifest are parent tool errors.
- The parent trajectory records a normal tool span plus a nested `subagent`
  span. The child run has its own `trajectory.jsonl` under the parent run's
  `child-runs/<tool>/<tool_call_id>/<child_run_id>/` directory.

Prompt the parent to keep delegation small and explicit. A useful parent prompt
names the child tools, when each should be called, what to do if a child returns
`status: failed`, and when to stop calling tools and produce final output.

Validate and smoke-run an agent-tool bundle from the Jeju source checkout:

```bash
jeju validate <bundle>/agents/<parent>.agent.yaml
jeju run --workspace /path/to/project --runs-dir .jeju-dev/runs/<name> <bundle>/agents/<parent>.agent.yaml "<sample input>"
jeju view --runs-dir .jeju-dev/runs/<name> <run_id>
```

Example to study when available:

```text
examples/code-review-with-agent-tools/
```

## AgentTeam Authoring

Use `kind: AgentTeam` for bounded lead-worker orchestration from one user goal.
Do not use it for peer-to-peer chat, dynamic worker creation, distributed
workers, shared long-term memory, or a general multi-agent platform.

Minimal team shape:

```text
teams/<name>.team.yaml
agents/<lead>.agent.yaml
agents/<worker>.agent.yaml
prompts/<lead>.md
prompts/<worker>.md
workspace/<name>/.gitkeep
```

Team manifest rules:

- Set `runtime.topology: lead_worker`.
- Declare one `lead.agent` and a fixed `workers` catalog.
- Do not write `lead.synthesisAgent`; it is no longer supported.
- If final composition needs a separate model pass, declare a normal `writer`
  worker and have the lead create a final writer task.
- Do not package `AgentTeam` in V1; package only reusable `kind: Agent`
  manifests.

Lead decision contract:

- The controller injects an AgentTeam protocol system prompt before the lead
  agent's own system prompt. Keep the lead prompt focused on team strategy,
  worker responsibilities, escalation policy, and when to finish.
- The lead must return exactly one TeamDecision JSON object on every turn.
- `decision` is a tagged union: `continue`, `finish`, or `abort`.
- For `continue`, use `tasks` to add new worker tasks. Each task needs stable
  `id`, declared `worker`, non-empty `objective`, optional `depends_on`,
  optional `context_refs`, and optional `output_contract`.
- `depends_on` is the scheduling edge. `context_refs` is the data-injection
  edge. If `context_refs` is omitted, it defaults to `depends_on`; explicit
  `[]` means inject no prior task output.
- Repeated task ids are ignored, not updated. Tell the lead to create a new id
  for replacement work after rejection.
- For `finish`, set exactly one of `finish.content` or `finish.task_id`.
  Prefer `finish.task_id` when a verified writer task produced the final
  answer. The referenced task must be verified and have non-empty final output.
- For `abort`, set `abort.reason` when the declared team cannot complete the
  goal with available workers, tools, policy, or workspace.
- Do not use old decisions such as `synthesize` or `blocked`.

Worker/output rules:

- Workers are ordinary `kind: Agent` manifests and do not chat with each other.
- Do not force every worker to return JSON. Use task `output_contract` only
  when the controller or another worker needs machine-checkable output.
- When `verification.requireStructuredTaskOutput` is enabled, required-field
  checks apply only to tasks whose active output contract format is `json`.
  Markdown/text writer tasks are valid for final reports.
- If `verification.requireVerifier` is true, declare a worker named `verifier`;
  finish remains blocked until at least one verifier task is verified.

Minimal team manifest sketch:

```yaml
apiVersion: jeju/v1alpha1
kind: AgentTeam

metadata:
  name: review-team

lead:
  agent: ../agents/review-lead.agent.yaml

workers:
  reviewer:
    agent: ../agents/reviewer.agent.yaml
    maxTasks: 3
  verifier:
    agent: ../agents/verifier.agent.yaml
    maxTasks: 2
  writer:
    agent: ../agents/writer.agent.yaml
    maxTasks: 1

runtime:
  topology: lead_worker
  maxRounds: 6
  maxTasks: 8
  maxParallel: 2
  maxRetriesPerTask: 1

verification:
  requireStructuredTaskOutput: true
  requireVerifier: true
  requiredTaskFields: [summary, findings, evidence, gaps, residual_risk]

output:
  dir: ../.jeju-dev/team/review-team
```

Validate and smoke-run a team from the Jeju source checkout:

```bash
jeju validate <bundle>/teams/<name>.team.yaml
jeju team run --workspace /path/to/project --output final <bundle>/teams/<name>.team.yaml "<sample goal>"
```

## Minimal Manifest Template

```yaml
apiVersion: jeju/v1alpha1
kind: Agent

metadata:
  name: repo-inspector
  description: "Inspect a local repository and produce a structured summary"

models:
  providers:
    primary:
      type: openaiCompatible
      preset: deepseek
      model: deepseek-v4-flash
      envKey: DEEPSEEK_API_KEY
      temperature: 0.1
      thinking:
        type: disabled
      maxOutputTokens: 2048
      contextWindow: 128000
      timeoutSec: 60

instructions:
  system: ../prompts/repo-inspector.md

runtime:
  model: primary
  loop:
    type: react
  compressionThreshold: 0.8
  recentTokenBudget: 20000
  limits:
    maxSteps: 16
    maxDurationSec: 240
    maxToolCalls: 30
    maxConsecutiveErrors: 2

workspace:
  path: ../workspace/repo-inspector

tools:
  - read
  - search

permissions:
  access: readOnly
  approval: never

output:
  name: repo_summary
  schema:
    type: object
    required: [summary, findings, residual_risk]
    additionalProperties: false
    properties:
      summary:
        type: string
      findings:
        type: array
        items:
          type: string
      residual_risk:
        type: string

evaluate:
  enabled: true
  evaluators:
    - name: basic
      uses: rules
      rules:
        - finalAnswerExists
        - runCompleted
```

## Authoring Defaults

- Keep behavior in manifest fields, prompt files, tools, skills, permissions,
  and evaluators.
- `workspace.path` should point at a placeholder in the bundle; use
  `jeju run --workspace /path/to/project ...` for real target repos.
- Default to `permissions.access: readOnly` and `permissions.approval: never`
  for inspection agents.
- Use `permissions.access: workspace` and `permissions.approval: onRequest`
  only when the agent must write, run shell commands, or access networks.
- Declare tool capabilities accurately: `workspaceRead`, `workspaceWrite`,
  `command`, `networkRead`, `networkWrite`, or `agentRun`. Agent tools infer
  `agentRun` automatically.
- Make the system prompt narrow: exact workflow, output format, inspection
  strategy, residual-risk reporting, and non-goals.
- For structured final JSON, put the exact shape in manifest `output.schema`
  and keep the prompt focused on workflow and field semantics. Do not duplicate
  a large JSON Schema block in the prompt unless a weak provider needs a short
  reminder.
- `output.schema` currently supports inline JSON Schema. Jeju validates final
  output locally, retries once with tools disabled if the final answer does not
  match, and fails the run if the retry also violates the schema.
- Include at least a smoke evaluator when practical.

## Install Jeju If Needed

Before validating or running a generated agent, check whether Jeju is available:

```bash
jeju version
```

If the command is missing, use the README binary install command on macOS or
Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/cosmtrek/jeju/master/scripts/install.sh | sh
jeju version
```

Jeju currently supports macOS and Linux. Windows is not guaranteed yet. Use the
README source install path only when Go is already installed:

```bash
go install github.com/cosmtrek/jeju/cmd/jeju@latest
jeju version
```

Do not install Go automatically. Ask the user before taking the source-install
path when Go is missing.

## Validation

From the Jeju source checkout, write local run artifacts under `.jeju-dev/`:

```bash
jeju validate <bundle>/agents/<name>.agent.yaml
jeju run --runs-dir .jeju-dev/runs/<name> <bundle>/agents/<name>.agent.yaml "<sample input>"
jeju view --runs-dir .jeju-dev/runs/<name>
jeju view --runs-dir .jeju-dev/runs/<name> <run_id>
```

For distributable bundles, validate both the agent and the package before
packing or sharing:

```bash
jeju package validate <bundle>
jeju package pack <bundle> --out .jeju-dev/packages/<name>
jeju package add .jeju-dev/packages/<name>/<namespace-name>-<version>.jpkg
jeju run --runs-dir .jeju-dev/runs/<name> package://<namespace/name>@<version> "<sample input>"
jeju view --runs-dir .jeju-dev/runs/<name> package://<namespace/name>@<version>
```

When only adding or editing authoring docs, `go test ./...` is not required
unless source code changed.

## Source Lookup Advice

If repository access is available, inspect source and docs before inventing
fields or behavior. Prefer conceptual search terms over hardcoded paths because
the repository can be reorganized:

```bash
rg -n "AgentManifest|ToolConfig|finalAnswerExists|workspace.path|permissions.access|compressionThreshold"
rg -n "config.LoadFile|config.Validate|compiler.Compile|runtime.Run"
rg -n "uses: command|uses: http|allowed-tools|active skills|evaluate|output.schema"
rg -n "AgentPackage|jeju.package.yaml|package add|package://|JEJU_REGISTRY_INDEX"
rg -n "uses: agent|agentRun|AgentToolConfig|SpanSubagent|child-runs|code-review-with-agent-tools"
rg -n "AgentTeam|TeamDecision|finish.task_id|lead.synthesisAgent|lead_worker|jeju team run"
```

Look for current docs, schema structs, defaults, validators, compiler behavior,
runtime loop behavior, policy checks, tool implementations, skill loading,
evaluator behavior, evolve behavior, and concrete examples.
