package render

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/llm"
)

func TestAgentRendererRendersCompleteAgentEventSequence(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	renderer, err := NewAgentRenderer(&stdout, &stderr)
	if err != nil {
		t.Fatalf("create Agent renderer: %v", err)
	}
	events := []event.Event{
		event.TurnStarted{TurnID: "run-1", TaskID: "task-1"},
		event.LLMCallStarted{LLMCallID: "turn-1"},
		event.ReasoningDelta{LLMCallID: "turn-1", Delta: "hidden chain of thought"},
		event.TextDelta{LLMCallID: "turn-1", Delta: "working"},
		event.ToolCallStarted{TurnID: "run-1", CallID: "call-1", ToolName: "read_file"},
		event.ApprovalRequested{RequestID: "call-2", ToolName: "write_file", Risk: "high", Reason: "tool writes files"},
		event.ApprovalResolved{RequestID: "call-2", ToolName: "write_file", Outcome: "allow", Scope: "once", Source: "user", Reason: "approved once"},
		event.ToolCallCompleted{TurnID: "run-1", CallID: "call-1", ToolName: "read_file", Success: true, Duration: 12 * time.Millisecond, Summary: "read 10 lines"},
		event.UsageUpdated{LLMCallID: "turn-1", Usage: llm.Usage{InputTokens: 10, CachedInputTokens: 2, OutputTokens: 4, ReasoningTokens: 1, TotalTokens: 14}},
		event.StatusChanged{Entity: "task", EntityID: "task-1", From: "ready", To: "running"},
		event.TurnStatusChanged{Entity: "run", EntityID: "run-1", From: "running", To: "verifying"},
		event.DiagnosticPublished{Severity: "warn", Code: "partial", Message: "output was truncated"},
		event.ErrorOccurred{LLMCallID: "turn-1", Error: event.ErrorInfo{Message: "provider\nfailed"}},
		event.TurnCompleted{TurnID: "run-1", Status: "cancelled", StopReason: "context_cancelled", Reason: "user interrupted"},
		event.LLMCallCompleted{LLMCallID: "turn-1"},
	}
	for _, runtimeEvent := range events {
		if err := renderer.Publish(context.Background(), runtimeEvent); err != nil {
			t.Fatalf("render %T: %v", runtimeEvent, err)
		}
	}
	if stdout.String() != "working\n" {
		t.Fatalf("unexpected Agent stdout: %q", stdout.String())
	}
	output := stderr.String()
	for _, fragment := range []string{
		"run: started run-1 (task=task-1)",
		"tool: read_file (call-1) started",
		"approval: requested for write_file",
		"approval: allow for write_file",
		"tool: read_file (call-1) completed in 12ms: read 10 lines",
		"usage: input=10 cached=2 output=4 reasoning=1 total=14",
		"status: task task-1 ready -> running",
		"diagnostic[warn/partial]: output was truncated",
		"error: provider failed",
		"run: cancelled (stop=context_cancelled): user interrupted",
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("Agent stderr missing %q: %s", fragment, output)
		}
	}
	if strings.Contains(output, "hidden chain of thought") {
		t.Fatalf("reasoning was rendered: %s", output)
	}
}

func TestAgentRendererRendersSuccessPartialAndFallbacks(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	renderer, err := NewAgentRenderer(&stdout, &stderr)
	if err != nil {
		t.Fatalf("create Agent renderer: %v", err)
	}
	for _, runtimeEvent := range []event.Event{
		event.ToolCallCompleted{CallID: "call", ToolName: "execute_command", Success: false, Partial: true, Summary: "exit 1"},
		event.ErrorOccurred{},
	} {
		if err := renderer.Publish(context.Background(), runtimeEvent); err != nil {
			t.Fatalf("render fallback event: %v", err)
		}
	}
	output := stderr.String()
	if !strings.Contains(output, "failed partial") || !strings.Contains(output, "error: request failed") {
		t.Fatalf("unexpected fallback rendering: %s", output)
	}
}

