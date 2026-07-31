package instruction

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
)

type fakeResolver struct {
	resolution Resolution
	err        error
}

func (resolver fakeResolver) Resolve(context.Context, ResolveRequest) (Resolution, error) {
	return resolver.resolution.Clone(), resolver.err
}

func TestResolveRequestUsesNormalizedProjectRelativeTarget(t *testing.T) {
	root := newInstructionProjectRoot(t)
	request, err := NewResolveRequest(root, "./pkg/../cmd/main.go", TargetFile)
	if err != nil {
		t.Fatalf("create instruction resolve request: %v", err)
	}
	if request.Project.Path() != root.Path() || request.TargetPath != "cmd/main.go" || request.TargetKind != TargetFile {
		t.Fatalf("unexpected resolve request: %#v", request)
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("validate resolve request: %v", err)
	}
	for _, target := range []string{"", "../outside", "/absolute", `pkg\\file.go`} {
		if _, err := NewResolveRequest(root, target, TargetFile); err == nil {
			t.Fatalf("expected invalid target %q to fail", target)
		}
	}
	if err := (ResolveRequest{Project: root, TargetPath: "./pkg", TargetKind: TargetDirectory}).Validate(); err == nil || !strings.Contains(err.Error(), "not normalized") {
		t.Fatalf("unexpected non-normalized request error: %v", err)
	}
	if _, err := NewResolveRequest(root, ".", "resource"); err == nil || !strings.Contains(err.Error(), "target kind") {
		t.Fatalf("unexpected invalid target kind error: %v", err)
	}
}

func TestResolutionValidatesBroadToSpecificInstructionChain(t *testing.T) {
	root := newInstructionProjectRoot(t)
	request, err := NewResolveRequest(root, "pkg/service/file.go", TargetFile)
	if err != nil {
		t.Fatalf("create resolve request: %v", err)
	}
	user := newInstructionDocument(t, SourceUser, filepath.Join(t.TempDir(), "AGENTS.md"), UserScope(), "User rules")
	projectDocument := newInstructionDocument(t, SourceProject, filepath.Join(root.Path(), "AGENTS.md"), ProjectScope(), "Project rules")
	pkgScope, _ := NewDirectoryScope("pkg")
	pkgDocument := newInstructionDocument(t, SourceProject, filepath.Join(root.Path(), "pkg", "AGENTS.md"), pkgScope, "Package rules")
	serviceScope, _ := NewDirectoryScope("pkg/service")
	serviceDocument := newInstructionDocument(t, SourceProject, filepath.Join(root.Path(), "pkg", "service", "AGENTS.md"), serviceScope, "Service rules")
	resolution := Resolution{
		TargetPath: request.TargetPath,
		TargetKind: request.TargetKind,
		Documents:  []InstructionDocument{user, projectDocument, pkgDocument, serviceDocument},
	}
	if err := resolution.Validate(request); err != nil {
		t.Fatalf("validate instruction resolution: %v", err)
	}
	clone := resolution.Clone()
	clone.Documents[0].Content = "mutated"
	if resolution.Documents[0].Content == "mutated" {
		t.Fatal("instruction resolution clone shares document slice storage")
	}

	var resolver Resolver = fakeResolver{resolution: resolution}
	resolved, err := resolver.Resolve(context.Background(), request)
	if err != nil || !reflect.DeepEqual(resolved, resolution) {
		t.Fatalf("unexpected Resolver Port result: resolution=%#v err=%v", resolved, err)
	}
}

