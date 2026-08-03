package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/skill"
	"github.com/Godric-W/Amadeus/internal/tool"
)

func TestLoadSkillQueuesContentForNextRequest(t *testing.T) {
	projectPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectPath, ".amadeus", "skills", "review"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, ".amadeus", "skills", "review", "SKILL.md"), []byte("---\nname: review\ndescription: Review code\n---\nUse concise findings.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := project.NewRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, warnings, err := skill.Load("", root, skill.DefaultLoadOptions())
	if err != nil || len(warnings) != 0 {
		t.Fatalf("load catalog: warnings=%v err=%v", warnings, err)
	}
	buffer, err := skill.NewContextBuffer(skill.BufferOptions{})
	if err != nil {
		t.Fatal(err)
	}
	loader, err := NewLoadSkill(catalog, buffer)
	if err != nil {
		t.Fatal(err)
	}
	result, err := loader.Execute(context.Background(), json.RawMessage(`{"name":"review"}`))
	if err != nil || result.Metadata["source"] != "project" || buffer.Len() != 1 {
		t.Fatalf("load skill result=%#v err=%v buffer=%d", result, err, buffer.Len())
	}
	if values := buffer.Consume(); len(values) != 1 || values[0].Content != "Use concise findings." {
		t.Fatalf("unexpected buffered skills: %#v", values)
	}
	if loader.Spec().SideEffect != tool.SideEffectRead || !loader.Spec().ParallelSafe {
		t.Fatalf("unexpected load_skill spec: %#v", loader.Spec())
	}
}
