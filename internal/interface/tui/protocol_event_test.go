package tui

import "github.com/Godric-W/Amadeus/internal/agent/protocol"

func testProtocolEvent(threadID, turnID string, message protocol.EventMsg) protocol.Event {
	return protocol.Event{
		ID:  "test-submission",
		Msg: protocol.ScopeEventMsg(message, protocol.ThreadID(threadID), protocol.TurnID(turnID)),
	}
}
