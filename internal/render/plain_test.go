package render

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

func now() time.Time { return time.Now().UTC() }

func TestPlainRendererStreamsAnswerTextToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	renderer, err := NewPlainRenderer(&stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []protocol.SessionEvent{
		{ThreadID: "thread-1", TurnID: "turn-1", Message: protocol.ItemStarted{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: now()}}},
		{ThreadID: "thread-1", TurnID: "turn-1", Message: protocol.ReasoningDelta{ItemID: "reasoning-1", Delta: "hidden reasoning"}},
		{ThreadID: "thread-1", TurnID: "turn-1", Message: protocol.AssistantMessageDelta{ItemID: "assistant-1", Delta: "hel"}},
		{ThreadID: "thread-1", TurnID: "turn-1", Message: protocol.AssistantMessageDelta{ItemID: "assistant-1", Delta: "lo"}},
		{ThreadID: "thread-1", TurnID: "turn-1", Message: protocol.ItemCompleted{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemStatusCompleted, CreatedAt: now(), CompletedAt: now()}}},
	} {
		if err := renderer.Publish(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	if stdout.String() != "hello\n" {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestPlainRendererWritesErrorsToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	renderer, _ := NewPlainRenderer(&stdout, &stderr)
	started := now()
	_ = renderer.Publish(context.Background(), protocol.SessionEvent{ThreadID: "thread-1", TurnID: "turn-1", Message: protocol.ItemStarted{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: started}}})
	_ = renderer.Publish(context.Background(), protocol.SessionEvent{ThreadID: "thread-1", TurnID: "turn-1", Message: protocol.AssistantMessageDelta{ItemID: "assistant-1", Delta: "partial"}})
	if err := renderer.Publish(context.Background(), protocol.SessionEvent{ThreadID: "thread-1", TurnID: "turn-1", Message: protocol.StreamError{Error: "slow down\ntry later"}}); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "partial\n" || stderr.String() != "error: slow down try later\n" {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestPlainRendererUsesFallbackErrorMessage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	renderer, _ := NewPlainRenderer(&stdout, &stderr)
	if err := renderer.Publish(context.Background(), protocol.SessionEvent{ThreadID: "thread-1", Message: protocol.StreamError{}}); err != nil {
		t.Fatal(err)
	}
	if stderr.String() != "error: request failed\n" {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestPlainRendererPropagatesWriterErrors(t *testing.T) {
	writeErr := errors.New("write failed")
	renderer, _ := NewPlainRenderer(failingWriter{err: writeErr}, failingWriter{err: writeErr})
	if err := renderer.Publish(context.Background(), protocol.SessionEvent{ThreadID: "thread-1", Message: protocol.AssistantMessageDelta{ItemID: "assistant-1", Delta: "text"}}); !errors.Is(err, writeErr) {
		t.Fatalf("stdout error=%v", err)
	}
	if err := renderer.Publish(context.Background(), protocol.SessionEvent{ThreadID: "thread-1", Message: protocol.StreamError{Error: "failed"}}); !errors.Is(err, writeErr) {
		t.Fatalf("stderr error=%v", err)
	}
}

func TestPlainRendererRejectsInvalidDependenciesAndContext(t *testing.T) {
	var output bytes.Buffer
	if _, err := NewPlainRenderer(nil, &output); err == nil {
		t.Fatal("expected nil stdout")
	}
	if _, err := NewPlainRenderer(&output, nil); err == nil {
		t.Fatal("expected nil stderr")
	}
	renderer, err := NewPlainRenderer(&output, &output)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	valid := protocol.SessionEvent{ThreadID: "thread-1", Message: protocol.Warning{Message: "ignored"}}
	if err := renderer.Publish(ctx, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error=%v", err)
	}
	if err := renderer.Publish(nil, valid); err == nil {
		t.Fatal("expected nil context")
	}
	if err := renderer.Publish(context.Background(), protocol.SessionEvent{}); err == nil {
		t.Fatal("expected invalid event")
	}
}
