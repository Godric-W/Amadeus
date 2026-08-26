package tui

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type Startup struct {
	Version string
}

type ClipboardWriter func(string) error

type ApplicationPort interface {
	Events() <-chan application.InteractiveEvent
	SubmitUser(context.Context, string, string, protocol.ThreadSettingsOverrides) (protocol.UserMessageAdmission, error)
	SubmitCompact(context.Context) error
	SetMode(context.Context, protocol.ModeKind) error
	Interrupt(context.Context) error
	ResolveApproval(context.Context, string, policy.ApprovalDecision) error
	ResolveUserInput(context.Context, protocol.RequestID, protocol.RequestUserInputResponse) error
	LoadSessions(context.Context)
	Resume(context.Context, protocol.ThreadID)
	Clear(context.Context)
	Rename(context.Context, uint64, string)
	Delete(context.Context, uint64)
	Status() application.StatusSnapshot
	LoadMCP(context.Context, uint64, application.MCPDetail)
	LoadSkills()
	SetSkillEnabled(string, bool)
	Shutdown(context.Context) error
}

type ApplicationOptions struct {
	Input              io.Reader
	Output             io.Writer
	Startup            Startup
	InitialUserMessage *UserMessage
	Snapshot           application.ThreadViewSnapshot
	Application        ApplicationPort
	ClipboardWrite     ClipboardWriter
	OpenSessions       bool
	NoColor            bool
	DisableAnimations  bool
	Width              int
}

type Application struct {
	options ApplicationOptions

	programMutex sync.RWMutex
	program      *tea.Program
	done         chan struct{}
}

type appModel struct {
	app                     *Application
	ctx                     context.Context
	startup                 Startup
	input                   textarea.Model
	renderer                *glamour.TermRenderer
	lastMouseEvent          time.Time
	width                   int
	height                  int
	transcript              TranscriptState
	protocolEvents          *protocolEventState
	historyCells            []HistoryCell
	pendingHistoryCells     []HistoryCell
	hasEmittedHistoryLines  bool
	historyMode             HistoryRenderMode
	draft                   string
	proposedPlanDraft       string
	completedProposedPlan   bool
	running                 bool
	status                  string
	statusDetails           string
	retryStatus             savedStatus
	session                 sessionViewState
	footer                  footerState
	workspace               statusLineWorkspaceState
	history                 []string
	historyPos              int
	runStartedAt            time.Time
	palette                 terminalPalette
	clock                   motionClock
	motion                  motionMode
	motionStartedAt         time.Time
	details                 *transcriptDetailStore
	detailViewport          viewport.Model
	viewingDetails          bool
	approval                *approvalState
	approvalDialog          *approvalDialog
	userInputRequest        *protocol.RequestUserInputEvent
	userInputDialog         *requestUserInputDialog
	sessions                []application.SessionOption
	slashPopup              slashCommandPopup
	pendingMode             protocol.ModeKind
	selection               *selectionOverlay
	selectionKind           string
	skills                  []application.SkillOption
	pendingSkillsView       string
	mcpRequestID            uint64
	clearing                bool
	exit                    exitState
	nextClientUserMessage   uint64
	optimisticUserMessages  map[string]string
	seenRuntimeUserMessages map[string]struct{}
	initialUserMessage      *UserMessage
	nextTurnQueue           NextTurnQueue
}

type savedStatus struct {
	header  string
	details string
	active  bool
}

type approvalState struct {
	requestID string
	request   policy.ApprovalRequest
}

type approvalChoice struct {
	label       string
	description string
	decision    policy.ApprovalDecision
}

var approvalChoicesDefault = []approvalChoice{
	{label: "Yes", description: "Allow this operation once", decision: policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "user approved once"}},
	{label: "Yes, during this session", description: "Allow matching operations until the session ends", decision: policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalSession, Source: policy.ApprovalSourceUser, Reason: "user approved for the session"}},
	{label: "No", description: "Do not perform this operation", decision: policy.ApprovalDecision{Outcome: policy.ApprovalDeny, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "user denied the request"}},
}

func approvalChoices(request policy.ApprovalRequest) []approvalChoice {
	if len(request.Presentation.Options) > 0 {
		choices := make([]approvalChoice, 0, len(request.Presentation.Options))
		for _, option := range request.Presentation.Options {
			choices = append(choices, approvalChoice{
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
	return approvalChoicesDefault
}

type appEventMsg struct{ event application.InteractiveEvent }
type operationFailedMsg struct {
	operation string
	err       error
}
type userMessageAdmittedMsg struct {
	submission UserMessageSubmission
	admission  protocol.UserMessageAdmission
}
type userMessageRejectedMsg struct {
	submission UserMessageSubmission
	err        error
}
type workingTickMsg time.Time
type startupReadyMsg struct{}

const (
	inputPrompt      = "› "
	inputPlaceholder = "Ask Amadeus to do anything"
	inputCharLimit   = 20000
	maxInputRows     = 5
)

var (
	panelStyle     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	inputFillStyle = lipgloss.NewStyle()
)

func NewApplication(options ApplicationOptions) (*Application, error) {
	if options.Input == nil || options.Output == nil {
		return nil, errors.New("TUI streams are nil")
	}
	if options.Application == nil {
		return nil, errors.New("TUI interactive application is nil")
	}
	if options.OpenSessions && options.InitialUserMessage != nil {
		return nil, errors.New("TUI session picker cannot start with an initial user message")
	}
	if options.InitialUserMessage != nil {
		if err := options.InitialUserMessage.Validate(); err != nil {
			return nil, err
		}
	}
	options.InitialUserMessage = cloneUserMessage(options.InitialUserMessage)
	if options.ClipboardWrite == nil {
		options.ClipboardWrite = clipboard.WriteAll
	}
	return &Application{options: options, done: make(chan struct{})}, nil
}

func (app *Application) Run(ctx context.Context) (AppExitInfo, error) {
	if app == nil {
		return AppExitInfo{}, errors.New("TUI application is nil")
	}
	initialModel := newModel(ctx, app)
	originalColorProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(initialModel.palette.colorProfile())
	defer lipgloss.SetColorProfile(originalColorProfile)
	programOptions := []tea.ProgramOption{tea.WithContext(ctx), tea.WithInput(app.options.Input), tea.WithOutput(app.options.Output)}
	program := tea.NewProgram(initialModel, programOptions...)
	app.programMutex.Lock()
	app.program = program
	app.programMutex.Unlock()
	go app.forwardEvents(ctx)
	finalModel, err := program.Run()
	app.programMutex.Lock()
	app.program = nil
	select {
	case <-app.done:
	default:
		close(app.done)
	}
	app.programMutex.Unlock()
	exitInfo := initialModel.appExitInfo()
	if final, ok := finalModel.(appModel); ok {
		exitInfo = final.appExitInfo()
	}
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		exitInfo.ExitReason = ExitReasonFatal
		exitInfo.Error = ctx.Err()
		return exitInfo, ctx.Err()
	}
	if err != nil {
		exitInfo.ExitReason = ExitReasonFatal
		exitInfo.Error = err
	}
	return exitInfo, err
}

func (app *Application) forwardEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-app.done:
			return
		case event, ok := <-app.options.Application.Events():
			if !ok {
				return
			}
			if err := app.send(ctx, appEventMsg{event: event}); err != nil {
				return
			}
		}
	}
}

