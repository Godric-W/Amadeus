package tool

import (
	"context"
	"errors"
	"strings"
	"time"
)

func (service *ToolExecutionService) complete(call ToolCall, output ToolResult, handleErr error, startedAt time.Time) ToolExecution {
	output.CallID = call.ID
	output.ToolName = call.Name
	status := ToolCallCompleted
	var toolError *ToolError
	blocking := false
	if handleErr != nil {
		status, blocking = statusForError(handleErr)
		kind := "execution_failed"
		var kindProvider ErrorKindProvider
		if errors.As(handleErr, &kindProvider) && strings.TrimSpace(kindProvider.ToolErrorKind()) != "" {
			kind = strings.TrimSpace(kindProvider.ToolErrorKind())
		}
		toolError = &ToolError{Kind: kind, Message: handleErr.Error()}
		if strings.TrimSpace(output.Text) == "" {
			output.Text = handleErr.Error()
		}
	}
	return ToolExecution{
		Call: call.Clone(), Output: output.Clone(),
		Outcome: ToolCallOutcome{Status: status, Error: toolError, Blocking: blocking, Duration: service.durationSince(startedAt), Metadata: cloneMetadata(output.Metadata)},
	}
}

type phaseError struct {
	kind string
	err  error
}

func (err *phaseError) Error() string         { return err.err.Error() }
func (err *phaseError) Unwrap() error         { return err.err }
func (err *phaseError) ToolErrorKind() string { return err.kind }

func (service *ToolExecutionService) failure(call ToolCall, kind string, err error, startedAt time.Time) ToolExecution {
	status, blocking := statusForError(err)
	return ToolExecution{
		Call: call.Clone(), Output: ToolResult{CallID: call.ID, ToolName: call.Name, Text: err.Error()},
		Outcome: ToolCallOutcome{Status: status, Error: &ToolError{Kind: kind, Message: err.Error()}, Blocking: blocking, Duration: service.durationSince(startedAt)},
	}
}

func (service *ToolExecutionService) durationSince(startedAt time.Time) time.Duration {
	finishedAt := service.now()
	if finishedAt.Before(startedAt) {
		return 0
	}
	return finishedAt.Sub(startedAt)
}

func statusForError(err error) (ToolCallStatus, bool) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ToolCallInterrupted, false
	}
	var kindProvider ErrorKindProvider
	if errors.As(err, &kindProvider) {
		switch strings.TrimSpace(kindProvider.ToolErrorKind()) {
		case "permission_required", "permission_denied", "path_denied", "symlink_escape", "approval_denied":
			return ToolCallDenied, false
		}
	}
	return ToolCallFailed, false
}
