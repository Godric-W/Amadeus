package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type RequestPermissionsOptions struct {
	Policy             *project.FileSystemPolicy
	RunPermissions     *project.PermissionStore
	SessionPermissions *project.PermissionStore
	Approvals          policy.ApprovalHandler
	Events             event.Sink
	Audit              audit.Sink
	Now                func() time.Time
}

type RequestPermissions struct{ options RequestPermissionsOptions }

type requestPermissionsArguments struct {
	WritableRoots []string `json:"writable_roots"`
	Reason        string   `json:"reason"`
}

type permissionRequest struct {
	arguments requestPermissionsArguments
	roots     []string
}

func NewRequestPermissions(options RequestPermissionsOptions) (*RequestPermissions, error) {
	if options.Policy == nil || options.RunPermissions == nil || options.SessionPermissions == nil {
		return nil, errors.New("request_permissions policy or permission store is nil")
	}
	if options.Approvals == nil {
		return nil, errors.New("request_permissions approval handler is nil")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &RequestPermissions{options: options}, nil
}

func (request *RequestPermissions) Spec() tool.Spec {
	return tool.Spec{
		Name:        "request_permissions",
		Description: "Request additional writable directory roots after another tool returned permission_required. Ask only for the minimal roots needed, then retry the original tool after approval.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"writable_roots":{"type":"array","minItems":1,"maxItems":16,"items":{"type":"string","minLength":1},"uniqueItems":true},"reason":{"type":"string","minLength":1}},"required":["writable_roots","reason"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectNone, Idempotent: false,
	}
}

func (request *RequestPermissions) SupportsParallelToolCalls() bool { return false }

func (request *RequestPermissions) Handle(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	call := invocation.Call
	var arguments requestPermissionsArguments
	if err := decodeArguments(call.Payload, &arguments); err != nil {
		return tool.Output{}, err
	}
	if strings.TrimSpace(arguments.Reason) == "" {
		return tool.Output{}, errors.New("request_permissions reason is empty")
	}
	roots := make([]string, 0, len(arguments.WritableRoots))
	for _, candidate := range arguments.WritableRoots {
		canonical, err := request.options.Policy.ResolveGrantRoot(candidate)
		if err != nil {
			return tool.Output{}, err
		}
		unique := true
		for _, root := range roots {
			if root == canonical {
				unique = false
				break
			}
		}
		if unique {
			roots = append(roots, canonical)
		}
	}
	if len(roots) == 0 {
		return tool.Output{}, errors.New("request_permissions has no writable roots")
	}
	payload := permissionRequest{arguments: arguments, roots: roots}
	encodedArguments, err := json.Marshal(map[string]any{"writable_roots": payload.roots, "reason": payload.arguments.Reason})
	if err != nil {
		return tool.Output{}, err
	}
	approval, err := policy.NewApprovalRequestForPurpose(call.ID, call.Name, encodedArguments, policy.ApprovalPurposePermission, policy.CommandRiskHigh, payload.arguments.Reason)
	if err != nil {
		return tool.Output{}, err
	}
	if request.options.Events != nil {
		if err := request.options.Events.Publish(ctx, event.ApprovalRequested{RequestID: approval.ID, ToolName: approval.ToolName, Risk: string(approval.Risk), Reason: approval.Reason}); err != nil {
			return tool.Output{}, err
		}
	}
	decision, err := request.options.Approvals.Decide(ctx, approval)
	if err != nil {
		return tool.Output{}, fmt.Errorf("resolve permission request: %w", err)
	}
	if err := decision.Validate(); err != nil {
		return tool.Output{}, err
	}
	if decision.Scope == policy.ApprovalOnce {
		return tool.Output{}, errors.New("filesystem permission decision cannot use once scope")
	}
	if request.options.Events != nil {
		if err := request.options.Events.Publish(ctx, event.ApprovalResolved{RequestID: approval.ID, ToolName: approval.ToolName, Outcome: string(decision.Outcome), Scope: string(decision.Scope), Source: string(decision.Source), Reason: decision.Reason}); err != nil {
			return tool.Output{}, err
		}
	}
	if request.options.Audit != nil {
		metadata := event.MetadataFromContext(ctx)
		record := audit.Record{
			Timestamp: request.options.Now().UTC(), SessionID: metadata.SessionID, RequestID: approval.ID, ToolName: approval.ToolName,
			ArgumentsSHA256: approval.ArgumentsSHA256, Risk: string(approval.Risk), Outcome: audit.OutcomeDeny,
			Scope: string(decision.Scope), Source: string(decision.Source), Reason: decision.Reason,
		}
		if decision.Allowed() {
			record.Outcome = audit.OutcomeAllow
		}
		if err := request.options.Audit.Write(ctx, record); err != nil {
			return tool.Output{}, fmt.Errorf("write permission grant audit: %w", err)
		}
	}
	if !decision.Allowed() {
		return tool.Output{}, &policy.ToolDeniedError{ToolName: call.Name, Risk: policy.CommandRiskHigh, Source: decision.Source, Reason: decision.Reason}
	}
	switch decision.Scope {
	case policy.ApprovalRun:
		err = request.options.RunPermissions.GrantWritableRoots(payload.roots)
	case policy.ApprovalSession:
		err = request.options.SessionPermissions.GrantWritableRoots(payload.roots)
	default:
		err = fmt.Errorf("permission approval scope %q is invalid", decision.Scope)
	}
	if err != nil {
		return tool.Output{}, err
	}
	encoded, _ := json.Marshal(map[string]any{"granted": true, "scope": decision.Scope, "writable_roots": payload.roots, "retry_original_tool": true})
	return tool.Output{ToolName: "request_permissions", Text: string(encoded), Metadata: map[string]any{"scope": string(decision.Scope), "writable_roots": payload.roots, "retry_original_tool": true}}, nil
}

var _ tool.Handler = (*RequestPermissions)(nil)
