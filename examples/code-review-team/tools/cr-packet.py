#!/usr/bin/env python3
import argparse
import datetime as dt
import json
import os
import pathlib
import re
import subprocess
import sys


DIMENSIONS = [
    "diff_context",
    "runtime_correctness",
    "safety_policy",
    "tests_docs",
    "static_analysis",
]

MAX_TEXT = 24000
MAX_FILE_EXCERPT = 12000
MAX_PACKET_CHARS = 85000
MAX_EVIDENCE_CHARS = 6000

DIFF_LIMITS = {
    "diff_context": 20,
    "runtime_correctness": 12,
    "safety_policy": 12,
    "tests_docs": 18,
    "static_analysis": 0,
}

FILE_LIMITS = {
    "diff_context": 0,
    "runtime_correctness": 14,
    "safety_policy": 16,
    "tests_docs": 24,
    "static_analysis": 0,
}

SAFETY_PATH_TOKENS = {
    "auth",
    "command",
    "config",
    "controller",
    "credential",
    "exec",
    "http",
    "manifest",
    "network",
    "path",
    "permission",
    "policy",
    "provider",
    "runtime",
    "sandbox",
    "secret",
    "security",
    "shell",
    "token",
    "tool",
    "tools",
    "workspace",
}

STATIC_FILE_NAMES = {
    "go.mod",
    "go.sum",
    "package.json",
    "package-lock.json",
    "pnpm-lock.yaml",
    "yarn.lock",
    "requirements.txt",
    "pyproject.toml",
    "poetry.lock",
    "makefile",
}

STATIC_PATH_TOKENS = {"ci", "workflow", "workflows"}

RUN_ID_SAFE = re.compile(r"[^A-Za-z0-9_.-]+")


def emit(output=None, ok=True, error=None):
    envelope = {"ok": ok}
    if ok:
        envelope["output"] = output if output is not None else {}
    else:
        envelope["error"] = error or "unknown error"
    print(json.dumps(envelope, ensure_ascii=False))
    return 0 if ok else 1


def run(cmd, timeout=30):
    try:
        completed = subprocess.run(
            cmd,
            cwd=os.getcwd(),
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            timeout=timeout,
            check=False,
        )
        return {
            "command": " ".join(cmd),
            "exit_code": completed.returncode,
            "output": trim(completed.stdout),
        }
    except Exception as exc:
        return {"command": " ".join(cmd), "exit_code": -1, "output": str(exc)}


def trim(text, limit=MAX_TEXT):
    text = text or ""
    if len(text) <= limit:
        return text
    return text[:limit] + f"\n...[truncated {len(text) - limit} chars]"


def packet_root():
    return pathlib.Path(".jeju-dev") / "code-review-team-packets"


def normalize_run_id(run_id):
    if not run_id or run_id == "auto":
        stamp = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        return f"run-{stamp}-{os.getpid()}"
    cleaned = RUN_ID_SAFE.sub("-", run_id.strip())
    return cleaned[:96] or "current"


def current_pointer():
    return packet_root() / "current.json"


def write_current_pointer(run_id):
    pointer = current_pointer()
    pointer.parent.mkdir(parents=True, exist_ok=True)
    pointer.write_text(json.dumps({"run_id": run_id}, ensure_ascii=False, indent=2), encoding="utf-8")


def resolve_run_id(run_id):
    if run_id == "current":
        pointer = current_pointer()
        if pointer.exists():
            try:
                data = json.loads(pointer.read_text(encoding="utf-8"))
                resolved = data.get("run_id", "")
                if resolved:
                    return resolved
            except Exception:
                pass
    return run_id


def out_dir(run_id):
    return packet_root() / resolve_run_id(run_id)


def safe_text(path, limit=MAX_FILE_EXCERPT):
    try:
        data = path.read_bytes()
    except Exception as exc:
        return f"<unable to read: {exc}>"
    if b"\x00" in data:
        return "<binary file omitted>"
    text = data.decode("utf-8", errors="replace")
    numbered = "\n".join(f"{idx + 1}: {line}" for idx, line in enumerate(text.splitlines()))
    return trim(numbered, limit)


