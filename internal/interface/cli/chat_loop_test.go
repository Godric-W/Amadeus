package cli

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	agentruntime "github.com/Godric-W/Amadeus/internal/agent/runtime"
	"github.com/Godric-W/Amadeus/internal/llm"
)

type fakeTurnRunner struct {
	mutex  sync.Mutex
	inputs []agentruntime.TurnInput
	errors []error
}

func (runner *fakeTurnRunner) RunTurn(_ context.Context, input agentruntime.TurnInput) (llm.Response, error) {
	runner.mutex.Lock()
	defer runner.mutex.Unlock()
	runner.inputs = append(runner.inputs, input)
	index := len(runner.inputs) - 1
	if index < len(runner.errors) {
		return llm.Response{}, runner.errors[index]
	}
	return llm.Response{Message: llm.AssistantMessage("answer")}, nil
}

func (runner *fakeTurnRunner) snapshotInputs() []agentruntime.TurnInput {
	runner.mutex.Lock()
	defer runner.mutex.Unlock()
	return append([]agentruntime.TurnInput(nil), runner.inputs...)
}

type cancellableTurnRunner struct {
	started chan struct{}
	inputs  []agentruntime.TurnInput
}

func (runner *cancellableTurnRunner) RunTurn(ctx context.Context, input agentruntime.TurnInput) (llm.Response, error) {
	runner.inputs = append(runner.inputs, input)
	if len(runner.inputs) == 1 {
		close(runner.started)
		<-ctx.Done()
		return llm.Response{}, ctx.Err()
	}
	return llm.Response{Message: llm.AssistantMessage("continued")}, nil
}

type errorReader struct {
	err error
}

func (reader errorReader) Read([]byte) (int, error) {
	return 0, reader.err
}

func TestChatLoopProcessesInputUntilExit(t *testing.T) {
	runner := &fakeTurnRunner{}
	loop, err := NewChatLoop(strings.NewReader("first\n\n second \n/exit\nignored\n"), runner)
	if err != nil {
		t.Fatalf("create chat loop: %v", err)
	}
	if err := loop.Run(context.Background()); err != nil {
		t.Fatalf("run chat loop: %v", err)
	}
	if len(runner.inputs) != 2 {
		t.Fatalf("unexpected turn count: %#v", runner.inputs)
	}
	if runner.inputs[0] != (agentruntime.TurnInput{ID: "turn-1", Content: "first"}) {
		t.Fatalf("unexpected first turn: %#v", runner.inputs[0])
	}
	if runner.inputs[1] != (agentruntime.TurnInput{ID: "turn-2", Content: " second "}) {
		t.Fatalf("unexpected second turn: %#v", runner.inputs[1])
	}
}

func TestChatLoopProcessesFinalLineBeforeEOF(t *testing.T) {
	runner := &fakeTurnRunner{}
	loop, err := NewChatLoop(strings.NewReader("final input"), runner)
	if err != nil {
		t.Fatalf("create chat loop: %v", err)
	}
	if err := loop.Run(context.Background()); err != nil {
		t.Fatalf("run chat loop: %v", err)
	}
	if len(runner.inputs) != 1 || runner.inputs[0].Content != "final input" {
		t.Fatalf("final EOF line was not processed: %#v", runner.inputs)
	}
}

func TestChatLoopContinuesAfterProviderError(t *testing.T) {
	runner := &fakeTurnRunner{errors: []error{
		&llm.ProviderError{Kind: llm.ProviderErrorRateLimit, Message: "slow down"},
		nil,
	}}
	loop, err := NewChatLoop(strings.NewReader("first\nsecond\n"), runner)
	if err != nil {
		t.Fatalf("create chat loop: %v", err)
	}
	if err := loop.Run(context.Background()); err != nil {
		t.Fatalf("provider error terminated chat loop: %v", err)
	}
	if len(runner.inputs) != 2 {
		t.Fatalf("chat loop did not continue: %#v", runner.inputs)
	}
}

func TestChatLoopContinuesAfterCurrentTurnCancellation(t *testing.T) {
	runner := &cancellableTurnRunner{started: make(chan struct{})}
	var cancelCurrent context.CancelFunc
	loop, err := NewChatLoop(strings.NewReader("first\nsecond\n/exit\n"), runner)
	if err != nil {
		t.Fatalf("create chat loop: %v", err)
	}
	loop.WithTurnContextFactory(func(parent context.Context) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(parent)
		cancelCurrent = cancel
		return ctx, cancel
	})
	done := make(chan error, 1)
	go func() {
		done <- loop.Run(context.Background())
	}()
	<-runner.started
	cancelCurrent()
	if err := <-done; err != nil {
		t.Fatalf("cancelled turn terminated chat loop: %v", err)
	}
	if len(runner.inputs) != 2 || runner.inputs[0].Content != "first" || runner.inputs[1].Content != "second" {
		t.Fatalf("chat loop did not continue after cancellation: %#v", runner.inputs)
	}
}

func TestChatLoopStopsWhenParentContextIsCancelled(t *testing.T) {
	runner := &cancellableTurnRunner{started: make(chan struct{})}
	loop, err := NewChatLoop(strings.NewReader("first\nsecond\n"), runner)
	if err != nil {
		t.Fatalf("create chat loop: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- loop.Run(ctx)
	}()
	<-runner.started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected parent cancellation error: %v", err)
	}
	if len(runner.inputs) != 1 {
		t.Fatalf("parent cancellation allowed another turn: %#v", runner.inputs)
	}
}

func TestChatLoopReturnsNonProviderAndReaderErrors(t *testing.T) {
	runErr := errors.New("runner failed")
	runner := &fakeTurnRunner{errors: []error{runErr}}
	loop, err := NewChatLoop(strings.NewReader("input\n"), runner)
	if err != nil {
		t.Fatalf("create chat loop: %v", err)
	}
	if err := loop.Run(context.Background()); !errors.Is(err, runErr) {
		t.Fatalf("unexpected runner error: %v", err)
	}

	readErr := errors.New("read failed")
	loop, err = NewChatLoop(errorReader{err: readErr}, &fakeTurnRunner{})
	if err != nil {
		t.Fatalf("create reader error loop: %v", err)
	}
	if err := loop.Run(context.Background()); !errors.Is(err, readErr) {
		t.Fatalf("unexpected reader error: %v", err)
	}
}

func TestChatLoopHandlesImmediateEOFAndInvalidDependencies(t *testing.T) {
	runner := &fakeTurnRunner{}
	loop, err := NewChatLoop(strings.NewReader(""), runner)
	if err != nil {
		t.Fatalf("create chat loop: %v", err)
	}
	if err := loop.Run(context.Background()); err != nil {
		t.Fatalf("run immediate EOF loop: %v", err)
	}
	if len(runner.inputs) != 0 {
		t.Fatalf("immediate EOF created turns: %#v", runner.inputs)
	}
	if _, err := NewChatLoop(nil, runner); err == nil {
		t.Fatal("expected nil input error")
	}
	if _, err := NewChatLoop(strings.NewReader(""), nil); err == nil {
		t.Fatal("expected nil runner error")
	}
}

var _ TurnRunner = (*fakeTurnRunner)(nil)
var _ io.Reader = errorReader{}
