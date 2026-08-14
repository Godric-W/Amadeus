package policy

import (
	"errors"
	"fmt"
	"strings"
)

type ApprovalDecision struct {
	OptionID string          `json:"option_id,omitempty"`
	Outcome  ApprovalOutcome `json:"outcome"`
	Scope    ApprovalScope   `json:"scope"`
	Source   ApprovalSource  `json:"source"`
	Reason   string          `json:"reason"`
}

// ResolveApprovalInput maps plain terminal input to the option supplied by
// the tool. Numeric choices are useful for structured prompts; y/s/n remain
// compatible aliases for the common allow/session/deny presentation.
func ResolveApprovalInput(request ApprovalRequest, input string) (ApprovalDecision, bool) {
	value := strings.ToLower(strings.TrimSpace(input))
	options := request.Presentation.Options
	if len(options) == 0 {
		firstScope := ApprovalOnce
		firstReason := "user approved once"
		switch value {
		case "y", "yes":
			return ApprovalDecision{Outcome: ApprovalAllow, Scope: firstScope, Source: ApprovalSourceUser, Reason: firstReason}, true
		case "s", "session":
			return ApprovalDecision{Outcome: ApprovalAllow, Scope: ApprovalSession, Source: ApprovalSourceUser, Reason: "user approved for the session"}, true
		case "n", "no", "deny":
			return ApprovalDecision{Outcome: ApprovalDeny, Scope: firstScope, Source: ApprovalSourceUser, Reason: "user denied the request"}, true
		default:
			return ApprovalDecision{}, false
		}
	}
	selected := -1
	if len(value) == 1 && value[0] >= '1' && int(value[0]-'1') < len(options) {
		selected = int(value[0] - '1')
	}
	if selected < 0 {
		for index, option := range options {
			if value == strings.ToLower(strings.TrimSpace(option.ID)) {
				selected = index
				break
			}
		}
	}
	if selected < 0 {
		switch value {
		case "y", "yes":
			for index, option := range options {
				if option.Outcome == ApprovalAllow {
					selected = index
					break
				}
			}
		case "s", "session":
			for index, option := range options {
				if option.Outcome == ApprovalAllow && option.Scope == ApprovalSession {
					selected = index
					break
				}
			}
		case "n", "no", "deny":
			for index, option := range options {
				if option.Outcome == ApprovalDeny {
					selected = index
					break
				}
			}
		}
	}
	if selected < 0 {
		return ApprovalDecision{}, false
	}
	option := options[selected]
	reason := "user selected " + option.Label
	return ApprovalDecision{OptionID: option.ID, Outcome: option.Outcome, Scope: option.Scope, Source: ApprovalSourceUser, Reason: reason}, true
}

func (decision ApprovalDecision) Validate() error {
	if decision.Outcome != ApprovalAllow && decision.Outcome != ApprovalDeny {
		return fmt.Errorf("approval outcome %q is invalid", decision.Outcome)
	}
	if decision.Scope != ApprovalOnce && decision.Scope != ApprovalSession {
		return fmt.Errorf("approval scope %q is invalid", decision.Scope)
	}
	switch decision.Source {
	case ApprovalSourceUser, ApprovalSourcePolicy, ApprovalSourceGrant:
	default:
		return fmt.Errorf("approval source %q is invalid", decision.Source)
	}
	if strings.TrimSpace(decision.Reason) == "" {
		return errors.New("approval decision reason is empty")
	}
	return nil
}

func (decision ApprovalDecision) Allowed() bool { return decision.Outcome == ApprovalAllow }
