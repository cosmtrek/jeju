# Long Horizon Context Compression Fixture

This fixture is designed to exercise Jeju context compression with a real native
tool-calling provider.

The agent must call `chapter_probe` for multiple chapters. Each call returns a
large deterministic payload, so the run should cross the configured context
threshold, summarize older blocks, preserve recent native tool-call blocks, and
finish with a written report.

The checked-in manifest uses the mock provider only so CI can validate and
compile the fixture without credentials. The mock model does not run the full
compression workflow. Use the script below with a real provider key to exercise
context compression end to end.

The default script settings use `contextWindow=16000`,
`compressionThreshold=0.25`, and 12 paragraphs per chapter. This exercises the
normal summary regime: recent native tool blocks should fit while older blocks
are summarized. Set
`JEJU_LONG_HORIZON_CONTEXT_WINDOW=7000` and `JEJU_LONG_HORIZON_PARAGRAPHS=33`
for a stress regime that also forces emergency recent tool-result truncation.
Set `JEJU_MIMO_MODEL` or `JEJU_DEEPSEEK_MODEL` if the provider's current model
id differs from the script default.

Use `scripts/run-long-horizon-agent.sh` from the repository root.
