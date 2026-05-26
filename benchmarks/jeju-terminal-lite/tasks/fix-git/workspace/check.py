import subprocess
from pathlib import Path

repo = Path("repo")
content = (repo / "index.html").read_text()
if "Jeju benchmark recovered change" not in content:
    raise SystemExit("missing recovered change in index.html")

branch = subprocess.check_output(["git", "-C", str(repo), "branch", "--show-current"], text=True).strip()
if branch != "master":
    raise SystemExit(f"expected master branch, got {branch}")

status = subprocess.check_output(["git", "-C", str(repo), "status", "--porcelain"], text=True)
if status.strip():
    raise SystemExit(f"repository is not clean: {status}")

print("fix-git: PASS")
