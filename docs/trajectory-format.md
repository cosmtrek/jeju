# Trajectory Format

This document defines the target Jeju trajectory format. The trajectory is the
canonical append-only record for a run. Reports, final answers, evaluation
files, and interchange exports are derived from it.

## Goals

- Keep one self-contained, append-only event log per run.
- Make the raw log readable enough for debugging and audit.
- Project the same log into a timeline, trace tree, step view, report, or
  interchange format without losing runtime facts.
- Keep event types small and stable. Extend behavior through span kinds,
  status, attributes, metrics, content references, and artifact records.
- Preserve large model, tool, context, reasoning, final, and evaluation payloads
  without separate required artifact files.

## Non-Goals

- This format is not a web telemetry protocol.
- This format is not a training dataset format, though it should export cleanly
  to one.
- This format does not require Jeju to expose a public API outside `internal/`.
- Derived files such as `report.html`, `final.md`, or `evaluation.json` are not
  canonical once this format is active.

## Run Directory

The target run directory is:

```text
runs/<run_id>/
  trajectory.jsonl
  report.html        # optional derived report
```

`trajectory.jsonl` is required. Every line is one compact JSON object. Writers
must append complete lines and must not pretty-print events. Readers must not
depend on small line sizes; artifact chunks can be larger than the default
`bufio.Scanner` token limit.

## Event Envelope

Every event uses the same top-level envelope:

```json
{
  "schema_version": "jeju.trajectory.v1",
  "seq": 1,
  "event_id": "evt_000001",
  "type": "trajectory.header",
  "ts": "2026-06-04T10:20:30.123Z",
  "trajectory_id": "trj_20260604_102030_bfcl",
  "session_id": "20260604-102030-bfcl",
  "run_id": "20260604-102030-bfcl",
  "step_id": 1,
  "span_id": "span_step_001",
  "parent_span_id": "span_run",
  "actor": "runtime",
  "payload": {}
}
```

| Field | Required | Description |
| --- | --- | --- |
| `schema_version` | yes | Format version. It is repeated on every event so sampled lines are self-describing. |
| `seq` | yes | Strictly increasing event sequence. This is the primary ordering key. |
| `event_id` | yes | Stable event identifier inside the trajectory. |
| `type` | yes | Event type from the fixed set below. |
| `ts` | yes | Event write time in RFC3339 format with timezone. |
| `trajectory_id` | yes | Identifier for this trajectory document/log. |
| `session_id` | yes | Logical session identifier. Continuations can share it. In v1, normal one-shot runs usually set `session_id` to `run_id`; splitting sessions across trajectories is reserved for resumable runs. |
| `run_id` | yes | Jeju run identifier. |
| `step_id` | no | High-level agent step number. |
| `span_id` | no | Span identifier when the event belongs to a span. |
| `parent_span_id` | no | Parent span identifier for trace-tree projection. |
| `actor` | yes | Event source, such as `runtime`, `model:primary`, `tool:shell`, `policy`, or `evaluate`. |
| `payload` | yes | Event-specific data. Use `{}` when no payload is needed. |

## Event Types

The core event set is intentionally small:

| Type | Meaning |
| --- | --- |
| `trajectory.header` | First event. Declares run identity, agent identity, input, and format metadata. |
| `span.started` | Starts an operation with duration, input, output, status, metrics, or error. |
| `span.ended` | Ends a span and records its status, output, metrics, and error. |
| `message.created` | Adds a message to replayable conversation state. |
| `action.created` | Records a runtime action parsed from model output or produced internally. |
| `permission.decided` | Records a policy decision for a tool call or operation. |
| `artifact.created` | Creates an inline or chunked content artifact. |
| `artifact.chunk` | Appends one data chunk to a chunked artifact. |
| `artifact.finalized` | Completes a chunked artifact and records integrity metadata. |
| `run.summary` | Final run summary for fast inspection. It is derivable from prior events. |

An implementation may add diagnostic `run.note` events, but core behavior should
prefer the fixed event types above.

## Spans

Most execution work is represented as spans. A span is appropriate when the
operation has a start, end, duration, input, output, metrics, or error.

Span kinds:

