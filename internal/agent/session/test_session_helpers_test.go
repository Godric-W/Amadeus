package session

import (
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/testutil"
)

func newTestSession(history []rollout.Line, manager *agentcontext.Manager) *Session {
	_ = history
	return &Session{
		sessionID: testutil.SessionID(1), threadID: testutil.ThreadID(1),
		state: SessionState{
			Context:       manager,
			Configuration: Configuration{Mode: "default"},
		},
		services: SessionServices{
			Clock:  func() time.Time { return time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC) },
			NextID: func(kind string) string { return kind + "-test" },
		},
	}
}

func testTurnContext(turnID protocol.TurnID) turnContextFixture {
	return turnContextFixture{SessionID: testutil.SessionID(1), ThreadID: testutil.ThreadID(1), TurnID: turnID}
}

type turnContextFixture struct {
	SessionID protocol.SessionID
	ThreadID  protocol.ThreadID
	TurnID    protocol.TurnID
}
