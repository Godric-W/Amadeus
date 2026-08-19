package tui

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/rollout"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type FullscreenStartup struct {
	Version       string
	Provider      string
	Model         string
	Project       string
	Branch        string
	Session       string
	ContextWindow int64
}

type FullscreenClipboardWriter func(string) error

type FullscreenApplicationPort interface {
	Events() <-chan application.InteractiveEvent
	SubmitUser(context.Context, string) error
	SubmitCompact(context.Context) error
	SetMode(context.Context, turn.ModeKind) error
	Interrupt(context.Context) error
	ResolveApproval(context.Context, string, policy.ApprovalDecision) error
	LoadSessions(context.Context)
	Resume(context.Context, rollout.ThreadID)
	Clear(context.Context)
	Rename(context.Context, uint64, string)
	Delete(context.Context, uint64)
	Status() application.StatusSnapshot
	LoadMCP(context.Context, uint64, application.MCPDetail)
	LoadSkills()
	SetSkillEnabled(string, bool)
	Shutdown(context.Context)
}

type FullscreenOptions struct {
	Input             io.Reader
	Output            io.Writer
	Startup           FullscreenStartup
	Snapshot          application.ThreadViewSnapshot
	Application       FullscreenApplicationPort
	ClipboardWrite    FullscreenClipboardWriter
	OpenSessions      bool
	NoColor           bool
	DisableAnimations bool
	Width             int
}

type FullscreenApplication struct {
	options FullscreenOptions

	programMutex sync.RWMutex
	program      *tea.Program
	done         chan struct{}
}

type fullscreenModel struct {
	app                    *FullscreenApplication
	ctx                    context.Context
	startup                FullscreenStartup
	input                  textarea.Model
	renderer               *glamour.TermRenderer
	lastMouseEvent         time.Time
	width                  int
	height                 int
	transcript             TranscriptState
	runtimeTranscript      *protocol.TranscriptState
	historyCells           []HistoryCell
	pendingHistoryCells    []HistoryCell
	hasEmittedHistoryLines bool
	historyMode            HistoryRenderMode
	draft                  string
	running                bool
	status                 string
	statusDetails          string
	retryStatus            savedStatus
	model                  string
	sessionTitle           string
	inputUsage             int64
	outputUsage            int64
	contextUsage           int64
	contextLimit           int64
	history                []string
	historyPos             int
	runStartedAt           time.Time
	palette                terminalPalette
	clock                  motionClock
	motion                 motionMode
	motionStartedAt        time.Time
	details                *transcriptDetailStore
	detailViewport         viewport.Model
	viewingDetails         bool
	approval               *fullscreenApproval
	approvalDialog         *approvalDialog
	sessions               []application.SessionOption
	slashPopup             slashCommandPopup
	collaboration          CollaborationMode
	selection              *selectionOverlay
	selectionKind          string
	skills                 []application.SkillOption
	pendingSkillsView      string
	generation             uint64
	pendingModeTask        string
	mcpRequestID           uint64
	clearing               bool
	shutdownRequested      bool
}

type savedStatus struct {
	header  string
	details string
	active  bool
}

type fullscreenApproval struct {
	requestID string
	request   policy.ApprovalRequest
}

type fullscreenApprovalChoice struct {
	label       string
	description string
	decision    policy.ApprovalDecision
}

var fullscreenApprovalChoices = []fullscreenApprovalChoice{
	{label: "Yes", description: "Allow this operation once", decision: policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "user approved once"}},
	{label: "Yes, during this session", description: "Allow matching operations until the session ends", decision: policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalSession, Source: policy.ApprovalSourceUser, Reason: "user approved for the session"}},
	{label: "No", description: "Do not perform this operation", decision: policy.ApprovalDecision{Outcome: policy.ApprovalDeny, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "user denied the request"}},
}

func approvalChoices(request policy.ApprovalRequest) []fullscreenApprovalChoice {
	if len(request.Presentation.Options) > 0 {
		choices := make([]fullscreenApprovalChoice, 0, len(request.Presentation.Options))
		for _, option := range request.Presentation.Options {
			choices = append(choices, fullscreenApprovalChoice{
				label: option.Label, description: option.Description,
				decision: policy.ApprovalDecision{
					OptionID: option.ID,
					Outcome:  option.Outcome,
					Scope:    option.Scope,
					Source:   policy.ApprovalSourceUser,
					Reason:   "user selected " + option.Label,
				},
			})
		}
		return choices
	}
	return fullscreenApprovalChoices
}

type fullscreenAppEventMsg struct{ event application.InteractiveEvent }
type fullscreenOperationFailedMsg struct {
	operation string
	err       error
}
type fullscreenWorkingTickMsg time.Time

const (
	fullscreenInputPrompt      = "› "
	fullscreenInputPlaceholder = "Ask Amadeus to do anything, or type / for commands"
	fullscreenInputCharLimit   = 20000
	fullscreenMaxInputRows     = 5
)

var (
	fullscreenPanelStyle     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	fullscreenInputFillStyle = lipgloss.NewStyle()
)

func NewFullscreenApplication(options FullscreenOptions) (*FullscreenApplication, error) {
	if options.Input == nil || options.Output == nil {
		return nil, errors.New("fullscreen TUI streams are nil")
	}
	if options.Application == nil {
		return nil, errors.New("fullscreen interactive application is nil")
	}
	if options.ClipboardWrite == nil {
		options.ClipboardWrite = clipboard.WriteAll
	}
	return &FullscreenApplication{options: options, done: make(chan struct{})}, nil
}

