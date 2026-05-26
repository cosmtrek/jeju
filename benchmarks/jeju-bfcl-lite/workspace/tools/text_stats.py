#!/usr/bin/env python3
import argparse
import json
import re


def main():
    parser = argparse.ArgumentParser(description="Count words and characters in text.")
    parser.add_argument("--text", required=True)
    args = parser.parse_args()

    words = re.findall(r"\b[\w'-]+\b", args.text)
    print(json.dumps({
        "ok": True,
        "output": {
            "words": len(words),
            "characters": len(args.text)
        }
    }))


if __name__ == "__main__":
    main()
