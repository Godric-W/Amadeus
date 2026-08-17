## `read`

Reads a UTF-8 text file from the local workspace.

Usage:
- The `path` parameter identifies a file below the current workspace. Use the path supplied by the user or an absolute path when the tool output provides one.
- By default, the tool reads from the beginning of the file and returns bounded output with `cat -n`-style line numbers starting at 1.
- Use `line` and `limit` when you already know the relevant region or when the file is large.
- The tool reads files, not directories. Use `glob` to discover files and `execute_command` when directory metadata is required.
- Preserve the exact file content after the line-number prefix when using the result for a later `edit`.
- Output may be truncated by the runtime's byte and line limits; use a narrower range to inspect omitted content.
