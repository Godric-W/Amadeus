package builtin

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/tool"
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
	registry, err := NewCoreRegistry(root, options)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, registry.Len())
	for _, entry := range registry.Snapshot() {
		names = append(names, entry.Spec.Name)
	}
	sort.Strings(names)
	want := []string{"edit", "execute_command", "glob", "grep", "read", "request_user_input", "update_plan", "write", "write_stdin"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("core tools = %v, want %v", names, want)
	}
	for _, retired := range []string{"apply_patch"} {
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

func TestCoreSpecsExposeSourceSpecificModelContracts(t *testing.T) {
	specs := make(map[string]tool.ToolSpec)
	for _, spec := range CoreSpecs() {
		specs[spec.Name] = spec
	}
	for name, phrase := range map[string]string{
		"read":               "complete_snapshot",
		"edit":               "complete, non-truncated snapshot",
		"write":              "complete, non-truncated snapshot",
		"glob":               "path-sorted",
		"grep":               "literal by default",
		"execute_command":    "process ID",
		"write_stdin":        "originating command Approval",
		"update_plan":        "At most one step",
		"request_user_input": "Default or Plan mode",
	} {
		if !strings.Contains(specs[name].Description, phrase) {
			t.Fatalf("%s description does not contain %q: %q", name, phrase, specs[name].Description)
		}
	}
	var readSchema map[string]any
	if err := json.Unmarshal(specs["read"].InputSchema, &readSchema); err != nil {
		t.Fatal(err)
	}
	properties := readSchema["properties"].(map[string]any)
	if properties["path"].(map[string]any)["description"] == nil || properties["line"].(map[string]any)["description"] == nil {
		t.Fatalf("read property descriptions are incomplete: %#v", readSchema)
	}
}
