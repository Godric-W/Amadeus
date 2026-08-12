package policy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/audit"
	sandboxdomain "github.com/Godric-W/Amadeus/internal/sandbox"
	"github.com/Godric-W/Amadeus/internal/tool"
)

var ErrToolDenied = errors.New("tool execution denied")

type ToolDeniedError struct {
	ToolName string
	Risk     CommandRisk
	Source   ApprovalSource
	Reason   string
}

func (err *ToolDeniedError) Error() string {
	return fmt.Sprintf("%s: %s: %s", ErrToolDenied, err.ToolName, err.Reason)
}
func (err *ToolDeniedError) Unwrap() error         { return ErrToolDenied }
func (err *ToolDeniedError) ToolErrorKind() string { return "approval_denied" }

type CommandRequest struct {
	Call          tool.ToolCall
	Shell         string
	Command       string
	CWD           string
	TTY           bool
	IsolationMode sandboxdomain.IsolationMode
}

type CommandAuthorizer struct {
	commandGuard *CommandGuard
	approvals    ApprovalHandler
	session      *SessionApprovalStore
	audit        audit.Sink
	events       event.Sink
	sessionID    string
	now          func() time.Time
}

type CommandAuthorizerOptions struct {
	SessionApprovals *SessionApprovalStore
	Audit            audit.Sink
	Events           event.Sink
	SessionID        string
	Now              func() time.Time
}

func NewCommandAuthorizer(approvals ApprovalHandler, sessionApprovals *SessionApprovalStore) (*CommandAuthorizer, error) {
	return NewCommandAuthorizerWithOptions(approvals, CommandAuthorizerOptions{SessionApprovals: sessionApprovals})
}

