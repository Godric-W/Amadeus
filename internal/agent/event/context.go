package event

import "context"

type contextKey string

const metadataContextKey contextKey = "amadeus.agent.event_metadata"

func WithMetadata(ctx context.Context, metadata Metadata) context.Context {
	current := MetadataFromContext(ctx)
	if metadata.SessionID == "" {
		metadata.SessionID = current.SessionID
	}
	if metadata.RunID == "" {
		metadata.RunID = current.RunID
	}
	if metadata.TaskID == "" {
		metadata.TaskID = current.TaskID
	}
	if metadata.Iteration == 0 {
		metadata.Iteration = current.Iteration
	}
	if metadata.LLMCallID == "" {
		metadata.LLMCallID = current.LLMCallID
	}
	return context.WithValue(ctx, metadataContextKey, metadata)
}

func MetadataFromContext(ctx context.Context) Metadata {
	if ctx == nil {
		return Metadata{}
	}
	metadata, _ := ctx.Value(metadataContextKey).(Metadata)
	return metadata
}

func WithTaskID(ctx context.Context, taskID string) context.Context {
	return WithMetadata(ctx, Metadata{TaskID: taskID})
}

func TaskIDFromContext(ctx context.Context) string { return MetadataFromContext(ctx).TaskID }
