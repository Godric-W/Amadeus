package openai

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
)

type toolCallAccumulator struct {
	order     int64
	call      llm.ToolCall
	arguments strings.Builder
}

type toolCallAggregator struct {
	byKey map[string]*toolCallAccumulator
}

func (aggregator *toolCallAggregator) add(key string, order int64, id, name, arguments string) error {
	if strings.TrimSpace(key) == "" {
		return protocolToolCallError("tool call aggregation key is empty")
	}
	if order < 0 {
		return protocolToolCallError("tool call index is negative")
	}
	if aggregator.byKey == nil {
		aggregator.byKey = make(map[string]*toolCallAccumulator)
	}
	accumulator, ok := aggregator.byKey[key]
	if !ok {
		accumulator = &toolCallAccumulator{order: order}
		aggregator.byKey[key] = accumulator
	} else if accumulator.order != order {
		return protocolToolCallError(fmt.Sprintf("tool call %q changed index from %d to %d", key, accumulator.order, order))
	}
	accumulator.call.ID = mergeToolCallFragment(accumulator.call.ID, id)
	accumulator.call.Name = mergeToolCallFragment(accumulator.call.Name, name)
	accumulator.arguments.WriteString(arguments)
	return nil
}

func (aggregator *toolCallAggregator) replaceArguments(key, name, arguments string) error {
	accumulator, ok := aggregator.byKey[key]
	if !ok {
		return protocolToolCallError(fmt.Sprintf("tool call %q completed before it was added", key))
	}
	accumulator.call.Name = mergeToolCallFragment(accumulator.call.Name, name)
	accumulator.arguments.Reset()
	accumulator.arguments.WriteString(arguments)
	return nil
}

func (aggregator *toolCallAggregator) finalize() ([]llm.ToolCall, error) {
	accumulators := make([]*toolCallAccumulator, 0, len(aggregator.byKey))
	for _, accumulator := range aggregator.byKey {
		accumulators = append(accumulators, accumulator)
	}
	sort.Slice(accumulators, func(left, right int) bool { return accumulators[left].order < accumulators[right].order })

	calls := make([]llm.ToolCall, 0, len(accumulators))
	for _, accumulator := range accumulators {
		arguments := accumulator.arguments.String()
		if strings.TrimSpace(accumulator.call.ID) == "" {
			return nil, protocolToolCallError(fmt.Sprintf("tool call at index %d has an empty call ID", accumulator.order))
		}
		if strings.TrimSpace(accumulator.call.Name) == "" {
			return nil, protocolToolCallError(fmt.Sprintf("tool call %q has an empty name", accumulator.call.ID))
		}
		if strings.TrimSpace(arguments) == "" {
			return nil, protocolToolCallError(fmt.Sprintf("tool call %q has empty arguments", accumulator.call.ID))
		}
		call := accumulator.call
		call.Arguments = json.RawMessage(arguments)
		calls = append(calls, call)
	}
	return calls, nil
}

func mergeToolCallFragment(current, fragment string) string {
	if fragment == "" || fragment == current || strings.HasSuffix(current, fragment) {
		return current
	}
	if strings.HasPrefix(fragment, current) {
		return fragment
	}
	return current + fragment
}

func protocolToolCallError(message string) error {
	return &llm.ProviderError{Kind: llm.ProviderErrorProtocol, Message: message}
}
