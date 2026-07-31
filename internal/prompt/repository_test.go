package prompt

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRepositoryLoadsNormalizedDocumentMetadata(t *testing.T) {
	repository, err := NewRepository(fstest.MapFS{
		"layer.md": {Data: []byte("\nProject: {{ project_root }}\nTask: {{task}}\nAgain: {{task}}\n")},
	}, "test")
	if err != nil {
		t.Fatalf("create prompt repository: %v", err)
	}
	document, err := repository.Load("layer.md")
	if err != nil {
		t.Fatalf("load prompt document: %v", err)
	}
	if document.Content != "Project: {{ project_root }}\nTask: {{task}}\nAgain: {{task}}" {
		t.Fatalf("unexpected normalized content: %q", document.Content)
	}
	if document.Source.Kind != "test" || document.Source.Path != "layer.md" || len(document.Source.SHA256) != 64 {
		t.Fatalf("unexpected prompt source: %#v", document.Source)
	}
	wantVariables := []string{"project_root", "task"}
	if !reflect.DeepEqual(document.Source.Variables, wantVariables) {
		t.Fatalf("unexpected prompt variables: got %v, want %v", document.Source.Variables, wantVariables)
	}
}

func TestRepositoryRejectsInvalidDocuments(t *testing.T) {
	repository, err := NewRepository(fstest.MapFS{
		"empty.md":   {Data: []byte(" \n")},
		"invalid.md": {Data: []byte("Hello {{ProjectRoot}}")},
	}, "test")
	if err != nil {
		t.Fatalf("create prompt repository: %v", err)
	}
	tests := []struct {
		path     string
		contains string
	}{
		{path: "", contains: "path is empty"},
		{path: "../outside.md", contains: "is invalid"},
		{path: "empty.md", contains: "is empty"},
		{path: "invalid.md", contains: "expected {{variable_name}}"},
	}
	for _, test := range tests {
		if _, err := repository.Load(test.path); err == nil || !strings.Contains(err.Error(), test.contains) {
			t.Fatalf("unexpected repository error for %q: %v", test.path, err)
		}
	}
}

func TestRepositoryConstructorsRejectInvalidDependencies(t *testing.T) {
	if _, err := NewRepository(nil, "test"); err == nil || !strings.Contains(err.Error(), "filesystem is nil") {
		t.Fatalf("unexpected nil filesystem error: %v", err)
	}
	if _, err := NewRepository(fstest.MapFS{}, " "); err == nil || !strings.Contains(err.Error(), "source kind is empty") {
		t.Fatalf("unexpected empty source error: %v", err)
	}
	var repository *Repository
	if _, err := repository.Load("layer.md"); err == nil || !strings.Contains(err.Error(), "repository is nil") {
		t.Fatalf("unexpected nil repository error: %v", err)
	}
}
