package tool

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type SideEffect string

const (
	SideEffectNone    SideEffect = "none"
	SideEffectRead    SideEffect = "read"
	SideEffectWrite   SideEffect = "write"
	SideEffectExecute SideEffect = "execute"
	SideEffectNetwork SideEffect = "network"
)

func (effect SideEffect) Valid() bool {
	switch effect {
	case SideEffectNone, SideEffectRead, SideEffectWrite, SideEffectExecute, SideEffectNetwork:
		return true
	default:
		return false
	}
}

type Exposure string

const (
	ExposureDirect      Exposure = "direct"
	ExposureConditional Exposure = "conditional"
	ExposureDeferred    Exposure = "deferred"
	ExposureHidden      Exposure = "hidden"
)

func (exposure Exposure) Valid() bool {
	switch exposure {
	case ExposureDirect, ExposureConditional, ExposureDeferred, ExposureHidden:
		return true
	default:
		return false
	}
}

type Registration struct {
	Exposure  Exposure
	Condition string
}

func DirectRegistration() Registration {
	return Registration{Exposure: ExposureDirect}
}

type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	SideEffect  SideEffect      `json:"side_effect"`
	Idempotent  bool            `json:"idempotent"`
}

func (spec ToolSpec) Clone() ToolSpec {
	spec.InputSchema = append(json.RawMessage(nil), spec.InputSchema...)
	return spec
}

type ToolPayload = json.RawMessage

type ToolCall struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Payload ToolPayload `json:"payload"`
}

type ToolCallSource string

const (
	ToolCallSourceModel ToolCallSource = "model"
	ToolCallSourceUser  ToolCallSource = "user"
	ToolCallSourceMCP   ToolCallSource = "mcp"
)

type Invocation struct {
	SessionID string
	TurnID    string
	Call      ToolCall
	Source    ToolCallSource
}

type InvocationMetadata struct {
	SessionID string
	TurnID    string
	Source    ToolCallSource
}

type invocationMetadataContextKey struct{}

func WithInvocationMetadata(ctx context.Context, metadata InvocationMetadata) context.Context {
	return context.WithValue(ctx, invocationMetadataContextKey{}, metadata)
}

func InvocationMetadataFromContext(ctx context.Context) InvocationMetadata {
	if ctx == nil {
		return InvocationMetadata{}
	}
	metadata, _ := ctx.Value(invocationMetadataContextKey{}).(InvocationMetadata)
	return metadata
}

type RequestSnapshot struct {
	MCPBindingRevision string
	SkillRevision      string
	ToolRevision       string
}

type requestSnapshotContextKey struct{}

func WithRequestSnapshot(ctx context.Context, snapshot RequestSnapshot) context.Context {
	return context.WithValue(ctx, requestSnapshotContextKey{}, snapshot)
}

func RequestSnapshotFromContext(ctx context.Context) (RequestSnapshot, bool) {
	if ctx == nil {
		return RequestSnapshot{}, false
	}
	snapshot, ok := ctx.Value(requestSnapshotContextKey{}).(RequestSnapshot)
	return snapshot, ok
}

func NewCall(id, name string, arguments json.RawMessage) ToolCall {
	return ToolCall{
		ID:      strings.TrimSpace(id),
		Name:    strings.TrimSpace(name),
		Payload: append(json.RawMessage(nil), arguments...),
	}
}

func (call ToolCall) Clone() ToolCall {
	call.Payload = append(json.RawMessage(nil), call.Payload...)
	return call
}

type ContentKind string

const (
	ContentText  ContentKind = "text"
	ContentImage ContentKind = "image"
)

type ContentPart struct {
	Kind      ContentKind `json:"kind"`
	Text      string      `json:"text,omitempty"`
	MediaType string      `json:"media_type,omitempty"`
	Data      string      `json:"data,omitempty"`
}

// ToolResult is the model-facing result of one prepared tool execution. Data
// carries the typed domain result while Text and Parts are the bounded model
// projection. Display is a separate UI projection and must not be reconstructed
// from Text.
type ToolResult struct {
	CallID    string            `json:"call_id"`
	ToolName  string            `json:"tool_name"`
	Text      string            `json:"text,omitempty"`
	Parts     []ContentPart     `json:"parts,omitempty"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
	Partial   bool              `json:"partial,omitempty"`
	Artifacts []ArtifactRef     `json:"artifacts,omitempty"`
	Data      any               `json:"data,omitempty"`
	Display   ToolDisplayResult `json:"display,omitempty"`
}

type ToolDisplayKind string

const (
	ToolDisplayNone       ToolDisplayKind = "none"
	ToolDisplayText       ToolDisplayKind = "text"
	ToolDisplayFileChange ToolDisplayKind = "file_change"
	ToolDisplayProcess    ToolDisplayKind = "process"
	ToolDisplayMedia      ToolDisplayKind = "media"
)

type ToolDisplayResult struct {
	Kind    ToolDisplayKind `json:"kind,omitempty"`
	Title   string          `json:"title,omitempty"`
	Summary string          `json:"summary,omitempty"`
	Data    any             `json:"data,omitempty"`
}

type TruncationReason string

const (
	TruncationNone        TruncationReason = ""
	TruncationResultLimit TruncationReason = "result_limit"
	TruncationByteLimit   TruncationReason = "byte_limit"
	TruncationTokenLimit  TruncationReason = "token_limit"
)

type BoundedResult[T any] struct {
	Items         []T              `json:"items"`
	Truncated     bool             `json:"truncated"`
	Reason        TruncationReason `json:"truncation_reason,omitempty"`
	Bytes         int              `json:"bytes"`
	TokenEstimate int              `json:"token_estimate"`
}

type ToolCallStatus string

const (
	ToolCallCompleted   ToolCallStatus = "completed"
	ToolCallFailed      ToolCallStatus = "failed"
	ToolCallDenied      ToolCallStatus = "denied"
	ToolCallInterrupted ToolCallStatus = "interrupted"
)

func (status ToolCallStatus) Valid() bool {
	switch status {
	case ToolCallCompleted, ToolCallFailed, ToolCallDenied, ToolCallInterrupted:
		return true
	default:
		return false
	}
}

type ToolError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type ArtifactRef struct {
	Path   string `json:"path,omitempty"`
	URI    string `json:"uri,omitempty"`
	Digest string `json:"digest,omitempty"`
}

type ToolCallOutcome struct {
	Status   ToolCallStatus `json:"status"`
	Error    *ToolError     `json:"error,omitempty"`
	Blocking bool           `json:"blocking,omitempty"`
	Duration time.Duration  `json:"duration"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type ToolExecution struct {
	Call    ToolCall        `json:"call"`
	Output  ToolResult      `json:"output"`
	Outcome ToolCallOutcome `json:"outcome"`
}

type ErrorKindProvider interface {
	ToolErrorKind() string
}

func (result ToolResult) Clone() ToolResult {
	result.Parts = append([]ContentPart(nil), result.Parts...)
	result.Artifacts = append([]ArtifactRef(nil), result.Artifacts...)
	if result.Metadata != nil {
		result.Metadata = cloneMetadata(result.Metadata)
	}
	return result
}

func cloneMetadata(metadata map[string]any) map[string]any {
	cloned := make(map[string]any, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
