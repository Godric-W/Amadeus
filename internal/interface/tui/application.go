package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
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

type SessionOption struct {
	ID      string
	Title   string
	Current bool
}

type FullscreenOptions struct {
	Input          io.Reader
	Output         io.Writer
	Startup        FullscreenStartup
	Task           TaskHandler
	NewTask        TaskContextFactory
	Command        FullscreenCommandHandler
	Sessions       FullscreenSessionLister
	Resume         FullscreenSessionResumer
	CurrentSession FullscreenCurrentSession
	OpenSessions   bool
	NoColor        bool
	Width          int
}

type FullscreenApplication struct {
	options FullscreenOptions

	programMutex sync.RWMutex
	program      *tea.Program
	done         chan struct{}

	cancelMutex sync.Mutex
	cancelRun   context.CancelFunc
}

type fullscreenEntry struct {
	kind       string
	content    string
	successful bool
}

type fullscreenModel struct {
	app              *FullscreenApplication
	ctx              context.Context
	startup          FullscreenStartup
	input            textarea.Model
	renderer         *glamour.TermRenderer
	lastMouseEvent   time.Time
	width            int
	height           int
	entries          []fullscreenEntry
	committed        int
	draft            string
	running          bool
	status           string
	model            string
	inputUsage       int64
	outputUsage      int64
	contextUsage     int64
	contextLimit     int64
	history          []string
	historyPos       int
	queuedTasks      []string
	runStartedAt     time.Time
	workingFrame     int
	activitySeq      int
	iterations       map[int]*iterationActivity
	activeTools      map[string]*toolActivity
	details          *transcriptDetailStore
	detailViewport   viewport.Model
	viewingDetails   bool
	approval         *fullscreenApproval
	approvalSelected int
	sessions         []SessionOption
	selecting        bool
	selected         int
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

type fullscreenEventMsg struct{ item event.Event }
type fullscreenApprovalMsg struct{ prompt *fullscreenApproval }
type fullscreenTaskDoneMsg struct {
	err     error
	session string
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
type fullscreenWorkingTickMsg time.Time

const (
	fullscreenInputPrompt      = "> "
	fullscreenInputPlaceholder = "输入任务，或输入 /help 查看命令"
	fullscreenInputCharLimit   = 20000
	fullscreenMaxInputRows     = 5
	fullscreenWorkingInterval  = 36 * time.Millisecond
	workingBeamSubframes       = 3
	workingBeamPadding         = 2 * workingBeamSubframes
	workingBeamPauseFrames     = 6
)

var (
	fullscreenLogoStyle         = lipgloss.NewStyle()
	fullscreenTitleStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	fullscreenSectionStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	fullscreenMutedStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	fullscreenPanelStyle        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")).Padding(0, 1)
	fullscreenUserStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	fullscreenAssistantStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	fullscreenErrorStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	fullscreenInputFillStyle    = lipgloss.NewStyle()
	fullscreenInputPromptStyle  = lipgloss.NewStyle()
	fullscreenInputTextStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	fullscreenPlaceholderStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	fullscreenPhaseStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	fullscreenToolStyle         = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(lipgloss.Color("39")).PaddingLeft(1)
	fullscreenResultStyle       = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(lipgloss.Color("244")).Foreground(lipgloss.Color("245")).PaddingLeft(1)
	fullscreenPlanStyle         = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(lipgloss.Color("220")).PaddingLeft(1)
	fullscreenApprovalStyle     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("220")).Padding(0, 1)
	fullscreenSelectedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	fullscreenWorkingInkStyle   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "16", Dark: "15"})
	fullscreenWorkingMetaStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#71717A", Dark: "#A1A1AA"})
	fullscreenActivityVerbStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#67E8F9"}).Bold(true)
	fullscreenActivityOKStyle   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#86EFAC"}).Bold(true)
	fullscreenResumeAccentStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#A16207", Dark: "#FDE68A"}).Bold(true)
	fullscreenStatusModelStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#0369A1", Dark: "#7DD3FC"})
	fullscreenStatusPathStyle   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#1D4ED8", Dark: "#93C5FD"})
	fullscreenStatusGitStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#7E22CE", Dark: "#C4B5FD"})
	fullscreenStatusGoodStyle   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#86EFAC"})
	fullscreenStatusWarnStyle   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#A16207", Dark: "#FDE68A"})
	fullscreenStatusBadStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#FCA5A5"})
	fullscreenStatusMutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#71717A", Dark: "#A1A1AA"})
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
	return &FullscreenApplication{options: options, done: make(chan struct{})}, nil
}

