#!/usr/bin/env python3
"""Compare Jeju structured-patch evolution with a paper-style workspace edit.

This keeps the task set, solver/evolver model, and evaluator fixed. The only
mechanism difference is how the evolver updates the candidate harness:

- Jeju: `jeju evolve` parses structured JSON proposals and applies patches.
- Paper-style: the evolver receives a bash tool rooted at a candidate bundle
  and directly edits the allowed harness files.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[3]
EXAMPLE_ROOT = REPO_ROOT / "examples" / "skillsbench-lite-agent"
EXPERIMENT = EXAMPLE_ROOT / "experiments" / "skillsbench-lite-evolve.yaml"
DEFAULT_OUT_ROOT = REPO_ROOT / ".jeju-dev" / "evolve" / "skillsbench-lite-mechanism"

ALLOWED_EDIT_FILES = {
    "agents/solver.agent.yaml",
    "prompts/solver.md",
    "skills/skillsbench-lite/SKILL.md",
}


PAPER_STYLE_SYSTEM = """\
You are a meta-learning agent that improves another agent by modifying its workspace files.

The workspace contains a Jeju agent bundle. You may inspect files and directly edit the
allowed harness files using the workspace_bash tool.

Your job each cycle:
1. Analyze task observation logs and identify recurring failures.
2. Improve the agent harness for future tasks.
3. Use the provided bash tool to read/write files in the workspace.
4. Verify your changes with `git diff` before finishing.

