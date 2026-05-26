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

    flags = 0 if args.case_sensitive.lower() in {"1", "true", "yes"} else re.IGNORECASE
    count = len(re.findall(re.escape(args.keyword), args.text, flags))
    print(json.dumps({
        "ok": True,
        "output": {
            "keyword": args.keyword,
            "count": count,
            "case_sensitive": flags == 0
        }
    }))


if __name__ == "__main__":
    main()
