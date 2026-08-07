package builtin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/tool"
	patchtool "github.com/Godric-W/Amadeus/internal/tool/patch"
)

type ApplyPatchOptions struct {
	Parse    patchtool.ParseOptions
	Executor patchtool.ExecutorOptions
}

type patchApplier interface {
	Prepare(context.Context, patchtool.Document) (*patchtool.PreparedDocument, error)
	ApplyPrepared(context.Context, *patchtool.PreparedDocument) (patchtool.ApplyResult, error)
}

type ApplyPatch struct {
	parseOptions patchtool.ParseOptions
	executor     patchApplier
}

type applyPatchArguments struct {
	Patch string `json:"patch"`
}

type preparedApplyPatch struct {
	arguments applyPatchArguments
	document  patchtool.Document
	prepared  *patchtool.PreparedDocument
}

func NewApplyPatch(root project.Root, options ApplyPatchOptions) (*ApplyPatch, error) {
	executor, err := patchtool.NewExecutor(root, options.Executor)
	if err != nil {
		return nil, err
	}
	return newApplyPatch(options, executor)
}

func newApplyPatch(options ApplyPatchOptions, executor patchApplier) (*ApplyPatch, error) {
	if executor == nil {
		return nil, errors.New("apply_patch executor is nil")
	}
	return &ApplyPatch{parseOptions: options.Parse, executor: executor}, nil
}

func (applyPatch *ApplyPatch) Spec() tool.Spec {
	return applyPatchSpec()
}

func (applyPatch *ApplyPatch) Prepare(ctx context.Context, call tool.Call) (tool.PreparedCall, error) {
	if err := ctx.Err(); err != nil {
		return tool.PreparedCall{}, err
	}
	var arguments applyPatchArguments
	if err := decodeArguments(call.Arguments, &arguments); err != nil {
		return tool.PreparedCall{}, err
	}
	if strings.TrimSpace(arguments.Patch) == "" {
		return tool.PreparedCall{}, errors.New("apply_patch patch is empty")
	}
	document, err := patchtool.Parse([]byte(arguments.Patch), applyPatch.parseOptions)
	if err != nil {
		return tool.PreparedCall{}, err
	}
	preparedDocument, err := applyPatch.executor.Prepare(ctx, document)
	if err != nil {
		return tool.PreparedCall{}, err
	}
	targets := make([]tool.PreparedTarget, 0, len(preparedDocument.Targets()))
	for _, target := range preparedDocument.Targets() {
		targets = append(targets, tool.PreparedTarget{Kind: tool.TargetFilesystem, RequestedPath: target.Requested, CanonicalPath: target.Canonical, Access: tool.TargetAccessWrite})
	}
	return tool.NewPreparedCall(call, tool.PreparedOptions{Targets: targets, Payload: preparedApplyPatch{arguments: arguments, document: document, prepared: preparedDocument}})
}

func (applyPatch *ApplyPatch) Execute(ctx context.Context, prepared tool.PreparedCall) (tool.Result, error) {
	payload, err := preparedPayload[preparedApplyPatch](prepared, "apply_patch")
	if err != nil {
		return tool.Result{}, err
	}
	applied, applyErr := applyPatch.executor.ApplyPrepared(ctx, payload.prepared)
	result := patchToolResult(payload.document, applied, applyErr)
	return result, applyErr
}

func patchToolResult(document patchtool.Document, applied patchtool.ApplyResult, applyErr error) tool.Result {
	operations := make([]map[string]any, len(applied.Applied))
	for index, operation := range applied.Applied {
		operations[index] = map[string]any{
			"kind": operation.Kind, "path": operation.Path, "bytes": operation.Bytes,
			"destination": operation.Destination, "created": operation.Created, "deleted": operation.Deleted, "moved": operation.Moved,
		}
	}
	text := fmt.Sprintf("applied %d patch operation(s)", len(applied.Applied))
	if applyErr != nil {
		text = fmt.Sprintf("applied %d of %d patch operation(s) before failure", len(applied.Applied), len(document.Operations))
	}
	return tool.Result{
		ToolName: "apply_patch", Text: text, Partial: applied.Partial,
		Metadata: map[string]any{
			"operations": operations, "operation_count": len(applied.Applied),
			"total_operations": len(document.Operations), "partial": applied.Partial,
		},
	}
}

var _ tool.Tool = (*ApplyPatch)(nil)
