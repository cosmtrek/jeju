# Agent Package

Agent Package is Jeju's distribution format for one reusable `kind: Agent`.
It lets users add, inspect, update, remove, and run agents by stable references while
keeping Jeju's runtime path unchanged:

```text
config.LoadFile -> config.Validate -> compiler.Compile -> runtime.Run
```

`AgentPackage` is for packaging and distribution. `jeju run` is the only
normal agent execution entrypoint.

## Scope

V1 supports:

- one `AgentPackage` per package root,
- one exported `kind: Agent` per package,
- local package initialization, validation, packing, adding, updating,
  inspecting, listing, and removing,
- GitHub and generic Git package sources through `package add` and `jeju run`,
- `jeju:` registry references backed by `JEJU_REGISTRY_INDEX`,
- content-addressed local storage,
- package run provenance,
- `jeju run` over local manifests, local package refs, and remote package
  sources.

V1 does not support:

- `AgentTeam` packages,
- packages exporting multiple agents,
- package dependencies,
- install hooks or postinstall scripts,
- package-managed runtime input schemas or task templates,
- remote execution, hosted sandboxes, or Web UI,
- marketplace features.

## Package Layout

The only fixed layout rule is that the package root contains
`jeju.package.yaml`.

The package protocol only interprets paths declared in `jeju.package.yaml` and
the referenced `kind: Agent` manifest. Jeju validates that declared paths stay
inside the package root. Packaging copies regular files from the package root
except local output/cache directories. Directory names such as `agents/`,
`prompts/`, `skills/`, or `eval/` are conventions, not protocol requirements.

Example:

```text
jeju.package.yaml
agent.yaml
instructions.md
workspace/.gitkeep
README.md
```

## Package Manifest

Package manifest filename:

```text
jeju.package.yaml
```

Minimal manifest:

```yaml
apiVersion: jeju/v1alpha1
kind: AgentPackage

metadata:
  id: coding/code-review
  version: 0.1.0
  description: "Review a local repository change and return structured findings."

agent:
  manifest: agent.yaml
```

Recommended optional metadata:

```yaml
metadata:
  id: coding/code-review
  name: Code Review
  version: 0.1.0
  description: "Review a local repository change and return structured findings."
  labels:
    domain: coding
    task: review
  license: MIT
  homepage: https://github.com/jeju-ai/agents/tree/main/coding/code-review
  repository: https://github.com/jeju-ai/agents

compatibility:
  jeju: ">=0.4.0 <0.6.0"

agent:
  manifest: agents/code-review.agent.yaml
```

Required fields:

| Field | Meaning |
| --- | --- |
| `apiVersion` | Must be `jeju/v1alpha1`. |
| `kind` | Must be `AgentPackage`. |
| `metadata.id` | Stable package ID. Prefer `namespace/agent-name`. |
| `metadata.version` | Semantic package version. |
| `metadata.description` | Short capability description. |
| `agent.manifest` | Relative path to one `kind: Agent` manifest. |

Optional fields:

| Field | Meaning |
| --- | --- |
| `metadata.name` | Human-readable name. |
| `metadata.labels` | Search and grouping labels. |
| `metadata.license` | License string. |
| `metadata.homepage` | Documentation or package page. |
| `metadata.repository` | Source repository URL, for display only. |
| `compatibility.jeju` | Supported Jeju CLI version range. |

Do not put `usage`, `quality`, `risk`, or `publish.source` in the package
manifest in V1.

- Usage belongs in `README.md`, examples, or registry display metadata.
- Quality status belongs in registry metadata, CI evidence, local eval output,
  or run provenance.
- Risk and permission summaries should be derived from the agent manifest and
  may be reviewed by the official registry.
- Source, commit, digest, add time, and active version belong in local store
  metadata, registry entries, and run provenance. A package file cannot
  self-certify where it came from.

`jeju package init` creates this file for an existing local agent. It should not
copy files or modify the referenced agent manifest:

```bash
jeju package init ./coding/code-review \
  --agent agent.yaml \
  --id coding/code-review \
  --version 0.1.0 \
  --description "Review a local repository change and return structured findings."
```

## Source References

`jeju package add` accepts package sources. Package-backed `jeju run` accepts
remote package sources plus installed `package://` refs or the short `p:`
alias; local directories and `.jpkg` artifacts should be added first.

