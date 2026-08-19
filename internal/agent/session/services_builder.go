package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	extensionruntime "github.com/Godric-W/Amadeus/internal/extension"
	"github.com/Godric-W/Amadeus/internal/instruction"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/mcp"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/project"
	"github.com/Godric-W/Amadeus/internal/webfetch"
	"github.com/Godric-W/Amadeus/internal/websearch"
)

type AuditFactory func() (audit.Sink, io.Closer, error)

type ServicesOptions struct {
	Config           config.Config
	Project          project.Root
	WorkspaceRoots   []string
	AmadeusRoot      string
	ClientFactory    engine.ClientFactory
	MCPClientFactory mcp.ClientFactory
	WebFetcher       webfetch.Fetcher
	WebSearch        websearch.Provider
	AuditFactory     AuditFactory
	ModelMessages    llm.ModelMessages
	Clock            func() time.Time
}

type ServicesBuilder struct {
	mu                sync.Mutex
	configured        config.Config
	project           project.Root
	amadeusRoot       string
	clientFactory     engine.ClientFactory
	mcpClientFactory  mcp.ClientFactory
	webFetcher        webfetch.Fetcher
	webSearch         websearch.Provider
	auditFactory      AuditFactory
	extensionAssembly *extensionruntime.Assembly
	permissions       *policy.SessionPermissionContext
	fileSystemPolicy  *project.FileSystemPolicy
	instructions      *instruction.WorkspaceResolver
	modelMessages     llm.ModelMessages
	clock             func() time.Time
	runtime           *engine.Services
	closed            bool
}

