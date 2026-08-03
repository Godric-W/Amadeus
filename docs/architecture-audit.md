# Amadeus Architecture Integration Audit

Audit date: 2026-08-03

## Production Request Chain

```text
CLI/TUI input
  → parseAgentTask
      normal text       → react
      /plan <task>      → planned
  → Session Coordinator.BeginTask
      creates first Project/Session/Run/User Message lazily
      persists execution_mode + provider metadata
  → Instruction Resolver
      $AMADEUS_HOME/AGENTS.md
      project/directory AGENTS.md
  → ContextBuilder
      system prompt
      resolved developer instructions
      conversation summary/history
      interrupted work
      current user task
      available tools/skills
  → bootstrap Agent composition
      shared Provider Client
      shared Tool Registry/Executor/Policy/Audit/Snapshot
      direct ReAct Runner with visible TextDelta
      planned Task Runner with child TextDelta suppression
  → selected RunEngine
      react   → ReActEngine → one root Task → ReActRunner
      planned → Planner → serial graph → planned ReActRunner → Replanner loop
  → Event Sink
      Fullscreen TUI / InlineRenderer / Plain AgentRenderer
  → Coordinator.FinishTask
      completed: persist assistant message + usage
      interrupted/failed: persist bounded interrupted_context_json
```

## Component Contract Review

| Component | Contract | Audit result |
|---|---|---|
| CLI directive parsing | Mode is selected once per Run; stored objective excludes `/plan` | Connected and tested |
| Config/project root | Same validated config and `project.Root` feed Provider, tools, instructions, snapshots and LSP | Connected |
| ContextBuilder | Produces system/developer/history/user messages and bounded context metadata | Connected; both engines receive the same envelope |
| ReActEngine | Does not call Planner/Replanner; emits one root Task lifecycle | Added and tested |
| PlanExecuteEngine | Receives current planned Task as an explicit developer message; final Replanner answer emits TextDelta | Connected and tested |
| ReAct runners | Default runner exposes assistant TextDelta; planned runner suppresses child candidate text | Split to prevent output loss |
| Tool pipeline | Registry → argument validation → Path/Command guard → Approval → Audit → hooks → Result/Evidence | Shared by both execution modes |
| TUI | Event projection only; does not own Run or Conversation truth | Connected; running input queues serial tasks |
| Session Coordinator | Draft is memory-only; first real task atomically creates records | Connected |
| SQLite | Six tables remain; migration v4 adds `runs.execution_mode` | Migrated and legacy-tested |
| Interrupted work | No call-stack replay; next Run receives bounded context and rechecks workspace/instructions | Connected |

## Defects Found And Corrected

1. Production bootstrap always selected PlanExecuteEngine, forcing greetings through Planner and Replanner.
2. The single Runner used a planned-task event filter, so default ReAct assistant TextDelta was swallowed.
3. Plan final answers were written directly to CLI output and disappeared in fullscreen mode where output is discarded.
4. Bubble Tea mouse tracking prevented native terminal text selection.
5. Fullscreen input was replaced with a non-editable status block while a Run was active.
6. Terminal C1 detection matched raw UTF-8 bytes; Chinese characters such as `条` could be dropped because their encoding contains `0x9d`.
7. The TUI Session banner remained `draft` after the first persisted task.
8. SQLite Run records did not retain whether the user selected `react` or `planned`.

## Deliberate Current Limits

- `/plan` selects Plan-and-Execute but does not pause for plan review.
- Planned Task scheduling is serial; tool calls inside one ReAct step may still use configured parallel tool execution.
- Running TUI input is queued and executed after the active Run; it does not steer an in-flight model call.
- Native mouse selection is prioritized over application mouse-wheel capture; use PageUp/PageDown for viewport scrolling.
- Legacy DirectEngine, Verifier and Reflector code remains outside production bootstrap and should not be treated as the active path.
- Multi-Agent remains optional and is not part of the audited single-Agent production path.

## Verification Scope

The audit test set covers CLI, bootstrap, ReAct/Plan engines, ContextBuilder, Session Coordinator, memory and SQLite stores, migrations, TUI, ReAct runner, Policy and built-in tools. Full repository checks remain the final release gate.
