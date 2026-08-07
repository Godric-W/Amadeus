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

type preparedRequestPermissions struct {
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
		SideEffect:  tool.SideEffectNone, Concurrency: tool.ToolConcurrencyExclusive, Idempotent: false,
	}
}

func (request *RequestPermissions) Prepare(ctx context.Context, call tool.Call) (tool.PreparedCall, error) {
	var arguments requestPermissionsArguments
	if err := decodeArguments(call.Arguments, &arguments); err != nil {
		return tool.PreparedCall{}, err
	}
	if strings.TrimSpace(arguments.Reason) == "" {
		return tool.PreparedCall{}, errors.New("request_permissions reason is empty")
	}
	roots := make([]string, 0, len(arguments.WritableRoots))
	for _, candidate := range arguments.WritableRoots {
		canonical, err := request.options.Policy.ResolveGrantRoot(candidate)
		if err != nil {
			return tool.PreparedCall{}, err
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
		return tool.PreparedCall{}, errors.New("request_permissions has no writable roots")
	}
	targets := make([]tool.PreparedTarget, 0, len(roots))
	for _, root := range roots {
		targets = append(targets, tool.PreparedTarget{Kind: tool.TargetFilesystem, RequestedPath: root, CanonicalPath: root, Access: tool.TargetAccessWrite})
	}
	return tool.NewPreparedCall(call, tool.PreparedOptions{Targets: targets, Payload: preparedRequestPermissions{arguments: arguments, roots: roots}})
}

func (request *RequestPermissions) Execute(ctx context.Context, prepared tool.PreparedCall) (tool.Result, error) {
	payload, err := preparedPayload[preparedRequestPermissions](prepared, "request_permissions")
	if err != nil {
		return tool.Result{}, err
	}
	arguments, err := json.Marshal(map[string]any{"writable_roots": payload.roots, "reason": payload.arguments.Reason})
	if err != nil {
		return tool.Result{}, err
	}
	call := prepared.Call()
	approval, err := policy.NewApprovalRequestForPurpose(call.ID, call.Name, arguments, policy.ApprovalPurposePermission, policy.CommandRiskHigh, payload.arguments.Reason)
	if err != nil {
		return tool.Result{}, err
	}
	if request.options.Events != nil {
		if err := request.options.Events.Publish(ctx, event.ApprovalRequested{RequestID: approval.ID, ToolName: approval.ToolName, Risk: string(approval.Risk), Reason: approval.Reason}); err != nil {
			return tool.Result{}, err
		}
	}
	decision, err := request.options.Approvals.Decide(ctx, approval)
	if err != nil {
		return tool.Result{}, fmt.Errorf("resolve permission request: %w", err)
	}
	if err := decision.Validate(); err != nil {
		return tool.Result{}, err
	}
	if decision.Scope == policy.ApprovalOnce {
		return tool.Result{}, errors.New("filesystem permission decision cannot use once scope")
	}
	if request.options.Events != nil {
		if err := request.options.Events.Publish(ctx, event.ApprovalResolved{RequestID: approval.ID, ToolName: approval.ToolName, Outcome: string(decision.Outcome), Scope: string(decision.Scope), Source: string(decision.Source), Reason: decision.Reason}); err != nil {
			return tool.Result{}, err
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
			return tool.Result{}, fmt.Errorf("write permission grant audit: %w", err)
		}
	}
	if !decision.Allowed() {
		return tool.Result{}, &policy.ToolDeniedError{ToolName: call.Name, Risk: policy.CommandRiskHigh, Source: decision.Source, Reason: decision.Reason}
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
		return tool.Result{}, err
	}
	encoded, _ := json.Marshal(map[string]any{"granted": true, "scope": decision.Scope, "writable_roots": payload.roots, "retry_original_tool": true})
	return tool.Result{ToolName: "request_permissions", Text: string(encoded), Metadata: map[string]any{"scope": string(decision.Scope), "writable_roots": payload.roots, "retry_original_tool": true}}, nil
}

var _ tool.Tool = (*RequestPermissions)(nil)
