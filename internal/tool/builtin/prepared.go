package builtin

import (
	"fmt"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func preparedFilesystemTarget(resolved project.ResolvedPath) tool.PreparedTarget {
	access := tool.TargetAccessRead
	if resolved.Access == project.AccessWrite {
		access = tool.TargetAccessWrite
	}
	return tool.PreparedTarget{
		Kind: tool.TargetFilesystem, RequestedPath: resolved.Requested, CanonicalPath: resolved.Canonical,
		Access: access, MatchedRoot: resolved.MatchedRoot, RootSource: string(resolved.RootSource),
	}
}

func preparedPayload[T any](prepared tool.PreparedCall, toolName string) (T, error) {
	payload, ok := tool.PreparedPayloadAs[T](prepared)
	if !ok {
		var zero T
		return zero, fmt.Errorf("%s prepared payload has unexpected type", toolName)
	}
	return payload, nil
}
