Analyze all log files in `/app/logs`. Each filename follows
`YYYY-MM-DD_<source>.log`. Count `ERROR`, `WARNING`, and `INFO` lines for:

- `today`
- `last_7_days`
- `last_30_days`
- `month_to_date`
- `total`

Use `2025-08-12` as the current date. Write `/app/summary.csv` with this exact
header:

```text
period,severity,count
```

Rows must be ordered by period in the list above and severity in this order:
`ERROR`, `WARNING`, `INFO`.

Run `python3 check.py` to verify.