def git_lines(args):
    result = run(["git"] + args)
    if result["exit_code"] != 0:
        return []
    return [line.strip() for line in result["output"].splitlines() if line.strip()]


def changed_files():
    files = []
    for args in [
        ["diff", "--name-only"],
        ["diff", "--cached", "--name-only"],
        ["ls-files", "--others", "--exclude-standard"],
    ]:
        files.extend(git_lines(args))
    deduped = []
    seen = set()
    for item in files:
        if item not in seen:
            seen.add(item)
            deduped.append(item)
    return deduped[:120]


def path_parts_and_tokens(path):
    p = path.replace("\\", "/").lower()
    parts = [part for part in p.split("/") if part]
    tokens = set(parts)
    for part in parts:
        stem = part.rsplit(".", 1)[0]
        tokens.add(stem)
        tokens.update(token for token in re.split(r"[^a-z0-9]+", part) if token)
    return p, parts, tokens


def classify(path):
    p, parts, tokens = path_parts_and_tokens(path)
    name = parts[-1] if parts else p
    buckets = set()
    if (
        p.endswith((".md", ".rst", ".txt", ".yaml", ".yml"))
        or "docs" in parts
        or name.startswith("readme")
    ):
        buckets.add("tests_docs")
    if "_test." in name or {"test", "tests", "fixtures"} & tokens:
        buckets.add("tests_docs")
    if p.endswith((".go", ".rs", ".py", ".ts", ".tsx", ".js", ".jsx", ".java", ".kt")):
        buckets.add("runtime_correctness")
    if SAFETY_PATH_TOKENS & tokens or name.endswith((".agent.yaml", ".team.yaml")):
        buckets.add("safety_policy")
    if (
        name in STATIC_FILE_NAMES
        or name.endswith(".lock")
        or ".github" in parts
        or STATIC_PATH_TOKENS & tokens
    ):
        buckets.add("static_analysis")
    buckets.add("diff_context")
    return buckets


def changed_file_inventory(files, dimension):
    rows = []
    for rel in files:
        buckets = sorted(classify(rel))
        rows.append(
            {
                "file": rel,
                "classifications": buckets,
                "selected_for_dimension": dimension in buckets,
            }
        )
    return {
        "id": f"{dimension}.scope",
        "type": "changed_file_inventory",
        "content": json.dumps(rows, ensure_ascii=False, indent=2),
    }


def file_evidence(files, dimension):
    evidence = []
    count = 0
    limit = FILE_LIMITS.get(dimension, 8)
    for rel in files:
        if dimension not in classify(rel):
            continue
        path = pathlib.Path(rel)
        if not path.exists() or path.is_dir():
            continue
        count += 1
        evidence.append(
            {
                "id": f"{dimension}.file.{count}",
                "type": "file_excerpt",
                "file": rel,
                "content": safe_text(path),
                "why_included": f"changed file classified for {dimension}",
            }
        )
        if count >= limit:
            break
    return evidence


def diff_for_file(rel):
    diff = run(["git", "diff", "--", rel], timeout=15)["output"]
    if diff.strip():
        return diff
    cached = run(["git", "diff", "--cached", "--", rel], timeout=15)["output"]
    if cached.strip():
        return cached
    path = pathlib.Path(rel)
    if path.exists() and not path.is_dir():
        untracked = git_lines(["ls-files", "--others", "--exclude-standard", "--", rel])
        if untracked:
            pseudo = run(["git", "diff", "--no-index", "--", os.devnull, rel], timeout=15)["output"]
            if pseudo.strip():
                return pseudo
    return ""


