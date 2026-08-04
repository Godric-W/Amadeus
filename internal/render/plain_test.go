package render

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/llm"
)

type failingWriter struct {
	err error
}

func (writer failingWriter) Write([]byte) (int, error) {
	return 0, writer.err
}

func TestPlainRendererStreamsAnswerTextToStdout(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	renderer, err := NewPlainRenderer(&stdout, &stderr)
	if err != nil {
		t.Fatalf("create plain renderer: %v", err)
	}
	events := []event.Event{
		event.LLMCallStarted{LLMCallID: "turn_1"},
		event.ReasoningDelta{LLMCallID: "turn_1", Delta: "hidden reasoning"},
		event.TextDelta{LLMCallID: "turn_1", Delta: "hel"},
		event.UsageUpdated{LLMCallID: "turn_1", Usage: llm.Usage{TotalTokens: 5}},
		event.TextDelta{LLMCallID: "turn_1", Delta: "lo"},
		event.LLMCallCompleted{LLMCallID: "turn_1", FinishReason: llm.FinishReasonStop},
	}
	for _, runtimeEvent := range events {
		if err := renderer.Publish(context.Background(), runtimeEvent); err != nil {
			t.Fatalf("render event: %v", err)
		}
	}
	if stdout.String() != "hello\n" {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestPlainRendererWritesErrorsToStderr(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	renderer, err := NewPlainRenderer(&stdout, &stderr)
	if err != nil {
		t.Fatalf("create plain renderer: %v", err)
	}
	if err := renderer.Publish(context.Background(), event.TextDelta{LLMCallID: "turn_1", Delta: "partial"}); err != nil {
		t.Fatalf("render partial text: %v", err)
	}
	if err := renderer.Publish(context.Background(), event.ErrorOccurred{
		LLMCallID: "turn_1",
		Error: event.ErrorInfo{
			Kind:    llm.ProviderErrorRateLimit,
			Message: "slow down\ntry later",
		},
	}); err != nil {
		t.Fatalf("render error: %v", err)
	}
	if stdout.String() != "partial\n" {
		t.Fatalf("unexpected partial stdout: %q", stdout.String())
	}
	if stderr.String() != "error: slow down try later\n" {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestPlainRendererUsesFallbackErrorMessage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	renderer, err := NewPlainRenderer(&stdout, &stderr)
	if err != nil {
		t.Fatalf("create plain renderer: %v", err)
	}
	if err := renderer.Publish(context.Background(), event.ErrorOccurred{}); err != nil {
		t.Fatalf("render fallback error: %v", err)
	}
	if stderr.String() != "error: request failed\n" {
		t.Fatalf("unexpected fallback stderr: %q", stderr.String())
	}
}

func TestPlainRendererPropagatesWriterErrors(t *testing.T) {
	writeErr := errors.New("write failed")
	renderer, err := NewPlainRenderer(failingWriter{err: writeErr}, failingWriter{err: writeErr})
	if err != nil {
		t.Fatalf("create plain renderer: %v", err)
	}
	if err := renderer.Publish(context.Background(), event.TextDelta{LLMCallID: "turn_1", Delta: "text"}); !errors.Is(err, writeErr) {
		t.Fatalf("unexpected stdout writer error: %v", err)
	}
	if err := renderer.Publish(context.Background(), event.ErrorOccurred{LLMCallID: "turn_2", Error: event.ErrorInfo{Message: "failed"}}); !errors.Is(err, writeErr) {
		t.Fatalf("unexpected stderr writer error: %v", err)
	}
}

func TestPlainRendererRejectsInvalidDependenciesAndContext(t *testing.T) {
	var output bytes.Buffer
	if _, err := NewPlainRenderer(nil, &output); err == nil {
		t.Fatal("expected nil stdout error")
	}
	if _, err := NewPlainRenderer(&output, nil); err == nil {
		t.Fatal("expected nil stderr error")
	}
	renderer, err := NewPlainRenderer(&output, &output)
	if err != nil {
		t.Fatalf("create plain renderer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := renderer.Publish(ctx, event.TextDelta{Delta: "ignored"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected cancelled context error: %v", err)
	}
	if err := renderer.Publish(context.Background(), nil); !errors.Is(err, event.ErrNilEvent) {
		t.Fatalf("unexpected nil event error: %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("rejected events produced output: %q", output.String())
	}
}
