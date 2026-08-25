package llm

import (
	"encoding/json"
	"strings"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleDeveloper Role = "developer"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ResponseItem struct {
	Role       Role          `json:"role"`
	Content    string        `json:"content,omitempty"`
	Parts      []ContentPart `json:"parts,omitempty"`
	Reasoning  string        `json:"reasoning,omitempty"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
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
	Detail    string      `json:"detail,omitempty"`
}

func TextPart(text string) ContentPart { return ContentPart{Kind: ContentText, Text: text} }

func ImagePart(mediaType, base64Data string) ContentPart {
	return ContentPart{Kind: ContentImage, MediaType: strings.TrimSpace(mediaType), Data: strings.TrimSpace(base64Data)}
}

func ImagePartWithDetail(mediaType, base64Data, detail string) ContentPart {
	part := ImagePart(mediaType, base64Data)
	part.Detail = strings.TrimSpace(detail)
	return part
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func SystemMessage(content string) ResponseItem {
	return ResponseItem{Role: RoleSystem, Content: content}
}

func DeveloperMessage(content string) ResponseItem {
	return ResponseItem{Role: RoleDeveloper, Content: content}
}

func UserMessage(content string) ResponseItem {
	return ResponseItem{Role: RoleUser, Content: content}
}

func AssistantMessage(content string) ResponseItem {
	return ResponseItem{Role: RoleAssistant, Content: content}
}

func AssistantToolCallMessage(content string, calls ...ToolCall) ResponseItem {
	return ResponseItem{Role: RoleAssistant, Content: content, ToolCalls: cloneToolCalls(calls)}
}

func ToolResultMessage(callID, content string) ResponseItem {
	return ResponseItem{Role: RoleTool, ToolCallID: callID, Content: content}
}

func ToolResultMessageWithParts(callID, content string, parts ...ContentPart) ResponseItem {
	return ResponseItem{Role: RoleTool, ToolCallID: callID, Content: content, Parts: cloneContentParts(parts)}
}

func cloneToolCalls(calls []ToolCall) []ToolCall {
	cloned := make([]ToolCall, len(calls))
	for index, call := range calls {
		cloned[index] = call
		cloned[index].Arguments = append(json.RawMessage(nil), call.Arguments...)
	}
	return cloned
}

func cloneContentParts(parts []ContentPart) []ContentPart {
	return append([]ContentPart(nil), parts...)
}

func (role Role) Valid() bool {
	switch role {
	case RoleSystem, RoleDeveloper, RoleUser, RoleAssistant, RoleTool:
		return true
	default:
		return false
	}
}
