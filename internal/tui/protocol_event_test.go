package tui

import (
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

func testThreadID(index uint64) protocol.ThreadID { return testutil.ThreadID(index) }

func testProtocolEvent(threadID protocol.ThreadID, turnID string, message protocol.EventMsg) protocol.Event {
	return protocol.Event{
		ID:  "test-submission",
		Msg: protocol.ScopeEventMsg(message, threadID, protocol.TurnID(turnID)),
	}
}
