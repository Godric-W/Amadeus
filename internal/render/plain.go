package render

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
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

func (renderer *PlainRenderer) Publish(ctx context.Context, runtimeEvent protocol.SessionEvent) error {
	if renderer == nil {
		return errors.New("plain renderer is nil")
	}
	if ctx == nil {
		return errors.New("plain renderer context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := runtimeEvent.Validate(); err != nil {
		return err
	}

	renderer.mutex.Lock()
	defer renderer.mutex.Unlock()

	switch typedEvent := runtimeEvent.Message.(type) {
	case protocol.AssistantMessageDelta:
		if typedEvent.Delta == "" {
			return nil
		}
		written, err := io.WriteString(renderer.stdout, typedEvent.Delta)
		if written > 0 {
			renderer.openTurns[typedEvent.ItemID] = true
		}
		if err != nil {
			return fmt.Errorf("write plain text delta: %w", err)
		}
	case protocol.ItemCompleted:
		if typedEvent.Item.Kind != protocol.ItemAssistantMessage || !renderer.openTurns[typedEvent.Item.ID] {
			return nil
		}
		if _, err := io.WriteString(renderer.stdout, "\n"); err != nil {
			return fmt.Errorf("write plain completion newline: %w", err)
		}
		delete(renderer.openTurns, typedEvent.Item.ID)
	case protocol.StreamError:
		return renderer.writeError(typedEvent.Error)
	case protocol.TurnCompleted, protocol.TurnAborted:
		return renderer.finishOpenText()
	}
	return nil
}

func (renderer *PlainRenderer) writeError(message string) error {
	var outputErr error
	if err := renderer.finishOpenText(); err != nil {
		outputErr = err
	}
	message = strings.Join(strings.Fields(message), " ")
	if message == "" {
		message = "request failed"
	}
	if _, err := fmt.Fprintf(renderer.stderr, "error: %s\n", message); err != nil {
		outputErr = errors.Join(outputErr, fmt.Errorf("write plain error: %w", err))
	}
	return outputErr
}

func (renderer *PlainRenderer) finishOpenText() error {
	if len(renderer.openTurns) == 0 {
		return nil
	}
	if _, err := io.WriteString(renderer.stdout, "\n"); err != nil {
		return fmt.Errorf("write plain completion newline: %w", err)
	}
	clear(renderer.openTurns)
	return nil
}

var _ protocol.EventSink = (*PlainRenderer)(nil)
