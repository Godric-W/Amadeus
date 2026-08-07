package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/skill"
)

func TestReadSkillReturnsProjectBodyAndBoundedReference(t *testing.T) {
	userRoot := t.TempDir()
	projectPath := t.TempDir()
	writeSkillFixture(t, filepath.Join(userRoot, "skills", "review"), "User review", "USER BODY", "")
	writeSkillFixture(t, filepath.Join(projectPath, ".amadeus", "skills", "review"), "Project review", "PROJECT BODY", "one\ntwo\nthree\n")
	root, err := project.NewRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, warnings, err := skill.Load(userRoot, root, skill.DefaultLoadOptions())
	if err != nil || len(warnings) != 0 {
		t.Fatalf("load Skill catalog: warnings=%v err=%v", warnings, err)
	}
	reader, err := NewReadSkill(catalog, ReadSkillOptions{MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	body, err := executePreparedTool(t, context.Background(), reader, json.RawMessage(`{"name":"review"}`))
	if err != nil || body.ToolName != "read_skill" || !strings.Contains(body.Text, "PROJECT BODY") || strings.Contains(body.Text, "USER BODY") || body.Metadata["source"] != "project" {
		t.Fatalf("unexpected Skill body: %#v err=%v", body, err)
	}
	references, ok := body.Metadata["references"].([]string)
	if !ok || len(references) != 1 || references[0] != "guide.md" {
		t.Fatalf("unexpected Skill references: %#v", body.Metadata)
	}
	reference, err := executePreparedTool(t, context.Background(), reader, json.RawMessage(`{"name":"review","path":"guide.md","line":2,"limit":1}`))
	if err != nil || reference.Text != "L2:two\n" || !reference.Partial || reference.Metadata["next_line"] != 3 {
		t.Fatalf("unexpected Skill reference: %#v err=%v", reference, err)
	}
	if _, err := executePreparedTool(t, context.Background(), reader, json.RawMessage(`{"name":"review","path":"../SKILL.md"}`)); err == nil {
		t.Fatal("Skill reference path escape was accepted")
	}
}

func writeSkillFixture(t *testing.T, root, description, body, reference string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: review\ndescription: " + description + "\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if reference != "" {
		if err := os.WriteFile(filepath.Join(root, "references", "guide.md"), []byte(reference), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
