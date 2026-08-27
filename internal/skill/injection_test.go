package skill

import "testing"

func TestNewInjectionUsesEnabledDocumentIdentity(t *testing.T) {
	injection, err := NewInjection(SkillDocument{SkillMetadata: SkillMetadata{
		Name: "review", PathToSkillMD: "/workspace/review/SKILL.md", Revision: "revision", Enabled: true,
	}, Content: "review instructions"})
	if err != nil {
		t.Fatal(err)
	}
	if injection.Name != "review" || injection.Path != "/workspace/review/SKILL.md" || injection.Revision != "revision" || injection.Content != "review instructions" {
		t.Fatalf("Skill injection = %#v", injection)
	}
}

func TestNewInjectionRejectsDisabledDocument(t *testing.T) {
	if _, err := NewInjection(SkillDocument{SkillMetadata: SkillMetadata{Name: "review", PathToSkillMD: "/review/SKILL.md", Revision: "revision"}, Content: "body"}); err == nil {
		t.Fatal("disabled Skill document became an injection")
	}
}
