package react

import (
	"encoding/json"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
	"strings"
	"testing"
)

func TestReplayToolResultsUsesAssistantCallOrder(t *testing.T) {
	assistant := llm.AssistantToolCallMessage("checking files",
		llm.ToolCall{ID: "call_1", Name: "read_file", Arguments: json.RawMessage(`{"path":"a"}`)},
		llm.ToolCall{ID: "call_2", Name: "read_file", Arguments: json.RawMessage(`{"path":"b"}`)},
	)
	outcomes := []ToolOutcome{
		replayExecution("call_2", "read_file", tool.Result{Text: "B"}, ""),
		replayExecution("call_1", "read_file", tool.Result{Text: "A"}, ""),
	}

	messages, err := ReplayToolResults(assistant, outcomes)
	if err != nil {
		t.Fatalf("replay tool results: %v", err)
	}
	if len(messages) != 3 || messages[0].Role != llm.RoleAssistant || messages[1].ToolCallID != "call_1" || messages[2].ToolCallID != "call_2" {
		t.Fatalf("unexpected replay order: %#v", messages)
	}
	var first ToolResultPayload
	if err := json.Unmarshal([]byte(messages[1].Content), &first); err != nil {
		t.Fatalf("decode replay payload: %v", err)
	}
	if !first.OK || first.Text != "A" {
		t.Fatalf("unexpected success payload: %#v", first)
	}
}

func TestReplayToolResultsIncludesFailureAndPartialOutput(t *testing.T) {
	assistant := llm.AssistantToolCallMessage("", llm.ToolCall{ID: "call_1", Name: "execute_command", Arguments: json.RawMessage(`{"command":"test"}`)})
	execution := replayExecution("call_1", "execute_command", tool.Result{Text: "partial", Partial: true}, "exit status 1")

	messages, err := ReplayToolResults(assistant, []ToolOutcome{execution})
	if err != nil {
		t.Fatalf("replay failed result: %v", err)
	}
	var payload ToolResultPayload
	if err := json.Unmarshal([]byte(messages[1].Content), &payload); err != nil {
		t.Fatalf("decode failed payload: %v", err)
	}
	if payload.OK || payload.Text != "partial" || !payload.Partial || payload.Error != "exit status 1" {
		t.Fatalf("unexpected failed payload: %#v", payload)
	}
}

func TestReplayToolResultsRejectsIncompleteMappings(t *testing.T) {
	assistant := llm.AssistantToolCallMessage("", llm.ToolCall{ID: "call_1", Name: "read_file", Arguments: json.RawMessage(`{}`)})
	tests := []struct {
		name     string
		outcomes []ToolOutcome
		match    string
	}{
		{name: "missing", match: "missing result"},
		{name: "duplicate", outcomes: []ToolOutcome{replayExecution("call_1", "read_file", tool.Result{}, ""), replayExecution("call_1", "read_file", tool.Result{}, "")}, match: "duplicate"},
		{name: "unknown", outcomes: []ToolOutcome{replayExecution("call_1", "read_file", tool.Result{}, ""), replayExecution("call_2", "read_file", tool.Result{}, "")}, match: "unknown call"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ReplayToolResults(assistant, test.outcomes)
			if err == nil || !strings.Contains(err.Error(), test.match) {
				t.Fatalf("unexpected replay validation error: %v", err)
			}
		})
	}
}

func replayExecution(callID, toolName string, result tool.Result, outcomeError string) ToolOutcome {
	result.CallID = callID
	result.ToolName = toolName
	outcome := ToolOutcome{CallID: callID, ToolName: toolName, Status: ToolOutcomeSucceeded, Result: result}
	if outcomeError != "" {
		outcome.Status = ToolOutcomeFailed
		outcome.Error = &ToolError{Kind: "execution_failed", Message: outcomeError}
	}
	return outcome
}
