package session

import "github.com/Godric-W/Amadeus/internal/protocol"

type TurnState struct {
	pendingRequests map[protocol.RequestID]interactiveWaiter
	pendingInput    TurnInputQueue
}

func newTurnState() *TurnState {
	return &TurnState{pendingRequests: make(map[protocol.RequestID]interactiveWaiter)}
}
