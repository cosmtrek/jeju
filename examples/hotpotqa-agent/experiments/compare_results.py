#!/usr/bin/env python3
"""Summarize and compare jeju evolve experiment results.

Usage:
  python3 compare_results.py <experiment_dir> [<experiment_dir> ...]

Each experiment_dir is one timestamped output directory containing
baseline/results.json, best/results.json, and leaderboard.json.
"""

import json
import pathlib
import sys

METRIC = 'evaluation.evaluators["hotpotqa_em_f1"].score'


def load(path):
    with open(path) as f:
        return json.load(f)


def split_row(results, split):
    data = results.get("results", {}).get(split)
    if not data:
        return None
    metrics = data.get("metrics", {})
    return {
        "f1": metrics.get(METRIC),
        "em": metrics.get("evaluation.passed_rate"),
        "trials": len(data.get("trials", [])),
    }


def candidate_summary(results):
    out = {"id": results.get("id")}
    for split in ("train", "selection", "test"):
        out[split] = split_row(results, split)
    return out


def fmt(row):
    if not row:
        return "-"
    f1 = row["f1"]
    em = row["em"]
    return f"F1={f1:.4f} EM={em:.4f}" if f1 is not None and em is not None else "-"


def main():
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    for arg in sys.argv[1:]:
        exp = pathlib.Path(arg)
        snapshot = load(exp / "experiment.snapshot.json")
        leaderboard = load(exp / "leaderboard.json")
        baseline = candidate_summary(load(exp / "baseline" / "results.json"))
        best = candidate_summary(load(exp / "best" / "results.json"))
        strategy = snapshot.get("Search", {}).get("Strategy") or snapshot.get("search", {}).get("strategy", "?")

        total_runs = 0
        total_tokens = 0
        for cand in leaderboard:
            for split, data in (cand.get("results") or {}).items():
                for trial in data.get("trials") or []:
                    total_runs += 1
                    total_tokens += (trial.get("stats") or {}).get("total_tokens", 0)

        accepted = [c for c in leaderboard if not c.get("rejected") and c.get("id") != "baseline"]
        rejected = [c for c in leaderboard if c.get("rejected")]

        print(f"== {exp.name} (strategy={strategy})")
        print(f"   candidates: {len(leaderboard) - 1} evaluated, {len(accepted)} accepted, {len(rejected)} rejected")
        print(f"   solver runs: {total_runs}, solver tokens: {total_tokens}")
        print(f"   baseline : train {fmt(baseline['train'])} | selection {fmt(baseline['selection'])} | test {fmt(baseline['test'])}")
        print(f"   best ({best['id']}): train {fmt(best['train'])} | selection {fmt(best['selection'])} | test {fmt(best['test'])}")
        if best["test"] and baseline["test"] and best["test"]["f1"] is not None:
            df1 = best["test"]["f1"] - baseline["test"]["f1"]
            dem = best["test"]["em"] - baseline["test"]["em"]
            print(f"   test delta vs baseline: F1 {df1:+.4f}, EM {dem:+.4f}")
        print()


if __name__ == "__main__":
    main()
