package protocol

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/filechange"
)

type ApprovalDecisionOp struct {
	RequestID RequestID
	OptionID  string
	Outcome   string
	Scope     string
	Source    string
	Reason    string
}

func (ApprovalDecisionOp) isOp() {}

type ApprovalPresentation struct {
	Title       string
	Description string
	Details     []string
	Options     []ApprovalOption
	Diff        *filechange.Preview
}

type ApprovalOption struct {
	ID          string
	Label       string
	Description string
}

type ApprovalRequest struct {
	ID           RequestID
	ToolName     string
	Presentation ApprovalPresentation
	Raw          json.RawMessage
}

type ApprovalRequestEvent struct {
	RequestID RequestID
	ThreadID  ThreadID
	TurnID    TurnID
	Approval  ApprovalRequest
}

func (ApprovalRequestEvent) isEventMsg() {}

func (request ApprovalRequestEvent) Validate() error {
	if strings.TrimSpace(string(request.RequestID)) == "" {
		return errors.New("approval request ID is empty")
	}
	if strings.TrimSpace(string(request.Approval.ID)) == "" {
		return errors.New("approval request payload is incomplete")
	}
	return nil
}
