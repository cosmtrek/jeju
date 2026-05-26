#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workdir="${JEJU_DEEPSEEK_AGENT_WORKDIR:-"$repo_root/.jeju-dev/deepseek-agent-run"}"
task="${1:-write a brief AgentOps note and save it to notes.md}"
env_key="${JEJU_DEEPSEEK_ENV_KEY:-DEEPSEEK_API_KEY}"

if [[ -z "${!env_key:-}" ]]; then
  echo "missing DeepSeek API key env: $env_key" >&2
  echo "example: export $env_key=sk-..." >&2
  exit 1
fi

rm -rf "$workdir"
mkdir -p "$workdir"
cp -R "$repo_root/tests/fixtures/deepseek-agent/." "$workdir/"

if [[ "$env_key" != "DEEPSEEK_API_KEY" ]]; then
  perl -0pi -e "s/env_key: DEEPSEEK_API_KEY/env_key: $env_key/" "$workdir/agents/deepseek.agent.yaml"
fi

echo "==> Workdir: $workdir"
echo "==> Env key: $env_key"
(
  cd "$workdir"
  go run "$repo_root/cmd/jeju" validate agents/deepseek.agent.yaml
  printf 'y\n' | go run "$repo_root/cmd/jeju" run agents/deepseek.agent.yaml "$task"
  go run "$repo_root/cmd/jeju" runs

  run_id="$(find runs -mindepth 1 -maxdepth 1 -type d -name '20*' -exec basename {} \; | sort | tail -n 1)"
  if [[ -z "$run_id" ]]; then
    echo "no run directory found" >&2
    exit 1
  fi
  go run "$repo_root/cmd/jeju" inspect "$run_id"
  go run "$repo_root/cmd/jeju" view "$run_id"

  echo
  echo "Run dir: $workdir/runs/$run_id"
  echo "Report: $workdir/runs/$run_id/report.html"
  echo "Workspace note: $workdir/workspace/deepseek/notes.md"
)
