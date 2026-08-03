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

type RevertTurn struct {
	snapshots snapshot.Service
}

type revertTurnArguments struct {
	RunID string `json:"run_id"`
}

func NewRevertTurn(snapshots snapshot.Service) (*RevertTurn, error) {
	if snapshots == nil {
		return nil, errors.New("revert_turn snapshot service is nil")
	}
	return &RevertTurn{snapshots: snapshots}, nil
}

func (revert *RevertTurn) Spec() tool.Spec {
	return revertTurnSpec()
}

func (revert *RevertTurn) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	if revert == nil || revert.snapshots == nil {
		return tool.Result{}, errors.New("revert_turn is not initialized")
	}
	var arguments revertTurnArguments
	if err := decodeArguments(input, &arguments); err != nil {
		return tool.Result{}, err
	}
	arguments.RunID = strings.TrimSpace(arguments.RunID)
	if arguments.RunID == "" {
		return tool.Result{}, errors.New("revert_turn run_id is empty")
	}
	result, err := revert.snapshots.Revert(ctx, arguments.RunID)
	if err != nil {
		return tool.Result{}, err
	}
	metadata := map[string]any{"run_id": result.RunID, "changed_files": len(result.Changes)}
	return tool.Result{Text: fmt.Sprintf("reverted %d file changes from run %s", len(result.Changes), result.RunID), Metadata: metadata}, nil
}

func revertTurnSpec() tool.Spec {
	return tool.Spec{
		Name: "revert_turn", Description: "Restore the project files captured before a completed Run snapshot. This is a write operation and requires explicit approval.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"run_id":{"type":"string","minLength":1,"maxLength":160}},"required":["run_id"],"additionalProperties":false}`),
		SideEffect:  tool.SideEffectWrite, ParallelSafe: false, Idempotent: false,
		ResourceStrategy: tool.ResourceStrategy{Mode: tool.ResourceModeExclusive},
	}
}

var _ tool.Tool = (*RevertTurn)(nil)
