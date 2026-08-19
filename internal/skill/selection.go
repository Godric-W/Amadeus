package skill

import (
	"fmt"
	"regexp"
	"strings"
)

var explicitSkillPattern = regexp.MustCompile(`\$([A-Za-z0-9][A-Za-z0-9._-]{0,63})`)

func (catalog *SkillCatalog) ResolveExplicit(task string) ([]SkillDocument, error) {
	if catalog == nil {
		return nil, nil
	}
	matches := explicitSkillPattern.FindAllStringSubmatch(task, -1)
	documents := make([]SkillDocument, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		name := strings.TrimSpace(match[1])
		if _, exists := seen[name]; exists {
			continue
		}
		document, err := catalog.LoadDocument(name)
		if err != nil {
			return nil, fmt.Errorf("resolve explicit Skill %q: %w", name, err)
		}
		documents = append(documents, document)
		seen[name] = struct{}{}
	}
	return documents, nil
}
