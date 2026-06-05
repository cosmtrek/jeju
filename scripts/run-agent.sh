#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
provider="${JEJU_AGENT_PROVIDER:-mock}"

case "${1:-}" in
  mock|deepseek|mimo)
    provider="$1"
    shift
    ;;
esac

task="${1:-write a brief AgentOps note and save it to notes.md}"
upper_provider="$(printf '%s' "$provider" | tr '[:lower:]' '[:upper:]')"
provider_workdir_var="JEJU_${upper_provider}_AGENT_WORKDIR"
workdir="${JEJU_AGENT_WORKDIR:-${!provider_workdir_var:-"$repo_root/.jeju-dev/$provider-agent-run"}}"

case "$provider" in
  mock)
    model="mock-react"
    ;;
  deepseek)
    env_key="${JEJU_DEEPSEEK_ENV_KEY:-DEEPSEEK_API_KEY}"
    model="deepseek-v4-flash"
    ;;
  mimo)
    env_key="${JEJU_MIMO_ENV_KEY:-MIMO_API_KEY}"
    model="mimo-v2.5-pro"
    base_url="${JEJU_MIMO_BASE_URL:-https://api.xiaomimimo.com/v1}"
    ;;
  *)
    echo "unsupported provider: $provider" >&2
    echo "supported providers: mock, deepseek, mimo" >&2
    exit 1
    ;;
esac

fixture="agent"
manifest="agents/agent.yaml"
note_path="workspace/agent/notes.md"

if [[ "$provider" != "mock" && -z "${!env_key:-}" ]]; then
  echo "missing $provider API key env: $env_key" >&2
  echo "example: export $env_key=sk-..." >&2
  exit 1
fi

rm -rf "$workdir"
mkdir -p "$workdir"
cp -R "$repo_root/tests/fixtures/$fixture/." "$workdir/"

if [[ "$provider" != "mock" ]]; then
  JEJU_SCRIPT_PROVIDER="$provider" JEJU_SCRIPT_MODEL="$model" JEJU_SCRIPT_ENV_KEY="$env_key" JEJU_SCRIPT_BASE_URL="${base_url:-}" perl -0pi -e '
    s/type: mock/"type: openaiCompatible\n      preset: ".$ENV{JEJU_SCRIPT_PROVIDER}/e;
    s/model: mock-react/"model: ".$ENV{JEJU_SCRIPT_MODEL}/e;
    s/(model: .+\n)/$1      envKey: $ENV{JEJU_SCRIPT_ENV_KEY}\n/;
    if ($ENV{JEJU_SCRIPT_BASE_URL}) {
      s/(envKey: .+\n)/$1      baseUrl: $ENV{JEJU_SCRIPT_BASE_URL}\n/;
    }
  ' "$workdir/$manifest"
fi

echo "==> Provider: $provider"
echo "==> Workdir: $workdir"
if [[ "$provider" != "mock" ]]; then
  echo "==> Env key: $env_key"
fi

(
  cd "$workdir"
  go run "$repo_root/cmd/jeju" validate "$manifest"
  printf 'y\n' | go run "$repo_root/cmd/jeju" run "$manifest" "$task"
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
  echo "Workspace note: $workdir/$note_path"

  if ! python3 - "runs/$run_id/trajectory.jsonl" <<'PY'
import json
import sys
from pathlib import Path

status = None
for line in Path(sys.argv[1]).read_text().splitlines():
    if not line.strip():
        continue
    event = json.loads(line)
    if event.get("type") == "run.summary":
        status = event.get("payload", {}).get("status")
if status != "completed":
    raise SystemExit(1)
PY
  then
    echo "run did not complete successfully: $run_id" >&2
    exit 1
  fi
)
