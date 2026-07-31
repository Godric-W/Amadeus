package prompt

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Godric-W/Amadeus/prompts"
)

func TestBuiltinAssemblerProducesStableAgentBundle(t *testing.T) {
	repository, err := NewBuiltinRepository()
	if err != nil {
		t.Fatalf("create built-in prompt repository: %v", err)
	}
	assembler, err := NewAssembler(repository)
	if err != nil {
		t.Fatalf("create prompt assembler: %v", err)
	}
	layers := promptPaths(prompts.AgentLayers())

	first, err := assembler.Assemble(AssembleInput{Layers: layers})
	if err != nil {
		t.Fatalf("assemble built-in Agent prompt: %v", err)
	}
	second, err := assembler.Assemble(AssembleInput{Layers: layers})
	if err != nil {
		t.Fatalf("reassemble built-in Agent prompt: %v", err)
	}
	if first.Content != prompts.AgentSystem() || first.SHA256 != second.SHA256 || len(first.SHA256) != 64 {
		t.Fatalf("built-in Agent bundle is unstable: first=%#v second=%#v", first, second)
	}
	if len(first.Sources) != len(layers) || len(first.RequiredVariables) != 0 {
		t.Fatalf("unexpected built-in Agent bundle metadata: %#v", first)
	}
	for index, source := range first.Sources {
		if source.Kind != BuiltinSource || source.Path != layers[index] || len(source.SHA256) != 64 {
			t.Fatalf("unexpected source %d: %#v", index, source)
		}
	}
}

func TestAssemblerReportsAllMissingLayers(t *testing.T) {
	assembler := newMapAssembler(t, fstest.MapFS{"base.md": {Data: []byte("base")}})
	_, err := assembler.Assemble(AssembleInput{Layers: []string{"base.md", "approval.md", "handoff.md"}})
	var missing *MissingLayersError
	if !errors.As(err, &missing) {
		t.Fatalf("unexpected missing layer error: %v", err)
	}
	want := []string{"approval.md", "handoff.md"}
	if !reflect.DeepEqual(missing.Layers, want) || err.Error() != "missing prompt layers: approval.md, handoff.md" {
		t.Fatalf("unexpected missing layer diagnostics: %#v", missing)
	}
}

func TestAssemblerValidatesAndRendersVariables(t *testing.T) {
	assembler := newMapAssembler(t, fstest.MapFS{
		"base.md":    {Data: []byte("Project {{project_root}}")},
		"context.md": {Data: []byte("Task {{task}} in {{project_root}}")},
	})
	input := AssembleInput{
		Layers:    []string{"base.md", "context.md"},
		Variables: map[string]string{"project_root": "/work", "extra": "unused"},
	}
	_, err := assembler.Assemble(input)
	var variableErr *VariableError
	if !errors.As(err, &variableErr) {
		t.Fatalf("unexpected variable validation error: %v", err)
	}
	if !reflect.DeepEqual(variableErr.Missing, []string{"task"}) || !reflect.DeepEqual(variableErr.Unknown, []string{"extra"}) {
		t.Fatalf("unexpected variable diagnostics: %#v", variableErr)
	}

	input.Variables = map[string]string{"project_root": "/work/$repo", "task": "fix braces {{literally}}"}
	bundle, err := assembler.Assemble(input)
	if err != nil {
		t.Fatalf("assemble variables: %v", err)
	}
	wantContent := "Project /work/$repo\n\nTask fix braces {{literally}} in /work/$repo"
	if bundle.Content != wantContent {
		t.Fatalf("unexpected rendered content: got %q, want %q", bundle.Content, wantContent)
	}
	wantRequired := []string{"project_root", "task"}
	if !reflect.DeepEqual(bundle.RequiredVariables, wantRequired) {
		t.Fatalf("unexpected required variables: got %v, want %v", bundle.RequiredVariables, wantRequired)
	}
	if len(bundle.Sources) != 2 || !reflect.DeepEqual(bundle.Sources[0].Variables, []string{"project_root"}) {
		t.Fatalf("unexpected source variables: %#v", bundle.Sources)
	}
}

func TestAssemblerHashChangesWithRenderedContentAndLayerOrder(t *testing.T) {
	assembler := newMapAssembler(t, fstest.MapFS{
		"a.md": {Data: []byte("A {{value}}")},
		"b.md": {Data: []byte("B")},
	})
	first := mustAssemble(t, assembler, AssembleInput{Layers: []string{"a.md", "b.md"}, Variables: map[string]string{"value": "one"}})
	changedValue := mustAssemble(t, assembler, AssembleInput{Layers: []string{"a.md", "b.md"}, Variables: map[string]string{"value": "two"}})
	changedOrder := mustAssemble(t, assembler, AssembleInput{Layers: []string{"b.md", "a.md"}, Variables: map[string]string{"value": "one"}})
	if first.SHA256 == changedValue.SHA256 || first.SHA256 == changedOrder.SHA256 {
		t.Fatalf("final hash did not capture rendered content or layer order: %q %q %q", first.SHA256, changedValue.SHA256, changedOrder.SHA256)
	}
}

func TestAssemblerRejectsInvalidAssemblyShape(t *testing.T) {
	assembler := newMapAssembler(t, fstest.MapFS{"base.md": {Data: []byte("base")}})
	tests := []struct {
		name     string
		input    AssembleInput
		contains string
	}{
		{name: "no layers", contains: "layers are empty"},
		{name: "empty layer", input: AssembleInput{Layers: []string{"base.md", " "}}, contains: "empty layer"},
		{name: "duplicate layer", input: AssembleInput{Layers: []string{"base.md", "base.md"}}, contains: "is duplicated"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := assembler.Assemble(test.input); err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("unexpected assembly error: %v", err)
			}
		})
	}
	if _, err := NewAssembler(nil); err == nil || !strings.Contains(err.Error(), "repository is nil") {
		t.Fatalf("unexpected nil repository error: %v", err)
	}
	var nilAssembler *Assembler
	if _, err := nilAssembler.Assemble(AssembleInput{Layers: []string{"base.md"}}); err == nil || !strings.Contains(err.Error(), "assembler is nil") {
		t.Fatalf("unexpected nil assembler error: %v", err)
	}
}

func newMapAssembler(t *testing.T, files fstest.MapFS) *Assembler {
	t.Helper()
	repository, err := NewRepository(files, "test")
	if err != nil {
		t.Fatalf("create test repository: %v", err)
	}
	assembler, err := NewAssembler(repository)
	if err != nil {
		t.Fatalf("create test assembler: %v", err)
	}
	return assembler
}

func mustAssemble(t *testing.T, assembler *Assembler, input AssembleInput) Bundle {
	t.Helper()
	bundle, err := assembler.Assemble(input)
	if err != nil {
		t.Fatalf("assemble prompt: %v", err)
	}
	return bundle
}

func promptPaths(ids []prompts.ID) []string {
	paths := make([]string, len(ids))
	for index, id := range ids {
		paths[index] = string(id)
	}
	return paths
}
