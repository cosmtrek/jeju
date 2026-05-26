# Jeju

Jeju is a local-first, config-defined mini agent runtime written in Go.

It loads an Agent Manifest, validates and compiles it, runs a single-agent ReAct loop, writes JSONL trajectory, stores run artifacts, applies tool permissions, and evaluates completed runs with rule-based checks.

The V0 technical spec is in [docs/spec-v0.md](docs/spec-v0.md).
DeepSeek setup notes are in [docs/deepseek.md](docs/deepseek.md).

## Quick Start

```bash
go run ./cmd/jeju --help
go run ./cmd/jeju init research --dir .jeju-dev
cd .jeju-dev
go run ../cmd/jeju validate agents/research.agent.yaml
printf 'y\n' | go run ../cmd/jeju run agents/research.agent.yaml "写一份关于 AgentOps 的简短分析，并保存到 notes.md"
go run ../cmd/jeju runs
go run ../cmd/jeju inspect <run_id>
```

The generated agent uses the `mock` model by default so the V0 lifecycle can run without API credentials. `init --dir` writes a separate Jeju working directory, so quick-start agents, workspaces, run directories, and skill fixtures stay separate from source code. Run `validate`, `run`, `runs`, and `inspect` from that generated directory. Change the model provider to `openai_compatible` in the manifest to use a real Chat Completions-compatible endpoint.

## V0 Scope

- Manifest YAML loading, defaults, env interpolation, validation
- Config to `CompiledAgent`
- Single-agent ReAct runtime
- Mock model and OpenAI-compatible model client
- Builtin `file_read`, `file_write`, and `shell`
- Basic command tool protocol
- Permission and risk gate with interactive approval
- Skill disclosure plus manual active skill loading
- Local sandbox restricted to the configured workspace
- JSONL trajectory with console and file sinks
- Run metadata, config snapshot, final output, artifacts
- Rule-based evaluation
- `init`, `validate`, `run`, `inspect`, and `runs` CLI commands

## Tests

```bash
go test ./...
```

The full-path fixture agent is under `tests/fixtures/basic-agent/`. The test copies it into a temporary directory before running, so fixture sources stay clean.

To run the fixture agent locally end to end:

```bash
make test-agent
```

Or run the script directly with an optional task:

```bash
./scripts/run-basic-agent.sh "write a brief AgentOps note and save it to notes.md"
```

The script writes local output under `.jeju-dev/basic-agent-run/`.

To test with DeepSeek V4 Flash:

```bash
export DEEPSEEK_API_KEY=sk-...
make test-agent-deepseek
```
