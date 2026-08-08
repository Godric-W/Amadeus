package tui

import (
	"fmt"
	"strings"
)

type CollaborationMode string

const (
	CollaborationExecute CollaborationMode = "execute"
	CollaborationPlan    CollaborationMode = "plan"
)

type TaskSubmission struct {
	Content string
	Mode    CollaborationMode
}

type SlashCommand string

const (
	SlashResume  SlashCommand = "resume"
	SlashSkills  SlashCommand = "skills"
	SlashRename  SlashCommand = "rename"
	SlashDelete  SlashCommand = "delete"
	SlashCompact SlashCommand = "compact"
	SlashPlan    SlashCommand = "plan"
	SlashCopy    SlashCommand = "copy"
	SlashStatus  SlashCommand = "status"
	SlashMCP     SlashCommand = "mcp"
	SlashClear   SlashCommand = "clear"
	SlashExit    SlashCommand = "exit"
)

type SlashCommandSpec struct {
	Command            SlashCommand
	Description        string
	SupportsInlineArgs bool
	AvailableDuringRun bool
	Destructive        bool
}

var slashCommandCatalog = []SlashCommandSpec{
	{Command: SlashResume, Description: "resume a saved chat", SupportsInlineArgs: true},
	{Command: SlashSkills, Description: "use skills to improve how Amadeus performs specific tasks", AvailableDuringRun: true},
	{Command: SlashRename, Description: "rename the current session", SupportsInlineArgs: true},
	{Command: SlashDelete, Description: "permanently delete this session and exit", Destructive: true},
	{Command: SlashCompact, Description: "summarize conversation to prevent hitting the context limit"},
	{Command: SlashPlan, Description: "switch to Plan mode"},
	{Command: SlashCopy, Description: "copy last response as markdown", AvailableDuringRun: true},
	{Command: SlashStatus, Description: "show current session configuration and token usage", AvailableDuringRun: true},
	{Command: SlashMCP, Description: "list configured MCP tools; use /mcp verbose for details", SupportsInlineArgs: true, AvailableDuringRun: true},
	{Command: SlashClear, Description: "clear the terminal and start a new chat"},
	{Command: SlashExit, Description: "exit Amadeus"},
}

func SlashCommandCatalog() []SlashCommandSpec {
	return append([]SlashCommandSpec(nil), slashCommandCatalog...)
}

func SlashCommands() []string {
	result := make([]string, 0, len(slashCommandCatalog))
	for _, spec := range slashCommandCatalog {
		result = append(result, "/"+string(spec.Command))
	}
	return result
}

func FindSlashCommand(name string) (SlashCommandSpec, bool) {
	name = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(name)), "/")
	for _, spec := range slashCommandCatalog {
		if string(spec.Command) == name {
			return spec, true
		}
	}
	return SlashCommandSpec{}, false
}

func FilterSlashCommands(input string, running bool) []SlashCommandSpec {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") || strings.Contains(strings.TrimPrefix(input, "/"), "/") {
		return nil
	}
	trimmed := strings.TrimPrefix(input, "/")
	if strings.ContainsAny(trimmed, " \t\r\n") {
		return nil
	}
	query := ""
	if fields := strings.Fields(trimmed); len(fields) > 0 {
		query = strings.ToLower(fields[0])
	}
	exact := make([]SlashCommandSpec, 0, 1)
	prefix := make([]SlashCommandSpec, 0, len(slashCommandCatalog))
	for _, spec := range slashCommandCatalog {
		if running && !spec.AvailableDuringRun {
			continue
		}
		name := string(spec.Command)
		switch {
		case query == "":
			prefix = append(prefix, spec)
		case name == query:
			exact = append(exact, spec)
		case strings.HasPrefix(name, query):
			prefix = append(prefix, spec)
		}
	}
	return append(exact, prefix...)
}

func CompleteSlashCommand(prefix string) []string {
	matches := FilterSlashCommands(prefix, false)
	result := make([]string, 0, len(matches))
	for _, spec := range matches {
		result = append(result, "/"+string(spec.Command))
	}
	return result
}

func ParseSlashCommand(value string) (SlashCommandSpec, string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "/") {
		return SlashCommandSpec{}, "", false
	}
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return SlashCommandSpec{}, "", false
	}
	spec, ok := FindSlashCommand(fields[0])
	if !ok {
		return SlashCommandSpec{}, "", false
	}
	arguments := strings.TrimSpace(strings.TrimPrefix(value, fields[0]))
	return spec, arguments, true
}

func ValidateSlashCommandArguments(spec SlashCommandSpec, arguments string) error {
	arguments = strings.TrimSpace(arguments)
	if arguments != "" && !spec.SupportsInlineArgs {
		return fmt.Errorf("/%s does not accept arguments", spec.Command)
	}
	if spec.Command == SlashMCP && arguments != "" && !strings.EqualFold(arguments, "verbose") {
		return errorsForSlashUsage(spec, "expected /mcp or /mcp verbose")
	}
	return nil
}

func errorsForSlashUsage(spec SlashCommandSpec, message string) error {
	return fmt.Errorf("invalid /%s arguments: %s", spec.Command, message)
}

func FormatSlashCatalog() string {
	var builder strings.Builder
	for index, spec := range slashCommandCatalog {
		if index > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString("/")
		builder.WriteString(string(spec.Command))
		builder.WriteString("  ")
		builder.WriteString(spec.Description)
	}
	return builder.String()
}
