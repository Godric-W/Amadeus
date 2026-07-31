package policy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	ApprovalAlways  ApprovalScope = "always"
)

type ApprovalSource string

const (
	ApprovalSourceUser    ApprovalSource = "user"
	ApprovalSourceDefault ApprovalSource = "default"
	ApprovalSourcePolicy  ApprovalSource = "policy"
	ApprovalSourceGrant   ApprovalSource = "grant"
)

type ApprovalRequest struct {
	ID              string          `json:"id"`
	ToolName        string          `json:"tool_name"`
	Arguments       json.RawMessage `json:"arguments"`
	ArgumentsSHA256 string          `json:"arguments_sha256"`
	Risk            CommandRisk     `json:"risk"`
	Reason          string          `json:"reason"`
}

func NewApprovalRequest(id, toolName string, arguments json.RawMessage, risk CommandRisk, reason string) (ApprovalRequest, error) {
	canonical, err := canonicalArguments(arguments)
	if err != nil {
		return ApprovalRequest{}, err
	}
	request := ApprovalRequest{
		ID: strings.TrimSpace(id), ToolName: strings.TrimSpace(toolName), Arguments: canonical,
		ArgumentsSHA256: approvalHash(canonical), Risk: risk, Reason: strings.TrimSpace(reason),
	}
	if err := request.Validate(); err != nil {
		return ApprovalRequest{}, err
	}
	return request, nil
}

func (request ApprovalRequest) Validate() error {
	if request.ID == "" {
		return errors.New("approval request ID is empty")
	}
	if request.ToolName == "" {
		return errors.New("approval request tool name is empty")
	}
	if !request.Risk.Valid() {
		return fmt.Errorf("approval request risk %q is invalid", request.Risk)
	}
	if request.Reason == "" {
		return errors.New("approval request reason is empty")
	}
	canonical, err := canonicalArguments(request.Arguments)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, request.Arguments) {
		return errors.New("approval request arguments are not canonical JSON")
	}
	if request.ArgumentsSHA256 != approvalHash(request.Arguments) {
		return errors.New("approval request arguments SHA-256 does not match")
	}
	return nil
}

func (request ApprovalRequest) Clone() ApprovalRequest {
	request.Arguments = append(json.RawMessage(nil), request.Arguments...)
	return request
}

type ApprovalDecision struct {
	Outcome ApprovalOutcome `json:"outcome"`
	Scope   ApprovalScope   `json:"scope"`
	Source  ApprovalSource  `json:"source"`
	Reason  string          `json:"reason"`
}

func (decision ApprovalDecision) Validate() error {
	if decision.Outcome != ApprovalAllow && decision.Outcome != ApprovalDeny {
		return fmt.Errorf("approval outcome %q is invalid", decision.Outcome)
	}
	if decision.Scope != ApprovalOnce && decision.Scope != ApprovalSession && decision.Scope != ApprovalAlways {
		return fmt.Errorf("approval scope %q is invalid", decision.Scope)
	}
	switch decision.Source {
	case ApprovalSourceUser, ApprovalSourceDefault, ApprovalSourcePolicy, ApprovalSourceGrant:
	default:
		return fmt.Errorf("approval source %q is invalid", decision.Source)
	}
	if strings.TrimSpace(decision.Reason) == "" {
		return errors.New("approval decision reason is empty")
	}
	return nil
}

func (decision ApprovalDecision) Allowed() bool { return decision.Outcome == ApprovalAllow }

type ApprovalHandler interface {
	Decide(context.Context, ApprovalRequest) (ApprovalDecision, error)
}

func canonicalArguments(arguments json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(arguments))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode approval arguments: %w", err)
	}
	if value == nil {
		return nil, errors.New("decode approval arguments: expected JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("decode approval arguments: multiple JSON values")
		}
		return nil, fmt.Errorf("decode approval arguments trailing content: %w", err)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode canonical approval arguments: %w", err)
	}
	return canonical, nil
}

func approvalHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
