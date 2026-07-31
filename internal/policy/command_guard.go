package policy

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

type CommandRisk string

const (
	CommandRiskLow      CommandRisk = "low"
	CommandRiskModerate CommandRisk = "moderate"
	CommandRiskHigh     CommandRisk = "high"
	CommandRiskBlocked  CommandRisk = "blocked"
)

func (risk CommandRisk) Valid() bool {
	return risk == CommandRiskLow || risk == CommandRiskModerate || risk == CommandRiskHigh || risk == CommandRiskBlocked
}

type CommandDisposition string

const (
	CommandAllow           CommandDisposition = "allow"
	CommandRequireApproval CommandDisposition = "require_approval"
	CommandDeny            CommandDisposition = "deny"
)

type CommandAssessment struct {
	Risk        CommandRisk        `json:"risk"`
	Disposition CommandDisposition `json:"disposition"`
	Reason      string             `json:"reason"`
	Programs    []string           `json:"programs"`
	Operators   []string           `json:"operators,omitempty"`
}

type CommandGuard struct{}

func NewCommandGuard() *CommandGuard { return &CommandGuard{} }

func (guard *CommandGuard) Assess(command string) (CommandAssessment, error) {
	if guard == nil {
		return CommandAssessment{}, errors.New("command guard is nil")
	}
	tokens, err := tokenizeCommand(command)
	if err != nil {
		return CommandAssessment{}, err
	}
	segments, operators, err := commandSegments(tokens)
	if err != nil {
		return CommandAssessment{}, err
	}
	assessment := CommandAssessment{Risk: CommandRiskLow, Disposition: CommandAllow, Reason: "read-only command", Operators: operators}
	for _, segment := range segments {
		program, arguments := commandProgram(segment)
		assessment.Programs = append(assessment.Programs, program)
		risk, reason := classifyCommand(program, arguments)
		if riskRank(risk) > riskRank(assessment.Risk) {
			assessment.Risk, assessment.Reason = risk, reason
		}
	}
	if downloadsIntoShell(segments, operators) {
		assessment.Risk, assessment.Reason = CommandRiskBlocked, "downloaded content is piped into a shell"
	}
	if strings.Contains(command, "$(") || strings.ContainsRune(command, '`') {
		if riskRank(assessment.Risk) < riskRank(CommandRiskHigh) {
			assessment.Risk, assessment.Reason = CommandRiskHigh, "command contains nested command substitution"
		}
	}
	assessment.Disposition = dispositionForRisk(assessment.Risk)
	return assessment, nil
}

func tokenizeCommand(command string) ([]string, error) {
	if strings.TrimSpace(command) == "" {
		return nil, errors.New("command is empty")
	}
	if strings.IndexByte(command, 0) >= 0 {
		return nil, errors.New("command contains NUL")
	}
	var tokens []string
	var current strings.Builder
	quote := rune(0)
	escaped := false
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	for _, char := range command {
		if escaped {
			current.WriteRune(char)
			escaped = false
			continue
		}
		if char == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				current.WriteRune(char)
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			continue
		}
		if unicode.IsSpace(char) {
			flush()
			if char == '\n' {
				tokens = append(tokens, ";")
			}
			continue
		}
		if strings.ContainsRune(";&|<>", char) {
			flush()
			operator := string(char)
			if len(tokens) > 0 && (char == '&' || char == '|' || char == '>' || char == '<') && tokens[len(tokens)-1] == operator {
				tokens[len(tokens)-1] += operator
			} else {
				tokens = append(tokens, operator)
			}
			continue
		}
		current.WriteRune(char)
	}
	if escaped || quote != 0 {
		return nil, errors.New("command contains an unterminated quote or escape")
	}
	flush()
	return tokens, nil
}

func commandSegments(tokens []string) ([][]string, []string, error) {
	var segments [][]string
	var current []string
	var operators []string
	for _, token := range tokens {
		if isOperator(token) {
			if len(current) == 0 {
				return nil, nil, fmt.Errorf("command operator %q has no preceding command", token)
			}
			segments = append(segments, current)
			current = nil
			operators = append(operators, token)
			continue
		}
		current = append(current, token)
	}
	if len(current) == 0 {
		return nil, nil, errors.New("command ends with an operator")
	}
	segments = append(segments, current)
	return segments, operators, nil
}

