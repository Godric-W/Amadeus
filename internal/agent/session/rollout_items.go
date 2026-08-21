package session

import (
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/thread"
)

func sessionMetaItem(input thread.CreateInput) rollout.SessionMetaItem {
	return rollout.SessionMetaItem{
		ThreadID: input.ID, CWD: input.CWD, Title: input.Title,
		ModelProvider: input.ModelProvider, Model: input.Model,
		GitSHA: input.GitSHA, GitBranch: input.GitBranch, GitOriginURL: input.GitOriginURL,
		CreatedAt: input.CreatedAt.UTC(),
	}
}

func turnContextItem(value turn.TurnContext) rollout.TurnContextItem {
	return rollout.TurnContextItem{
		ThreadID: value.ThreadID, TurnID: value.TurnID,
		Provider: value.Provider, Model: value.Model, ReasoningEffort: llm.CloneReasoningEffort(value.ReasoningEffort), CWD: value.CWD, Shell: value.Shell,
		CurrentDate: value.CurrentDate, Timezone: value.Timezone, Mode: string(value.Mode), Personality: string(value.Personality),
		OutputSchema: append([]byte(nil), value.OutputSchema...), OutputSchemaStrict: value.OutputSchemaStrict,
	}
}
