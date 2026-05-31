#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
agent_dir="$repo_root/usecases/code-review-agent"
manifest="agents/code-review.agent.yaml"
bin="$repo_root/.jeju-dev/bin/jeju"

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

if [[ $# -ne 0 ]]; then
  echo "usage: ./scripts/run-code-review-agent.sh" >&2
  echo "The agent reads current repository changes through read-only git tools." >&2
  exit 1
fi

if git -C "$repo_root" diff --quiet && git -C "$repo_root" diff --cached --quiet; then
  echo "no diff to review" >&2
  exit 1
fi

"$bin" validate "$agent_dir/$manifest"
(
  cd "$agent_dir"
  "$bin" run "$manifest" "Review the current repository workspace changes. Use read-only Git and file inspection tools, then return the JSON review result directly."
)
