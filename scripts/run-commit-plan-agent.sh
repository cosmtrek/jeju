#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
agent_dir="$repo_root/examples/commit-plan-agent"
manifest="agents/commit-plan.agent.yaml"
bin="$repo_root/.jeju-dev/bin/jeju"
runs_dir="$repo_root/.jeju-dev/runs/commit-plan"
target_workspace="${1:-$(pwd)}"

env_key="${JEJU_DEEPSEEK_ENV_KEY:-DEEPSEEK_API_KEY}"
if [[ -z "${!env_key:-}" ]]; then
  echo "missing DeepSeek API key env: $env_key" >&2
  echo "example: export $env_key=sk-..." >&2
  exit 1
fi

if [[ $# -gt 1 ]]; then
  echo "usage: ./scripts/run-commit-plan-agent.sh [workspace]" >&2
  echo "The agent reads the target repository changes through read-only git tools." >&2
  exit 1
fi

target_workspace="$(cd "$target_workspace" && pwd)"

if ! git -C "$target_workspace" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "workspace is not a git repository: $target_workspace" >&2
  exit 1
fi

if git -C "$target_workspace" diff --quiet &&
  git -C "$target_workspace" diff --cached --quiet &&
  [[ -z "$(git -C "$target_workspace" ls-files --others --exclude-standard)" ]]; then
  echo "no changes to plan" >&2
  exit 1
fi

mkdir -p "$repo_root/.jeju-dev/bin"
(
  cd "$repo_root"
  go build -o "$bin" ./cmd/jeju
)

"$bin" validate "$agent_dir/$manifest" >&2
(
  cd "$target_workspace"
  "$bin" run --output final --runs-dir "$runs_dir" --workspace "$target_workspace" "$agent_dir/$manifest" "Cluster the current repository workspace changes into reviewable commit themes. Use read-only Git and file inspection tools, then return the JSON commit plan directly."
)