| Kind | Meaning |
| --- | --- |
| `run` | Whole runtime execution. |
| `step` | One high-level agent loop step. |
| `llm` | One model request/response. |
| `tool` | One tool execution. |
| `policy` | One permission or policy gate when duration matters. |
| `context` | Context estimation, compression, summarization, or truncation. |
| `evaluator` | One evaluation pass. |
| `skill` | Skill disclosure or loading work when modeled as an operation. |
| `subagent` | Nested agent run. Reserved for future use. |
| `shell` | Shell command execution inside a tool, when the command needs a nested span. |

`span.started` example:

```json
{
  "schema_version": "jeju.trajectory.v1",
  "seq": 10,
  "event_id": "evt_000010",
  "type": "span.started",
  "ts": "2026-06-04T10:20:31.000Z",
  "trajectory_id": "trj_20260604_102030_bfcl",
  "session_id": "20260604-102030-bfcl",
  "run_id": "20260604-102030-bfcl",
  "step_id": 1,
  "span_id": "span_llm_001",
  "parent_span_id": "span_step_001",
  "actor": "model:primary",
  "payload": {
    "kind": "llm",
    "name": "primary",
    "input": { "content_ref": "art_model_input_001" },
    "attrs": {
      "provider": "deepseek",
      "model": "deepseek-v4",
      "temperature": 0.2
    }
  }
}
```

`span.ended` example:

```json
{
  "schema_version": "jeju.trajectory.v1",
  "seq": 13,
  "event_id": "evt_000013",
  "type": "span.ended",
  "ts": "2026-06-04T10:20:32.320Z",
  "trajectory_id": "trj_20260604_102030_bfcl",
  "session_id": "20260604-102030-bfcl",
  "run_id": "20260604-102030-bfcl",
  "step_id": 1,
  "span_id": "span_llm_001",
  "parent_span_id": "span_step_001",
  "actor": "model:primary",
  "payload": {
    "kind": "llm",
    "status": "ok",
    "output": { "content_ref": "art_model_output_001" },
    "metrics": {
      "latency_ms": 1320,
      "prompt_tokens": 800,
      "completion_tokens": 120,
      "total_tokens": 920
    }
  }
}
```

Failure is represented by `span.ended` with `status: "error"`:

```json
{
  "type": "span.ended",
  "span_id": "span_tool_001",
  "payload": {
    "kind": "tool",
    "status": "error",
    "error": {
      "code": "timeout",
      "message": "tool exceeded 30s timeout"
    }
  }
}
```

Span status values:

| Status | Meaning |
| --- | --- |
| `ok` | Operation completed successfully. |
| `error` | Operation failed. |
| `cancelled` | Operation was cancelled. |
| `skipped` | Operation was intentionally skipped. |

## Messages

`message.created` records replayable conversation state. It is separate from LLM
spans because a model call can produce tool calls, reasoning, or final text, and
runtime compression can copy or transform prior messages.

```json
{
  "type": "message.created",
  "step_id": 1,
  "actor": "runtime",
  "payload": {
    "message_id": "msg_assistant_001",
    "role": "assistant",
    "source": "agent",
    "content": [
      { "type": "text", "text": "I will inspect the file." }
    ],
    "reasoning_ref": "art_reasoning_001",
    "is_copied_context": false
  }
}
```

Message roles:

| Role | Meaning |
| --- | --- |
| `system` | System or developer instruction. |
| `user` | User task or user follow-up. |
| `assistant` | Model/agent response. |
| `tool` | Tool observation returned to the model. |

Content parts:

```json
{ "type": "text", "text": "..." }
{ "type": "json", "value": {} }
{ "type": "image", "source": { "media_type": "image/png", "content_ref": "art_img_001" } }
```

`is_copied_context` marks content copied from an earlier trajectory or earlier
context window rather than newly produced in this step. Exporters can use it to
avoid treating copied context as fresh training output.

## Actions

`action.created` records the runtime action parsed from a model output or
produced by deterministic runtime logic.

Action kinds:

| Kind | Meaning |
| --- | --- |
| `final` | The agent produced a final answer. |
| `tool_call` | The agent requested a tool call. |
| `ask_user` | The agent requested user input. |
| `context_management` | The runtime changed context state. |

Tool-call action example:

