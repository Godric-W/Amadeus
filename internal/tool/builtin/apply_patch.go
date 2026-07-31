package builtin

import (
	"context"
	"encoding/json"
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
	Apply(context.Context, patchtool.Document) (patchtool.ApplyResult, error)
}

type ApplyPatch struct {
	parseOptions patchtool.ParseOptions
	executor     patchApplier
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
	return &ApplyPatch{parseOptions: options.Parse, executor: executor}, nil
}

func (applyPatch *ApplyPatch) Spec() tool.Spec {
	return applyPatchSpec()
}

func (applyPatch *ApplyPatch) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	if err := ctx.Err(); err != nil {
		return tool.Result{}, err
	}
	var arguments applyPatchArguments
	if err := decodeArguments(input, &arguments); err != nil {
		return tool.Result{}, err
	}
	if strings.TrimSpace(arguments.Patch) == "" {
		return tool.Result{}, errors.New("apply_patch patch is empty")
	}
	document, err := patchtool.Parse([]byte(arguments.Patch), applyPatch.parseOptions)
	if err != nil {
		return tool.Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return tool.Result{}, err
	}
	applied, applyErr := applyPatch.executor.Apply(ctx, document)
	result := patchToolResult(document, applied, applyErr)
	return result, applyErr
}

func patchToolResult(document patchtool.Document, applied patchtool.ApplyResult, applyErr error) tool.Result {
	operations := make([]map[string]any, len(applied.Applied))
	for index, operation := range applied.Applied {
		operations[index] = map[string]any{
			"kind": operation.Kind, "path": operation.Path, "bytes": operation.Bytes,
			"created": operation.Created, "deleted": operation.Deleted,
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
