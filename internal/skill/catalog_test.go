package skill

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
	if !ok || review.Source != SourceProject || review.Description != "project review" || review.PathToSkillMD == "" || review.Size == 0 || len(review.Revision) != 64 {
		t.Fatalf("project override was not retained: %#v", review)
	}
	loaded, err := catalog.LoadDocument("review")
	if err != nil || loaded.Content != "project body" {
		t.Fatalf("project Skill body was not loaded on demand: value=%#v err=%v", loaded, err)
	}
	index := catalog.Index()
	if got, want := []string{index[0].Name, index[1].Name}, []string{"format", "review"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("skill index order = %#v, want %#v", got, want)
	}
	for _, entry := range index {
		if entry.PathToSkillMD == "" || entry.Size == 0 || len(entry.Revision) != 64 {
			t.Fatalf("skill metadata index is incomplete: %#v", entry)
		}
	}
}

func TestCatalogLoadsCurrentSkillBodyWithoutCachingContent(t *testing.T) {
	projectPath := t.TempDir()
	path := filepath.Join(projectPath, ".amadeus", "skills", "review", "SKILL.md")
	writeSkill(t, path, "review", "review", "first body")
	root, err := project.NewRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, warnings, err := Load("", root, DefaultLoadOptions())
	if err != nil || len(warnings) != 0 {
		t.Fatalf("load Skill catalog: warnings=%v err=%v", warnings, err)
	}
	metadata, ok := catalog.Lookup("review")
	if !ok || metadata.PathToSkillMD == "" {
		t.Fatalf("catalog retained Skill body: %#v", metadata)
	}
	writeSkill(t, path, "review", "review", "second body")
	loaded, err := catalog.LoadDocument("review")
	if err != nil || loaded.Content != "second body" || loaded.Revision == metadata.Revision {
		t.Fatalf("on-demand Skill load did not observe current content: value=%#v err=%v", loaded, err)
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

func TestCheckedInSkillExampleLoadsOnDemand(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve Skill test path")
	}
	content, err := os.ReadFile(filepath.Join(filepath.Dir(current), "..", "..", "configs", "skills", "review", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	projectPath := t.TempDir()
	path := filepath.Join(projectPath, ".amadeus", "skills", "review", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := project.NewRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, warnings, err := Load("", root, DefaultLoadOptions())
	if err != nil || len(warnings) != 0 {
		t.Fatalf("load checked-in Skill example: warnings=%v err=%v", warnings, err)
	}
	metadata, ok := catalog.Lookup("review")
	if !ok || metadata.PathToSkillMD == "" {
		t.Fatalf("checked-in Skill example was not metadata-only: %#v", metadata)
	}
	loaded, err := catalog.LoadDocument("review")
	if err != nil || !strings.Contains(loaded.Content, "Review Workflow") {
		t.Fatalf("load checked-in Skill body: value=%#v err=%v", loaded, err)
	}
}

func TestSkillRevisionTracksPolicyAndDisabledResourceChanges(t *testing.T) {
	projectPath := t.TempDir()
	skillPath := filepath.Join(projectPath, ".amadeus", "skills", "review", "SKILL.md")
	writeSkill(t, skillPath, "review", "review code", "first body")
	root, err := project.NewRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, _, err := Load("", root, DefaultLoadOptions())
	if err != nil {
		t.Fatal(err)
	}
	first, err := catalog.Revision()
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.SetEnabled("review", false); err != nil {
		t.Fatal(err)
	}
	second, err := catalog.Revision()
	if err != nil || first == second {
		t.Fatalf("policy change did not create a new Skill revision: first=%s second=%s err=%v", first, second, err)
	}
	if _, err := catalog.LoadDocument("review"); err == nil {
		t.Fatal("disabled Skill was loadable")
	}
	writeSkill(t, skillPath, "review", "review code", "changed while disabled")
	third, err := catalog.Revision()
	if err != nil || second == third {
		t.Fatalf("disabled resource change did not create a new Skill revision: second=%s third=%s err=%v", second, third, err)
	}
}

func TestCatalogPersistsDisabledProjectSkill(t *testing.T) {
	projectDirectory := t.TempDir()
	writeSkill(t, filepath.Join(projectDirectory, ".amadeus", "skills", "review", "SKILL.md"), "review", "Review code", "Review instructions")
	root, err := project.NewRoot(projectDirectory)
	if err != nil {
		t.Fatal(err)
	}
	catalog, warnings, err := Load("", root, DefaultLoadOptions())
	if err != nil || len(warnings) != 0 {
		t.Fatalf("load catalog: warnings=%v err=%v", warnings, err)
	}
	if err := catalog.SetEnabled("review", false); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.LoadDocument("review"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled Skill loaded: %v", err)
	}
	reloaded, warnings, err := Load("", root, DefaultLoadOptions())
	if err != nil || len(warnings) != 0 {
		t.Fatalf("reload catalog: warnings=%v err=%v", warnings, err)
	}
	entries := reloaded.Index()
	if len(entries) != 1 || entries[0].Enabled {
		t.Fatalf("disabled state was not persisted: %#v", entries)
	}
	if err := reloaded.SetEnabled("review", true); err != nil {
		t.Fatal(err)
	}
	if _, err := reloaded.LoadDocument("review"); err != nil {
		t.Fatalf("re-enabled Skill did not load: %v", err)
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
