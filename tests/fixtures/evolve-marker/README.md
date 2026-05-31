# Evolve Marker Fixture

This fixture validates the self-evolution loop with a deliberately weak target agent.

- Baseline target prompt does not mention `APPROVED_FIX_MARKER`, so task-level `expected.mustInclude` fails.
- The evolver emits a structured exact-replacement proposal for `instructions.system`.
- The accepted candidate prompt requires the marker, so train and selection should improve.

Run the cheap smoke path without external API calls:

```bash
./scripts/run-evolve-e2e.sh mock
```

Run the full real-provider path with Mimo:

```bash
export MIMO_API_KEY=...
./scripts/run-evolve-e2e.sh mimo
```
