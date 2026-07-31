package instruction

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
)

func TestProjectLoaderDiscoversRootToTargetDirectory(t *testing.T) {
	root := newInstructionProjectRoot(t)
	writeProjectInstruction(t, root.Path(), "Root rules")
	writeProjectInstruction(t, filepath.Join(root.Path(), "pkg"), "Package rules")
	writeProjectInstruction(t, filepath.Join(root.Path(), "pkg", "service"), "Service rules")
	writeProjectInstruction(t, filepath.Join(root.Path(), "pkg", "other"), "Sibling rules")
	if err := os.MkdirAll(filepath.Join(root.Path(), "pkg", "service", "internal"), 0o700); err != nil {
		t.Fatalf("create target directory: %v", err)
	}
	loader := newTestProjectLoader(t, root, ProjectLoaderOptions{})

	documents, err := loader.Discover(context.Background(), "./pkg/../pkg/service/internal")
	if err != nil {
		t.Fatalf("discover project instructions: %v", err)
	}
	if loader.Root().Path() != root.Path() || loader.MaxBytes() != DefaultMaxProjectInstructionBytes {
		t.Fatalf("unexpected project loader metadata: root=%q max=%d", loader.Root().Path(), loader.MaxBytes())
	}
	wantScopes := []Scope{ProjectScope(), mustDirectoryScope(t, "pkg"), mustDirectoryScope(t, "pkg/service")}
	wantContents := []string{"Root rules", "Package rules", "Service rules"}
	if len(documents) != len(wantScopes) {
		t.Fatalf("unexpected project instruction count: got %d, want %d (%#v)", len(documents), len(wantScopes), documents)
	}
	for index, document := range documents {
		if document.Source != SourceProject || document.Scope != wantScopes[index] || document.Content != wantContents[index] || len(document.SHA256) != 64 {
			t.Fatalf("unexpected project instruction %d: %#v", index, document)
		}
		if err := document.Validate(); err != nil {
			t.Fatalf("validate project instruction %d: %v", index, err)
		}
	}
	request, _ := NewResolveRequest(root, "pkg/service/internal", TargetDirectory)
	if err := (Resolution{TargetPath: request.TargetPath, TargetKind: request.TargetKind, Documents: documents}).Validate(request); err != nil {
		t.Fatalf("discovered chain violates Resolution contract: %v", err)
	}
}

func TestProjectLoaderHandlesMissingInstructionsAndDirectories(t *testing.T) {
	root := newInstructionProjectRoot(t)
	loader := newTestProjectLoader(t, root, ProjectLoaderOptions{})
	documents, err := loader.Discover(context.Background(), "missing/deeper")
	if err != nil || len(documents) != 0 {
		t.Fatalf("unexpected missing project instruction result: documents=%#v err=%v", documents, err)
	}
	writeProjectInstruction(t, root.Path(), "Root rules")
	documents, err = loader.Discover(context.Background(), ".")
	if err != nil || len(documents) != 1 || documents[0].Scope != ProjectScope() {
		t.Fatalf("unexpected project-root discovery: documents=%#v err=%v", documents, err)
	}
}

func TestProjectLoaderKeepsLogicalScopeForInternalSymlinks(t *testing.T) {
	root := newInstructionProjectRoot(t)
	realDirectory := filepath.Join(root.Path(), "real", "nested")
	writeProjectInstruction(t, realDirectory, "Nested rules")
	if err := os.Symlink(filepath.Join(root.Path(), "real"), filepath.Join(root.Path(), "alias")); err != nil {
		t.Fatalf("create internal directory symlink: %v", err)
	}
	loader := newTestProjectLoader(t, root, ProjectLoaderOptions{})
	documents, err := loader.Discover(context.Background(), "alias/nested")
	if err != nil {
		t.Fatalf("discover through internal symlink: %v", err)
	}
	if len(documents) != 1 || documents[0].Path != filepath.Join(root.Path(), "alias", "nested", InstructionFileName) || documents[0].Scope != mustDirectoryScope(t, "alias/nested") {
		t.Fatalf("internal symlink lost logical provenance or scope: %#v", documents)
	}
	request, _ := NewResolveRequest(root, "alias/nested", TargetDirectory)
	if err := (Resolution{TargetPath: request.TargetPath, TargetKind: request.TargetKind, Documents: documents}).Validate(request); err != nil {
		t.Fatalf("internal symlink chain violates Resolution contract: %v", err)
	}
}

func TestProjectLoaderRejectsSymlinkEscapes(t *testing.T) {
	t.Run("directory escape", func(t *testing.T) {
		root := newInstructionProjectRoot(t)
		external := t.TempDir()
		writeProjectInstruction(t, external, "Outside rules")
		if err := os.Symlink(external, filepath.Join(root.Path(), "escape")); err != nil {
			t.Fatalf("create escaping directory symlink: %v", err)
		}
		loader := newTestProjectLoader(t, root, ProjectLoaderOptions{})
		if documents, err := loader.Discover(context.Background(), "escape"); err == nil || !strings.Contains(err.Error(), "outside project root") || documents != nil {
			t.Fatalf("unexpected directory escape result: documents=%#v err=%v", documents, err)
		}
	})

	t.Run("file escape", func(t *testing.T) {
		root := newInstructionProjectRoot(t)
		external := filepath.Join(t.TempDir(), InstructionFileName)
		if err := os.WriteFile(external, []byte("Outside rules"), 0o600); err != nil {
			t.Fatalf("write external instruction: %v", err)
		}
		if err := os.Symlink(external, filepath.Join(root.Path(), InstructionFileName)); err != nil {
			t.Fatalf("create escaping instruction symlink: %v", err)
		}
		loader := newTestProjectLoader(t, root, ProjectLoaderOptions{})
		if documents, err := loader.Discover(context.Background(), "."); err == nil || !strings.Contains(err.Error(), "outside project root") || documents != nil {
			t.Fatalf("unexpected file escape result: documents=%#v err=%v", documents, err)
		}
	})
}

