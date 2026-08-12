package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
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

type FullscreenCommandHandler func(context.Context, string) (string, error)
type FullscreenSessionLister func(context.Context) ([]SessionOption, error)
type FullscreenSessionResumer func(context.Context, string) (string, error)
type FullscreenCurrentSession func() string
type FullscreenCurrentSessionTitle func() string
type FullscreenSessionRenamer func(context.Context, string) (string, error)
type FullscreenSessionDeleter func(context.Context) (string, error)
type FullscreenCompactor func(context.Context) (string, error)
type FullscreenSkillLister func(context.Context) ([]SkillOption, error)
type FullscreenSkillSetter func(context.Context, string, bool) error
type FullscreenClipboardWriter func(string) error

type SessionOption struct {
	ID      string
	Title   string
	Current bool
}

type SkillOption struct {
	Name        string
	Description string
	Source      string
	Enabled     bool
}

type FullscreenOptions struct {
	Input               io.Reader
	Output              io.Writer
	Startup             FullscreenStartup
	Task                TaskHandler
	NewTask             TaskContextFactory
	Command             FullscreenCommandHandler
	Sessions            FullscreenSessionLister
	Resume              FullscreenSessionResumer
	CurrentSession      FullscreenCurrentSession
	CurrentSessionTitle FullscreenCurrentSessionTitle
	Rename              FullscreenSessionRenamer
	Delete              FullscreenSessionDeleter
	Compact             FullscreenCompactor
	Skills              FullscreenSkillLister
	SetSkill            FullscreenSkillSetter
	ClipboardWrite      FullscreenClipboardWriter
	OpenSessions        bool
	NoColor             bool
	DisableAnimations   bool
	Width               int
}

type FullscreenApplication struct {
	options FullscreenOptions

	programMutex sync.RWMutex
	program      *tea.Program
	done         chan struct{}

	cancelMutex sync.Mutex
	cancelRun   context.CancelFunc
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
	historyCells           []HistoryCell
	pendingHistoryCells    []HistoryCell
	hasEmittedHistoryLines bool
	historyMode            HistoryRenderMode
	draft                  string
	running                bool
	status                 string
	model                  string
	inputUsage             int64
	outputUsage            int64
	contextUsage           int64
	contextLimit           int64
	runDiffChanges         []event.RunDiffChange
	runDiffExact           bool
	history                []string
	historyPos             int
	queuedTasks            []TaskSubmission
	runStartedAt           time.Time
	palette                terminalPalette
	clock                  motionClock
	motion                 motionMode
	motionStartedAt        time.Time
	details                *transcriptDetailStore
	detailViewport         viewport.Model
	viewingDetails         bool
	approval               *fullscreenApproval
	sessions               []SessionOption
	slashPopup             slashCommandPopup
	collaboration          CollaborationMode
	selection              *selectionOverlay
	selectionKind          string
	skills                 []SkillOption
}

type fullscreenApproval struct {
	request  policy.ApprovalRequest
	response chan fullscreenApprovalResult
}

type fullscreenApprovalResult struct {
	decision policy.ApprovalDecision
	err      error
}

type fullscreenApprovalChoice struct {
	label    string
	decision policy.ApprovalDecision
}

var fullscreenApprovalChoices = []fullscreenApprovalChoice{
	{label: "Yes, allow once", decision: policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "user approved once"}},
	{label: "Yes, allow for this session", decision: policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalSession, Source: policy.ApprovalSourceUser, Reason: "user approved for the session"}},
	{label: "No, deny", decision: policy.ApprovalDecision{Outcome: policy.ApprovalDeny, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "user denied the request"}},
}

func approvalChoices(request policy.ApprovalRequest) []fullscreenApprovalChoice {
	if request.Purpose != policy.ApprovalPurposePermission {
		return fullscreenApprovalChoices
	}
	return []fullscreenApprovalChoice{
		{label: "Yes, allow for this run", decision: policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalRun, Source: policy.ApprovalSourceUser, Reason: "user approved for the run"}},
		{label: "Yes, allow for this session", decision: policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalSession, Source: policy.ApprovalSourceUser, Reason: "user approved for the session"}},
		{label: "No, deny", decision: policy.ApprovalDecision{Outcome: policy.ApprovalDeny, Scope: policy.ApprovalRun, Source: policy.ApprovalSourceUser, Reason: "user denied the request"}},
	}
}

