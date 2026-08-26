package rollout

import (
	"encoding/json"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func ScopeItem(item RolloutItem, threadID protocol.ThreadID, turnID protocol.TurnID) RolloutItem {
	switch value := item.(type) {
	case SessionMetaItem:
		value.ID = threadID
		return value
	case ResponseItem:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case CompactedItem:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case TurnContextItem:
		value.ThreadID, value.TurnID = threadID, turnID
		return value
	case EventMsgItem:
		value.Msg = protocol.ScopeEventMsg(value.Msg, threadID, turnID)
		return value
	default:
		return item
	}
}

func ThreadIDOf(item RolloutItem) protocol.ThreadID {
	switch value := item.(type) {
	case SessionMetaItem:
		return value.ID
	case ResponseItem:
		return value.ThreadID
	case CompactedItem:
		return value.ThreadID
	case TurnContextItem:
		return value.ThreadID
	case EventMsgItem:
		return protocol.ThreadIDOf(value.Msg)
	default:
		return protocol.ThreadID{}
	}
}

func TurnIDOf(item RolloutItem) protocol.TurnID {
	switch value := item.(type) {
	case ResponseItem:
		return value.TurnID
	case CompactedItem:
		return value.TurnID
	case TurnContextItem:
		return value.TurnID
	case EventMsgItem:
		return protocol.TurnIDOf(value.Msg)
	default:
		return ""
	}
}

func CloneItem(item RolloutItem) RolloutItem {
	switch value := item.(type) {
	case SessionMetaItem:
		return value
	case ResponseItem:
		value.Arguments = append(json.RawMessage(nil), value.Arguments...)
		value.Metadata = cloneMap(value.Metadata)
		value.Parts = append([]tool.ContentPart(nil), value.Parts...)
		if value.Result != nil {
			result := value.Result.Clone()
			value.Result = &result
		}
		if value.Error != nil {
			errorValue := *value.Error
			value.Error = &errorValue
		}
		return value
	case CompactedItem:
		value.ReplacementHistory = cloneLLMResponseItems(value.ReplacementHistory)
		return value
	case TurnContextItem:
		value.OutputSchema = append(json.RawMessage(nil), value.OutputSchema...)
		return value
	case EventMsgItem:
		encoded, err := protocol.EncodeEventMsg(value.Msg)
		if err != nil {
			return value
		}
		cloned, err := protocol.DecodeEventMsg(encoded)
		if err != nil {
			return value
		}
		return EventMsgItem{Msg: cloned}
	default:
		return item
	}
}

func cloneLLMResponseItems(items []llm.ResponseItem) []llm.ResponseItem {
	cloned := make([]llm.ResponseItem, len(items))
	for index, item := range items {
		cloned[index] = item
		cloned[index].Parts = append([]llm.ContentPart(nil), item.Parts...)
		cloned[index].ToolCalls = append([]llm.ToolCall(nil), item.ToolCalls...)
		for callIndex := range cloned[index].ToolCalls {
			cloned[index].ToolCalls[callIndex].Arguments = append(json.RawMessage(nil), item.ToolCalls[callIndex].Arguments...)
		}
	}
	return cloned
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	encoded, err := json.Marshal(source)
	if err != nil {
		return nil
	}
	var result map[string]any
	if json.Unmarshal(encoded, &result) != nil {
		return nil
	}
	return result
}
