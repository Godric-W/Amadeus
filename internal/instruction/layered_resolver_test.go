package instruction

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
)

func TestLayeredResolverAppliesFileDirectoryAndCommandTargets(t *testing.T) {
	amadeusHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(amadeusHome, InstructionFileName), []byte("User rules"), 0o600); err != nil {
		t.Fatalf("write user instructions: %v", err)
	}
	root := newInstructionProjectRoot(t)
	writeProjectInstruction(t, root.Path(), "Project rules")
	writeProjectInstruction(t, filepath.Join(root.Path(), "pkg"), "Package rules")
	writeProjectInstruction(t, filepath.Join(root.Path(), "pkg", "service"), "Service rules")
	resolver := newTestLayeredResolver(t, amadeusHome, root)

	tests := []struct {
		name        string
		targetPath  string
		targetKind  TargetKind
		wantContent []string
	}{
		{name: "file uses parent directory", targetPath: "pkg/service/main.go", targetKind: TargetFile, wantContent: []string{"User rules", "Project rules", "Package rules", "Service rules"}},
		{name: "directory uses itself", targetPath: "pkg/service", targetKind: TargetDirectory, wantContent: []string{"User rules", "Project rules", "Package rules", "Service rules"}},
		{name: "command uses cwd", targetPath: "pkg", targetKind: TargetCommandCWD, wantContent: []string{"User rules", "Project rules", "Package rules"}},
		{name: "root file", targetPath: "main.go", targetKind: TargetFile, wantContent: []string{"User rules", "Project rules"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := NewResolveRequest(root, test.targetPath, test.targetKind)
			if err != nil {
				t.Fatalf("create resolve request: %v", err)
			}
			resolution, err := resolver.Resolve(context.Background(), request)
			if err != nil {
				t.Fatalf("resolve instruction chain: %v", err)
			}
			if resolution.TargetPath != request.TargetPath || resolution.TargetKind != request.TargetKind {
				t.Fatalf("resolution target drifted: %#v", resolution)
			}
			contents := make([]string, len(resolution.Documents))
			for index, document := range resolution.Documents {
				contents[index] = document.Content
			}
			if !reflect.DeepEqual(contents, test.wantContent) {
				t.Fatalf("unexpected instruction priority chain: got %v, want %v", contents, test.wantContent)
			}
			if err := resolution.Validate(request); err != nil {
				t.Fatalf("validate resolved instruction chain: %v", err)
			}
		})
	}
}

func TestLayeredResolverHandlesMissingUserAndSharedRoot(t *testing.T) {
	t.Run("missing user instruction", func(t *testing.T) {
		amadeusHome := t.TempDir()
		root := newInstructionProjectRoot(t)
		writeProjectInstruction(t, root.Path(), "Project rules")
		resolver := newTestLayeredResolver(t, amadeusHome, root)
		request, _ := NewResolveRequest(root, ".", TargetDirectory)
		resolution, err := resolver.Resolve(context.Background(), request)
		if err != nil || len(resolution.Documents) != 1 || resolution.Documents[0].Source != SourceProject {
			t.Fatalf("unexpected missing-user resolution: resolution=%#v err=%v", resolution, err)
		}
	})

	t.Run("same Amadeus and project root deduplicates path", func(t *testing.T) {
		root := newInstructionProjectRoot(t)
		writeProjectInstruction(t, root.Path(), "Shared rules")
		resolver := newTestLayeredResolver(t, root.Path(), root)
		request, _ := NewResolveRequest(root, ".", TargetDirectory)
		resolution, err := resolver.Resolve(context.Background(), request)
		if err != nil || len(resolution.Documents) != 1 || resolution.Documents[0].Source != SourceProject || resolution.Documents[0].Scope != ProjectScope() {
			t.Fatalf("unexpected shared-root resolution: resolution=%#v err=%v", resolution, err)
		}
	})
}

