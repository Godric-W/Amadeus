package runtime

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Godric-W/Amadeus/internal/agent/plan"
	"github.com/Godric-W/Amadeus/internal/agent/react"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	rundiff "github.com/Godric-W/Amadeus/internal/diff"
	"github.com/Godric-W/Amadeus/internal/instruction"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/project"
	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type FileSystemProfile struct {
	ReadHost       bool
	WorkspaceRoots []string
	TemporaryRoots []string
	ReadOnlyRoots  []string
	DeniedRoots    []string
}

func (profile FileSystemProfile) Clone() FileSystemProfile {
	profile.WorkspaceRoots = append([]string(nil), profile.WorkspaceRoots...)
	profile.TemporaryRoots = append([]string(nil), profile.TemporaryRoots...)
	profile.ReadOnlyRoots = append([]string(nil), profile.ReadOnlyRoots...)
	profile.DeniedRoots = append([]string(nil), profile.DeniedRoots...)
	return profile
}

type RunContext struct {
	Project sessiondomain.Project
	Session sessiondomain.Session
	Run     sessiondomain.Run

	Provider string
	Model    string
	Mode     sessiondomain.RunMode
	CWD      string

	FileSystem     FileSystemProfile
	ContextProfile agentcontext.ContextProfile
	Budget         react.Budget
}

func NewRunContext(project sessiondomain.Project, value sessiondomain.Session, run sessiondomain.Run, input RunContext) (*RunContext, error) {
	input.Project = project
	input.Session = value
	input.Run = run
	input.Provider = strings.TrimSpace(input.Provider)
	input.Model = strings.TrimSpace(input.Model)
	input.CWD = filepath.Clean(strings.TrimSpace(input.CWD))
	input.FileSystem = input.FileSystem.Clone()
	if err := input.Validate(); err != nil {
		return nil, err
	}
	return &input, nil
}

func (value RunContext) Validate() error {
	if err := value.Project.Validate(); err != nil {
		return fmt.Errorf("run context project: %w", err)
	}
	if err := value.Session.Validate(); err != nil {
		return fmt.Errorf("run context session: %w", err)
	}
	if err := value.Run.Validate(); err != nil {
		return fmt.Errorf("run context Run: %w", err)
	}
	if value.Session.ProjectID != value.Project.ID || value.Run.SessionID != value.Session.ID {
		return errors.New("run context records do not belong to one hierarchy")
	}
	if value.Run.Status != sessiondomain.RunRunning {
		return errors.New("run context requires a running Run")
	}
	if !value.Mode.Valid() || value.Mode != value.Run.Mode {
		return errors.New("run context mode does not match persisted Run")
	}
	if value.Provider == "" || value.Model == "" {
		return errors.New("run context Provider or model is empty")
	}
	if value.CWD == "" || !filepath.IsAbs(value.CWD) {
		return errors.New("run context cwd must be absolute")
	}
	if len(value.FileSystem.WorkspaceRoots) == 0 {
		return errors.New("run context requires at least one workspace root")
	}
	for _, root := range value.FileSystem.WorkspaceRoots {
		if !filepath.IsAbs(root) {
			return errors.New("run context workspace roots must be absolute")
		}
	}
	if err := value.ContextProfile.Validate(); err != nil {
		return fmt.Errorf("run context profile: %w", err)
	}
	if err := (react.BudgetState{Budget: value.Budget}).Validate(); err != nil {
		return fmt.Errorf("run context budget: %w", err)
	}
	return nil
}

type RunStateSnapshot struct {
	Plan         plan.Snapshot
	Usage        llm.Usage
	ToolCalls    int
	ProcessOwner string
	Terminal     bool
}

type RunState struct {
	mutex        sync.RWMutex
	plan         *plan.State
	usage        llm.Usage
	toolCalls    int
	processOwner string
	terminal     bool
	runDiff      *rundiff.Tracker
	permissions  *project.PermissionStore
}

func NewRunState(processOwner string) *RunState {
	return &RunState{plan: plan.NewState(), processOwner: strings.TrimSpace(processOwner), permissions: project.NewPermissionStore()}
}

func (state *RunState) PermissionStore() *project.PermissionStore {
	if state == nil {
		return nil
	}
	return state.permissions
}

func (state *RunState) Plan() *plan.State {
	if state == nil {
		return nil
	}
	return state.plan
}

func (state *RunState) AttachRunDiff(tracker *rundiff.Tracker) error {
	if state == nil {
		return errors.New("run state is nil")
	}
	if tracker == nil {
		return errors.New("run diff tracker is nil")
	}
	state.mutex.Lock()
	defer state.mutex.Unlock()
	if state.runDiff != nil {
		return errors.New("run state already has a Run Diff Tracker")
	}
	state.runDiff = tracker
	return nil
}

