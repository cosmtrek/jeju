#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
agent_dir="$repo_root/catalog/coding/code-review"
manifest="agents/code-review.agent.yaml"
bin="$repo_root/.jeju-dev/bin/jeju"
runs_dir="$repo_root/.jeju-dev/runs/code-review"
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

validate_output="$("$bin" validate "$agent_dir/$manifest")"
printf '%s\n' "$validate_output" >&2
(
  cd "$target_workspace"
  final_output="$("$bin" run --output final --runs-dir "$runs_dir" --workspace "$target_workspace" "$agent_dir/$manifest" "Review the current repository workspace changes. Use read-only Git and file inspection tools, then return the JSON review result directly.")"
  printf '%s\n' "$final_output" | python3 -c '
import json
import sys

text = sys.stdin.read().strip()

try:
    parsed = json.loads(text)
except json.JSONDecodeError:
    print("code-review final output was not valid JSON", file=sys.stderr)
    print(text, file=sys.stderr)
    raise SystemExit(1)

print(json.dumps(parsed, ensure_ascii=False, separators=(",", ":")))
'
)
