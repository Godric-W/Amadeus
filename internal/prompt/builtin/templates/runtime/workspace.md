## Workspace Context

- Current working directory: `{{cwd}}`
- Workspace roots: {{workspace_roots}}
- Platform temporary writable roots: {{temporary_roots}}
- Read-only roots: {{read_only_roots}}
- Denied roots: {{denied_roots}}

Resolve relative paths against the current working directory. Workspace roots are project scopes; temporary and dynamically granted writable roots do not automatically gain project instruction, Skill, or MCP semantics.
