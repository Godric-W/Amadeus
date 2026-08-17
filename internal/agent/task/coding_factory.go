package task

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
	"github.com/Godric-W/Amadeus/internal/skill"
	"github.com/Godric-W/Amadeus/internal/webfetch"
	"github.com/Godric-W/Amadeus/internal/websearch"
)

type AuditFactory func() (audit.Sink, io.Closer, error)

type Capabilities interface {
	PermissionGrantCount() int
	SkillRevision() string
	MCPRevision() string
	Skills() []skill.IndexEntry
	SetSkillEnabled(string, bool) error
	MCPServers() []string
	MCPBindings() mcp.BindingSnapshot
	MCPTools(context.Context, string) ([]mcp.RemoteTool, error)
}

type CodingFactoryOptions struct {
	Config           config.Config
	Project          project.Root
	WorkspaceRoots   []string
	AmadeusRoot      string
	ClientFactory    engine.ClientFactory
	MCPClientFactory mcp.ClientFactory
	WebFetcher       webfetch.Fetcher
	WebSearch        websearch.Provider
	AuditFactory     AuditFactory
	BaseInstructions llm.BaseInstructions
	Clock            func() time.Time
}

type CodingFactory struct {
	mu               sync.Mutex
	configured       config.Config
	project          project.Root
	amadeusRoot      string
	clientFactory    engine.ClientFactory
	mcpClientFactory mcp.ClientFactory
	webFetcher       webfetch.Fetcher
	webSearch        websearch.Provider
	auditFactory     AuditFactory
	extensions       *extensionruntime.Runtime
	permissions      *policy.SessionPermissionContext
	fileSystemPolicy *project.FileSystemPolicy
	instructions     *instruction.WorkspaceResolver
	baseInstructions llm.BaseInstructions
	clock            func() time.Time
	runtime          *engine.CodingRuntime
	closed           bool
}

