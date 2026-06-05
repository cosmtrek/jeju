#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
suite_root="$repo_root/benchmarks/jeju-bfcl-lite"
work_root="$repo_root/.jeju-dev/bfcl-lite"
task_filter=""
provider="${JEJU_BENCHMARK_PROVIDER:-${PROVIDER:-deepseek}}"
model=""
env_key=""
base_url=""

usage() {
  cat <<'USAGE'
Usage: ./scripts/run-bfcl-lite-benchmark.sh [--task TASK_NAME] [--workdir DIR]

Runs the Jeju BFCL Lite benchmark.

Environment:
  PROVIDER=deepseek|mimo
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --task)
      task_filter="${2:-}"
      shift 2
      ;;
    --workdir)
      work_root="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

case "$provider" in
  deepseek)
    model="${JEJU_DEEPSEEK_MODEL:-deepseek-v4-flash}"
    env_key="${JEJU_DEEPSEEK_ENV_KEY:-DEEPSEEK_API_KEY}"
    ;;
  mimo)
    model="${JEJU_MIMO_MODEL:-mimo-v2.5-pro}"
    env_key="${JEJU_MIMO_ENV_KEY:-MIMO_API_KEY}"
    base_url="${JEJU_MIMO_BASE_URL:-https://api.xiaomimimo.com/v1}"
    ;;
  *)
    echo "supported benchmark providers: deepseek, mimo" >&2
    exit 2
    ;;
esac

if [[ -z "${!env_key:-}" ]]; then
  echo "$env_key is required for the $provider bfcl-lite benchmark agent." >&2
  exit 2
fi

tasks=(
  simple-single-tool
  multiple-tool-choice
  irrelevance-no-call
)

if [[ -n "$task_filter" ]]; then
  found=0
  for task in "${tasks[@]}"; do
    if [[ "$task" == "$task_filter" ]]; then
      found=1
    fi
  done
  if [[ "$found" -ne 1 ]]; then
    echo "unknown task: $task_filter" >&2
    exit 2
  fi
  tasks=("$task_filter")
fi

mkdir -p "$work_root"
(
  cd "$repo_root"
  go build -o "$work_root/jeju" ./cmd/jeju
)

pass_count=0
failed_tasks=()
for task in "${tasks[@]}"; do
  echo "==> $task"
  run_dir="$work_root/$task"
  rm -rf "$run_dir"
  mkdir -p "$run_dir"

  cp -R "$suite_root/agents" "$run_dir/agents"
  cp -R "$suite_root/prompts" "$run_dir/prompts"
  JEJU_SCRIPT_PROVIDER="$provider" JEJU_SCRIPT_MODEL="$model" JEJU_SCRIPT_ENV_KEY="$env_key" JEJU_SCRIPT_BASE_URL="$base_url" perl -0pi -e '
    s/preset: \w+/preset: $ENV{JEJU_SCRIPT_PROVIDER}/;
    s/model: [^\n]+/model: $ENV{JEJU_SCRIPT_MODEL}/;
    s/envKey: [^\n]+/envKey: $ENV{JEJU_SCRIPT_ENV_KEY}/;
    if ($ENV{JEJU_SCRIPT_BASE_URL}) {
      if (/baseUrl:/) {
        s/baseUrl: [^\n]+/baseUrl: $ENV{JEJU_SCRIPT_BASE_URL}/;
      } else {
        s/(envKey: [^\n]+\n)/$1      baseUrl: $ENV{JEJU_SCRIPT_BASE_URL}\n/;
      }
    }
  ' "$run_dir/agents/benchmark.agent.yaml"
  mkdir -p "$run_dir/workspace"
  cp -R "$suite_root/workspace" "$run_dir/workspace/app"

  task_prompt="$(cat "$suite_root/tasks/$task/task.md")"
  task_failed=0
  (
    cd "$run_dir"
    "$work_root/jeju" run agents/benchmark.agent.yaml "$task_prompt"
  ) || task_failed=1

  latest_run="$(find "$run_dir/runs" -mindepth 1 -maxdepth 1 -type d | sort | tail -n 1)"
  python3 - "$latest_run" "$suite_root/tasks/$task/expected.json" <<'PY' || task_failed=1
import json
import sys
from pathlib import Path

run_dir = Path(sys.argv[1])
expected = json.loads(Path(sys.argv[2]).read_text())

for name in ["trajectory.jsonl", "report.html"]:
    if not (run_dir / name).exists():
        raise SystemExit(f"missing run artifact: {name}")

events = [json.loads(line) for line in (run_dir / "trajectory.jsonl").read_text().splitlines() if line.strip()]

summary = next((event.get("payload", {}) for event in events if event.get("type") == "run.summary"), {})
if summary.get("status") != "completed":
    raise SystemExit(f"run did not complete: {summary.get('status')}")

def artifact_text(role):
    for event in events:
        if event.get("type") != "artifact.created":
            continue
        payload = event.get("payload", {})
        if payload.get("role") == role:
            return payload.get("text", "")
    return ""

evaluation_text = artifact_text("evaluation")
evaluation = json.loads(evaluation_text) if evaluation_text else {}
if not evaluation.get("passed"):
    raise SystemExit(f"evaluation did not pass: {evaluation}")

requested = [
    event.get("payload", {}).get("function_name")
    for event in events
    if event.get("type") == "action.created" and event.get("payload", {}).get("kind") == "tool_call"
]
expected_tools = [item["tool"] for item in expected.get("expected_tool_calls", [])]
if requested != expected_tools:
    raise SystemExit(f"expected tool calls {expected_tools}, got {requested}")

for forbidden in expected.get("forbidden_tools", []):
    if forbidden in requested:
        raise SystemExit(f"forbidden tool was called: {forbidden}")

final = artifact_text("final")
missing = [text for text in expected.get("expected_final_contains", []) if text.lower() not in final.lower()]
if missing:
    raise SystemExit(f"final answer missing expected text: {missing}; final={final!r}")
PY

  if [[ "$task_failed" -eq 0 ]]; then
    pass_count=$((pass_count + 1))
  else
    failed_tasks+=("$task")
  fi
done

echo "PASS $pass_count/${#tasks[@]} bfcl-lite tasks"
if [[ "${#failed_tasks[@]}" -gt 0 ]]; then
  echo "FAIL ${#failed_tasks[@]}/${#tasks[@]} bfcl-lite tasks: ${failed_tasks[*]}" >&2
  exit 1
fi
