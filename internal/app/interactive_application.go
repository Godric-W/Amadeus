package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	agentsession "github.com/Godric-W/Amadeus/internal/agent/session"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/Godric-W/Amadeus/internal/state"
	"github.com/Godric-W/Amadeus/internal/thread"
	threadmanager "github.com/Godric-W/Amadeus/internal/thread/manager"
)

type InteractiveOptions struct {
	Workspace     *ThreadWorkspace
	Configuration agentsession.Configuration
	Project       string
	Provider      string
	Model         string
	ContextWindow int64
	MaxTaskBytes  int
}

type InteractiveApplication struct {
	workspace     *ThreadWorkspace
	configuration agentsession.Configuration
	project       string
	provider      string
	model         string
	contextWindow int64
	maxTaskBytes  int
	operationMu   sync.Mutex

	mu               sync.RWMutex
	active           *threadmanager.AmadeusThread
	generation       uint64
	attachmentCancel context.CancelFunc
	phase            string
	title            string
	usage            protocol.ThreadTokenUsageUpdated

	ctx    context.Context
	cancel context.CancelFunc
	events chan InteractiveEvent
}

func NewInteractiveApplication(parent context.Context, options InteractiveOptions) (*InteractiveApplication, error) {
	if parent == nil || options.Workspace == nil {
		return nil, errors.New("interactive application options are incomplete")
	}
	ctx, cancel := context.WithCancel(parent)
	return &InteractiveApplication{
		workspace: options.Workspace, configuration: options.Configuration,
		project: options.Project, provider: options.Provider, model: options.Model,
		contextWindow: options.ContextWindow, maxTaskBytes: options.MaxTaskBytes,
		ctx: ctx, cancel: cancel, events: make(chan InteractiveEvent, 256), phase: "idle",
	}, nil
}

func (application *InteractiveApplication) Start(ctx context.Context) (ThreadViewSnapshot, error) {
	active, err := application.workspace.EnsureCurrent(ctx, application.configuration)
	if err != nil {
		return ThreadViewSnapshot{}, err
	}
	snapshot, err := application.snapshot(ctx, active, application.nextGeneration())
	if err != nil {
		return ThreadViewSnapshot{}, err
	}
	application.installAttachment(active, snapshot)
	return snapshot, nil
}

func (application *InteractiveApplication) Events() <-chan InteractiveEvent {
	if application == nil {
		return nil
	}
	return application.events
}

func (application *InteractiveApplication) SubmitUser(ctx context.Context, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return errors.New("interactive task is empty")
	}
	if application.maxTaskBytes > 0 && len(content) > application.maxTaskBytes {
		return fmt.Errorf("interactive task exceeds %d bytes", application.maxTaskBytes)
	}
	active, _, err := application.current()
	if err != nil {
		return err
	}
	return active.Submit(ctx, protocol.UserInputOp{Content: content})
}

func (application *InteractiveApplication) SubmitCompact(ctx context.Context) error {
	active, _, err := application.current()
	if err != nil {
		return err
	}
	return active.Submit(ctx, protocol.CompactOp{})
}

func (application *InteractiveApplication) SetMode(ctx context.Context, mode turn.ModeKind) error {
	if mode != turn.ModeKindDefault && mode != turn.ModeKindPlan {
		return fmt.Errorf("collaboration mode %q is invalid", mode)
	}
	active, _, err := application.current()
	if err != nil {
		return err
	}
	return active.Submit(ctx, protocol.ThreadSettingsOp{Mode: string(mode)})
}

func (application *InteractiveApplication) Interrupt(ctx context.Context) error {
	active, _, err := application.current()
	if err != nil {
		return err
	}
	return active.Submit(ctx, protocol.InterruptOp{})
}