def diff_evidence(files, dimension):
    evidence = []
    count = 0
    limit = DIFF_LIMITS.get(dimension, 6)
    for rel in files:
        if dimension not in classify(rel):
            continue
        diff = diff_for_file(rel)
        if not diff.strip():
            continue
        count += 1
        evidence.append(
            {
                "id": f"{dimension}.diff.{count}",
                "type": "diff_hunk",
                "file": rel,
                "content": trim(diff, 9000),
                "why_included": f"diff for changed file classified for {dimension}",
            }
        )
        if count >= limit:
            break
    return evidence


def command_evidence():
    checks = [
        (["git", "diff", "--check"], 30),
    ]
    if pathlib.Path("go.mod").exists():
        checks.append((["go", "test", "./..."], 240))
        checks.append((["go", "vet", "./..."], 240))
    elif pathlib.Path("package.json").exists():
        checks.append((["npm", "test", "--", "--runInBand"], 120))
    elif pathlib.Path("pyproject.toml").exists() or pathlib.Path("pytest.ini").exists():
        checks.append((["python3", "-m", "pytest"], 120))

    evidence = []
    for idx, (cmd, timeout) in enumerate(checks, 1):
        result = run(cmd, timeout=timeout)
        evidence.append(
            {
                "id": f"static_analysis.command.{idx}",
                "type": "command_result",
                "command": result["command"],
                "exit_code": result["exit_code"],
                "output": trim(result["output"], 9000),
            }
        )
    return evidence


def files_mentioned_by_failures(files, command_items):
    failures = [item for item in command_items if item.get("exit_code") not in (0, None)]
    if not failures:
        return []
    output = "\n".join(item.get("output", "") for item in failures).lower()
    mentioned = []
    basename_hits = {}
    for rel in files:
        name = pathlib.PurePosixPath(rel).name.lower()
        basename_hits.setdefault(name, []).append(rel)
    for rel in files:
        lowered = rel.lower()
        name = pathlib.PurePosixPath(rel).name.lower()
        if lowered in output or (len(basename_hits.get(name, [])) == 1 and name in output):
            mentioned.append(rel)
    return mentioned[:8]


def static_failure_context(files, command_items):
    mentioned = files_mentioned_by_failures(files, command_items)
    failures = [item for item in command_items if item.get("exit_code") not in (0, None)]
    if not failures:
        return [], []
    if not mentioned:
        return [], [
            "Static command failures did not mention changed files, so this packet cannot attribute them to the current diff."
        ]

    evidence = []
    for idx, rel in enumerate(mentioned, 1):
        diff = diff_for_file(rel)
        if not diff.strip():
            continue
        evidence.append(
            {
                "id": f"static_analysis.failure_diff.{idx}",
                "type": "failure_related_diff_hunk",
                "file": rel,
                "content": trim(diff, 9000),
                "why_included": "changed file mentioned by a failing static command",
            }
        )
    return evidence, []


def evidence_payload_key(item):
    if "content" in item:
        return "content"
    if "output" in item:
        return "output"
    return ""


def packet_size(packet):
    return len(json.dumps(packet, ensure_ascii=False))


def enforce_packet_budget(packet):
    if packet_size(packet) <= MAX_PACKET_CHARS:
        return

    packet.setdefault("gaps", []).append(
        f"Packet exceeded {MAX_PACKET_CHARS} chars and large evidence bodies were truncated."
    )
    shrinkable = []
    for item in packet.get("evidence", []):
        key = evidence_payload_key(item)
        if not key:
            continue
        text = item.get(key, "")
        if isinstance(text, str) and len(text) > 1200:
            shrinkable.append((len(text), item, key))

    shrinkable.sort(reverse=True, key=lambda row: row[0])
    for _, item, key in shrinkable:
        if packet_size(packet) <= MAX_PACKET_CHARS:
            break
        text = item.get(key, "")
        over = packet_size(packet) - MAX_PACKET_CHARS
        next_limit = max(1200, len(text) - over - 1000)
        if next_limit < len(text):
            item[key] = trim(text, next_limit)
            item["truncated_for_packet_budget"] = True

    for _, item, key in shrinkable:
        if packet_size(packet) <= MAX_PACKET_CHARS:
            break
        item[key] = "<omitted to keep packet within aggregate budget>"
        item["truncated_for_packet_budget"] = True


