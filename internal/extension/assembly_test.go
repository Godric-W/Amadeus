package extension

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
)

func TestAssemblyLoadsStableSessionScopedRevisions(t *testing.T) {
	userRoot := t.TempDir()
	projectPath := t.TempDir()
	writeAssemblySkill(t, filepath.Join(userRoot, "skills", "review", "SKILL.md"), "first body")
	root, err := project.NewRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}

	first, err := Assemble(userRoot, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if first.SkillCatalog() == nil || first.SkillCatalog().Len() != 1 || len(first.SkillRevision()) != 64 || len(first.MCPRevision()) != 64 || first.MCPRuntime() == nil {
		t.Fatalf("incomplete extension runtime: skills=%v skill_revision=%q mcp_revision=%q", first.SkillCatalog(), first.SkillRevision(), first.MCPRevision())
	}
	injections, err := first.ResolveSkillInjections("use $review and $review")
	if err != nil {
		t.Fatal(err)
	}
	if len(injections) != 1 || injections[0].Name != "review" || injections[0].Path == "" || len(injections[0].Revision) != 64 {
		t.Fatalf("unexpected explicit Skill injections: %#v", injections)
	}
	if _, err := first.ResolveSkillInjections("use $missing"); err == nil {
		t.Fatal("missing explicit Skill was accepted")
	}

	second, err := Assemble(userRoot, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if first.SkillRevision() != second.SkillRevision() || first.MCPRevision() != second.MCPRevision() {
		t.Fatalf("unchanged extension revisions are unstable: first=%q/%q second=%q/%q", first.SkillRevision(), first.MCPRevision(), second.SkillRevision(), second.MCPRevision())
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("extension close is not idempotent: %v", err)
	}

	originalRevision := first.SkillRevision()
	writeAssemblySkill(t, filepath.Join(userRoot, "skills", "review", "SKILL.md"), "changed body")
	changed, err := Assemble(userRoot, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer changed.Close()
	if changed.SkillRevision() == originalRevision || first.SkillRevision() == originalRevision {
		t.Fatal("Skill revision did not change with Skill content")
	}
}

func writeAssemblySkill(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: review\ndescription: review code\n---\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
