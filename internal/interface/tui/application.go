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
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

type FullscreenStartup struct {
	Version    string
	Provider   string
	Model      string
	Project    string
	Session    string
	MaxContext int64
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
	kind    string
	content string
}

type fullscreenModel struct {
	app            *FullscreenApplication
	ctx            context.Context
	startup        FullscreenStartup
	input          textarea.Model
	renderer       *glamour.TermRenderer
	lastMouseEvent time.Time
	width          int
	height         int
	entries        []fullscreenEntry
	committed      int
	draft          string
	running        bool
	status         string
	model          string
	inputUsage     int64
	outputUsage    int64
	history        []string
	historyPos     int
	queuedTasks    []string
	approval       *fullscreenApproval
	sessions       []SessionOption
	selecting      bool
	selected       int
}

type fullscreenApproval struct {
	request  policy.ApprovalRequest
	response chan fullscreenApprovalResult
}

type fullscreenApprovalResult struct {
	decision policy.ApprovalDecision
	err      error
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

const (
	fullscreenInputPrompt      = "› "
	fullscreenInputPlaceholder = "输入任务，或输入 /help 查看命令"
	fullscreenInputCharLimit   = 20000
	fullscreenMaxInputRows     = 5
)

var (
	fullscreenLogoStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	fullscreenTitleStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	fullscreenSectionStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	fullscreenMutedStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	fullscreenPanelStyle       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")).Padding(0, 1)
	fullscreenUserStyle        = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("252")).Padding(0, 1)
	fullscreenAssistantStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	fullscreenErrorStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	fullscreenInputFillStyle   = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	fullscreenInputPromptStyle = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("42")).Bold(true)
	fullscreenInputTextStyle   = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("252"))
	fullscreenPlaceholderStyle = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("244"))
	fullscreenStatusStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	fullscreenPhaseStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	fullscreenToolStyle        = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(lipgloss.Color("39")).PaddingLeft(1)
	fullscreenResultStyle      = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(lipgloss.Color("244")).Foreground(lipgloss.Color("245")).PaddingLeft(1)
	fullscreenPlanStyle        = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(lipgloss.Color("220")).PaddingLeft(1)
	fullscreenApprovalStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("220")).Padding(0, 1)
	fullscreenSelectedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
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
	renderer, _ := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(96))
	startup := app.options.Startup
	entries := []fullscreenEntry{{kind: "assistant", content: "你好，我是 Amadeus。告诉我你希望在当前项目中完成什么。"}}
	model := fullscreenModel{
		app: app, ctx: ctx, startup: startup, input: input, renderer: renderer,
		width: 100, height: 30, status: "idle", model: startup.Model, historyPos: -1,
		entries: entries, committed: len(entries),
	}
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

func (model fullscreenModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.width = maxInt(40, message.Width)
		model.height = maxInt(10, message.Height)
		model.updateInputLayout()
		model.renderer, _ = glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(maxInt(40, model.width-4)))
		return model, nil
	case fullscreenEventMsg:
		model.applyEvent(message.item)
		return model, model.flushTranscript()
	case fullscreenApprovalMsg:
		model.approval = message.prompt
		model.status = "awaiting approval"
		return model, nil
	case fullscreenTaskDoneMsg:
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
		}
		if len(model.queuedTasks) > 0 {
			next := model.queuedTasks[0]
			model.queuedTasks = model.queuedTasks[1:]
			model.running = true
			model.status = taskPhase(next)
			model.draft = ""
			return model, tea.Sequence(model.flushTranscript(), model.runTask(next))
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
	case "esc":
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
		model.running = true
		model.status = taskPhase(text)
		model.draft = ""
		return model, tea.Sequence(model.flushTranscript(), model.runTask(text))
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
		model.running = true
		model.status = "planning"
		model.draft = ""
		return model, tea.Sequence(model.flushTranscript(), model.runTask(command))
	case "/exit":
		return model, tea.Quit
	case "/clear":
		model.entries = nil
		model.committed = 0
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
	var decision policy.ApprovalDecision
	valid := true
	switch key.String() {
	case "y", "Y":
		decision = policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "user approved once"}
	case "s", "S":
		decision = policy.ApprovalDecision{Outcome: policy.ApprovalAllow, Scope: policy.ApprovalSession, Source: policy.ApprovalSourceUser, Reason: "user approved for the session"}
	case "n", "N", "esc":
		decision = policy.ApprovalDecision{Outcome: policy.ApprovalDeny, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "user denied the request"}
	case "ctrl+c":
		model.app.cancelActiveRun()
		decision = policy.ApprovalDecision{Outcome: policy.ApprovalDeny, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourceUser, Reason: "user cancelled the run"}
	default:
		valid = false
	}
	if !valid {
		return model, nil
	}
	prompt := model.approval
	model.approval = nil
	model.status = "executing"
	prompt.response <- fullscreenApprovalResult{decision: decision}
	return model, nil
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
	case event.TurnStarted:
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
	case event.PlanUpdated:
		model.finishDraft()
		var builder strings.Builder
		fmt.Fprintf(&builder, "Plan · cycle %d", item.Cycle)
		for _, task := range item.Tasks {
			fmt.Fprintf(&builder, "\n- [%s] %s · %s", task.Status, task.ID, task.Objective)
		}
		model.entries = append(model.entries, fullscreenEntry{kind: "plan", content: builder.String()})
		model.status = "planning"
	case event.ToolCallStarted:
		model.finishDraft()
		model.entries = append(model.entries, fullscreenEntry{kind: "tool", content: "Tool · " + item.ToolName})
		model.status = "executing"
	case event.ToolCallCompleted:
		model.finishDraft()
		state := "completed"
		if !item.Success {
			state = "failed"
		}
		partial := ""
		if item.Partial {
			partial = " · partial"
		}
		content := fmt.Sprintf("%s · %s%s · %s", item.ToolName, state, partial, item.Duration.Round(time.Millisecond))
		if strings.TrimSpace(item.Summary) != "" {
			content += "\n" + item.Summary
		}
		model.entries = append(model.entries, fullscreenEntry{kind: "result", content: content})
	case event.ApprovalRequested:
		model.status = "awaiting approval"
	case event.ApprovalResolved:
		model.finishDraft()
		model.entries = append(model.entries, fullscreenEntry{kind: "notice", content: fmt.Sprintf("Approval · %s · %s", item.ToolName, item.Outcome)})
	case event.EngineStatusChanged:
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
	case event.EngineRunCompleted:
		model.status = item.Status
	}
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

