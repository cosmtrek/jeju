# HotpotQA Evolve Benchmark

Public-benchmark harness for `jeju evolve`: a multi-hop QA solver whose system
prompt is evolved against the official HotpotQA answer EM/F1 metric. This
example is also the validation study that made `pareto` the default
`search.strategy` — full process and results below.

- Dataset: HotpotQA dev (distractor) — each task carries the question plus
  ~10 titled context paragraphs (2 relevant, the rest distractors), so no
  retrieval tooling is required.
- Evaluator: `eval/hotpotqa_em_f1.py`, the official HotpotQA answer
  normalization (lowercase, strip punctuation and articles), F1 as the score
  and exact match as passed. Fully programmatic, no LLM judge.
- Editable surface: `harness:prompt` (the solver system prompt) only.
- Metrics: the objective `evaluation.evaluators["hotpotqa_em_f1"].score` is
  mean answer F1; `evaluation.passed_rate` is the exact-match rate. The
  prediction is the text after the last `Answer:` line of the final output.

## Setup

Build the train/selection/test task files (downloads and caches the HotpotQA
dev distractor split under repo-root `.jeju-dev/cache/`; falls back to the
HuggingFace datasets server when the CMU mirror is down):

```bash
python3 datasets/build_datasets.py
```

The committed `datasets/manifest.json` pins the exact HotpotQA example ids
of the 100/50/100 splits used in the validation study below, so this command
reproduces the benchmark byte-for-byte for anyone, independent of source
ordering or Python version. To draw a fresh sample instead, pass
`--resample [--train N --selection N --test N --seed N]`, which also rewrites
the manifest.

## Run

```bash
export DEEPSEEK_API_KEY=sk-...

# validate + compile only, no model calls
jeju evolve --dry-run experiments/hotpotqa-evolve.yaml

# baseline metrics only
jeju evolve --baseline-only experiments/hotpotqa-evolve.yaml

# hillclimb arm (explicit strategy) with held-out test scoring
jeju evolve --test experiments/hotpotqa-evolve.yaml

# pareto arm (GEPA-style search) on the same data and budget
jeju evolve --test experiments/hotpotqa-evolve-pareto.yaml
```

Outputs land under repo-root `.jeju-dev/evolve/hotpotqa*/<experiment_id>/`:
`report.md`, `leaderboard.json`, `baseline/results.json`, `best/`. Summarize
any set of experiment directories with:

```bash
python3 experiments/compare_results.py <experiment_dir> [<experiment_dir> ...]
```

## Validation Study (June 2026)

Question: does the GEPA-style `pareto` strategy beat the original greedy
`hillclimb` strategy, and does either beat the unevolved baseline on a
held-out public benchmark?

Protocol:

- Model: DeepSeek `deepseek-v4-flash` for both solver (temperature 0) and
  evolver (temperature 0.5).
- Both arms share the same data, evaluator, guards, evolver prompt, and a
  `budget.maxRuns: 2200` cap. The evolver only sees train feedback; selection
  drives acceptance; the 100-task test split is scored once at the end for
  baseline and best in the same run.
- Hillclimb: 5 iterations x 3 proposals. Pareto: 12 iterations x 2 proposals,
  `minibatch: 12`, `pool: 8`, `seed: 42`.

### Round 1 at 50/30/50: below the noise floor

The first round used 50/30/50 splits to shake out the pipeline. It produced
no usable conclusion, and the reason is itself a result: re-running the
identical baseline config showed +-1.7 to +-3.4pp F1 run-to-run noise on the
30-50 task splits (API nondeterminism at temperature 0), the same magnitude
as the effects under test. Both arms' test deltas landed inside the noise
band. Two protocol bugs also surfaced:

- The guard `run.modelErrors <= baseline.run.modelErrors` with a zero-error
  baseline rejected ~half of all candidates on transient API errors. Fixed by
  tolerance bands: `passed_rate >= baseline - 0.05`,
  `modelErrors <= baseline + 0.1`.
- 30-50 task splits cannot resolve single-digit-pp gains. Treat
  selection >= 50 and test >= 100 as the working minimum for this evaluator.

### Round 2 at 100/50/100: pareto wins on held-out test

| arm | selection F1 / EM | test F1 / EM | test delta vs baseline |
| --- | --- | --- | --- |
| baseline (seed prompt) | 0.816-0.846 / 0.64-0.70 | 0.778-0.794 / 0.60-0.61 | — |
| hillclimb best | 0.879 / 0.74 | 0.795 / 0.64 | +1.7pp F1, +4pp EM |
| pareto best | 0.895 / 0.76 | **0.830 / 0.68** | **+3.6pp F1, +7pp EM** |

Reading the result:

- Test noise on 100 tasks is roughly +-1pp F1, so the pareto gain is ~3x the
  noise floor; the hillclimb F1 gain is marginal. In plain terms the pareto
  prompt answers 7 more of the 100 held-out questions exactly right.
- Hillclimb showed the classic greedy failure: it self-terminated after three
  no-improvement iterations, and its large selection gain (+6.3pp F1 / +10pp
  EM) only partially generalized to test — selection overfitting. Pareto kept
  adding pool candidates (10 across 11 iterations, multiple lineages; the
  winner came from iteration 5) and converted more of its selection gain into
  test gain.
- The minibatch cascade gate rejected weak proposals after 12 runs instead of
  150 (~90% evaluation cost saved per rejection).
- The winning prompt is fully general — answer-type identification, verbatim
  span extraction, distractor-paragraph caution, strict `Answer:` format — with
  no memorized task ids, questions, or gold answers (verified by inspection;
  the guidance in the experiment manifests forbids memorization).

Cost: each arm used ~1,100-2,030 solver runs and 5-10M flash-tier tokens.

This study is why `search.strategy` defaults to `pareto`. The hillclimb arm
stays in `experiments/hotpotqa-evolve.yaml` (explicit `strategy: hillclimb`)
as the comparison baseline.

## Notes

- `datasets/*.jsonl` are generated locally and are not meant to be committed.
- The react non-native path costs ~2 model calls per task (the flash model's
  first reply often fails action-JSON parsing and is retried); this is a known
  constant overhead and an evolvable failure mode, not a benchmark bug.
