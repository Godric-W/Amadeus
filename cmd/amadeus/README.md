# `cmd/amadeus` responsibilities

The command package is intentionally split by application responsibility:

- `root.go`: Cobra root command wiring.
- `agent_invocation.go`: converts CLI arguments, stdin, project flags, and session flags into an Agent invocation.
- `agent_controller.go`: dispatches one-shot and interactive invocations.
- `agent_interactive.go`: plain and fullscreen interaction loops.
- `session_lifecycle.go`: internal Session creation, continue, resume, selection, and cleanup.
- `turn_interface.go`: bridges Session events and interactive requests to CLI renderers.
- `interactive_commands.go`: dispatches application-owned interactive commands without owning Agent execution.
- `agent_audit.go`: resolves and opens the Agent audit sink.
- `runtime_ids.go`: supplies runtime IDs and clock access.
- `thread_store_factory.go`: command-level Session store factory.

Provider adapters, Session-scoped CodingRuntime, TurnEngine, Context, Tools, Approval, persistence, and policy remain under `internal/`; this directory only composes and presents those capabilities.
