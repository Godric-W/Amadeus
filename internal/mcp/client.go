package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

const defaultTimeout = 30 * time.Second

type RemoteTool struct {
	Name                  string
	Description           string
	InputSchema           json.RawMessage
	ReadOnlyHint          bool
	IdempotentHint        bool
	SupportsParallelCalls bool
}

type RemoteResult struct {
	Text    string
	IsError bool
	Partial bool
}

type RemoteResource struct {
	URI         string
	Name        string
	Description string
	MIMEType    string
}

type RemoteResourceContent struct {
	URI      string
	MIMEType string
	Text     string
	Blob     string
}

type Client interface {
	ListTools(context.Context) ([]RemoteTool, error)
	CallTool(context.Context, string, json.RawMessage) (RemoteResult, error)
	ListResources(context.Context) ([]RemoteResource, error)
	ReadResource(context.Context, string) ([]RemoteResourceContent, error)
	Close() error
}

type ClientFactory func(context.Context, ServerConfig) (Client, error)

type nativeClient struct {
	client  *client.Client
	timeout time.Duration
}

func NewClient(ctx context.Context, server ServerConfig) (Client, error) {
	if err := (Config{Servers: map[string]ServerConfig{"server": server}}).Validate(); err != nil {
		return nil, err
	}
	var connection transport.Interface
	switch server.Transport {
	case TransportStdio:
		connection = transport.NewStdio(server.Command, stdioEnvironment(server.Env), server.Args...)
	case TransportStreamableHTTP:
		options := []transport.StreamableHTTPCOption{transport.WithHTTPHeaders(cloneMap(server.Headers))}
		if server.Timeout > 0 {
			options = append(options, transport.WithHTTPTimeout(server.Timeout))
		}
		value, err := transport.NewStreamableHTTP(server.URL, options...)
		if err != nil {
			return nil, fmt.Errorf("create MCP HTTP transport: %w", err)
		}
		connection = value
	default:
		return nil, fmt.Errorf("unsupported MCP transport %q", server.Transport)
	}
	value := client.NewClient(connection)
	if err := value.Start(ctx); err != nil {
		_ = value.Close()
		return nil, fmt.Errorf("start MCP client: %w", err)
	}
	request := mcpgo.InitializeRequest{}
	request.Params.ProtocolVersion = mcpgo.LATEST_PROTOCOL_VERSION
	request.Params.ClientInfo = mcpgo.Implementation{Name: "amadeus", Version: "dev"}
	initializeContext, cancel := withTimeout(ctx, server.Timeout)
	defer cancel()
	if _, err := value.Initialize(initializeContext, request); err != nil {
		_ = value.Close()
		return nil, fmt.Errorf("initialize MCP client: %w", err)
	}
	return &nativeClient{client: value, timeout: normalizeTimeout(server.Timeout)}, nil
}

func (client *nativeClient) ListTools(ctx context.Context) ([]RemoteTool, error) {
	if client == nil || client.client == nil {
		return nil, errors.New("MCP client is nil")
	}
	requestContext, cancel := withTimeout(ctx, client.timeout)
	defer cancel()
	result, err := client.client.ListTools(requestContext, mcpgo.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("list MCP tools: %w", err)
	}
	tools := make([]RemoteTool, 0, len(result.Tools))
	for _, value := range result.Tools {
		schema := value.RawInputSchema
		if len(schema) == 0 {
			schema, err = json.Marshal(value.InputSchema)
			if err != nil {
				return nil, fmt.Errorf("encode MCP tool %q schema: %w", value.Name, err)
			}
		}
		readOnly := value.Annotations.ReadOnlyHint != nil && *value.Annotations.ReadOnlyHint
		idempotent := value.Annotations.IdempotentHint != nil && *value.Annotations.IdempotentHint
		tools = append(tools, RemoteTool{
			Name:                  value.Name,
			Description:           value.Description,
			InputSchema:           append(json.RawMessage(nil), schema...),
			ReadOnlyHint:          readOnly,
			IdempotentHint:        idempotent,
			SupportsParallelCalls: readOnly,
		})
	}
	sort.Slice(tools, func(left, right int) bool { return tools[left].Name < tools[right].Name })
	return tools, nil
}