```json
{
  "type": "action.created",
  "step_id": 1,
  "actor": "runtime",
  "payload": {
    "action_id": "act_001",
    "kind": "tool_call",
    "thought": "Need to read the file.",
    "tool_call_id": "call_001_a",
    "function_name": "file_read",
    "arguments": { "path": "README.md" }
  }
}
```

Final action example:

```json
{
  "type": "action.created",
  "step_id": 3,
  "actor": "runtime",
  "payload": {
    "action_id": "act_003",
    "kind": "final",
    "final": { "content_ref": "art_final" }
  }
}
```

## Tool Calls

Tool requests are represented by `action.created` with `kind: "tool_call"`.
Tool execution is represented by `span.started` and `span.ended` with
`kind: "tool"`. The action and span are connected with `tool_call_id`.

```json
{
  "type": "span.ended",
  "span_id": "span_tool_001",
  "actor": "tool:file_read",
  "payload": {
    "kind": "tool",
    "status": "ok",
    "tool_call_id": "call_001_a",
    "output": { "content_ref": "art_tool_output_001" },
    "metrics": { "latency_ms": 18 }
  }
}
```

Use `tool_call_id` on both the action and tool span sides. The event type
already distinguishes request intent (`action.created`) from execution result
(`span.ended`), so consumers should not need a second field name for the same
correlation key.

### Complete Tool Call

A complete successful tool call is an event group. This is intentionally more
verbose than a single event because it preserves the model decision, policy
decision, execution duration, and output content independently:

```json
{"seq":10,"type":"action.created","step_id":1,"actor":"runtime","payload":{"kind":"tool_call","tool_call_id":"call_001_a","function_name":"file_write","arguments":{"path":"notes.md"}}}
{"seq":11,"type":"permission.decided","step_id":1,"actor":"policy","payload":{"tool_call_id":"call_001_a","tool":"file_write","decision":"approved","capabilities":["workspaceWrite"]}}
{"seq":12,"type":"span.started","step_id":1,"span_id":"span_tool_001","parent_span_id":"span_step_001","actor":"tool:file_write","payload":{"kind":"tool","name":"file_write","tool":"file_write","tool_call_id":"call_001_a","input":{"value":{"path":"notes.md"}}}}
{"seq":13,"type":"artifact.created","step_id":1,"actor":"runtime","payload":{"artifact_id":"art_tool_output_001","role":"tool_output","media_type":"application/json","encoding":"json","value":{"bytes":42,"path":"notes.md"}}}
{"seq":14,"type":"span.ended","step_id":1,"span_id":"span_tool_001","parent_span_id":"span_step_001","actor":"tool:file_write","payload":{"kind":"tool","status":"ok","tool":"file_write","tool_call_id":"call_001_a","output":{"content_ref":"art_tool_output_001"},"metrics":{"latency_ms":18}}}
```

Denied tool calls stop after `permission.decided`. They do not create a tool
execution span because the tool never ran.

## Permissions

Permission decisions are first-class audit facts. Use one event for the final
decision instead of separate checked, approved, and denied event types.

```json
{
  "type": "permission.decided",
  "step_id": 1,
  "actor": "policy",
  "payload": {
    "tool_call_id": "call_001_a",
    "tool": "file_write",
    "decision": "approved",
    "reason": "workspace write allowed",
    "capabilities": ["workspaceWrite"]
  }
}
```

Decision values:

| Decision | Meaning |
| --- | --- |
| `approved` | Policy allowed the operation. |
| `denied` | Policy denied the operation. |
| `ask` | Policy required user approval. |
| `auto_approved` | Runtime auto-approved the operation, for example in an isolated evolution run. |

## Artifacts

Artifacts store content inside the trajectory. Small text and JSON content can
be inline. Large or binary content should be chunked.

Inline artifact:

```json
{
  "type": "artifact.created",
  "actor": "runtime",
  "payload": {
    "artifact_id": "art_model_output_001",
    "role": "model_output",
    "media_type": "text/plain",
    "encoding": "utf-8",
    "text": "..."
  }
}
```

Structured JSON artifact:

```json
{
  "type": "artifact.created",
  "actor": "runtime",
  "payload": {
    "artifact_id": "art_tool_output_001",
    "role": "tool_output",
    "media_type": "application/json",
    "encoding": "json",
    "value": {
      "output": "ok"
    }
  }
}
```

