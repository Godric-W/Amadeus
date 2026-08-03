package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/project"
)

func TestProcessClientLifecycleAndDiagnostics(t *testing.T) {
	const fixtureTimeout = 5 * time.Second
	root := newTestRoot(t)
	client, err := NewProcessClient(ProcessOptions{
		Command: os.Args[0], Args: []string{"-test.run=TestLSPFixtureProcess"}, Root: root, Timeout: fixtureTimeout,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AMADEUS_LSP_FIXTURE", "serve")
	if err := client.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Observe(context.Background(), Document{Path: "main.go", Content: "package main\n"}); err != nil {
		t.Fatal(err)
	}
	var diagnostics []Diagnostic
	deadline := time.Now().Add(fixtureTimeout)
	for time.Now().Before(deadline) {
		diagnostics = client.Diagnostics("main.go")
		if len(diagnostics) > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(diagnostics) != 1 || diagnostics[0].Severity != SeverityError || diagnostics[0].Code != "E100" || diagnostics[0].Message != "fixture diagnostic" {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.Start(context.Background()); err == nil {
		t.Fatal("expected closed client rejection")
	}
}

func TestProcessClientCancelsInitialization(t *testing.T) {
	root := newTestRoot(t)
	t.Setenv("AMADEUS_LSP_FIXTURE", "hang")
	client, err := NewProcessClient(ProcessOptions{
		Command: os.Args[0], Args: []string{"-test.run=TestLSPFixtureProcess"}, Root: root, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := client.Start(ctx); err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("expected initialization cancellation, got %v", err)
	}
}

func TestProcessClientRejectsInvalidInputs(t *testing.T) {
	if _, err := NewProcessClient(ProcessOptions{}); err == nil {
		t.Fatal("expected empty command rejection")
	}
	root := newTestRoot(t)
	client, err := NewProcessClient(ProcessOptions{Command: "fixture", Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Observe(context.Background(), Document{Path: "../outside.go"}); err == nil {
		t.Fatal("expected out-of-root document rejection")
	}
}

func TestLSPFixtureProcess(t *testing.T) {
	mode := os.Getenv("AMADEUS_LSP_FIXTURE")
	if mode == "" {
		return
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		message, err := readMessage(reader)
		if err != nil {
			return
		}
		switch message.Method {
		case "initialize":
			if mode == "hang" {
				time.Sleep(200 * time.Millisecond)
			}
			writeFixtureMessage(rpcMessage{JSONRPC: "2.0", ID: message.ID, Result: json.RawMessage(`{"capabilities":{}}`)})
		case "textDocument/didOpen", "textDocument/didChange":
			var params struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(message.Params, &params)
			writeFixtureMessage(rpcMessage{JSONRPC: "2.0", Method: "textDocument/publishDiagnostics", Params: json.RawMessage(fmt.Sprintf(`{"uri":%q,"diagnostics":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"severity":1,"code":"E100","source":"fixture","message":"fixture diagnostic"}]}`, params.TextDocument.URI))})
		case "shutdown":
			writeFixtureMessage(rpcMessage{JSONRPC: "2.0", ID: message.ID, Result: json.RawMessage(`null`)})
		case "exit":
			return
		}
	}
}

func writeFixtureMessage(message rpcMessage) {
	content, _ := json.Marshal(message)
	_, _ = fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n", len(content))
	_, _ = os.Stdout.Write(content)
}

func newTestRoot(t *testing.T) project.Root {
	t.Helper()
	path := t.TempDir()
	if err := os.WriteFile(filepath.Join(path, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := project.NewRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	return root
}
