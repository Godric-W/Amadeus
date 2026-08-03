package skill

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
)

func TestLoadMergesUserAndProjectSkillsWithProjectOverride(t *testing.T) {
	userRoot := t.TempDir()
	projectPath := t.TempDir()
	writeSkill(t, filepath.Join(userRoot, "skills", "review", "SKILL.md"), "review", "user review", "user body")
	writeSkill(t, filepath.Join(userRoot, "skills", "format", "SKILL.md"), "format", "format code", "format body")
	writeSkill(t, filepath.Join(projectPath, ".amadeus", "skills", "review", "SKILL.md"), "review", "project review", "project body")
	root, err := project.NewRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, warnings, err := Load(userRoot, root, DefaultLoadOptions())
	if err != nil || len(warnings) != 0 || catalog.Len() != 2 {
		t.Fatalf("load catalog = %#v, warnings=%v, err=%v", catalog, warnings, err)
	}
	review, ok := catalog.Lookup("review")
	if !ok || review.Source != SourceProject || review.Description != "project review" || review.Content != "project body" {
		t.Fatalf("project override was not retained: %#v", review)
	}
	if got, want := catalog.Index(), []IndexEntry{{Name: "format", Description: "format code", Source: SourceUser}, {Name: "review", Description: "project review", Source: SourceProject}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("skill index = %#v, want %#v", got, want)
	}
}

func TestLoadSkipsInvalidSkillsAndSymlinkEscapes(t *testing.T) {
	projectPath := t.TempDir()
	outside := t.TempDir()
	writeSkill(t, filepath.Join(projectPath, ".amadeus", "skills", "valid", "SKILL.md"), "valid", "valid skill", "body")
	if err := os.MkdirAll(filepath.Join(projectPath, ".amadeus", "skills", "invalid"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, ".amadeus", "skills", "invalid", "SKILL.md"), []byte("name: invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(outside, "escape", "SKILL.md"), "escape", "escape", "body")
	if err := os.Symlink(filepath.Join(outside, "escape"), filepath.Join(projectPath, ".amadeus", "skills", "escape")); err != nil {
		t.Fatal(err)
	}
	root, err := project.NewRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, warnings, err := Load("", root, DefaultLoadOptions())
	if err != nil || catalog.Len() != 1 || len(warnings) != 2 {
		t.Fatalf("load invalid catalog = %#v, warnings=%v, err=%v", catalog, warnings, err)
	}
	if _, ok := catalog.Lookup("valid"); !ok {
		t.Fatal("valid skill was discarded with invalid entries")
	}
}

func TestLoadRejectsInvalidMetadataAndIndexBudget(t *testing.T) {
	projectPath := t.TempDir()
	writeSkill(t, filepath.Join(projectPath, ".amadeus", "skills", "bad-name", "SKILL.md"), "Bad", "description", "body")
	root, err := project.NewRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, warnings, err := Load("", root, LoadOptions{MaxIndexBytes: 1})
	if err != nil || catalog.Len() != 0 || len(warnings) != 1 {
		t.Fatalf("invalid metadata handling: catalog=%#v warnings=%v err=%v", catalog, warnings, err)
	}
	writeSkill(t, filepath.Join(projectPath, ".amadeus", "skills", "good", "SKILL.md"), "good", "description", "body")
	if _, _, err := Load("", root, LoadOptions{MaxIndexBytes: 1}); err == nil || !strings.Contains(err.Error(), "index exceeds") {
		t.Fatalf("expected index budget error, got %v", err)
	}
}

func writeSkill(t *testing.T, path, name, description, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
