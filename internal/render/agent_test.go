package render

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/llm"
)

func sessionMessage(message protocol.EventMessage) protocol.SessionEvent {
	return protocol.SessionEvent{ThreadID: "thread-1", TurnID: "turn-1", Message: message}
}

func toolItem(id, name string, status protocol.ItemStatus, text string) protocol.TurnItem {
	now := time.Now().UTC()
	item := protocol.TurnItem{ID: id, Kind: protocol.ItemToolCall, Status: status, CreatedAt: now, ToolName: name, CallID: id, Text: text}
	if status != protocol.ItemInProgress {
		item.CompletedAt = now
	}
	return item
}

func TestAgentRendererRendersCompleteAgentEventSequence(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	renderer, err := NewAgentRenderer(&stdout, &stderr)
	if err != nil {
		t.Fatalf("create Agent renderer: %v", err)
	}
	started := toolItem("call-1", "read", protocol.ItemInProgress, "")
	started.Payload = map[string]any{"action_summary": "Read file", "side_effect": "read"}
	completed := toolItem("call-1", "read", protocol.ItemStatusCompleted, "read 10 lines")
	completed.CreatedAt = started.CreatedAt
	completed.Payload = map[string]any{"duration": "12ms", "partial": false}
	assistant := protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: time.Now().UTC()}
	completedAssistant := assistant
	completedAssistant.Status = protocol.ItemStatusCompleted
	completedAssistant.CompletedAt = time.Now().UTC()
	events := []protocol.SessionEvent{
		sessionMessage(protocol.TurnStarted{StartedAt: time.Now().UTC(), Input: "test"}),
		sessionMessage(protocol.ItemStarted{Item: assistant}),
		sessionMessage(protocol.AssistantMessageDelta{ItemID: "assistant-1", Delta: "working"}),
		sessionMessage(protocol.ItemCompleted{Item: completedAssistant}),
		sessionMessage(protocol.ItemStarted{Item: started}),
		sessionMessage(protocol.ItemCompleted{Item: completed}),
		sessionMessage(protocol.ThreadTokenUsageUpdated{Usage: llm.Usage{InputTokens: 10, CachedInputTokens: 2, OutputTokens: 4, ReasoningTokens: 1, TotalTokens: 14}}),
		sessionMessage(protocol.Warning{Message: "output was truncated"}),
		sessionMessage(protocol.StreamError{Message: "provider\nfailed"}),
		sessionMessage(protocol.TurnAborted{Reason: "user interrupted", FinishedAt: time.Now().UTC()}),
	}
	for _, runtimeEvent := range events {
		if err := renderer.Publish(context.Background(), runtimeEvent); err != nil {
			t.Fatalf("render: %v", err)
		}
	}
	if stdout.String() != "working\n" {
		t.Fatalf("unexpected Agent stdout: %q", stdout.String())
	}
	output := stderr.String()
	for _, fragment := range []string{
		"turn: started",
		"tool: read (call-1) started",
		"tool: read (call-1) completed",
		"usage: input=10 cached=2 output=4 reasoning=1 total=14",
		"warning: output was truncated",
		"error: provider failed",
		"turn: aborted: user interrupted",
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("Agent stderr missing %q: %s", fragment, output)
		}
	}
}

func TestAgentRendererRendersSuccessPartialAndFallbacks(t *testing.T) {
	var stdout, stderr bytes.Buffer
	renderer, err := NewAgentRenderer(&stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	item := toolItem("call", "execute_command", protocol.ItemFailed, "exit 1")
	item.Payload = map[string]any{"partial": true}
	for _, runtimeEvent := range []protocol.SessionEvent{
		sessionMessage(protocol.ItemCompleted{Item: item}),
		sessionMessage(protocol.StreamError{}),
	} {
		if err := renderer.Publish(context.Background(), runtimeEvent); err != nil {
			t.Fatalf("render: %v", err)
		}
	}
	output := stderr.String()
	if !strings.Contains(output, "failed: exit 1") || !strings.Contains(output, "error: request failed") {
		t.Fatalf("unexpected fallback rendering: %s", output)
	}
}

func TestAgentRendererKeepsDiffStateOutOfPlainTranscript(t *testing.T) {
	var stdout, stderr bytes.Buffer
	renderer, err := NewAgentRenderer(&stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if err := renderer.Publish(context.Background(), sessionMessage(protocol.Warning{Message: "diff is shown during approval"})); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("unexpected diff transcript: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestAgentRendererSanitizesAndBoundsStatusText(t *testing.T) {
	var stderr bytes.Buffer
	renderer, err := NewAgentRenderer(&bytes.Buffer{}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	message := "line one\n\x1b[31mline two " + strings.Repeat("x", maxAgentEventTextRunes+100)
	if err := renderer.Publish(context.Background(), sessionMessage(protocol.Warning{Message: message})); err != nil {
		t.Fatal(err)
	}
	output := stderr.String()
	if strings.Contains(output, "\x1b") || strings.Count(output, "\n") != 1 || !strings.Contains(output, "…") {
		t.Fatalf("unsafe status output: %q", output)
	}
}

func TestAgentRendererPropagatesWriterErrorsAndContext(t *testing.T) {
	expected := errors.New("write failed")
	renderer, err := NewAgentRenderer(failingWriter{err: expected}, failingWriter{err: expected})
	if err != nil {
		t.Fatal(err)
	}
	if err := renderer.Publish(context.Background(), sessionMessage(protocol.Warning{Message: "warning"})); !errors.Is(err, expected) {
		t.Fatalf("unexpected writer error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := renderer.Publish(ctx, sessionMessage(protocol.Warning{Message: "ignored"})); !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected cancelled error: %v", err)
	}
	if err := renderer.Publish(nil, sessionMessage(protocol.Warning{Message: "ignored"})); err == nil {
		t.Fatal("expected nil context error")
	}
	if err := renderer.Publish(context.Background(), protocol.SessionEvent{}); err == nil {
		t.Fatal("expected invalid event error")
	}
}

type failingWriter struct{ err error }

func (writer failingWriter) Write([]byte) (int, error) { return 0, writer.err }

type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (buffer *lockedBuffer) Write(content []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.buffer.Write(content)
}
func (buffer *lockedBuffer) String() string {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.buffer.String()
}
