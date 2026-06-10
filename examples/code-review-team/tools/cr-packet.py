#!/usr/bin/env python3
"""Single-packet evidence tool for the code review AgentTeam.

Subcommands:
  build           collect working tree changes into one review packet
  packet          print the packet for a run
  evidence-index  print compact evidence ids for a run
  evidence        print one evidence body by id
  expand          print a bounded excerpt of a repository file
  check           run detected deterministic checks (tests, vet)
"""
import argparse
import datetime as dt
import json
import os
import pathlib
import re
import subprocess
import sys


MAX_TEXT = 24000
MAX_DIFF_CHARS = 9000
MAX_PACKET_CHARS = 80000
MAX_EVIDENCE_CHARS = 6000
MAX_EXPAND_CHARS = 6000
MAX_EXPAND_LINES = 200
MAX_CHANGED_FILES = 200
LARGE_CHANGE_LINES = 800
LARGE_UNTRACKED_BYTES = 1_000_000

GENERATED_PATH_TOKENS = {
    "vendor",
    "node_modules",
    "dist",
    "build",
    ".next",
    "target",
    "__pycache__",
}

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
    return pathlib.Path(os.getcwd()) / ".jeju-dev" / "code-review-team-packets"


def normalize_run_id(run_id):
    cleaned = RUN_ID_SAFE.sub("-", (run_id or "").strip()) or "default"
    return cleaned[:80]


def current_pointer():
    return packet_root() / "current.json"


def write_current_pointer(run_id):
    packet_root().mkdir(parents=True, exist_ok=True)
    current_pointer().write_text(
        json.dumps({"run_id": run_id}, ensure_ascii=False), encoding="utf-8"
    )


def resolve_run_id(run_id):
    requested = (run_id or "current").strip()
    if requested and requested != "current":
        return normalize_run_id(requested)
    pointer = current_pointer()
    if pointer.exists():
        try:
            data = json.loads(pointer.read_text(encoding="utf-8"))
            return normalize_run_id(data.get("run_id", "default"))
        except Exception:
            return "default"
    return "default"


def out_dir(run_id):
    return packet_root() / normalize_run_id(run_id)


def packet_path(run_id):
    return out_dir(run_id) / "packet.json"


def load_packet(run_id):
    path = packet_path(run_id)
    if not path.exists():
        return None
    return json.loads(path.read_text(encoding="utf-8"))


def git_lines(args):
    result = run(["git", *args], timeout=20)
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
    dropped = max(0, len(deduped) - MAX_CHANGED_FILES)
    return deduped[:MAX_CHANGED_FILES], dropped


def numstat_map():
    stats = {}
    for args in [["diff", "--numstat"], ["diff", "--cached", "--numstat"]]:
        for line in git_lines(args):
            parts = line.split("\t")
            if len(parts) != 3:
                continue
            added, deleted, rel = parts
            binary = added == "-" or deleted == "-"
            row = stats.setdefault(rel, {"added": 0, "deleted": 0, "binary": False})
            if binary:
                row["binary"] = True
            else:
                row["added"] += int(added)
                row["deleted"] += int(deleted)
    return stats


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


def detect_checks():
    checks = []
    if pathlib.Path("go.mod").exists():
        checks.append({"name": "go_vet", "command": ["go", "vet", "./..."], "timeout": 240})
        checks.append({"name": "go_test", "command": ["go", "test", "./..."], "timeout": 300})
    elif pathlib.Path("package.json").exists():
        checks.append({"name": "npm_test", "command": ["npm", "test", "--", "--runInBand"], "timeout": 300})
    elif pathlib.Path("pyproject.toml").exists() or pathlib.Path("pytest.ini").exists():
        checks.append({"name": "pytest", "command": ["python3", "-m", "pytest"], "timeout": 300})
    return checks


def extensions_histogram(files):
    histogram = {}
    for rel in files:
        suffix = pathlib.PurePosixPath(rel).suffix.lower() or "(none)"
        histogram[suffix] = histogram.get(suffix, 0) + 1
    return dict(sorted(histogram.items(), key=lambda kv: -kv[1]))


def compute_scope_flags(files, dropped, stats):
    flags = []
    if dropped:
        flags.append(
            f"changed-file inventory truncated: {dropped} files beyond the {MAX_CHANGED_FILES} cap are not in this packet"
        )
    for rel, row in stats.items():
        if row["binary"]:
            flags.append(f"binary file changed: {rel}")
        elif row["added"] + row["deleted"] > LARGE_CHANGE_LINES:
            flags.append(f"very large change ({row['added']}+{row['deleted']} lines): {rel}")
    generated = [
        rel
        for rel in files
        if GENERATED_PATH_TOKENS & set(pathlib.PurePosixPath(rel).parts)
    ]
    if generated:
        flags.append(
            f"{len(generated)} changed files look generated or vendored: {', '.join(generated[:5])}"
            + ("..." if len(generated) > 5 else "")
        )
    for rel in git_lines(["ls-files", "--others", "--exclude-standard"]):
        path = pathlib.Path(rel)
        try:
            if path.is_file() and path.stat().st_size > LARGE_UNTRACKED_BYTES:
                flags.append(f"large untracked file ({path.stat().st_size} bytes): {rel}")
        except OSError:
            continue
    return flags


