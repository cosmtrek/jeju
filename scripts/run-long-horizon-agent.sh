#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
provider="${JEJU_LONG_HORIZON_PROVIDER:-mimo}"

case "${1:-}" in
  deepseek|mimo)
    provider="$1"
    shift
    ;;
  "" )
    ;;
  * )
    echo "unsupported provider: $1" >&2
    echo "supported providers: deepseek, mimo" >&2
    exit 1
    ;;
esac

case "$provider" in
  deepseek)
    env_key="${JEJU_DEEPSEEK_ENV_KEY:-DEEPSEEK_API_KEY}"
    model="${JEJU_DEEPSEEK_MODEL:-deepseek-v4-flash}"
    base_url=""
    ;;
  mimo)
    env_key="${JEJU_MIMO_ENV_KEY:-MIMO_API_KEY}"
    model="${JEJU_MIMO_MODEL:-mimo-v2.5-pro}"
    base_url="${JEJU_MIMO_BASE_URL:-https://api.xiaomimimo.com/v1}"
    ;;
esac

if [[ -z "${!env_key:-}" ]]; then
  echo "missing $provider API key env: $env_key" >&2
  exit 1
fi

workdir="${JEJU_LONG_HORIZON_WORKDIR:-"$repo_root/.jeju-dev/long-horizon-$provider-agent-run"}"
manifest="agents/long-horizon.yaml"
task="${1:-Run the long-horizon compression audit exactly as instructed by the active skill.}"
context_window="${JEJU_LONG_HORIZON_CONTEXT_WINDOW:-16000}"
threshold="${JEJU_LONG_HORIZON_THRESHOLD:-0.25}"
paragraphs="${JEJU_LONG_HORIZON_PARAGRAPHS:-12}"

rm -rf "$workdir"
mkdir -p "$workdir"
cp -R "$repo_root/tests/fixtures/long-horizon/." "$workdir/"

JEJU_SCRIPT_PROVIDER="$provider" JEJU_SCRIPT_MODEL="$model" JEJU_SCRIPT_ENV_KEY="$env_key" JEJU_SCRIPT_BASE_URL="$base_url" perl -0pi -e '
  s/type: mock/"type: openaiCompatible\n      preset: ".$ENV{JEJU_SCRIPT_PROVIDER}/e;
  s/model: mock-react/"model: ".$ENV{JEJU_SCRIPT_MODEL}/e;
  s/(model: .+\n)/$1      envKey: $ENV{JEJU_SCRIPT_ENV_KEY}\n/;
  if ($ENV{JEJU_SCRIPT_BASE_URL}) {
    s/(envKey: .+\n)/$1      baseUrl: $ENV{JEJU_SCRIPT_BASE_URL}\n/;
  }
' "$workdir/$manifest"

JEJU_SCRIPT_CONTEXT_WINDOW="$context_window" JEJU_SCRIPT_THRESHOLD="$threshold" JEJU_SCRIPT_PARAGRAPHS="$paragraphs" perl -0pi -e '
  s/contextWindow: \d+/contextWindow: $ENV{JEJU_SCRIPT_CONTEXT_WINDOW}/;
  s/compressionThreshold: [0-9.]+/compressionThreshold: $ENV{JEJU_SCRIPT_THRESHOLD}/;
  s/default: 12/default: $ENV{JEJU_SCRIPT_PARAGRAPHS}/;
' "$workdir/$manifest"

chmod +x "$workdir/workspace/long-horizon/tools/chapter_probe.py"

echo "==> Provider: $provider"
echo "==> Model: $model"
echo "==> Workdir: $workdir"
echo "==> Context window: $context_window"
echo "==> Compression threshold: $threshold"
echo "==> Chapter payload paragraphs: $paragraphs"

(
  cd "$workdir"
  go run "$repo_root/cmd/jeju" validate "$manifest"
  go run "$repo_root/cmd/jeju" run "$manifest" "$task"

  run_id="$(find runs -mindepth 1 -maxdepth 1 -type d -name '20*' -exec basename {} \; | sort | tail -n 1)"
  if [[ -z "$run_id" ]]; then
    echo "no run directory found" >&2
    exit 1
  fi

  go run "$repo_root/cmd/jeju" inspect "$run_id"

  trajectory="runs/$run_id/trajectory.jsonl"
  report="workspace/long-horizon/reports/long-horizon-summary.md"
  echo
  echo "Run dir: $workdir/runs/$run_id"
  echo "Trajectory: $workdir/$trajectory"
  echo "Report: $workdir/$report"

  python3 - "$trajectory" <<'PY'
import json
import sys

trajectory = sys.argv[1]
compactions = []
summary_spans = []
compressed_reports = []
summary_reports = []

with open(trajectory, encoding="utf-8") as f:
    for line in f:
        line = line.strip()
        if not line:
            continue
        event = json.loads(line)
        payload = event.get("payload") or {}
        if (
            event.get("type") == "span.ended"
            and payload.get("kind") == "context"
            and payload.get("operation") == "compaction"
            and payload.get("status") == "ok"
        ):
            compactions.append(event)
        if (
            event.get("type") == "span.ended"
            and payload.get("kind") == "llm"
            and (payload.get("attrs") or {}).get("operation") == "context_summary"
            and payload.get("status") == "ok"
        ):
            summary_spans.append(event)
        if event.get("type") == "artifact.created" and payload.get("role") == "context_report":
            report = payload.get("value") or {}
            if report.get("compressed"):
                compressed_reports.append(report)
                if "summary" in (report.get("strategies") or []):
                    summary_reports.append(report)

if not compactions:
    raise SystemExit("expected context compaction span in trajectory")
if not compressed_reports:
    raise SystemExit("expected compressed context report in trajectory")
if not summary_spans:
    raise SystemExit("expected context summary LLM span in trajectory")
if not summary_reports:
    raise SystemExit("expected compressed context report with summary strategy")
if not any((report.get("recent_token_budget") or 0) > 0 for report in compressed_reports):
    raise SystemExit("expected recent_token_budget in compressed context report")

print(
    "Context compression checks: "
    f"compactions={len(compactions)} "
    f"summaries={len(summary_spans)} "
    f"compressed_reports={len(compressed_reports)}"
)
PY

  if [[ ! -s "$report" ]]; then
    echo "expected report file: $report" >&2
    exit 1
  fi
  for checkpoint in \
    CHK-01-137 CHK-02-274 CHK-03-411 CHK-04-548 \
    CHK-05-685 CHK-06-822 CHK-07-959 CHK-08-099
  do
    if ! grep -q "$checkpoint" "$report"; then
      echo "expected checkpoint $checkpoint in report" >&2
      exit 1
    fi
  done
)
