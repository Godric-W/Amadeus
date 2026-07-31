package prompts

import (
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestBuiltinsMatchStableCatalog(t *testing.T) {
	expected := []ID{
		Base,
		EngineProtocol,
		Approval,
		Tools,
		RuntimeContext,
		Instructions,
		Skills,
		ContextManagement,
		Handoff,
		EngineRetry,
		TaskReflection,
	}
	if got := All(); !reflect.DeepEqual(got, expected) {
		t.Fatalf("unexpected built-in prompt catalog: got %v, want %v", got, expected)
	}
	if err := Validate(); err != nil {
		t.Fatalf("validate built-in prompts: %v", err)
	}

	embeddedFiles := make([]string, 0, len(expected))
	if err := fs.WalkDir(Embedded(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			embeddedFiles = append(embeddedFiles, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk embedded prompts: %v", err)
	}
	wantFiles := make([]string, len(expected))
	for index, id := range expected {
		wantFiles[index] = string(id)
	}
	sort.Strings(embeddedFiles)
	sort.Strings(wantFiles)
	if !reflect.DeepEqual(embeddedFiles, wantFiles) {
		t.Fatalf("embedded prompt files drifted from catalog: got %v, want %v", embeddedFiles, wantFiles)
	}
}

func TestAgentSystemUsesDocumentedLayerOrder(t *testing.T) {
	expectedLayers := []ID{Base, EngineProtocol, Approval, Tools, RuntimeContext, Instructions, Skills, ContextManagement, Handoff}
	if got := AgentLayers(); !reflect.DeepEqual(got, expectedLayers) {
		t.Fatalf("unexpected Agent prompt layers: got %v, want %v", got, expectedLayers)
	}

	combined := AgentSystem()
	position := -1
	for _, id := range expectedLayers {
		content, err := Read(id)
		if err != nil {
			t.Fatalf("read Agent layer %q: %v", id, err)
		}
		next := strings.Index(combined, content)
		if next <= position {
			t.Fatalf("Agent layer %q is missing or out of order", id)
		}
		position = next
	}
	if !strings.Contains(combined, "Deterministic verification") || !strings.Contains(combined, "Do not claim success") {
		t.Fatalf("Agent protocol omitted verification or handoff contract: %q", combined)
	}
	for _, required := range []string{"structured exploration tools", "Use `apply_patch` for normal edits", "`mode=create`", "builds, tests, Git", "Do not use shell redirection"} {
		if !strings.Contains(combined, required) {
			t.Fatalf("Agent protocol omitted tool selection rule %q", required)
		}
	}
}

func TestCatalogSnapshotsAndUnknownIDsAreSafe(t *testing.T) {
	layers := AgentLayers()
	layers[0] = EngineRetry
	if AgentLayers()[0] != Base {
		t.Fatal("Agent layer snapshot shares mutable storage")
	}
	all := All()
	all[0] = EngineRetry
	if All()[0] != Base {
		t.Fatal("built-in prompt catalog snapshot shares mutable storage")
	}
	if _, err := Read("missing.md"); err == nil || !strings.Contains(err.Error(), "unknown built-in prompt") {
		t.Fatalf("unexpected unknown prompt error: %v", err)
	}
}
