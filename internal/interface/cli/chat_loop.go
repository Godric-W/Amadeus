package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	agentruntime "github.com/Godric-W/Amadeus/internal/agent/runtime"
	"github.com/Godric-W/Amadeus/internal/llm"
)

type TurnRunner interface {
	RunCall(context.Context, agentruntime.CallInput) (llm.Response, error)
}

type TurnContextFactory func(context.Context) (context.Context, context.CancelFunc)

type ChatLoop struct {
	reader             *bufio.Reader
	runner             TurnRunner
	turnContextFactory TurnContextFactory
	nextID             uint64
}

func NewChatLoop(input io.Reader, runner TurnRunner) (*ChatLoop, error) {
	if input == nil {
		return nil, errors.New("chat input is nil")
	}
	if runner == nil {
		return nil, errors.New("chat turn runner is nil")
	}
	return &ChatLoop{
		reader:             bufio.NewReader(input),
		runner:             runner,
		turnContextFactory: context.WithCancel,
	}, nil
}

func (loop *ChatLoop) WithTurnContextFactory(factory TurnContextFactory) *ChatLoop {
	if factory != nil {
		loop.turnContextFactory = factory
	}
	return loop
}

func (loop *ChatLoop) Run(ctx context.Context) error {
	for {
		line, readErr := loop.reader.ReadString('\n')
		if len(line) > 0 {
			exit, err := loop.runLine(ctx, trimLineEnding(line))
			if err != nil {
				return err
			}
			if exit {
				return nil
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return fmt.Errorf("read chat input: %w", readErr)
		}
	}
}

func (loop *ChatLoop) runLine(ctx context.Context, line string) (bool, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false, nil
	}
	if trimmed == "/exit" {
		return true, nil
	}

	loop.nextID++
	callCtx, stopTurn := loop.turnContextFactory(ctx)
	_, err := loop.runner.RunCall(callCtx, agentruntime.CallInput{
		ID:      fmt.Sprintf("turn-%d", loop.nextID),
		Content: line,
	})
	stopTurn()
	if err == nil {
		return false, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	var providerError *llm.ProviderError
	if errors.As(err, &providerError) {
		return false, nil
	}
	if errors.Is(err, context.Canceled) {
		return false, nil
	}
	return false, err
}

func trimLineEnding(line string) string {
	line = strings.TrimSuffix(line, "\n")
	return strings.TrimSuffix(line, "\r")
}

var _ TurnRunner = (*agentruntime.ChatSession)(nil)
