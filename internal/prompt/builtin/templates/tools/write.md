## `write`

Writes content to a file in the local workspace.

Usage:
- If the target is an existing file, you must use `read` first so the runtime can enforce the file-read and stale-content checks.
- Prefer `edit` for focused modifications to an existing file; use `write` for new files or complete rewrites.
- This tool overwrites the target after Approval when the target already exists.
- The path must identify a file, not a directory. Include a filename and an appropriate extension.
- Do not create unrelated documentation or README files unless the user explicitly requests them.
- Only use emojis when the user explicitly requests them.
