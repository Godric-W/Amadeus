package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/instruction"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type instructionScopeHost struct {
	manager *agentcontext.Manager
	lines   []rollout.Line
}

func (host *instructionScopeHost) AppendItems(_ context.Context, turnID protocol.TurnID, items ...rollout.RolloutItem) error {
	for _, item := range items {
		scoped := rollout.ScopeItem(item, "thread-1", turnID)
		host.lines = append(host.lines, rollout.Line{Version: rollout.CurrentVersion, Sequence: uint64(len(host.lines) + 1), Timestamp: time.Now().UTC(), Item: scoped})
	}
	return host.manager.Rebuild(host.lines)
}

func (host *instructionScopeHost) History() []rollout.Line {
	return rollout.CloneLines(host.lines)
}

func (host *instructionScopeHost) Snapshot(model llm.ModelInfo, prompt llm.Prompt) agentcontext.PromptSnapshot {
	return host.manager.Snapshot(model, prompt)
}

func (host *instructionScopeHost) ContextUpdate(key agentcontext.UpdateKey) string {
	return host.manager.Update(key)
}

func TestTargetInstructionScopePersistsNestedResolutionAndGatesMutation(t *testing.T) {
	rootPath := t.TempDir()
	nestedPath := filepath.Join(rootPath, "nested")
	if err := os.MkdirAll(nestedPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootPath, instruction.InstructionFileName), []byte("root rules"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedPath, instruction.InstructionFileName), []byte("nested rules"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := project.NewRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	user, err := instruction.NewUserLoader(t.TempDir(), instruction.UserLoaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := instruction.NewWorkspaceResolver(user, []project.Root{root}, instruction.ProjectLoaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	host := &instructionScopeHost{manager: agentcontext.NewManager(nil)}
	scope, err := newTargetInstructionScope(host.ContextUpdate, host.AppendItems, resolver, "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scope.Initialize(context.Background(), rootPath); err != nil {
		t.Fatal(err)
	}
	scope.MarkSampled()
	target := filepath.Join(nestedPath, "file.go")
	if err := scope.Ensure(context.Background(), tool.ContextTarget{Path: target, Kind: tool.ContextTargetFile, SideEffect: tool.SideEffectRead}); err != nil {
		t.Fatal(err)
	}
	content := host.ContextUpdate(agentcontext.UpdateAgents)
	if !strings.Contains(content, "root rules") || !strings.Contains(content, "nested rules") {
		t.Fatalf("nested instruction projection = %q", content)
	}
	err = scope.Ensure(context.Background(), tool.ContextTarget{Path: target, Kind: tool.ContextTargetFile, SideEffect: tool.SideEffectWrite})
	var refresh *contextRefreshRequiredError
	if !errors.As(err, &refresh) || refresh.ToolErrorKind() != "context_refresh_required" {
		t.Fatalf("mutation gate error = %v", err)
	}
	scope.MarkSampled()
	if err := scope.Ensure(context.Background(), tool.ContextTarget{Path: target, Kind: tool.ContextTargetFile, SideEffect: tool.SideEffectWrite}); err != nil {
		t.Fatalf("sampled mutation remained blocked: %v", err)
	}
	last := host.lines[len(host.lines)-1]
	eventItem, ok := last.Item.(rollout.EventMsgItem)
	if !ok {
		t.Fatalf("instruction update item = %T", last.Item)
	}
	update, ok := eventItem.Msg.(protocol.ContextUpdateEvent)
	if !ok {
		t.Fatalf("instruction update event = %T", eventItem.Msg)
	}
	if update.InstructionResolution == nil || update.InstructionResolution.TargetPath != "nested/file.go" || len(update.InstructionResolution.Documents) != 2 {
		t.Fatalf("typed instruction resolution = %#v", update.InstructionResolution)
	}
}

func TestTargetInstructionScopeGatesCommandCWDInAnotherWorkspaceRoot(t *testing.T) {
	firstPath := t.TempDir()
	secondPath := t.TempDir()
	secondNested := filepath.Join(secondPath, "nested")
	if err := os.MkdirAll(secondNested, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		filepath.Join(firstPath, instruction.InstructionFileName):    "first root rules",
		filepath.Join(secondPath, instruction.InstructionFileName):   "second root rules",
		filepath.Join(secondNested, instruction.InstructionFileName): "second nested rules",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	first, err := project.NewRoot(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := project.NewRoot(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	user, err := instruction.NewUserLoader(t.TempDir(), instruction.UserLoaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := instruction.NewWorkspaceResolver(user, []project.Root{first, second}, instruction.ProjectLoaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	host := &instructionScopeHost{manager: agentcontext.NewManager(nil)}
	scope, err := newTargetInstructionScope(host.ContextUpdate, host.AppendItems, resolver, "turn-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scope.Initialize(context.Background(), firstPath); err != nil {
		t.Fatal(err)
	}
	scope.MarkSampled()
	err = scope.Ensure(context.Background(), tool.ContextTarget{Path: secondNested, Kind: tool.ContextTargetCommandCWD, SideEffect: tool.SideEffectExecute})
	var refresh *contextRefreshRequiredError
	if !errors.As(err, &refresh) {
		t.Fatalf("cross-root command was not gated: %v", err)
	}
	content := host.ContextUpdate(agentcontext.UpdateAgents)
	if !strings.Contains(content, "second root rules") || !strings.Contains(content, "second nested rules") {
		t.Fatalf("cross-root instructions = %q", content)
	}
	eventItem, ok := host.lines[len(host.lines)-1].Item.(rollout.EventMsgItem)
	if !ok {
		t.Fatalf("instruction update item = %T", host.lines[len(host.lines)-1].Item)
	}
	update, ok := eventItem.Msg.(protocol.ContextUpdateEvent)
	if !ok {
		t.Fatalf("instruction update event = %T", eventItem.Msg)
	}
	if update.InstructionResolution == nil || update.InstructionResolution.TargetPath != "nested" || update.InstructionResolution.TargetKind != string(instruction.TargetCommandCWD) {
		t.Fatalf("command CWD resolution = %#v", update.InstructionResolution)
	}
	scope.MarkSampled()
	if err := scope.Ensure(context.Background(), tool.ContextTarget{Path: secondNested, Kind: tool.ContextTargetCommandCWD, SideEffect: tool.SideEffectExecute}); err != nil {
		t.Fatalf("sampled cross-root command remained blocked: %v", err)
	}
}