func NewCodingFactory(options CodingFactoryOptions) (*CodingFactory, error) {
	if err := config.Validate(options.Config); err != nil {
		return nil, fmt.Errorf("validate coding task configuration: %w", err)
	}
	if options.Project.Path() == "" {
		return nil, errors.New("coding task project root is empty")
	}
	if options.AuditFactory == nil {
		return nil, errors.New("coding task audit factory is nil")
	}
	if strings.TrimSpace(options.BaseInstructions.Text) == "" {
		return nil, errors.New("coding task base instructions are empty")
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
	extensions, err := extensionruntime.New(options.AmadeusRoot, options.Project, extensionruntime.Options{MCPClientFactory: options.MCPClientFactory})
	if err != nil {
		return nil, err
	}
	return &CodingFactory{
		configured: options.Config, project: options.Project, amadeusRoot: options.AmadeusRoot, clientFactory: options.ClientFactory,
		mcpClientFactory: options.MCPClientFactory, webFetcher: options.WebFetcher, webSearch: options.WebSearch,
		auditFactory: options.AuditFactory, extensions: extensions, permissions: policy.NewSessionPermissionContext(),
		fileSystemPolicy: fileSystemPolicy, instructions: instructions,
		baseInstructions: options.BaseInstructions, clock: options.Clock,
	}, nil
}

func (factory *CodingFactory) Prepare(ctx context.Context, host Host, request PrepareRequest) (Prepared, error) {
	if factory == nil {
		return Prepared{}, errors.New("coding task factory is nil")
	}
	if ctx == nil || host == nil {
		return Prepared{}, errors.New("coding task preparation is incomplete")
	}
	if err := factory.ensureOpen(); err != nil {
		return Prepared{}, err
	}
	snapshot := request.Context
	now := factory.clock()
	if snapshot.CurrentDate == "" {
		snapshot.CurrentDate = now.Format("2006-01-02")
	}
	if snapshot.Timezone == "" {
		snapshot.Timezone = now.Location().String()
	}
	switch request.Kind {
	case KindCompact:
		return Prepared{Task: &compactTask{factory: factory}, Context: snapshot}, snapshot.Validate()
	case KindRegular:
		goal := strings.TrimSpace(request.Input)
		if goal == "" {
			return Prepared{}, errors.New("regular task goal is empty")
		}
		return factory.prepareRegular(ctx, host, snapshot, goal)
	default:
		return Prepared{}, fmt.Errorf("unsupported task kind %q", request.Kind)
	}
}

func (factory *CodingFactory) ensureOpen() error {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if factory.closed {
		return errors.New("coding task factory is closed")
	}
	return nil
}

func (factory *CodingFactory) Close() error {
	if factory == nil {
		return nil
	}
	factory.mu.Lock()
	if factory.closed {
		factory.mu.Unlock()
		return nil
	}
	factory.closed = true
	runtime := factory.runtime
	extensions := factory.extensions
	factory.mu.Unlock()
	if runtime != nil {
		return runtime.Close()
	}
	factory.permissions.Clear()
	if extensions != nil {
		return extensions.Close()
	}
	return nil
}

func (factory *CodingFactory) ensureRuntime(host Host) (*engine.CodingRuntime, error) {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if factory.closed {
		return nil, errors.New("coding task factory is closed")
	}
	if factory.runtime != nil {
		return factory.runtime, nil
	}
	runtimeHost, ok := host.(eventRequestHost)
	if !ok {
		return nil, errors.New("session task host does not expose event and request boundaries")
	}
	planHost, ok := host.(PlanHost)
	if !ok {
		return nil, errors.New("turn host does not support session plans")
	}
	approvals, err := newSessionApprovalPort(runtimeHost)
	if err != nil {
		return nil, err
	}
	auditSink, auditCloser, err := factory.auditFactory()
	if err != nil {
		return nil, err
	}
	if auditSink == nil {
		if auditCloser != nil {
			_ = auditCloser.Close()
		}
		return nil, errors.New("coding task audit factory returned nil sink")
	}
	runtime, err := engine.NewCodingRuntime(engine.RuntimeOptions{
		Config: factory.configured, Project: factory.project, ClientFactory: factory.clientFactory,
		Events: runtimeHost, Approvals: approvals, PlanUpdater: planHost,
		Audit: auditSink, AuditCloser: auditCloser, Extensions: factory.extensions,
		WebFetcher: factory.webFetcher, WebSearch: factory.webSearch,
		FileSystemPolicy: factory.fileSystemPolicy, Permissions: factory.permissions,
		Instructions: factory.instructions, BaseInstructions: factory.baseInstructions,
	})
	if err != nil {
		if auditCloser != nil {
			_ = auditCloser.Close()
		}
		return nil, err
	}
	factory.runtime = runtime
	return runtime, nil
}

func (factory *CodingFactory) PermissionGrantCount() int {
	if factory == nil || factory.permissions == nil {
		return 0
	}
	return factory.permissions.GrantCount()
}

func (factory *CodingFactory) SkillRevision() string {
	if factory == nil || factory.extensions == nil {
		return ""
	}
	return factory.extensions.SkillRevision()
}

func (factory *CodingFactory) MCPRevision() string {
	if factory == nil || factory.extensions == nil {
		return ""
	}
	return factory.extensions.MCPRevision()
}

func (factory *CodingFactory) Skills() []skill.IndexEntry {
	if factory == nil || factory.extensions == nil || factory.extensions.Skills() == nil {
		return nil
	}
	return factory.extensions.Skills().Index()
}

func (factory *CodingFactory) SetSkillEnabled(name string, enabled bool) error {
	if factory == nil || factory.extensions == nil {
		return errors.New("Skill catalog is unavailable")
	}
	return factory.extensions.SetSkillEnabled(name, enabled)
}

func (factory *CodingFactory) MCPServers() []string {
	if factory == nil || factory.extensions == nil || factory.extensions.MCP() == nil {
		return nil
	}
	return factory.extensions.MCP().EnabledServers()
}

func (factory *CodingFactory) MCPBindings() mcp.BindingSnapshot {
	if factory == nil || factory.extensions == nil {
		return mcp.BindingSnapshot{}
	}
	return factory.extensions.MCPBinding()
}

func (factory *CodingFactory) MCPTools(ctx context.Context, server string) ([]mcp.RemoteTool, error) {
	if factory == nil || factory.extensions == nil || factory.extensions.MCP() == nil {
		return nil, errors.New("MCP manager is unavailable")
	}
	return factory.extensions.MCP().ListTools(ctx, server)
}

type regularTask struct {
	runtime      *engine.CodingRuntime
	goal         string
	events       protocol.EventSink
	instructions *targetInstructionScope
}

func (*regularTask) Kind() Kind { return KindRegular }

func (sessionTask *regularTask) Run(ctx context.Context, host Host, turnContext *turn.Context, _ []Input) (Result, error) {
	return sessionTask.run(ctx, host, turnContext)
}

func (sessionTask *regularTask) Abort(context.Context, Host, *turn.Context) error {
	return nil
}

type compactTask struct{ factory *CodingFactory }

func (*compactTask) Kind() Kind { return KindCompact }

func (*compactTask) Abort(context.Context, Host, *turn.Context) error { return nil }

type eventRequestHost interface {
	Host
	protocol.EventSink
	Request(context.Context, protocol.InteractiveRequest) (protocol.Op, error)
}

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

var _ Factory = (*CodingFactory)(nil)
var _ Capabilities = (*CodingFactory)(nil)
var _ io.Closer = (*CodingFactory)(nil)
