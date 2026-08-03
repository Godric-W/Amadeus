package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/agent/react"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestWriteHookPublishesDiagnosticEvidenceWithoutFailingWrite(t *testing.T) {
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sink := event.NewMemorySink()
	hook, err := NewWriteHook(&hookClient{diagnostics: []Diagnostic{{Severity: SeverityError, Code: "E1", Source: "fixture", Message: "bad declaration"}}}, root, sink)
	if err != nil {
		t.Fatal(err)
	}
	registry := tool.NewRegistry()
	if err := registry.Register(&hookWriteTool{}); err != nil {
		t.Fatal(err)
	}
	executor, err := react.NewToolExecutorWithOptions(registry, tool.NewArgumentValidator(), react.ToolExecutorOptions{Hooks: []react.PostExecutionHook{hook}})
	if err != nil {
		t.Fatal(err)
	}
	execution, err := executor.Execute(context.Background(), tool.NewCall("write-1", "write_file", json.RawMessage(`{"path":"main.go","content":"package main"}`)))
	if err != nil || execution.Observation.Error != "" || len(execution.SupplementalEvidence) != 1 || execution.SupplementalEvidence[0].Verified {
		t.Fatalf("write hook execution = %#v, err=%v", execution, err)
	}
	if len(sink.Snapshot()) != 1 || sink.Snapshot()[0].Type() != event.TypeDiagnosticPublished {
		t.Fatalf("diagnostic event was not published: %#v", sink.Snapshot())
	}
}

func TestWriteHookRecordsFailureWithoutFailingWrite(t *testing.T) {
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hook, err := NewWriteHook(&hookClient{err: errors.New("server unavailable")}, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := hook.After(context.Background(), hookWriteTool{}.Spec(), tool.NewCall("write-1", "write_file", json.RawMessage(`{"path":"main.go","content":"package main"}`)), tool.Result{})
	if err != nil || len(evidence) != 1 || evidence[0].Verified || evidence[0].Kind != "diagnostic" {
		t.Fatalf("failure evidence = %#v, err=%v", evidence, err)
	}
}

func TestWriteHookFiltersConfiguredExtensions(t *testing.T) {
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := &hookClient{}
	hook, err := NewWriteHookWithOptions(client, root, nil, WriteHookOptions{Extensions: []string{".go"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"README.md", "main.go"} {
		arguments, _ := json.Marshal(map[string]string{"path": path, "content": "package main"})
		if _, err := hook.After(context.Background(), hookWriteTool{}.Spec(), tool.NewCall("write-"+path, "write_file", arguments), tool.Result{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(client.documents) != 1 || client.documents[0].Path != "main.go" {
		t.Fatalf("unexpected filtered documents: %#v", client.documents)
	}
}

type hookClient struct {
	diagnostics []Diagnostic
	err         error
	documents   []Document
}

func (client *hookClient) Start(context.Context) error { return nil }
func (client *hookClient) Observe(_ context.Context, document Document) ([]Diagnostic, error) {
	client.documents = append(client.documents, document)
	return append([]Diagnostic(nil), client.diagnostics...), client.err
}
func (client *hookClient) Diagnostics(string) []Diagnostic { return nil }
func (client *hookClient) Close(context.Context) error     { return nil }

type hookWriteTool struct{}

func (hookWriteTool) Spec() tool.Spec {
	return tool.Spec{Name: "write_file", Description: "test write", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`), SideEffect: tool.SideEffectWrite, ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeExclusive}}
}
func (hookWriteTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{Text: "written"}, nil
}