def packet_for(dimension, index, files):
    evidence = [
        {
            "id": f"{dimension}.status",
            "type": "git_status",
            "content": index["git_status"],
        },
        {
            "id": f"{dimension}.diff_stat",
            "type": "git_diff_stat",
            "content": index["git_diff_stat"],
        },
        changed_file_inventory(files, dimension),
    ]
    gaps = []
    if dimension == "static_analysis":
        commands = command_evidence()
        evidence.extend(commands)
        static_context, static_gaps = static_failure_context(files, commands)
        evidence.extend(static_context)
        gaps.extend(static_gaps)
    elif dimension == "diff_context":
        evidence.extend(diff_evidence(files, dimension))
    else:
        evidence.extend(diff_evidence(files, dimension))
        evidence.extend(file_evidence(files, dimension))

    if len(evidence) <= 3:
        gaps.append(f"No dimension-specific evidence was found for {dimension}.")

    packet = {
        "packet_id": f"pkt-{dimension}-{index['run_id']}",
        "dimension": dimension,
        "run_id": index["run_id"],
        "generated_at": index["generated_at"],
        "scope": {
            "workspace": os.getcwd(),
            "changed_files": files,
            "review_focus": focus_for(dimension),
        },
        "evidence": evidence,
        "constraints": [
            "Use only this packet and task context for review.",
            "Every finding must cite evidence_ref values that exist in this packet.",
            "If evidence is insufficient, put the issue in gaps instead of guessing.",
        ],
        "gaps": gaps,
        "truncation": {
            "max_text_chars": MAX_TEXT,
            "max_file_excerpt_chars": MAX_FILE_EXCERPT,
            "max_packet_chars": MAX_PACKET_CHARS,
        },
    }
    enforce_packet_budget(packet)
    return packet


def focus_for(dimension):
    return {
        "diff_context": ["changed-file scope", "unexpected files", "diff shape", "reviewability", "large or risky hunks"],
        "runtime_correctness": ["state transitions", "error handling", "concurrency", "edge cases", "data consistency"],
        "safety_policy": ["permissions", "sandbox boundaries", "path traversal", "network access", "secret handling"],
        "tests_docs": ["test coverage", "fixture quality", "documentation accuracy", "validation gaps"],
        "static_analysis": ["deterministic checks", "test failures", "lint-style errors", "build regressions"],
    }.get(dimension, ["general review"])


def build(args):
    run_id = normalize_run_id(args.run_id)
    root = out_dir(run_id)
    packets_dir = root / "packets"
    packets_dir.mkdir(parents=True, exist_ok=True)
    files = changed_files()
    index = {
        "run_id": run_id,
        "requested_run_id": args.run_id,
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "workspace": os.getcwd(),
        "git_status": run(["git", "status", "--short"], timeout=20)["output"],
        "git_diff_stat": run(["git", "diff", "--stat"], timeout=20)["output"],
        "changed_files": files,
        "dimensions": DIMENSIONS,
    }
    packet_summaries = []
    for dimension in DIMENSIONS:
        packet = packet_for(dimension, index, files)
        path = packets_dir / f"{dimension}.json"
        path.write_text(json.dumps(packet, ensure_ascii=False, indent=2), encoding="utf-8")
        packet_summaries.append(
            {
                "dimension": dimension,
                "packet": str(path),
                "evidence_count": len(packet["evidence"]),
                "gaps": packet["gaps"],
            }
        )
    (root / "index.json").write_text(json.dumps(index, ensure_ascii=False, indent=2), encoding="utf-8")
    write_current_pointer(run_id)
    return emit(
        {
            "summary": f"Built {len(packet_summaries)} review packets for {len(files)} changed files.",
            "run_id": run_id,
            "packet_root": str(root),
            "changed_files": files,
            "packets": packet_summaries,
            "next_step": "Reviewer tasks should call get_review_packet for their assigned dimension.",
        }
    )


