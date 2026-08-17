## `glob`

Fast file-pattern matching for the current workspace.

Usage:
- Use patterns such as `**/*.go` or `internal/**/test_*.go` to discover files by name.
- Results are workspace-scoped, bounded by the runtime limit, and returned as file paths.
- Use `glob` for file discovery and `grep` for content search; do not substitute one for the other.
- Narrow the pattern or use `limit` when a broad pattern produces too many results.
