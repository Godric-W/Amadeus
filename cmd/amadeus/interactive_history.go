package main

import (
	"sort"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
)

func replayTurnItems(lines []rollout.Line) ([]protocol.TurnItem, error) {
	items, err := protocol.ProjectCompletedItems(lines)
	if err != nil {
		return nil, err
	}
	legacy, err := protocol.LegacyResponseItemsToCompleted(lines)
	if err != nil {
		return nil, err
	}
	if len(legacy) == 0 {
		return items, nil
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		seen[item.ID] = struct{}{}
	}
	for _, item := range legacy {
		if _, exists := seen[item.ID]; exists {
			continue
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(left, right int) bool {
		return items[left].CompletedAt.Before(items[right].CompletedAt)
	})
	return items, nil
}
