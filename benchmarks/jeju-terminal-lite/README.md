# Jeju Terminal Lite Benchmark

This is a Jeju-specific benchmark suite inspired by five public Terminal-Bench
2.0 task categories:

- `regex-log`
- `log-summary-date-ranges`
- `constraints-scheduling`
- `fix-git`
- `openssl-selfsigned-cert`

The fixtures and checkers in this directory are local Jeju fixtures, not copied
Terminal-Bench hidden benchmark data. The suite is intentionally small and
deterministic so it can be used as a development benchmark for Jeju's V0 runtime.

## Coverage

| Task | Main paths covered |
| --- | --- |
| `regex-log` | file write, deterministic checker |
| `log-summary-date-ranges` | multi-file read, data processing, CSV output |
| `constraints-scheduling` | read-only inputs, structured ICS output |
| `fix-git` | shell tool, git state mutation, permission gate |
| `openssl-selfsigned-cert` | shell tool, file permissions, generated artifacts |

Every task also checks that Jeju produced the expected run artifacts:

- `metadata.json`
- `config.snapshot.yaml`
- `trajectory.jsonl`
- `final.md`
- `evaluation.json`

## Run

Set `DEEPSEEK_API_KEY` for the default benchmark agent, then run:

```bash
./scripts/run-terminal-lite-benchmark.sh
```

To run one task:

```bash
./scripts/run-terminal-lite-benchmark.sh --task regex-log
```

The runner writes disposable output under `.jeju-dev/terminal-lite/`.
