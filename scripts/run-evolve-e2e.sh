#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
provider="${JEJU_EVOLVE_PROVIDER:-mimo}"

case "${1:-}" in
  mock|mimo)
    provider="$1"
    shift
    ;;
esac

case "$provider" in
  mock)
    model="mock-react"
    ;;
  mimo)
    env_key="${JEJU_MIMO_ENV_KEY:-MIMO_API_KEY}"
    model="${JEJU_MIMO_MODEL:-mimo-v2.5-pro}"
    base_url="${JEJU_MIMO_BASE_URL:-https://api.xiaomimimo.com/v1}"
    if [[ -z "${!env_key:-}" ]]; then
      echo "missing Mimo API key env: $env_key" >&2
      echo "example: export $env_key=sk-..." >&2
      exit 1
    fi
    ;;
  *)
    echo "unsupported provider: $provider" >&2
    echo "supported providers: mock, mimo" >&2
    exit 1
    ;;
esac

workdir="${JEJU_EVOLVE_WORKDIR:-"$repo_root/.jeju-dev/evolve-e2e-$provider"}"
rm -rf "$workdir"
mkdir -p "$workdir"
cp -R "$repo_root/tests/fixtures/evolve-marker/." "$workdir/"

if [[ "$provider" == "mimo" ]]; then
  JEJU_SCRIPT_MODEL="$model" JEJU_SCRIPT_ENV_KEY="$env_key" JEJU_SCRIPT_BASE_URL="$base_url" perl -0pi -e '
    s/type: mock/"type: openaiCompatible\n      preset: mimo"/eg;
    s/model: mock-react/"model: ".$ENV{JEJU_SCRIPT_MODEL}/eg;
    s/(      model: .+\n)/$1      envKey: $ENV{JEJU_SCRIPT_ENV_KEY}\n      baseUrl: $ENV{JEJU_SCRIPT_BASE_URL}\n/g;
  ' "$workdir"/agents/*.agent.yaml
fi

echo "==> Provider: $provider"
echo "==> Workdir: $workdir"
if [[ "$provider" == "mimo" ]]; then
  echo "==> Env key: $env_key"
fi

(
  cd "$workdir"
  go run "$repo_root/cmd/jeju" validate agents/support.agent.yaml
  go run "$repo_root/cmd/jeju" validate agents/evolver.agent.yaml

  if [[ "$provider" == "mock" ]]; then
    go run "$repo_root/cmd/jeju" evolve --baseline-only experiments/evolve.yaml
  else
    go run "$repo_root/cmd/jeju" evolve --max-iterations 2 experiments/evolve.yaml
  fi

  experiment_dir="$(find .jeju-dev/evolve-marker -mindepth 1 -maxdepth 1 -type d -name '20*' -print | sort | tail -n 1)"
  if [[ -z "$experiment_dir" ]]; then
    echo "no evolution experiment directory found" >&2
    exit 1
  fi

  test -f "$experiment_dir/report.md"
  test -f "$experiment_dir/leaderboard.json"

  if [[ "$provider" == "mimo" ]]; then
    test -f "$experiment_dir/best/prompts/support.md"
    if ! grep -q 'APPROVED_FIX_MARKER' "$experiment_dir/best/prompts/support.md"; then
      echo "best prompt does not contain APPROVED_FIX_MARKER" >&2
      exit 1
    fi
    if ! grep -q 'best: `candidate-' "$experiment_dir/report.md"; then
      echo "report did not record a candidate as best" >&2
      exit 1
    fi
  fi

  echo
  echo "Evolution dir: $workdir/$experiment_dir"
  echo "Report: $workdir/$experiment_dir/report.md"
  echo "Leaderboard: $workdir/$experiment_dir/leaderboard.json"
)
