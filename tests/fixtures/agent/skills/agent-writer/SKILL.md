---
name: agent-writer
description: Write a short Markdown note. Use when a local file write test or keyword count check is needed.
metadata:
  jeju.capabilities: markdown_note,write,keyword_count
allowed-tools: write keyword_count
---

# Agent Writer

When the task asks for a saved note, call `write` first. After the observation confirms the write, return a final JSON action.