func (application *InteractiveApplication) ResolveApproval(ctx context.Context, requestID string, decision policy.ApprovalDecision) error {
	active, _, err := application.current()
	if err != nil {
		return err
	}
	return active.Submit(ctx, protocol.ApprovalDecisionOp{
		RequestID: requestID, OptionID: decision.OptionID, Outcome: string(decision.Outcome),
		Scope: string(decision.Scope), Source: string(decision.Source), Reason: decision.Reason,
	})
}

func (application *InteractiveApplication) LoadSessions(ctx context.Context) {
	values, err := application.workspace.List(ctx, state.ListQuery{CWD: application.project})
	current, _ := application.workspace.Current()
	options := make([]SessionOption, 0, len(values))
	for _, value := range values {
		options = append(options, SessionOption{ID: value.ID, Title: value.Title, Current: current != nil && value.ID == current.ID()})
	}
	application.emit(SessionsLoaded{Sessions: options, Error: err})
}

func (application *InteractiveApplication) Resume(ctx context.Context, id rollout.ThreadID) {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	prepared, err := application.workspace.PrepareResume(ctx, thread.ID(id), application.configuration)
	if err != nil {
		application.emit(ThreadAttachFailed{Error: err})
		return
	}
	target := prepared.Target()
	generation := application.nextGeneration()
	snapshot, err := application.snapshot(ctx, target, generation)
	if err != nil {
		_ = prepared.Abort(context.WithoutCancel(ctx))
		application.emit(ThreadAttachFailed{Error: err})
		return
	}
	if err := prepared.Commit(); err != nil {
		_ = prepared.Abort(context.WithoutCancel(ctx))
		application.emit(ThreadAttachFailed{Error: err})
		return
	}
	previous := prepared.Previous()
	application.installAttachment(target, snapshot)
	application.emit(ThreadAttached{Snapshot: snapshot})
	application.releasePrevious(previous, target)
}

func (application *InteractiveApplication) Clear(ctx context.Context) {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	prepared, err := application.workspace.PrepareNew(ctx, application.configuration)
	if err != nil {
		application.emit(ThreadAttachFailed{Error: err})
		return
	}
	target := prepared.Target()
	generation := application.nextGeneration()
	snapshot, err := application.snapshot(ctx, target, generation)
	if err != nil {
		_ = prepared.Abort(context.WithoutCancel(ctx))
		application.emit(ThreadAttachFailed{Error: err})
		return
	}
	if err := prepared.Commit(); err != nil {
		_ = prepared.Abort(context.WithoutCancel(ctx))
		application.emit(ThreadAttachFailed{Error: err})
		return
	}
	previous := prepared.Previous()
	application.installAttachment(target, snapshot)
	application.emit(ClearUIStarted{})
	application.emit(ThreadAttached{Snapshot: snapshot})
	application.releasePrevious(previous, target)
}

func (application *InteractiveApplication) Rename(ctx context.Context, generation uint64, name string) {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		application.emit(ThreadRenameFailed{Error: errors.New("thread name cannot be empty")})
		return
	}
	active, currentGeneration, err := application.current()
	if err != nil {
		application.emit(ThreadRenameFailed{Error: err})
		return
	}
	if generation != currentGeneration {
		application.emit(ThreadRenameFailed{Error: ErrThreadSelectionChanged})
		return
	}
	if err := application.workspace.RenameCurrent(ctx, name); err != nil {
		application.emit(ThreadRenameFailed{Error: err})
		return
	}
	application.mu.Lock()
	application.title = name
	application.mu.Unlock()
	application.emit(ThreadNameUpdated{Generation: generation, ThreadID: active.ID(), Name: name})
}

func (application *InteractiveApplication) Delete(ctx context.Context, generation uint64) {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	active, currentGeneration, err := application.current()
	if err != nil {
		application.emit(ThreadDeleteFailed{Error: err})
		return
	}
	if generation != currentGeneration {
		application.emit(ThreadDeleteFailed{Error: ErrThreadSelectionChanged})
		return
	}
	application.stopAttachment()
	deleted, err := application.workspace.DeleteCurrent(ctx)
	if err != nil {
		application.installAttachment(active, application.currentSnapshot(active, generation))
		application.emit(ThreadDeleteFailed{Error: err})
		return
	}
	application.emit(ThreadDeleted{ThreadID: deleted})
}

