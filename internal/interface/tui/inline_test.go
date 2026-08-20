package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/llm"
)

func TestInlineRendererKeepsTextAndStatusBlocksSeparate(t *testing.T) {
	var text, status bytes.Buffer
	renderer, err := NewInlineRenderer(&text, &status)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := renderer.Publish(ctx, testProtocolEvent("thread-1", "turn-1", protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "hello"})); err != nil {
		t.Fatal(err)
	}
	started := toolStartedMessage("read", "read", "read", "Read file", "")
	if err := renderer.Publish(ctx, testProtocolEvent("thread-1", "turn-1", started)); err != nil {
		t.Fatal(err)
	}
	if text.String() != "hello\n" || !strings.Contains(status.String(), "Exploring") || !strings.Contains(status.String(), "Read file") || !strings.Contains(status.String(), "status: phase=working") {
		t.Fatalf("unexpected inline output: text=%q status=%q", text.String(), status.String())
	}
}

func TestDefaultTaskPhaseUsesWorking(t *testing.T) {
	if got := taskPhase(TaskSubmission{Mode: turn.ModeKindDefault}); got != "working" {
		t.Fatalf("default task phase = %q, want working", got)
	}
	if got := statusHeader("working"); got != "Working" {
		t.Fatalf("working status header = %q", got)
	}
}

func TestInlineRendererRendersPlanAndUsage(t *testing.T) {
	var text, status bytes.Buffer
	renderer, err := NewInlineRenderer(&text, &status)
	if err != nil {
		t.Fatal(err)
	}
	started := toolStartedMessage("write-1", "write", "write", "Create file", "")
	for _, event := range []protocol.Event{
		testProtocolEvent("thread-1", "turn-1", protocol.PlanUpdateEvent{UpdatePlanArgs: protocol.UpdatePlanArgs{Plan: []protocol.PlanItemArg{{Step: "Read source", Status: protocol.StepPending}}}}),
		testProtocolEvent("thread-1", "turn-1", started),
		testProtocolEvent("thread-1", "turn-1", toolCompletedMessage(started, protocol.ItemStatusCompleted, "done", "0s", false)),
		testProtocolEvent("thread-1", "turn-1", protocol.TokenCountEvent{Usage: llm.Usage{InputTokens: 3, OutputTokens: 5}}),
		testProtocolEvent("thread-1", "turn-1", protocol.TurnCompleteEvent{Status: "completed", FinishedAt: time.Now().UTC()}),
	} {
		if err := renderer.Publish(context.Background(), event); err != nil {
			t.Fatalf("publish %T: %v", event.Msg, err)
		}
	}
	output := status.String()
	for _, fragment := range []string{"Updated Plan", "□ Read source", "Created file", "usage: input=3 output=5 total=8", "status: phase=idle"} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("inline transcript omitted %q: %s", fragment, output)
		}
	}
}

func TestInlineRendererRetryDoesNotEnterErrorPhase(t *testing.T) {
	var text, status bytes.Buffer
	renderer, err := NewInlineRenderer(&text, &status)
	if err != nil {
		t.Fatal(err)
	}
	details := "idle timeout waiting for provider stream"
	if err := renderer.Publish(context.Background(), testProtocolEvent("thread-1", "turn-1", protocol.StreamErrorEvent{
		Message: "Reconnecting... 1/5", AdditionalDetails: &details, WillRetry: true,
	})); err != nil {
		t.Fatal(err)
	}
	if renderer.phase == "error" || !strings.Contains(status.String(), "Reconnecting... 1/5") || !strings.Contains(status.String(), details) {
		t.Fatalf("inline retry phase=%q status=%q", renderer.phase, status.String())
	}
}