func (app *FullscreenApplication) Run(ctx context.Context) error {
	if app == nil {
		return errors.New("fullscreen TUI application is nil")
	}
	model := newFullscreenModel(ctx, app)
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
	input := textarea.New()
	input.Placeholder = fullscreenInputPlaceholder
	input.Prompt = fullscreenInputPrompt
	input.ShowLineNumbers = false
	input.EndOfBufferCharacter = ' '
	input.CharLimit = fullscreenInputCharLimit
	input.MaxHeight = fullscreenMaxInputRows
	input.FocusedStyle.Base = fullscreenInputFillStyle
	input.FocusedStyle.CursorLine = fullscreenInputFillStyle
	input.FocusedStyle.Prompt = fullscreenInputPromptStyle
	input.FocusedStyle.Placeholder = fullscreenPlaceholderStyle
	input.FocusedStyle.Text = fullscreenInputTextStyle
	input.FocusedStyle.EndOfBuffer = fullscreenInputFillStyle
	input.SetWidth(80)
	input.SetHeight(1)
	input.Focus()
	renderer, _ := newFullscreenMarkdownRenderer(94)
	startup := app.options.Startup
	initialWidth := app.options.Width
	if initialWidth < 20 {
		initialWidth = 100
	}
	model := fullscreenModel{
		app: app, ctx: ctx, startup: startup, input: input, renderer: renderer,
		width: initialWidth, height: 30, status: "idle", model: startup.Model, historyPos: -1,
		iterations: map[int]*iterationActivity{}, activeTools: map[string]*toolActivity{},
		details: newTranscriptDetailStore(0, 0), detailViewport: newTranscriptViewport(initialWidth, 30),
	}
	model.updateInputLayout()
	model.renderer, _ = newFullscreenMarkdownRenderer(maxInt(20, initialWidth-6))
	return model
}

func (model fullscreenModel) Init() tea.Cmd {
	header := model.banner()
	if len(model.entries) > 0 {
		header += "\n\n" + model.renderEntry(model.entries[0])
	}
	commands := []tea.Cmd{tea.Println(header), tea.HideCursor, model.input.Focus()}
	if model.app.options.OpenSessions && model.app.options.Sessions != nil {
		commands = append(commands, model.loadSessions())
	}
	return tea.Sequence(commands...)
}

func fullscreenWorkingTick() tea.Cmd {
	return tea.Tick(fullscreenWorkingInterval, func(now time.Time) tea.Msg {
		return fullscreenWorkingTickMsg(now)
	})
}

func (model fullscreenModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.width = maxInt(40, message.Width)
		model.height = maxInt(10, message.Height)
		model.updateInputLayout()
		model.renderer, _ = newFullscreenMarkdownRenderer(maxInt(20, model.width-6))
		model.resizeTranscriptViewport()
		return model, nil
	case fullscreenEventMsg:
		model.applyEvent(message.item)
		if model.viewingDetails && model.details != nil && !model.details.Empty() {
			model.refreshTranscriptViewport()
		}
		return model, model.flushTranscript()
	case fullscreenApprovalMsg:
		model.approval = message.prompt
		model.approvalSelected = 0
		model.status = "awaiting approval"
		model.input.Blur()
		return model, nil
	case fullscreenTaskDoneMsg:
		runDuration := elapsedRunDuration(model.runStartedAt)
		model.finishDraft()
		if strings.TrimSpace(message.session) != "" {
			model.startup.Session = strings.TrimSpace(message.session)
		}
		if message.err != nil {
			if errors.Is(message.err, context.Canceled) {
				model.entries = append(model.entries, fullscreenEntry{kind: "notice", content: "当前任务已取消。"})
			} else {
				model.entries = append(model.entries, fullscreenEntry{kind: "error", content: message.err.Error()})
			}
		} else {
			model.entries = append(model.entries, fullscreenEntry{kind: "worked", content: formatRunDuration(runDuration)})
		}
		if len(model.queuedTasks) > 0 {
			next := model.queuedTasks[0]
			model.queuedTasks = model.queuedTasks[1:]
			model.details = newTranscriptDetailStore(0, 0)
			model.running = true
			model.runStartedAt = time.Now()
			model.workingFrame = 0
			model.status = taskPhase(next)
			model.draft = ""
			return model, tea.Sequence(model.flushTranscript(), tea.Batch(model.runTask(next), fullscreenWorkingTick()))
		}
		model.running = false
		model.status = "idle"
		return model, model.flushTranscript()
	case fullscreenCommandDoneMsg:
		if message.err != nil {
			model.entries = append(model.entries, fullscreenEntry{kind: "error", content: message.err.Error()})
		} else if strings.TrimSpace(message.output) != "" {
			model.entries = append(model.entries, fullscreenEntry{kind: "notice", content: message.output})
		}
		model.status = "idle"
		return model, model.flushTranscript()
	case fullscreenSessionsMsg:
		model.status = "idle"
		if message.err != nil {
			model.entries = append(model.entries, fullscreenEntry{kind: "error", content: message.err.Error()})
			return model, model.flushTranscript()
		}
		if len(message.sessions) == 0 {
			model.entries = append(model.entries, fullscreenEntry{kind: "notice", content: "当前项目还没有可恢复的 Session。"})
			return model, model.flushTranscript()
		}
		model.sessions = message.sessions
		model.selecting = true
		model.selected = 0
		for index, session := range model.sessions {
			if session.Current {
				model.selected = index
				break
			}
		}
		return model, nil
	case fullscreenResumeMsg:
		model.selecting = false
		model.sessions = nil
		model.status = "idle"
		if message.err != nil {
			model.entries = append(model.entries, fullscreenEntry{kind: "error", content: message.err.Error()})
		} else if strings.TrimSpace(message.message) != "" {
			model.entries = append(model.entries, fullscreenEntry{kind: "notice", content: message.message})
			model.refreshCurrentSession()
		}
		return model, model.flushTranscript()
	case fullscreenWorkingTickMsg:
		if !model.running || model.approval != nil {
			return model, nil
		}
		model.workingFrame++
		return model, fullscreenWorkingTick()
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
		if model.approval != nil {
			return model.handleApprovalKey(message)
		}
		if model.selecting {
			return model.handleSessionKey(message)
		}
		return model.handleInputKey(message)
	}
	return model, nil
}