type fullscreenEventMsg struct{ item event.Event }
type fullscreenApprovalMsg struct{ prompt *fullscreenApproval }
type fullscreenTaskDoneMsg struct {
	err     error
	session string
	elapsed time.Duration
}
type fullscreenCommandDoneMsg struct {
	command string
	output  string
	err     error
}
type fullscreenSessionsMsg struct {
	sessions []SessionOption
	err      error
}
type fullscreenResumeMsg struct {
	message string
	err     error
}
type fullscreenRenameMsg struct {
	message string
	err     error
}
type fullscreenDeleteMsg struct {
	message string
	err     error
}
type fullscreenCompactMsg struct {
	message string
	err     error
}
type fullscreenSkillsMsg struct {
	skills []SkillOption
	err    error
}
type fullscreenSkillSetMsg struct {
	name    string
	enabled bool
	err     error
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
	if options.Task == nil {
		return nil, errors.New("fullscreen TUI task handler is nil")
	}
	if options.NewTask == nil {
		options.NewTask = defaultTaskContext
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
	_, err := program.Run()
	app.cancelActiveRun()
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

func (app *FullscreenApplication) Publish(ctx context.Context, item event.Event) error {
	if item == nil {
		return errors.New("fullscreen TUI event is nil")
	}
	return app.send(ctx, fullscreenEventMsg{item: item})
}

func (app *FullscreenApplication) Decide(ctx context.Context, request policy.ApprovalRequest) (policy.ApprovalDecision, error) {
	if err := request.Validate(); err != nil {
		return policy.ApprovalDecision{}, fmt.Errorf("validate fullscreen approval request: %w", err)
	}
	response := make(chan fullscreenApprovalResult, 1)
	prompt := &fullscreenApproval{request: request, response: response}
	if err := app.send(ctx, fullscreenApprovalMsg{prompt: prompt}); err != nil {
		return policy.ApprovalDecision{}, err
	}
	select {
	case <-ctx.Done():
		return policy.ApprovalDecision{}, ctx.Err()
	case result := <-response:
		return result.decision, result.err
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

func (app *FullscreenApplication) setActiveRun(cancel context.CancelFunc) {
	app.cancelMutex.Lock()
	app.cancelRun = cancel
	app.cancelMutex.Unlock()
}

func (app *FullscreenApplication) clearActiveRun(cancel context.CancelFunc) {
	app.cancelMutex.Lock()
	if app.cancelRun != nil {
		app.cancelRun = nil
	}
	app.cancelMutex.Unlock()
}

func (app *FullscreenApplication) cancelActiveRun() {
	app.cancelMutex.Lock()
	cancel := app.cancelRun
	app.cancelRun = nil
	app.cancelMutex.Unlock()
	if cancel != nil {
		cancel()
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
	initialWidth := app.options.Width
	if initialWidth < 20 {
		initialWidth = 100
	}
	model := fullscreenModel{
		app: app, ctx: ctx, startup: startup, input: input, renderer: renderer,
		width: initialWidth, height: 30, status: "idle", model: startup.Model, historyPos: -1, collaboration: CollaborationExecute,
		palette: palette, clock: systemMotionClock{}, motion: motionAnimated, motionStartedAt: time.Now(), runDiffExact: true,
		details: newTranscriptDetailStore(0, 0), detailViewport: newTranscriptViewport(initialWidth, 30),
	}
	if app.options.DisableAnimations {
		model.motion = motionReduced
	}
	model.updateInputLayout()
	model.renderer, _ = newFullscreenMarkdownRenderer(maxInt(20, initialWidth-6), palette)
	return model
}

func (model fullscreenModel) Init() tea.Cmd {
	header := model.banner()
	if len(model.historyCells) > 0 {
		header += "\n\n" + model.renderHistoryCell(model.historyCells[0])
	}
	commands := []tea.Cmd{tea.Println(header), tea.HideCursor, model.input.Focus()}
	if model.app.options.OpenSessions && model.app.options.Sessions != nil {
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

func (model fullscreenModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.width = maxInt(40, message.Width)
		model.height = maxInt(10, message.Height)
		model.updateInputLayout()
		model.renderer, _ = newFullscreenMarkdownRenderer(maxInt(20, model.width-6), model.palette)
		model.resizeTranscriptViewport()
		return model, nil
	case fullscreenEventMsg:
		model.applyEvent(message.item)
		if model.viewingDetails && model.details != nil && !model.details.Empty() {
			model.refreshTranscriptViewport()
		}
		return model, model.flushHistory()
	case fullscreenApprovalMsg:
		model.approval = message.prompt
		choices := approvalChoices(message.prompt.request)
		items := make([]selectionItem, 0, len(choices))
		for _, choice := range choices {
			items = append(items, selectionItem{Name: choice.label})
		}
		model.selection = &selectionOverlay{Title: "Approval required", Subtitle: "Choose an option", Items: items}
		model.selectionKind = "approval"
		model.status = "awaiting approval"
		model.input.Blur()
		return model, nil
	case fullscreenTaskDoneMsg:
		runDuration := message.elapsed
		if runDuration <= 0 {
			runDuration = model.runElapsed()
		}
		model.finishDraft()
		model.flushActiveHistoryCell()
		if strings.TrimSpace(message.session) != "" {
			model.startup.Session = strings.TrimSpace(message.session)
		}
		if model.transcript.HadWorkActivity && model.transcript.NeedsFinalMessageSeparator {
			model.insertHistoryCell(FinalMessageSeparator{Elapsed: runDuration})
			model.transcript.NeedsFinalMessageSeparator = false
		}
		if message.err != nil {
			if errors.Is(message.err, context.Canceled) {
				model.insertHistoryCell(NewNoticeHistoryCell("当前任务已取消。"))
			} else {
				model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
			}
		}
		model.transcript.HadWorkActivity = false
		model.transcript.NeedsFinalMessageSeparator = false
		if len(model.queuedTasks) > 0 {
			next := model.queuedTasks[0]
			model.queuedTasks = model.queuedTasks[1:]
			model.details = newTranscriptDetailStore(0, 0)
			model.running = true
			model.runStartedAt = time.Now()
			model.motionStartedAt = model.runStartedAt
			model.runDiffChanges = nil
			model.runDiffExact = true
			model.status = taskPhase(next)
			model.draft = ""
			return model, tea.Sequence(model.flushHistory(), tea.Batch(model.runTask(next), model.workingTick()))
		}
		model.running = false
		model.status = "idle"
		return model, model.flushHistory()
	case fullscreenCommandDoneMsg:
		if message.err != nil {
			model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
		} else if strings.HasPrefix(strings.TrimSpace(message.command), "/clear") {
			model.resetHistory()
			model.details = newTranscriptDetailStore(0, 0)
			model.draft = ""
			model.collaboration = CollaborationExecute
			model.refreshCurrentSession()
			model.status = "idle"
			return model, tea.Sequence(func() tea.Msg { return tea.ClearScreen() }, tea.Println(model.banner()))
		} else if strings.TrimSpace(message.output) != "" {
			model.insertHistoryCell(NewNoticeHistoryCell(message.output))
		}
		model.status = "idle"
		return model, model.flushHistory()
	case fullscreenSessionsMsg:
		model.status = "idle"
		if message.err != nil {
			model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
			return model, model.flushHistory()
		}
		if len(message.sessions) == 0 {
			model.insertHistoryCell(NewNoticeHistoryCell("当前项目还没有可恢复的 Session。"))
			return model, model.flushHistory()
		}
		model.sessions = message.sessions
		selected := 0
		items := make([]selectionItem, 0, len(message.sessions))
		for index, session := range model.sessions {
			description := session.Title
			if session.Current {
				description += " · current"
			}
			items = append(items, selectionItem{Name: session.ID, Description: description})
			if session.Current {
				selected = index
			}
		}
		model.selection = &selectionOverlay{Title: "Resume Session", Subtitle: "Select a saved chat", Items: items, Selected: selected, Search: true, Hint: "Type to search · Esc cancel"}
		model.selectionKind = "resume"
		return model, nil
	case fullscreenResumeMsg:
		model.selection = nil
		model.selectionKind = ""
		model.sessions = nil
		model.status = "idle"
		if message.err != nil {
			model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
		} else if strings.TrimSpace(message.message) != "" {
			model.insertHistoryCell(NewNoticeHistoryCell(message.message))
			model.refreshCurrentSession()
			model.collaboration = CollaborationExecute
		}
		return model, model.flushHistory()
	case fullscreenRenameMsg:
		model.selection = nil
		model.selectionKind = ""
		model.status = "idle"
		if message.err != nil {
			model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
		} else {
			model.insertHistoryCell(NewNoticeHistoryCell(message.message))
			model.refreshCurrentSession()
		}
		return model, model.flushHistory()
	case fullscreenDeleteMsg:
		model.selection = nil
		model.selectionKind = ""
		model.status = "idle"
		if message.err != nil {
			model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
			return model, model.flushHistory()
		}
		model.insertHistoryCell(NewNoticeHistoryCell(message.message))
		return model, tea.Sequence(model.flushHistory(), tea.Quit)
	case fullscreenCompactMsg:
		model.status = "idle"
		if message.err != nil {
			model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
		} else {
			model.insertHistoryCell(NewNoticeHistoryCell(message.message))
		}
		return model, model.flushHistory()
	case fullscreenSkillsMsg:
		model.status = "idle"
		if message.err != nil {
			model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
			return model, model.flushHistory()
		}
		model.skills = message.skills
		if len(message.skills) == 0 {
			model.insertHistoryCell(NewNoticeHistoryCell("No skills available."))
			return model, model.flushHistory()
		}
		items := make([]selectionItem, 0, len(message.skills))
		for _, skill := range message.skills {
			state := "enabled"
			if !skill.Enabled {
				state = "disabled"
			}
			items = append(items, selectionItem{Name: skill.Name, Description: skill.Description + " · " + skill.Source + " · " + state})
		}
		model.selection = &selectionOverlay{Title: "Skills", Subtitle: "Enter toggles the selected skill", Items: items, Search: true, Hint: "Type to search · Enter toggle · Esc cancel"}
		model.selectionKind = "skills"
		return model, nil
	case fullscreenSkillSetMsg:
		if message.err != nil {
			model.insertHistoryCell(NewErrorHistoryCell(message.err.Error()))
			return model, model.flushHistory()
		}
		for index := range model.skills {
			if model.skills[index].Name == message.name {
				model.skills[index].Enabled = message.enabled
				if model.selection != nil && index < len(model.selection.Items) {
					state := "enabled"
					if !message.enabled {
						state = "disabled"
					}
					model.selection.Items[index].Description = model.skills[index].Description + " · " + model.skills[index].Source + " · " + state
				}
			}
		}
		return model, nil
	case fullscreenWorkingTickMsg:
		if !model.running || model.approval != nil {
			return model, nil
		}
		return model, model.workingTick()
	case tea.MouseMsg:
		model.lastMouseEvent = time.Now()
		return model, nil
	case tea.KeyMsg:
		if model.isRecentMouseControlFragment(message) {
			model.lastMouseEvent = time.Now()
			return model, nil
		}
		if isTerminalControlResponse(message) {
			return model, nil
		}
		if model.viewingDetails {
			return model.handleDetailViewerKey(message)
		}
		if model.selection != nil {
			return model.handleSelectionKey(message)
		}
		return model.handleInputKey(message)
	}
	return model, nil
}

func (model fullscreenModel) handleInputKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	model.slashPopup.sync(model.input.Value(), model.running)
	switch key.String() {
	case "shift+tab":
		if model.running {
			model.insertHistoryCell(NewNoticeHistoryCell("Collaboration mode cannot change while a task is in progress."))
			return model, model.flushHistory()
		}
		if model.collaboration == CollaborationPlan {
			model.collaboration = CollaborationExecute
			model.status = "idle"
			model.insertHistoryCell(NewNoticeHistoryCell("Switched to Execute mode"))
		} else {
			model.collaboration = CollaborationPlan
			model.status = "plan mode"
			model.insertHistoryCell(NewNoticeHistoryCell("Switched to Plan mode"))
		}
		return model, model.flushHistory()
	case "ctrl+c":
		if model.running {
			model.status = "cancelling"
			model.app.cancelActiveRun()
			return model, nil
		}
		model.input.Reset()
		model.historyPos = -1
		model.updateInputLayout()
		return model, nil
	case "ctrl+d":
		if !model.running && strings.TrimSpace(model.input.Value()) == "" {
			return model, tea.Quit
		}
	case "ctrl+t":
		if model.details == nil || model.details.Empty() {
			return model, nil
		}
		model.viewingDetails = true
		model.input.Blur()
		model.refreshTranscriptViewport()
		return model, nil
	case "esc":
		if model.slashPopup.active() {
			model.slashPopup.dismiss(model.input.Value())
			return model, nil
		}
		if model.running && strings.TrimSpace(model.input.Value()) == "" {
			model.status = "cancelling"
			model.app.cancelActiveRun()
			return model, nil
		}
		model.input.Reset()
		model.historyPos = -1
		model.updateInputLayout()
		return model, nil
	case "pgup", "pgdown":
		return model, nil
	case "up":
		if model.slashPopup.active() {
			model.slashPopup.move(-1)
			return model, nil
		}
		if !model.running && model.recallHistory(-1) {
			return model, nil
		}
	case "down":
		if model.slashPopup.active() {
			model.slashPopup.move(1)
			return model, nil
		}
		if !model.running && model.recallHistory(1) {
			return model, nil
		}
	case "tab":
		if selected, ok := model.slashPopup.selectedItem(); ok {
			value := "/" + string(selected.Command)
			if selected.SupportsInlineArgs {
				value += " "
			}
			model.input.SetValue(value)
			model.input.CursorEnd()
			model.slashPopup.dismiss(value)
			model.updateInputLayout()
			return model, nil
		}
	case "enter":
		if selected, ok := model.slashPopup.selectedItem(); ok {
			command := "/" + string(selected.Command)
			model.input.Reset()
			model.slashPopup.dismiss("")
			model.updateInputLayout()
			model.history = append(model.history, command)
			model.historyPos = -1
			return model.submitCommand(command)
		}
		text := strings.TrimSpace(model.input.Value())
		if text == "" {
			return model, nil
		}
		model.input.Reset()
		model.updateInputLayout()
		model.history = append(model.history, text)
		model.historyPos = -1
		if model.running {
			if strings.HasPrefix(text, "/") {
				spec, _, ok := ParseSlashCommand(text)
				if !ok || !spec.AvailableDuringRun {
					model.insertHistoryCell(NewNoticeHistoryCell("This command is disabled while a task is in progress."))
					return model, model.flushHistory()
				}
				return model.submitCommand(text)
			}
			if strings.HasPrefix(text, "/") {
				return model, model.flushHistory()
			}
			model.insertHistoryCell(NewUserMessageCell(text))
			model.queuedTasks = append(model.queuedTasks, TaskSubmission{Content: text, Mode: model.collaboration})
			model.status = fmt.Sprintf("%s · %d queued", model.status, len(model.queuedTasks))
			return model, model.flushHistory()
		}
		if strings.HasPrefix(text, "/") {
			return model.submitCommand(text)
		}
		model.insertHistoryCell(NewUserMessageCell(text))
		model.details = newTranscriptDetailStore(0, 0)
		model.running = true
		model.runStartedAt = time.Now()
		model.motionStartedAt = model.runStartedAt
		model.runDiffChanges = nil
		model.runDiffExact = true
		model.transcript.HadWorkActivity = false
		model.transcript.NeedsFinalMessageSeparator = false
		submission := TaskSubmission{Content: text, Mode: model.collaboration}
		model.status = taskPhase(submission)
		model.draft = ""
		return model, tea.Sequence(model.flushHistory(), tea.Batch(model.runTask(submission), model.workingTick()))
	}
	var command tea.Cmd
	model.input, command = model.input.Update(key)
	model.sanitizeInput()
	model.slashPopup.resetDismissal(model.input.Value())
	model.slashPopup.sync(model.input.Value(), model.running)
	model.updateInputLayout()
	return model, command
}

func (model *fullscreenModel) applyEvent(item event.Event) {
	if item == nil {
		return
	}
	switch item := item.(type) {
	case event.LLMCallStarted:
		if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
			model.flushActiveHistoryCell()
		}
		if strings.TrimSpace(item.Model.Name) != "" {
			model.model = item.Model.Name
		}
	case event.TextDelta:
		if model.draft == "" && model.transcript.HadWorkActivity && model.transcript.NeedsFinalMessageSeparator {
			model.flushActiveHistoryCell()
			model.insertHistoryCell(FinalMessageSeparator{Elapsed: model.runElapsed()})
			model.transcript.NeedsFinalMessageSeparator = false
		}
		model.draft += item.Delta
	case event.ReasoningDelta:
		if strings.TrimSpace(item.Delta) != "" {
			model.status = "thinking"
		}
	case event.UsageUpdated:
		model.inputUsage = item.Usage.InputTokens
		model.outputUsage = item.Usage.OutputTokens
	case event.ContextWindowUpdated:
		model.contextUsage = item.EstimatedInputTokens
		if item.ContextWindow > 0 {
			model.contextLimit = item.ContextWindow
		}
	case event.PlanUpdated:
		model.finishDraft()
		model.insertHistoryCell(NewPlanUpdateCell(item))
		model.status = "planning"
	case event.RunDiffUpdated:
		model.runDiffChanges = append([]event.RunDiffChange(nil), item.Changes...)
		model.runDiffExact = true
	case event.RunDiffInvalidated:
		model.runDiffChanges = nil
		model.runDiffExact = false
	case event.IterationStarted, event.IterationCompleted:
		// Reactor iterations are diagnostic boundaries, not transcript layout boundaries.
	case event.ToolCallStarted:
		if item.ToolName == "update_plan" {
			return
		}
		model.finishDraft()
		if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
			model.flushActiveHistoryCell()
		}
		if model.transcript.ActiveCell == nil {
			model.transcript.ActiveCell = newToolHistoryCell()
		}
		if model.transcript.ActiveCell.Apply(item) {
			model.transcript.bumpActiveCellRevision()
		}
		model.transcript.HadWorkActivity = true
		model.transcript.NeedsFinalMessageSeparator = true
		model.status = "executing"
	case event.ToolCallCompleted:
		if item.ToolName == "update_plan" {
			return
		}
		model.finishDraft()
		if model.transcript.ActiveCell == nil {
			cell := newToolHistoryCell()
			cell.Apply(event.ToolCallStarted{CallID: item.CallID, ToolName: item.ToolName, Iteration: item.Iteration})
			model.transcript.ActiveCell = cell
		}
		if model.transcript.ActiveCell.Apply(item) {
			model.transcript.bumpActiveCellRevision()
		}
		model.transcript.HadWorkActivity = true
		model.transcript.NeedsFinalMessageSeparator = true
		if model.details == nil {
			model.details = newTranscriptDetailStore(0, 0)
		}
		if cell, ok := model.transcript.ActiveCell.(*ToolHistoryCell); ok {
			activity := cell.byCallID[item.CallID]
			if activity != nil {
				detailContent := strings.TrimSpace(strings.Join([]string{activity.Detail, item.Summary}, "\n\n"))
				if detail, ok := model.details.Add(item.CallID, activity.Title, detailContent); ok {
					activity.ResultDetailID = detail.ID
					activity.DetailAvailable = activity.Kind == activityExplore || detail.Truncated || strings.Count(item.Summary, "\n") >= 5 || len([]rune(item.Summary)) > 600
					activity.Result = detail.Content
				}
			}
		}
	case event.ApprovalRequested:
		model.status = "awaiting approval"
	case event.ApprovalResolved:
		model.finishDraft()
		model.insertHistoryCell(NewNoticeHistoryCell(fmt.Sprintf("Approval · %s · %s", item.ToolName, item.Outcome)))
	case event.TurnStatusChanged:
		if strings.TrimSpace(item.To) != "" {
			model.status = item.To
		}
	case event.StatusChanged:
		if strings.TrimSpace(item.To) != "" {
			model.status = item.To
		}
	case event.DiagnosticPublished:
		model.finishDraft()
		content := strings.TrimSpace(strings.Join([]string{item.Severity, item.Code, item.Message}, " "))
		model.insertHistoryCell(NewDiagnosticHistoryCell(content))
	case event.ErrorOccurred:
		model.finishDraft()
		if strings.TrimSpace(item.Error.Message) != "" {
			model.insertHistoryCell(NewErrorHistoryCell(item.Error.Message))
		}
	case event.TurnCompleted:
		if model.transcript.ActiveCell != nil && model.transcript.ActiveCell.IsComplete() {
			model.flushActiveHistoryCell()
		}
		model.status = item.Status
	}
}

func (model *fullscreenModel) finishDraft() {
	if strings.TrimSpace(model.draft) != "" {
		model.transcript.LastAgentMarkdown = model.draft
		model.insertHistoryCell(NewAgentMessageCell(model.draft))
	}
	model.draft = ""
}

func (model *fullscreenModel) insertHistoryCell(cell HistoryCell) {
	if model == nil || cell == nil {
		return
	}
	if len(cell.RawLines()) == 0 && len(cell.DisplayLines(model.historyRenderContext())) == 0 {
		return
	}
	model.historyCells = append(model.historyCells, cell)
	model.pendingHistoryCells = append(model.pendingHistoryCells, cell)
}

func (model *fullscreenModel) flushActiveHistoryCell() {
	if model == nil || model.transcript.ActiveCell == nil {
		return
	}
	model.insertHistoryCell(model.transcript.ActiveCell.Complete())
	model.transcript.ActiveCell = nil
	model.transcript.bumpActiveCellRevision()
}

func (model *fullscreenModel) resetHistory() {
	if model == nil {
		return
	}
	model.transcript.reset()
	model.historyCells = nil
	model.pendingHistoryCells = nil
	model.hasEmittedHistoryLines = false
}

func (model fullscreenModel) historyRenderContext() HistoryRenderContext {
	now := time.Now()
	if model.clock != nil {
		now = model.clock.Now()
	}
	return HistoryRenderContext{
		Width: maxInt(36, model.width-3), Palette: model.palette, Markdown: model.renderer,
		Now: now, MotionStart: model.motionStartedAt, Motion: model.motion,
	}
}

func (model fullscreenModel) renderHistoryCell(cell HistoryCell) (rendered string) {
	if cell == nil {
		return ""
	}
	rendered = renderStyledLines(historyLinesForMode(cell, model.historyMode, model.historyRenderContext()), model.historyRenderContext())
	if model.app != nil && model.app.options.NoColor {
		return xansi.Strip(rendered)
	}
	return rendered
}

func (model *fullscreenModel) recallHistory(direction int) bool {
	if len(model.history) == 0 || strings.Contains(model.input.Value(), "\n") {
		return false
	}
	if model.historyPos < 0 {
		if direction > 0 {
			return false
		}
		model.historyPos = len(model.history) - 1
	} else {
		model.historyPos += direction
		if model.historyPos < 0 {
			model.historyPos = 0
		}
		if model.historyPos >= len(model.history) {
			model.historyPos = -1
			model.input.Reset()
			return true
		}
	}
	model.input.SetValue(model.history[model.historyPos])
	model.input.CursorEnd()
	model.updateInputLayout()
	return true
}

func (model *fullscreenModel) completeSlashCommand() bool {
	value := strings.TrimSpace(model.input.Value())
	if !strings.HasPrefix(value, "/") || strings.Contains(value, " ") {
		return false
	}
	matches := make([]string, 0)
	for _, command := range SlashCommands() {
		if strings.HasPrefix(command, value) {
			matches = append(matches, command)
		}
	}
	if len(matches) != 1 {
		return false
	}
	model.input.SetValue(matches[0])
	model.input.CursorEnd()
	return true
}

func (model fullscreenModel) View() (rendered string) {
	defer func() {
		if model.app != nil && model.app.options.NoColor {
			rendered = xansi.Strip(rendered)
		}
	}()
	if model.viewingDetails {
		return model.renderTranscriptViewer()
	}
	input := model.inputBox()
	status := model.statusBar()
	parts := make([]string, 0, 3)
	if active := model.renderActiveCell(); active != "" {
		parts = append(parts, active)
	}
	if draft := model.renderActiveDraft(); draft != "" {
		parts = append(parts, draft)
	}
	if working := model.workingLine(); working != "" {
		parts = append(parts, working)
	}
	activity := strings.Join(parts, "\n\n")
	inputRegion := input + "\n\n" + status
	if activity == "" {
		return "\n\n" + inputRegion
	}
	if model.hasEmittedHistoryLines {
		activity = "\n" + activity
	}
	return activity + "\n\n\n" + inputRegion
}

func newTranscriptViewport(width, height int) viewport.Model {
	viewer := viewport.New(maxInt(20, width-4), maxInt(3, height-4))
	viewer.MouseWheelEnabled = false
	return viewer
}

func (model *fullscreenModel) resizeTranscriptViewport() {
	if model == nil {
		return
	}
	model.detailViewport.Width = maxInt(20, model.width-4)
	model.detailViewport.Height = maxInt(3, model.height-4)
}

func (model *fullscreenModel) refreshTranscriptViewport() {
	if model == nil {
		return
	}
	model.resizeTranscriptViewport()
	model.detailViewport.SetContent(model.details.Render())
	model.detailViewport.GotoTop()
}

func (model fullscreenModel) handleDetailViewerKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "ctrl+t":
		model.viewingDetails = false
		return model, model.input.Focus()
	case "ctrl+c":
		model.viewingDetails = false
		return model, model.input.Focus()
	}
	var command tea.Cmd
	model.detailViewport, command = model.detailViewport.Update(key)
	return model, command
}

func (model fullscreenModel) renderTranscriptViewer() string {
	header := model.palette.strong().Render("Transcript Details") + "  " + model.palette.dim().Render("↑/↓ · PgUp/PgDn · Esc return")
	footer := model.palette.dim().Render(fmt.Sprintf("%d retained item(s) · bounded in memory", len(model.details.items)))
	return strings.Join([]string{header, model.detailViewport.View(), footer}, "\n")
}

func (model fullscreenModel) banner() (rendered string) {
	defer func() {
		if model.app != nil && model.app.options.NoColor {
			rendered = xansi.Strip(rendered)
		}
	}()
	width := maxInt(40, model.width)
	logo := model.palette.plain().Render(strings.Trim(terminalLogo(width), "\r\n"))
	version := strings.TrimSpace(model.startup.Version)
	if version != "" {
		version = " (" + version + ")"
	}
	title := model.palette.bold().Render("Amadeus") + model.palette.dim().Render(version)
	modelName := strings.TrimSpace(model.model)
	project := strings.TrimSpace(model.startup.Project)
	rows := []string{title}
	rowWidth := width
	if width >= 60 {
		rowWidth = maxInt(20, width-8)
	}
	if modelName != "" {
		rows = append(rows, bannerMetadataRow("model:", modelName, rowWidth))
	}
	if project != "" {
		rows = append(rows, bannerMetadataRow("directory:", project, rowWidth))
	}
	if width < 60 {
		return logo + "\n" + strings.Join(rows, "\n")
	}
	panelWidth := maxInt(24, width-6)
	panel := fullscreenPanelStyle.BorderForeground(model.palette.border().GetForeground()).Width(panelWidth).Render(strings.Join(rows, "\n"))
	return logo + "\n\n" + panel
}

func bannerMetadataRow(label, value string, width int) string {
	const labelWidth = 11
	if width <= labelWidth {
		return truncateFullscreen(strings.TrimSpace(label)+strings.TrimSpace(value), width)
	}
	return fmt.Sprintf("%-*s%s", labelWidth, label, truncateFullscreen(strings.TrimSpace(value), width-labelWidth))
}

func isTerminalControlResponse(message tea.KeyMsg) bool {
	value := message.String()
	if len(message.Runes) > 0 {
		value += string(message.Runes)
	}
	return terminalMouseResponseRE.MatchString(value) ||
		strings.Contains(value, "]11;") ||
		strings.Contains(value, "]10;") ||
		strings.Contains(value, "]12;") ||
		strings.Contains(value, "rgb:") ||
		strings.Contains(value, "\x1b]") ||
		strings.ContainsRune(value, '\u009d')
}

func (model fullscreenModel) isRecentMouseControlFragment(message tea.KeyMsg) bool {
	if model.lastMouseEvent.IsZero() || time.Since(model.lastMouseEvent) > terminalControlFragmentWindow {
		return false
	}
	value := message.String()
	if len(message.Runes) > 0 {
		value = string(message.Runes)
	}
	return value != "" && strings.Trim(value, "[") == ""
}

func (model *fullscreenModel) sanitizeInput() {
	value := model.input.Value()
	clean := stripTerminalControlResponses(value)
	if clean != value {
		model.input.SetValue(clean)
		model.input.CursorEnd()
	}
}

var (
	terminalControlResponseRE = regexp.MustCompile(`(?s)(?:\x1b\]|\])?(?:10|11|12);rgb:[0-9a-fA-F]{1,4}/[0-9a-fA-F]{1,4}/[0-9a-fA-F]{1,4}(?:\x1b\\|\\)?`)
	terminalMouseResponseRE   = regexp.MustCompile(`(?:\x1b\[|\x{009b}|\[)?<\d{1,3};\d{1,4};\d{1,4}[mM]`)
)

const terminalControlFragmentWindow = 300 * time.Millisecond

func stripTerminalControlResponses(value string) string {
	value = terminalControlResponseRE.ReplaceAllString(value, "")
	value = terminalMouseResponseRE.ReplaceAllString(value, "")
	value = strings.ReplaceAll(value, "]11;", "")
	value = strings.ReplaceAll(value, "]10;", "")
	value = strings.ReplaceAll(value, "]12;", "")
	return strings.TrimLeft(value, "\x1b\\] ")
}

func (model fullscreenModel) inputBox() string {
	width := maxInt(40, model.width)
	if model.selection != nil {
		return model.renderSelectionOverlay(width)
	}
	input := fullscreenInputFillStyle.Width(width).Render(strings.TrimRight(model.input.View(), "\n"))
	if model.slashPopup.active() {
		visible, start := model.slashPopup.visibleItems()
		items := make([]listVisualItem, 0, len(visible))
		for index, spec := range visible {
			items = append(items, listVisualItem{
				Name: "/" + string(spec.Command), Description: spec.Description,
				Selected: start+index == model.slashPopup.selected,
			})
		}
		list := model.renderListVisual(listVisual{
			Title: "Commands", Hint: "↑/↓ select · Enter insert · Esc dismiss", Items: items,
		}, width)
		return input + "\n\n" + list
	}
	return input
}

func (model fullscreenModel) statusBar() string {
	width := maxInt(40, model.width)
	parts := []statusBarPart{}
	if modelName := strings.TrimSpace(model.model); modelName != "" {
		parts = append(parts, statusBarPart{text: modelName, style: model.palette.statusLineStyle(statusAccentModel)})
	} else {
		parts = append(parts, statusBarPart{text: "AMADEUS", style: model.palette.statusLineStyle(statusAccentModel)})
	}
	if project := strings.TrimSpace(model.startup.Project); project != "" {
		parts = append(parts, statusBarPart{text: project, style: model.palette.statusLineStyle(statusAccentPath)})
	}
	if branch := strings.TrimSpace(model.startup.Branch); branch != "" {
		parts = append(parts, statusBarPart{text: branch, style: model.palette.statusLineStyle(statusAccentBranch)})
	}
	if model.collaboration == CollaborationPlan {
		parts = append(parts, statusBarPart{text: "Plan", style: model.palette.statusLineStyle(statusAccentMode)})
	}
	contextWindow := model.contextLimit
	if contextWindow <= 0 {
		contextWindow = model.startup.ContextWindow
	}
	if contextWindow > 0 {
		percent := int64(0)
		if model.contextUsage > 0 {
			percent = minInt64(100, model.contextUsage*100/contextWindow)
		}
		contextStyle := statusContextStyle(model.palette, percent)
		parts = append(parts,
			statusBarPart{text: fmt.Sprintf("Context %d%% used", percent), style: contextStyle},
			statusBarPart{text: compactTokenCount(contextWindow) + " window", style: model.palette.statusLineStyle(statusAccentUsage)},
		)
	}
	if len(parts) == 1 && strings.TrimSpace(model.startup.Project) == "" && strings.TrimSpace(model.startup.Branch) == "" && contextWindow <= 0 {
		parts = append(parts, statusBarPart{text: model.status, style: model.palette.dim()})
	}
	line := renderStatusBarParts(parts, model.palette)
	if lipgloss.Width(line) > width {
		for len(parts) > 1 && lipgloss.Width(renderStatusBarParts(parts, model.palette)) > width {
			parts = append(parts[:1], parts[2:]...)
		}
		line = xansi.Truncate(renderStatusBarParts(parts, model.palette), width, "")
	}
	return line
}

func statusContextStyle(palette terminalPalette, percent int64) lipgloss.Style {
	switch {
	case percent >= 90:
		return palette.failure()
	case percent >= 70:
		return palette.warning()
	default:
		return palette.statusLineStyle(statusAccentUsage)
	}
}

type statusBarPart struct {
	text  string
	style lipgloss.Style
}

func renderStatusBarParts(parts []statusBarPart, palette terminalPalette) string {
	var builder strings.Builder
	separator := palette.dim().Render(" · ")
	for index, part := range parts {
		if index > 0 {
			builder.WriteString(separator)
		}
		builder.WriteString(part.style.Render(part.text))
	}
	return builder.String()
}

func (model fullscreenModel) workingLine() string {
	if !model.running || model.approval != nil {
		return ""
	}
	now := time.Now()
	if model.clock != nil {
		now = model.clock.Now()
	}
	elapsed := elapsedRunDurationAt(model.runStartedAt, now)
	marker := activityIndicator(now, model.motionStartedAt, model.motion, model.palette)
	word := shimmerText("Working", now, model.motionStartedAt, model.motion, model.palette)
	line := marker + " " + word + model.palette.dim().Render(fmt.Sprintf(" (%s • esc to interrupt)", formatElapsedCompact(elapsed)))
	return xansi.Truncate(line, maxInt(12, model.width), "")
}

func (model fullscreenModel) runElapsed() time.Duration {
	now := time.Now()
	if model.clock != nil {
		now = model.clock.Now()
	}
	return elapsedRunDurationAt(model.runStartedAt, now)
}

func elapsedRunDurationAt(startedAt, now time.Time) time.Duration {
	if startedAt.IsZero() {
		return 0
	}
	elapsed := now.Sub(startedAt).Round(time.Second)
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

func compactTokenCount(value int64) string {
	switch {
	case value >= 1_000_000:
		if value%1_000_000 == 0 {
			return fmt.Sprintf("%dM", value/1_000_000)
		}
		return fmt.Sprintf("%.1fM", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%dK", value/1_000)
	default:
		return fmt.Sprintf("%d", value)
	}
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func (model *fullscreenModel) updateInputLayout() {
	width := maxInt(40, model.width)
	model.input.SetWidth(width - 2)
	rows := 1 + strings.Count(model.input.Value(), "\n")
	if rows > fullscreenMaxInputRows {
		rows = fullscreenMaxInputRows
	}
	model.input.SetHeight(rows)
}

func (model fullscreenModel) transcriptContent() string {
	cells := append([]HistoryCell(nil), model.historyCells...)
	if model.draft != "" {
		cells = append(cells, NewAgentMessageCell(model.draft))
	}
	if model.transcript.ActiveCell != nil {
		cells = append(cells, model.transcript.ActiveCell)
	}
	return renderHistoryCells(cells, model.historyMode, model.historyRenderContext())
}

func (model *fullscreenModel) displayLinesForHistoryInsert(cell HistoryCell) []styledLine {
	if model == nil || cell == nil {
		return nil
	}
	lines := historyLinesForMode(cell, model.historyMode, model.historyRenderContext())
	if len(lines) == 0 {
		return nil
	}
	if model.hasEmittedHistoryLines && !cell.IsStreamContinuation() {
		lines = append([]styledLine{{}}, lines...)
	}
	model.hasEmittedHistoryLines = true
	return lines
}

func (model *fullscreenModel) flushHistory() tea.Cmd {
	if model == nil || len(model.pendingHistoryCells) == 0 {
		return nil
	}
	pending := append([]HistoryCell(nil), model.pendingHistoryCells...)
	model.pendingHistoryCells = nil
	var lines []styledLine
	for _, cell := range pending {
		lines = append(lines, model.displayLinesForHistoryInsert(cell)...)
	}
	output := renderStyledLines(lines, model.historyRenderContext())
	if output == "" {
		return nil
	}
	return tea.Println(output)
}

func (model fullscreenModel) renderActiveDraft() string {
	if strings.TrimSpace(model.draft) == "" {
		return ""
	}
	available := maxInt(1, model.height-lipgloss.Height(model.inputBox())-lipgloss.Height(model.statusBar())-2)
	sourceLines := strings.Split(model.draft, "\n")
	if len(sourceLines) > available {
		sourceLines = sourceLines[len(sourceLines)-available:]
	}
	rendered := model.renderHistoryCell(NewAgentMessageCell(strings.Join(sourceLines, "\n")))
	lines := strings.Split(rendered, "\n")
	if len(lines) > available {
		lines = lines[len(lines)-available:]
	}
	return strings.Join(lines, "\n")
}

func (model fullscreenModel) renderActiveCell() string {
	if model.transcript.ActiveCell == nil {
		return ""
	}
	rendered := model.renderHistoryCell(model.transcript.ActiveCell)
	available := maxInt(1, model.height-lipgloss.Height(model.inputBox())-lipgloss.Height(model.statusBar())-4)
	lines := strings.Split(rendered, "\n")
	if len(lines) > available {
		lines = lines[len(lines)-available:]
	}
	return strings.Join(lines, "\n")
}

func prefixRenderedBlock(value, prefix string) string {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(xansi.Strip(lines[0])) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(xansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return strings.TrimSpace(prefix)
	}
	lines[0] = prefix + lines[0]
	return strings.Join(lines, "\n")
}

func sanitizeFullscreenContent(value string) string {
	value = xansi.Strip(value)
	var builder strings.Builder
	for _, character := range value {
		if character == '\n' || character == '\t' || !unicode.IsControl(character) {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func truncateFullscreen(value string, width int) string {
	if width <= 0 || lipgloss.Width(value) <= width {
		return value
	}
	return xansi.Truncate(value, width, "…")
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

var _ event.Sink = (*FullscreenApplication)(nil)
var _ policy.ApprovalHandler = (*FullscreenApplication)(nil)
