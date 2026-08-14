package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/policy"
	processdomain "github.com/Godric-W/Amadeus/internal/process"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
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
		ProcessManager: manager,
	})
	if err != nil {
		t.Fatal(err)
	}
	write, err := NewWriteStdin(WriteStdinOptions{Manager: manager, DefaultYield: time.Millisecond, MaxYield: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tool.WithInvocationMetadata(context.Background(), tool.InvocationMetadata{TurnID: "run-owner", Source: tool.ToolCallSourceModel})
	started, err := executePreparedTool(t, ctx, execute, json.RawMessage(`{"command":"read line; printf 'got:%s\\n' \"$line\"","yield_time_ms":1}`))
	if err != nil || started.Metadata["status"] != string(processdomain.StateRunning) {
		t.Fatalf("start interactive process: result=%#v err=%v", started, err)
	}
	processID, _ := started.Metadata["process_id"].(string)
	originCallID, _ := started.Metadata["origin_call_id"].(string)
	result, err := executePreparedTool(t, ctx, write, json.RawMessage(`{"process_id":"`+processID+`","origin_call_id":"`+originCallID+`","chars":"hello","enter":true,"yield_time_ms":1000}`))
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
		ProcessManager: manager,
	})
	if err != nil {
		t.Fatal(err)
	}
	write, err := NewWriteStdin(WriteStdinOptions{Manager: manager, DefaultYield: time.Millisecond, MaxYield: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	owner := tool.WithInvocationMetadata(context.Background(), tool.InvocationMetadata{TurnID: "run-owner", Source: tool.ToolCallSourceModel})
	started, err := executePreparedTool(t, owner, execute, json.RawMessage(`{"command":"sleep 5","yield_time_ms":1}`))
	if err != nil {
		t.Fatal(err)
	}
	processID, _ := started.Metadata["process_id"].(string)
	originCallID, _ := started.Metadata["origin_call_id"].(string)
	other := tool.WithInvocationMetadata(context.Background(), tool.InvocationMetadata{TurnID: "run-other", Source: tool.ToolCallSourceModel})
	if _, err := executePreparedTool(t, other, write, json.RawMessage(`{"process_id":"`+processID+`","origin_call_id":"`+originCallID+`","yield_time_ms":1}`)); !errors.Is(err, processdomain.ErrOwnerMismatch) {
		t.Fatalf("unexpected owner mismatch: %v", err)
	}
}

func TestWriteStdinRejectsWrongOriginCallID(t *testing.T) {
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
		ProcessManager: manager,
	})
	if err != nil {
		t.Fatal(err)
	}
	write, err := NewWriteStdin(WriteStdinOptions{Manager: manager, DefaultYield: time.Millisecond, MaxYield: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tool.WithInvocationMetadata(context.Background(), tool.InvocationMetadata{TurnID: "run-owner", Source: tool.ToolCallSourceModel})
	started, err := executePreparedTool(t, ctx, execute, json.RawMessage(`{"command":"sleep 5","yield_time_ms":1}`))
	if err != nil {
		t.Fatal(err)
	}
	processID, _ := started.Metadata["process_id"].(string)
	if _, err := executePreparedTool(t, ctx, write, json.RawMessage(`{"process_id":"`+processID+`","origin_call_id":"wrong","yield_time_ms":1}`)); err == nil || !strings.Contains(err.Error(), "origin_call_id") {
		t.Fatalf("unexpected origin mismatch: %v", err)
	}
}

func TestWriteStdinReusesOriginalCommandApproval(t *testing.T) {
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
		ProcessManager: manager,
	})
	if err != nil {
		t.Fatal(err)
	}
	write, err := NewWriteStdin(WriteStdinOptions{Manager: manager, DefaultYield: time.Millisecond, MaxYield: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	port := &testApprovalPort{decision: policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "test"}}
	coordinator, err := policy.NewApprovalCoordinator(port)
	if err != nil {
		t.Fatal(err)
	}
	ctx := withTestApprovalCoordinator(context.Background(), coordinator)
	ctx = tool.WithInvocationMetadata(ctx, tool.InvocationMetadata{TurnID: "run-owner", Source: tool.ToolCallSourceModel})
	started, err := executePreparedTool(t, ctx, execute, json.RawMessage(`{"command":"read line; printf '%s' \"$line\"","yield_time_ms":1}`))
	if err != nil {
		t.Fatal(err)
	}
	processID, _ := started.Metadata["process_id"].(string)
	originCallID, _ := started.Metadata["origin_call_id"].(string)
	if _, err := executePreparedTool(t, ctx, write, json.RawMessage(`{"process_id":"`+processID+`","origin_call_id":"`+originCallID+`","chars":"ok","enter":true,"yield_time_ms":1000}`)); err != nil {
		t.Fatal(err)
	}
	if port.calls != 1 {
		t.Fatalf("write_stdin requested a second approval: %d", port.calls)
	}
}