func (model fullscreenModel) handleInputKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
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
		if !model.running && model.recallHistory(-1) {
			return model, nil
		}
	case "down":
		if !model.running && model.recallHistory(1) {
			return model, nil
		}
	case "tab":
		if !model.running && model.completeSlashCommand() {
			return model, nil
		}
	case "enter":
		text := strings.TrimSpace(model.input.Value())
		if text == "" {
			return model, nil
		}
		model.input.Reset()
		model.updateInputLayout()
		model.history = append(model.history, text)
		model.historyPos = -1
		if model.running {
			if strings.HasPrefix(text, "/") && !isPlanTask(text) {
				model.entries = append(model.entries, fullscreenEntry{kind: "notice", content: "Agent 执行期间只能排队普通任务或 /plan <task>。"})
				return model, model.flushTranscript()
			}
			model.entries = append(model.entries, fullscreenEntry{kind: "user", content: text})
			model.queuedTasks = append(model.queuedTasks, text)
			model.status = fmt.Sprintf("%s · %d queued", model.status, len(model.queuedTasks))
			return model, model.flushTranscript()
		}
		if strings.HasPrefix(text, "/") {
			return model.submitCommand(text)
		}
		model.entries = append(model.entries, fullscreenEntry{kind: "user", content: text})
		model.details = newTranscriptDetailStore(0, 0)
		model.running = true
		model.runStartedAt = time.Now()
		model.workingFrame = 0
		model.status = taskPhase(text)
		model.draft = ""
		return model, tea.Sequence(model.flushTranscript(), tea.Batch(model.runTask(text), fullscreenWorkingTick()))
	}
	var command tea.Cmd
	model.input, command = model.input.Update(key)
	model.sanitizeInput()
	model.updateInputLayout()
	return model, command
}

func (model fullscreenModel) submitCommand(command string) (tea.Model, tea.Cmd) {
	name := strings.Fields(command)[0]
	switch name {
	case "/plan":
		if !isPlanTask(command) {
			model.entries = append(model.entries, fullscreenEntry{kind: "error", content: "用法：/plan <task>"})
			return model, model.flushTranscript()
		}
		model.entries = append(model.entries, fullscreenEntry{kind: "user", content: command})
		model.details = newTranscriptDetailStore(0, 0)
		model.running = true
		model.runStartedAt = time.Now()
		model.workingFrame = 0
		model.status = "planning"
		model.draft = ""
		return model, tea.Sequence(model.flushTranscript(), tea.Batch(model.runTask(command), fullscreenWorkingTick()))
	case "/exit":
		return model, tea.Quit
	case "/clear":
		model.entries = nil
		model.committed = 0
		model.details = newTranscriptDetailStore(0, 0)
		model.draft = ""
		return model, tea.Sequence(func() tea.Msg { return tea.ClearScreen() }, tea.Println(model.banner()))
	case "/help":
		model.entries = append(model.entries, fullscreenEntry{kind: "notice", content: fullscreenSlashHelp()})
		return model, model.flushTranscript()
	case "/resume":
		if model.app.options.Sessions == nil || model.app.options.Resume == nil {
			model.entries = append(model.entries, fullscreenEntry{kind: "error", content: "Session 选择器不可用。"})
			return model, model.flushTranscript()
		}
		model.status = "loading sessions"
		return model, model.loadSessions()
	default:
		if model.app.options.Command == nil {
			model.entries = append(model.entries, fullscreenEntry{kind: "error", content: fmt.Sprintf("未知命令 %q", name)})
			return model, model.flushTranscript()
		}
		model.status = "running command"
		return model, func() tea.Msg {
			output, err := model.app.options.Command(model.ctx, command)
			return fullscreenCommandDoneMsg{command: command, output: output, err: err}
		}
	}
}