func TestResolutionRejectsInvalidChains(t *testing.T) {
	root := newInstructionProjectRoot(t)
	request, _ := NewResolveRequest(root, "pkg/file.go", TargetFile)
	user := newInstructionDocument(t, SourceUser, filepath.Join(t.TempDir(), "AGENTS.md"), UserScope(), "User rules")
	projectDocument := newInstructionDocument(t, SourceProject, filepath.Join(root.Path(), "AGENTS.md"), ProjectScope(), "Project rules")
	pkgScope, _ := NewDirectoryScope("pkg")
	pkgDocument := newInstructionDocument(t, SourceProject, filepath.Join(root.Path(), "pkg", "AGENTS.md"), pkgScope, "Package rules")
	otherScope, _ := NewDirectoryScope("other")
	otherDocument := newInstructionDocument(t, SourceProject, filepath.Join(root.Path(), "other", "AGENTS.md"), otherScope, "Other rules")
	fileNamedScope, _ := NewDirectoryScope("pkg/file.go")
	fileNamedDocument := newInstructionDocument(t, SourceProject, filepath.Join(root.Path(), "pkg", "file.go", "AGENTS.md"), fileNamedScope, "Wrong file scope")
	outOfRoot := newInstructionDocument(t, SourceProject, filepath.Join(t.TempDir(), "AGENTS.md"), ProjectScope(), "Outside rules")
	wrongScopeDocument := newInstructionDocument(t, SourceProject, filepath.Join(root.Path(), "pkg", "AGENTS.md"), ProjectScope(), "Wrong scope")

	tests := []struct {
		name       string
		resolution Resolution
		contains   string
	}{
		{name: "target mismatch", resolution: Resolution{TargetPath: ".", TargetKind: request.TargetKind}, contains: "does not match request"},
		{name: "target kind mismatch", resolution: Resolution{TargetPath: request.TargetPath, TargetKind: TargetDirectory}, contains: "target kind"},
		{name: "specificity order", resolution: Resolution{TargetPath: request.TargetPath, TargetKind: request.TargetKind, Documents: []InstructionDocument{pkgDocument, projectDocument}}, contains: "broadest to most specific"},
		{name: "non-applicable scope", resolution: Resolution{TargetPath: request.TargetPath, TargetKind: request.TargetKind, Documents: []InstructionDocument{otherDocument}}, contains: "does not apply"},
		{name: "file target uses parent scope", resolution: Resolution{TargetPath: request.TargetPath, TargetKind: request.TargetKind, Documents: []InstructionDocument{fileNamedDocument}}, contains: "does not apply"},
		{name: "duplicate path", resolution: Resolution{TargetPath: request.TargetPath, TargetKind: request.TargetKind, Documents: []InstructionDocument{user, user}}, contains: "path"},
		{name: "duplicate scope", resolution: Resolution{TargetPath: request.TargetPath, TargetKind: request.TargetKind, Documents: []InstructionDocument{user, newInstructionDocument(t, SourceUser, filepath.Join(t.TempDir(), "AGENTS.md"), UserScope(), "More user rules")}}, contains: "scope"},
		{name: "outside project", resolution: Resolution{TargetPath: request.TargetPath, TargetKind: request.TargetKind, Documents: []InstructionDocument{outOfRoot}}, contains: "outside project root"},
		{name: "path scope mismatch", resolution: Resolution{TargetPath: request.TargetPath, TargetKind: request.TargetKind, Documents: []InstructionDocument{wrongScopeDocument}}, contains: "does not match scope"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.resolution.Validate(request); err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("unexpected resolution validation error: %v", err)
			}
		})
	}
}

func TestResolutionAllowsNoInstructionDocuments(t *testing.T) {
	root := newInstructionProjectRoot(t)
	request, _ := NewResolveRequest(root, ".", TargetDirectory)
	resolution := Resolution{TargetPath: ".", TargetKind: TargetDirectory}
	if err := resolution.Validate(request); err != nil {
		t.Fatalf("empty instruction resolution should be valid: %v", err)
	}
	resolverErr := errors.New("resolver failed")
	var resolver Resolver = fakeResolver{err: resolverErr}
	if _, err := resolver.Resolve(context.Background(), request); !errors.Is(err, resolverErr) {
		t.Fatalf("unexpected Resolver Port error: %v", err)
	}
}

func newInstructionProjectRoot(t *testing.T) project.Root {
	t.Helper()
	root, err := project.NewRoot(t.TempDir())
	if err != nil {
		t.Fatalf("create instruction project root: %v", err)
	}
	return root
}

func newInstructionDocument(t *testing.T, source Source, path string, scope Scope, content string) InstructionDocument {
	t.Helper()
	document, err := NewInstructionDocument(source, path, scope, content)
	if err != nil {
		t.Fatalf("create instruction document: %v", err)
	}
	return document
}
