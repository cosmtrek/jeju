# Agent Team Mechanism Patterns

- Orchestrator-worker: a lead or manager decomposes a goal, dispatches
  bounded subtasks, collects results, and synthesizes the final answer.
- Agents-as-tools: a parent agent calls specialist agents through a tool-like
  interface.
- Handoff: control transfers from one specialist to another.
- Group chat: agents share one conversation, often managed by a speaker
  selector. This is flexible but can inflate context and blur responsibility.
- Shared task list and mailbox: agents coordinate through explicit task state
  and messages, at higher coordination cost.
- Artifact-first synthesis: workers write or return durable outputs that the
  lead references instead of copying full conversations.
- Verifier loop: task outputs are checked before final synthesis.