func taskPhase(task string) string {
	if isPlanTask(strings.TrimSpace(task)) {
		return "planning"
	}
	return "executing"
}

func (model fullscreenModel) loadSessions() tea.Cmd {
	return func() tea.Msg {
		sessions, err := model.app.options.Sessions(model.ctx)
		return fullscreenSessionsMsg{sessions: sessions, err: err}
	}
}

func (model fullscreenModel) runTask(task string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel, err := model.app.options.NewTask(model.ctx)
		if err != nil {
			return fullscreenTaskDoneMsg{err: err}
		}
		model.app.setActiveRun(cancel)
		defer func() {
			cancel()
			model.app.clearActiveRun(cancel)
		}()
		taskErr := model.app.options.Task(ctx, task)
		session := ""
		if model.app.options.CurrentSession != nil {
			session = model.app.options.CurrentSession()
		}
		return fullscreenTaskDoneMsg{err: taskErr, session: session}
	}
}

func (model *fullscreenModel) refreshCurrentSession() {
	if model != nil && model.app.options.CurrentSession != nil {
		if session := strings.TrimSpace(model.app.options.CurrentSession()); session != "" {
			model.startup.Session = session
		}
	}
}

func (model fullscreenModel) handleApprovalKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		if model.approvalSelected > 0 {
			model.approvalSelected--
		}
		return model, nil
	case "down", "j":
		if model.approvalSelected+1 < len(fullscreenApprovalChoices) {
			model.approvalSelected++
		}
		return model, nil
	case "enter":
		return model.resolveApproval(fullscreenApprovalChoices[model.approvalSelected].decision)
	case "y", "Y":
		return model.resolveApproval(fullscreenApprovalChoices[0].decision)
	case "s", "S":
		return model.resolveApproval(fullscreenApprovalChoices[1].decision)
	case "n", "N", "esc":
		return model.resolveApproval(fullscreenApprovalChoices[2].decision)
	case "ctrl+c":
		model.app.cancelActiveRun()
		decision := fullscreenApprovalChoices[2].decision
		decision.Reason = "user cancelled the run"
		return model.resolveApproval(decision)
	}
	return model, nil
}

func (model fullscreenModel) resolveApproval(decision policy.ApprovalDecision) (tea.Model, tea.Cmd) {
	prompt := model.approval
	model.approval = nil
	model.approvalSelected = 0
	model.status = "executing"
	prompt.response <- fullscreenApprovalResult{decision: decision}
	commands := []tea.Cmd{model.input.Focus()}
	if model.running {
		commands = append(commands, fullscreenWorkingTick())
	}
	return model, tea.Batch(commands...)
}

func (model fullscreenModel) handleSessionKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "ctrl+c":
		model.selecting = false
		model.sessions = nil
		return model, nil
	case "up", "k":
		if model.selected > 0 {
			model.selected--
		}
	case "down", "j":
		if model.selected+1 < len(model.sessions) {
			model.selected++
		}
	case "enter":
		if len(model.sessions) == 0 {
			return model, nil
		}
		selected := model.sessions[model.selected]
		model.status = "resuming session"
		return model, func() tea.Msg {
			message, err := model.app.options.Resume(model.ctx, selected.ID)
			return fullscreenResumeMsg{message: message, err: err}
		}
	}
	return model, nil
}

