package builtin

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	processdomain "github.com/Godric-W/Amadeus/internal/process"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type WriteStdinOptions struct {
	Manager      *processdomain.Manager
	DefaultYield time.Duration
	MaxYield     time.Duration
}

type WriteStdin struct{ options WriteStdinOptions }

type writeStdinArguments struct {
	ProcessID    string `json:"process_id"`
	OriginCallID string `json:"origin_call_id"`
	Chars        string `json:"chars,omitempty"`
	Enter        bool   `json:"enter,omitempty"`
	EOF          bool   `json:"eof,omitempty"`
	YieldTimeMS  int64  `json:"yield_time_ms,omitempty"`
}

func NewWriteStdin(options WriteStdinOptions) (*WriteStdin, error) {
	if options.Manager == nil {
		return nil, errors.New("write_stdin process manager is nil")
	}
	if options.DefaultYield <= 0 {
		options.DefaultYield = 5 * time.Second
	}
	if options.MaxYield <= 0 {
		options.MaxYield = 30 * time.Second
	}
	if options.DefaultYield > options.MaxYield {
		return nil, errors.New("write_stdin yield limits are invalid")
	}
	return &WriteStdin{options: options}, nil
}

func (writeStdin *WriteStdin) Spec() tool.ToolSpec { return writeStdinSpec() }

func (writeStdin *WriteStdin) SupportsParallelToolCalls() bool { return true }

func (writeStdin *WriteStdin) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	var arguments writeStdinArguments
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
		return err
	}
	if strings.TrimSpace(arguments.ProcessID) == "" {
		return errors.New("write_stdin process_id is empty")
	}
	if strings.TrimSpace(arguments.OriginCallID) == "" {
		return errors.New("write_stdin origin_call_id is empty")
	}
	if arguments.YieldTimeMS < 0 {
		return errors.New("write_stdin yield time cannot be negative")
	}
	return nil
}

func (writeStdin *WriteStdin) Prepare(toolContext tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	var arguments writeStdinArguments
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
		return tool.PreparedToolUse{}, err
	}
	chars := arguments.Chars
	if arguments.Enter {
		chars += "\n"
	}
	owner := strings.TrimSpace(invocation.TurnID)
	if owner == "" {
		owner = "standalone"
	}
	yield := durationFromMilliseconds(arguments.YieldTimeMS, writeStdin.options.DefaultYield, writeStdin.options.MaxYield)
	snapshot, err := writeStdin.options.Manager.SnapshotContext(toolContext.Context, processdomain.ID(arguments.ProcessID), owner, 0)
	if err != nil {
		return tool.PreparedToolUse{}, err
	}
	if snapshot.OriginCallID != arguments.OriginCallID {
		return tool.PreparedToolUse{}, errors.New("write_stdin origin_call_id does not match process")
	}
	state := preparedWriteStdin{arguments: arguments, owner: owner, chars: chars, yield: yield}
	return tool.PreparedToolUse{Invocation: invocation, Input: arguments, State: state, Permission: tool.AllowPermission()}, nil
}

type preparedWriteStdin struct {
	arguments    writeStdinArguments
	owner, chars string
	yield        time.Duration
}

func (writeStdin *WriteStdin) Execute(toolContext tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	state, ok := prepared.State.(preparedWriteStdin)
	if !ok {
		return tool.ToolResult{}, errors.New("write_stdin preparation state is invalid")
	}
	snapshot, err := writeStdin.options.Manager.WriteContext(toolContext.Context, processdomain.ID(state.arguments.ProcessID), state.owner, state.chars, state.arguments.EOF, state.yield)
	if err != nil {
		return tool.ToolResult{}, err
	}
	return commandSnapshotResult("write_stdin", "", snapshot, time.Since(snapshot.StartedAt))
}

func writeStdinSpec() tool.ToolSpec {
	return tool.ToolSpec{
		Name: "write_stdin", Description: "Continue or poll a running process created by execute_command in the current Run; optionally write characters, Enter, or EOF.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"process_id":{"type":"string","minLength":1},"origin_call_id":{"type":"string","minLength":1},"chars":{"type":"string"},"enter":{"type":"boolean"},"eof":{"type":"boolean"},"yield_time_ms":{"type":"integer","minimum":0}},"required":["process_id","origin_call_id"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectExecute, Idempotent: false,
	}
}

var _ tool.ToolDefinition = (*WriteStdin)(nil)
