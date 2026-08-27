package builtin

import (
	"encoding/json"

	"github.com/Godric-W/Amadeus/internal/tool"
)

const executeCommandDescription = `Runs a shell command in the requested working directory, returning output or a process ID for ongoing interaction.

Use execute_command for builds, tests, Git, formatting, project scripts, persistent processes, and command-oriented work that dedicated tools cannot express. Set cwd when the command belongs in another allowed directory. Commands are assessed and shown for Approval when required; do not use shell syntax to evade that contract. Respect timeout, yield, cancellation, output limits, and the actual exit result. Use write_stdin only with a running process returned by this tool.`

func executeCommandSpec() tool.ToolSpec {
	return tool.ToolSpec{
		Name: "execute_command", Description: executeCommandDescription,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","minLength":1,"description":"Shell command to execute."},"description":{"type":"string","description":"Clear concise active-voice description shown in Approval UI."},"cwd":{"type":"string","description":"Working directory. Defaults to the current turn workspace and must be allowed by filesystem policy."},"timeout_ms":{"type":"integer","minimum":1,"description":"Maximum command runtime in milliseconds, capped by runtime policy."},"yield_time_ms":{"type":"integer","minimum":0,"description":"Time to wait before returning output or a process ID. Defaults to 10000 ms and is capped by runtime policy."},"max_output_tokens":{"type":"integer","minimum":1,"description":"Model-visible output token budget, capped by runtime policy."},"tty":{"type":"boolean","description":"Allocate a PTY when true; use plain pipes when false or omitted."}},"required":["command"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectExecute,
	}
}
