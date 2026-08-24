# `cmd/amadeus` responsibilities

The command package is intentionally split by application responsibility:

- `root.go`: Cobra root command wiring.
- `agent_invocation.go`: converts CLI arguments, stdin, project flags, and session flags into an Agent invocation.
- `agent_controller.go`: dispatches one-shot and interactive invocations.
- `composition.go`: builds the Thread store, Session adapters, ThreadManager, and ThreadWorkspace.
- `agent_interactive.go`: selects the plain or fullscreen interface and wires the fullscreen `InteractiveApplication` to the TUI.
- `session_lifecycle.go`: internal Session creation, continue, resume, selection, and cleanup.
- `turn_interface.go`: bridges one-shot/plain Session events and approval requests to CLI renderers.
- `interactive_request.go`: converts Approval protocol events and policy decisions at the CLI boundary.
- `agent_audit.go`: resolves and opens the Agent audit sink.
- `runtime_ids.go`: supplies runtime IDs and clock access.
- `thread_store_factory.go`: command-level Session store factory.

Fullscreen Slash Commands are dispatched by `internal/interface/tui/application_commands.go` and call the typed API owned by `internal/app/interactive_application.go`. Provider adapters, Session-owned agent services, continuation execution, Context, Tools, Approval, persistence, and policy remain under `internal/`; this directory only composes and presents those capabilities.