Chunked artifact:

```json
{"type":"artifact.created","payload":{"artifact_id":"art_model_input_001","role":"model_input","media_type":"application/json","chunked":true}}
{"type":"artifact.chunk","payload":{"artifact_id":"art_model_input_001","index":0,"encoding":"base64","data":"..."}}
{"type":"artifact.finalized","payload":{"artifact_id":"art_model_input_001","bytes":128000,"sha256":"..."}}
```

Recommended inline threshold: 64 KiB per artifact. Chunk sizes should keep each
JSONL event comfortably readable and streamable.

Artifact roles:

| Role | Meaning |
| --- | --- |
| `config_snapshot` | Effective config snapshot used for the run. |
| `model_input` | Provider request payload or normalized model request. |
| `model_output` | Provider response text or normalized response. |
| `model_reasoning` | Provider reasoning content. |
| `tool_output` | Tool result wrapper or raw result. |
| `context_before` | Context before compression or truncation. |
| `context_after` | Context after compression or truncation. |
| `context_summary` | Generated context summary. |
| `context_report` | Context budgeting and compression report. |
| `final` | Final answer. |
| `evaluation` | Full evaluation result. |
| `workspace_file` | Snapshot or generated file content when captured by the runtime. |

## Context Management

Context estimation, compression, summarization, and truncation are represented
as `span` events with `kind: "context"`.

```json
{
  "type": "span.ended",
  "span_id": "span_context_006",
  "step_id": 6,
  "actor": "runtime",
  "payload": {
    "kind": "context",
    "status": "ok",
    "operation": "compaction",
    "boundary": "replace",
    "before": { "tokens": 120000, "content_ref": "art_context_before_006" },
    "after": { "tokens": 32000, "content_ref": "art_context_after_006" },
    "summary_ref": "art_context_summary_006",
    "metrics": {
      "before_tokens": 120000,
      "after_tokens": 32000,
      "truncated_tool_results": 4
    }
  }
}
```

Context boundaries:

| Boundary | Meaning |
| --- | --- |
| `replace` | Later context replaces older raw context with summary or compacted content. |
| `append` | Runtime injected additional context. |
| `truncate` | Runtime dropped older content without summary replacement. |

## Evaluation

Evaluation is optional. A trajectory can represent a normal agent run without
any evaluator. Run success is determined by the runtime outcome, not by whether
an evaluator exists or passes.

When evaluation runs, represent it as an evaluator span. Store the full
evaluation result in the span output and, for large results, also as an
`evaluation` artifact.

```json
{
  "type": "span.ended",
  "span_id": "span_eval_001",
  "actor": "evaluate",
  "payload": {
    "kind": "evaluator",
    "status": "ok",
    "output": {
      "passed": true,
      "score": 1,
      "evaluators": [
        {
          "name": "rules",
          "type": "rule",
          "passed": true,
          "score": 1,
          "results": [
            { "rule": "finalAnswerExists", "passed": true }
          ]
        }
      ]
    }
  }
}
```

Evaluation failures are `span.ended` events with `status: "error"` and a normal
`error` object.

## Run Summary

The final event should be `run.summary`. It is a convenience snapshot for fast
inspection, not a separate source of truth. Readers should derive counts,
tokens, tool calls, and span state from prior events. If `run.summary.stats`
disagrees with the event stream, the event stream wins.

```json
{
  "type": "run.summary",
  "actor": "runtime",
  "payload": {
    "status": "completed",
    "started_at": "2026-06-04T10:20:30.000Z",
    "ended_at": "2026-06-04T10:20:35.000Z",
    "duration_ms": 5000,
    "final": { "content_ref": "art_final" },
    "stats": {
      "steps": 3,
      "model_calls": 2,
      "tool_calls": 1,
      "permission_denied": 0,
      "model_errors": 0,
      "tool_errors": 0,
      "total_tokens": 1800
    },
    "evaluation": {
      "passed": true,
      "score": 1
    }
  }
}
```

Run status describes the agent task outcome only:

| Status | Meaning |
| --- | --- |
| `completed` | A final output was produced and no runtime hard constraint stopped the run. Evaluation is not required. |
| `failed` | No final output was produced, or a runtime hard constraint stopped the run. |
| `cancelled` | Run was cancelled. |
| `waiting_user` | Run is waiting for user input. Reserved for resumable runs. |
| `interrupted` | The trajectory is partial and the runtime outcome cannot be confirmed. |

