package instruction

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
)

func TestWorkspaceResolverSelectsOnlyTargetWorkspaceInstructions(t *testing.T) {
	home, firstPath, secondPath := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(firstPath, InstructionFileName), []byte("first workspace"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secondPath, InstructionFileName), []byte("second workspace"), 0o600); err != nil {
		t.Fatal(err)
	}
	user, err := NewUserLoader(home, UserLoaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := project.NewRoot(firstPath)
	second, _ := project.NewRoot(secondPath)
	resolver, err := NewWorkspaceResolver(user, []project.Root{first, second}, ProjectLoaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	request, resolution, err := resolver.ResolveTarget(context.Background(), secondPath, TargetCommandCWD)
	if err != nil {
		t.Fatal(err)
	}
	if request.Project.Path() != secondPath || len(resolution.Documents) != 1 || resolution.Documents[0].Content != "second workspace" {
		t.Fatalf("unexpected workspace resolution: request=%#v resolution=%#v", request, resolution)
	}
}
