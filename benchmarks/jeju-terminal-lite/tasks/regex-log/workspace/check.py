import re
from pathlib import Path

pattern = Path("regex.txt").read_text().strip()
log_text = Path("sample.log").read_text()
matches = re.findall(pattern, log_text, re.MULTILINE)

normalized = []
for item in matches:
    if isinstance(item, tuple):
        item = next((part for part in reversed(item) if part), "")
    normalized.append(item)

expected = ["2025-08-01", "2025-08-04", "2025-02-29", "2025-12-02"]
if normalized != expected:
    raise SystemExit(f"expected {expected}, got {normalized}")

print("regex-log: PASS")