func TestLayeredResolverRejectsMismatchedProjectAndLoaderErrors(t *testing.T) {
	amadeusHome := t.TempDir()
	root := newInstructionProjectRoot(t)
	resolver := newTestLayeredResolver(t, amadeusHome, root)
	otherRoot := newInstructionProjectRoot(t)
	request, _ := NewResolveRequest(otherRoot, ".", TargetDirectory)
	if resolution, err := resolver.Resolve(context.Background(), request); err == nil || !strings.Contains(err.Error(), "does not match request") || len(resolution.Documents) != 0 {
		t.Fatalf("unexpected mismatched-project result: resolution=%#v err=%v", resolution, err)
	}

	if err := os.WriteFile(filepath.Join(amadeusHome, InstructionFileName), []byte{0xff}, 0o600); err != nil {
		t.Fatalf("write invalid user instruction: %v", err)
	}
	request, _ = NewResolveRequest(root, ".", TargetDirectory)
	if resolution, err := resolver.Resolve(context.Background(), request); err == nil || !strings.Contains(err.Error(), "load user instructions") || len(resolution.Documents) != 0 {
		t.Fatalf("unexpected user-loader failure: resolution=%#v err=%v", resolution, err)
	}
}

func TestLayeredResolverHonorsCancellationAndConstructorBoundaries(t *testing.T) {
	amadeusHome := t.TempDir()
	root := newInstructionProjectRoot(t)
	userLoader := newTestUserLoader(t, amadeusHome, UserLoaderOptions{})
	projectLoader := newTestProjectLoader(t, root, ProjectLoaderOptions{})
	if _, err := NewLayeredResolver(nil, projectLoader); err == nil || !strings.Contains(err.Error(), "user loader is nil") {
		t.Fatalf("unexpected nil user loader error: %v", err)
	}
	if _, err := NewLayeredResolver(userLoader, nil); err == nil || !strings.Contains(err.Error(), "project loader is nil") {
		t.Fatalf("unexpected nil project loader error: %v", err)
	}
	resolver, err := NewLayeredResolver(userLoader, projectLoader)
	if err != nil {
		t.Fatalf("create layered resolver: %v", err)
	}
	request, _ := NewResolveRequest(root, ".", TargetDirectory)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if resolution, err := resolver.Resolve(ctx, request); !errors.Is(err, context.Canceled) || len(resolution.Documents) != 0 {
		t.Fatalf("unexpected cancelled resolution: resolution=%#v err=%v", resolution, err)
	}
	var nilResolver *LayeredResolver
	if resolution, err := nilResolver.Resolve(context.Background(), request); err == nil || !strings.Contains(err.Error(), "Resolver is nil") || len(resolution.Documents) != 0 {
		t.Fatalf("unexpected nil resolver result: resolution=%#v err=%v", resolution, err)
	}
}

func TestInstructionTargetDirectoryMapping(t *testing.T) {
	root := newInstructionProjectRoot(t)
	tests := []struct {
		path string
		kind TargetKind
		want string
	}{
		{path: "pkg/file.go", kind: TargetFile, want: "pkg"},
		{path: "file.go", kind: TargetFile, want: "."},
		{path: "pkg", kind: TargetDirectory, want: "pkg"},
		{path: "pkg", kind: TargetCommandCWD, want: "pkg"},
	}
	for _, test := range tests {
		request, err := NewResolveRequest(root, test.path, test.kind)
		if err != nil {
			t.Fatalf("create resolve request: %v", err)
		}
		if got := instructionTargetDirectory(request); got != test.want {
			t.Fatalf("unexpected target directory for %q/%q: got %q, want %q", test.kind, test.path, got, test.want)
		}
	}
}

func newTestLayeredResolver(t *testing.T, amadeusHome string, root project.Root) *LayeredResolver {
	t.Helper()
	userLoader := newTestUserLoader(t, amadeusHome, UserLoaderOptions{})
	projectLoader := newTestProjectLoader(t, root, ProjectLoaderOptions{})
	resolver, err := NewLayeredResolver(userLoader, projectLoader)
	if err != nil {
		t.Fatalf("create layered instruction resolver: %v", err)
	}
	return resolver
}
