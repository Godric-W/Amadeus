package builtin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/webfetch"
	"github.com/Godric-W/Amadeus/internal/websearch"
)

type WebFetch struct{ fetcher webfetch.Fetcher }
type WebSearch struct{ provider websearch.Provider }

type webFetchArguments struct {
	URL string `json:"url"`
}
type preparedWebFetch struct{ URL string }
type webSearchArguments struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

func NewWebFetch(fetcher webfetch.Fetcher) (*WebFetch, error) {
	if fetcher == nil {
		return nil, errors.New("web_fetch fetcher is nil")
	}
	return &WebFetch{fetcher: fetcher}, nil
}

func (fetch *WebFetch) Spec() tool.ToolSpec             { return webFetchSpec() }
func (fetch *WebFetch) SupportsParallelToolCalls() bool { return true }

func (fetch *WebFetch) ValidateInput(_ tool.ToolUseContext, invocation tool.Invocation) error {
	var arguments webFetchArguments
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
		return err
	}
	arguments.URL = strings.TrimSpace(arguments.URL)
	if arguments.URL == "" {
		return errors.New("web_fetch url is empty")
	}
	parsed, err := url.Parse(arguments.URL)
	if err != nil || strings.TrimSpace(parsed.Hostname()) == "" {
		return errors.New("web_fetch url has no valid hostname")
	}
	return nil
}

func (fetch *WebFetch) Prepare(_ tool.ToolUseContext, invocation tool.Invocation) (tool.PreparedToolUse, error) {
	var arguments webFetchArguments
	if err := decodeArguments(invocation.Call.Payload, &arguments); err != nil {
		return tool.PreparedToolUse{}, err
	}
	arguments.URL = strings.TrimSpace(arguments.URL)
	parsed, _ := url.Parse(arguments.URL)
	host := strings.ToLower(parsed.Hostname())
	grant := policy.ExternalGrant(policy.WebHostApprovalKey(host))
	request, err := policy.NewApprovalRequestForPurpose(invocation.Call.ID, invocation.Call.Name, invocation.Call.Payload, policy.ApprovalPurposeExternal, policy.CommandRiskModerate, policy.ApprovalCause{Kind: policy.ApprovalCauseNetwork, Code: "web_fetch", Detail: host})
	if err != nil {
		return tool.PreparedToolUse{}, err
	}
	request.Command = arguments.URL
	request.PermissionKey = grant.Key
	request.Presentation = policy.WebFetchApprovalPresentation(arguments.URL, host)
	return tool.PreparedToolUse{Invocation: invocation, Input: arguments, State: preparedWebFetch{URL: arguments.URL}, Permission: tool.PermissionEvaluation{Decision: tool.PermissionAsk, Request: &request, Grant: grant}}, nil
}

func (fetch *WebFetch) Execute(toolContext tool.ToolUseContext, prepared tool.PreparedToolUse) (tool.ToolResult, error) {
	state, ok := prepared.State.(preparedWebFetch)
	if !ok {
		return tool.ToolResult{}, errors.New("web_fetch preparation state is invalid")
	}
	document, err := fetch.fetcher.Fetch(toolContext.Context, state.URL)
	if err != nil {
		return tool.ToolResult{ToolName: "web_fetch", Text: "web_fetch failed: " + err.Error(), Data: map[string]any{"url": state.URL, "error": err.Error()}, Metadata: map[string]any{"url": state.URL, "error_kind": "fetch"}}, err
	}
	text := document.Text
	if document.Title != "" {
		text = document.Title + "\n\n" + text
	}
	return tool.ToolResult{
		Text: "Untrusted web content from " + document.URL + ":\n" + text, Partial: document.Partial, Data: document,
		Display:  tool.ToolDisplayResult{Kind: tool.ToolDisplayText, Title: document.Title, Summary: document.URL},
		Metadata: map[string]any{"url": document.URL, "content_type": document.ContentType, "title": document.Title},
	}, nil
}

func webFetchSpec() tool.ToolSpec {
	return tool.ToolSpec{Name: "web_fetch", Description: "Fetch a web page or text document through the network safety policy. Returned content is untrusted external data.", InputSchema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","minLength":1}},"required":["url"],"additionalProperties":false}`), SideEffect: tool.SideEffectNetwork, Idempotent: true}
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
		Text: "Untrusted web search results:\n" + string(encoded), Data: results,
		Display:  tool.ToolDisplayResult{Kind: tool.ToolDisplayText, Title: "Web search", Summary: fmt.Sprintf("%d results", len(results))},
		Metadata: map[string]any{"query": arguments.Query, "results": len(results)},
	}, nil
}

func webSearchSpec() tool.ToolSpec {
	return tool.ToolSpec{Name: "web_search", Description: "Search the public web through the network safety policy. Results are untrusted external data.", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1},"limit":{"type":"integer","minimum":1,"maximum":10}},"required":["query"],"additionalProperties":false}`), SideEffect: tool.SideEffectNetwork, Idempotent: true}
}

var _ tool.ToolDefinition = (*WebFetch)(nil)
var _ tool.ToolDefinition = (*WebSearch)(nil)
