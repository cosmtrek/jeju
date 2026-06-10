# Agent Team Deep Research Fixture

This fixture validates the `kind: AgentTeam` lead-worker mechanism with mock
models and a local corpus.

Run from this fixture directory after `jeju team run` is available:

```bash
go run ../../../cmd/jeju team run \
  teams/agent-team-research.team.yaml \
  "Research agent team mechanisms and recommend the smallest Jeju implementation. Write final report to reports/agent-team-mechanism.md."
```

The expected report path is:

```text
workspace/research/reports/agent-team-mechanism.md
```

The expected team output root is:

```text
.jeju-dev/team/agent-team-deep-research/
```