func (model *fullscreenModel) applyEvent(item event.Event) {
	if item == nil {
		return
	}
	switch item := item.(type) {
	case event.LLMCallStarted:
		if strings.TrimSpace(item.Model.Name) != "" {
			model.model = item.Model.Name
		}
	case event.TextDelta:
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
		var builder strings.Builder
		fmt.Fprintf(&builder, "• Updated Plan · cycle %d", item.Cycle)
		for index, task := range item.Tasks {
			marker := "○"
			if task.Status == "completed" {
				marker = "✔"
			} else if task.Status == "running" {
				marker = "◉"
			} else if task.Status == "failed" || task.Status == "blocked" {
				marker = "✘"
			}
			prefix := "    "
			if index == 0 {
				prefix = "  └ "
			}
			fmt.Fprintf(&builder, "\n%s%s %s · %s", prefix, marker, task.ID, task.Objective)
		}
		model.entries = append(model.entries, fullscreenEntry{kind: "plan", content: builder.String()})
		model.status = "planning"
	case event.IterationStarted:
		if _, exists := model.iterations[item.Iteration]; !exists {
			model.iterations[item.Iteration] = newIterationActivity(item.Iteration)
		}
	case event.ToolCallStarted:
		model.finishDraft()
		iteration := model.iterations[item.Iteration]
		if iteration == nil {
			iteration = newIterationActivity(item.Iteration)
			model.iterations[item.Iteration] = iteration
		}
		model.activitySeq++
		activity := activityFromStarted(item, model.activitySeq)
		iteration.CallIDs = append(iteration.CallIDs, item.CallID)
		iteration.Tools[item.CallID] = activity
		model.activeTools[item.CallID] = activity
		model.status = "executing"
	case event.ToolCallCompleted:
		model.finishDraft()
		activity := model.activeTools[item.CallID]
		if activity == nil {
			iteration := model.iterations[item.Iteration]
			if iteration == nil {
				iteration = newIterationActivity(item.Iteration)
				model.iterations[item.Iteration] = iteration
			}
			model.activitySeq++
			activity = &toolActivity{CallID: item.CallID, Iteration: item.Iteration, Sequence: model.activitySeq, Kind: activityExplore, Title: item.ToolName}
			iteration.CallIDs = append(iteration.CallIDs, item.CallID)
			iteration.Tools[item.CallID] = activity
		}
		activity.Success, activity.Partial, activity.Result, activity.Duration, activity.Completed = item.Success, item.Partial, strings.TrimSpace(item.Summary), item.Duration, true
		if model.details == nil {
			model.details = newTranscriptDetailStore(0, 0)
		}
		detailContent := strings.TrimSpace(strings.Join([]string{activity.Detail, item.Summary}, "\n\n"))
		if detail, ok := model.details.Add(item.CallID, activity.Title, detailContent); ok {
			activity.ResultDetailID = detail.ID
			activity.DetailAvailable = activity.Kind == activityExplore || detail.Truncated || strings.Count(item.Summary, "\n") >= 5 || len([]rune(item.Summary)) > 600
			activity.Result = detail.Content
		}
		delete(model.activeTools, item.CallID)
	case event.IterationCompleted:
		model.finishDraft()
		model.entries = append(model.entries, renderIterationActivities(model.iterations[item.Iteration])...)
		delete(model.iterations, item.Iteration)
	case event.ApprovalRequested:
		model.status = "awaiting approval"
	case event.ApprovalResolved:
		model.finishDraft()
		model.entries = append(model.entries, fullscreenEntry{kind: "notice", content: fmt.Sprintf("Approval · %s · %s", item.ToolName, item.Outcome)})
	case event.RunStatusChanged:
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
		model.entries = append(model.entries, fullscreenEntry{kind: "diagnostic", content: content})
	case event.ErrorOccurred:
		model.finishDraft()
		if strings.TrimSpace(item.Error.Message) != "" {
			model.entries = append(model.entries, fullscreenEntry{kind: "error", content: item.Error.Message})
		}
	case event.RunCompleted:
		model.flushPendingActivities()
		model.status = item.Status
	}
}

func (model *fullscreenModel) flushPendingActivities() {
	if model == nil || len(model.iterations) == 0 {
		return
	}
	iterations := make([]int, 0, len(model.iterations))
	for iteration := range model.iterations {
		iterations = append(iterations, iteration)
	}
	slices.Sort(iterations)
	for _, iteration := range iterations {
		model.entries = append(model.entries, renderIterationActivities(model.iterations[iteration])...)
		delete(model.iterations, iteration)
	}
	clear(model.activeTools)
}

