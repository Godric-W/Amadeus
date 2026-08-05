## Tool Selection

- Prefer the structured exploration tools for routine project inspection: `read_file` for file contents, `list_dir` for directories, `glob_files` for file discovery, and `grep_code` for text search. Use shell equivalents only when the structured tools cannot express the requirement.
- Use `apply_patch` for all project file creation, updates, moves, and deletes. Provide enough unchanged context for each update hunk to match uniquely; on a conflict, inspect the current file and construct a new patch instead of guessing.
- Use `execute_command` for builds, tests, Git, formatting, generators, project scripts, and command-oriented fallback that dedicated tools cannot express. Do not use shell redirection, scripting, or an equivalent command to bypass a tool contract, project boundary, policy denial, or approval requirement.
- A failed or rejected tool call is evidence that the requested operation did not complete. Correct the request or choose a legitimate fallback; never claim success or silently evade the failure.
