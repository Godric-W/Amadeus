package mcp

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/project"
)

func TestLoadMergesServersWithProjectWholeServerOverride(t *testing.T) {
	userRoot := t.TempDir()
	projectPath := t.TempDir()
	writeMCPConfig(t, filepath.Join(userRoot, "mcp.yaml"), `
servers:
  shared:
    transport: stdio
    command: user-command
    args: [--user]
    env: {TOKEN: "${MCP_TOKEN}"}
  user-only:
    transport: stdio
    command: user-only
`)
	writeMCPConfig(t, filepath.Join(projectPath, ".amadeus", "mcp.yaml"), `
servers:
  shared:
    transport: streamable_http
    url: https://project.example.invalid/mcp
    headers: {Authorization: "Bearer ${MCP_TOKEN}"}
  disabled:
    transport: stdio
    command: ignored
    enabled: false
`)
	root, err := project.NewRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	configured, err := Load(userRoot, root, LoadOptions{LookupEnv: func(name string) (string, bool) { return "token", name == "MCP_TOKEN" }})
	if err != nil {
		t.Fatalf("load MCP config: %v", err)
	}
	shared, ok := configured.Server("shared")
	if !ok || shared.Transport != TransportStreamableHTTP || shared.URL != "https://project.example.invalid/mcp" || shared.Command != "" || len(shared.Args) != 0 || shared.Headers["Authorization"] != "Bearer token" {
		t.Fatalf("project whole-server override failed: %#v", shared)
	}
	if got := configured.EnabledServers(); !reflect.DeepEqual(got, []string{"shared", "user-only"}) {
		t.Fatalf("unexpected enabled servers: %v", got)
	}
	redacted := configured.Redacted()
	if redacted.Servers["shared"].Headers["Authorization"] != RedactedSecret || configured.Servers["shared"].Headers["Authorization"] == RedactedSecret {
		t.Fatalf("MCP redaction changed source or leaked header: original=%#v redacted=%#v", configured, redacted)
	}
}

func TestLoadValidatesAndRejectsUnknownOrMissingEnvironment(t *testing.T) {
	projectPath := t.TempDir()
	root, err := project.NewRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	writeMCPConfig(t, filepath.Join(projectPath, ".amadeus", "mcp.yaml"), "servers:\n  bad:\n    transport: stdio\n    command: ${MISSING}\n")
	if _, err := Load("", root, LoadOptions{LookupEnv: func(string) (string, bool) { return "", false }}); err == nil || !strings.Contains(err.Error(), "MISSING") {
		t.Fatalf("missing environment variable was accepted: %v", err)
	}
	writeMCPConfig(t, filepath.Join(projectPath, ".amadeus", "mcp.yaml"), "servers:\n  bad:\n    transport: stdio\n    command: ok\n    unknown: value\n")
	if _, err := Load("", root, LoadOptions{}); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown MCP config field was accepted: %v", err)
	}
}

func TestConfigValidationRejectsWrongTransportFields(t *testing.T) {
	configured := Config{Servers: map[string]ServerConfig{
		"bad": {Transport: TransportStdio, Command: "cmd", URL: "https://example.invalid"},
	}}
	if err := configured.Validate(); err == nil {
		t.Fatal("invalid stdio fields were accepted")
	}
	configured.Servers["bad"] = ServerConfig{Transport: TransportStreamableHTTP, URL: "https://example.invalid", Timeout: -time.Second}
	if err := configured.Validate(); err == nil {
		t.Fatal("negative timeout was accepted")
	}
}

func writeMCPConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