func (model *fullscreenModel) finishDraft() {
	if strings.TrimSpace(model.draft) != "" {
		model.entries = append(model.entries, fullscreenEntry{kind: "assistant", content: model.draft})
	}
	model.draft = ""
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
	draft := model.renderActiveDraft()
	working := model.workingLine()
	activity := ""
	switch {
	case draft != "" && working != "":
		activity = draft + "\n\n" + working
	case draft != "":
		activity = draft
	case working != "":
		activity = "\n" + working
	}
	inputRegion := input + "\n" + status
	if activity == "" {
		return "\n\n" + inputRegion
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
	header := fullscreenSectionStyle.Render("Transcript Details") + "  " + fullscreenMutedStyle.Render("↑/↓ · PgUp/PgDn · Esc return")
	footer := fullscreenMutedStyle.Render(fmt.Sprintf("%d retained item(s) · bounded in memory", len(model.details.items)))
	return strings.Join([]string{header, model.detailViewport.View(), footer}, "\n")
}

func (model fullscreenModel) banner() (rendered string) {
	defer func() {
		if model.app != nil && model.app.options.NoColor {
			rendered = xansi.Strip(rendered)
		}
	}()
	width := maxInt(40, model.width)
	logo := fullscreenLogoStyle.Render(strings.Trim(terminalLogo(width), "\r\n"))
	version := strings.TrimSpace(model.startup.Version)
	if version != "" {
		version = " (" + version + ")"
	}
	title := fullscreenTitleStyle.Render("Amadeus") + fullscreenMutedStyle.Render(version)
	provider := strings.TrimSpace(model.startup.Provider)
	modelName := strings.TrimSpace(model.model)
	project := strings.TrimSpace(model.startup.Project)
	session := strings.TrimSpace(model.startup.Session)
	if session == "" || session == "draft" {
		session = "draft session"
	}
	rows := []string{title}
	rowWidth := width
	if width >= 60 {
		rowWidth = maxInt(20, width-8)
	}
	if modelName != "" {
		rows = append(rows, bannerMetadataRow("model:", modelName, rowWidth))
	}
	if provider != "" {
		rows = append(rows, bannerMetadataRow("provider:", provider, rowWidth))
	}
	if project != "" {
		rows = append(rows, bannerMetadataRow("directory:", project, rowWidth))
	}
	if branch := strings.TrimSpace(model.startup.Branch); branch != "" {
		rows = append(rows, bannerMetadataRow("branch:", branch, rowWidth))
	}
	rows = append(rows, bannerMetadataRow("session:", session, rowWidth))
	if width < 60 {
		return logo + "\n" + strings.Join(rows, "\n")
	}
	panelWidth := maxInt(24, width-6)
	panel := fullscreenPanelStyle.Width(panelWidth).Render(strings.Join(rows, "\n"))
	return logo + "\n\n" + panel
}

func bannerMetadataRow(label, value string, width int) string {
	const labelWidth = 11
	if width <= labelWidth {
		return truncateFullscreen(strings.TrimSpace(label)+strings.TrimSpace(value), width)
	}
	return fmt.Sprintf("%-*s%s", labelWidth, label, truncateFullscreen(strings.TrimSpace(value), width-labelWidth))
}

func newFullscreenMarkdownRenderer(width int) (*glamour.TermRenderer, error) {
	style := styles.DarkStyleConfig
	zero := uint(0)
	cyan := "#67E8F9"
	bold := true
	style.Document.Margin = &zero
	style.Document.Indent = &zero
	style.Document.BlockPrefix = ""
	style.Document.BlockSuffix = ""
	style.Code.Color = &cyan
	style.Code.BackgroundColor = nil
	style.Code.Bold = &bold
	return glamour.NewTermRenderer(glamour.WithStyles(style), glamour.WithWordWrap(width))
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
	activityVerbRE            = regexp.MustCompile(`\b(?:Read|Search)\b`)
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
	if model.approval != nil {
		request := model.approval.request
		lines := []string{
			fullscreenSectionStyle.Render("Approval required") + "  " + fullscreenMutedStyle.Render("↑/↓ select · Enter confirm · Esc deny"),
			"Tool: " + sanitizeInlineEventText(request.ToolName),
			fmt.Sprintf("Risk: %s", request.Risk),
			"Reason: " + sanitizeInlineEventText(request.Reason),
		}
		for index, choice := range fullscreenApprovalChoices {
			prefix := "  "
			if index == model.approvalSelected {
				prefix = "› "
			}
			line := fmt.Sprintf("%s%d. %s", prefix, index+1, choice.label)
			if index == model.approvalSelected {
				line = fullscreenSelectedStyle.Render(line)
			}
			lines = append(lines, line)
		}
		return fullscreenApprovalStyle.Width(maxInt(20, width-6)).Render(strings.Join(lines, "\n"))
	}
	if model.selecting {
		lines := []string{fullscreenResumeAccentStyle.Render("Resume Session") + "  " + fullscreenMutedStyle.Render("↑/↓ select · Enter resume · Esc cancel")}
		for index, session := range model.sessions {
			prefix := "  "
			if index == model.selected {
				prefix = "› "
			}
			current := ""
			if session.Current {
				current = " · current"
			}
			line := truncateFullscreen(fmt.Sprintf("%s%s  %s%s", prefix, session.ID, session.Title, current), width)
			if index == model.selected {
				line = fullscreenResumeAccentStyle.Render(line)
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	}
	input := fullscreenInputFillStyle.Width(width).Render(strings.TrimRight(model.input.View(), "\n"))
	if model.running {
		hint := fmt.Sprintf("Agent 正在执行；Enter 排队下一条任务 · Esc/Ctrl+C 取消 · %d queued", len(model.queuedTasks))
		return input + "\n" + fullscreenMutedStyle.Render(hint)
	}
	return input
}

func (model fullscreenModel) statusBar() string {
	width := maxInt(40, model.width)
	parts := []statusBarPart{}
	if modelName := strings.TrimSpace(model.model); modelName != "" {
		parts = append(parts, statusBarPart{text: modelName, style: fullscreenStatusModelStyle})
	} else {
		parts = append(parts, statusBarPart{text: "AMADEUS", style: fullscreenStatusModelStyle})
	}
	if project := strings.TrimSpace(model.startup.Project); project != "" {
		parts = append(parts, statusBarPart{text: project, style: fullscreenStatusPathStyle})
	}
	if branch := strings.TrimSpace(model.startup.Branch); branch != "" {
		parts = append(parts, statusBarPart{text: branch, style: fullscreenStatusGitStyle})
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
		contextStyle := statusContextStyle(percent)
		parts = append(parts,
			statusBarPart{text: fmt.Sprintf("Context %d%% used", percent), style: contextStyle},
			statusBarPart{text: compactTokenCount(contextWindow) + " window", style: fullscreenStatusMutedStyle},
		)
	}
	if len(parts) == 1 && strings.TrimSpace(model.startup.Project) == "" && strings.TrimSpace(model.startup.Branch) == "" && contextWindow <= 0 {
		parts = append(parts, statusBarPart{text: model.status, style: fullscreenStatusMutedStyle})
	}
	line := renderStatusBarParts(parts)
	if lipgloss.Width(line) > width {
		for len(parts) > 1 && lipgloss.Width(renderStatusBarParts(parts)) > width {
			parts = append(parts[:1], parts[2:]...)
		}
		line = xansi.Truncate(renderStatusBarParts(parts), width, "")
	}
	return line
}

func statusContextStyle(percent int64) lipgloss.Style {
	switch {
	case percent >= 90:
		return fullscreenStatusBadStyle
	case percent >= 70:
		return fullscreenStatusWarnStyle
	default:
		return fullscreenStatusGoodStyle
	}
}

type statusBarPart struct {
	text  string
	style lipgloss.Style
}

func renderStatusBarParts(parts []statusBarPart) string {
	var builder strings.Builder
	separator := fullscreenStatusMutedStyle.Render(" · ")
	for index, part := range parts {
		if index > 0 {
			builder.WriteString(separator)
		}
		builder.WriteString(part.style.Render(part.text))
	}
	return builder.String()
}

func (model fullscreenModel) workingLine() string {
	if !model.running {
		return ""
	}
	elapsed := elapsedRunDuration(model.runStartedAt)
	word := animatedWorkingWord(model.workingFrame)
	return fullscreenWorkingInkStyle.Render(workingMarker(model.workingFrame)+" ") + word + fullscreenWorkingMetaStyle.Render(fmt.Sprintf(" (%s • esc to interrupt)", elapsed))
}

func workingMarker(frame int) string {
	if (frame/8)%2 == 1 {
		return "◦"
	}
	return "•"
}

func animatedWorkingWord(frame int) string {
	const word = "Working"
	var builder strings.Builder
	for index, character := range word {
		builder.WriteString(lipgloss.NewStyle().Foreground(workingBeamColor(workingBeamDistance(frame, index, len(word)))).Render(string(character)))
	}
	return builder.String()
}

func workingBeamDistance(frame, index, wordLength int) int {
	scanFrames := wordLength*workingBeamSubframes + 2*workingBeamPadding
	cycleFrame := frame % (scanFrames + workingBeamPauseFrames)
	if cycleFrame >= scanFrames {
		return scanFrames
	}
	beam := cycleFrame - workingBeamPadding
	return absoluteInt(index*workingBeamSubframes - beam)
}

func elapsedRunDuration(startedAt time.Time) time.Duration {
	if startedAt.IsZero() {
		return 0
	}
	elapsed := time.Since(startedAt).Round(time.Second)
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

func formatRunDuration(duration time.Duration) string {
	seconds := int64(duration.Round(time.Second) / time.Second)
	if seconds < 0 {
		seconds = 0
	}
	hours := seconds / 3600
	minutes := seconds % 3600 / 60
	seconds %= 60
	switch {
	case hours > 0:
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	case minutes > 0:
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

func workingBeamColor(distance int) lipgloss.TerminalColor {
	switch {
	case distance <= 1:
		return lipgloss.AdaptiveColor{Light: "16", Dark: "15"}
	case distance <= 3:
		return lipgloss.AdaptiveColor{Light: "237", Dark: "255"}
	case distance <= 5:
		return lipgloss.AdaptiveColor{Light: "240", Dark: "252"}
	default:
		return lipgloss.AdaptiveColor{Light: "246", Dark: "246"}
	}
}

func absoluteInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
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
	entries := append([]fullscreenEntry(nil), model.entries...)
	if model.draft != "" {
		entries = append(entries, fullscreenEntry{kind: "assistant", content: model.draft})
	}
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, model.renderEntry(entry))
	}
	return strings.Join(parts, "\n\n")
}

func (model *fullscreenModel) flushTranscript() tea.Cmd {
	if model == nil || model.committed >= len(model.entries) {
		return nil
	}
	pending := model.entries[model.committed:]
	previousKind := ""
	if model.committed > 0 {
		previousKind = model.entries[model.committed-1].kind
	}
	output := model.renderCommittedEntries(pending, previousKind)
	model.committed = len(model.entries)
	return tea.Println(output)
}

func (model fullscreenModel) renderCommittedEntries(entries []fullscreenEntry, previousKind string) string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, model.renderEntry(entry))
	}
	output := strings.Join(parts, "\n\n")
	if len(entries) == 0 {
		return output
	}
	if previousKind == "worked" && entries[0].kind == "user" {
		output = "\n" + output
	}
	switch entries[len(entries)-1].kind {
	case "assistant", "activity", "separator":
		output += "\n"
	}
	return output
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
	rendered := model.renderEntry(fullscreenEntry{kind: "assistant", content: strings.Join(sourceLines, "\n")})
	lines := strings.Split(rendered, "\n")
	if len(lines) > available {
		lines = lines[len(lines)-available:]
	}
	return strings.Join(lines, "\n")
}

