package session

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Godric-W/Amadeus/internal/agent/turn"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/instruction"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type targetInstructionScope struct {
	mu              sync.Mutex
	contextUpdate   func(agentcontext.UpdateKey) string
	appendItems     func(context.Context, turn.ID, ...rollout.Item) error
	resolver        *instruction.WorkspaceResolver
	turnID          turn.ID
	documents       map[string]instruction.InstructionDocument
	revision        uint64
	sampledRevision uint64
}

func newTargetInstructionScope(contextUpdate func(agentcontext.UpdateKey) string, appendItems func(context.Context, turn.ID, ...rollout.Item) error, resolver *instruction.WorkspaceResolver, turnID turn.ID) (*targetInstructionScope, error) {
	if contextUpdate == nil || appendItems == nil {
		return nil, errors.New("instruction scope callbacks are incomplete")
	}
	if resolver == nil {
		return nil, errors.New("instruction scope resolver is nil")
	}
	if turnID == "" {
		return nil, errors.New("instruction scope turn ID is empty")
	}
	return &targetInstructionScope{contextUpdate: contextUpdate, appendItems: appendItems, resolver: resolver, turnID: turnID, documents: make(map[string]instruction.InstructionDocument)}, nil
}

func (scope *targetInstructionScope) Initialize(ctx context.Context, target string) (instruction.ResolveRequest, error) {
	return scope.resolve(ctx, target, instruction.TargetCommandCWD, tool.SideEffectRead)
}

func (scope *targetInstructionScope) Ensure(ctx context.Context, target tool.ContextTarget) error {
	kind, err := instructionTargetKind(target.Kind)
	if err != nil {
		return err
	}
	_, err = scope.resolve(ctx, target.Path, kind, target.SideEffect)
	return err
}

func (scope *targetInstructionScope) MarkSampled() {
	if scope == nil {
		return
	}
	scope.mu.Lock()
	scope.sampledRevision = scope.revision
	scope.mu.Unlock()
}

func (scope *targetInstructionScope) resolve(ctx context.Context, target string, kind instruction.TargetKind, effect tool.SideEffect) (instruction.ResolveRequest, error) {
	if scope == nil || scope.resolver == nil || scope.contextUpdate == nil || scope.appendItems == nil {
		return instruction.ResolveRequest{}, errors.New("instruction scope is unavailable")
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	request, resolution, err := scope.resolver.ResolveTarget(ctx, target, kind)
	if err != nil {
		if errors.Is(err, instruction.ErrTargetOutsideWorkspace) {
			return instruction.ResolveRequest{}, nil
		}
		return instruction.ResolveRequest{}, err
	}
	changed := false
	for _, document := range resolution.Documents {
		current, exists := scope.documents[document.Path]
		if !exists || current.SHA256 != document.SHA256 {
			scope.documents[document.Path] = document
			changed = true
		}
	}
	if changed || scope.revision == 0 {
		content := renderInstructionDocuments(scope.documents)
		if scope.contextUpdate(agentcontext.UpdateAgents) != content {
			item, itemErr := rollout.NewItem(rollout.KindContextUpdate, rollout.ContextUpdate{
				Key: string(agentcontext.UpdateAgents), Content: content,
				InstructionResolution: instructionResolutionFact(request, resolution),
			})
			if itemErr != nil {
				return instruction.ResolveRequest{}, itemErr
			}
			if appendErr := scope.appendItems(ctx, scope.turnID, item); appendErr != nil {
				return instruction.ResolveRequest{}, fmt.Errorf("persist target instruction resolution: %w", appendErr)
			}
		}
		scope.revision++
	}
	if (effect == tool.SideEffectWrite || effect == tool.SideEffectExecute) && scope.sampledRevision < scope.revision {
		return request, &contextRefreshRequiredError{Target: request.TargetPath}
	}
	return request, nil
}

func instructionTargetKind(kind tool.ContextTargetKind) (instruction.TargetKind, error) {
	switch kind {
	case tool.ContextTargetFile:
		return instruction.TargetFile, nil
	case tool.ContextTargetDirectory:
		return instruction.TargetDirectory, nil
	case tool.ContextTargetCommandCWD:
		return instruction.TargetCommandCWD, nil
	default:
		return "", fmt.Errorf("unsupported tool context target kind %q", kind)
	}
}

func renderInstructionDocuments(documents map[string]instruction.InstructionDocument) string {
	ordered := make([]instruction.InstructionDocument, 0, len(documents))
	for _, document := range documents {
		ordered = append(ordered, document)
	}
	sort.Slice(ordered, func(left, right int) bool {
		leftSpecificity := ordered[left].Scope.Specificity()
		rightSpecificity := ordered[right].Scope.Specificity()
		if leftSpecificity != rightSpecificity {
			return leftSpecificity < rightSpecificity
		}
		return ordered[left].Path < ordered[right].Path
	})
	parts := []string{"## Persistent Instructions", "{\"type\":\"amadeus.instructions.v1\"}"}
	for _, document := range ordered {
		if content := strings.TrimSpace(document.Content); content != "" {
			parts = append(parts, "Instructions from "+document.Path+":\n"+content)
		}
	}
	return strings.Join(parts, "\n\n")
}

func instructionResolutionFact(request instruction.ResolveRequest, resolution instruction.Resolution) *rollout.InstructionScopeResolution {
	documents := make([]rollout.InstructionDocumentRef, 0, len(resolution.Documents))
	for _, document := range resolution.Documents {
		scope := string(document.Scope.Kind)
		if document.Scope.Path != "" {
			scope += ":" + document.Scope.Path
		}
		documents = append(documents, rollout.InstructionDocumentRef{Path: document.Path, Scope: scope, SHA256: document.SHA256})
	}
	return &rollout.InstructionScopeResolution{TargetPath: request.TargetPath, TargetKind: string(request.TargetKind), Documents: documents}
}

type contextRefreshRequiredError struct {
	Target string
}

func (err *contextRefreshRequiredError) Error() string {
	return fmt.Sprintf("context_refresh_required: instructions for target %q were added; sample the updated context before retrying the side-effect tool", err.Target)
}

func (err *contextRefreshRequiredError) ToolErrorKind() string { return "context_refresh_required" }
