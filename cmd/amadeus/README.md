# `cmd/amadeus` responsibilities

The command package is intentionally split by application responsibility:

- `root.go`: Cobra root command wiring.
- `agent_invocation.go`: converts CLI arguments, stdin, project flags, and session flags into an Agent invocation.
- `agent_controller.go`: dispatches one-shot and interactive invocations.
- `agent_interactive.go`: plain and fullscreen interaction loops.
- `session_lifecycle.go`: SessionRuntime creation, continue, resume, selection, and cleanup.
- `turn_execution.go`: creates and orchestrates one canonical Turn.
- `context_updates.go`: prepares stable dynamic Context Updates for the Session-owned ContextManager.
- `turn_rollout.go`: records Tool calls, Tool outcomes, and Context compaction in canonical rollout history.
- `turn_result.go`: executes the Reactor and maps its result to persistent Turn terminal state and CLI exit behavior.
- `agent_audit.go`: resolves and opens the Agent audit sink.
- `runtime_ids.go`: supplies runtime IDs and clock access.
- `thread_store_factory.go`: command-level Session store factory.

Provider adapters, Reactor, ContextBuilder, SessionRuntime, Tools, and policy remain under `internal/`; this directory only coordinates those components for the CLI application.
