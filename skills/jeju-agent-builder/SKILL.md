---
name: jeju-agent-builder
description: Use when converting a repeated workflow, local developer task, or specialist agent idea into a minimal Jeju agent bundle with manifest, prompt, tools, permissions, evaluation, and smoke validation. This is for any higher-level AI agent that needs to author Jeju agents.
metadata:
  short-description: Create focused Jeju agents from repeated workflows
  version: "0.2.0"
---

# Jeju Agent Builder

Use this skill when the user wants an AI agent to turn a repeated workflow into
a Jeju agent. Jeju agents should be narrow, bounded execution units that another
higher-level agent can create, run, inspect, and improve.

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
  `command`, `networkRead`, or `networkWrite`.
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
jeju inspect --runs-dir .jeju-dev/runs/<name> <run_id>
```

For distributable bundles, validate both the agent and the package before
packing or sharing:

```bash
jeju package validate <bundle>
jeju package pack <bundle> --out .jeju-dev/packages/<name>
jeju package add .jeju-dev/packages/<name>/<namespace-name>-<version>.jpkg
jeju run --runs-dir .jeju-dev/runs/<name> package://<namespace/name>@<version> "<sample input>"
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
```

Look for current docs, schema structs, defaults, validators, compiler behavior,
runtime loop behavior, policy checks, tool implementations, skill loading,
evaluator behavior, evolve behavior, and concrete examples.