func (model fullscreenModel) renderEntry(entry fullscreenEntry) (rendered string) {
	defer func() {
		if model.app != nil && model.app.options.NoColor {
			rendered = xansi.Strip(rendered)
		}
	}()
	width := maxInt(36, model.width-3)
	content := sanitizeFullscreenContent(entry.content)
	switch entry.kind {
	case "user":
		return fullscreenUserStyle.Width(width).Render("› " + content)
	case "error":
		return fullscreenErrorStyle.Render("Error: " + content)
	case "tool":
		return fullscreenToolStyle.Width(width).Render(content)
	case "result", "diagnostic":
		return fullscreenResultStyle.Width(width).Render(content)
	case "plan":
		return fullscreenPlanStyle.UnsetBorderStyle().UnsetPadding().Width(width).Render(content)
	case "activity":
		return renderActivityContent(content, width, entry.successful)
	case "separator":
		return fullscreenMutedStyle.Render(strings.Repeat("─", maxInt(12, width)))
	case "worked":
		return renderWorkedLine(content, width)
	case "notice":
		return fullscreenMutedStyle.Render(content)
	default:
		if model.renderer != nil {
			if rendered, err := model.renderer.Render(content); err == nil {
				return fullscreenAssistantStyle.Render(prefixRenderedBlock(rendered, "• "))
			}
		}
		return fullscreenAssistantStyle.Render("• " + content)
	}
}