def packet(args):
    path = out_dir(args.run_id) / "packets" / f"{args.dimension}.json"
    if not path.exists():
        return emit(ok=False, error=f"missing packet {path}")
    return emit(json.loads(path.read_text(encoding="utf-8")))


def list_packets(args):
    root = out_dir(args.run_id)
    index_path = root / "index.json"
    if not index_path.exists():
        return emit(ok=False, error=f"missing packet index {index_path}")
    index = json.loads(index_path.read_text(encoding="utf-8"))
    packets = []
    for dimension in DIMENSIONS:
        path = root / "packets" / f"{dimension}.json"
        packets.append({"dimension": dimension, "exists": path.exists(), "path": str(path)})
    return emit({"resolved_run_id": resolve_run_id(args.run_id), "index": index, "packets": packets})


def evidence_index(args):
    root = out_dir(args.run_id)
    packets = {}
    for dimension in DIMENSIONS:
        path = root / "packets" / f"{dimension}.json"
        if not path.exists():
            packets[dimension] = {"exists": False, "evidence": []}
            continue
        packet = json.loads(path.read_text(encoding="utf-8"))
        evidence = []
        for item in packet.get("evidence", []):
            evidence.append(
                {
                    "id": item.get("id", ""),
                    "type": item.get("type", ""),
                    "file": item.get("file", ""),
                    "command": item.get("command", ""),
                    "exit_code": item.get("exit_code", None),
                    "body_chars": len(str(item.get("content", item.get("output", "")))),
                }
            )
        packets[dimension] = {
            "exists": True,
            "packet_id": packet.get("packet_id", ""),
            "evidence": evidence,
            "gaps": packet.get("gaps", []),
        }
    return emit({"run_id": args.run_id, "resolved_run_id": resolve_run_id(args.run_id), "packets": packets})


def evidence(args):
    path = out_dir(args.run_id) / "packets" / f"{args.dimension}.json"
    if not path.exists():
        return emit(ok=False, error=f"missing packet {path}")
    packet = json.loads(path.read_text(encoding="utf-8"))
    for item in packet.get("evidence", []):
        if item.get("id") != args.id:
            continue
        result = dict(item)
        for key in ("content", "output"):
            if key in result and isinstance(result[key], str):
                result[key] = trim(result[key], args.max_chars)
        result["dimension"] = args.dimension
        result["run_id"] = packet.get("run_id", resolve_run_id(args.run_id))
        result["packet_id"] = packet.get("packet_id", "")
        return emit(result)
    return emit(ok=False, error=f"missing evidence id {args.id} in {args.dimension}")


def main():
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="cmd", required=True)

    p_build = sub.add_parser("build")
    p_build.add_argument("--run-id", default="auto")
    p_build.set_defaults(func=build)

    p_packet = sub.add_parser("packet")
    p_packet.add_argument("--run-id", default="current")
    p_packet.add_argument("--dimension", required=True, choices=DIMENSIONS)
    p_packet.set_defaults(func=packet)

    p_list = sub.add_parser("list")
    p_list.add_argument("--run-id", default="current")
    p_list.set_defaults(func=list_packets)

    p_evidence = sub.add_parser("evidence-index")
    p_evidence.add_argument("--run-id", default="current")
    p_evidence.set_defaults(func=evidence_index)

    p_evidence_item = sub.add_parser("evidence")
    p_evidence_item.add_argument("--run-id", default="current")
    p_evidence_item.add_argument("--dimension", required=True, choices=DIMENSIONS)
    p_evidence_item.add_argument("--id", required=True)
    p_evidence_item.add_argument("--max-chars", type=int, default=MAX_EVIDENCE_CHARS)
    p_evidence_item.set_defaults(func=evidence)

    args = parser.parse_args()
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
