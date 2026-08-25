package transcript

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

type State struct {
	ThreadID            protocol.ThreadID
	TurnID              protocol.TurnID
	Working             bool
	Items               []protocol.TurnItem
	Active              map[protocol.ItemID]protocol.TurnItem
	Plan                *protocol.PlanUpdateEvent
	TokenInfo           *protocol.TokenUsageInfo
	ActiveContextTokens int64
	Pending             *protocol.ApprovalRequestEvent
	Warning             string
	Error               string
}

func New(threadID protocol.ThreadID) *State {
	return &State{ThreadID: threadID, Active: make(map[protocol.ItemID]protocol.TurnItem)}
}

func (state *State) Apply(event protocol.Event) error {
	if state == nil {
		return errors.New("transcript state is nil")
	}
	if err := event.Validate(); err != nil {
		return err
	}
	if state.ThreadID.IsZero() {
		state.ThreadID = protocol.ThreadIDOf(event.Msg)
	}
	threadID := protocol.ThreadIDOf(event.Msg)
	if threadID != state.ThreadID {
		return fmt.Errorf("event thread ID %q does not match %q", threadID, state.ThreadID)
	}
	if turnID := protocol.TurnIDOf(event.Msg); turnID != "" {
		state.TurnID = turnID
	}
	if state.Active == nil {
		state.Active = make(map[protocol.ItemID]protocol.TurnItem)
	}
	switch message := event.Msg.(type) {
	case protocol.SessionConfiguredEvent:
	case protocol.TurnStartedEvent:
		state.Working = true
		state.Error = ""
	case protocol.TurnCompleteEvent, protocol.TurnAbortedEvent:
		state.Working = false
		state.Pending = nil
	case protocol.ItemStartedEvent:
		if err := message.Item.Validate(); err != nil {
			return err
		}
		state.Active[message.Item.ID] = message.Item
	case protocol.ItemCompletedEvent:
		if err := message.Item.Validate(); err != nil {
			return err
		}
		delete(state.Active, message.Item.ID)
		replaceItem(&state.Items, message.Item)
	case protocol.AgentMessageContentDeltaEvent:
		return state.applyDelta(message.ItemID, message.Delta, message.Reset)
	case protocol.ReasoningContentDeltaEvent:
		return state.applyDelta(message.ItemID, message.Delta, message.Reset)
	case protocol.CommandOutputDeltaEvent:
		return state.applyDelta(message.ItemID, message.Delta, false)
	case protocol.PlanDeltaEvent:
		return state.applyDelta(message.ItemID, message.Delta, false)
	case protocol.PlanUpdateEvent:
		copy := message
		state.Plan = &copy
	case protocol.TokenCountEvent:
		state.TokenInfo = cloneTokenUsageInfo(message.Info)
		state.ActiveContextTokens = message.ActiveContextTokens
	case protocol.WarningEvent:
		state.Warning = message.Message
	case protocol.StreamErrorEvent:
		if !message.WillRetry {
			state.Error = message.Message
		}
	}
	return nil
}

func cloneTokenUsageInfo(info *protocol.TokenUsageInfo) *protocol.TokenUsageInfo {
	if info == nil {
		return nil
	}
	cloned := info.Clone()
	return &cloned
}

func (state *State) applyDelta(itemID protocol.ItemID, delta string, reset bool) error {
	if strings.TrimSpace(string(itemID)) == "" {
		return errors.New("transcript delta item ID is empty")
	}
	item, ok := state.Active[itemID]
	if !ok {
		for _, completed := range state.Items {
			if completed.ID == itemID {
				return nil
			}
		}
		return fmt.Errorf("transcript delta references unknown item %q", itemID)
	}
	if reset {
		item.Text = delta
	} else {
		item.Text += delta
	}
	state.Active[itemID] = item
	return nil
}

func replaceItem(items *[]protocol.TurnItem, item protocol.TurnItem) {
	for index := range *items {
		if (*items)[index].ID == item.ID {
			(*items)[index] = item
			return
		}
	}
	*items = append(*items, item)
}
