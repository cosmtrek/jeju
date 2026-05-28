# Deep Research Fixture

This fixture defines a MiMo-backed deep research agent that searches the web through Exa and writes a Markdown report.

Required environment for a real run:

```bash
export MIMO_API_KEY=...
export EXA_API_KEY=...
```

Example:

```bash
cd tests/fixtures/deep-research
printf 'y\ny\n' | go run ../../../cmd/jeju run agents/deep-research.yaml "Research the latest Nvidia AI chip news and write a report"
```

The expected report path is `workspace/research/reports/deep-research.md`.
