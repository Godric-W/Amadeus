package contextmanager

import (
	"errors"
	"fmt"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type recordState struct {
	projection       RolloutMessageProjection
	worldState       WorldStateSnapshot
	worldStateKind   PreviousSectionKind
	referenceContext *rollout.TurnContextItem
	lastSequence     uint64
	tokenInfo        *protocol.TokenUsageInfo
	activeTokens     int64
	activeEstimated  bool
	tokenSequence    uint64
}

func newRecordState() recordState {
	return recordState{worldState: make(WorldStateSnapshot), worldStateKind: PreviousSectionAbsent}
}

func (manager *Manager) recordState() recordState {
	return recordState{
		projection: RolloutMessageProjection{Messages: cloneResponseItems(manager.items), SourceSequences: append([]int64(nil), manager.sourceSequences...), Origins: append([]MessageOrigin(nil), manager.origins...)},
		worldState: manager.worldState.Clone(), worldStateKind: manager.worldStateKind, lastSequence: manager.lastSequence,
		referenceContext: cloneTurnContextItem(manager.referenceContext),
		tokenInfo:        cloneTokenUsageInfo(manager.tokenInfo), activeTokens: manager.activeTokens,
		activeEstimated: manager.activeEstimated, tokenSequence: manager.tokenSequence,
	}
}

func (state *recordState) record(sequence uint64, item rollout.RolloutItem) error {
	if sequence != state.lastSequence+1 {
		return fmt.Errorf("context rollout sequence is %d, expected %d", sequence, state.lastSequence+1)
	}
	if item == nil {
		return errors.New("context rollout item is nil")
	}
	if err := item.Validate(); err != nil {
		return err
	}
	if response, ok := item.(rollout.ResponseItem); ok && response.Type == rollout.ResponseContextMessage && response.ContextKind == rollout.ContextKindWorldState {
		state.worldStateKind = PreviousSectionUnknown
	}
	if err := state.projection.record(sequence, item); err != nil {
		return err
	}
	if compacted, ok := item.(rollout.CompactedItem); ok && compacted.ResetWorldState {
		state.worldState = make(WorldStateSnapshot)
		state.worldStateKind = PreviousSectionAbsent
	}
	if compacted, ok := item.(rollout.CompactedItem); ok && compacted.ResetTurnContext {
		state.referenceContext = nil
	}
	if turnContext, ok := item.(rollout.TurnContextItem); ok {
		state.referenceContext = cloneTurnContextItem(&turnContext)
	}
	if worldState, ok := item.(rollout.WorldStateItem); ok {
		updated, err := ApplyWorldStatePatch(state.worldState, worldState.Full, worldState.Sections)
		if err != nil {
			return err
		}
		state.worldState, state.worldStateKind = updated, PreviousSectionKnown
	}
	if event, ok := item.(rollout.EventMsgItem); ok {
		switch update := event.Msg.(type) {
		case protocol.TokenCountEvent:
			state.tokenInfo = cloneTokenUsageInfo(update.Info)
			state.activeTokens = update.ActiveContextTokens
			state.activeEstimated = update.ActiveContextEstimated
			state.tokenSequence = update.ObservedThroughSequence
		}
	}
	state.lastSequence = sequence
	return nil
}

func (manager *Manager) applyRecordState(state recordState) {
	manager.items = state.projection.Messages
	manager.sourceSequences = state.projection.SourceSequences
	manager.origins = state.projection.Origins
	manager.worldState = state.worldState.Clone()
	manager.worldStateKind = state.worldStateKind
	manager.referenceContext = cloneTurnContextItem(state.referenceContext)
	manager.lastSequence = state.lastSequence
	manager.tokenInfo = cloneTokenUsageInfo(state.tokenInfo)
	manager.activeTokens = state.activeTokens
	manager.activeEstimated = state.activeEstimated
	manager.tokenSequence = state.tokenSequence
}

func cloneTurnContextItem(item *rollout.TurnContextItem) *rollout.TurnContextItem {
	if item == nil {
		return nil
	}
	cloned := *item
	cloned.ReasoningEffort = llm.CloneReasoningEffort(item.ReasoningEffort)
	cloned.OutputSchema = append([]byte(nil), item.OutputSchema...)
	return &cloned
}

func cloneResponseItems(items []llm.ResponseItem) []llm.ResponseItem {
	cloned := make([]llm.ResponseItem, len(items))
	for index, item := range items {
		cloned[index] = item
		cloned[index].Parts = append([]llm.ContentPart(nil), item.Parts...)
		if item.ToolCalls == nil {
			cloned[index].ToolCalls = nil
			continue
		}
		cloned[index].ToolCalls = make([]llm.ToolCall, len(item.ToolCalls))
		for callIndex, call := range item.ToolCalls {
			cloned[index].ToolCalls[callIndex] = call
			cloned[index].ToolCalls[callIndex].Arguments = append([]byte(nil), call.Arguments...)
		}
	}
	return cloned
}