func NewServicesBuilder(options ServicesOptions) (*ServicesBuilder, error) {
	if err := config.Validate(options.Config); err != nil {
		return nil, fmt.Errorf("validate session configuration: %w", err)
	}
	if options.Project.Path() == "" {
		return nil, errors.New("session project root is empty")
	}
	if options.AuditFactory == nil {
		return nil, errors.New("session audit factory is nil")
	}
	if !options.ModelMessages.HasInstructions() {
		return nil, errors.New("session model messages are empty")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	workspaceRoots := append([]string{options.Project.Path()}, options.WorkspaceRoots...)
	fileSystemPolicy, err := project.NewFileSystemPolicy(project.FileSystemPolicyOptions{
		CWD: options.Project.Path(),
		Profile: project.PermissionProfile{
			ReadHost: true, WorkspaceRoots: workspaceRoots, TemporaryRoots: project.DefaultTemporaryRoots(),
			ReadOnlyRoots: workspaceReadOnlyRoots(workspaceRoots),
			DeniedRoots:   append(project.DefaultDeniedRoots(), amadeusDeniedRoots(options.AmadeusRoot)...),
		},
	})
	if err != nil {
		return nil, err
	}
	userLoader, err := instruction.NewUserLoader(options.AmadeusRoot, instruction.UserLoaderOptions{})
	if err != nil {
		return nil, fmt.Errorf("create user instruction loader: %w", err)
	}
	roots := []project.Root{options.Project}
	for _, workspaceRoot := range options.WorkspaceRoots {
		resolved, resolveErr := project.NewRoot(workspaceRoot)
		if resolveErr != nil {
			return nil, resolveErr
		}
		roots = append(roots, resolved)
	}
	instructions, err := instruction.NewWorkspaceResolver(userLoader, roots, instruction.ProjectLoaderOptions{})
	if err != nil {
		return nil, err
	}
	extensionAssembly, err := extensionruntime.Assemble(options.AmadeusRoot, options.Project, extensionruntime.Options{MCPClientFactory: options.MCPClientFactory})
	if err != nil {
		return nil, err
	}
	return &ServicesBuilder{
		configured: options.Config, project: options.Project, amadeusRoot: options.AmadeusRoot, clientFactory: options.ClientFactory,
		mcpClientFactory: options.MCPClientFactory, webFetcher: options.WebFetcher, webSearch: options.WebSearch,
		auditFactory: options.AuditFactory, extensionAssembly: extensionAssembly, permissions: policy.NewSessionPermissionContext(),
		fileSystemPolicy: fileSystemPolicy, instructions: instructions,
		modelMessages: options.ModelMessages, clock: options.Clock,
	}, nil
}

func (builder *ServicesBuilder) NewRegularTask(ctx context.Context, session *Session, input string, value turn.TurnContext) (SessionTask, turn.TurnContext, error) {
	if err := builder.validateTaskRequest(ctx, session); err != nil {
		return nil, turn.TurnContext{}, err
	}
	goal := strings.TrimSpace(input)
	if goal == "" {
		return nil, turn.TurnContext{}, errors.New("regular task goal is empty")
	}
	snapshot := builder.withEnvironment(value)
	return builder.prepareRegular(ctx, session, snapshot, goal)
}

func (builder *ServicesBuilder) NewCompactTask(ctx context.Context, session *Session, input string, value turn.TurnContext) (SessionTask, turn.TurnContext, error) {
	if err := builder.validateTaskRequest(ctx, session); err != nil {
		return nil, turn.TurnContext{}, err
	}
	runtime := session.AgentServices()
	if runtime == nil {
		return nil, turn.TurnContext{}, errors.New("session agent services are unavailable")
	}
	snapshot := builder.withEnvironment(value)
	if err := snapshot.Validate(); err != nil {
		return nil, turn.TurnContext{}, err
	}
	return &compactTask{runtime: runtime}, snapshot, nil
}

func (builder *ServicesBuilder) validateTaskRequest(ctx context.Context, session *Session) error {
	if builder == nil {
		return errors.New("session services builder is nil")
	}
	if ctx == nil || session == nil {
		return errors.New("session task construction is incomplete")
	}
	return builder.ensureOpen()
}

func (builder *ServicesBuilder) withEnvironment(value turn.TurnContext) turn.TurnContext {
	snapshot := value
	now := builder.clock()
	if snapshot.CurrentDate == "" {
		snapshot.CurrentDate = now.Format("2006-01-02")
	}
	if snapshot.Timezone == "" {
		snapshot.Timezone = now.Location().String()
	}
	return snapshot
}

func (builder *ServicesBuilder) BuildServices(ctx context.Context, session *Session) (*engine.Services, error) {
	if ctx == nil || session == nil {
		return nil, errors.New("session services construction is incomplete")
	}
	builder.mu.Lock()
	defer builder.mu.Unlock()
	if builder.closed {
		return nil, errors.New("session services builder is closed")
	}
	if builder.runtime != nil {
		return builder.runtime, nil
	}
	approvals, err := newSessionApprovalPort(session)
	if err != nil {
		return nil, err
	}
	auditSink, auditCloser, err := builder.auditFactory()
	if err != nil {
		return nil, err
	}
	if auditSink == nil {
		if auditCloser != nil {
			_ = auditCloser.Close()
		}
		return nil, errors.New("session audit builder returned nil sink")
	}
	runtime, err := engine.NewServices(engine.ServicesOptions{
		Config: builder.configured, Project: builder.project, ClientFactory: builder.clientFactory,
		Events: session, Approvals: approvals, PlanUpdater: session,
		Audit: auditSink, AuditCloser: auditCloser, ExtensionAssembly: builder.extensionAssembly,
		WebFetcher: builder.webFetcher, WebSearch: builder.webSearch,
		FileSystemPolicy: builder.fileSystemPolicy, Permissions: builder.permissions,
		Instructions: builder.instructions, ModelMessages: builder.modelMessages,
	})
	if err != nil {
		if auditCloser != nil {
			_ = auditCloser.Close()
		}
		return nil, err
	}
	builder.runtime = runtime
	return runtime, nil
}

func (builder *ServicesBuilder) ensureOpen() error {
	builder.mu.Lock()
	defer builder.mu.Unlock()
	if builder.closed {
		return errors.New("session services builder is closed")
	}
	return nil
}

func (builder *ServicesBuilder) Close() error {
	if builder == nil {
		return nil
	}
	builder.mu.Lock()
	if builder.closed {
		builder.mu.Unlock()
		return nil
	}
	builder.closed = true
	runtime := builder.runtime
	extensionAssembly := builder.extensionAssembly
	builder.mu.Unlock()
	if runtime != nil {
		return runtime.Close()
	}
	builder.permissions.Clear()
	if extensionAssembly != nil {
		return extensionAssembly.Close()
	}
	return nil
}

type regularTask struct {
	runtime      *engine.Services
	goal         string
	events       protocol.EventSink
	instructions *targetInstructionScope
}

func (*regularTask) Kind() TaskKind { return TaskKindRegular }

func (sessionTask *regularTask) Run(ctx context.Context, session *Session, turnContext *turn.TurnContext, _ []TurnInput) (Result, error) {
	return sessionTask.run(ctx, session, turnContext)
}

func (sessionTask *regularTask) Abort(context.Context, *Session, *turn.TurnContext) error {
	return nil
}

type compactTask struct{ runtime *engine.Services }

func (*compactTask) Kind() TaskKind { return TaskKindCompact }

func (*compactTask) Abort(context.Context, *Session, *turn.TurnContext) error { return nil }

func amadeusDeniedRoots(root string) []string {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	return []string{filepath.Join(root, "config.yaml"), filepath.Join(root, "mcp.yaml"), filepath.Join(root, "skills"), filepath.Join(root, "data")}
}

func workspaceReadOnlyRoots(workspaceRoots []string) []string {
	values := make([]string, 0, len(workspaceRoots)*2)
	for _, root := range workspaceRoots {
		for _, name := range []string{".git", ".amadeus"} {
			candidate := filepath.Join(root, name)
			if _, err := os.Stat(candidate); err == nil {
				values = append(values, candidate)
			}
		}
	}
	return values
}

var _ io.Closer = (*ServicesBuilder)(nil)
