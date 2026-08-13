package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
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
	ProcessID   string `json:"process_id"`
	Chars       string `json:"chars,omitempty"`
	Enter       bool   `json:"enter,omitempty"`
	EOF         bool   `json:"eof,omitempty"`
	YieldTimeMS int64  `json:"yield_time_ms,omitempty"`
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

func (writeStdin *WriteStdin) SupportsParallelToolCalls() bool { return false }

func (writeStdin *WriteStdin) Call(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	call := invocation.Call
	var arguments writeStdinArguments
	if err := decodeArguments(call.Payload, &arguments); err != nil {
		return tool.Output{}, err
	}
	if strings.TrimSpace(arguments.ProcessID) == "" {
		return tool.Output{}, errors.New("write_stdin process_id is empty")
	}
	if arguments.YieldTimeMS < 0 {
		return tool.Output{}, errors.New("write_stdin yield time cannot be negative")
	}
	chars := arguments.Chars
	if arguments.Enter {
		chars += "\n"
	}
	owner := event.MetadataFromContext(ctx).TurnID
	if owner == "" {
		owner = "standalone"
	}
	yield := durationFromMilliseconds(arguments.YieldTimeMS, writeStdin.options.DefaultYield, writeStdin.options.MaxYield)
	snapshot, err := writeStdin.options.Manager.WriteContext(ctx, processdomain.ID(arguments.ProcessID), owner, chars, arguments.EOF, yield)
	if err != nil {
		return tool.Output{}, err
	}
	return commandSnapshotResult("write_stdin", "", snapshot, time.Since(snapshot.StartedAt))
}

func writeStdinSpec() tool.ToolSpec {
	return tool.ToolSpec{
		Name: "write_stdin", Description: "Continue or poll a running process created by execute_command in the current Run; optionally write characters, Enter, or EOF.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"process_id":{"type":"string","minLength":1},"chars":{"type":"string"},"enter":{"type":"boolean"},"eof":{"type":"boolean"},"yield_time_ms":{"type":"integer","minimum":0}},"required":["process_id"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectExecute, Idempotent: false,
	}
}

var _ tool.Tool = (*WriteStdin)(nil)