Runtime hard constraints include max steps, max duration, max tool calls,
context overflow, unrecoverable model/tool errors, and policy/user cancellation.
A failed optional evaluator does not change `run.summary.status`; it is
evaluation metadata.

## Trajectory Integrity

Trajectory integrity is separate from run status:

| Integrity | Meaning |
| --- | --- |
| `complete` | The log has a header, a run summary, closed spans, and complete artifact records. |
| `partial` | The log is readable and useful, but expected events are missing, such as `run.summary`, a `span.ended`, or an `artifact.finalized`. |
| `corrupt` | The log has structural damage such as sequence gaps, malformed chunks, or unparseable JSON. |

Readers should still project partial trajectories when possible. A missing
`run.summary` does not imply failure: if a final artifact or final action is
present and no hard-constraint failure is recorded, a reader may project
`status: "completed"` with `integrity: "partial"`.

Recommended integrity issue codes are strings such as:

- `partial:missing_run_summary`
- `partial:open_span:<span_id>`
- `partial:unfinalized_artifact:<artifact_id>`
- `corrupt:seq_gap`
- `corrupt:artifact_chunk_base64:<artifact_id>`

## Metrics

Metric names should be stable across providers:

| Metric | Meaning |
| --- | --- |
| `latency_ms` | Wall-clock duration for an operation. |
| `prompt_tokens` | Input tokens sent to a model. |
| `completion_tokens` | Output tokens returned by a model. |
| `reasoning_tokens` | Provider-reported reasoning tokens when available. |
| `cached_tokens` | Provider-reported cached input tokens when available. |
| `total_tokens` | Total model tokens for the operation or run. |
| `cost_usd` | Estimated or provider-reported cost in USD. |
| `bytes` | Content size in bytes. |
| `exit_code` | Shell or command exit code. |

Reader compatibility can map legacy `tokens_in` to `prompt_tokens` and
`tokens_out` to `completion_tokens`.

## Projection Rules

The raw trajectory can be projected into richer views:

- Timeline: sort by `seq`.
- Trace tree: group spans by `span_id` and `parent_span_id`.
- Step view: group events by `step_id`.
- Conversation replay: read `message.created` events in `seq` order.
- Tool view: pair `action.created.tool_call_id` with
  `span.ended.tool_call_id`.
- Artifact view: reconstruct inline or chunked artifacts by `artifact_id`.
- Final answer: read the final action or `run.summary.final` content reference.
- Evaluation result: read the evaluator span output or referenced evaluation
  artifact.
- Integrity: derive from structural checks such as header/summary presence,
  span pairing, sequence continuity, and artifact finalization.

## Interchange Export

Jeju's native format remains an append-only audit log. Exporters can convert it
to a step-oriented interchange format by projecting:

- `trajectory_id` and `session_id` to interchange identity fields.
- `message.created` events to message steps.
- `action.created kind=tool_call` plus tool spans to tool calls and
  observations.
- `span kind=llm` metrics to per-step token, latency, and cost metrics.
- `is_copied_context` and `span kind=context` to context-management metadata.
- `run.summary` and evaluator spans to final metrics.

The reverse direction should be treated as import, not as the runtime source of
truth. Runtime execution writes events; higher-level formats are views.

## Minimal Example

