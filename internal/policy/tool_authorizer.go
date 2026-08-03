package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
	patchtool "github.com/Godric-W/Amadeus/internal/tool/patch"
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

func (err *ToolDeniedError) Unwrap() error { return ErrToolDenied }

type ToolAuthorizer struct {
	pathGuard    *project.PathGuard
	commandGuard *CommandGuard
	approvals    ApprovalHandler
	grants       *GrantCache
	audit        audit.Sink
	events       event.Sink
	sessionID    string
	now          func() time.Time
}

func NewToolAuthorizer(root project.Root, approvals ApprovalHandler, grants *GrantCache) (*ToolAuthorizer, error) {
	return NewToolAuthorizerWithOptions(root, approvals, ToolAuthorizerOptions{Grants: grants})
}

type ToolAuthorizerOptions struct {
	Grants    *GrantCache
	Audit     audit.Sink
	Events    event.Sink
	SessionID string
	Now       func() time.Time
}

func NewToolAuthorizerWithOptions(root project.Root, approvals ApprovalHandler, options ToolAuthorizerOptions) (*ToolAuthorizer, error) {
	if approvals == nil {
		return nil, errors.New("tool authorizer approval handler is nil")
	}
	pathGuard, err := project.NewPathGuard(root)
	if err != nil {
		return nil, fmt.Errorf("create tool authorizer path guard: %w", err)
	}
	if options.Grants == nil {
		options.Grants = NewGrantCache()
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &ToolAuthorizer{
		pathGuard: pathGuard, commandGuard: NewCommandGuard(), approvals: approvals, grants: options.Grants,
		audit: options.Audit, events: options.Events, sessionID: strings.TrimSpace(options.SessionID), now: options.Now,
	}, nil
}

func (authorizer *ToolAuthorizer) Authorize(ctx context.Context, spec tool.Spec, call tool.Call) (authorizationErr error) {
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
	record := audit.Record{
		Timestamp: startedAt, SessionID: authorizer.sessionID, RequestID: call.ID, ToolName: call.Name,
		Outcome: audit.OutcomeError, Source: string(ApprovalSourcePolicy), Reason: "tool authorization failed",
	}
	if canonical, err := canonicalArguments(call.Arguments); err == nil {
		record.ArgumentsSHA256 = approvalHash(canonical)
	}
	defer func() {
		if authorizer.audit == nil {
			return
		}
		finishedAt := authorizer.now()
		if !finishedAt.Before(startedAt) {
			record.DurationMS = finishedAt.Sub(startedAt).Milliseconds()
		}
		if auditErr := authorizer.audit.Write(ctx, record); auditErr != nil {
			auditErr = fmt.Errorf("write tool authorization audit: %w", auditErr)
			if authorizationErr == nil {
				authorizationErr = auditErr
			} else {
				authorizationErr = errors.Join(authorizationErr, auditErr)
			}
		}
	}()

	decision, risk, err := authorizer.authorize(ctx, spec, call)
	record.Risk = string(risk)
	if err != nil {
		var denied *ToolDeniedError
		if errors.As(err, &denied) {
			record.Outcome = audit.OutcomeDeny
			record.Source = string(denied.Source)
			record.Reason = denied.Reason
			record.Scope = string(decision.Scope)
		} else {
			record.Reason = err.Error()
		}
		return err
	}
	record.Outcome = audit.OutcomeAllow
	record.Scope = string(decision.Scope)
	record.Source = string(decision.Source)
	record.Reason = decision.Reason
	return nil
}

func (authorizer *ToolAuthorizer) authorize(ctx context.Context, spec tool.Spec, call tool.Call) (ApprovalDecision, CommandRisk, error) {
	if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Name) == "" {
		return ApprovalDecision{}, "", errors.New("tool authorizer call identity is incomplete")
	}
	if call.Name != spec.Name {
		return ApprovalDecision{}, "", fmt.Errorf("tool authorizer call name %q does not match spec %q", call.Name, spec.Name)
	}

	risk, disposition, reason, err := authorizer.assess(spec, call.Arguments)
	if err != nil {
		return ApprovalDecision{}, risk, err
	}
	switch disposition {
	case CommandAllow:
		return ApprovalDecision{Outcome: ApprovalAllow, Scope: ApprovalOnce, Source: ApprovalSourcePolicy, Reason: reason}, risk, nil
	case CommandDeny:
		decision := ApprovalDecision{Outcome: ApprovalDeny, Scope: ApprovalOnce, Source: ApprovalSourcePolicy, Reason: reason}
		return decision, risk, &ToolDeniedError{ToolName: call.Name, Risk: risk, Source: ApprovalSourcePolicy, Reason: reason}
	case CommandRequireApproval:
		decision, err := authorizer.requestApproval(ctx, call, risk, reason)
		return decision, risk, err
	default:
		return ApprovalDecision{}, risk, fmt.Errorf("tool authorizer disposition %q is invalid", disposition)
	}
}

