#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
agent_dir="$repo_root/examples/bug-rescue-agent"
fixture_dir="$agent_dir/fixtures/buggy-ledger"
manifest="$agent_dir/agents/bug-rescue.agent.yaml"
bin="${JEJU_BIN:-}"
workspace="$repo_root/.jeju-dev/workspaces/bug-rescue"
runs_dir="$repo_root/.jeju-dev/runs/bug-rescue"
env_key="${JEJU_DEEPSEEK_ENV_KEY:-DEEPSEEK_API_KEY}"

if [[ -z "${!env_key:-}" ]]; then
  echo "missing DeepSeek API key env: $env_key" >&2
  echo "example: export $env_key=sk-..." >&2
  exit 1
fi

rm -rf "$workspace"
mkdir -p "$workspace" "$repo_root/.jeju-dev/bin" "$runs_dir"
cp -R "$fixture_dir/." "$workspace/"

if [[ -z "$bin" ]]; then
  if command -v jeju >/dev/null 2>&1; then
    bin="$(command -v jeju)"
  else
    bin="$repo_root/.jeju-dev/bin/jeju"
    (
      cd "$repo_root"
      go build -o "$bin" ./cmd/jeju
    )
  fi
fi

echo "==> Workspace: $workspace"
echo "==> Initial test result:"
(
  cd "$workspace"
  python3 test_harness.py
)

"$bin" validate "$manifest" >&2

"$bin" run \
  --runs-dir "$runs_dir" \
  --workspace "$workspace" \
  "$manifest" \
  "Fix the failing Orbital Ledger tests with the smallest safe implementation change, rerun the tests, and write REPAIR.md with a concise repair note."

run_id="$(find "$runs_dir" -mindepth 1 -maxdepth 1 -type d -name '20*' -exec basename {} \; | sort | tail -n 1)"
if [[ -z "$run_id" ]]; then
  echo "no run directory found in $runs_dir" >&2
  exit 1
fi

echo
echo "Run dir: $runs_dir/$run_id"
echo "Report: $runs_dir/$run_id/report.html"
echo "Workspace: $workspace"
echo "View: $bin view --runs-dir $runs_dir $run_id"
