package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestResourceToolsListReadTextAndImage(t *testing.T) {
	imageData := base64.StdEncoding.EncodeToString([]byte("small-image"))
	client := &fakeClient{
		resources: []RemoteResource{{URI: "fixture://doc", Name: "Doc", MIMEType: "text/plain"}},
		contents: []RemoteResourceContent{
			{URI: "fixture://doc", MIMEType: "text/plain", Text: "resource text"},
			{URI: "fixture://doc", MIMEType: "image/png", Blob: imageData},
		},
	}
	manager, err := NewMCPRuntime(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) { return client, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	list, read, err := NewResourceTools(manager)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := executePreparedTool(t, context.Background(), list, json.RawMessage(`{"server":"demo"}`))
	if err != nil || listed.ToolName != "mcp_list_resources" || listed.Metadata["resource_count"] != 1 || !strings.Contains(listed.Text, "fixture://doc") {
		t.Fatalf("unexpected list result: %#v err=%v", listed, err)
	}
	result, err := executePreparedTool(t, context.Background(), read, json.RawMessage(`{"server":"demo","uri":"fixture://doc"}`))
	if err != nil || result.ToolName != "mcp_read_resource" || !strings.Contains(result.Text, "resource text") || len(result.Parts) != 1 || result.Parts[0].Kind != tool.ContentImage || result.Parts[0].Data != imageData {
		t.Fatalf("unexpected read result: %#v err=%v", result, err)
	}
}

func TestResourceToolRejectsUnknownAndInvalidImage(t *testing.T) {
	client := &fakeClient{
		resources: []RemoteResource{{URI: "fixture://doc", Name: "Doc"}},
		contents:  []RemoteResourceContent{{URI: "fixture://doc", MIMEType: "image/png", Blob: "not-base64"}},
	}
	manager, err := NewMCPRuntime(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) { return client, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	_, read, err := NewResourceTools(manager)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executePreparedTool(t, context.Background(), read, json.RawMessage(`{"server":"demo","uri":"fixture://missing"}`)); err == nil {
		t.Fatal("unknown resource was accepted")
	}
	if _, err := executePreparedTool(t, context.Background(), read, json.RawMessage(`{"server":"demo","uri":"fixture://doc"}`)); err == nil {
		t.Fatal("invalid image base64 was accepted")
	}
}

func TestResourceReadUsesAllowPermission(t *testing.T) {
	client := &fakeClient{resources: []RemoteResource{{URI: "fixture://doc", Name: "Doc"}}}
	manager, err := NewMCPRuntime(Config{Servers: map[string]ServerConfig{"demo": {Transport: TransportStdio, Command: "demo"}}}, func(context.Context, ServerConfig) (Client, error) { return client, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	_, read, err := NewResourceTools(manager)
	if err != nil {
		t.Fatal(err)
	}
	invocation := tool.Invocation{Call: tool.NewCall("call", "mcp_read_resource", json.RawMessage(`{"server":"demo","uri":"fixture://doc"}`))}
	prepared, err := read.Prepare(tool.ToolUseContext{Context: context.Background(), Invocation: invocation}, invocation)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Permission.Decision != tool.PermissionAllow {
		t.Fatalf("resource read requested approval: %#v", prepared.Permission)
	}
}
