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
	Parse     patchtool.ParseOptions
	Executor  patchtool.ExecutorOptions
	Projector PatchProjector
}

type patchApplier interface {
	PreparePatch(context.Context, patchtool.Document) (*patchtool.PreparedPatch, error)
	ApplyPrepared(context.Context, *patchtool.PreparedPatch) (patchtool.ApplyResult, error)
}

type PatchProjector interface {
	ProjectPatch(context.Context, []patchtool.AppliedPatchDelta) error
}

type ApplyPatch struct {
	parseOptions patchtool.ParseOptions
	executor     patchApplier
	projector    PatchProjector
}

type applyPatchArguments struct {
	Patch string `json:"patch"`
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
	return &ApplyPatch{parseOptions: options.Parse, executor: executor, projector: options.Projector}, nil
}

func (applyPatch *ApplyPatch) Spec() tool.Spec {
	return applyPatchSpec()
}

func (applyPatch *ApplyPatch) SupportsParallelToolCalls() bool { return false }

func (applyPatch *ApplyPatch) Handle(ctx context.Context, invocation tool.Invocation) (tool.Output, error) {
	call := invocation.Call
	if err := ctx.Err(); err != nil {
		return tool.Output{}, err
	}
	var arguments applyPatchArguments
	if err := decodeArguments(call.Payload, &arguments); err != nil {
		return tool.Output{}, err
	}
	if strings.TrimSpace(arguments.Patch) == "" {
		return tool.Output{}, errors.New("apply_patch patch is empty")
	}
	document, err := patchtool.Parse([]byte(arguments.Patch), applyPatch.parseOptions)
	if err != nil {
		return tool.Output{}, err
	}
	preparedPatch, err := applyPatch.executor.PreparePatch(ctx, document)
	if err != nil {
		return tool.Output{}, err
	}
	applied, applyErr := applyPatch.executor.ApplyPrepared(ctx, preparedPatch)
	result := patchToolResult(document, applied, applyErr)
	if applyPatch.projector != nil && len(applied.Applied) > 0 {
		deltas := make([]patchtool.AppliedPatchDelta, 0, len(applied.Applied))
		for _, operation := range applied.Applied {
			deltas = append(deltas, operation.Delta)
		}
		if projectErr := applyPatch.projector.ProjectPatch(ctx, deltas); projectErr != nil {
			applyErr = errors.Join(applyErr, fmt.Errorf("project apply_patch diff: %w", projectErr))
		}
	}
	return result, applyErr
}

func patchToolResult(document patchtool.Document, applied patchtool.ApplyResult, applyErr error) tool.Output {
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
	return tool.Output{
		ToolName: "apply_patch", Text: text, Partial: applied.Partial,
		Metadata: map[string]any{
			"operations": operations, "operation_count": len(applied.Applied),
			"total_operations": len(document.Operations), "partial": applied.Partial,
		},
	}
}

var _ tool.Handler = (*ApplyPatch)(nil)
