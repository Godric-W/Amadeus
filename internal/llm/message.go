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

type Message struct {
	Role       Role
	Content    string
	Parts      []ContentPart
	Reasoning  string
	ToolCalls  []ToolCall
	ToolCallID string
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

func TextPart(text string) ContentPart { return ContentPart{Kind: ContentText, Text: text} }

func ImagePart(mediaType, base64Data string) ContentPart {
	return ContentPart{Kind: ContentImage, MediaType: strings.TrimSpace(mediaType), Data: strings.TrimSpace(base64Data)}
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

func SystemMessage(content string) Message {
	return Message{Role: RoleSystem, Content: content}
}

func DeveloperMessage(content string) Message {
	return Message{Role: RoleDeveloper, Content: content}
}

func UserMessage(content string) Message {
	return Message{Role: RoleUser, Content: content}
}

func AssistantMessage(content string) Message {
	return Message{Role: RoleAssistant, Content: content}
}

func AssistantToolCallMessage(content string, calls ...ToolCall) Message {
	return Message{Role: RoleAssistant, Content: content, ToolCalls: cloneToolCalls(calls)}
}

func ToolResultMessage(callID, content string) Message {
	return Message{Role: RoleTool, ToolCallID: callID, Content: content}
}

func ToolResultMessageWithParts(callID, content string, parts ...ContentPart) Message {
	return Message{Role: RoleTool, ToolCallID: callID, Content: content, Parts: cloneContentParts(parts)}
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
