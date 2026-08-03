package mcp

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestStdioFixtureProcess(t *testing.T) {
	if os.Getenv("AMADEUS_MCP_STDIO_FIXTURE") != "1" {
		return
	}
	mcpServer := server.NewMCPServer("fixture", "1.0.0")
	mcpServer.AddTool(mcpgo.NewTool("echo"), func(_ context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return mcpgo.NewToolResultText("echo"), nil
	})
	if err := server.ServeStdio(mcpServer); err != nil {
		t.Fatalf("serve MCP stdio fixture: %v", err)
	}
}

func TestNativeClientStdioInitializesListsCallsAndCloses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := NewClient(ctx, ServerConfig{Transport: TransportStdio, Command: os.Args[0], Args: []string{"-test.run=^TestStdioFixtureProcess$"}, Env: map[string]string{"AMADEUS_MCP_STDIO_FIXTURE": "1"}, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("start stdio MCP client: %v", err)
	}
	defer client.Close()
	tools, err := client.ListTools(ctx)
	if err != nil || len(tools) != 1 || tools[0].Name != "echo" {
		stderr := ""
		if native, ok := client.(*nativeClient); ok {
			if output, found := mcpclient.GetStderr(native.client); found {
				content, _ := io.ReadAll(output)
				stderr = string(content)
			}
		}
		t.Fatalf("list stdio MCP tools: tools=%#v err=%v stderr=%q", tools, err, stderr)
	}
	result, err := client.CallTool(ctx, "echo", nil)
	if err != nil || result.Text != "echo" || result.IsError {
		t.Fatalf("call stdio MCP tool: result=%#v err=%v", result, err)
	}
}

func TestNativeClientStreamableHTTPInitializesListsCallsAndSendsHeaders(t *testing.T) {
	mcpServer := server.NewMCPServer("http-fixture", "1.0.0")
	mcpServer.AddTool(mcpgo.NewTool("echo"), func(_ context.Context, request mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if request.Header.Get("X-Amadeus-Test") != "yes" {
			return nil, errors.New("missing test header")
		}
		return mcpgo.NewToolResultText("http echo"), nil
	})
	fixture := httptest.NewServer(server.NewStreamableHTTPServer(mcpServer))
	defer fixture.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := NewClient(ctx, ServerConfig{Transport: TransportStreamableHTTP, URL: fixture.URL, Headers: map[string]string{"X-Amadeus-Test": "yes"}, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("start HTTP MCP client: %v", err)
	}
	defer client.Close()
	tools, err := client.ListTools(ctx)
	if err != nil || len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("list HTTP MCP tools: tools=%#v err=%v", tools, err)
	}
	result, err := client.CallTool(ctx, "echo", nil)
	if err != nil || result.Text != "http echo" {
		t.Fatalf("call HTTP MCP tool: result=%#v err=%v", result, err)
	}
}
