package session

import (
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/threadstore"
)

func sessionMetaItem(input threadstore.CreateInput) rollout.SessionMetaItem {
	item := rollout.SessionMetaItem{
		SessionID: input.SessionID, ID: input.ID, Source: input.Source.Clone(), CWD: input.CWD, Title: input.Title,
		ModelProvider: input.ModelProvider, Model: input.Model,
		GitSHA: input.GitSHA, GitBranch: input.GitBranch, GitOriginURL: input.GitOriginURL,
		CreatedAt: input.CreatedAt.UTC(),
	}
	if input.Source.IsSubAgent() {
		parent := input.Source.SubAgent.ParentThreadID
		item.ParentThreadID = &parent
	}
	return item
}

func turnContextItem(value TurnContext) rollout.TurnContextItem {
	return rollout.TurnContextItem{
		ThreadID: value.ThreadID, TurnID: value.TurnID,
		Provider: value.Provider, Model: value.Model, ReasoningEffort: llm.CloneReasoningEffort(value.ReasoningEffort), CWD: value.CWD, Shell: value.Shell,
		CurrentDate: value.CurrentDate, Timezone: value.Timezone, Mode: string(value.Mode), Personality: string(value.Personality),
		OutputSchema: append([]byte(nil), value.OutputSchema...), OutputSchemaStrict: value.OutputSchemaStrict,
	}
}
