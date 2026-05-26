#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workdir="${JEJU_BASIC_AGENT_WORKDIR:-"$repo_root/.jeju-dev/basic-agent-run"}"
task="${1:-write a brief AgentOps note and save it to notes.md}"

rm -rf "$workdir"
mkdir -p "$workdir"
cp -R "$repo_root/tests/fixtures/basic-agent/." "$workdir/"

echo "==> Workdir: $workdir"
(
  cd "$workdir"
  go run "$repo_root/cmd/jeju" validate agents/basic.agent.yaml
  printf 'y\n' | go run "$repo_root/cmd/jeju" run agents/basic.agent.yaml "$task"
  go run "$repo_root/cmd/jeju" runs

  run_id="$(find runs -mindepth 1 -maxdepth 1 -type d -name '20*' -exec basename {} \; | sort | tail -n 1)"
  if [[ -z "$run_id" ]]; then
    echo "no run directory found" >&2
    exit 1
  fi
  go run "$repo_root/cmd/jeju" inspect "$run_id"

  echo
  echo "Run dir: $workdir/runs/$run_id"
  echo "Workspace note: $workdir/workspace/basic/notes.md"
)