def evidence_payload_key(item):
    if "content" in item:
        return "content"
    if "output" in item:
        return "output"
    return None


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


def build(args):
    run_id = normalize_run_id(args.run_id)
    root = out_dir(run_id)
    root.mkdir(parents=True, exist_ok=True)

    files, dropped = changed_files()
    stats = numstat_map()
    flags = compute_scope_flags(files, dropped, stats)
    checks = detect_checks()
    git_status = run(["git", "status", "--short"], timeout=20)["output"]
    git_diff_stat = run(["git", "diff", "--stat"], timeout=20)["output"]
    diff_check = run(["git", "diff", "--check"], timeout=30)

    inventory = [
        {
            "file": rel,
            "added": stats.get(rel, {}).get("added", 0),
            "deleted": stats.get(rel, {}).get("deleted", 0),
            "binary": stats.get(rel, {}).get("binary", False),
        }
        for rel in files
    ]

    evidence = [
        {"id": "scope.status", "type": "git_status", "content": git_status},
        {"id": "scope.diff_stat", "type": "git_diff_stat", "content": git_diff_stat},
        {
            "id": "scope.files",
            "type": "changed_file_inventory",
            "content": json.dumps(inventory, ensure_ascii=False, indent=2),
        },
        {
            "id": "scope.flags",
            "type": "scope_flags",
            "content": json.dumps(flags, ensure_ascii=False, indent=2),
        },
        {
            "id": "check.git_diff_check",
            "type": "command_result",
            "command": diff_check["command"],
            "exit_code": diff_check["exit_code"],
            "output": diff_check["output"],
        },
    ]

    gaps = []
    count = 0
    for rel in files:
        diff = diff_for_file(rel)
        if not diff.strip():
            continue
        count += 1
        evidence.append(
            {
                "id": f"diff.{count}",
                "type": "diff_hunk",
                "file": rel,
                "content": trim(diff, MAX_DIFF_CHARS),
            }
        )
    if not files:
        gaps.append("No working tree changes were found.")
    if dropped:
        gaps.append(
            f"{dropped} changed files beyond the {MAX_CHANGED_FILES} cap were dropped from this packet."
        )

    packet = {
        "packet_id": f"pkt-{run_id}",
        "run_id": run_id,
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "scope": {
            "workspace": os.getcwd(),
            "changed_files": files,
            "dropped_files": dropped,
            "extensions": extensions_histogram(files),
            "flags": flags,
        },
        "checks": {
            "status": "detected" if checks else "unavailable",
            "available": [check["name"] for check in checks],
            "note": (
                "Heavy checks are not run at build time. A reviewer task can run them with the run_static_check tool."
                if checks
                else "No applicable check runner found (no go.mod/package.json/pyproject.toml)."
            ),
        },
        "evidence": evidence,
        "constraints": [
            "Findings must cite evidence ids that exist in this packet.",
            "Use expand_evidence for bounded extra context; record expanded paths and line ranges in the finding.",
            "If evidence is insufficient, put the issue in gaps instead of guessing.",
        ],
        "gaps": gaps,
        "truncation": {
            "max_diff_chars": MAX_DIFF_CHARS,
            "max_packet_chars": MAX_PACKET_CHARS,
        },
    }
    enforce_packet_budget(packet)

    packet_path(run_id).write_text(
        json.dumps(packet, ensure_ascii=False, indent=2), encoding="utf-8"
    )
    index = {
        "run_id": run_id,
        "requested_run_id": args.run_id,
        "generated_at": packet["generated_at"],
        "workspace": os.getcwd(),
        "changed_files": files,
        "dropped_files": dropped,
    }
    (root / "index.json").write_text(
        json.dumps(index, ensure_ascii=False, indent=2), encoding="utf-8"
    )
    write_current_pointer(run_id)

    return emit(
        {
            "summary": f"Built one review packet for {len(files)} changed files.",
            "run_id": run_id,
            "packet_path": str(packet_path(run_id)),
            "changed_files_count": len(files),
            "dropped_files": dropped,
            "extensions": packet["scope"]["extensions"],
            "scope_flags": flags,
            "checks": packet["checks"],
            "evidence_count": len(packet["evidence"]),
            "gaps": packet["gaps"],
            "next_step": "Reviewer tasks should call get_review_packet with this run_id.",
        }
    )


def packet_cmd(args):
    run_id = resolve_run_id(args.run_id)
    packet = load_packet(run_id)
    if packet is None:
        return emit(ok=False, error=f"missing packet {packet_path(run_id)}")
    return emit(packet)


