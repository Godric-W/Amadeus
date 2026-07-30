package tool

import (
	"encoding/json"
	"strings"
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

type ResourceMode string

const (
	ResourceModeNone      ResourceMode = "none"
	ResourceModeExclusive ResourceMode = "exclusive"
	ResourceModeArguments ResourceMode = "arguments"
)

func (mode ResourceMode) Valid() bool {
	switch mode {
	case ResourceModeNone, ResourceModeExclusive, ResourceModeArguments:
		return true
	default:
		return false
	}
}

type ResourceStrategy struct {
	Mode          ResourceMode `json:"mode"`
	ArgumentPaths []string     `json:"argument_paths,omitempty"`
}

func (strategy ResourceStrategy) Clone() ResourceStrategy {
	strategy.ArgumentPaths = append([]string(nil), strategy.ArgumentPaths...)
	return strategy
}

type Spec struct {
	Name             string           `json:"name"`
	Description      string           `json:"description"`
	InputSchema      json.RawMessage  `json:"input_schema"`
	SideEffect       SideEffect       `json:"side_effect"`
	ParallelSafe     bool             `json:"parallel_safe"`
	Idempotent       bool             `json:"idempotent"`
	ResourceStrategy ResourceStrategy `json:"resource_strategy"`
}

func (spec Spec) Clone() Spec {
	spec.InputSchema = append(json.RawMessage(nil), spec.InputSchema...)
	spec.ResourceStrategy = spec.ResourceStrategy.Clone()
	return spec
}

type Call struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func NewCall(id, name string, arguments json.RawMessage) Call {
	return Call{
		ID:        strings.TrimSpace(id),
		Name:      strings.TrimSpace(name),
		Arguments: append(json.RawMessage(nil), arguments...),
	}
}

func (call Call) Clone() Call {
	call.Arguments = append(json.RawMessage(nil), call.Arguments...)
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

type Result struct {
	CallID   string         `json:"call_id"`
	ToolName string         `json:"tool_name"`
	Text     string         `json:"text,omitempty"`
	Parts    []ContentPart  `json:"parts,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Partial  bool           `json:"partial,omitempty"`
}

func (result Result) Clone() Result {
	result.Parts = append([]ContentPart(nil), result.Parts...)
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
