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

type ToolAuthorizer struct {
	commandGuard *CommandGuard
	approvals    ApprovalHandler
	session      *SessionApprovalStore
	audit        audit.Sink
	events       event.Sink
	sessionID    string
	now          func() time.Time
}

type ToolAuthorizerOptions struct {
	SessionApprovals *SessionApprovalStore
	Audit            audit.Sink
	Events           event.Sink
	SessionID        string
	Now              func() time.Time
}

func NewToolAuthorizer(approvals ApprovalHandler, sessionApprovals *SessionApprovalStore) (*ToolAuthorizer, error) {
	return NewToolAuthorizerWithOptions(approvals, ToolAuthorizerOptions{SessionApprovals: sessionApprovals})
}

func NewToolAuthorizerWithOptions(approvals ApprovalHandler, options ToolAuthorizerOptions) (*ToolAuthorizer, error) {
	if approvals == nil {
		return nil, errors.New("tool authorizer approval handler is nil")
	}
	if options.SessionApprovals == nil {
		options.SessionApprovals = NewSessionApprovalStore()
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &ToolAuthorizer{
		commandGuard: NewCommandGuard(), approvals: approvals, session: options.SessionApprovals,
		audit: options.Audit, events: options.Events, sessionID: strings.TrimSpace(options.SessionID), now: options.Now,
	}, nil
}

func (authorizer *ToolAuthorizer) Authorize(ctx context.Context, spec tool.Spec, prepared tool.PreparedCall) (authorizationErr error) {
	if authorizer == nil {
		return errors.New("tool authorizer is nil")
	}
	if ctx == nil {
		return errors.New("tool authorizer context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	startedAt := authorizer.now()
	call := prepared.Call()
	record := audit.Record{
		Timestamp: startedAt, SessionID: authorizer.sessionID, RequestID: call.ID, ToolName: call.Name,
		Outcome: audit.OutcomeError, Source: string(ApprovalSourcePolicy), Reason: "tool authorization failed",
	}
	if record.SessionID == "" {
		record.SessionID = event.MetadataFromContext(ctx).SessionID
	}
	if canonical, err := canonicalArguments(call.Arguments); err == nil {
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
			err = fmt.Errorf("write tool authorization audit: %w", err)
			if authorizationErr == nil {
				authorizationErr = err
			} else {
				authorizationErr = errors.Join(authorizationErr, err)
			}
		}
	}()

	decision, risk, err := authorizer.authorize(ctx, spec, prepared)
	record.Risk = string(risk)
	record.Scope, record.Source, record.Reason = string(decision.Scope), string(decision.Source), decision.Reason
	if err != nil {
		record.Outcome = audit.OutcomeDeny
		return err
	}
	record.Outcome = audit.OutcomeAllow
	return nil
}

func (authorizer *ToolAuthorizer) authorize(ctx context.Context, spec tool.Spec, prepared tool.PreparedCall) (ApprovalDecision, CommandRisk, error) {
	call := prepared.Call()
	if call.Name != spec.Name || strings.TrimSpace(call.ID) == "" {
		return ApprovalDecision{}, "", errors.New("tool authorizer call identity is invalid")
	}
	if spec.Name == "write_stdin" || spec.Name != "execute_command" {
		return ApprovalDecision{Outcome: ApprovalAllow, Scope: ApprovalOnce, Source: ApprovalSourcePolicy, Reason: "permission check completed during tool preparation"}, CommandRiskLow, nil
	}
	assessment, err := authorizer.commandGuard.Assess(prepared.Command())
	if err != nil {
		return ApprovalDecision{}, "", fmt.Errorf("command policy assessment failed: %w", err)
	}
	if assessment.Disposition == CommandDeny {
		decision := ApprovalDecision{Outcome: ApprovalDeny, Scope: ApprovalOnce, Source: ApprovalSourcePolicy, Reason: assessment.Reason}
		return decision, assessment.Risk, &ToolDeniedError{ToolName: call.Name, Risk: assessment.Risk, Source: decision.Source, Reason: decision.Reason}
	}
	isolation := sandboxdomain.IsolationMode(prepared.IsolationMode())
	if isolation == sandboxdomain.IsolationSandboxed {
		return ApprovalDecision{Outcome: ApprovalAllow, Scope: ApprovalOnce, Source: ApprovalSourcePolicy, Reason: "command runs inside the filesystem sandbox"}, CommandRiskLow, nil
	}
	if isolation != sandboxdomain.IsolationUnsandboxed {
		return ApprovalDecision{}, "", fmt.Errorf("execute_command isolation mode %q is invalid", isolation)
	}
	key, ok := NewCommandApprovalKey(prepared.Shell(), prepared.Command(), prepared.CWD(), prepared.TTY(), isolation)
	if !ok {
		return ApprovalDecision{}, "", errors.New("prepared execute_command approval key is invalid")
	}
	if authorizer.session.IsApproved(key) {
		return ApprovalDecision{Outcome: ApprovalAllow, Scope: ApprovalSession, Source: ApprovalSourceGrant, Reason: "matching unsandboxed command was approved for this session"}, CommandRiskHigh, nil
	}
	request, err := NewApprovalRequestForPurpose(call.ID, call.Name, call.Arguments, ApprovalPurposeCommand, CommandRiskHigh, "unsandboxed host command may access resources outside declared permissions")
	if err != nil {
		return ApprovalDecision{}, CommandRiskHigh, err
	}
	decision, err := authorizer.requestApproval(ctx, request)
	if err != nil {
		return ApprovalDecision{}, CommandRiskHigh, err
	}
	if decision.Allowed() && decision.Scope == ApprovalSession {
		authorizer.session.Approve(key)
	}
	if !decision.Allowed() {
		return decision, CommandRiskHigh, &ToolDeniedError{ToolName: call.Name, Risk: CommandRiskHigh, Source: decision.Source, Reason: decision.Reason}
	}
	return decision, CommandRiskHigh, nil
}

func (authorizer *ToolAuthorizer) requestApproval(ctx context.Context, request ApprovalRequest) (ApprovalDecision, error) {
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

var _ tool.Authorizer = (*ToolAuthorizer)(nil)
