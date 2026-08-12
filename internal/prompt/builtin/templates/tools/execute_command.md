## `execute_command` And `write_stdin`

Use `execute_command` for builds, tests, Git, formatting, project scripts, persistent processes, and command-oriented work that dedicated tools cannot express. Set `cwd` when the command belongs in a directory other than the current working directory. The command is shown to the user for approval when required; do not use shell syntax to evade the approval contract.

Use `write_stdin` only with a process owned by the current Turn, including empty input when polling for more output. Respect timeouts, cancellation, output limits, and the command's actual exit result.
