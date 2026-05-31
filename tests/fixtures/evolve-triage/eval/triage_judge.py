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
    fields = expected.get("fields") or {}
    evidence = expected.get("evidence") or []
    final = ctx.get("Final") or ctx.get("final") or ""

    parsed, parse_error = extract_json(final)
    if not isinstance(parsed, dict):
        emit(0.0, False, parse_error or "final answer is not a JSON object")
        return

    score = 0.20
    reasons = ["valid_json"]
    weights = {
        "severity": 0.25,
        "route": 0.20,
        "action": 0.20,
    }
    for key, weight in weights.items():
        got = normalize(parsed.get(key))
        want = normalize(fields.get(key))
        if got == want and want:
            score += weight
            reasons.append(f"{key}=ok")
        else:
            reasons.append(f"{key}=got:{got or '<missing>'},want:{want or '<missing>'}")

    rationale = normalize(parsed.get("rationale"))
    evidence_hit = False
    for item in evidence:
        item_norm = normalize(item)
        if item_norm and item_norm in rationale:
            evidence_hit = True
            break
    if evidence_hit:
        score += 0.15
        reasons.append("evidence=ok")
    else:
        reasons.append("evidence=missing")

    score = round(score, 4)
    emit(score, score >= 0.80, "; ".join(reasons))


if __name__ == "__main__":
    main()