func renderActivityContent(content string, width int, successful bool) string {
	wrapped := wrapActivityContent(content, width)
	lines := strings.Split(wrapped, "\n")
	for index, line := range lines {
		if index == 0 && successful && strings.HasPrefix(line, "•") {
			lines[index] = fullscreenActivityOKStyle.Render("•") + highlightActivityVerbs(line[len("•"):])
			continue
		}
		lines[index] = highlightActivityVerbs(line)
	}
	return strings.Join(lines, "\n")
}

func highlightActivityVerbs(line string) string {
	matches := activityVerbRE.FindAllStringIndex(line, -1)
	if len(matches) == 0 {
		return fullscreenAssistantStyle.Render(line)
	}
	var builder strings.Builder
	previous := 0
	for _, match := range matches {
		builder.WriteString(fullscreenAssistantStyle.Render(line[previous:match[0]]))
		builder.WriteString(fullscreenActivityVerbStyle.Render(line[match[0]:match[1]]))
		previous = match[1]
	}
	builder.WriteString(fullscreenAssistantStyle.Render(line[previous:]))
	return builder.String()
}

func renderWorkedLine(duration string, width int) string {
	prefix := "─Worked for " + strings.TrimSpace(duration)
	line := prefix + strings.Repeat("─", maxInt(1, width-lipgloss.Width(prefix)))
	return fullscreenWorkingMetaStyle.Render(xansi.Truncate(line, width, ""))
}

func wrapActivityContent(content string, width int) string {
	if width <= 0 {
		return content
	}
	var result []string
	for _, line := range strings.Split(content, "\n") {
		prefix, continuation, body := activityLineParts(line)
		prefixWidth := maxInt(lipgloss.Width(prefix), lipgloss.Width(continuation))
		available := maxInt(1, width-prefixWidth)
		wrapped := strings.Split(xansi.Hardwrap(body, available, true), "\n")
		for index, part := range wrapped {
			linePrefix := prefix
			if index > 0 {
				linePrefix = continuation
			}
			result = append(result, linePrefix+part)
		}
	}
	return strings.Join(result, "\n")
}

func activityLineParts(line string) (prefix, continuation, body string) {
	for _, candidate := range []struct {
		prefix       string
		continuation string
	}{
		{prefix: "• ", continuation: "  │ "},
		{prefix: "  │ ", continuation: "  │ "},
		{prefix: "  └ ", continuation: "    "},
		{prefix: "    └ ", continuation: "      "},
		{prefix: "    ", continuation: "    "},
	} {
		if strings.HasPrefix(line, candidate.prefix) {
			return candidate.prefix, candidate.continuation, strings.TrimPrefix(line, candidate.prefix)
		}
	}
	return "", "", line
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

func fullscreenSlashHelp() string {
	return strings.TrimSpace(`Amadeus commands:

- /resume   切换当前项目的 Session
- /plan     对指定任务启用 Plan → ReAct → Replan
- /status   显示项目和 Session 状态
- /tools    显示当前核心工具
- /clear    清空当前 TUI transcript
- /help     显示帮助
- /exit     退出 Amadeus`)
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

var _ event.Sink = (*FullscreenApplication)(nil)
var _ policy.ApprovalHandler = (*FullscreenApplication)(nil)
