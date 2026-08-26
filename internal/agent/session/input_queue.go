package session

import (
	"errors"
	"sync"
)

type TurnInput interface {
	isTurnInput()
}

type UserTurnInput struct {
	Content  string
	ClientID string
}

func (UserTurnInput) isTurnInput() {}

func (input UserTurnInput) validate() error {
	if input.Content == "" {
		return errors.New("turn user input is empty")
	}
	return nil
}

var errTurnInputQueueSealed = errors.New("turn input queue is sealed")

type TurnInputQueue struct {
	mu     sync.Mutex
	items  []TurnInput
	sealed bool
}

func (queue *TurnInputQueue) enqueue(input TurnInput) error {
	if input == nil {
		return errors.New("turn input is nil")
	}
	if userInput, ok := input.(UserTurnInput); ok {
		if err := userInput.validate(); err != nil {
			return err
		}
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.sealed {
		return errTurnInputQueueSealed
	}
	queue.items = append(queue.items, input)
	return nil
}

func (queue *TurnInputQueue) hasPending() bool {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return len(queue.items) > 0
}

func (queue *TurnInputQueue) drain() []TurnInput {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	items := append([]TurnInput(nil), queue.items...)
	queue.items = nil
	return items
}

func (queue *TurnInputQueue) sealIfEmpty() bool {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.items) > 0 {
		return false
	}
	queue.sealed = true
	return true
}

func (queue *TurnInputQueue) sealAndDrain() []TurnInput {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	queue.sealed = true
	items := append([]TurnInput(nil), queue.items...)
	queue.items = nil
	return items
}

type InputQueue struct{}

func (InputQueue) Enqueue(state *TurnState, input TurnInput) error {
	if state == nil {
		return errors.New("turn state is nil")
	}
	return state.pendingInput.enqueue(input)
}

func (InputQueue) HasPending(state *TurnState) bool {
	return state != nil && state.pendingInput.hasPending()
}

func (InputQueue) Drain(state *TurnState) []TurnInput {
	if state == nil {
		return nil
	}
	return state.pendingInput.drain()
}

func (InputQueue) SealIfEmpty(state *TurnState) bool {
	return state == nil || state.pendingInput.sealIfEmpty()
}

func (InputQueue) SealAndDrain(state *TurnState) []TurnInput {
	if state == nil {
		return nil
	}
	return state.pendingInput.sealAndDrain()
}
