package builtin

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/websearch"
)

type WebSearch struct {
	provider websearch.Provider
}

type webSearchArguments struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

func NewWebSearch(provider websearch.Provider) (*WebSearch, error) {
	if provider == nil {
		return nil, errors.New("web_search provider is nil")
	}
	return &WebSearch{provider: provider}, nil
}

func (search *WebSearch) Spec() tool.ToolSpec             { return webSearchSpec() }
func (search *WebSearch) SupportsParallelToolCalls() bool { return true }

func (search *WebSearch) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	var arguments webSearchArguments
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
		return err
	}
	if strings.TrimSpace(arguments.Query) == "" {
		return errors.New("web_search query is empty")
	}
	if arguments.Limit < 0 {
		return errors.New("web_search limit cannot be negative")
	}
	return nil
}

func (search *WebSearch) Prepare(_ tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	var arguments webSearchArguments
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
		return tool.PreparedToolUse{}, err
	}
	arguments.Query = strings.TrimSpace(arguments.Query)
	return tool.PreparedToolUse{Invocation: invocation, Input: arguments, State: arguments, Permission: tool.AllowPermission()}, nil
}

func (search *WebSearch) Execute(toolContext tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	arguments, ok := prepared.State.(webSearchArguments)
	if !ok {
		return tool.ToolResult{}, errors.New("web_search preparation state is invalid")
	}
	results, err := search.provider.Search(toolContext.Context, arguments.Query, arguments.Limit)
	if err != nil {
		metadata := map[string]any{"query": arguments.Query, "error": err.Error()}
		var providerErr *websearch.Error
		if errors.As(err, &providerErr) {
			metadata["error_kind"] = string(providerErr.Kind)
			metadata["provider"] = providerErr.Provider
		}
		return tool.ToolResult{ToolName: "web_search", Text: "web_search failed: " + err.Error(), Data: metadata, Metadata: metadata}, err
	}
	encoded, err := json.Marshal(results)
	if err != nil {
		return tool.ToolResult{}, fmt.Errorf("encode web search results: %w", err)
	}
	return tool.ToolResult{
		Text:     "Untrusted web search-summary evidence. Results may be sufficient for an answer; use web_fetch only when full-page verification is necessary.\n" + string(encoded),
		Data:     results,
		Display:  tool.ToolDisplayResult{Kind: tool.ToolDisplayText, Title: "Web search", Summary: fmt.Sprintf("%d results", len(results))},
		Metadata: map[string]any{"query": arguments.Query, "results": len(results), "evidence_type": "search_summary", "page_verified": false},
	}, nil
}

func webSearchSpec() tool.ToolSpec {
	return tool.ToolSpec{
		Name:        "web_search",
		Description: "Search the public web and return untrusted title, URL, and snippet evidence. Search snippets are not verified full-page content. If the evidence is sufficient, answer directly; use web_fetch only for exact pages that require full-page verification.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1},"limit":{"type":"integer","minimum":1,"maximum":10}},"required":["query"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectNetwork,
		Idempotent:  true,
	}
}

var _ tool.ToolDefinition = (*WebSearch)(nil)
