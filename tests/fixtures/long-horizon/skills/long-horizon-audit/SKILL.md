---
name: long-horizon-audit
description: Exercise context compression by requiring many large tool observations before a final report.
metadata:
  jeju.capabilities: context_compression,long_horizon,tool_calling
allowed-tools: chapter_probe write
---

# Long Horizon Audit

For this fixture, follow this exact workflow:

1. Call `chapter_probe` exactly once for each chapter number from 1 through 8, in ascending order.
2. After each tool result, retain the chapter id, the checkpoint code, the risk label, and the next action.
3. After chapter 8 has been observed, call `write` to save `reports/long-horizon-summary.md`.
4. The report must contain:
   - a title,
   - one bullet per chapter using the checkpoint code,
   - a final section named `Compression Probe Verdict`.
5. After the write succeeds, call `final_answer` with a short completion note.

Do not call `final_answer` before the report has been written.
