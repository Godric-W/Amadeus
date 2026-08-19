package skill

import "fmt"

type StaleRevisionError struct {
	Expected string
	Current  string
}

func (err *StaleRevisionError) Error() string {
	if err == nil {
		return "Skill catalog revision is stale"
	}
	return fmt.Sprintf("stale_skill_revision: expected %s, current %s", err.Expected, err.Current)
}

func (err *StaleRevisionError) ToolErrorKind() string { return "stale_skill_revision" }
