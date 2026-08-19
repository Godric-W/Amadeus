package skill

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Godric-W/Amadeus/internal/project"
)

func TestFindScriptAttributesEnabledSkill(t *testing.T) {
	projectPath := t.TempDir()
	scriptPath := filepath.Join(projectPath, ".amadeus", "skills", "review", "scripts", "check.py")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(scriptPath), "..", "SKILL.md"), []byte("---\nname: review\ndescription: review code\n---\nReview workflow\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte("print('ok')\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := project.NewRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, warnings, err := Load("", root, DefaultLoadOptions())
	if err != nil || len(warnings) != 0 {
		t.Fatalf("load catalog: warnings=%v err=%v", warnings, err)
	}
	invocation, found, err := catalog.FindScript("python scripts/check.py", filepath.Join(projectPath, ".amadeus", "skills", "review"))
	if err != nil || !found || invocation.Skill.Name != "review" || invocation.Script.Path != "scripts/check.py" {
		t.Fatalf("unexpected script attribution: found=%v invocation=%#v err=%v", found, invocation, err)
	}
	if _, found, err := catalog.FindScript("python -u scripts/check.py", filepath.Join(projectPath, ".amadeus", "skills", "review")); err != nil || !found {
		t.Fatalf("interpreter flag prevented script attribution: found=%v err=%v", found, err)
	}
	if _, found, err := catalog.FindScript("python -c print('ok')", filepath.Join(projectPath, ".amadeus", "skills", "review")); err != nil || found {
		t.Fatalf("inline interpreter command was attributed as a Skill script: found=%v err=%v", found, err)
	}
}

func TestFindScriptDoesNotAttributeDisabledOrExternalScript(t *testing.T) {
	projectPath := t.TempDir()
	skillRoot := filepath.Join(projectPath, ".amadeus", "skills", "review")
	if err := os.MkdirAll(filepath.Join(skillRoot, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("---\nname: review\ndescription: review code\nallow_implicit_invocation: false\n---\nReview workflow\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(skillRoot, "scripts", "check.sh")
	if err := os.WriteFile(scriptPath, []byte("echo ok\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := project.NewRoot(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, _, err := Load("", root, DefaultLoadOptions())
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := catalog.FindScript("bash scripts/check.sh", skillRoot); err != nil || found {
		t.Fatalf("disabled skill was attributed: found=%v err=%v", found, err)
	}
	external := filepath.Join(projectPath, "check.sh")
	if err := os.WriteFile(external, []byte("echo external\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, found, err := catalog.FindScript("bash check.sh", projectPath); err != nil || found {
		t.Fatalf("external script was attributed: found=%v err=%v", found, err)
	}
}