func (state *RunState) RunDiff() *rundiff.Tracker {
	if state == nil {
		return nil
	}
	state.mutex.RLock()
	defer state.mutex.RUnlock()
	return state.runDiff
}

func (state *RunState) AddUsage(usage llm.Usage) {
	if state == nil {
		return
	}
	state.mutex.Lock()
	state.usage.InputTokens += usage.InputTokens
	state.usage.OutputTokens += usage.OutputTokens
	state.usage.TotalTokens += usage.TotalTokens
	state.mutex.Unlock()
}

func (state *RunState) AddToolCalls(count int) error {
	if state == nil {
		return errors.New("run state is nil")
	}
	if count < 0 {
		return errors.New("run state tool call count cannot be negative")
	}
	state.mutex.Lock()
	state.toolCalls += count
	state.mutex.Unlock()
	return nil
}

func (state *RunState) markTerminal() bool {
	state.mutex.Lock()
	defer state.mutex.Unlock()
	if state.terminal {
		return false
	}
	state.terminal = true
	return true
}

func (state *RunState) Snapshot() RunStateSnapshot {
	if state == nil {
		return RunStateSnapshot{}
	}
	state.mutex.RLock()
	usage, toolCalls, processOwner, terminal := state.usage, state.toolCalls, state.processOwner, state.terminal
	state.mutex.RUnlock()
	return RunStateSnapshot{Plan: state.plan.Snapshot(), Usage: usage, ToolCalls: toolCalls, ProcessOwner: processOwner, Terminal: terminal}
}

type FinalState struct {
	Status           sessiondomain.RunStatus
	Reason           string
	AssistantContent string
	State            RunStateSnapshot
}

type FinishFunc func(context.Context, FinalState) error

type RunRuntime struct {
	context      *RunContext
	state        *RunState
	ctx          context.Context
	cancel       context.CancelCauseFunc
	done         chan struct{}
	finish       FinishFunc
	once         sync.Once
	cleanupMutex sync.Mutex
	cleanups     []runCleanup
	err          error
}

type runCleanup struct {
	name  string
	close func() error
}

func NewRunRuntime(parent context.Context, runContext *RunContext, finish FinishFunc) (*RunRuntime, error) {
	if parent == nil {
		return nil, errors.New("RunRuntime parent context is nil")
	}
	if runContext == nil {
		return nil, errors.New("RunRuntime context is nil")
	}
	if err := runContext.Validate(); err != nil {
		return nil, err
	}
	if finish == nil {
		return nil, errors.New("RunRuntime finish function is nil")
	}
	ctx, cancel := context.WithCancelCause(parent)
	return &RunRuntime{context: runContext, state: NewRunState(string(runContext.Run.ID)), ctx: ctx, cancel: cancel, done: make(chan struct{}), finish: finish}, nil
}

func (runtime *RunRuntime) Context() context.Context { return runtime.ctx }
func (runtime *RunRuntime) RunContext() *RunContext  { return runtime.context }
func (runtime *RunRuntime) State() *RunState         { return runtime.state }
func (runtime *RunRuntime) Done() <-chan struct{}    { return runtime.done }

func (runtime *RunRuntime) Cancel(cause error) {
	if runtime == nil || runtime.cancel == nil {
		return
	}
	if cause == nil {
		cause = context.Canceled
	}
	runtime.cancel(cause)
}

func (runtime *RunRuntime) RegisterCleanup(name string, cleanup func() error) error {
	if runtime == nil {
		return errors.New("RunRuntime is nil")
	}
	name = strings.TrimSpace(name)
	if name == "" || cleanup == nil {
		return errors.New("RunRuntime cleanup is incomplete")
	}
	runtime.cleanupMutex.Lock()
	defer runtime.cleanupMutex.Unlock()
	if runtime.state.Snapshot().Terminal {
		return errors.New("RunRuntime is already terminal")
	}
	runtime.cleanups = append(runtime.cleanups, runCleanup{name: name, close: cleanup})
	return nil
}

func (runtime *RunRuntime) Finish(ctx context.Context, status sessiondomain.RunStatus, reason, assistantContent string) error {
	if runtime == nil {
		return errors.New("RunRuntime is nil")
	}
	if !status.Terminal() {
		return errors.New("RunRuntime finish requires terminal status")
	}
	runtime.once.Do(func() {
		if !runtime.state.markTerminal() {
			runtime.err = errors.New("RunRuntime state was already terminal")
			close(runtime.done)
			return
		}
		runtime.cancel(errors.New("Run finished"))
		cleanupErr := runtime.runCleanups()
		finishErr := runtime.finish(ctx, FinalState{Status: status, Reason: strings.TrimSpace(reason), AssistantContent: strings.TrimSpace(assistantContent), State: runtime.state.Snapshot()})
		runtime.state.permissions.Clear()
		runtime.err = errors.Join(cleanupErr, finishErr)
		close(runtime.done)
	})
	return runtime.err
}

