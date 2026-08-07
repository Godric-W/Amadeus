## `apply_patch`

Use `apply_patch` for deterministic file creation, updates, moves, and deletes. A patch starts with `*** Begin Patch` and ends with `*** End Patch`; each operation uses `*** Add File:`, `*** Update File:`, or `*** Delete File:`. Update hunks must include enough unchanged context to match the current file uniquely.

For an update conflict, read the current file and construct a new patch instead of retrying stale context. Treat partial results as committed only for the operations reported as applied. Paths may be cwd-relative or absolute when the Permission Profile allows them; do not use shell redirection to evade the patch or permission contract.
