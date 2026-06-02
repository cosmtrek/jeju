# Jeju Agent Coding Notes

## Project Shape

- Jeju is a Go 1.25 experimental local-first agent harness.
- Keep implementation under `internal/`. Do not add `pkg/` until a public API is intentionally stabilized.
- Agent manifest docs live in `docs/agent-manifest.md`; evolution experiment docs live in `docs/agent-evolution-manifest.md`.
- DeepSeek setup notes live in `docs/deepseek.md`.
- CLI entrypoint is `cmd/jeju/main.go`; command handlers live in `internal/cli/`.

## Scope Rules

- Keep the core runtime scope tight: no Web UI, multi-agent runtime, Docker sandbox, remote sandbox, long-term memory, distributed workers, or full MCP client unless explicitly requested.
- Runtime must not read YAML directly. The path is `config.LoadFile -> config.Validate -> compiler.Compile -> runtime.Run`.
- Agent behavior should come from manifest config, loaded instructions, tools, skills, policy, sandbox, trajectory, and evaluator config rather than hardcoded runtime branches.

## Runtime Invariants

- Every run creates a run directory through `runs.Store`.
- Every run saves `metadata.json`, `config.snapshot.yaml`, `trajectory.jsonl`, and `final.md`; when evaluation is enabled it also saves `evaluation.json`.
- Trajectory is JSONL. Record model calls, tool calls, permission decisions, skill events, artifacts, evaluation, and run lifecycle events.
- Large model/tool payloads go under run `artifacts/`; events should store artifact refs instead of large content.
- All tool calls must pass through `policy.Gate` before execution.
- File tools must stay inside the configured local workspace. Shell runs must use the sandbox workdir and enforce timeout.
- Skills use disclosure plus manual active loading. Do not inject all skill assets by default.

## Generated Files

- `jeju init <name>` is allowed to scaffold into the current directory, but tests and local quick-start runs should use `jeju init <name> --dir <workdir>`.
- Do not leave temporary scaffold output such as `agents/research.agent.yaml`, `prompts/research.md`, `skills/web-research`, `runs/<run_id>`, or `workspace/<agent>` in the repo root unless the task explicitly asks to add source fixtures.
- `.jeju-dev/` is the preferred ignored local quick-start directory.
- In the Jeju source checkout, local development runs, demo runs, benchmark outputs, and evolution outputs must be written under repo-root `.jeju-dev/`, for example `--runs-dir .jeju-dev/runs/<scenario>` or `--out .jeju-dev/evolve/<scenario>`.
- Keep user project defaults separate from source-checkout hygiene: `./runs` is acceptable inside a generated agent project, but source-repo scripts and examples should not create root `runs/` or example-local `.jeju-dev/` directories.

## Path Hygiene

- Do not commit machine-local absolute paths from a developer home directory. Prefer neutral placeholders such as `/path/to/project`; use repo-relative paths such as `scripts/...` when the example is already rooted at the Jeju checkout.

## Verification

- Run `go test ./...` after code changes.
- Run `go vet ./...` for runtime/compiler/tooling changes.
- Use `make test-agent` or `./scripts/run-agent.sh mock` for a local one-command fixture agent run.
- Use `make test-agent PROVIDER=deepseek` only when `DEEPSEEK_API_KEY` or `JEJU_DEEPSEEK_ENV_KEY` is intentionally set; it calls the real DeepSeek API.
- Use `make test-agent PROVIDER=mimo` only when `MIMO_API_KEY` or `JEJU_MIMO_ENV_KEY` is intentionally set; it calls the real MiMo API.
- Keep the core smoke test in `internal/cli/core_flow_test.go` fast and isolated with `t.TempDir()`. It should continue covering `init --dir -> validate -> run -> runs -> inspect` plus run artifacts and key trajectory events.
- `tests/fixtures/agent/` is the static full agent fixture for full-path testing. Tests must copy it into `t.TempDir()` before running so fixture sources do not accumulate `runs/` or `workspace/` outputs.
