package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/llm"
)

func TestInlineRendererKeepsTextAndStatusBlocksSeparate(t *testing.T) {
	var text, status bytes.Buffer
	renderer, err := NewInlineRenderer(&text, &status)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := renderer.Publish(ctx, protocol.SessionEvent{ThreadID: "thread-1", TurnID: "turn-1", Message: protocol.AssistantMessageDelta{ItemID: "assistant-1", Delta: "hello"}}); err != nil {
		t.Fatal(err)
	}
	started := toolStartedMessage("read", "read", "read", "Read file", "")
	if err := renderer.Publish(ctx, protocol.SessionEvent{ThreadID: "thread-1", TurnID: "turn-1", Message: started}); err != nil {
		t.Fatal(err)
	}
	if text.String() != "hello\n" || !strings.Contains(status.String(), "tool started: read") {
		t.Fatalf("unexpected inline output: text=%q status=%q", text.String(), status.String())
	}
}

func TestInlineRendererRendersPlanAndUsage(t *testing.T) {
	var text, status bytes.Buffer
	renderer, err := NewInlineRenderer(&text, &status)
	if err != nil {
		t.Fatal(err)
	}
	started := toolStartedMessage("write-1", "write", "write", "Write file", "")
	for _, event := range []protocol.SessionEvent{
		{ThreadID: "thread-1", TurnID: "turn-1", Message: protocol.PlanUpdated{Revision: 1, Items: []protocol.PlanItem{{Step: "Read source", Status: "pending"}}}},
		{ThreadID: "thread-1", TurnID: "turn-1", Message: started},
		{ThreadID: "thread-1", TurnID: "turn-1", Message: toolCompletedMessage(started, protocol.ItemStatusCompleted, "done", "0s", false)},
		{ThreadID: "thread-1", TurnID: "turn-1", Message: protocol.ThreadTokenUsageUpdated{Usage: llm.Usage{InputTokens: 3, OutputTokens: 5}}},
		{ThreadID: "thread-1", TurnID: "turn-1", Message: protocol.TurnCompleted{Status: "completed", FinishedAt: time.Now().UTC()}},
	} {
		if err := renderer.Publish(context.Background(), event); err != nil {
			t.Fatalf("publish %T: %v", event.Message, err)
		}
	}
	output := status.String()
	for _, fragment := range []string{"plan 1:", "tool started: write", "usage: input=3 output=5 total=8", "status: phase=idle"} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("inline transcript omitted %q: %s", fragment, output)
		}
	}
}
