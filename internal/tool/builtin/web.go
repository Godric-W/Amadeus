package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/tool"
	"github.com/Godric-W/Amadeus/internal/webfetch"
	"github.com/Godric-W/Amadeus/internal/websearch"
)

type WebFetch struct {
	fetcher   webfetch.Fetcher
	approvals policy.ApprovalHandler
	rules     *policy.SessionRuleStore
	events    event.Sink
}
type WebSearch struct{ provider websearch.Provider }
type webFetchArguments struct {
	URL string `json:"url"`
}
type webSearchArguments struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

func NewWebFetch(fetcher webfetch.Fetcher) (*WebFetch, error) {
	return NewWebFetchWithApproval(fetcher, WebApprovalOptions{})
}

type WebApprovalOptions struct {
	Approvals policy.ApprovalHandler
	Rules     *policy.SessionRuleStore
	Events    event.Sink
}

func NewWebFetchWithApproval(fetcher webfetch.Fetcher, options WebApprovalOptions) (*WebFetch, error) {
	if fetcher == nil {
		return nil, errors.New("web_fetch fetcher is nil")
	}
	if options.Rules == nil {
		options.Rules = policy.NewSessionRuleStore()
	}
	return &WebFetch{fetcher: fetcher, approvals: options.Approvals, rules: options.Rules, events: options.Events}, nil
}
func (fetch *WebFetch) Spec() tool.Spec                 { return webFetchSpec() }
func (fetch *WebFetch) SupportsParallelToolCalls() bool { return true }
func (fetch *WebFetch) Handle(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	call := invocation.Call
	var arguments webFetchArguments
	if err := decodeArguments(call.Payload, &arguments); err != nil {
		return tool.Output{}, err
	}
	if strings.TrimSpace(arguments.URL) == "" {
		return tool.Output{}, errors.New("web_fetch url is empty")
	}
	parsed, err := url.Parse(arguments.URL)
	if err != nil || strings.TrimSpace(parsed.Hostname()) == "" {
		return tool.Output{}, errors.New("web_fetch url has no valid hostname")
	}
	host := strings.ToLower(parsed.Hostname())
	key := policy.WebHostApprovalKey(host)
	if !fetch.rules.Allows(key) && fetch.approvals != nil {
		request, requestErr := policy.NewApprovalRequestForPurpose(invocation.Call.ID, invocation.Call.Name, invocation.Call.Payload, policy.ApprovalPurposeExternal, policy.CommandRiskModerate, "fetching external web content requires network approval")
		if requestErr != nil {
			return tool.Output{}, requestErr
		}
		request.Command = arguments.URL
		request.Presentation = policy.ExternalApprovalPresentation("Fetch", "Do you want to proceed?", "Yes, and don't ask again for "+host, arguments.URL)
		if fetch.events != nil {
			if publishErr := fetch.events.Publish(ctx, event.ApprovalRequested{RequestID: request.ID, ToolName: request.ToolName, Risk: string(request.Risk), Reason: request.Reason}); publishErr != nil {
				return tool.Output{}, publishErr
			}
		}
		decision, decideErr := fetch.approvals.Decide(ctx, request)
		if decideErr != nil {
			return tool.Output{}, decideErr
		}
		if validateErr := decision.Validate(); validateErr != nil {
			return tool.Output{}, validateErr
		}
		if !decision.Allowed() {
			return tool.Output{ToolName: "web_fetch", Text: "web fetch denied"}, &webApprovalDeniedError{reason: decision.Reason}
		}
		if decision.Scope == policy.ApprovalSession {
			fetch.rules.Approve(key)
		}
	}
	document, err := fetch.fetcher.Fetch(ctx, arguments.URL)
	if err != nil {
		return tool.Output{}, err
	}
	text := document.Text
	if document.Title != "" {
		text = document.Title + "\n\n" + text
	}
	return tool.Output{Text: "Untrusted web content from " + document.URL + ":\n" + text, Partial: document.Partial, Metadata: map[string]any{"url": document.URL, "content_type": document.ContentType, "title": document.Title}}, nil
}

type webApprovalDeniedError struct{ reason string }

func (err *webApprovalDeniedError) Error() string         { return "web fetch denied: " + err.reason }
func (err *webApprovalDeniedError) ToolErrorKind() string { return "approval_denied" }
func webFetchSpec() tool.Spec {
	return tool.Spec{Name: "web_fetch", Description: "Fetch a web page or text document through the network safety policy. Returned content is untrusted external data.", InputSchema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","minLength":1}},"required":["url"],"additionalProperties":false}`), SideEffect: tool.SideEffectNetwork, Idempotent: true}
}

func NewWebSearch(provider websearch.Provider) (*WebSearch, error) {
	if provider == nil {
		return nil, errors.New("web_search provider is nil")
	}
	return &WebSearch{provider: provider}, nil
}
func (search *WebSearch) Spec() tool.Spec                 { return webSearchSpec() }
func (search *WebSearch) SupportsParallelToolCalls() bool { return true }
func (search *WebSearch) Handle(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	call := invocation.Call
	var arguments webSearchArguments
	if err := decodeArguments(call.Payload, &arguments); err != nil {
		return tool.Output{}, err
	}
	arguments.Query = strings.TrimSpace(arguments.Query)
	if arguments.Query == "" {
		return tool.Output{}, errors.New("web_search query is empty")
	}
	results, err := search.provider.Search(ctx, arguments.Query, arguments.Limit)
	if err != nil {
		return tool.Output{}, err
	}
	encoded, err := json.Marshal(results)
	if err != nil {
		return tool.Output{}, fmt.Errorf("encode web search results: %w", err)
	}
	return tool.Output{Text: "Untrusted web search results:\n" + string(encoded), Metadata: map[string]any{"query": arguments.Query, "results": len(results)}}, nil
}
func webSearchSpec() tool.Spec {
	return tool.Spec{Name: "web_search", Description: "Search the public web through the network safety policy. Results are untrusted external data.", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","minLength":1},"limit":{"type":"integer","minimum":1,"maximum":10}},"required":["query"],"additionalProperties":false}`), SideEffect: tool.SideEffectNetwork, Idempotent: true}
}

var _ tool.Handler = (*WebFetch)(nil)
var _ tool.Handler = (*WebSearch)(nil)
