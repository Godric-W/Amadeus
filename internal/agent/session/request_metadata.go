package session

import (
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/llm"
)

func requestMetadata(value turn.TurnContext) llm.RequestMetadata {
	return llm.RequestMetadata{
		SessionID: value.SessionID, ThreadID: value.ThreadID, TurnID: value.TurnID,
		ParentThreadID: cloneOptionalThreadID(value.ParentThreadID),
	}
}
