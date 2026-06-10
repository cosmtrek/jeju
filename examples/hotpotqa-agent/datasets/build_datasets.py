#!/usr/bin/env python3
"""Build HotpotQA distractor train/selection/test task files for jeju evolve.

Downloads the official HotpotQA dev (distractor) split, samples a fixed-seed
subset, and writes train.jsonl / selection.jsonl / test.jsonl next to this
script. The download is cached under <repo-root>/.jeju-dev/cache/hotpotqa/.

Usage:
  python3 build_datasets.py [--train 50] [--selection 30] [--test 50] [--seed 42]
"""

import argparse
import json
import pathlib
import random
import sys
import time
import urllib.error
import urllib.request

HOTPOT_URL = "http://curtis.ml.cmu.edu/datasets/hotpot/hotpot_dev_distractor_v1.json"
HF_ROWS_URL = (
    "https://datasets-server.huggingface.co/rows"
    "?dataset=hotpotqa%2Fhotpot_qa&config=distractor&split=validation"
)
MAX_CONTEXT_CHARS = 20000

SCRIPT_DIR = pathlib.Path(__file__).resolve().parent
REPO_ROOT = SCRIPT_DIR.parents[2]
CACHE_PATH = REPO_ROOT / ".jeju-dev" / "cache" / "hotpotqa" / "hotpot_dev_distractor_v1.json"


def fetch_from_hf():
    """Page the HotpotQA distractor validation split from the HF datasets server
    and convert rows to the original HotpotQA JSON schema."""
    examples = []
    offset = 0
    page = 100
    while True:
        url = f"{HF_ROWS_URL}&offset={offset}&length={page}"
        payload = None
        for attempt in range(6):
            try:
                with urllib.request.urlopen(url, timeout=120) as resp:
                    payload = json.loads(resp.read())
                break
            except urllib.error.HTTPError as exc:
                if exc.code != 429:
                    raise
                wait = 5 * (attempt + 1)
                print(f"\nrate limited at offset {offset}, retrying in {wait}s ...")
                time.sleep(wait)
        if payload is None:
            sys.exit(f"giving up: HuggingFace rate limit persisted at offset {offset}")
        rows = payload.get("rows", [])
        if not rows:
            break
        for item in rows:
            row = item["row"]
            ctx = row["context"]
            examples.append(
                {
                    "_id": row.get("id", ""),
                    "question": row["question"],
                    "answer": row["answer"],
                    "type": row.get("type", ""),
                    "level": row.get("level", ""),
                    "context": list(zip(ctx["title"], ctx["sentences"])),
                }
            )
        offset += len(rows)
        print(f"\rfetched {offset} rows from HuggingFace ...", end="", flush=True)
        if len(rows) < page:
            break
        time.sleep(1.5)
    print()
    return examples


def load_source(url):
    if CACHE_PATH.exists():
        print(f"using cached dataset: {CACHE_PATH}")
        return json.loads(CACHE_PATH.read_text())
    CACHE_PATH.parent.mkdir(parents=True, exist_ok=True)
    try:
        print(f"downloading {url} ...")
        with urllib.request.urlopen(url, timeout=300) as resp:
            data = resp.read()
        examples = json.loads(data)
    except Exception as exc:
        print(f"primary source failed ({exc}); falling back to HuggingFace datasets server")
        examples = fetch_from_hf()
    CACHE_PATH.write_text(json.dumps(examples, ensure_ascii=False))
    print(f"cached {len(examples)} examples to {CACHE_PATH}")
    return examples


def render_context(context):
    blocks = []
    for title, sentences in context:
        blocks.append(f"## {title}\n{''.join(sentences)}")
    return "\n\n".join(blocks)


def to_task(example, split, index):
    return {
        "id": f"hotpotqa-{split}-{index:03d}",
        "input": {
            "question": example["question"],
            "context": render_context(example["context"]),
        },
        "expected": {"answer": example["answer"]},
        "metadata": {
            "split": split,
            "level": example.get("level", ""),
            "type": example.get("type", ""),
            "hotpot_id": example.get("_id", ""),
        },
        "weight": 1,
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--train", type=int, default=50)
    parser.add_argument("--selection", type=int, default=30)
    parser.add_argument("--test", type=int, default=50)
    parser.add_argument("--seed", type=int, default=42)
    parser.add_argument("--url", default=HOTPOT_URL)
    args = parser.parse_args()

    examples = load_source(args.url)
    usable = [
        ex
        for ex in examples
        if ex.get("answer", "").strip()
        and len(render_context(ex["context"])) <= MAX_CONTEXT_CHARS
    ]
    total = args.train + args.selection + args.test
    if len(usable) < total:
        sys.exit(f"not enough usable examples: have {len(usable)}, need {total}")

    rng = random.Random(args.seed)
    sampled = rng.sample(usable, total)
    splits = {
        "train": sampled[: args.train],
        "selection": sampled[args.train : args.train + args.selection],
        "test": sampled[args.train + args.selection :],
    }
    for split, items in splits.items():
        path = SCRIPT_DIR / f"{split}.jsonl"
        with path.open("w") as f:
            for i, ex in enumerate(items, start=1):
                f.write(json.dumps(to_task(ex, split, i), ensure_ascii=False) + "\n")
        print(f"wrote {path} ({len(items)} tasks)")


if __name__ == "__main__":
    main()
