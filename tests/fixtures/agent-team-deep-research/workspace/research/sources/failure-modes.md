# Agent Team Failure Modes

- Unbounded task creation can consume too many runs and tokens.
- Lead context can explode if it includes every worker trajectory.
- Workers may duplicate each other's work.
- Workers may invent unsupported claims.
- The lead may hide failed tasks during final synthesis.
- Peer chat can create ambiguous ownership and hard-to-audit decisions.

