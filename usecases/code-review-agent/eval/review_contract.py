#!/usr/bin/env python3
import json
import re
import sys


def extract_json(text):
    match = re.search(r"\{.*\}", text or "", re.S)
    if not match:
        return None, "final answer does not contain a JSON object"
    try:
        return json.loads(match.group(0)), None
    except json.JSONDecodeError as exc:
        return None, f"final answer JSON is invalid: {exc}"


def emit(score, passed, reason):
    print(json.dumps({"score": score, "passed": passed, "reason": reason}))


def main():
    ctx = json.load(sys.stdin)
    final = ctx.get("Final") or ctx.get("final") or ""
    parsed, err = extract_json(final)
    if err:
        emit(0.0, False, err)
        return

    findings = parsed.get("findings")
    if not isinstance(findings, list):
        emit(0.2, False, "findings must be an array")
        return
    if not findings:
        emit(0.3, False, "sample diff contains a high-risk cache bug but findings is empty")
        return

    required = {"severity", "file", "title", "evidence", "impact", "recommendation"}
    for idx, finding in enumerate(findings):
        if not isinstance(finding, dict):
            emit(0.3, False, f"finding {idx} is not an object")
            return
        missing = sorted(required - set(finding))
        if missing:
            emit(0.4, False, f"finding {idx} missing fields: {missing}")
            return

    joined = json.dumps(findings, ensure_ascii=False).lower()
    catches_plaintext_token = "session_cache.go" in joined and (
        "token" in joined and ("hash" in joined or "plaintext" in joined or "plain text" in joined)
    )
    catches_ttl_regression = "ttl" in joined or "expire" in joined or "expiration" in joined
    has_high_severity = any(f.get("severity") in ("P0", "P1") for f in findings if isinstance(f, dict))

    score = 0.4
    if catches_plaintext_token:
        score += 0.25
    if catches_ttl_regression:
        score += 0.2
    if has_high_severity:
        score += 0.15

    passed = score >= 0.8
    reason = "review caught plaintext-token and TTL regressions" if passed else "review missed key session-cache risks"
    emit(round(score, 2), passed, reason)


if __name__ == "__main__":
    main()

