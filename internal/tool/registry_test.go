package tool

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
)

type fakeTool struct {
	spec     ToolSpec
	parallel bool
}

func (toolImpl *fakeTool) Spec() ToolSpec                                 { return toolImpl.spec }
func (toolImpl *fakeTool) SupportsParallelToolCalls() bool                { return toolImpl.parallel }
func (toolImpl *fakeTool) ValidateInput(ToolUseContext, Invocation) error { return nil }
func (toolImpl *fakeTool) Prepare(_ ToolUseContext, invocation Invocation) (PreparedToolUse, error) {
	return PreparedToolUse{Invocation: invocation, Permission: AllowPermission()}, nil
}
func (toolImpl *fakeTool) Execute(_ ToolUseContext, prepared PreparedToolUse) (ToolResult, error) {
	return ToolResult{CallID: prepared.Invocation.Call.ID, ToolName: toolImpl.spec.Name}, nil
}

func TestRegistryRegisterLookupDuplicateAndSnapshot(t *testing.T) {
	registry := NewRegistry()
	readTool := newFakeTool("read", true)
	writeTool := newFakeTool("write_file", false)
	if err := registry.RegisterDefinition(writeTool); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterDefinition(readTool); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterDefinition(newFakeTool("read", true)); !errors.Is(err, ErrDuplicateTool) {
		t.Fatalf("unexpected duplicate error: %v", err)
	}
	got, exists := registry.Lookup(" read ")
	if !exists || got.Spec().Name != readTool.Spec().Name {
		t.Fatalf("unexpected lookup result: %#v, %v", got, exists)
	}
	entries := registry.Snapshot()
	if len(entries) != 2 || entries[0].Spec.Name != "read" {
		t.Fatalf("snapshot is not stable: %#v", entries)
	}
	entries[0].Spec.InputSchema[0] = '['
	if registry.Snapshot()[0].Spec.InputSchema[0] == '[' {
		t.Fatal("snapshot mutation changed registry state")
	}
}

func TestRegistryRejectsNilAndInvalidTools(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterDefinition(nil); !errors.Is(err, ErrNilTool) {
		t.Fatalf("unexpected nil tool error: %v", err)
	}
	var typedNil *fakeTool
	if err := registry.RegisterDefinition(typedNil); !errors.Is(err, ErrNilTool) {
		t.Fatalf("unexpected typed nil error: %v", err)
	}
	if err := registry.RegisterDefinition(newFakeTool("", false)); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("unexpected invalid spec error: %v", err)
	}
}

func TestRegistrySupportsConcurrentRegistrationAndLookup(t *testing.T) {
	registry := NewRegistry()
	const toolCount = 100
	var waitGroup sync.WaitGroup
	for index := 0; index < toolCount; index++ {
		index := index
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			name := fmt.Sprintf("tool_%03d", index)
			if err := registry.RegisterDefinition(newFakeTool(name, true)); err != nil {
				t.Errorf("register %s: %v", name, err)
				return
			}
			if _, exists := registry.Lookup(name); !exists {
				t.Errorf("lookup %s failed", name)
			}
		}()
	}
	waitGroup.Wait()
	if registry.Len() != toolCount {
		t.Fatalf("unexpected registry length: got %d, want %d", registry.Len(), toolCount)
	}
}

func TestRegistryReplaceGroupIsAtomicAndLeavesOtherTools(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterDefinition(newFakeTool("read", true)); err != nil {
		t.Fatal(err)
	}
	if err := registry.ReplaceDefinitionGroup("mcp:demo", []ToolDefinition{newFakeTool("mcp__demo__one", false)}); err != nil {
		t.Fatal(err)
	}
	if err := registry.ReplaceDefinitionGroup("mcp:demo", []ToolDefinition{newFakeTool("mcp__demo__two", false)}); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Lookup("mcp__demo__one"); ok {
		t.Fatal("stale dynamic tool remained registered")
	}
	if _, ok := registry.Lookup("mcp__demo__two"); !ok {
		t.Fatal("replacement dynamic tool is missing")
	}
	if _, ok := registry.Lookup("read"); !ok {
		t.Fatal("group replacement removed unrelated tool")
	}
	if err := registry.ReplaceDefinitionGroup("mcp:demo", []ToolDefinition{newFakeTool("read", true)}); !errors.Is(err, ErrDuplicateTool) {
		t.Fatalf("group replacement accepted collision: %v", err)
	}
}

func TestRegistryStoresExposureMetadata(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterDefinitionWithRegistration(newFakeTool("view_image", true), Registration{Exposure: ExposureConditional, Condition: "provider.images"}); err != nil {
		t.Fatal(err)
	}
	entries := registry.Snapshot()
	if len(entries) != 1 || entries[0].Exposure != ExposureConditional || entries[0].Condition != "provider.images" {
		t.Fatalf("unexpected registry metadata: %#v", entries)
	}
	if _, ok := registry.LookupVisible("view_image", nil); ok {
		t.Fatal("conditional Tool was visible without its condition")
	}
	if toolImpl, ok := registry.LookupVisible("view_image", map[string]bool{"provider.images": true}); !ok || toolImpl.Spec().Name != "view_image" {
		t.Fatalf("conditional Tool was not visible: %#v, %v", toolImpl, ok)
	}
}

func newFakeTool(name string, parallel bool) *fakeTool {
	return &fakeTool{parallel: parallel, spec: ToolSpec{Name: name, Description: "test tool", InputSchema: json.RawMessage(`{"type":"object"}`), SideEffect: SideEffectRead, Idempotent: true}}
}

var _ ToolDefinition = (*fakeTool)(nil)
