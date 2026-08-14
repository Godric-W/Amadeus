package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/task"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type reasoningRolloutHost struct {
	items []rollout.Item
}

func (host *reasoningRolloutHost) AppendItems(_ context.Context, _ turn.ID, items ...rollout.Item) error {
	host.items = append(host.items, items...)
	return nil
}

func (*reasoningRolloutHost) History() []rollout.Line { return nil }

var _ task.Host = (*reasoningRolloutHost)(nil)

func TestTurnRolloutRecorderPreservesAssistantReasoning(t *testing.T) {
	host := &reasoningRolloutHost{}
	recorder := &turnRolloutRecorder{host: host, turnID: "turn-1"}
	message := llm.AssistantToolCallMessage("", llm.ToolCall{
		ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"README.md"}`),
	})
	message.Reasoning = "inspect the repository first"
	if err := recorder.RecordToolCalls(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(host.items) != 1 {
		t.Fatalf("recorded items = %d, want one", len(host.items))
	}
	var payload struct {
		Reasoning string `json:"reasoning_content"`
	}
	if err := json.Unmarshal(host.items[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Reasoning != message.Reasoning {
		t.Fatalf("recorded reasoning = %q, want %q", payload.Reasoning, message.Reasoning)
	}
}
