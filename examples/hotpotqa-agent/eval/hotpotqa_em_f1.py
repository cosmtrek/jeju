#!/usr/bin/env python3
"""Command evaluator: official HotpotQA answer EM/F1 against expected.answer.

Reads the jeju evaluate context JSON on stdin and emits
{"score": <f1>, "passed": <em>, "reason": "..."}.

The prediction is the text after the last "Answer:" line in the final output;
if no such line exists, the whole final output is used.
"""

import json
import re
import string
import sys

ANSWER_LINE = re.compile(r"^\s*\**\s*(?:final\s+)?answer\s*[::]\s*(.*?)\s*\**\s*$", re.I)


def normalize_answer(s):
    s = s.lower()
    s = "".join(ch for ch in s if ch not in string.punctuation)
    s = re.sub(r"\b(a|an|the)\b", " ", s)
    return " ".join(s.split())


def f1_score(prediction, ground_truth):
    norm_pred = normalize_answer(prediction)
    norm_gold = normalize_answer(ground_truth)
    if norm_pred in ("yes", "no", "noanswer") and norm_pred != norm_gold:
        return 0.0
    if norm_gold in ("yes", "no", "noanswer") and norm_pred != norm_gold:
        return 0.0
    pred_tokens = norm_pred.split()
    gold_tokens = norm_gold.split()
    common = {}
    for tok in pred_tokens:
        common[tok] = common.get(tok, 0)
    overlap = 0
    gold_counts = {}
    for tok in gold_tokens:
        gold_counts[tok] = gold_counts.get(tok, 0) + 1
    pred_counts = {}
    for tok in pred_tokens:
        pred_counts[tok] = pred_counts.get(tok, 0) + 1
    for tok, count in pred_counts.items():
        overlap += min(count, gold_counts.get(tok, 0))
    if overlap == 0 or not pred_tokens or not gold_tokens:
        return 0.0
    precision = overlap / len(pred_tokens)
    recall = overlap / len(gold_tokens)
    return 2 * precision * recall / (precision + recall)


def exact_match(prediction, ground_truth):
    return normalize_answer(prediction) == normalize_answer(ground_truth)


def extract_prediction(final):
    final = (final or "").strip()
    matches = [ANSWER_LINE.match(line) for line in final.splitlines()]
    matches = [m for m in matches if m]
    if matches:
        candidate = matches[-1].group(1).strip()
        if candidate:
            return candidate, "answer_line"
    return final, "full_text"


def main():
    ctx = json.load(sys.stdin)
    expected = ctx.get("Expected") or ctx.get("expected") or {}
    gold = str(expected.get("answer", "")).strip()
    final = ctx.get("Final") or ctx.get("final") or ""

    if not gold:
        print(json.dumps({"score": 0.0, "passed": False, "reason": "missing expected.answer"}))
        return

    pred, source = extract_prediction(final)
    em = exact_match(pred, gold)
    f1 = f1_score(pred, gold)

    shown_pred = pred if len(pred) <= 160 else pred[:160] + "..."
    reason = f"em={int(em)} f1={f1:.3f} source={source} pred={shown_pred!r} gold={gold!r}"
    print(json.dumps({"score": round(f1, 4), "passed": em, "reason": reason}, ensure_ascii=False))


if __name__ == "__main__":
    main()
