#!/usr/bin/env python3
import json
import re
import sys


def emit(score, passed, reason):
    print(json.dumps({"score": score, "passed": passed, "reason": reason}))


def normalize(value):
    return str(value or "").strip().lower()


def extract_json(text):
    text = (text or "").strip()
    if text.startswith("```"):
        text = re.sub(r"^```(?:json)?\s*", "", text, flags=re.I)
        text = re.sub(r"\s*```$", "", text)
    try:
        return json.loads(text), None
    except Exception:
        pass
    start = text.find("{")
    end = text.rfind("}")
    if start >= 0 and end > start:
        try:
            return json.loads(text[start : end + 1]), None
        except Exception as exc:
            return None, str(exc)
    return None, "final answer does not contain a JSON object"


def main():
    ctx = json.load(sys.stdin)
    expected = ctx.get("Expected") or ctx.get("expected") or {}
    final = ctx.get("Final") or ctx.get("final") or ""

    parsed, parse_error = extract_json(final)
    if not isinstance(parsed, dict):
        emit(0.0, False, parse_error or "final answer is not a JSON object")
        return

    score = 0.15
    reasons = ["valid_json"]
    expected_keys = {"domain", "answer", "skill_used", "evidence"}
    got_keys = set(parsed.keys())
    if got_keys == expected_keys:
        score += 0.15
        reasons.append("shape=ok")
    else:
        reasons.append("shape=unexpected_keys:" + ",".join(sorted(got_keys)))

    for key, weight in (("domain", 0.20), ("answer", 0.30)):
        got = normalize(parsed.get(key))
        want = normalize(expected.get(key))
        if got == want and want:
            score += weight
            reasons.append(f"{key}=ok")
        else:
            reasons.append(f"{key}=got:{got or '<missing>'},want:{want or '<missing>'}")

    if normalize(parsed.get("skill_used")) == "skillsbench-lite":
        score += 0.10
        reasons.append("skill_used=ok")
    else:
        reasons.append("skill_used=missing")

    evidence = normalize(parsed.get("evidence"))
    evidence_hits = []
    for item in expected.get("evidence") or []:
        item_norm = normalize(item)
        if item_norm and item_norm in evidence:
            evidence_hits.append(item)
    if evidence_hits:
        score += 0.10
        reasons.append("evidence=ok")
    else:
        reasons.append("evidence=missing")

    forbidden_hits = []
    haystack = normalize(json.dumps(parsed, ensure_ascii=False))
    for item in expected.get("forbidden") or []:
        item_norm = normalize(item)
        if item_norm and item_norm in haystack:
            forbidden_hits.append(item)
    if forbidden_hits:
        score = min(score, 0.79)
        reasons.append("forbidden=" + "|".join(forbidden_hits))

    score = round(max(0.0, min(1.0, score)), 4)
    emit(score, score >= 0.80, "; ".join(reasons))


if __name__ == "__main__":
    main()
