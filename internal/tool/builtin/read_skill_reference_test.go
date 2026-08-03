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

func TestReadSkillReferenceReadsBoundedTextWithinSkillRoot(t *testing.T) {
	catalog := testReferenceCatalog(t, map[string][]byte{"guide.md": []byte("one\ntwo\nthree\n")})
	reader, err := NewReadSkillReference(catalog, ReadSkillReferenceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := reader.Execute(context.Background(), json.RawMessage(`{"skill":"review","path":"guide.md","offset":1,"limit":1}`))
	if err != nil {
		t.Fatalf("read Skill reference: %v", err)
	}
	if result.Text != "two\n" || !result.Partial || result.Metadata["total_lines"] != 3 || result.Metadata["source"] != "project" {
		t.Fatalf("unexpected Skill reference result: %#v", result)
	}
}

func TestReadSkillReferenceRejectsEscapesBinaryAndOversizedFiles(t *testing.T) {
	catalog := testReferenceCatalog(t, map[string][]byte{"binary.dat": {0, 1}, "large.txt": []byte("0123456789")})
	reader, err := NewReadSkillReference(catalog, ReadSkillReferenceOptions{MaxBytes: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{
		`{"skill":"review","path":"../SKILL.md"}`,
		`{"skill":"review","path":"binary.dat"}`,
		`{"skill":"review","path":"large.txt"}`,
	} {
		if _, err := reader.Execute(context.Background(), json.RawMessage(input)); err == nil {
			t.Fatalf("unsafe reference input was accepted: %s", input)
		}
	}
}

func TestReadSkillReferenceRejectsEscapingSymlink(t *testing.T) {
	catalog := testReferenceCatalog(t, nil)
	value, ok := catalog.Lookup("review")
	if !ok {
		t.Fatal("missing test Skill")
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(value.Root, "references", "escape.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	reader, err := NewReadSkillReference(catalog, ReadSkillReferenceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = reader.Execute(context.Background(), json.RawMessage(`{"skill":"review","path":"escape.txt"}`))
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("escaping symlink was accepted: %v", err)
	}
}

func testReferenceCatalog(t *testing.T, references map[string][]byte) *skill.Catalog {
	t.Helper()
	projectPath := t.TempDir()
	skillRoot := filepath.Join(projectPath, ".amadeus", "skills", "review")
	if err := os.MkdirAll(filepath.Join(skillRoot, "references"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("---\nname: review\ndescription: Review code\n---\nUse concise findings.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, content := range references {
		if err := os.WriteFile(filepath.Join(skillRoot, "references", name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := project.NewRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, warnings, err := skill.Load("", root, skill.DefaultLoadOptions())
	if err != nil || len(warnings) != 0 {
		t.Fatalf("load catalog: warnings=%v err=%v", warnings, err)
	}
	return catalog
}
