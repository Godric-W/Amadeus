package session

import (
	"github.com/Godric-W/Amadeus/internal/contextmanager"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
)

func (session *Session) Snapshot(model llm.ModelInfo, prompt llm.Prompt) contextmanager.PromptSnapshot {
	if session == nil || session.state.Context == nil {
		return contextmanager.PromptSnapshot{}
	}
	return session.state.Context.Snapshot(model, prompt)
}

func (session *Session) ContextUpdate(key contextmanager.UpdateKey) string {
	if session == nil || session.state.Context == nil {
		return ""
	}
	return session.state.Context.Update(key)
}

func (session *Session) ContextProjection() contextmanager.RolloutMessageProjection {
	if session == nil || session.state.Context == nil {
		return contextmanager.RolloutMessageProjection{}
	}
	return session.state.Context.Projection()
}

func (session *Session) RolloutItemCount() int {
	if session == nil || session.state.Context == nil {
		return 0
	}
	return session.state.Context.RolloutItemCount()
}

func (session *Session) TokenCountSnapshot() protocol.TokenCountEvent {
	if session == nil || session.state.Context == nil {
		return protocol.TokenCountEvent{}
	}
	snapshot := session.state.Context.TokenSnapshot()
	active, estimated := session.state.Context.ActiveContextTokens(session.services.ModelInfo())
	if active <= 0 {
		active = snapshot.ActiveContextTokens
		estimated = snapshot.ActiveContextEstimated
	}
	return protocol.TokenCountEvent{
		Info: cloneProtocolTokenUsageInfo(snapshot.Info), ActiveContextTokens: active,
		ActiveContextEstimated: estimated,
	}
}
