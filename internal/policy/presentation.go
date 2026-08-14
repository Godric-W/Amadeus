package policy

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Godric-W/Amadeus/internal/filechange"
)

func FileApprovalPresentation(operation, path string, change *filechange.Preview) ApprovalPresentation {
	fileName := displayBase(path, "this file")
	title := "Edit file"
	question := "Do you want to make this edit to " + fileName + "?"
	onceDescription := "Apply this edit once"
	switch operation {
	case "write-new":
		title = "Create file"
		question = "Do you want to create " + fileName + "?"
		onceDescription = "Create this file once"
	case "write-overwrite":
		title = "Overwrite file"
		question = "Do you want to overwrite " + fileName + "?"
		onceDescription = "Overwrite this file once"
	}
	directory := filepath.Clean(filepath.Dir(path))
	directoryName := displayBase(directory, directory)
	sessionLabel := "Yes, allow all edits in " + directoryName + "/ during this session"
	return ApprovalPresentation{
		Title: title, Question: question,
		Details: []string{"File: " + path, fmt.Sprintf("Changes: +%d -%d", change.Stats.Insertions, change.Stats.Deletions)},
		Options: []ApprovalOption{
			{ID: "allow", Label: "Yes", Description: onceDescription, Outcome: ApprovalAllow, Scope: ApprovalOnce},
			{ID: "allow-session", Label: sessionLabel, Description: "Allow file edits under " + directory + " until this session ends", Outcome: ApprovalAllow, Scope: ApprovalSession},
			{ID: "deny", Label: "No", Description: "Do not change this file", Outcome: ApprovalDeny, Scope: ApprovalOnce},
		},
	}
}

func CommandApprovalPresentation(command, cwd string) ApprovalPresentation {
	return ApprovalPresentation{
		Title: "Bash command", Question: "Do you want to proceed?",
		Details: []string{"Command: " + command, "Working directory: " + cwd},
		Options: []ApprovalOption{
			{ID: "allow", Label: "Yes", Description: "Run this command once", Outcome: ApprovalAllow, Scope: ApprovalOnce},
			{ID: "allow-session", Label: "Yes, and don't ask again for this exact command during this session", Description: "Only the same command in " + cwd + " will be allowed", Outcome: ApprovalAllow, Scope: ApprovalSession},
			{ID: "deny", Label: "No", Description: "Do not run this command", Outcome: ApprovalDeny, Scope: ApprovalOnce},
		},
	}
}

func WebFetchApprovalPresentation(rawURL, host string) ApprovalPresentation {
	return ApprovalPresentation{
		Title: "Fetch", Question: "Do you want to allow Amadeus to fetch this content?",
		Details: []string{"URL: " + rawURL, "Site: " + host},
		Options: []ApprovalOption{
			{ID: "allow", Label: "Yes", Description: "Fetch this URL once", Outcome: ApprovalAllow, Scope: ApprovalOnce},
			{ID: "allow-session", Label: "Yes, and don't ask again for " + host + " during this session", Description: "Allow web fetches from this site until the session ends", Outcome: ApprovalAllow, Scope: ApprovalSession},
			{ID: "deny", Label: "No", Description: "Do not fetch this content", Outcome: ApprovalDeny, Scope: ApprovalOnce},
		},
	}
}

func ReadDirectoryApprovalPresentation(title, noun, path string) ApprovalPresentation {
	directory := filepath.Clean(filepath.Dir(path))
	directoryName := displayBase(directory, directory)
	return ApprovalPresentation{
		Title: title, Question: "Do you want to allow Amadeus to read this " + noun + "?",
		Details: []string{"File: " + path, "Outside the current workspace"},
		Options: []ApprovalOption{
			{ID: "allow", Label: "Yes", Description: "Read this file once", Outcome: ApprovalAllow, Scope: ApprovalOnce},
			{ID: "allow-session", Label: "Yes, allow reading from " + directoryName + "/ during this session", Description: "Allow reads under " + directory + " until the session ends", Outcome: ApprovalAllow, Scope: ApprovalSession},
			{ID: "deny", Label: "No", Description: "Do not read this file", Outcome: ApprovalDeny, Scope: ApprovalOnce},
		},
	}
}

func MCPToolApprovalPresentation(server, name, description string) ApprovalPresentation {
	details := []string{"Tool: " + name, "MCP server: " + server}
	if description = strings.TrimSpace(description); description != "" {
		details = append(details, "Description: "+description)
	}
	label := server + "/" + name
	return ApprovalPresentation{
		Title: "Use MCP tool", Question: "Do you want to allow Amadeus to use this MCP tool?", Details: details,
		Options: []ApprovalOption{
			{ID: "allow", Label: "Yes", Description: "Use " + label + " once", Outcome: ApprovalAllow, Scope: ApprovalOnce},
			{ID: "allow-session", Label: "Yes, and don't ask again for " + label + " during this session", Description: "Only this tool on this MCP server will be allowed", Outcome: ApprovalAllow, Scope: ApprovalSession},
			{ID: "deny", Label: "No", Description: "Do not use this MCP tool", Outcome: ApprovalDeny, Scope: ApprovalOnce},
		},
	}
}

func MCPResourceApprovalPresentation(server, uri string) ApprovalPresentation {
	return ApprovalPresentation{
		Title: "Read MCP resource", Question: "Do you want to allow Amadeus to read this MCP resource?",
		Details: []string{"Resource: " + uri, "MCP server: " + server},
		Options: []ApprovalOption{
			{ID: "allow", Label: "Yes", Description: "Read this resource once", Outcome: ApprovalAllow, Scope: ApprovalOnce},
			{ID: "allow-session", Label: "Yes, and don't ask again for this resource during this session", Description: "Only this resource on this MCP server will be allowed", Outcome: ApprovalAllow, Scope: ApprovalSession},
			{ID: "deny", Label: "No", Description: "Do not read this resource", Outcome: ApprovalDeny, Scope: ApprovalOnce},
		},
	}
}

func ExternalApprovalPresentation(title, question, sessionLabel string, details ...string) ApprovalPresentation {
	return ApprovalPresentation{
		Title: title, Question: question, Details: append([]string(nil), details...),
		Options: []ApprovalOption{
			{ID: "allow", Label: "Yes", Outcome: ApprovalAllow, Scope: ApprovalOnce},
			{ID: "allow-session", Label: sessionLabel, Outcome: ApprovalAllow, Scope: ApprovalSession},
			{ID: "deny", Label: "No", Outcome: ApprovalDeny, Scope: ApprovalOnce},
		},
	}
}

func displayBase(path, fallback string) string {
	base := strings.TrimSpace(filepath.Base(filepath.Clean(path)))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return fallback
	}
	return base
}
