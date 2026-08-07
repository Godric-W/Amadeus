# `cmd/amadeus` responsibilities

The command package is intentionally split by application responsibility:

- `root.go`: Cobra root command wiring.
- `agent_invocation.go`: converts CLI arguments, stdin, project flags, and session flags into an Agent invocation.
- `agent_controller.go`: dispatches one-shot and interactive invocations.
- `agent_interactive.go`: plain and fullscreen interaction loops.
- `agent_session.go`: SessionRuntime creation, continue, resume, selection, and cleanup.
- `agent_run.go`: creates and orchestrates one canonical Run.
- `agent_request_view.go`: builds each dynamic RequestContext, Prompt, Context envelope, and RequestView.
- `agent_rollout.go`: records Tool calls, Tool outcomes, and Context compaction in canonical rollout history.
- `agent_run_result.go`: executes the Reactor and maps its result to persistent Run terminal state and CLI exit behavior.
- `agent_audit.go`: resolves and opens the Agent audit sink.
- `agent_ids.go`: supplies runtime IDs and clock access.
- `session_runtime.go`: command-level Session store factory.

Provider adapters, Reactor, ContextBuilder, SessionRuntime, Tools, and policy remain under `internal/`; this directory only coordinates those components for the CLI application.