func (application *InteractiveApplication) Status() StatusSnapshot {
	active, generation, err := application.current()
	if err != nil {
		return StatusSnapshot{Project: application.project, Provider: application.provider, Model: application.model, Phase: "unavailable", ContextWindow: application.contextWindow}
	}
	application.mu.RLock()
	title := application.title
	phase := application.phase
	usage := application.usage
	application.mu.RUnlock()
	result := StatusSnapshot{
		ThreadID: active.ID(), Title: title, Project: application.project,
		Provider: application.provider, Model: application.model, Mode: active.Mode(), Phase: phase,
		Usage: usage.Usage, ContextWindow: application.contextWindow, RolloutItems: len(active.History()),
	}
	if capabilities, ok := active.CapabilityView(); ok {
		result.PermissionGrantCount = capabilities.PermissionGrantCount()
		result.SkillRevision = shortRevision(capabilities.SkillRevision())
		result.MCPRevision = shortRevision(capabilities.MCPRevision())
	}
	_ = generation
	return result
}

func (application *InteractiveApplication) LoadMCP(ctx context.Context, requestID uint64, detail MCPDetail) {
	active, generation, err := application.current()
	if err != nil {
		application.emit(MCPInventoryLoaded{RequestID: requestID, Detail: detail, Error: err})
		return
	}
	result := MCPInventoryLoaded{RequestID: requestID, Generation: generation, ThreadID: active.ID(), Detail: detail}
	capabilities, ok := active.CapabilityView()
	if !ok {
		result.Error = errors.New("active thread capabilities are unavailable")
		application.emit(result)
		return
	}
	configured := capabilities.MCPConfiguration()
	servers := make([]string, 0, len(configured.Servers))
	for server := range configured.Servers {
		servers = append(servers, server)
	}
	sort.Strings(servers)
	for _, server := range servers {
		serverConfig := configured.Servers[server]
		status := MCPServerStatus{Name: server, Enabled: serverConfig.IsEnabled(), AuthStatus: mcpAuthStatus(serverConfig)}
		if !status.Enabled {
			result.Inventory.Servers = append(result.Inventory.Servers, status)
			continue
		}
		catalog, listErr := capabilities.MCPTools(ctx, server)
		if listErr != nil {
			status.Error = listErr.Error()
		} else {
			status.Tools = append(status.Tools, catalog.Tools...)
		}
		if detail == MCPDetailVerbose {
			resources, resourceErr := capabilities.MCPResources(ctx, server)
			if resourceErr != nil {
				if status.Error == "" {
					status.Error = resourceErr.Error()
				} else {
					status.Error += "; " + resourceErr.Error()
				}
			} else {
				status.Resources = append(status.Resources, resources.Resources...)
			}
		}
		result.Inventory.Servers = append(result.Inventory.Servers, status)
	}
	application.emit(result)
}

func mcpAuthStatus(server mcp.ServerConfig) MCPAuthStatus {
	for name, value := range server.Headers {
		if strings.EqualFold(strings.TrimSpace(name), "Authorization") && strings.TrimSpace(value) != "" {
			return MCPAuthBearerToken
		}
	}
	if server.Transport == mcp.TransportStreamableHTTP {
		return MCPAuthUnsupported
	}
	return MCPAuthUnknown
}

func (application *InteractiveApplication) LoadSkills() {
	active, generation, err := application.current()
	if err != nil {
		application.emit(SkillsLoaded{Error: err})
		return
	}
	capabilities, ok := active.CapabilityView()
	if !ok {
		application.emit(SkillsLoaded{Generation: generation, Error: errors.New("active thread capabilities are unavailable")})
		return
	}
	metadata := capabilities.Skills()
	values := make([]SkillOption, 0, len(metadata))
	for _, value := range metadata {
		values = append(values, SkillOption{
			Name: value.Name, Description: value.Description, Source: string(value.Source),
			Path: value.PathToSkillMD, Enabled: value.Enabled,
		})
	}
	application.emit(SkillsLoaded{Generation: generation, Skills: values})
}