```bash
jeju package add ./coding/code-review
jeju package add dist/coding-code-review-0.1.0.jpkg
jeju package add github:owner/repo//coding/code-review?ref=v0.1.0
jeju package add git+https://github.com/owner/repo.git//coding/code-review?ref=8f3a123
jeju package add jeju:coding/code-review@0.1.0

jeju run github:owner/repo//coding/code-review?ref=v0.1.0 "Review current diff."
jeju run git+https://github.com/owner/repo.git//coding/code-review?ref=8f3a123 "Review current diff."
jeju run jeju:coding/code-review@0.1.0 "Review current diff."
```

Reference rules:

- `github:owner/repo//subdir?ref=<ref>` resolves `subdir` as the package root.
- `git+https://...//subdir?ref=<ref>` uses the same subdir rule.
- `jeju:namespace/agent-name@version` resolves through the configured registry
  index.
- `p:namespace/agent-name[@version]` is a shorthand for the installed local
  package ref `package://namespace/agent-name[@version]`.
- Local directories and `.jpkg` artifacts are accepted by `jeju package add`.
- Tags and commits are preferred. Mutable refs such as `main` are allowed for
  experimentation but must be marked unstable in local metadata.
- All package-relative paths must stay inside the resolved package root.

## Official Agent Repository

The official Jeju agents repository should store multiple packages by
`namespace/agent-name`:

```text
jeju-ai/agents/
  coding/
    code-review/
      jeju.package.yaml
      agent.yaml
      instructions.md
      README.md
    test-writer/
      jeju.package.yaml
      agent.yaml
      instructions.md
      README.md
  research/
    paper-reader/
      jeju.package.yaml
      agent.yaml
      instructions.md
      README.md
```

Direct GitHub reference:

```bash
jeju run github:jeju-ai/agents//coding/code-review?ref=<commit> "Review current diff."
```

Official registry reference:

```bash
jeju run jeju:coding/code-review@0.1.0 "Review current diff."
```

The official registry maps package IDs and versions to concrete sources. The
local V1 resolver reads an index in one of these shapes:

```yaml
entries:
  - id: coding/code-review
    version: 0.1.0
    source: github:jeju-ai/agents//coding/code-review?ref=<commit>
    digest: sha256:9f1c...

packages:
  coding/code-review:
    versions:
      0.1.0:
        source: github:jeju-ai/agents//coding/code-review?ref=<commit>
        digest: sha256:9f1c...
```

The registry is metadata and resolution infrastructure. It is not the runtime
authority; `agent.yaml` remains the runtime truth.

Until a hosted registry endpoint is available, the CLI resolver may read a
registry index YAML from `JEJU_REGISTRY_INDEX`. The index must map package
`id/version` to a concrete source and optional digest. HTTP registry indexes are
read with bounded timeout and response size.

## Unified Run

`jeju run` accepts an agent reference:

```bash
jeju run [--model <model-id>] [--workspace <dir>] [--runs-dir <dir>] [--output live|final] [--from clipboard|stdin|<path>] <agent-ref> ["<task>"]
```

Supported agent refs:

```bash
# Local manifest.
jeju run agents/review.agent.yaml "Review current diff."

# Local package ref.
jeju run package://coding/code-review@0.1.0 "Review current diff."

# Active local version.
jeju run package://coding/code-review "Review current diff."

# Short local package ref.
jeju run p:coding/code-review "Review current diff."

# Override the active runtime provider's model ID for this run.
jeju run --model deepseek-v4 p:coding/code-review "Review current diff."

# Read task input from a source.
jeju run --from clipboard p:coding/code-review
jeju run --from clipboard p:coding/code-review "Translate to Chinese."
jeju run --from notes.md p:coding/code-review

# Remote package source, materialize then run.
jeju run github:jeju-ai/agents//coding/code-review?ref=<commit> "Review current diff."

# Official registry ref.
jeju run jeju:coding/code-review@0.1.0 "Review current diff."
```

Run behavior:

```text
resolve <agent-ref>
  -> find or fetch package when the ref is package-backed
  -> validate package content when fetching from a source
  -> lightly resolve metadata when using an installed package:// or p: ref
  -> use or store package content by digest
  -> resolve agent.manifest
  -> compile with normal run flags
  -> runtime.Run(task)
  -> record package provenance when package-backed
```

