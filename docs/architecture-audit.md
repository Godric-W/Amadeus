# Amadeus Architecture Integration Audit

Audit date: 2026-08-04
Target architecture: `docs/design.md` ADR-011、ADR-014、ADR-017、ADR-018、ADR-019、ADR-020
Implementation plan: `docs/development-progress.md` M6R、M8T
Result: M6R runtime architecture and M8T Rich Inline product requirements are implemented; final verification commands pass.

## Production Request Chain

```text
CLI / Rich Inline TUI / --plain input
  → parseAgentTask
      normal text       → standalone Reactor
      /plan <task>      → Plan Controller
  → Session Coordinator.BeginRun
      lazily creates Project / Session / Run / user Message
      loads completed Run Message Pairs only
      recovers abandoned running Runs as bounded Previous Work
  → Instruction Resolver
      $AMADEUS_HOME/AGENTS.md
      project and directory AGENTS.md
  → BaseContextBuilder
      stable system prompt
      resolved developer instructions
      completed conversation Message Pairs
      bounded Summary
      bounded Previous Work and workspace revalidation
      current user objective
      available tools and Skills
  → execution path
      react   → Reactor: Think → Analyze → Act → Observe
      planned → Planner → ExecutionGraph → serial ready-task selection
                → ReActTaskExecutor → same Reactor → Replanner
  → Per-Think ContextWindowManager
      creates a fresh RequestView for every Provider call
      preserves Message Pair and Tool Call/Result atomic groups
      projects oversized Tool Results within the Provider context window
  → shared ToolExecutor safety pipeline
      Registry → argument normalization/schema validation
      → PathGuard / CommandGuard / Approval / Audit / Snapshot
      → bounded resource-aware execution → Observation / Evidence
  → Typed EventHub
      ContextWindowUpdated and safe Tool Activity metadata
      Rich Inline TUI / Plain Renderer / Audit subscribers
      SessionID / RunID / optional TaskID / Iteration / LLMCallID
  → Rich Inline projection
      responsive static Logo and workspace metadata
      assistant / Plan / Explored / Ran / Called transcript hierarchy
      Working animation and editable queued input
      bounded Ctrl+T detail viewer on terminal main screen
  → Session Coordinator.FinishRun
      completed: atomically persists assistant Message and usage
      interrupted/failed: persists bounded Previous Work JSON
```

## Requirement Audit

| M6R requirement | Authoritative implementation evidence | Verification evidence | Result |
|---|---|---|---|
| Session APIs use Run semantics | `internal/session/coordinator.go`、`internal/session/store.go` | coordinator, memory and SQLite tests | PASS |
| Conversation contains completed Run Message Pairs only | `ListCompletedMessages` in Memory/SQLite stores | `internal/session/*_test.go` and continuation command tests | PASS |
| Interrupted/failed work is bounded Previous Work | `internal/session/interrupted.go`、`internal/context/envelope.go` | Previous Work decode, ordering, abandoned Run and continuation tests | PASS |
| Reactor Domain is independent from Plan | `internal/agent/react` imports no Plan package | package-import guard and Reactor tests | PASS |
| Reactor uses Think→Analyze→Act→Observe | `internal/agent/react/phases.go`、`runner.go` | phase fakes, retry, argument error, blocked/stalled, budget and cancellation tests | PASS |
| Each Think receives a fresh bounded RequestView | `internal/context/window.go` | Message Pair atomicity, Tool projection and usage-feedback tests | PASS |
| Default input does not create Task/ExecutionGraph | `cmd/amadeus/coding_agent_command.go` directly invokes `agent.Runner.Run` | default greeting and default Coding Agent tests assert zero Planner calls | PASS |
| `/plan` reuses the same Reactor | `internal/agent/plan/react_task_executor.go` and bootstrap composition | adapter mapping, multi-cycle Controller and Provider E2E tests | PASS |
| Typed EventHub fans out one event stream | `internal/agent/event/hub.go` | filtering, multiple subscriber, slow consumer, critical backpressure, close and race tests | PASS |
| Events use Run/Task/Iteration/LLMCall metadata | `internal/agent/event/context.go`、`metadata.go` | metadata merge and lifecycle correlation tests | PASS |
| Old production Engine chain is removed | no `internal/agent/engine` or `internal/agent/reflect` package remains | architecture guard search and full build | PASS |
| Run budget uses Iteration terminology | `max_iterations` / `iterations_used` in config, Reactor and Plan state | config tests and Plan JSON regression test | PASS |
| Successful Run events do not report fake failure reasons | `reactorRunEventReason` | focused command regression test | PASS |

## M8T Requirement Audit