func (application *InteractiveApplication) SetSkillEnabled(path string, enabled bool) {
	active, generation, err := application.current()
	if err != nil {
		application.emit(SkillEnabledSet{Generation: generation, Path: path, Enabled: enabled, Error: err})
		return
	}
	capabilities, ok := active.CapabilityView()
	if !ok {
		application.emit(SkillEnabledSet{Generation: generation, Path: path, Enabled: enabled, Error: errors.New("active thread capabilities are unavailable")})
		return
	}
	name := ""
	for _, value := range capabilities.Skills() {
		if value.PathToSkillMD == path {
			name = value.Name
			break
		}
	}
	if name == "" {
		application.emit(SkillEnabledSet{Generation: generation, Path: path, Enabled: enabled, Error: fmt.Errorf("skill path %q is unavailable", path)})
		return
	}
	err = capabilities.SetSkillEnabled(name, enabled)
	application.emit(SkillEnabledSet{Generation: generation, Path: path, Enabled: enabled, Error: err})
}

func (application *InteractiveApplication) Shutdown(ctx context.Context) {
	application.operationMu.Lock()
	defer application.operationMu.Unlock()
	application.emit(ShutdownStarted{})
	err := application.workspace.Close(ctx)
	application.stopAttachment()
	application.emit(ShutdownFinished{Error: err})
}

func (application *InteractiveApplication) Close() {
	if application == nil {
		return
	}
	application.stopAttachment()
	application.cancel()
}

func (application *InteractiveApplication) snapshot(ctx context.Context, active *threadmanager.AmadeusThread, generation uint64) (ThreadViewSnapshot, error) {
	projection, err := protocol.ProjectThreadItems(active.History())
	if err != nil {
		return ThreadViewSnapshot{}, err
	}
	title := "draft"
	metadata, metadataErr := application.metadata(ctx, active.ID())
	if metadataErr == nil {
		title = metadata.Title
	} else if !errors.Is(metadataErr, state.ErrNotFound) {
		return ThreadViewSnapshot{}, metadataErr
	}
	return ThreadViewSnapshot{
		Generation: generation, ThreadID: active.ID(), Title: title, Mode: active.Mode(),
		Items: projection.Items, Usage: projection.Usage, ContextWindow: application.contextWindow,
		Provider: application.provider, Model: application.model,
	}, nil
}

func (application *InteractiveApplication) metadata(ctx context.Context, id thread.ID) (state.StoredThread, error) {
	values, err := application.workspace.List(ctx, state.ListQuery{CWD: application.project, IncludeArchived: true})
	if err != nil {
		return state.StoredThread{}, err
	}
	for _, value := range values {
		if value.ID == id {
			return value, nil
		}
	}
	return state.StoredThread{}, state.ErrNotFound
}

func (application *InteractiveApplication) nextGeneration() uint64 {
	application.mu.RLock()
	next := application.generation + 1
	application.mu.RUnlock()
	return next
}

func (application *InteractiveApplication) installAttachment(active *threadmanager.AmadeusThread, snapshot ThreadViewSnapshot) {
	application.mu.Lock()
	if application.attachmentCancel != nil {
		application.attachmentCancel()
	}
	ctx, cancel := context.WithCancel(application.ctx)
	application.active = active
	application.generation = snapshot.Generation
	application.attachmentCancel = cancel
	application.title = snapshot.Title
	application.phase = "idle"
	application.usage = protocol.ThreadTokenUsageUpdated{Usage: snapshot.Usage, ContextWindow: snapshot.ContextWindow}
	application.mu.Unlock()
	go application.pumpAttachment(ctx, active, snapshot.Generation)
}