func (runtime *RunRuntime) runCleanups() error {
	runtime.cleanupMutex.Lock()
	cleanups := append([]runCleanup(nil), runtime.cleanups...)
	runtime.cleanups = nil
	runtime.cleanupMutex.Unlock()
	var cleanupErr error
	for index := len(cleanups) - 1; index >= 0; index-- {
		if err := cleanups[index].close(); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("close Run resource %s: %w", cleanups[index].name, err))
		}
	}
	return cleanupErr
}

type WorkspaceSnapshot struct {
	CWD      string
	Revision string
}

type SkillInjection = agentcontext.SkillInjection

type RequestContext struct {
	Run             *RunContext
	History         sessiondomain.HistoryView
	Tools           []tool.Spec
	Instructions    instruction.Resolution
	Workspace       WorkspaceSnapshot
	Profile         agentcontext.ContextProfile
	MCPRevision     string
	SkillRevision   string
	ToolRevision    string
	SkillInjections []SkillInjection
}

func NewRequestContext(run *RunContext, history sessiondomain.HistoryView, tools []tool.Spec, instructions instruction.Resolution, workspace WorkspaceSnapshot, profile agentcontext.ContextProfile, mcpRevision, skillRevision string, injections []SkillInjection) (RequestContext, error) {
	if run == nil {
		return RequestContext{}, errors.New("RequestContext RunContext is nil")
	}
	if history.Session.ID != "" && history.Session.ID != run.Session.ID {
		return RequestContext{}, errors.New("RequestContext history Session does not match RunContext")
	}
	if strings.TrimSpace(workspace.CWD) == "" {
		return RequestContext{}, errors.New("RequestContext workspace cwd is empty")
	}
	if err := profile.Validate(); err != nil {
		return RequestContext{}, fmt.Errorf("RequestContext profile: %w", err)
	}
	clonedTools := cloneToolSpecs(tools)
	for index, spec := range clonedTools {
		if err := tool.ValidateSpec(spec); err != nil {
			return RequestContext{}, fmt.Errorf("RequestContext Tool %d: %w", index, err)
		}
	}
	toolRevision, err := agentcontext.ToolSetRevision(clonedTools)
	if err != nil {
		return RequestContext{}, err
	}
	revisions := agentcontext.ContextRevisions{MCPBinding: strings.TrimSpace(mcpRevision), SkillCatalog: strings.TrimSpace(skillRevision), ToolExposure: toolRevision}
	if err := revisions.Validate(); err != nil {
		return RequestContext{}, err
	}
	for index, injection := range injections {
		if strings.TrimSpace(injection.Name) == "" || strings.TrimSpace(injection.Content) == "" || strings.TrimSpace(injection.ContentHash) == "" {
			return RequestContext{}, fmt.Errorf("RequestContext Skill injection %d is invalid", index)
		}
	}
	history.Items = cloneRolloutItems(history.Items)
	return RequestContext{
		Run: run, History: history, Tools: clonedTools, Instructions: instructions,
		Workspace: WorkspaceSnapshot{CWD: filepath.Clean(workspace.CWD), Revision: strings.TrimSpace(workspace.Revision)}, Profile: profile,
		MCPRevision: revisions.MCPBinding, SkillRevision: revisions.SkillCatalog, ToolRevision: revisions.ToolExposure,
		SkillInjections: append([]SkillInjection(nil), injections...),
	}, nil
}

func (request RequestContext) Clone() RequestContext {
	request.Tools = cloneToolSpecs(request.Tools)
	request.History.Items = cloneRolloutItems(request.History.Items)
	request.Instructions.Documents = append([]instruction.InstructionDocument(nil), request.Instructions.Documents...)
	request.SkillInjections = append([]SkillInjection(nil), request.SkillInjections...)
	return request
}

func cloneToolSpecs(specs []tool.Spec) []tool.Spec {
	result := make([]tool.Spec, len(specs))
	for index := range specs {
		result[index] = specs[index].Clone()
	}
	return result
}

func cloneRolloutItems(items []sessiondomain.RolloutItem) []sessiondomain.RolloutItem {
	result := make([]sessiondomain.RolloutItem, len(items))
	for index, item := range items {
		item.PayloadJSON = append([]byte(nil), item.PayloadJSON...)
		result[index] = item
	}
	return result
}
