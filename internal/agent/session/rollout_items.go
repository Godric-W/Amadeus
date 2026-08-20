package session

import (
	"fmt"

	"github.com/Godric-W/Amadeus/internal/agent/plan"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
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
		Provider: value.Provider, Model: value.Model, CWD: value.CWD, Shell: value.Shell,
		CurrentDate: value.CurrentDate, Timezone: value.Timezone, Mode: string(value.Mode), Personality: string(value.Personality),
		OutputSchema: append([]byte(nil), value.OutputSchema...), OutputSchemaStrict: value.OutputSchemaStrict,
	}
}

func planUpdateEvent(snapshot plan.Snapshot) protocol.PlanUpdateEvent {
	items := make([]protocol.PlanItem, len(snapshot.Items))
	for index, item := range snapshot.Items {
		items[index] = protocol.PlanItem{Step: item.Step, Status: string(item.Status)}
	}
	return protocol.PlanUpdateEvent{
		ItemID:      protocol.ItemID(fmt.Sprintf("plan-%d", snapshot.Revision)),
		Explanation: snapshot.Explanation, Items: items, Revision: snapshot.Revision, UpdatedAt: snapshot.UpdatedAt,
	}
}

func planSnapshot(event protocol.PlanUpdateEvent) plan.Snapshot {
	items := make([]plan.Item, len(event.Items))
	for index, item := range event.Items {
		items[index] = plan.Item{Step: item.Step, Status: plan.ItemStatus(item.Status)}
	}
	return plan.Snapshot{
		Explanation: event.Explanation, Items: items, UpdatedAt: event.UpdatedAt, Revision: event.Revision,
	}
}

func latestPlanSnapshot(lines []rollout.Line) (plan.Snapshot, bool) {
	for index := len(lines) - 1; index >= 0; index-- {
		item, ok := lines[index].Item.(rollout.EventMsgItem)
		if !ok {
			continue
		}
		event, ok := item.Msg.(protocol.PlanUpdateEvent)
		if ok {
			return planSnapshot(event), true
		}
	}
	return plan.Snapshot{}, false
}
