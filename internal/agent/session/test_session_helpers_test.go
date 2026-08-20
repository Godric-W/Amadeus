package session

import (
	"time"

	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

func newTestSession(history []rollout.Line, manager *agentcontext.Manager) *Session {
	_ = history
	return &Session{
		threadID: "test-thread",
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