func TestProjectLoaderEnforcesDocumentBudgetAndValidity(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(string) error
		maxBytes  int64
		wantError string
	}{
		{name: "exact boundary", prepare: writeBytes([]byte("1234")), maxBytes: 4},
		{name: "over boundary", prepare: writeBytes([]byte("12345")), maxBytes: 4, wantError: "exceeds 4 byte limit"},
		{name: "invalid UTF-8", prepare: writeBytes([]byte{0xff}), wantError: "not valid UTF-8"},
		{name: "empty", prepare: writeBytes([]byte(" \n")), wantError: "content is empty"},
		{name: "directory", prepare: func(path string) error { return os.Mkdir(path, 0o700) }, wantError: "not a regular file"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := newInstructionProjectRoot(t)
			if err := test.prepare(filepath.Join(root.Path(), InstructionFileName)); err != nil {
				t.Fatalf("prepare project instruction: %v", err)
			}
			loader := newTestProjectLoader(t, root, ProjectLoaderOptions{MaxBytes: test.maxBytes})
			documents, err := loader.Discover(context.Background(), ".")
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) || documents != nil {
					t.Fatalf("unexpected invalid project instruction result: documents=%#v err=%v", documents, err)
				}
				return
			}
			if err != nil || len(documents) != 1 || documents[0].Content != "1234" {
				t.Fatalf("unexpected boundary result: documents=%#v err=%v", documents, err)
			}
		})
	}
}

func TestProjectLoaderValidatesInputsAndCancellation(t *testing.T) {
	root := newInstructionProjectRoot(t)
	loader := newTestProjectLoader(t, root, ProjectLoaderOptions{})
	for _, target := range []string{"", "../outside", "/absolute", `pkg\\service`} {
		if documents, err := loader.Discover(context.Background(), target); err == nil || documents != nil {
			t.Fatalf("expected invalid target %q to fail: documents=%#v err=%v", target, documents, err)
		}
	}
	fileTarget := filepath.Join(root.Path(), "file")
	if err := os.WriteFile(fileTarget, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file target: %v", err)
	}
	if documents, err := loader.Discover(context.Background(), "file"); err == nil || !strings.Contains(err.Error(), "not a directory") || documents != nil {
		t.Fatalf("unexpected file target result: documents=%#v err=%v", documents, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if documents, err := loader.Discover(ctx, "."); !errors.Is(err, context.Canceled) || documents != nil {
		t.Fatalf("unexpected cancelled discovery: documents=%#v err=%v", documents, err)
	}
	if documents, err := loader.Discover(nil, "."); err == nil || !strings.Contains(err.Error(), "context is nil") || documents != nil {
		t.Fatalf("unexpected nil context result: documents=%#v err=%v", documents, err)
	}
}

func TestNewProjectLoaderValidatesDependencies(t *testing.T) {
	root := newInstructionProjectRoot(t)
	tests := []struct {
		name     string
		root     project.Root
		options  ProjectLoaderOptions
		contains string
	}{
		{name: "empty root", contains: "root is empty"},
		{name: "negative max", root: root, options: ProjectLoaderOptions{MaxBytes: -1}, contains: "cannot be negative"},
		{name: "overflow max", root: root, options: ProjectLoaderOptions{MaxBytes: math.MaxInt64}, contains: "too large"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewProjectLoader(test.root, test.options); err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("unexpected loader construction error: %v", err)
			}
		})
	}
	var loader *ProjectLoader
	if loader.Root().Path() != "" || loader.MaxBytes() != 0 {
		t.Fatal("nil project loader metadata access is not safe")
	}
	if documents, err := loader.Discover(context.Background(), "."); err == nil || !strings.Contains(err.Error(), "loader is nil") || documents != nil {
		t.Fatalf("unexpected nil loader result: documents=%#v err=%v", documents, err)
	}
}

func TestInstructionDirectoriesAreStable(t *testing.T) {
	want := []string{".", "pkg", "pkg/service", "pkg/service/internal"}
	if got := instructionDirectories("pkg/service/internal"); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected instruction directory chain: got %v, want %v", got, want)
	}
	if got := instructionDirectories("."); !reflect.DeepEqual(got, []string{"."}) {
		t.Fatalf("unexpected root instruction directory chain: %v", got)
	}
}

func newTestProjectLoader(t *testing.T, root project.Root, options ProjectLoaderOptions) *ProjectLoader {
	t.Helper()
	loader, err := NewProjectLoader(root, options)
	if err != nil {
		t.Fatalf("create project instruction loader: %v", err)
	}
	return loader
}

func writeProjectInstruction(t *testing.T, directory, content string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("create instruction directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, InstructionFileName), []byte(content), 0o600); err != nil {
		t.Fatalf("write project instruction: %v", err)
	}
}

func writeBytes(content []byte) func(string) error {
	return func(path string) error {
		return os.WriteFile(path, content, 0o600)
	}
}

func mustDirectoryScope(t *testing.T, path string) Scope {
	t.Helper()
	scope, err := NewDirectoryScope(path)
	if err != nil {
		t.Fatalf("create directory scope %q: %v", path, err)
	}
	return scope
}
