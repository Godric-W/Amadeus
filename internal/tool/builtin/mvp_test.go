package builtin

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestMVPRegistryContainsSevenStableTools(t *testing.T) {
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
	want := []string{"apply_patch", "execute_command", "glob_files", "grep_code", "list_dir", "read_file", "write_stdin"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("unexpected MVP tools: got %v, want %v", names, want)
	}
	if entries[0].Spec.SideEffect != tool.SideEffectWrite || entries[0].Handler.SupportsParallelToolCalls() || entries[0].Spec.Idempotent || entries[1].Spec.SideEffect != tool.SideEffectExecute || entries[5].Spec.SideEffect != tool.SideEffectRead || !entries[5].Handler.SupportsParallelToolCalls() {
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
	first[2].Description = "changed"
	if second[2].Description == "changed" {
		t.Fatal("MVP specs share mutable fields")
	}
}

func TestMVPDescriptionsEnforceToolSelectionBoundaries(t *testing.T) {
	descriptions := make(map[string]string)
	for _, spec := range MVPSpecs() {
		descriptions[spec.Name] = spec.Description
	}
	checks := map[string][]string{
		"read_file":       {"Preferred over shell"},
		"list_dir":        {"Preferred over shell"},
		"glob_files":      {"Preferred over shell"},
		"grep_code":       {"Preferred over shell"},
		"apply_patch":     {"Preferred tool", "editing existing files"},
		"execute_command": {"builds, tests, Git", "never use it to bypass"},
	}
	for name, fragments := range checks {
		for _, fragment := range fragments {
			if !strings.Contains(descriptions[name], fragment) {
				t.Fatalf("tool %q description omitted %q: %q", name, fragment, descriptions[name])
			}
		}
	}
}
