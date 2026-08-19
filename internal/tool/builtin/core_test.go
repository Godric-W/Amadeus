package builtin

import (
	"reflect"
	"sort"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/project"
)

func TestCoreRegistryContainsOnlyPublicCoreTools(t *testing.T) {
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	filesystem, err := project.NewFileSystemPolicy(project.FileSystemPolicyOptions{
		CWD: root.Path(), Profile: project.PermissionProfile{ReadHost: true, WorkspaceRoots: []string{root.Path()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	options := DefaultCoreToolOptions()
	options.FileSystemPolicy = filesystem
	options.Events = protocol.NewMemorySink()
	options.PlanUpdater = &testPlanUpdater{}
	registry, err := NewCoreRegistry(root, options)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, registry.Len())
	for _, entry := range registry.Snapshot() {
		names = append(names, entry.Spec.Name)
	}
	sort.Strings(names)
	want := []string{"edit", "execute_command", "glob", "grep", "read", "update_plan", "write", "write_stdin"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("core tools = %v, want %v", names, want)
	}
	for _, retired := range []string{"apply_patch", "request_user_input"} {
		if _, exists := registry.Lookup(retired); exists {
			t.Fatalf("retired tool %q entered the default core registry", retired)
		}
	}
}

func TestCoreSpecsReturnIndependentCopies(t *testing.T) {
	first := CoreSpecs()
	second := CoreSpecs()
	first[0].InputSchema[0] = '['
	first[0].Description = "changed"
	if string(second[0].InputSchema[:1]) != "{" || second[0].Description == "changed" {
		t.Fatal("core specs share mutable storage")
	}
}
