package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
)

type fakeTool struct {
	spec Spec
}

func (tool *fakeTool) Spec() Spec {
	return tool.spec
}

func (tool *fakeTool) Execute(context.Context, json.RawMessage) (Result, error) {
	return Result{ToolName: tool.spec.Name}, nil
}

func TestRegistryRegisterLookupDuplicateAndSnapshot(t *testing.T) {
	registry := NewRegistry()
	readTool := newFakeTool("read_file")
	writeTool := newFakeTool("write_file")
	if err := registry.Register(writeTool); err != nil {
		t.Fatalf("register write tool: %v", err)
	}
	if err := registry.Register(readTool); err != nil {
		t.Fatalf("register read tool: %v", err)
	}
	if err := registry.Register(newFakeTool("read_file")); !errors.Is(err, ErrDuplicateTool) {
		t.Fatalf("unexpected duplicate error: %v", err)
	}
	got, exists := registry.Lookup(" read_file ")
	if !exists || got != readTool {
		t.Fatalf("unexpected lookup result: %#v, %v", got, exists)
	}
	entries := registry.Snapshot()
	if len(entries) != 2 || entries[0].Spec.Name != "read_file" || entries[1].Spec.Name != "write_file" {
		t.Fatalf("snapshot is not stable: %#v", entries)
	}
	entries[0].Spec.ResourceStrategy.ArgumentPaths[0] = "changed"
	if registry.Snapshot()[0].Spec.ResourceStrategy.ArgumentPaths[0] != "path" {
		t.Fatal("snapshot mutation changed registry state")
	}
}

func TestRegistryRejectsNilAndInvalidTools(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(nil); !errors.Is(err, ErrNilTool) {
		t.Fatalf("unexpected nil tool error: %v", err)
	}
	var typedNil *fakeTool
	if err := registry.Register(typedNil); !errors.Is(err, ErrNilTool) {
		t.Fatalf("unexpected typed nil error: %v", err)
	}
	invalid := newFakeTool("")
	if err := registry.Register(invalid); !errors.Is(err, ErrInvalidSpec) {
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
			if err := registry.Register(newFakeTool(name)); err != nil {
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

func newFakeTool(name string) *fakeTool {
	return &fakeTool{spec: Spec{
		Name:         name,
		Description:  "test tool",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		SideEffect:   SideEffectRead,
		ParallelSafe: true,
		Idempotent:   true,
		ResourceStrategy: ResourceStrategy{
			Mode:          ResourceModeArguments,
			ArgumentPaths: []string{"path"},
		},
	}}
}
