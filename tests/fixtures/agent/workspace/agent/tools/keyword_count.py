#!/usr/bin/env python3
import argparse
import json
import re


def main():
    parser = argparse.ArgumentParser(description="Count keyword occurrences in text.")
    parser.add_argument("--text", required=True)
    parser.add_argument("--keyword", required=True)
    parser.add_argument("--case-sensitive", default="false")
    args = parser.parse_args()

    text = args.text
    keyword = args.keyword
    case_sensitive = str(args.case_sensitive).lower() in {"1", "true", "yes"}

    if not keyword:
        print(json.dumps({
            "ok": False,
            "error": "expected non-empty keyword",
        }))
        return

    flags = 0 if case_sensitive else re.IGNORECASE
    pattern = re.compile(re.escape(keyword), flags)
    matches = pattern.findall(text)
    print(json.dumps({
        "ok": True,
        "output": {
            "keyword": keyword,
            "count": len(matches),
            "case_sensitive": case_sensitive,
        },
        "metadata": {
            "implementation": "tests/fixtures/agent/workspace/agent/tools/keyword_count.py",
        },
    }))


if __name__ == "__main__":
    main()
