package policy

import (
	"path/filepath"
	"strings"
)

func FileApprovalPresentation(operation, path string, change any) ApprovalPresentation {
	title := "Edit file"
	question := "Do you want to make this edit?"
	switch operation {
	case "write-new":
		title = "Create file"
		question = "Do you want to create this file?"
	case "write-overwrite":
		title = "Overwrite file"
		question = "Do you want to overwrite this file?"
	}
	directory := filepath.Dir(path)
	label := "Yes, allow all edits during this session"
	if directory != "." && strings.TrimSpace(directory) != "" {
		label = "Yes, allow all edits in " + directory + "/ during this session"
	}
	return ApprovalPresentation{
		Title: title, Question: question,
		Details: []string{path, "Diff: " + formatPresentationValue(change)},
		Options: []ApprovalOption{
			{ID: "allow", Label: "Yes", Outcome: ApprovalAllow, Scope: ApprovalOnce},
			{ID: "allow-session", Label: label, Outcome: ApprovalAllow, Scope: ApprovalSession},
			{ID: "deny", Label: "No", Outcome: ApprovalDeny, Scope: ApprovalOnce},
		},
	}
}

func CommandApprovalPresentation(command, cwd string) ApprovalPresentation {
	return ApprovalPresentation{
		Title: "Bash command", Question: "Do you want to proceed?",
		Details: []string{command, "cwd: " + cwd},
		Options: []ApprovalOption{
			{ID: "allow", Label: "Yes", Outcome: ApprovalAllow, Scope: ApprovalOnce},
			{ID: "allow-session", Label: "Yes, and don't ask again for this command in this directory", Outcome: ApprovalAllow, Scope: ApprovalSession},
			{ID: "deny", Label: "No", Outcome: ApprovalDeny, Scope: ApprovalOnce},
		},
	}
}

func formatPresentationValue(value any) string {
	if value == nil {
		return "none"
	}
	return strings.TrimSpace(strings.ReplaceAll(strings.TrimSpace(toPresentationString(value)), "\n", " "))
}

func toPresentationString(value any) string {
	if stringValue, ok := value.(string); ok {
		return stringValue
	}
	return "structured change"
}
