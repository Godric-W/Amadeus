## `edit`

Performs exact string replacements in an existing file.

Usage:
- You must use `read` at least once before editing. The tool records the file snapshot and rejects stale or unread files.
- Preserve the exact indentation shown after the line-number prefix. Never include line-number prefixes in `old_string` or `new_string`.
- Prefer the smallest `old_string` that is clearly unique. The edit fails when `old_string` is not unique unless `replace_all` is explicitly enabled.
- Use `replace_all` for an intentional rename or replacement of every matching occurrence.
- Prefer editing existing files. Do not use `edit` to create a new file or target a directory.
- Only use emojis when the user explicitly requests them.
