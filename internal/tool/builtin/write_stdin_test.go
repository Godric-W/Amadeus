package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	processdomain "github.com/Godric-W/Amadeus/internal/process"
	"github.com/Godric-W/Amadeus/internal/project"
)

func TestWriteStdinContinuesOwnedProcessAndPollsCompletion(t *testing.T) {
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := processdomain.NewManager()
	t.Cleanup(manager.Close)
	execute, err := NewExecuteCommand(root, ExecuteCommandOptions{
		DefaultTimeout: 5 * time.Second, MaxTimeout: 5 * time.Second,
		DefaultYield: time.Millisecond, MaxYield: time.Second,
		MaxOutputBytes: 1 << 20, MaxOutputLines: 1_000, MaxOutputTokens: 8_000,
		ProcessManager: manager, Authorizer: newTestCommandAuthorizer(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	write, err := NewWriteStdin(WriteStdinOptions{Manager: manager, DefaultYield: time.Millisecond, MaxYield: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx := event.WithMetadata(context.Background(), event.Metadata{TurnID: "run-owner"})
	started, err := executePreparedTool(t, ctx, execute, json.RawMessage(`{"command":"read line; printf 'got:%s\\n' \"$line\"","yield_time_ms":1}`))
	if err != nil || started.Metadata["status"] != string(processdomain.StateRunning) {
		t.Fatalf("start interactive process: result=%#v err=%v", started, err)
	}
	processID, _ := started.Metadata["process_id"].(string)
	result, err := executePreparedTool(t, ctx, write, json.RawMessage(`{"process_id":"`+processID+`","chars":"hello","enter":true,"yield_time_ms":1000}`))
	if err != nil {
		t.Fatalf("write process stdin: %v", err)
	}
	if result.Metadata["status"] != string(processdomain.StateCompleted) || !strings.Contains(result.Text, "got:hello") || result.Metadata["exit_code"] != 0 {
		t.Fatalf("unexpected write_stdin result: %#v", result)
	}
}

func TestWriteStdinRejectsAnotherRunOwner(t *testing.T) {
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := processdomain.NewManager()
	t.Cleanup(manager.Close)
	execute, err := NewExecuteCommand(root, ExecuteCommandOptions{
		DefaultTimeout: 5 * time.Second, MaxTimeout: 5 * time.Second,
		DefaultYield: time.Millisecond, MaxYield: time.Second,
		MaxOutputBytes: 1 << 20, MaxOutputLines: 1_000, MaxOutputTokens: 8_000,
		ProcessManager: manager, Authorizer: newTestCommandAuthorizer(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	write, err := NewWriteStdin(WriteStdinOptions{Manager: manager, DefaultYield: time.Millisecond, MaxYield: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	owner := event.WithMetadata(context.Background(), event.Metadata{TurnID: "run-owner"})
	started, err := executePreparedTool(t, owner, execute, json.RawMessage(`{"command":"sleep 5","yield_time_ms":1}`))
	if err != nil {
		t.Fatal(err)
	}
	processID, _ := started.Metadata["process_id"].(string)
	other := event.WithMetadata(context.Background(), event.Metadata{TurnID: "run-other"})
	if _, err := executePreparedTool(t, other, write, json.RawMessage(`{"process_id":"`+processID+`","yield_time_ms":1}`)); !errors.Is(err, processdomain.ErrOwnerMismatch) {
		t.Fatalf("unexpected owner mismatch: %v", err)
	}
}
