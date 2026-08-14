package policy

import (
	"errors"
	"strings"
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
}

type CommandGuard struct{}

func NewCommandGuard() *CommandGuard { return &CommandGuard{} }

func (guard *CommandGuard) Assess(command string) (CommandAssessment, error) {
	if guard == nil {
		return CommandAssessment{}, errors.New("command guard is nil")
	}
	if strings.TrimSpace(command) == "" {
		return CommandAssessment{}, errors.New("command is empty")
	}
	if strings.ContainsRune(command, '\x00') {
		return CommandAssessment{}, errors.New("command contains NUL")
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(command), " "))
	for _, forbidden := range []string{
		"rm -rf /", "rm -fr /", "mkfs ", "shutdown ", "reboot ", "poweroff ",
		"git reset --hard", "git clean -fd", "git clean -df", "git checkout -- .", "git restore .",
	} {
		if strings.Contains(normalized, forbidden) {
			return CommandAssessment{Risk: CommandRiskBlocked, Disposition: CommandDeny, Reason: "command matches the minimal catastrophic deny set"}, nil
		}
	}
	return CommandAssessment{Risk: CommandRiskHigh, Disposition: CommandRequireApproval, Reason: "command requires user confirmation"}, nil
}
