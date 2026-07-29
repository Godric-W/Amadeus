package render

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/Godric-W/Amadeus/internal/agent/event"
)

type PlainRenderer struct {
	stdout    io.Writer
	stderr    io.Writer
	mutex     sync.Mutex
	openTurns map[string]bool
}

func NewPlainRenderer(stdout, stderr io.Writer) (*PlainRenderer, error) {
	if stdout == nil {
		return nil, errors.New("plain renderer stdout is nil")
	}
	if stderr == nil {
		return nil, errors.New("plain renderer stderr is nil")
	}
	return &PlainRenderer{
		stdout:    stdout,
		stderr:    stderr,
		openTurns: make(map[string]bool),
	}, nil
}

func (renderer *PlainRenderer) Publish(ctx context.Context, runtimeEvent event.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if runtimeEvent == nil {
		return event.ErrNilEvent
	}

	renderer.mutex.Lock()
	defer renderer.mutex.Unlock()

	switch typedEvent := runtimeEvent.(type) {
	case event.TextDelta:
		if typedEvent.Delta == "" {
			return nil
		}
		written, err := io.WriteString(renderer.stdout, typedEvent.Delta)
		if written > 0 {
			renderer.openTurns[typedEvent.TurnID] = true
		}
		if err != nil {
			return fmt.Errorf("write plain text delta: %w", err)
		}
	case event.TurnCompleted:
		if !renderer.openTurns[typedEvent.TurnID] {
			return nil
		}
		if _, err := io.WriteString(renderer.stdout, "\n"); err != nil {
			return fmt.Errorf("write plain completion newline: %w", err)
		}
		delete(renderer.openTurns, typedEvent.TurnID)
	case event.ErrorOccurred:
		return renderer.writeError(typedEvent)
	}
	return nil
}

func (renderer *PlainRenderer) writeError(errorEvent event.ErrorOccurred) error {
	var outputErr error
	if renderer.openTurns[errorEvent.TurnID] {
		if _, err := io.WriteString(renderer.stdout, "\n"); err != nil {
			outputErr = fmt.Errorf("write plain error newline: %w", err)
		}
		delete(renderer.openTurns, errorEvent.TurnID)
	}
	message := strings.Join(strings.Fields(errorEvent.Error.Message), " ")
	if message == "" {
		message = "request failed"
	}
	if _, err := fmt.Fprintf(renderer.stderr, "error: %s\n", message); err != nil {
		outputErr = errors.Join(outputErr, fmt.Errorf("write plain error: %w", err))
	}
	return outputErr
}

var _ event.Sink = (*PlainRenderer)(nil)
