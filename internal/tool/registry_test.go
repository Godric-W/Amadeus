package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
)

type fakeHandler struct {
	spec     Spec
	parallel bool
}

func (handler *fakeHandler) Spec() Spec                      { return handler.spec }
func (handler *fakeHandler) SupportsParallelToolCalls() bool { return handler.parallel }
func (handler *fakeHandler) Handle(_ context.Context, invocation Invocation) (Output, error) {
	return Output{CallID: invocation.Call.ID, ToolName: handler.spec.Name}, nil
}

func TestRegistryRegisterLookupDuplicateAndSnapshot(t *testing.T) {
	registry := NewRegistry()
	readHandler := newFakeHandler("read_file", true)
	writeHandler := newFakeHandler("write_file", false)
	if err := registry.Register(writeHandler); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(readHandler); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(newFakeHandler("read_file", true)); !errors.Is(err, ErrDuplicateTool) {
		t.Fatalf("unexpected duplicate error: %v", err)
	}
	got, exists := registry.Lookup(" read_file ")
	if !exists || got != readHandler {
		t.Fatalf("unexpected lookup result: %#v, %v", got, exists)
	}
	entries := registry.Snapshot()
	if len(entries) != 2 || entries[0].Spec.Name != "read_file" || entries[1].Spec.Name != "write_file" {
		t.Fatalf("snapshot is not stable: %#v", entries)
	}
	entries[0].Spec.InputSchema[0] = '['
	if registry.Snapshot()[0].Spec.InputSchema[0] == '[' {
		t.Fatal("snapshot mutation changed registry state")
	}
}

func TestRegistryRejectsNilAndInvalidHandlers(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(nil); !errors.Is(err, ErrNilTool) {
		t.Fatalf("unexpected nil handler error: %v", err)
	}
	var typedNil *fakeHandler
	if err := registry.Register(typedNil); !errors.Is(err, ErrNilTool) {
		t.Fatalf("unexpected typed nil error: %v", err)
	}
	if err := registry.Register(newFakeHandler("", false)); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("unexpected invalid spec error: %v", err)
	}
}

func TestRegistrySupportsConcurrentRegistrationAndLookup(t *testing.T) {
	registry := NewRegistry()
	const handlerCount = 100
	var waitGroup sync.WaitGroup
	for index := 0; index < handlerCount; index++ {
		index := index
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			name := fmt.Sprintf("tool_%03d", index)
			if err := registry.Register(newFakeHandler(name, true)); err != nil {
				t.Errorf("register %s: %v", name, err)
				return
			}
			if _, exists := registry.Lookup(name); !exists {
				t.Errorf("lookup %s failed", name)
			}
		}()
	}
	waitGroup.Wait()
	if registry.Len() != handlerCount {
		t.Fatalf("unexpected registry length: got %d, want %d", registry.Len(), handlerCount)
	}
}

func TestRegistryReplaceGroupIsAtomicAndLeavesOtherHandlers(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(newFakeHandler("read_file", true)); err != nil {
		t.Fatal(err)
	}
	if err := registry.ReplaceGroup("mcp:demo", []Handler{newFakeHandler("mcp__demo__one", false)}); err != nil {
		t.Fatal(err)
	}
	if err := registry.ReplaceGroup("mcp:demo", []Handler{newFakeHandler("mcp__demo__two", false)}); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Lookup("mcp__demo__one"); ok {
		t.Fatal("stale dynamic handler remained registered")
	}
	if _, ok := registry.Lookup("mcp__demo__two"); !ok {
		t.Fatal("replacement dynamic handler is missing")
	}
	if _, ok := registry.Lookup("read_file"); !ok {
		t.Fatal("group replacement removed unrelated handler")
	}
	if err := registry.ReplaceGroup("mcp:demo", []Handler{newFakeHandler("read_file", true)}); !errors.Is(err, ErrDuplicateTool) {
		t.Fatalf("group replacement accepted collision: %v", err)
	}
}

func TestRegistryStoresExposureMetadata(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterWithRegistration(newFakeHandler("view_image", true), Registration{Exposure: ExposureConditional, Condition: "provider.images"}); err != nil {
		t.Fatal(err)
	}
	entries := registry.Snapshot()
	if len(entries) != 1 || entries[0].Exposure != ExposureConditional || entries[0].Condition != "provider.images" {
		t.Fatalf("unexpected registry metadata: %#v", entries)
	}
	if _, ok := registry.LookupVisible("view_image", nil); ok {
		t.Fatal("conditional Handler was visible without its condition")
	}
	if handler, ok := registry.LookupVisible("view_image", map[string]bool{"provider.images": true}); !ok || handler.Spec().Name != "view_image" {
		t.Fatalf("conditional Handler was not visible: %#v, %v", handler, ok)
	}
}

func newFakeHandler(name string, parallel bool) *fakeHandler {
	return &fakeHandler{parallel: parallel, spec: Spec{Name: name, Description: "test tool", InputSchema: json.RawMessage(`{"type":"object"}`), SideEffect: SideEffectRead, Idempotent: true}}
}

var _ Handler = (*fakeHandler)(nil)
