## `grep`

A powerful regular-expression and text search tool backed by ripgrep when available.

Usage:
- Use `grep` for repository content searches instead of embedding `grep` or `rg` in `execute_command`.
- `query` is literal by default; set `regex` for regular-expression matching.
- Use `glob` or `type` to constrain files, `path` to constrain the search root, and `context` to include nearby lines.
- Use `case_sensitive` deliberately and use `limit` to bound output.
- Search output is evidence for selecting files; use `read` before editing a matched file.
- Multiline matching is not supported by this Tool contract; use `execute_command` only when the user explicitly needs a command-oriented search.