func (client *nativeClient) CallTool(ctx context.Context, name string, arguments json.RawMessage) (RemoteResult, error) {
	if client == nil || client.client == nil {
		return RemoteResult{}, errors.New("MCP client is nil")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return RemoteResult{}, errors.New("MCP tool name is empty")
	}
	var decoded any
	if len(arguments) > 0 && string(arguments) != "null" {
		if err := json.Unmarshal(arguments, &decoded); err != nil {
			return RemoteResult{}, fmt.Errorf("decode MCP tool arguments: %w", err)
		}
	}
	requestContext, cancel := withTimeout(ctx, client.timeout)
	defer cancel()
	result, err := client.client.CallTool(requestContext, mcpgo.CallToolRequest{Params: mcpgo.CallToolParams{Name: name, Arguments: decoded}})
	if err != nil {
		return RemoteResult{}, fmt.Errorf("call MCP tool %q: %w", name, err)
	}
	text := make([]string, 0, len(result.Content)+1)
	for _, content := range result.Content {
		if value, ok := content.(mcpgo.TextContent); ok {
			text = append(text, value.Text)
		}
	}
	if len(text) == 0 && len(result.RawStructuredContent) > 0 {
		text = append(text, string(result.RawStructuredContent))
	}
	return RemoteResult{Text: strings.Join(text, "\n"), IsError: result.IsError}, nil
}

func (client *nativeClient) ListResources(ctx context.Context) ([]RemoteResource, error) {
	if client == nil || client.client == nil {
		return nil, errors.New("MCP client is nil")
	}
	requestContext, cancel := withTimeout(ctx, client.timeout)
	defer cancel()
	result, err := client.client.ListResources(requestContext, mcpgo.ListResourcesRequest{})
	if err != nil {
		return nil, fmt.Errorf("list MCP resources: %w", err)
	}
	resources := make([]RemoteResource, 0, len(result.Resources))
	for _, value := range result.Resources {
		resources = append(resources, RemoteResource{URI: value.URI, Name: value.Name, Description: value.Description, MIMEType: value.MIMEType})
	}
	sort.Slice(resources, func(left, right int) bool { return resources[left].URI < resources[right].URI })
	return resources, nil
}

func (client *nativeClient) ReadResource(ctx context.Context, uri string) ([]RemoteResourceContent, error) {
	if client == nil || client.client == nil {
		return nil, errors.New("MCP client is nil")
	}
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return nil, errors.New("MCP resource URI is empty")
	}
	requestContext, cancel := withTimeout(ctx, client.timeout)
	defer cancel()
	result, err := client.client.ReadResource(requestContext, mcpgo.ReadResourceRequest{Params: mcpgo.ReadResourceParams{URI: uri}})
	if err != nil {
		return nil, fmt.Errorf("read MCP resource %q: %w", uri, err)
	}
	contents := make([]RemoteResourceContent, 0, len(result.Contents))
	for _, content := range result.Contents {
		switch value := content.(type) {
		case mcpgo.TextResourceContents:
			contents = append(contents, RemoteResourceContent{URI: value.URI, MIMEType: value.MIMEType, Text: value.Text})
		case mcpgo.BlobResourceContents:
			contents = append(contents, RemoteResourceContent{URI: value.URI, MIMEType: value.MIMEType, Blob: value.Blob})
		}
	}
	return contents, nil
}

func (client *nativeClient) Close() error {
	if client == nil || client.client == nil {
		return nil
	}
	return client.client.Close()
}

func stdioEnvironment(values map[string]string) []string {
	environment := append([]string(nil), os.Environ()...)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	return environment
}

func withTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, normalizeTimeout(timeout))
}

func normalizeTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultTimeout
	}
	return timeout
}
