# Amadeus Engineering Instructions

## Project Direction

Amadeus is a Go coding agent inspired by Codex and Claude Code. It is not a
complete reimplementation of either project. Use the reference repositories to
learn their architecture, data models, ownership boundaries, responsibility
splits, lifecycle rules, event and failure ordering, and underlying design
ideas. Adapt those ideas to Amadeus's product scope and to idiomatic Go.

`docs/design.md` is the primary working architecture and contract document, and
`docs/development-progress.md` records implementation status and superseded
decisions. Neither document is infallible: both may contain stale, incomplete,
or internally inconsistent conclusions. Do not implement an uncertain contract
blindly and do not resolve ambiguity by preserving the current Amadeus code.
When a design or progress entry is unclear, inspect the corresponding Codex
and/or Claude Code source to identify the real owner, data model, input/output
contract, lifecycle, event and failure ordering, and completion semantics. Then
make a deterministic decision suited to Amadeus's product scope and Go runtime,
implement that decision consistently, and update both `docs/design.md` and
`docs/development-progress.md` in the same change.

## Reference Responsibilities

Use `../codex-main` as the primary reference for the outer agent architecture:

- Thread, Session, SessionServices, Turn, ActiveTurn, RunningTask, SessionTask,
  and the single `run_turn` continuation loop.
- Submission, Event/EventMsg, TurnItem, canonical Rollout, identity, terminal
  completion, interruption, and resume semantics.
- Context and Prompt construction, ModelMessages, world state, token accounting,
  compaction, StepContext, and request-scoped ToolRouter snapshots.
- Slash Command dispatch, TUI event projection, Plan Mode, `update_plan`,
  `request_user_input`, `write_stdin`, and basic multi-agent lifecycle.

Use `../claude-code-main` as the primary reference for the inner Tool and
Approval behavior:

- ToolUseContext and the ValidateInput -> Prepare -> Permission -> Approval ->
  Execute lifecycle.
- The behavior and model guidance of `read`, `edit`, `write`, `glob`, and
  `grep`.
- Read-before-write, stale-file detection, structured diff preview,
  confirmation before mutation, permission evaluation, command rules, and
  Approval interaction design.

Do not copy Claude Code's outer runtime into Amadeus. Do not treat either
reference repository as a requirement to reproduce every product feature.

## Alignment Requirements

1. "Align with Codex/Claude Code" means alignment of architecture, data models,
   terminology and naming, ownership, responsibility boundaries, dependency
   direction, lifecycle, state transitions, event ordering, completion
   protocol, and failure behavior. Similar UI text, matching type names, or a
   thin wrapper does not constitute alignment.

2. Do not preserve an old implementation by renaming it or wrapping it in
   target-shaped types. Do not add Adapter, Facade, callback, side-channel, or
   string-to-event layers whose only purpose is to make the old execution path
   look like the target design. When the current model cannot express the
   target contract, replace the model and ownership boundary, migrate all
   production callers, and remove the superseded path in the same change. A
   business fact must have one owner and one completion protocol.

3. Amadeus is under active, unreleased development. Backward compatibility with
   old Amadeus data, configuration, protocol, Event, Rollout, SQLite schema,
   package APIs, or test fixtures is not required unless a task explicitly says
   otherwise. Old development data may be deleted and regenerated. Prefer
   removing obsolete readers, writers, decoders, migrations, aliases, wrappers,
   and compatibility tests over maintaining dual formats. Reliability for the
   current format, including crash recovery and truncated-tail handling, is
   still required.

4. Keep package and file structure clear and responsibility-driven. Packages
   should represent stable domain, runtime, application, interface, or adapter
   boundaries. Within a package, split files by cohesive behavior and give them
   responsibility-revealing names. Do not accumulate every stage of a refactor
   in the original file merely because it already exists. Move code when its
   owner changes, keep tests near the owning package, and avoid both monolithic
   files and artificial one-type packages.

5. Implement the target architecture using Go's language strengths rather than
   translating Rust or TypeScript structure literally:

   - Use a long-lived owner goroutine or serial event loop when it makes state
     ownership and ordering explicit.
   - Use typed, direction-restricted channels for lifecycle and ownership
     boundaries, not as a replacement for ordinary synchronous calls.
   - Give every goroutine an owner, cancellation path, exit condition, and test;
     do not create fire-and-forget goroutines.
   - Propagate `context.Context` through Provider, Tool, HTTP, process, and
     Approval work. Preserve cancellation causes and bound cleanup work with a
     timeout.
   - Use bounded concurrency only for operations whose contracts permit it, and
     restore deterministic protocol order when concurrent work completes.
   - Prefer single-owner state over pervasive locking. Use mutexes or atomic
     protocols only where state is genuinely shared across goroutines.
   - Make channel closure and resource shutdown ownership explicit and test
     interruption, cancellation, panic, and terminal races.

## Change Standard

Before implementing an architectural change, identify the reference behavior's
owner, input/output contract, lifecycle, persistence boundary, and failure
order. Resolve uncertain behavior from the relevant Codex and Claude Code
implementation rather than from naming similarity or the legacy Amadeus path.
Record the resolved target contract in `docs/design.md` and the corresponding
task, status, and superseded conclusions in `docs/development-progress.md`.
Complete the ownership migration and legacy cleanup together, then add contract
tests that protect behavior and dependency direction. Run focused tests and
`make check` before considering the change complete.
