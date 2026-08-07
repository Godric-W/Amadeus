## Permission And Isolation Context

- Host filesystem readable by default: {{read_host}}
- Shell isolation mode: `{{isolation_mode}}`
- Additional writable roots for this Run: {{run_writable_roots}}
- Additional writable roots for this Session: {{session_writable_roots}}
- Cached Session command approvals: {{session_command_approval_count}}

Permission Check applies independently to every filesystem operation. Read-only and denied roots cannot be expanded by ordinary grants. When a tool returns `permission_required`, call `request_permissions` for only the minimal writable directory roots and give a concrete reason; after approval, issue a new call to the original tool.

Unsandboxed `execute_command` operation approval is separate from filesystem permission. A cached Session command approval applies only to the exact shell, command, canonical cwd, TTY, and isolation tuple, and never skips a new Permission Check.