func (application *InteractiveApplication) currentSnapshot(active *threadmanager.AmadeusThread, generation uint64) ThreadViewSnapshot {
	application.mu.RLock()
	defer application.mu.RUnlock()
	return ThreadViewSnapshot{
		Generation: generation, ThreadID: active.ID(), Title: application.title, Mode: active.Mode(),
		Usage: application.usage.Usage, ContextWindow: application.contextWindow,
		Provider: application.provider, Model: application.model,
	}
}

func (application *InteractiveApplication) stopAttachment() {
	application.mu.Lock()
	if application.attachmentCancel != nil {
		application.attachmentCancel()
		application.attachmentCancel = nil
	}
	application.mu.Unlock()
}

func (application *InteractiveApplication) pumpAttachment(ctx context.Context, active *threadmanager.AmadeusThread, generation uint64) {
	io := active.Io()
	events := io.Events
	requests := io.Requests
	statuses := io.Status
	terminated := io.Terminated
	for events != nil || requests != nil || statuses != nil || terminated != nil {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			application.observeSessionEvent(generation, event)
			application.emit(SessionEventObserved{Generation: generation, Event: event})
		case request, ok := <-requests:
			if !ok {
				requests = nil
				continue
			}
			if request.Kind != protocol.RequestApproval || request.Approval == nil || len(request.Approval.Raw) == 0 {
				application.emit(ApplicationError{Operation: "interactive request", Error: fmt.Errorf("unsupported interactive request %q", request.Kind)})
				continue
			}
			var approval policy.ApprovalRequest
			if err := json.Unmarshal(request.Approval.Raw, &approval); err != nil {
				application.emit(ApplicationError{Operation: "decode approval request", Error: err})
				continue
			}
			application.emit(ApprovalRequested{Generation: generation, RequestID: request.RequestID, Request: approval})
		case status, ok := <-statuses:
			if !ok {
				statuses = nil
				continue
			}
			application.emit(AgentStatusChanged{Generation: generation, Status: status})
		case _, ok := <-terminated:
			if !ok {
				terminated = nil
			}
		}
	}
}

func (application *InteractiveApplication) observeSessionEvent(generation uint64, event protocol.SessionEvent) {
	application.mu.Lock()
	defer application.mu.Unlock()
	if application.generation != generation || application.active == nil || application.active.ID() != event.ThreadID {
		return
	}
	switch message := event.Message.(type) {
	case protocol.TurnStarted:
		if message.Kind == protocol.TaskKindCompact {
			application.phase = "compacting"
		} else {
			application.phase = "working"
		}
	case protocol.TurnCompleted:
		application.phase = "completed"
	case protocol.TurnAborted:
		application.phase = "aborted"
	case protocol.TurnRejected:
		application.phase = "idle"
	case protocol.ThreadTokenUsageUpdated:
		application.usage = message
	}
}

func (application *InteractiveApplication) current() (*threadmanager.AmadeusThread, uint64, error) {
	if application == nil {
		return nil, 0, errors.New("interactive application is nil")
	}
	application.mu.RLock()
	defer application.mu.RUnlock()
	if application.active == nil {
		return nil, application.generation, ErrNoActiveThread
	}
	return application.active, application.generation, nil
}

func (application *InteractiveApplication) releasePrevious(previous, current *threadmanager.AmadeusThread) {
	if previous == nil || previous == current {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(application.ctx), 5*time.Second)
		defer cancel()
		if err := application.workspace.Release(ctx, previous); err != nil {
			application.emit(ApplicationError{Operation: "release previous thread", Error: err})
		}
	}()
}

func (application *InteractiveApplication) emit(event InteractiveEvent) {
	if application == nil || event == nil {
		return
	}
	select {
	case application.events <- event:
	case <-application.ctx.Done():
	}
}

func shortRevision(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}
