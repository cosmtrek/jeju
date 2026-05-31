#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
agent_dir="$repo_root/usecases/code-review-agent"
manifest="agents/code-review.agent.yaml"
bin="$repo_root/.jeju-dev/bin/jeju"
target_workspace="${1:-$(pwd)}"

env_key="${JEJU_DEEPSEEK_ENV_KEY:-DEEPSEEK_API_KEY}"
if [[ -z "${!env_key:-}" ]]; then
  echo "missing DeepSeek API key env: $env_key" >&2
  echo "example: export $env_key=sk-..." >&2
  exit 1
fi

mkdir -p "$repo_root/.jeju-dev/bin"
(
  cd "$repo_root"
  go build -o "$bin" ./cmd/jeju
)

if [[ $# -gt 1 ]]; then
  echo "usage: ./scripts/run-code-review-agent.sh [workspace]" >&2
  echo "The agent reads the target repository changes through read-only git tools." >&2
  exit 1
fi

target_workspace="$(cd "$target_workspace" && pwd)"

if git -C "$target_workspace" diff --quiet && git -C "$target_workspace" diff --cached --quiet; then
  echo "no diff to review" >&2
  exit 1
fi

"$bin" validate "$agent_dir/$manifest"
(
  cd "$target_workspace"
  "$bin" run --workspace "$target_workspace" "$agent_dir/$manifest" "Review the current repository workspace changes. Use read-only Git and file inspection tools, then return the JSON review result directly."
)