func (app *Application) send(ctx context.Context, message tea.Msg) error {
	if app == nil {
		return errors.New("TUI application is nil")
	}
	if ctx == nil {
		return errors.New("TUI context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	app.programMutex.RLock()
	program := app.program
	done := app.done
	app.programMutex.RUnlock()
	if program == nil {
		return errors.New("TUI is not running")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return errors.New("TUI is closed")
	default:
		program.Send(message)
		return nil
	}
}

func newModel(ctx context.Context, app *Application) appModel {
	palette := detectTerminalPalette(app.options.NoColor)
	input := textarea.New()
	input.Placeholder = inputPlaceholder
	input.SetPromptFunc(lipgloss.Width(inputPrompt), func(line int) string {
		if line == 0 {
			return inputPrompt
		}
		return ""
	})
	input.ShowLineNumbers = false
	input.EndOfBufferCharacter = ' '
	input.CharLimit = inputCharLimit
	input.MaxHeight = maxInputRows
	input.FocusedStyle.Base = inputFillStyle
	input.FocusedStyle.CursorLine = inputFillStyle
	input.FocusedStyle.Prompt = palette.strong()
	input.FocusedStyle.Placeholder = palette.dim()
	input.FocusedStyle.Text = palette.plain()
	input.FocusedStyle.EndOfBuffer = inputFillStyle
	input.BlurredStyle.Base = inputFillStyle
	input.BlurredStyle.CursorLine = inputFillStyle
	input.BlurredStyle.Prompt = palette.dim()
	input.BlurredStyle.Placeholder = palette.dim()
	input.BlurredStyle.Text = palette.dim()
	input.BlurredStyle.EndOfBuffer = inputFillStyle
	input.SetWidth(80)
	input.SetHeight(1)
	input.Focus()
	_ = input.Cursor.SetMode(cursor.CursorStatic)
	renderer, _ := newMarkdownRenderer(94, palette)
	startup := app.options.Startup
	snapshot := app.options.Snapshot
	initialWidth := app.options.Width
	if initialWidth < 20 {
		initialWidth = 100
	}
	model := appModel{
		app: app, ctx: ctx, startup: startup, input: input, renderer: renderer,
		width: initialWidth, height: 30, status: "idle", historyPos: -1,
		palette: palette, clock: systemMotionClock{}, motion: motionAnimated, motionStartedAt: time.Now(),
		details: newTranscriptDetailStore(0, 0), detailViewport: newTranscriptViewport(initialWidth, 30),
		protocolEvents:         newProtocolEventState(snapshot.ThreadID),
		optimisticUserMessages: make(map[string]string), seenRuntimeUserMessages: make(map[string]struct{}),
		initialUserMessage: cloneUserMessage(app.options.InitialUserMessage),
	}
	_ = model.applyThreadViewSnapshot(snapshot)
	if app.options.DisableAnimations {
		model.motion = motionReduced
	}
	model.updateInputLayout()
	model.renderer, _ = newMarkdownRenderer(maxInt(20, initialWidth-6), palette)
	model.restoreCompletedItems(snapshot.Items)
	model.pendingHistoryCells = nil
	model.hasEmittedHistoryLines = len(model.historyCells) > 0
	return model
}

func (model appModel) Init() tea.Cmd {
	header := model.banner()
	if len(model.historyCells) > 0 {
		header += "\n\n" + renderHistoryCells(model.historyCells, model.historyMode, model.historyRenderContext())
	}
	commands := []tea.Cmd{tea.Println(header), tea.HideCursor, model.input.Focus(), model.statusLineBranchLookupCommand()}
	if model.app.options.OpenSessions {
		commands = append(commands, model.loadSessions())
	}
	commands = append(commands, func() tea.Msg { return startupReadyMsg{} })
	return tea.Sequence(commands...)
}

func workingTick() tea.Cmd {
	return tea.Tick(motionFrameInterval, func(now time.Time) tea.Msg {
		return workingTickMsg(now)
	})
}

func (model appModel) workingTick() tea.Cmd {
	if model.motion == motionReduced {
		return nil
	}
	return workingTick()
}
