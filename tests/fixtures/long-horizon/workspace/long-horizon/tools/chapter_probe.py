#!/usr/bin/env python3
import argparse
import json


def main():
    parser = argparse.ArgumentParser(description="Emit a large deterministic chapter payload.")
    parser.add_argument("--chapter", required=True, type=int)
    parser.add_argument("--paragraphs", default=12, type=int)
    args = parser.parse_args()

    chapter = args.chapter
    if chapter < 1 or chapter > 8:
        print(json.dumps({
            "ok": False,
            "error": "chapter must be between 1 and 8",
        }))
        return
    paragraphs_count = args.paragraphs
    if paragraphs_count < 1 or paragraphs_count > 80:
        print(json.dumps({
            "ok": False,
            "error": "paragraphs must be between 1 and 80",
        }))
        return

    checkpoint = f"CHK-{chapter:02d}-{(chapter * 137) % 997:03d}"
    risk = ["low", "medium", "high", "critical"][chapter % 4]
    next_action = f"Preserve evidence for chapter {chapter} and continue to chapter {chapter + 1}."
    if chapter == 8:
        next_action = "Write the final report to reports/long-horizon-summary.md."

    paragraphs = []
    for i in range(1, paragraphs_count + 1):
        paragraphs.append(
            " ".join([
                f"chapter={chapter}",
                f"paragraph={i}",
                f"checkpoint={checkpoint}",
                f"risk={risk}",
                "This payload is intentionally verbose so Jeju has to compact older observations.",
                "The important durable facts are the checkpoint code, risk label, and next action.",
                "Filler trace text repeats domain-neutral evidence to grow the context window.",
            ])
        )

    print(json.dumps({
        "ok": True,
        "output": {
            "chapter": chapter,
            "checkpoint": checkpoint,
            "risk": risk,
            "next_action": next_action,
            "payload": "\n".join(paragraphs),
        },
        "metadata": {
            "fixture": "long-horizon-context-compression",
            "expected_report": "reports/long-horizon-summary.md",
            "paragraphs": paragraphs_count,
        },
    }))


if __name__ == "__main__":
    main()