func (authorizer *ToolAuthorizer) assess(spec tool.Spec, arguments json.RawMessage) (CommandRisk, CommandDisposition, string, error) {
	if err := authorizer.preflightPaths(spec.Name, arguments); err != nil {
		return "", "", "", fmt.Errorf("tool path preflight failed: %w", err)
	}
	if spec.Name == "execute_command" {
		command, err := requiredStringArgument(arguments, "command")
		if err != nil {
			return "", "", "", err
		}
		assessment, err := authorizer.commandGuard.Assess(command)
		if err != nil {
			return "", "", "", fmt.Errorf("command policy assessment failed: %w", err)
		}
		if assessment.Disposition == CommandAllow {
			return CommandRiskModerate, CommandRequireApproval, "tool executes command: " + assessment.Reason, nil
		}
		return assessment.Risk, assessment.Disposition, assessment.Reason, nil
	}

	switch spec.SideEffect {
	case tool.SideEffectNone, tool.SideEffectRead:
		return CommandRiskLow, CommandAllow, "read-only tool", nil
	case tool.SideEffectWrite:
		return CommandRiskHigh, CommandRequireApproval, "tool writes project files", nil
	case tool.SideEffectExecute:
		return CommandRiskHigh, CommandRequireApproval, "tool executes code or commands", nil
	case tool.SideEffectNetwork:
		return CommandRiskHigh, CommandRequireApproval, "tool accesses external systems", nil
	default:
		return "", "", "", fmt.Errorf("tool side effect %q is invalid", spec.SideEffect)
	}
}

func (authorizer *ToolAuthorizer) preflightPaths(toolName string, arguments json.RawMessage) error {
	switch toolName {
	case "apply_patch":
		content, err := requiredStringArgument(arguments, "patch")
		if err != nil {
			return err
		}
		document, err := patchtool.Parse([]byte(content), patchtool.ParseOptions{})
		if err != nil {
			return fmt.Errorf("parse patch document for policy: %w", err)
		}
		for _, operation := range document.Operations {
			if _, err := authorizer.pathGuard.ResolveForWrite(operation.Path); err != nil {
				return fmt.Errorf("preflight patch path %q: %w", operation.Path, err)
			}
		}
		return nil
	case "read_file":
		path, err := requiredStringArgument(arguments, "path")
		if err != nil {
			return err
		}
		_, err = authorizer.pathGuard.ResolveExisting(path, project.PathFile)
		return err
	case "write_file":
		path, err := requiredStringArgument(arguments, "path")
		if err != nil {
			return err
		}
		_, err = authorizer.pathGuard.ResolveForWrite(path)
		return err
	case "list_dir":
		path, err := optionalStringArgument(arguments, "path", ".")
		if err != nil {
			return err
		}
		_, err = authorizer.pathGuard.ResolveExisting(path, project.PathDirectory)
		return err
	case "glob_files":
		_, err := authorizer.pathGuard.ResolveExisting(".", project.PathDirectory)
		return err
	case "grep_code":
		path, err := optionalStringArgument(arguments, "path", ".")
		if err != nil {
			return err
		}
		_, err = authorizer.pathGuard.ResolveExisting(path, project.PathAny)
		return err
	case "execute_command":
		cwd, err := optionalStringArgument(arguments, "cwd", ".")
		if err != nil {
			return err
		}
		_, err = authorizer.pathGuard.ResolveExisting(cwd, project.PathDirectory)
		return err
	default:
		return nil
	}
}

