package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/snapshot"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type RevertRun struct{ snapshots snapshot.Service }
type revertRunArguments struct {
	RunID string `json:"run_id"`
}

func NewRevertRun(snapshots snapshot.Service) (*RevertRun, error) {
	if snapshots == nil {
		return nil, errors.New("revert_run snapshot service is nil")
	}
	return &RevertRun{snapshots: snapshots}, nil
}
func (revert *RevertRun) Spec() tool.Spec { return revertRunSpec() }
func (revert *RevertRun) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	if revert == nil || revert.snapshots == nil {
		return tool.Result{}, errors.New("revert_run is not initialized")
	}
	var arguments revertRunArguments
	if err := decodeArguments(input, &arguments); err != nil {
		return tool.Result{}, err
	}
	arguments.RunID = strings.TrimSpace(arguments.RunID)
	if arguments.RunID == "" {
		return tool.Result{}, errors.New("revert_run run_id is empty")
	}
	result, err := revert.snapshots.Revert(ctx, arguments.RunID)
	if err != nil {
		return tool.Result{}, err
	}
	return tool.Result{ToolName: "revert_run", Text: fmt.Sprintf("reverted %d file changes from run %s", len(result.Changes), result.RunID), Metadata: map[string]any{"run_id": result.RunID, "changed_files": len(result.Changes)}}, nil
}
func revertRunSpec() tool.Spec {
	return tool.Spec{Name: "revert_run", Description: "Restore project files captured before a completed Run snapshot. This is a write operation and requires explicit approval.", InputSchema: json.RawMessage(`{"type":"object","properties":{"run_id":{"type":"string","minLength":1,"maxLength":160}},"required":["run_id"],"additionalProperties":false}`), SideEffect: tool.SideEffectWrite, ParallelSafe: false, Idempotent: false, ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeExclusive}}
}

var _ tool.Tool = (*RevertRun)(nil)
