package skill

import (
	"errors"
	"strings"
)

type SkillInjection struct {
	Name     string
	Path     string
	Revision string
	Content  string
}

func NewInjection(document SkillDocument) (SkillInjection, error) {
	injection := SkillInjection{
		Name: strings.TrimSpace(document.Name), Path: strings.TrimSpace(document.PathToSkillMD),
		Revision: strings.TrimSpace(document.Revision), Content: strings.TrimSpace(document.Content),
	}
	if !document.Enabled || injection.Name == "" || injection.Path == "" || injection.Revision == "" || injection.Content == "" {
		return SkillInjection{}, errors.New("Skill injection is incomplete")
	}
	return injection, nil
}
