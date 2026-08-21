package tui

import (
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

type CollabAgentHistoryCell struct {
	items map[string]protocol.TurnItem
	order []string
}

func newCollabAgentHistoryCell() *CollabAgentHistoryCell {
	return &CollabAgentHistoryCell{items: make(map[string]protocol.TurnItem)}
}

func (cell *CollabAgentHistoryCell) Apply(message protocol.EventMsg) bool {
	var item protocol.TurnItem
	switch value := message.(type) {
	case protocol.ItemStartedEvent:
		item = value.Item
	case protocol.ItemCompletedEvent:
		item = value.Item
	default:
		return false
	}
	if item.Kind != protocol.ItemCollabAgentToolCall || item.CollabAgent == nil {
		return false
	}
	key := string(item.ID)
	if _, exists := cell.items[key]; !exists {
		cell.order = append(cell.order, key)
	}
	cell.items[key] = item
	return true
}

func (cell *CollabAgentHistoryCell) IsComplete() bool {
	if cell == nil || len(cell.items) == 0 {
		return false
	}
	for _, item := range cell.items {
		if item.Status == protocol.ItemInProgress {
			return false
		}
	}
	return true
}

func (cell *CollabAgentHistoryCell) Complete() HistoryCell { return cell }
func (*CollabAgentHistoryCell) IsStreamContinuation() bool { return false }

func (cell *CollabAgentHistoryCell) RawLines() []string {
	lines := make([]string, 0, len(cell.order)*3)
	for _, key := range cell.order {
		lines = append(lines, collabAgentLines(cell.items[key])...)
	}
	return lines
}

func (cell *CollabAgentHistoryCell) DisplayLines(HistoryRenderContext) []styledLine {
	lines := make([]styledLine, 0, len(cell.order)*3)
	for _, key := range cell.order {
		item := cell.items[key]
		style := styleBold
		if item.Status == protocol.ItemFailed || item.Status == protocol.ItemDeclined {
			style = styleFailure
		}
		for index, text := range collabAgentLines(item) {
			prefix := "  "
			lineStyle := styleDim
			if index == 0 {
				prefix, lineStyle = "• ", style
			}
			lines = append(lines, styledLine{{Text: prefix, Style: styleDim}, {Text: text, Style: lineStyle}})
		}
	}
	return lines
}

func collabAgentLines(item protocol.TurnItem) []string {
	lines := []string{collabAgentTitle(item)}
	collaboration := item.CollabAgent
	if collaboration == nil {
		return lines
	}
	if collaboration.Prompt != "" && (collaboration.Tool == protocol.CollabAgentSpawnAgent || collaboration.Tool == protocol.CollabAgentSendInput) {
		lines = append(lines, "↳ "+boundCollabText(collaboration.Prompt, 240))
	}
	if item.Status == protocol.ItemInProgress || collaboration.Tool != protocol.CollabAgentWait {
		return lines
	}
	for _, agent := range collaboration.ReceiverAgents {
		state, exists := collaboration.AgentsStates[agent.ThreadID]
		if !exists {
			continue
		}
		name := collabAgentName(agent)
		detail := name + ": " + string(state.Status.Kind)
		if state.Status.Message != "" {
			detail += " — " + boundCollabText(state.Status.Message, 240)
		}
		lines = append(lines, detail)
	}
	return lines
}

func collabAgentTitle(item protocol.TurnItem) string {
	collaboration := item.CollabAgent
	if collaboration == nil {
		return "Agent operation"
	}
	if item.Status == protocol.ItemFailed || item.Status == protocol.ItemDeclined || collaboration.Status == protocol.CollabAgentToolFailed {
		if collaboration.Tool == protocol.CollabAgentSpawnAgent {
			return "Agent spawn failed"
		}
		return fmt.Sprintf("%s failed", collaboration.Tool)
	}
	if item.Status == protocol.ItemInProgress {
		switch collaboration.Tool {
		case protocol.CollabAgentSpawnAgent:
			return "Spawning agent"
		case protocol.CollabAgentSendInput:
			return "Sending input to " + firstCollabAgentName(collaboration)
		case protocol.CollabAgentWait:
			if len(collaboration.ReceiverAgents) == 1 {
				return "Waiting for " + firstCollabAgentName(collaboration)
			}
			return fmt.Sprintf("Waiting for %d agents", len(collaboration.ReceiverAgents))
		case protocol.CollabAgentCloseAgent:
			return "Closing " + firstCollabAgentName(collaboration)
		}
	}
	switch collaboration.Tool {
	case protocol.CollabAgentSpawnAgent:
		return "Spawned " + firstCollabAgentName(collaboration) + " [explorer]"
	case protocol.CollabAgentSendInput:
		return "Sent input to " + firstCollabAgentName(collaboration)
	case protocol.CollabAgentWait:
		return "Finished waiting"
	case protocol.CollabAgentCloseAgent:
		return "Closed " + firstCollabAgentName(collaboration)
	default:
		return "Agent operation completed"
	}
}

func firstCollabAgentName(item *protocol.CollabAgentToolCallItem) string {
	if item == nil || len(item.ReceiverAgents) == 0 {
		return "agent"
	}
	return collabAgentName(item.ReceiverAgents[0])
}

func collabAgentName(agent protocol.CollabAgentRef) string {
	if strings.TrimSpace(agent.AgentNickname) != "" {
		return agent.AgentNickname
	}
	if strings.TrimSpace(string(agent.ThreadID)) != "" {
		return string(agent.ThreadID)
	}
	return "agent"
}

func boundCollabText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit-1]) + "…"
}