Guidelines:
- Quality over quantity. Make targeted edits that help held-out tasks.
- The skillsbench-lite skill uses SKILL.md format with YAML frontmatter.
- Do not memorize task ids or split names.
"""


def run(cmd: list[str], *, cwd: Path = REPO_ROOT) -> subprocess.CompletedProcess[str]:
    print("+", " ".join(cmd), flush=True)
    return subprocess.run(cmd, cwd=cwd, text=True, capture_output=True, check=True)


def run_jeju_structured(out_dir: Path, max_iterations: int) -> Path:
    proc = run(
        [
            "go",
            "run",
            "./cmd/jeju",
            "evolve",
            "--test",
            "--max-iterations",
            str(max_iterations),
            "--out",
            str(out_dir),
            str(EXPERIMENT),
        ]
    )
    matches = []
    for line in proc.stdout.splitlines():
        parts = line.split()
        if len(parts) == 2 and parts[0] == "output":
            matches.append(Path(parts[1]).resolve())
    if matches:
        return matches[-1]
    raise RuntimeError(f"could not parse Jeju output dir from:\n{proc.stdout}")


def load_metrics(results_path: Path) -> dict[str, dict[str, float]]:
    raw = json.loads(results_path.read_text())
    return {split: data["metrics"] for split, data in raw["results"].items()}


def copy_candidate_workspace(dest: Path) -> None:
    if dest.exists():
        shutil.rmtree(dest)
    ignore = shutil.ignore_patterns("workspace", ".jeju-dev", "__pycache__")
    shutil.copytree(EXAMPLE_ROOT, dest, ignore=ignore)
    run(["git", "init", "-q"], cwd=dest)
    run(["git", "add", "."], cwd=dest)


def snapshot_workspace(root: Path) -> dict[str, bytes]:
    snapshot: dict[str, bytes] = {}
    for path in sorted(root.rglob("*")):
        if path.is_file() and ".git" not in path.parts:
            snapshot[str(path.relative_to(root))] = path.read_bytes()
    return snapshot


def restore_disallowed_changes(root: Path, before: dict[str, bytes]) -> list[str]:
    restored: list[str] = []
    before_keys = set(before)
    for path in sorted(root.rglob("*"), reverse=True):
        if not path.is_file() or ".git" in path.parts:
            continue
        rel = str(path.relative_to(root))
        if rel in ALLOWED_EDIT_FILES:
            continue
        if rel not in before_keys:
            path.unlink()
            restored.append(rel)
        elif path.read_bytes() != before[rel]:
            path.write_bytes(before[rel])
            restored.append(rel)
    for rel, content in before.items():
        if rel in ALLOWED_EDIT_FILES:
            continue
        path = root / rel
        if not path.exists():
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)
            restored.append(rel)
    for path in sorted(root.rglob("*"), reverse=True):
        if ".git" not in path.parts and path.is_dir() and not any(path.iterdir()):
            path.rmdir()
    return sorted(set(restored))


def workspace_bash(root: Path, command: str) -> str:
    blocked = ["..", "~", "/Users/", "rm -rf", "curl ", "wget ", "ssh ", "sudo "]
    if any(token in command for token in blocked):
        return "ERROR: command rejected by local experiment guard."
    try:
        result = subprocess.run(
            ["bash", "-c", command],
            cwd=root,
            text=True,
            capture_output=True,
            timeout=60,
        )
    except subprocess.TimeoutExpired:
        return "ERROR: command timed out."
    output = (result.stdout + result.stderr).strip()
    return output or "(no output)"


def deepseek_chat(
    *,
    model: str,
    messages: list[dict[str, Any]],
    tools: list[dict[str, Any]],
    temperature: float,
    max_tokens: int,
) -> dict[str, Any]:
    api_key = os.environ.get("DEEPSEEK_API_KEY")
    if not api_key:
        raise RuntimeError("DEEPSEEK_API_KEY is not set")
    payload = {
        "model": model,
        "messages": messages,
        "tools": tools,
        "temperature": temperature,
        "max_tokens": max_tokens,
    }
    req = urllib.request.Request(
        "https://api.deepseek.com/chat/completions",
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=180) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"DeepSeek HTTP {exc.code}: {body}") from exc


def paper_style_evolve(
    *,
    candidate_root: Path,
    feedback_digest: dict[str, Any],
    model: str,
    max_tool_turns: int,
) -> dict[str, Any]:
    tools = [
        {
            "type": "function",
            "function": {
                "name": "workspace_bash",
                "description": "Execute a bash command in the candidate workspace.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "command": {
                            "type": "string",
                            "description": "Bash command to execute.",
                        }
                    },
                    "required": ["command"],
                },
            },
        }
    ]
    user = {
        "role": "user",
        "content": (
            "## Workspace permissions\n"
            "You MAY edit only these files:\n"
            "- agents/solver.agent.yaml\n"
            "- prompts/solver.md\n"
            "- skills/skillsbench-lite/SKILL.md\n\n"
            "To activate the skill, edit agents/solver.agent.yaml so skills.active "
            "contains skillsbench-lite.\n\n"
            "## Evidence digest\n```json\n"
            + json.dumps(feedback_digest, ensure_ascii=False, indent=2)
            + "\n```\n\n"
            "Use workspace_bash to inspect and edit files. Finish after `git diff` shows the intended changes."
        ),
    }
    messages: list[dict[str, Any]] = [
        {"role": "system", "content": PAPER_STYLE_SYSTEM},
        user,
    ]
    calls: list[dict[str, str]] = []
    final_content = ""
    for _ in range(max_tool_turns):
        response = deepseek_chat(
            model=model,
            messages=messages,
            tools=tools,
            temperature=0.1,
            max_tokens=4096,
        )
        choice = response["choices"][0]["message"]
        final_content = choice.get("content") or ""
        messages.append(choice)
        tool_calls = choice.get("tool_calls") or []
        if not tool_calls:
            break
        for tool_call in tool_calls:
            fn = tool_call.get("function", {})
            args = json.loads(fn.get("arguments") or "{}")
            command = str(args.get("command", ""))
            output = workspace_bash(candidate_root, command)
            calls.append({"command": command, "output": output[:2000]})
            messages.append(
                {
                    "role": "tool",
                    "tool_call_id": tool_call["id"],
                    "content": output,
                }
            )
    return {"final": final_content, "tool_calls": calls}


def evaluate_candidate(candidate_root: Path, out_dir: Path) -> Path:
    proc = run(
        [
            "go",
            "run",
            "./cmd/jeju",
            "evolve",
            "--baseline-only",
            "--test",
            "--out",
            str(out_dir),
            str(candidate_root / "experiments" / "skillsbench-lite-evolve.yaml"),
        ]
    )
    matches = []
    for line in proc.stdout.splitlines():
        parts = line.split()
        if len(parts) == 2 and parts[0] == "output":
            matches.append(Path(parts[1]).resolve())
    if matches:
        return matches[-1]
    raise RuntimeError(f"could not parse evaluation output dir from:\n{proc.stdout}")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--model", default="deepseek-v4-flash")
    parser.add_argument("--max-iterations", type=int, default=1)
    parser.add_argument("--max-tool-turns", type=int, default=8)
    parser.add_argument("--out-root", type=Path, default=DEFAULT_OUT_ROOT)
    parser.add_argument("--jeju-run-dir", type=Path, help="Reuse an existing Jeju evolve run dir.")
    args = parser.parse_args()

    stamp = time.strftime("%Y%m%d-%H%M%S")
    root = args.out_root / stamp
    root.mkdir(parents=True, exist_ok=True)

    jeju_run = args.jeju_run_dir.resolve() if args.jeju_run_dir else run_jeju_structured(root / "jeju", args.max_iterations)
    feedback_digest = json.loads((jeju_run / "iterations" / "001" / "feedback_digest.json").read_text())

    paper_candidate = root / "paper-style-candidate"
    copy_candidate_workspace(paper_candidate)
    before = snapshot_workspace(paper_candidate)
    paper_trace = paper_style_evolve(
        candidate_root=paper_candidate,
        feedback_digest=feedback_digest,
        model=args.model,
        max_tool_turns=args.max_tool_turns,
    )
    restored = restore_disallowed_changes(paper_candidate, before)
    paper_eval = evaluate_candidate(paper_candidate, root / "paper-style-eval")

    report = {
        "model": args.model,
        "jeju_run": str(jeju_run),
        "paper_candidate": str(paper_candidate),
        "paper_eval": str(paper_eval),
        "jeju_baseline": load_metrics(jeju_run / "baseline" / "results.json"),
        "jeju_structured": load_metrics(jeju_run / "best" / "results.json"),
        "paper_style": load_metrics(paper_eval / "baseline" / "results.json"),
        "paper_trace": paper_trace,
        "paper_restored_disallowed": restored,
    }
    (root / "mechanism_comparison.json").write_text(json.dumps(report, ensure_ascii=False, indent=2))
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
