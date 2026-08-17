## `write_stdin`

Continues or polls a running process created by `execute_command` in the current Run.

Usage:
- Preserve the original `process_id` and `origin_call_id`; both are required and identify the process owner.
- Use empty `chars` to poll output, `enter` to send a newline, and `eof` to close standard input.
- It reuses the originating command Approval and must not request permission again.
- Respect the process timeout, cancellation, output limit, and actual exit state. Do not assume a process succeeded until its result reports completion.