The resolved task string is passed unchanged to `runtime.Run`. `--from` reads
source text from clipboard, stdin, or a file. When a trailing task argument is
also provided, Jeju treats it as supplemental instructions for that source text
and builds the final task as the supplemental instructions, a blank line, then
the source text. Package metadata must not render, transform, or reinterpret the
task. `--model`, `--workspace`, `--output`, and `--from` keep the same semantics
for local manifests, local package refs, and remote package sources.

`--model <model-id>` changes only the `model` field of the provider selected by
`runtime.model`. It does not select another provider or override `type`,
`preset`, `baseUrl`, `envKey`, `temperature`, thinking settings, token limits,
or `contextWindow`. With multiple providers, all other provider entries remain
unchanged. The effective model ID is stored in the run's config snapshot and
model spans, while package ID, version, and digest provenance remain unchanged.
The override applies to the root agent only; separately compiled `uses: agent`
children keep their own manifests.

Package-backed runs default to `~/.jeju/runs` when neither `--runs-dir` nor
`JEJU_RUNS_DIR` is set, so invoking a package from an arbitrary working
directory does not create `./runs` there. Local manifest runs keep the generated
project convention of defaulting to `./runs`. `jeju view` and `jeju inspect`
search both the local and global default stores when no explicit run store is
configured. `jeju view <package-ref>` lists runs for one package.

A direct remote `jeju run` may materialize content into the local store and
record provenance, but it must not change the active unversioned package alias
unless the user explicitly runs `jeju package add` or `jeju package update`.

## Package Commands

Package commands manage local package content and metadata. They do not start
agent runs.

```bash
jeju package init ./coding/code-review --agent agent.yaml --id coding/code-review --version 0.1.0 --description "Review a local repository change and return structured findings."
jeju package validate ./coding/code-review
jeju package pack ./coding/code-review --out dist/
jeju package add ./coding/code-review
jeju package add dist/coding-code-review-0.1.0.jpkg
jeju package add github:jeju-ai/agents//coding/code-review?ref=<commit>
jeju package add jeju:coding/code-review@0.1.0
jeju package add ./coding/code-review --replace
jeju package update coding/code-review
jeju package update coding/code-review --version 0.2.0
jeju package update coding/code-review --replace
jeju package update --all
jeju package update --all --replace
jeju package ls
jeju package inspect coding/code-review
jeju package inspect coding/code-review --path
jeju package inspect coding/code-review --show-agent
jeju package rm coding/code-review
```

Command roles:

| Command | Role |
| --- | --- |
| `init` | Create `jeju.package.yaml` for an existing local agent. |
| `validate` | Check a package root without adding it. |
| `pack` | Create a local distributable artifact from a package root. |
| `add` | Resolve a local directory, local artifact, Git source, or `jeju:` ref into the local package store. |
| `update` | Re-resolve a package's saved source and update the active local ref when new content is available. |
| `ls` | List local package refs. |
| `inspect` | Show package metadata, agent entrypoint, local installation, resolved source, and derived risk summary. |
| `rm` | Remove local package refs. |

`package inspect` groups declared package information separately from local
installation state:

```yaml
package:
  id: coding/code-review
  version: 0.1.0
  description: Review a local repository change and return structured findings.
agent:
  manifest: agents/code-review.agent.yaml
installation:
  active: true
  path: /path/to/packages/store/sha256/9f1c...
  digest: sha256:9f1c...
risk:
  level: medium
  access: readOnly
  approval: never
source:
  requested: jeju:coding/code-review@0.1.0
  canonical: github:jeju-ai/agents//coding/code-review?ref=<commit>
  commit: <commit>
```

Use `--path` when another shell command needs only the installed package
directory:

```bash
cd "$(jeju package inspect coding/code-review --path)"
```

Use `--show-agent` to include the raw agent manifest under `agent.content`.
The installed directory is content-addressed execution material and should be
treated as read-only.

## Pack And Add

`jeju.package.yaml` in a package root is the authoring source of truth. `pack`
creates a distribution artifact from that source. `add` materializes any
supported source into the local package store.

```bash
jeju package pack ./coding/code-review --out dist/
jeju package add dist/coding-code-review-0.1.0.jpkg
```