func (app *FullscreenApplication) Run(ctx context.Context) error {
	if app == nil {
		return errors.New("fullscreen TUI application is nil")
	}
	model := newFullscreenModel(ctx, app)
	originalColorProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(model.palette.colorProfile())
	defer lipgloss.SetColorProfile(originalColorProfile)
	programOptions := []tea.ProgramOption{tea.WithContext(ctx), tea.WithInput(app.options.Input), tea.WithOutput(app.options.Output)}
	program := tea.NewProgram(model, programOptions...)
	app.programMutex.Lock()
	app.program = program
	app.programMutex.Unlock()
	go app.forwardEvents(ctx)
	_, err := program.Run()
	app.programMutex.Lock()
	app.program = nil
	select {
	case <-app.done:
	default:
		close(app.done)
	}
	app.programMutex.Unlock()
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (app *FullscreenApplication) forwardEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-app.options.Application.Events():
			if !ok {
				return
			}
			if err := app.send(ctx, fullscreenAppEventMsg{event: event}); err != nil {
				return
			}
		}
	}
}

func (app *FullscreenApplication) send(ctx context.Context, message tea.Msg) error {
	if app == nil {
		return errors.New("fullscreen TUI application is nil")
	}
	if ctx == nil {
		return errors.New("fullscreen TUI context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	app.programMutex.RLock()
	program := app.program
	done := app.done
	app.programMutex.RUnlock()
	if program == nil {
		return errors.New("fullscreen TUI is not running")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return errors.New("fullscreen TUI is closed")
	default:
		program.Send(message)
		return nil
	}
}

func newFullscreenModel(ctx context.Context, app *FullscreenApplication) fullscreenModel {
	palette := detectTerminalPalette(app.options.NoColor)
	input := textarea.New()
	input.Placeholder = fullscreenInputPlaceholder
	input.Prompt = fullscreenInputPrompt
	input.ShowLineNumbers = false
	input.EndOfBufferCharacter = ' '
	input.CharLimit = fullscreenInputCharLimit
	input.MaxHeight = fullscreenMaxInputRows
	input.FocusedStyle.Base = fullscreenInputFillStyle
	input.FocusedStyle.CursorLine = fullscreenInputFillStyle
	input.FocusedStyle.Prompt = palette.strong()
	input.FocusedStyle.Placeholder = palette.dim()
	input.FocusedStyle.Text = palette.plain()
	input.FocusedStyle.EndOfBuffer = fullscreenInputFillStyle
	input.BlurredStyle.Base = fullscreenInputFillStyle
	input.BlurredStyle.CursorLine = fullscreenInputFillStyle
	input.BlurredStyle.Prompt = palette.dim()
	input.BlurredStyle.Placeholder = palette.dim()
	input.BlurredStyle.Text = palette.dim()
	input.BlurredStyle.EndOfBuffer = fullscreenInputFillStyle
	input.SetWidth(80)
	input.SetHeight(1)
	input.Focus()
	renderer, _ := newFullscreenMarkdownRenderer(94, palette)
	startup := app.options.Startup
	snapshot := app.options.Snapshot
	startup.Session = string(snapshot.ThreadID)
	startup.Provider = snapshot.Provider
	startup.Model = snapshot.Model
	startup.ContextWindow = snapshot.ContextWindow
	initialWidth := app.options.Width
	if initialWidth < 20 {
		initialWidth = 100
	}
	model := fullscreenModel{
		app: app, ctx: ctx, startup: startup, input: input, renderer: renderer,
		width: initialWidth, height: 30, status: "idle", model: startup.Model, historyPos: -1, collaboration: CollaborationExecute,
		sessionTitle: snapshot.Title,
		palette:      palette, clock: systemMotionClock{}, motion: motionAnimated, motionStartedAt: time.Now(),
		details: newTranscriptDetailStore(0, 0), detailViewport: newTranscriptViewport(initialWidth, 30),
		runtimeTranscript: protocol.NewTranscriptState(rollout.ThreadID(startup.Session)), generation: snapshot.Generation,
	}
	if snapshot.Mode == turn.ModeKindPlan {
		model.collaboration = CollaborationPlan
	}
	model.inputUsage = snapshot.Usage.InputTokens
	model.outputUsage = snapshot.Usage.OutputTokens
	model.contextUsage = snapshot.Usage.TotalTokens
	if app.options.DisableAnimations {
		model.motion = motionReduced
	}
	model.updateInputLayout()
	model.renderer, _ = newFullscreenMarkdownRenderer(maxInt(20, initialWidth-6), palette)
	model.restoreCompletedItems(snapshot.Items)
	model.pendingHistoryCells = nil
	model.hasEmittedHistoryLines = len(model.historyCells) > 0
	return model
}

func (model fullscreenModel) Init() tea.Cmd {
	header := model.banner()
	if len(model.historyCells) > 0 {
		header += "\n\n" + renderHistoryCells(model.historyCells, model.historyMode, model.historyRenderContext())
	}
	commands := []tea.Cmd{tea.Println(header), tea.HideCursor, model.input.Focus()}
	if model.app.options.OpenSessions {
		commands = append(commands, model.loadSessions())
	}
	return tea.Sequence(commands...)
}

func fullscreenWorkingTick() tea.Cmd {
	return tea.Tick(motionFrameInterval, func(now time.Time) tea.Msg {
		return fullscreenWorkingTickMsg(now)
	})
}

func (model fullscreenModel) workingTick() tea.Cmd {
	if model.motion == motionReduced {
		return nil
	}
	return fullscreenWorkingTick()
}