func NewCommandAuthorizerWithOptions(approvals ApprovalHandler, options CommandAuthorizerOptions) (*CommandAuthorizer, error) {
	if approvals == nil {
		return nil, errors.New("command authorizer approval handler is nil")
	}
	if options.SessionApprovals == nil {
		options.SessionApprovals = NewSessionApprovalStore()
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &CommandAuthorizer{
		commandGuard: NewCommandGuard(), approvals: approvals, session: options.SessionApprovals,
		audit: options.Audit, events: options.Events, sessionID: strings.TrimSpace(options.SessionID), now: options.Now,
	}, nil
}

func (authorizer *CommandAuthorizer) Authorize(ctx context.Context, request CommandRequest) (authorizationErr error) {
	if authorizer == nil {
		return errors.New("command authorizer is nil")
	}
	if ctx == nil {
		return errors.New("command authorizer context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(request.Call.ID) == "" || request.Call.Name != "execute_command" {
		return errors.New("command authorizer call identity is invalid")
	}
	startedAt := authorizer.now()
	record := audit.Record{
		Timestamp: startedAt, SessionID: authorizer.sessionID, RequestID: request.Call.ID, ToolName: request.Call.Name,
		Outcome: audit.OutcomeError, Source: string(ApprovalSourcePolicy), Reason: "command authorization failed",
	}
	if record.SessionID == "" {
		record.SessionID = event.MetadataFromContext(ctx).SessionID
	}
	if canonical, err := canonicalArguments(request.Call.Payload); err == nil {
		record.ArgumentsSHA256 = approvalHash(canonical)
	}
	defer func() {
		if authorizer.audit == nil {
			return
		}
		if finishedAt := authorizer.now(); !finishedAt.Before(startedAt) {
			record.DurationMS = finishedAt.Sub(startedAt).Milliseconds()
		}
		if err := authorizer.audit.Write(ctx, record); err != nil {
			auditErr := fmt.Errorf("write command authorization audit: %w", err)
			if authorizationErr == nil {
				authorizationErr = auditErr
			} else {
				authorizationErr = errors.Join(authorizationErr, auditErr)
			}
		}
	}()

	decision, risk, err := authorizer.authorize(ctx, request)
	record.Risk = string(risk)
	record.Scope, record.Source, record.Reason = string(decision.Scope), string(decision.Source), decision.Reason
	if err != nil {
		record.Outcome = audit.OutcomeDeny
		return err
	}
	record.Outcome = audit.OutcomeAllow
	return nil
}

func (authorizer *CommandAuthorizer) authorize(ctx context.Context, request CommandRequest) (ApprovalDecision, CommandRisk, error) {
	assessment, err := authorizer.commandGuard.Assess(request.Command)
	if err != nil {
		return ApprovalDecision{}, "", fmt.Errorf("command policy assessment failed: %w", err)
	}
	if assessment.Disposition == CommandDeny {
		decision := ApprovalDecision{Outcome: ApprovalDeny, Scope: ApprovalOnce, Source: ApprovalSourcePolicy, Reason: assessment.Reason}
		return decision, assessment.Risk, &ToolDeniedError{ToolName: request.Call.Name, Risk: assessment.Risk, Source: decision.Source, Reason: decision.Reason}
	}
	if request.IsolationMode == sandboxdomain.IsolationSandboxed {
		return ApprovalDecision{Outcome: ApprovalAllow, Scope: ApprovalOnce, Source: ApprovalSourcePolicy, Reason: "command runs inside the filesystem sandbox"}, CommandRiskLow, nil
	}
	if request.IsolationMode != sandboxdomain.IsolationUnsandboxed {
		return ApprovalDecision{}, "", fmt.Errorf("execute_command isolation mode %q is invalid", request.IsolationMode)
	}
	key, ok := NewCommandApprovalKey(request.Command, request.CWD)
	if !ok {
		return ApprovalDecision{}, "", errors.New("execute_command approval key is invalid")
	}
	if authorizer.session.IsApproved(key) {
		return ApprovalDecision{Outcome: ApprovalAllow, Scope: ApprovalSession, Source: ApprovalSourceGrant, Reason: "matching unsandboxed command was approved for this session"}, CommandRiskHigh, nil
	}
	approval, err := NewApprovalRequestForPurpose(request.Call.ID, request.Call.Name, request.Call.Payload, ApprovalPurposeCommand, CommandRiskHigh, "unsandboxed host command may access resources outside declared permissions")
	if err != nil {
		return ApprovalDecision{}, CommandRiskHigh, err
	}
	approval.Command = request.Command
	approval.CWD = request.CWD
	approval.Presentation = CommandApprovalPresentation(request.Command, request.CWD)
	decision, err := authorizer.requestApproval(ctx, approval)
	if err != nil {
		return ApprovalDecision{}, CommandRiskHigh, err
	}
	if decision.Allowed() && decision.Scope == ApprovalSession {
		authorizer.session.Approve(key)
	}
	if !decision.Allowed() {
		return decision, CommandRiskHigh, &ToolDeniedError{ToolName: request.Call.Name, Risk: CommandRiskHigh, Source: decision.Source, Reason: decision.Reason}
	}
	return decision, CommandRiskHigh, nil
}

func (authorizer *CommandAuthorizer) requestApproval(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error) {
	if authorizer.events != nil {
		if err := authorizer.events.Publish(ctx, event.ApprovalRequested{RequestID: request.ID, ToolName: request.ToolName, Risk: string(request.Risk), Reason: request.Reason}); err != nil {
			return ApprovalDecision{}, fmt.Errorf("publish tool approval requested: %w", err)
		}
	}
	decision, err := authorizer.approvals.Decide(ctx, request.Clone())
	if err != nil {
		return ApprovalDecision{}, fmt.Errorf("resolve tool approval: %w", err)
	}
	if err := decision.Validate(); err != nil {
		return ApprovalDecision{}, fmt.Errorf("validate tool approval decision: %w", err)
	}
	if decision.Scope == ApprovalRun {
		return ApprovalDecision{}, errors.New("command operation approval cannot use run scope")
	}
	if authorizer.events != nil {
		if err := authorizer.events.Publish(ctx, event.ApprovalResolved{RequestID: request.ID, ToolName: request.ToolName, Outcome: string(decision.Outcome), Scope: string(decision.Scope), Source: string(decision.Source), Reason: decision.Reason}); err != nil {
			return ApprovalDecision{}, fmt.Errorf("publish tool approval resolved: %w", err)
		}
	}
	return decision, nil
}