`pack` must validate the package, copy only package-root content, exclude local
run output and caches, and compute the artifact digest. `add` must validate the
resolved package content, verify or compute its digest, store the package under
`store/sha256/<digest>/`, and create or update the local package ref. If the
same `metadata.id` and `metadata.version` resolve to different content, `add`
must fail unless the user passes `--replace`.

Regular file permission bits are part of the package digest and are preserved
when copying, packing, and extracting package content.

Adding a local artifact is the offline equivalent of adding a remote source.
`add` must not execute package code and must not trust artifact metadata that
claims a source, commit, or digest without local verification.

## Update

`jeju package update` re-resolves a package's saved source from `installed.yaml`
and updates the active local ref when new content is available:

```bash
jeju package update coding/code-review
jeju package update coding/code-review --version 0.2.0
jeju package update --all
jeju package update coding/code-review --replace
```

Immutable sources such as commit refs usually resolve to the same digest and
keep the same stored content. Mutable sources such as branches or `latest`
aliases are allowed but must stay marked unstable. If the same
`metadata.id` and `metadata.version` resolve to a different digest, Jeju should
refuse by default and require `--replace` instead of silently replacing content.

## Local Store

The local package store is content-addressed. Stored content is immutable by
digest; logical IDs and versions are local refs that point to stored content:

```text
~/.jeju/packages/
  store/
    sha256/
      9f1c.../
        jeju.package.yaml
        ...
      a83b.../
        jeju.package.yaml
        ...
  refs/
    coding/
      code-review/
        0.1.0 -> ../../../store/sha256/9f1c...
        0.2.0 -> ../../../store/sha256/a83b...
  installed.yaml
  cache/
```

`store/sha256/<digest>/` is the canonical execution source. `refs/` is a
best-effort convenience index from `package-id/version` to immutable content; it
may be a symlink or pointer file and must not be treated as the source of truth.
`cache/` is for Git clones, downloads, and temporary fetch state; it must not be
used as an execution source.

The default store root is `~/.jeju/packages`. Local automation and tests may
override it with `JEJU_PACKAGES_DIR`.

Example local metadata:

```yaml
packages:
  coding/code-review:
    active: 0.2.0
    versions:
      0.1.0:
        digest: sha256:9f1c...
        source: github:jeju-ai/agents//coding/code-review?ref=<commit>
        resolved:
          type: git
          commit: 8f3a1234567890abcdef
          unstable: false
      0.2.0:
        digest: sha256:a83b...
        source: github:jeju-ai/agents//coding/code-review?ref=<commit>
        resolved:
          type: git
          commit: c12d4567890abcdef123
          unstable: false
```

If the same `metadata.id` and `metadata.version` resolve to different digests,
Jeju must not silently overwrite the existing package. The initial behavior is
to reject the add or update unless the user explicitly passes `--replace`.

## Validation

`jeju package validate` checks:

- `jeju.package.yaml` parses and uses supported `apiVersion` and `kind`,
- required metadata fields are present and valid,
- `metadata.version` is semantic,
- `metadata.id` uses a stable package ID format such as `namespace/name`,
- `agent.manifest` is relative, stays inside package root, and points to a
  valid `kind: Agent`,
- the target agent passes `config.LoadFile`, `config.Validate`, and compile-time
  checks,
- all declared package paths stay inside package root,
- package files do not contain developer-home absolute paths,
- `compatibility.jeju`, when present, is satisfiable by the current CLI,
- no install hooks or executable package metadata are present.

Validation warns when:

- no README is present,
- no compatibility range is declared.

Source resolution separately marks mutable Git refs as `unstable` in local
metadata.

## Security

- Adding or validating a package must not execute package code.
- Package validation must not run package tools, evaluator commands, shell
  commands, or custom scripts.
- Package metadata must not contain secrets.
- Environment fields may declare variable names, but must not contain secret
  values.
- Remote Git packages must resolve to a revision and digest before storage or
  execution.
- Mutable refs must be visible as mutable.
- Runs continue to use existing `policy.Gate`.
- File and shell tools continue to be constrained by the compiled workspace and
  sandbox workdir.
- Package source resolution may use network access to fetch Git or registry
  content. Runtime network access remains controlled by the agent manifest tools
  and permissions.
- Live package-backed runs show derived permission and risk summaries before
  runtime execution starts.
