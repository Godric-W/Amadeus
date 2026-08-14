package policy

import (
	"errors"
	"fmt"
	"strings"
)

type ApprovalOutcome string

const (
	ApprovalAllow ApprovalOutcome = "allow"
	ApprovalDeny  ApprovalOutcome = "deny"
)

type ApprovalScope string

const (
	ApprovalOnce    ApprovalScope = "once"
	ApprovalSession ApprovalScope = "session"
)

type ApprovalPurpose string

const (
	ApprovalPurposeCommand    ApprovalPurpose = "command_operation"
	ApprovalPurposePermission ApprovalPurpose = "filesystem_permission"
	ApprovalPurposeFile       ApprovalPurpose = "file_edit"
	ApprovalPurposeExternal   ApprovalPurpose = "external_operation"
)

type ApprovalSource string

const (
	ApprovalSourceUser   ApprovalSource = "user"
	ApprovalSourcePolicy ApprovalSource = "policy"
	ApprovalSourceGrant  ApprovalSource = "grant"
)

type ApprovalCauseKind string

const (
	ApprovalCauseCommand        ApprovalCauseKind = "command"
	ApprovalCauseFileChange     ApprovalCauseKind = "file_change"
	ApprovalCauseFilesystemRead ApprovalCauseKind = "filesystem_read"
	ApprovalCauseNetwork        ApprovalCauseKind = "network"
	ApprovalCauseExternalTool   ApprovalCauseKind = "external_tool"
	ApprovalCausePolicy         ApprovalCauseKind = "policy"
)

type ApprovalCause struct {
	Kind   ApprovalCauseKind `json:"kind"`
	Code   string            `json:"code"`
	Detail string            `json:"detail,omitempty"`
}

func (cause ApprovalCause) Validate() error {
	switch cause.Kind {
	case ApprovalCauseCommand, ApprovalCauseFileChange, ApprovalCauseFilesystemRead, ApprovalCauseNetwork, ApprovalCauseExternalTool, ApprovalCausePolicy:
	default:
		return fmt.Errorf("approval cause kind %q is invalid", cause.Kind)
	}
	if strings.TrimSpace(cause.Code) == "" {
		return errors.New("approval cause code is empty")
	}
	return nil
}

type ApprovalPresentation struct {
	Title    string           `json:"title,omitempty"`
	Question string           `json:"question,omitempty"`
	Details  []string         `json:"details,omitempty"`
	Options  []ApprovalOption `json:"options,omitempty"`
}

type ApprovalOption struct {
	ID          string          `json:"id"`
	Label       string          `json:"label"`
	Description string          `json:"description,omitempty"`
	Scope       ApprovalScope   `json:"scope"`
	Outcome     ApprovalOutcome `json:"outcome"`
}