func TestAgentRendererKeepsDiffStateOutOfPlainTranscript(t *testing.T) {
	var stdout, stderr bytes.Buffer
	renderer, err := NewAgentRenderer(&stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	for _, runtimeEvent := range []event.Event{
		event.RunDiffUpdated{Revision: 1, Changes: []event.RunDiffChange{{Path: "/work/a.go", Kind: "updated"}}},
		event.RunDiffInvalidated{Revision: 2, Reason: "malformed Patch delta"},
	} {
		if err := renderer.Publish(context.Background(), runtimeEvent); err != nil {
			t.Fatalf("publish %T: %v", runtimeEvent, err)
		}
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("Diff state leaked into plain transcript: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestAgentRendererSanitizesAndBoundsStatusText(t *testing.T) {
	var stderr bytes.Buffer
	renderer, err := NewAgentRenderer(&bytes.Buffer{}, &stderr)
	if err != nil {
		t.Fatalf("create Agent renderer: %v", err)
	}
	message := "line one\n\x1b[31mline two " + strings.Repeat("x", maxAgentEventTextRunes+100)
	if err := renderer.Publish(context.Background(), event.DiagnosticPublished{Severity: "warn", Code: "unsafe", Message: message}); err != nil {
		t.Fatalf("render unsafe diagnostic: %v", err)
	}
	output := stderr.String()
	if strings.Contains(output, "\x1b") || strings.Count(output, "\n") != 1 || !strings.Contains(output, "line one line two") || !strings.Contains(output, "…") {
		t.Fatalf("unsafe diagnostic was not sanitized: %q", output)
	}
}

func TestAgentRendererPropagatesContextAndWriterErrors(t *testing.T) {
	expected := errors.New("write failed")
	renderer, err := NewAgentRenderer(failingWriter{err: expected}, failingWriter{err: expected})
	if err != nil {
		t.Fatalf("create failing Agent renderer: %v", err)
	}
	if err := renderer.Publish(context.Background(), event.TextDelta{LLMCallID: "turn", Delta: "x"}); !errors.Is(err, expected) {
		t.Fatalf("unexpected text writer error: %v", err)
	}
	if err := renderer.Publish(context.Background(), event.ToolCallStarted{CallID: "call", ToolName: "tool"}); !errors.Is(err, expected) {
		t.Fatalf("unexpected status writer error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := renderer.Publish(ctx, event.LLMCallStarted{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected canceled render error: %v", err)
	}
	if err := renderer.Publish(nil, event.LLMCallStarted{}); err == nil {
		t.Fatal("nil context did not fail")
	}
	if err := renderer.Publish(context.Background(), nil); !errors.Is(err, event.ErrNilEvent) {
		t.Fatalf("unexpected nil event error: %v", err)
	}
	var nilRenderer *AgentRenderer
	if err := nilRenderer.Publish(context.Background(), event.LLMCallStarted{}); err == nil {
		t.Fatal("nil renderer did not fail")
	}
	if renderer, err := NewAgentRenderer(nil, &bytes.Buffer{}); err == nil || renderer != nil {
		t.Fatalf("unexpected nil stdout result: renderer=%#v err=%v", renderer, err)
	}
	if renderer, err := NewAgentRenderer(&bytes.Buffer{}, nil); err == nil || renderer != nil {
		t.Fatalf("unexpected nil stderr result: renderer=%#v err=%v", renderer, err)
	}
}

func TestAgentRendererSerializesConcurrentEvents(t *testing.T) {
	var stderr lockedBuffer
	renderer, err := NewAgentRenderer(&bytes.Buffer{}, &stderr)
	if err != nil {
		t.Fatalf("create concurrent Agent renderer: %v", err)
	}
	const count = 64
	var waitGroup sync.WaitGroup
	for index := 0; index < count; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if err := renderer.Publish(context.Background(), event.DiagnosticPublished{Severity: "info", Code: "concurrent", Message: "one event"}); err != nil {
				t.Errorf("render concurrent event: %v", err)
			}
		}()
	}
	waitGroup.Wait()
	if lines := strings.Count(stderr.String(), "\n"); lines != count {
		t.Fatalf("concurrent events interleaved or disappeared: got %d lines, want %d", lines, count)
	}
}

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
