## `execute_command` And `write_stdin`

Use `execute_command` for builds, tests, Git, formatting, project scripts, persistent processes, and command-oriented work that dedicated tools cannot express. Set `cwd` when the command belongs in a directory other than the current working directory. Declare every known extra write location in `requested_permissions.writable_roots`; do not rely on parsing the command string to discover permissions.

Sandboxed commands are constrained by the effective filesystem profile. Unsandboxed commands require a separate operation approval unless an exact Session approval already exists. Use `write_stdin` only with a process owned by the current Run, including empty input when polling for more output. Respect timeouts, cancellation, output limits, and the command's actual exit result.