def evidence_index(args):
    run_id = resolve_run_id(args.run_id)
    packet = load_packet(run_id)
    if packet is None:
        return emit(ok=False, error=f"missing packet {packet_path(run_id)}")
    rows = []
    for item in packet.get("evidence", []):
        key = evidence_payload_key(item)
        rows.append(
            {
                "id": item.get("id", ""),
                "type": item.get("type", ""),
                "file": item.get("file", ""),
                "exit_code": item.get("exit_code", None),
                "body_chars": len(item.get(key, "")) if key else 0,
            }
        )
    return emit({"run_id": run_id, "evidence": rows, "gaps": packet.get("gaps", [])})


def evidence(args):
    run_id = resolve_run_id(args.run_id)
    packet = load_packet(run_id)
    if packet is None:
        return emit(ok=False, error=f"missing packet {packet_path(run_id)}")
    for item in packet.get("evidence", []):
        if item.get("id") != args.id:
            continue
        result = dict(item)
        key = evidence_payload_key(result)
        if key and isinstance(result[key], str):
            result[key] = trim(result[key], MAX_EVIDENCE_CHARS)
        result["run_id"] = run_id
        return emit(result)
    return emit(ok=False, error=f"missing evidence id {args.id}")


def expand(args):
    base = pathlib.Path(os.getcwd()).resolve()
    target = (base / args.path).resolve()
    try:
        rel = target.relative_to(base)
    except ValueError:
        return emit(ok=False, error="path escapes the workspace")
    if {".git", ".jeju-dev"} & set(rel.parts):
        return emit(ok=False, error="path is not reviewable")
    if not target.exists() or target.is_dir():
        return emit(ok=False, error=f"not a readable file: {rel}")
    try:
        with target.open("rb") as handle:
            head = handle.read(8000)
    except OSError as exc:
        return emit(ok=False, error=str(exc))
    if b"\x00" in head:
        return emit(ok=False, error=f"binary file: {rel}")

    lines = target.read_text(encoding="utf-8", errors="replace").splitlines()
    total = len(lines)
    start = max(1, args.start)
    end = args.end if args.end >= start else start + MAX_EXPAND_LINES - 1
    end = min(total, min(end, start + MAX_EXPAND_LINES - 1))
    if start > total:
        return emit(ok=False, error=f"start line {start} is beyond end of file ({total} lines)")
    numbered = "\n".join(
        f"{number:>6}| {line}" for number, line in enumerate(lines[start - 1 : end], start)
    )
    return emit(
        {
            "path": str(rel),
            "start": start,
            "end": end,
            "total_lines": total,
            "content": trim(numbered, MAX_EXPAND_CHARS),
        }
    )


def check(args):
    checks = detect_checks()
    if args.name != "all":
        checks = [item for item in checks if item["name"] == args.name]
        if not checks:
            available = ", ".join(item["name"] for item in detect_checks()) or "none"
            return emit(
                ok=False,
                error=f"unknown or unavailable check {args.name}; detected checks: {available}",
            )
    if not checks:
        return emit({"results": [], "failed": [], "note": "No applicable check runner found."})
    results = []
    for item in checks:
        result = run(item["command"], timeout=item["timeout"])
        result["name"] = item["name"]
        result["output"] = trim(result["output"], MAX_DIFF_CHARS)
        results.append(result)
    failed = [result["name"] for result in results if result["exit_code"] != 0]
    return emit(
        {
            "results": results,
            "failed": failed,
            "note": "Attribute failures to the current diff before reporting them as findings.",
        }
    )


def main():
    parser = argparse.ArgumentParser(prog="cr-packet")
    sub = parser.add_subparsers(dest="cmd", required=True)

    p_build = sub.add_parser("build")
    p_build.add_argument(
        "--run-id",
        default=dt.datetime.now(dt.timezone.utc).strftime("r-%Y%m%d-%H%M%S-%f"),
    )
    p_build.set_defaults(func=build)

    p_packet = sub.add_parser("packet")
    p_packet.add_argument("--run-id", default="current")
    p_packet.set_defaults(func=packet_cmd)

    p_index = sub.add_parser("evidence-index")
    p_index.add_argument("--run-id", default="current")
    p_index.set_defaults(func=evidence_index)

    p_evidence = sub.add_parser("evidence")
    p_evidence.add_argument("--run-id", default="current")
    p_evidence.add_argument("--id", required=True)
    p_evidence.set_defaults(func=evidence)

    p_expand = sub.add_parser("expand")
    p_expand.add_argument("--run-id", default="current")
    p_expand.add_argument("--path", required=True)
    p_expand.add_argument("--start", type=int, default=1)
    p_expand.add_argument("--end", type=int, default=0)
    p_expand.set_defaults(func=expand)

    p_check = sub.add_parser("check")
    p_check.add_argument("--name", default="all")
    p_check.set_defaults(func=check)

    args = parser.parse_args()
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
