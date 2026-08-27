package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/skill"
)

func renderSkillCatalog(index []skill.SkillMetadata) string {
	lines := make([]string, 0, len(index)+1)
	for _, entry := range index {
		if !entry.Enabled {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s (file: %s)", entry.Name, entry.Description, entry.PathToSkillMD))
	}
	if len(lines) == 0 {
		return "No Skills are currently available."
	}
	return "## Skills\n\n" + strings.Join(lines, "\n")
}

func (session *Session) recordExplicitSkills(ctx context.Context, services *SessionServices, turnID protocol.TurnID, input string) error {
	items, err := explicitSkillItems(services, input)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	return session.AppendItems(ctx, turnID, items...)
}

func explicitSkillItems(services *SessionServices, input string) ([]rollout.RolloutItem, error) {
	if services == nil || services.skills == nil {
		return nil, nil
	}
	documents, err := services.skills.ResolveExplicit(input)
	if err != nil {
		return nil, err
	}
	items := make([]rollout.RolloutItem, 0, len(documents))
	for _, document := range documents {
		injection, err := skill.NewInjection(document)
		if err != nil {
			return nil, err
		}
		content := "<skill>\n<name>" + injection.Name + "</name>\n<path>" + injection.Path + "</path>\n" + injection.Content + "\n</skill>"
		item, err := rollout.NewContextResponseItem(llm.UserMessage(content), rollout.ContextKindExplicitSkill)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