func commandProgram(segment []string) (string, []string) {
	index := 0
	for index < len(segment) && strings.Contains(segment[index], "=") && !strings.HasPrefix(segment[index], "=") {
		index++
	}
	if index >= len(segment) {
		return "environment", nil
	}
	return strings.ToLower(segment[index]), segment[index+1:]
}

func classifyCommand(program string, args []string) (CommandRisk, string) {
	switch program {
	case "sudo", "su", "shutdown", "reboot", "halt", "poweroff", "mkfs", "fdisk", "parted":
		return CommandRiskBlocked, program + " is prohibited"
	case "rm":
		if hasFlag(args, 'r') && hasFlag(args, 'f') && hasAny(args, "/", "/*", ".", "..", "~", "$HOME") {
			return CommandRiskBlocked, "recursive forced removal targets a broad path"
		}
		return CommandRiskHigh, "command removes files"
	case "git":
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "reset --hard") || gitCleanForced(args) || strings.Contains(joined, "checkout -- .") || strings.Contains(joined, "restore .") {
			return CommandRiskBlocked, "git command can discard uncommitted work"
		}
		if len(args) > 0 && hasAny([]string{args[0]}, "status", "diff", "log", "show", "branch", "rev-parse") {
			return CommandRiskLow, "read-only git command"
		}
		return CommandRiskHigh, "git command may modify repository state"
	case "pwd", "ls", "cat", "head", "tail", "grep", "rg", "find", "wc", "stat", "which", "type":
		return CommandRiskLow, "read-only inspection command"
	case "go":
		if len(args) > 0 && hasAny([]string{args[0]}, "version", "env", "list") {
			return CommandRiskLow, "read-only Go command"
		}
		return CommandRiskModerate, "build or test command can execute project code"
	case "mkdir", "touch", "mv", "cp", "tee", "truncate", "chmod", "chown", "sed", "patch", "curl", "wget", "ssh", "scp", "npm", "pnpm", "yarn", "pip", "cargo":
		return CommandRiskHigh, "command may modify files, dependencies, or external systems"
	case "bash", "sh", "zsh", "fish":
		return CommandRiskHigh, "nested shell hides command effects"
	case "environment":
		return CommandRiskModerate, "environment-only command has unclear effect"
	default:
		return CommandRiskHigh, "unknown command is classified conservatively"
	}
}

func downloadsIntoShell(segments [][]string, operators []string) bool {
	for index, operator := range operators {
		if operator != "|" || index+1 >= len(segments) {
			continue
		}
		left, _ := commandProgram(segments[index])
		right, _ := commandProgram(segments[index+1])
		if hasAny([]string{left}, "curl", "wget") && hasAny([]string{right}, "sh", "bash", "zsh") {
			return true
		}
	}
	return false
}

func isOperator(token string) bool {
	return hasAny([]string{token}, ";", "&&", "||", "|", "&", ">", ">>", "<", "<<")
}
func hasAny(values []string, candidates ...string) bool {
	for _, value := range values {
		for _, candidate := range candidates {
			if value == candidate {
				return true
			}
		}
	}
	return false
}
func hasFlag(arguments []string, flag rune) bool {
	for _, argument := range arguments {
		if strings.HasPrefix(argument, "-") && strings.ContainsRune(strings.TrimLeft(argument, "-"), flag) {
			return true
		}
	}
	return false
}
func gitCleanForced(arguments []string) bool {
	return len(arguments) > 1 && arguments[0] == "clean" && hasFlag(arguments[1:], 'f')
}
func riskRank(risk CommandRisk) int {
	switch risk {
	case CommandRiskLow:
		return 0
	case CommandRiskModerate:
		return 1
	case CommandRiskHigh:
		return 2
	case CommandRiskBlocked:
		return 3
	}
	return -1
}
func dispositionForRisk(risk CommandRisk) CommandDisposition {
	if risk == CommandRiskLow {
		return CommandAllow
	}
	if risk == CommandRiskBlocked {
		return CommandDeny
	}
	return CommandRequireApproval
}