func (authorizer *ToolAuthorizer) requestApproval(ctx context.Context, call tool.Call, risk CommandRisk, reason string) (ApprovalDecision, error) {
	request, err := NewApprovalRequest(call.ID, call.Name, call.Arguments, risk, reason)
	if err != nil {
		return ApprovalDecision{}, fmt.Errorf("create tool approval request: %w", err)
	}
	if authorizer.events != nil {
		if err := authorizer.events.Publish(ctx, event.ApprovalRequested{
			RequestID: request.ID, ToolName: request.ToolName, Risk: string(request.Risk), Reason: request.Reason,
		}); err != nil {
			return ApprovalDecision{}, fmt.Errorf("publish tool approval requested: %w", err)
		}
	}
	if decision, ok := authorizer.grants.Lookup(request); ok {
		if err := authorizer.publishApprovalResolved(ctx, request, decision); err != nil {
			return ApprovalDecision{}, err
		}
		return decision, deniedDecisionError(call.Name, risk, decision)
	}
	decision, err := authorizer.approvals.Decide(ctx, request.Clone())
	if err != nil {
		return ApprovalDecision{}, fmt.Errorf("resolve tool approval: %w", err)
	}
	if err := decision.Validate(); err != nil {
		return ApprovalDecision{}, fmt.Errorf("validate tool approval decision: %w", err)
	}
	if err := authorizer.publishApprovalResolved(ctx, request, decision); err != nil {
		return ApprovalDecision{}, err
	}
	if err := authorizer.grants.Remember(request, decision); err != nil {
		return ApprovalDecision{}, fmt.Errorf("remember tool approval decision: %w", err)
	}
	return decision, deniedDecisionError(call.Name, risk, decision)
}

func (authorizer *ToolAuthorizer) publishApprovalResolved(ctx context.Context, request ApprovalRequest, decision ApprovalDecision) error {
	if authorizer.events == nil {
		return nil
	}
	if err := authorizer.events.Publish(ctx, event.ApprovalResolved{
		RequestID: request.ID, ToolName: request.ToolName, Outcome: string(decision.Outcome),
		Scope: string(decision.Scope), Source: string(decision.Source), Reason: decision.Reason,
	}); err != nil {
		return fmt.Errorf("publish tool approval resolved: %w", err)
	}
	return nil
}

func deniedDecisionError(toolName string, risk CommandRisk, decision ApprovalDecision) error {
	if decision.Allowed() {
		return nil
	}
	return &ToolDeniedError{ToolName: toolName, Risk: risk, Source: decision.Source, Reason: decision.Reason}
}

func requiredStringArgument(arguments json.RawMessage, name string) (string, error) {
	value, err := optionalStringArgument(arguments, name, "")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("tool argument %q is empty", name)
	}
	return value, nil
}

func optionalStringArgument(arguments json.RawMessage, name, fallback string) (string, error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(arguments, &values); err != nil {
		return "", fmt.Errorf("decode tool arguments for policy: %w", err)
	}
	raw, ok := values[name]
	if !ok {
		return fallback, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("decode tool argument %q for policy: %w", name, err)
	}
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return value, nil
}

var _ tool.Authorizer = (*ToolAuthorizer)(nil)
