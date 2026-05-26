# Jeju V0 Technical Spec

Jeju is a local-first, config-defined mini agent platform.

Core lifecycle:

```text
Manifest -> Validate -> Compile -> Run -> Permission Gate -> Trajectory -> Evaluate -> Inspect
```

V0 intentionally focuses on one clean single-agent loop:

- Agent behavior is declared in YAML.
- Runtime receives a compiled agent, not raw YAML.
- Every run creates a run directory and saves `metadata.json`, `config.snapshot.yaml`, `trajectory.jsonl`, `final.md`, optional `evaluation.json`, and `artifacts/`.
- Every model call, tool call, permission decision, skill event, and evaluation is recorded in JSONL trajectory.
- Large payloads are stored as artifacts and referenced from events.
- Tools with side effects pass through `PermissionGate`.
- File tools are restricted to the configured workspace.
- Shell runs in the workspace and has a timeout.
- Skills are disclosed first and only active skills load instructions.

Out of scope for V0:

- Web UI
- Multi-agent runtime
- Docker or remote sandbox
- Full MCP client
- Long-term memory
- Replay and diff
- LLM evaluators