func (model fullscreenModel) View() string {
	input := model.inputBox()
	status := model.statusBar()
	parts := make([]string, 0, 3)
	if draft := model.renderActiveDraft(); draft != "" {
		parts = append(parts, draft)
	}
	parts = append(parts, input, status)
	return strings.Join(parts, "\n")
}

func (model fullscreenModel) banner() string {
	logo := fullscreenLogoStyle.Render(strings.Join([]string{
		"   ███   ",
		"  ██ ██  ",
		" ██   ██ ",
		" ███████ ",
		" ██   ██ ",
	}, "\n"))
	version := strings.TrimSpace(model.startup.Version)
	if version != "" {
		version = " " + fullscreenMutedStyle.Render(version)
	}
	title := fullscreenTitleStyle.Render("Amadeus") + version
	provider := strings.TrimSpace(strings.Join([]string{model.startup.Provider, model.model}, " · "))
	if provider == "·" {
		provider = "provider will load on first task"
	}
	session := strings.TrimSpace(model.startup.Session)
	if session == "" || session == "draft" {
		session = "draft session"
	} else {
		session = "session " + truncateFullscreen(session, 16)
	}
	info := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		fullscreenMutedStyle.Render(provider),
		fullscreenMutedStyle.Render(session),
	)
	panelWidth := maxInt(24, model.width-lipgloss.Width(logo)-lipgloss.Width(info)-8)
	panel := fullscreenPanelStyle.Width(panelWidth).Render(
		fullscreenSectionStyle.Render("Coding Agent") + "\n" +
			"ReAct 默认 · /plan 启用规划\n" +
			"Files · Shell · Skill · MCP · LSP")
	return lipgloss.JoinHorizontal(lipgloss.Top, logo, "  ", info, "  ", panel)
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
	if model.approval != nil {
		request := model.approval.request
		content := fmt.Sprintf("Approval required\nTool: %s\nRisk: %s\nReason: %s\n[y] once  [s] session  [n] deny", sanitizeInlineEventText(request.ToolName), request.Risk, sanitizeInlineEventText(request.Reason))
		return fullscreenApprovalStyle.Width(maxInt(20, width-4)).Render(content)
	}
	if model.selecting {
		lines := []string{fullscreenSectionStyle.Render("Resume Session") + "  " + fullscreenMutedStyle.Render("↑/↓ select · Enter resume · Esc cancel")}
		for index, session := range model.sessions {
			prefix := "  "
			if index == model.selected {
				prefix = "› "
			}
			current := ""
			if session.Current {
				current = " · current"
			}
			line := fmt.Sprintf("%s%s  %s%s", prefix, session.ID, session.Title, current)
			if index == model.selected {
				line = fullscreenSelectedStyle.Render(line)
			}
			lines = append(lines, line)
		}
		return fullscreenPanelStyle.Width(maxInt(20, width-4)).Render(strings.Join(lines, "\n"))
	}
	input := fullscreenInputFillStyle.Width(width).Padding(1, 0).Render(strings.TrimRight(model.input.View(), "\n"))
	if model.running {
		hint := fmt.Sprintf("Agent 正在执行；Enter 排队下一条任务 · Ctrl+C 取消当前任务 · %d queued", len(model.queuedTasks))
		return input + "\n" + fullscreenMutedStyle.Render(hint)
	}
	return input
}

func (model fullscreenModel) statusBar() string {
	width := maxInt(40, model.width)
	left := fullscreenPhaseStyle.Render("AMADEUS") + " " + fullscreenStatusStyle.Render(model.status)
	usage := fmt.Sprintf("tokens %d/%d", model.inputUsage, model.outputUsage)
	projectWidth := maxInt(8, width-lipgloss.Width(left)-lipgloss.Width(usage)-3)
	right := fullscreenMutedStyle.Render(usage + "  " + truncateFullscreen(model.startup.Project, projectWidth))
	gap := maxInt(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	line := left + strings.Repeat(" ", gap) + right
	if lipgloss.Width(line) > width {
		line = xansi.Truncate(line, width, "")
	}
	return line
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
	parts := make([]string, 0, len(model.entries)-model.committed)
	for _, entry := range model.entries[model.committed:] {
		parts = append(parts, model.renderEntry(entry))
	}
	model.committed = len(model.entries)
	return tea.Println(strings.Join(parts, "\n\n"))
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

func (model fullscreenModel) renderEntry(entry fullscreenEntry) string {
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
		return fullscreenPlanStyle.Width(width).Render(content)
	case "notice":
		return fullscreenMutedStyle.Render(content)
	default:
		if model.renderer != nil {
			if rendered, err := model.renderer.Render(content); err == nil {
				content = strings.TrimRight(rendered, "\n")
			}
		}
		return fullscreenAssistantStyle.Render(content)
	}
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
