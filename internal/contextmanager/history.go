package contextmanager

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

type recordState struct {
	projection      RolloutMessageProjection
	updates         map[UpdateKey]contextUpdateState
	lastSequence    uint64
	tokenInfo       *protocol.TokenUsageInfo
	activeTokens    int64
	activeEstimated bool
	tokenSequence   uint64
}

func newRecordState() recordState {
	return recordState{updates: make(map[UpdateKey]contextUpdateState)}
}

func (manager *Manager) recordState() recordState {
	updates := make(map[UpdateKey]contextUpdateState, len(manager.updates))
	for key, value := range manager.updates {
		updates[key] = value
	}
	return recordState{
		projection: RolloutMessageProjection{Messages: cloneResponseItems(manager.items), SourceSequences: append([]int64(nil), manager.sourceSequences...), Origins: append([]MessageOrigin(nil), manager.origins...)},
		updates:    updates, lastSequence: manager.lastSequence,
		tokenInfo: cloneTokenUsageInfo(manager.tokenInfo), activeTokens: manager.activeTokens,
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
	if err := state.projection.record(sequence, item); err != nil {
		return err
	}
	if event, ok := item.(rollout.EventMsgItem); ok {
		switch update := event.Msg.(type) {
		case protocol.ContextUpdateEvent:
			key := UpdateKey(strings.TrimSpace(update.Key))
			if validUpdateKey(key) {
				content := strings.TrimSpace(update.Content)
				if content == "" {
					delete(state.updates, key)
				} else {
					state.updates[key] = contextUpdateState{Content: content, Revision: strings.TrimSpace(update.Revision), Sequence: sequence}
				}
			}
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
	manager.updates = state.updates
	manager.lastSequence = state.lastSequence
	manager.tokenInfo = cloneTokenUsageInfo(state.tokenInfo)
	manager.activeTokens = state.activeTokens
	manager.activeEstimated = state.activeEstimated
	manager.tokenSequence = state.tokenSequence
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
