package mcp

import (
	"fmt"

	"github.com/Godric-W/Amadeus/internal/tool"
)

func preparedPayload[T any](prepared tool.PreparedCall, name string) (T, error) {
	value, ok := tool.PreparedPayloadAs[T](prepared)
	if !ok {
		var zero T
		return zero, fmt.Errorf("%s prepared payload has unexpected type", name)
	}
	return value, nil
}

func prepareMCPCall(call tool.Call, identity string, payload any) (tool.PreparedCall, error) {
	return tool.NewPreparedCall(call, tool.PreparedOptions{Targets: []tool.PreparedTarget{{Kind: tool.TargetMCP, Access: tool.TargetAccessNetwork, Identity: identity}}, Payload: payload})
}