```jsonl
{"schema_version":"jeju.trajectory.v1","seq":1,"event_id":"evt_000001","type":"trajectory.header","ts":"2026-06-04T10:20:30.000Z","trajectory_id":"trj_001","session_id":"20260604-102030-bfcl","run_id":"20260604-102030-bfcl","actor":"runtime","payload":{"agent":{"name":"bfcl-lite"},"input":"Solve the task."}}
{"schema_version":"jeju.trajectory.v1","seq":2,"event_id":"evt_000002","type":"artifact.created","ts":"2026-06-04T10:20:30.010Z","trajectory_id":"trj_001","session_id":"20260604-102030-bfcl","run_id":"20260604-102030-bfcl","actor":"runtime","payload":{"artifact_id":"art_config","role":"config_snapshot","media_type":"application/x-yaml","encoding":"utf-8","text":"metadata:\n  name: bfcl-lite\n"}}
{"schema_version":"jeju.trajectory.v1","seq":3,"event_id":"evt_000003","type":"span.started","ts":"2026-06-04T10:20:30.020Z","trajectory_id":"trj_001","session_id":"20260604-102030-bfcl","run_id":"20260604-102030-bfcl","span_id":"span_run","actor":"runtime","payload":{"kind":"run","name":"bfcl-lite"}}
{"schema_version":"jeju.trajectory.v1","seq":4,"event_id":"evt_000004","type":"span.started","ts":"2026-06-04T10:20:30.030Z","trajectory_id":"trj_001","session_id":"20260604-102030-bfcl","run_id":"20260604-102030-bfcl","step_id":1,"span_id":"span_step_001","parent_span_id":"span_run","actor":"runtime","payload":{"kind":"step","name":"step 1"}}
{"schema_version":"jeju.trajectory.v1","seq":5,"event_id":"evt_000005","type":"span.started","ts":"2026-06-04T10:20:30.040Z","trajectory_id":"trj_001","session_id":"20260604-102030-bfcl","run_id":"20260604-102030-bfcl","step_id":1,"span_id":"span_llm_001","parent_span_id":"span_step_001","actor":"model:primary","payload":{"kind":"llm","name":"primary","input":{"content_ref":"art_model_input_001"}}}
{"schema_version":"jeju.trajectory.v1","seq":6,"event_id":"evt_000006","type":"span.ended","ts":"2026-06-04T10:20:31.400Z","trajectory_id":"trj_001","session_id":"20260604-102030-bfcl","run_id":"20260604-102030-bfcl","step_id":1,"span_id":"span_llm_001","parent_span_id":"span_step_001","actor":"model:primary","payload":{"kind":"llm","status":"ok","output":{"content_ref":"art_model_output_001"},"metrics":{"prompt_tokens":800,"completion_tokens":120,"total_tokens":920,"latency_ms":1370}}}
{"schema_version":"jeju.trajectory.v1","seq":7,"event_id":"evt_000007","type":"artifact.created","ts":"2026-06-04T10:20:31.410Z","trajectory_id":"trj_001","session_id":"20260604-102030-bfcl","run_id":"20260604-102030-bfcl","actor":"runtime","payload":{"artifact_id":"art_model_output_001","role":"model_output","media_type":"text/plain","encoding":"utf-8","text":"{\"type\":\"final\",\"content\":\"Done.\"}"}}
{"schema_version":"jeju.trajectory.v1","seq":8,"event_id":"evt_000008","type":"artifact.created","ts":"2026-06-04T10:20:31.420Z","trajectory_id":"trj_001","session_id":"20260604-102030-bfcl","run_id":"20260604-102030-bfcl","actor":"runtime","payload":{"artifact_id":"art_final","role":"final","media_type":"text/markdown","encoding":"utf-8","text":"Done."}}
{"schema_version":"jeju.trajectory.v1","seq":9,"event_id":"evt_000009","type":"span.ended","ts":"2026-06-04T10:20:31.425Z","trajectory_id":"trj_001","session_id":"20260604-102030-bfcl","run_id":"20260604-102030-bfcl","step_id":1,"span_id":"span_step_001","parent_span_id":"span_run","actor":"runtime","payload":{"kind":"step","status":"ok"}}
{"schema_version":"jeju.trajectory.v1","seq":10,"event_id":"evt_000010","type":"span.ended","ts":"2026-06-04T10:20:31.428Z","trajectory_id":"trj_001","session_id":"20260604-102030-bfcl","run_id":"20260604-102030-bfcl","span_id":"span_run","actor":"runtime","payload":{"kind":"run","status":"ok"}}
{"schema_version":"jeju.trajectory.v1","seq":11,"event_id":"evt_000011","type":"run.summary","ts":"2026-06-04T10:20:31.430Z","trajectory_id":"trj_001","session_id":"20260604-102030-bfcl","run_id":"20260604-102030-bfcl","actor":"runtime","payload":{"status":"completed","final":{"content_ref":"art_final"},"stats":{"steps":1,"model_calls":1,"tool_calls":0,"total_tokens":920}}}
```
