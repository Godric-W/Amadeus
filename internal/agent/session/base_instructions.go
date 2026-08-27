package session

import (
	"errors"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

func resolveSessionBase(history threadstore.InitialHistory, configured llm.BaseInstructions, services *SessionServices, personality Personality) (llm.BaseInstructions, error) {
	if configured.Text != "" {
		if err := configured.ValidatePersisted(); err != nil {
			return llm.BaseInstructions{}, err
		}
		return configured.Clone(), nil
	}
	if history.Kind == threadstore.InitialHistoryResumed {
		meta, ok := history.Lines[0].Item.(rollout.SessionMetaItem)
		if !ok {
			return llm.BaseInstructions{}, errors.New("resumed history has no session metadata")
		}
		if err := meta.BaseInstructions.ValidatePersisted(); err != nil {
			return llm.BaseInstructions{}, err
		}
		return meta.BaseInstructions.Clone(), nil
	}
	if services == nil {
		return llm.BaseInstructions{}, errors.New("session services are unavailable")
	}
	model := services.ModelInfo()
	messages, err := services.ModelMessages(model)
	if err != nil {
		return llm.BaseInstructions{}, err
	}
	base, err := messages.ResolveBaseInstructions(string(personality), model.Name)
	if err != nil {
		return llm.BaseInstructions{}, err
	}
	if err := base.ValidatePersisted(); err != nil {
		return llm.BaseInstructions{}, err
	}
	return base, nil
}

func (session *Session) BaseInstructions() llm.BaseInstructions {
	if session == nil {
		return llm.BaseInstructions{}
	}
	return session.state.Base.Clone()
}
