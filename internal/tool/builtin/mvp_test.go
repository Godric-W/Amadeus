package builtin

import (
	"reflect"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestMVPRegistryContainsSixStableTools(t *testing.T) {
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatalf("create project root: %v", err)
	}
	registry, err := NewMVPRegistry(root, DefaultMVPOptions())
	if err != nil {
		t.Fatalf("create MVP registry: %v", err)
	}
	entries := registry.Snapshot()
	names := make([]string, len(entries))
	for index, entry := range entries {
		names[index] = entry.Spec.Name
	}
	want := []string{"execute_command", "glob_files", "grep_code", "list_dir", "read_file", "write_file"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("unexpected MVP tools: got %v, want %v", names, want)
	}
	if entries[0].Spec.SideEffect != tool.SideEffectExecute || entries[0].Spec.ParallelSafe || entries[4].Spec.SideEffect != tool.SideEffectRead || !entries[4].Spec.ParallelSafe {
		t.Fatalf("unexpected MVP metadata: %#v", entries)
	}
}

func TestMVPSpecsReturnIndependentCopies(t *testing.T) {
	first := MVPSpecs()
	second := MVPSpecs()
	first[0].InputSchema[0] = '['
	if string(second[0].InputSchema[:1]) != "{" {
		t.Fatal("MVP specs share input schema storage")
	}
	first[1].ResourceStrategy.ArgumentPaths[0] = "changed"
	if second[1].ResourceStrategy.ArgumentPaths[0] == "changed" {
		t.Fatal("MVP specs share resource strategy storage")
	}
}
