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

func BuiltinSlashCommands() []SlashCommand {
	return []SlashCommand{
		SlashResume, SlashSkills, SlashRename, SlashDelete, SlashCompact,
		SlashPlan, SlashCopy, SlashStatus, SlashMCP, SlashClear, SlashExit,
	}
}

func (command SlashCommand) Name() string { return string(command) }

func (command SlashCommand) Description() string {
	switch command {
	case SlashResume:
		return "resume a saved chat"
	case SlashSkills:
		return "use skills to improve how Amadeus performs specific tasks"
	case SlashRename:
		return "rename the current session"
	case SlashDelete:
		return "permanently delete this session and exit"
	case SlashCompact:
		return "summarize conversation to prevent hitting the context limit"
	case SlashPlan:
		return "switch to Plan mode"
	case SlashCopy:
		return "copy last response as markdown"
	case SlashStatus:
		return "show current session configuration and token usage"
	case SlashMCP:
		return "list configured MCP tools; use /mcp verbose for details"
	case SlashClear:
		return "clear the terminal and start a new chat"
	case SlashExit:
		return "exit Amadeus"
	default:
		return ""
	}
}

func (command SlashCommand) SupportsInlineArgs() bool {
	return command == SlashResume || command == SlashRename || command == SlashMCP || command == SlashPlan
}

func (command SlashCommand) AvailableDuringTask() bool {
	return command == SlashSkills || command == SlashCopy || command == SlashStatus || command == SlashMCP
}

func SlashCommands() []string {
	commands := BuiltinSlashCommands()
	result := make([]string, 0, len(commands))
	for _, command := range commands {
		result = append(result, "/"+command.Name())
	}
	return result
}

type SlashInvocation struct {
	Command SlashCommand
	Args    string
}

type InputResult struct {
	Text    string
	Command *SlashInvocation
}

func FindSlashCommand(name string) (SlashCommand, bool) {
	name = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(name)), "/")
	for _, command := range BuiltinSlashCommands() {
		if command.Name() == name {
			return command, true
		}
	}
	return "", false
}

func FilterSlashCommands(input string, running bool) []SlashCommand {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") || strings.Contains(strings.TrimPrefix(input, "/"), "/") {
		return nil
	}
	trimmed := strings.TrimPrefix(input, "/")
	if strings.ContainsAny(trimmed, " \t\r\n") {
		return nil
	}
	query := strings.ToLower(trimmed)
	commands := BuiltinSlashCommands()
	result := make([]SlashCommand, 0, len(commands))
	for _, command := range commands {
		if running && !command.AvailableDuringTask() {
			continue
		}
		if query == "" || strings.HasPrefix(command.Name(), query) {
			result = append(result, command)
		}
	}
	return result
}

func CompleteSlashCommand(prefix string) []string {
	matches := FilterSlashCommands(prefix, false)
	result := make([]string, 0, len(matches))
	for _, command := range matches {
		result = append(result, "/"+command.Name())
	}
	return result
}

func ParseInput(value string) (InputResult, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return InputResult{}, nil
	}
	if !strings.HasPrefix(value, "/") {
		return InputResult{Text: value}, nil
	}
	invocation, err := ParseSlashInvocation(value)
	if err != nil {
		return InputResult{}, err
	}
	return InputResult{Command: &invocation}, nil
}

func ParseSlashInvocation(value string) (SlashInvocation, error) {
	value = strings.TrimSpace(value)
	fields := strings.Fields(value)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return SlashInvocation{}, fmt.Errorf("invalid slash command %q", value)
	}
	command, ok := FindSlashCommand(fields[0])
	if !ok {
		return SlashInvocation{}, fmt.Errorf("unknown command %q", fields[0])
	}
	arguments := strings.TrimSpace(strings.TrimPrefix(value, fields[0]))
	if err := ValidateSlashCommandArguments(command, arguments); err != nil {
		return SlashInvocation{}, err
	}
	return SlashInvocation{Command: command, Args: arguments}, nil
}

func ValidateSlashCommandArguments(command SlashCommand, arguments string) error {
	arguments = strings.TrimSpace(arguments)
	if arguments != "" && !command.SupportsInlineArgs() {
		return fmt.Errorf("/%s does not accept arguments", command)
	}
	if command == SlashMCP && arguments != "" && !strings.EqualFold(arguments, "verbose") {
		return fmt.Errorf("invalid /%s arguments: expected /mcp or /mcp verbose", command)
	}
	return nil
}

func FormatSlashCommands() string {
	var builder strings.Builder
	for index, command := range BuiltinSlashCommands() {
		if index > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString("/")
		builder.WriteString(command.Name())
		builder.WriteString("  ")
		builder.WriteString(command.Description())
	}
	return builder.String()
}