| M8T requirement | Authoritative implementation evidence | Verification evidence | Result |
|---|---|---|---|
| Responsive terminal branding | `internal/interface/tui/logo_generated.go`、`banner()`、`bannerMetadataRow()` | 40/60/100/160 width matrix, initial-width and real 40/100-column PTY smoke | PASS |
| Workspace metadata is bounded and optional | `internal/interface/tui/workspace.go` | branch, detached HEAD, non-repository and timeout-safe resolver tests | PASS |
| Main-screen Rich Inline remains native-scrollback friendly | `FullscreenApplication.Run` uses no alternate-screen or mouse-enable option; completed entries use `tea.Println` | control-sequence guard checks alternate-screen and mouse-enable sequences; real PTY exit restores terminal | PASS |
| Assistant, Plan and Tool hierarchy matches `docs/tui.md` | `renderEntry`、`wrapActivityContent`、`renderIterationActivities` | assistant bullet/ANSI blank-line, Plan tree, Explored grouping, command wrapping and no-empty-separator tests | PASS |
| Tool presentation is safe | `internal/tool/presentation.go`; `ToolCallStarted` carries only SideEffect/ActionSummary/Detail | credential redaction, unknown Tool fallback, normalized-call event and no raw-argument payload tests | PASS |
| Parallel Tool completion does not reorder transcript | `iterationActivity.CallIDs` and CallID updates | original-call-order regression test and Reactor parallel execution tests | PASS |
| Working and cancellation remain interactive | `fullscreenWorkingTick`、`workingLine`、editable Bubbles textarea | running queue, Esc/Ctrl+C cancellation, Chinese input/backspace and active-draft tests | PASS |
| Context status uses Provider window semantics | `ContextWindowUpdated` event and Reactor publish after each RequestView | metadata/compaction event test and TUI Provider-usage-vs-context-estimate test | PASS |
| Transcript details are bounded and non-persistent | `transcript_detail.go` and temporary Bubbles viewport | per-item/per-Run bounds, UTF-8 head/tail, replacement, redaction and Ctrl+T/Esc tests | PASS |
| Rich Inline degradation does not regress Plain/Session/Approval | terminal capability routing and existing Plain handlers | non-TTY, `TERM=dumb`, `--plain`, no-color, Approval, `/resume`, queue and command tests | PASS |

## M6R-22 Scenario Evidence

| Scenario | Test evidence |
|---|---|
| Greeting stays on standalone ReAct | `TestDefaultGreetingUsesStandaloneReactorWithoutPlanner` |
| Read/change/test coding workflow | `TestCodingAgentCommandReadsFixesTestsAndCompletes` |
| Tool failure is observed and recovered | `TestDefaultReactorRecoversFromToolFailure` and Reactor argument/tool replay tests |
| Path boundary produces blocked | `TestRunnerStopsBlockedOnPathBoundary` |
| Repetition/no progress produces stalled | `TestRunnerReportsStalledWithoutRequestingPlan` and progress monitor tests |
| Cancellation followed by “请继续” creates a new Run | `TestInterruptedRunCreatesNewRunWithReplanEnvelope` |
| A new task remains current while old work is background | `TestNewTaskAfterInterruptionKeepsPreviousWorkAsBackground` |
| `/plan` executes multiple plan/replan cycles | `TestControllerRunsPlanThenReplans`、`TestControllerPublishesEachReplanCycle` and Provider replan E2E |
| Session continue/resume and abandoned Run recovery | `TestSessionPersistsAcrossRootCommandInstancesAndContinueReplaysHistory`、`TestContinueRecoversAbandonedRunningRun`、resume selector tests |
| Rich Inline TUI and `--plain` remain connected | TUI inline tests, render tests, terminal approval and dumb-terminal fallback tests |
| Responses and Chat Completions dialects | `TestCodingAgentProviderMockE2E` and `TestCodingAgentProviderMockE2EReplansBeforeCompletion` |

## Architecture Guards

Production source search returns no references to:

```text
TurnID / TypeTurn
DirectEngine / ReActEngine / PlanExecuteEngine / Adaptive Engine
TaskOutcomeNeedsPlan
synthetic root Task constructors
internal/agent/engine
internal/agent/reflect
max_steps / steps_used
dead Engine retry or Reflection Prompt bundles
```

The legacy SQLite migration intentionally retains old `turn_id`、`session_turns` and `needs_plan` strings so existing databases can be migrated without data loss. Negative regression tests may also contain forbidden names as test fixtures; neither case is a production execution dependency.

## Verification Commands

The following commands pass on 2026-08-04:

```bash
GOMODCACHE=/tmp/amadeus-go-mod GOCACHE=/tmp/amadeus-go-build make check
GOMODCACHE=/tmp/amadeus-go-mod GOCACHE=/tmp/amadeus-go-build go test -race ./... -count=1
```

`make check` covers formatting, `go vet ./...`, `go test ./...` and building `bin/amadeus`.

## Persistence Invariants

- SQLite remains six tables: `schema_migrations`、`projects`、`conversation_sessions`、`runs`、`conversation_messages`、`conversation_summaries`.
- One real user input atomically creates one persisted Run and user Message.
- A completed Run atomically adds its assistant Message; ordinary Conversation loading returns both Messages in sequence.
- An interrupted or failed Run never contributes its orphan user Message to ordinary Conversation history.
- The latest interrupted/failed Run newer than the latest completed Run supplies bounded Previous Work.
- A successful Run covers pending Previous Work; another interruption replaces it with the newer pending state.
- Running Runs found after process restart permanently transition to interrupted and are never resumed at a Tool Call, Approval, Iteration or call-stack location.

## Final Conclusion

The production architecture now has one Agent execution core: the standalone Reactor. Default input calls it directly; explicit `/plan` adds only an outer Plan Controller and reaches the same Reactor through `ReActTaskExecutor`. Session persistence, context projection, tools, safety, events, TUI and Provider adapters are connected to this unified runtime, and no second production Agent loop remains.
