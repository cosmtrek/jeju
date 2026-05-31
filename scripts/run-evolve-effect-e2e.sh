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

workdir="${JEJU_EVOLVE_EFFECT_WORKDIR:-"$repo_root/.jeju-dev/evolve-effect-e2e-$provider"}"
rm -rf "$workdir"
mkdir -p "$workdir"
cp -R "$repo_root/tests/fixtures/evolve-triage/." "$workdir/"
chmod +x "$workdir/eval/triage_judge.py"

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
  go run "$repo_root/cmd/jeju" validate agents/triage.agent.yaml
  go run "$repo_root/cmd/jeju" validate agents/evolver.agent.yaml

  if [[ "$provider" == "mock" ]]; then
    go run "$repo_root/cmd/jeju" evolve --baseline-only experiments/triage-evolve.yaml
  else
    go run "$repo_root/cmd/jeju" evolve --max-iterations 2 experiments/triage-evolve.yaml
  fi

  experiment_dir="$(find .jeju-dev/evolve-triage -mindepth 1 -maxdepth 1 -type d -name '20*' -print | sort | tail -n 1)"
  if [[ -z "$experiment_dir" ]]; then
    echo "no evolution experiment directory found" >&2
    exit 1
  fi

  if [[ "$provider" == "mock" ]]; then
    echo
    echo "Mock fixture smoke completed: $workdir/$experiment_dir"
    exit 0
  fi

  python3 - "$experiment_dir" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
leaderboard = json.loads((root / "leaderboard.json").read_text())
best = next((item for item in leaderboard if item["id"] != "baseline" and not item.get("rejected")), None)
if best is None:
    raise SystemExit("no accepted non-baseline candidate found")
print(f"==> Evolution best: {best['id']}")
PY

  head -n 1 datasets/test.jsonl > datasets/audit-probe.jsonl

  cat > experiments/audit-baseline.yaml <<'YAML'
apiVersion: jeju/v1alpha1
kind: EvolutionExperiment
metadata:
  name: triage-audit-baseline
target:
  agent: ../agents/triage.agent.yaml
  editable:
    - instructions.system
  forbidden:
    - permissions.access
data:
  format: jeju.task.v1
  train: ../datasets/audit-probe.jsonl
  selection: ../datasets/test.jsonl
  render:
    template: ../prompts/task_input.md.tmpl
objective:
  metric: evaluation.evaluators["triage_judge"].score
  direction: maximize
  minDelta: 0.01
evolver:
  agent: ../agents/evolver.agent.yaml
  proposals: 1
search:
  iterations: 1
  trialsPerTask: 1
  parallelism: 2
output:
  dir: ../.jeju-dev/triage-audit-baseline
YAML

  best_agent="$(pwd)/$experiment_dir/best/agents/triage.agent.yaml"
  cat > experiments/audit-best.yaml <<YAML
apiVersion: jeju/v1alpha1
kind: EvolutionExperiment
metadata:
  name: triage-audit-best
target:
  agent: $best_agent
  editable:
    - instructions.system
  forbidden:
    - permissions.access
data:
  format: jeju.task.v1
  train: ../datasets/audit-probe.jsonl
  selection: ../datasets/test.jsonl
  render:
    template: ../prompts/task_input.md.tmpl
objective:
  metric: evaluation.evaluators["triage_judge"].score
  direction: maximize
  minDelta: 0.01
evolver:
  agent: ../agents/evolver.agent.yaml
  proposals: 1
search:
  iterations: 1
  trialsPerTask: 1
  parallelism: 2
output:
  dir: ../.jeju-dev/triage-audit-best
YAML

  go run "$repo_root/cmd/jeju" evolve --baseline-only experiments/audit-baseline.yaml
  go run "$repo_root/cmd/jeju" evolve --baseline-only experiments/audit-best.yaml

  python3 - <<'PY'
import json
import pathlib

def latest(base):
    dirs = sorted(pathlib.Path(base).glob("20*"))
    if not dirs:
        raise SystemExit(f"no audit output under {base}")
    return dirs[-1]

def metrics(base):
    root = latest(base)
    leaderboard = json.loads((root / "leaderboard.json").read_text())
    candidate = leaderboard[0]
    selection = candidate["results"]["selection"]
    values = dict(selection["metrics"])
    if "evaluation.passed_rate" not in values:
        trials = selection.get("trials") or []
        if trials:
            passed = sum(1 for trial in trials if (trial.get("evaluation") or {}).get("passed"))
            values["evaluation.passed_rate"] = passed / len(trials)
        else:
            values["evaluation.passed_rate"] = 0
    return root, values

evolve_root = latest(".jeju-dev/evolve-triage")
leaderboard = json.loads((evolve_root / "leaderboard.json").read_text())
accepted = [item for item in leaderboard if item["id"] != "baseline" and not item.get("rejected")]
best_candidate = accepted[0]
evolution_score_key = 'evaluation.evaluators["triage_judge"].score'
evolution_train = best_candidate["results"]["train"]["metrics"]
evolution_selection = best_candidate["results"]["selection"]["metrics"]
baseline_root, baseline = metrics(".jeju-dev/triage-audit-baseline")
best_root, best = metrics(".jeju-dev/triage-audit-best")

score_key = evolution_score_key
baseline_score = baseline[score_key]
best_score = best[score_key]
baseline_pass = baseline["evaluation.passed_rate"]
best_pass = best["evaluation.passed_rate"]
delta = best_score - baseline_score
pass_delta = best_pass - baseline_pass
passed = (
    baseline_score <= 0.30
    and best_score >= 0.80
    and delta >= 0.50
    and pass_delta >= 0.50
)
summary = {
    "evolution_dir": str(evolve_root),
    "best_candidate": best_candidate["id"],
    "evolution_train_score": evolution_train[score_key],
    "evolution_selection_score": evolution_selection[score_key],
    "evolution_train_passed_rate": evolution_train["evaluation.passed_rate"],
    "evolution_selection_passed_rate": evolution_selection["evaluation.passed_rate"],
    "baseline_audit_dir": str(baseline_root),
    "best_audit_dir": str(best_root),
    "baseline_test_score": baseline_score,
    "best_test_score": best_score,
    "score_delta": round(delta, 4),
    "baseline_test_passed_rate": baseline_pass,
    "best_test_passed_rate": best_pass,
    "passed_rate_delta": round(pass_delta, 4),
    "passed": passed,
}
path = pathlib.Path(".jeju-dev/evolve-triage-effect-summary.json")
path.write_text(json.dumps(summary, indent=2) + "\n")
print(json.dumps(summary, indent=2))
if not passed:
    raise SystemExit("triage self-evolution effect thresholds were not met")
PY

  echo
  echo "Evolution dir: $workdir/$experiment_dir"
  echo "Effect summary: $workdir/.jeju-dev/evolve-triage-effect-summary.json"
)
