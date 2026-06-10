# Jeju Constraints

- Normal agent execution follows load, validate, compile, run.
- Runtime receives a compiled agent and does not read YAML directly.
- Every run creates a run directory and a canonical trajectory.
- Tool calls pass through policy gates.
- File tools stay inside the configured workspace.
- Skills are disclosed and manually active; all skill assets are not injected
  by default.
- Trajectory is the canonical evidence record for model, tool, context,
  artifact, evaluation, and lifecycle events.
- AgentTeam should be an outer controller that calls normal child agent runs.

