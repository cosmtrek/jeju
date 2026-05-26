import csv
from pathlib import Path

expected = [
    ("today", "ERROR", "1"),
    ("today", "WARNING", "1"),
    ("today", "INFO", "1"),
    ("last_7_days", "ERROR", "3"),
    ("last_7_days", "WARNING", "3"),
    ("last_7_days", "INFO", "4"),
    ("last_30_days", "ERROR", "4"),
    ("last_30_days", "WARNING", "4"),
    ("last_30_days", "INFO", "5"),
    ("month_to_date", "ERROR", "4"),
    ("month_to_date", "WARNING", "4"),
    ("month_to_date", "INFO", "5"),
    ("total", "ERROR", "5"),
    ("total", "WARNING", "4"),
    ("total", "INFO", "6"),
]

with Path("summary.csv").open(newline="") as f:
    rows = list(csv.reader(f))

if rows[0] != ["period", "severity", "count"]:
    raise SystemExit(f"bad header: {rows[0]}")

actual = [tuple(row) for row in rows[1:]]
if actual != expected:
    raise SystemExit(f"expected {expected}, got {actual}")

print("log-summary-date-ranges: PASS")
