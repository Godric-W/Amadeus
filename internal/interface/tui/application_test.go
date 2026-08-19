package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/rollout"
	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
)

type fakeFullscreenApplication struct {
	events       chan application.InteractiveEvent
	submitted    []string
	compactCount int
	modes        []turn.ModeKind
	interrupts   int
	approvals    []string
	resumed      []rollout.ThreadID
	renamed      []string
	deleted      []uint64
	clears       int
	mcpRequests  []uint64
	loadSkills   int
	skillPaths   []string
	shutdowns    int
	status       application.StatusSnapshot
	submitErr    error
	compactErr   error
	setModeErr   error
	interruptErr error
	approvalErr  error
}

func newFakeFullscreenApplication() *fakeFullscreenApplication {
	return &fakeFullscreenApplication{events: make(chan application.InteractiveEvent, 32)}
}

func (fake *fakeFullscreenApplication) Events() <-chan application.InteractiveEvent {
	return fake.events
}
func (fake *fakeFullscreenApplication) SubmitUser(_ context.Context, content string) error {
	fake.submitted = append(fake.submitted, content)
	return fake.submitErr
}
func (fake *fakeFullscreenApplication) SubmitCompact(context.Context) error {
	fake.compactCount++
	return fake.compactErr
}
func (fake *fakeFullscreenApplication) SetMode(_ context.Context, mode turn.ModeKind) error {
	fake.modes = append(fake.modes, mode)
	return fake.setModeErr
}
func (fake *fakeFullscreenApplication) Interrupt(context.Context) error {
	fake.interrupts++
	return fake.interruptErr
}
func (fake *fakeFullscreenApplication) ResolveApproval(_ context.Context, requestID string, _ policy.ApprovalDecision) error {
	fake.approvals = append(fake.approvals, requestID)
	return fake.approvalErr
}
func (fake *fakeFullscreenApplication) LoadSessions(context.Context) {}
func (fake *fakeFullscreenApplication) Resume(_ context.Context, id rollout.ThreadID) {
	fake.resumed = append(fake.resumed, id)
}
func (fake *fakeFullscreenApplication) Clear(context.Context) { fake.clears++ }
func (fake *fakeFullscreenApplication) Rename(_ context.Context, _ uint64, name string) {
	fake.renamed = append(fake.renamed, name)
}
func (fake *fakeFullscreenApplication) Delete(_ context.Context, generation uint64) {
	fake.deleted = append(fake.deleted, generation)
}
func (fake *fakeFullscreenApplication) Status() application.StatusSnapshot { return fake.status }
func (fake *fakeFullscreenApplication) LoadMCP(_ context.Context, requestID uint64, _ application.MCPDetail) {
	fake.mcpRequests = append(fake.mcpRequests, requestID)
}
func (fake *fakeFullscreenApplication) LoadSkills() { fake.loadSkills++ }
func (fake *fakeFullscreenApplication) SetSkillEnabled(path string, _ bool) {
	fake.skillPaths = append(fake.skillPaths, path)
}
func (fake *fakeFullscreenApplication) Shutdown(context.Context) {
	fake.shutdowns++
	fake.events <- application.ShutdownFinished{}
}

func newTestFullscreen(t *testing.T, configure func(*FullscreenOptions)) (*FullscreenApplication, fullscreenModel) {
	t.Helper()
	fake := newFakeFullscreenApplication()
	options := FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{}, Width: 100, DisableAnimations: true,
		Application: fake,
		Snapshot:    application.ThreadViewSnapshot{Generation: 1, ThreadID: "thread-1", Mode: turn.ModeKindDefault, Model: "test-model", ContextWindow: 128000},
	}
	if configure != nil {
		configure(&options)
	}
	app, err := NewFullscreenApplication(options)
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	model.palette = terminalPalette{Level: colorLevelNone, NoColor: true, Dark: true}
	return app, model
}

func fakeApplication(t *testing.T, model fullscreenModel) *fakeFullscreenApplication {
	t.Helper()
	fake, ok := model.app.options.Application.(*fakeFullscreenApplication)
	if !ok {
		t.Fatalf("application = %T", model.app.options.Application)
	}
	return fake
}

func cellContent(cell HistoryCell) string {
	if cell == nil {
		return ""
	}
	return strings.Join(cell.RawLines(), "\n")
}

func lastCellContent(model fullscreenModel) string {
	if len(model.historyCells) == 0 {
		return ""
	}
	return cellContent(model.historyCells[len(model.historyCells)-1])
}

func executeCommand(t *testing.T, command tea.Cmd) tea.Msg {
	t.Helper()
	if command == nil {
		t.Fatal("command is nil")
	}
	message := command()
	executeMessage(t, message)
	return message
}

func executeMessage(t *testing.T, message tea.Msg) {
	t.Helper()
	switch message := message.(type) {
	case tea.BatchMsg:
		for _, command := range message {
			if command != nil {
				executeMessage(t, command())
			}
		}
	}
}

func TestFullscreenTextareaPreservesChineseAndBackspace(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("请检查中文")})
	updated, _ = updated.(fullscreenModel).Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := updated.(fullscreenModel).input.Value(); got != "请检查中" {
		t.Fatalf("Chinese input = %q", got)
	}
}

func TestFullscreenBannerUsesRestrainedMetadata(t *testing.T) {
	_, model := newTestFullscreen(t, func(options *FullscreenOptions) {
		options.Startup = FullscreenStartup{Version: "dev", Provider: "openai", Model: "gpt", Project: "/tmp/project", Branch: "main", Session: "session"}
		options.Snapshot.Model = "gpt"
	})
	banner := xansi.Strip(model.banner())
	for _, expected := range []string{"Amadeus", "model:", "gpt", "directory:", "/tmp/project"} {
		if !strings.Contains(banner, expected) {
			t.Fatalf("banner omitted %q: %q", expected, banner)
		}
	}
	for _, excluded := range []string{"openai", "session", "main"} {
		if strings.Contains(banner, excluded) {
			t.Fatalf("banner leaked %q: %q", excluded, banner)
		}
	}
}

func TestFullscreenContextStatusUsesRuntimeUsage(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.applyEvent(protocol.SessionEvent{ThreadID: "thread-1", TurnID: "turn-1", Message: protocol.ThreadTokenUsageUpdated{
		Usage: llm.Usage{InputTokens: 13000, OutputTokens: 800}, EstimatedInputTokens: 12000, ContextWindow: 128000,
	}})
	if model.contextUsage != 13000 || model.inputUsage != 13000 || model.outputUsage != 800 {
		t.Fatalf("usage = context %d input %d output %d", model.contextUsage, model.inputUsage, model.outputUsage)
	}
}

func TestFullscreenSubmitsInputDirectlyWhileRunning(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.running = true
	model.input.SetValue("继续检查")
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(fullscreenModel)
	executeCommand(t, command)
	fake := fakeApplication(t, model)
	if len(fake.submitted) != 1 || fake.submitted[0] != "继续检查" || lastCellContent(model) != "继续检查" {
		t.Fatalf("submitted=%v last=%q", fake.submitted, lastCellContent(model))
	}
}

func TestFullscreenPlanTaskWaitsForSettingsAcknowledgement(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	updated, command := model.dispatchCommand(SlashInvocation{Command: SlashPlan, Args: "inspect the repository"})
	model = updated.(fullscreenModel)
	executeCommand(t, command)
	fake := fakeApplication(t, model)
	if len(fake.modes) != 1 || len(fake.submitted) != 0 || model.running {
		t.Fatalf("before acknowledgement modes=%v submitted=%v running=%v", fake.modes, fake.submitted, model.running)
	}
	updated, command = model.Update(fullscreenAppEventMsg{event: application.SessionEventObserved{Generation: 1, Event: protocol.SessionEvent{
		ThreadID: "thread-1", Message: protocol.ThreadSettingsUpdated{Mode: string(turn.ModeKindPlan)},
	}}})
	model = updated.(fullscreenModel)
	executeCommand(t, command)
	if model.collaboration != CollaborationPlan || len(fake.submitted) != 1 || fake.submitted[0] != "inspect the repository" {
		t.Fatalf("after acknowledgement mode=%q submitted=%v", model.collaboration, fake.submitted)
	}
}

func TestFullscreenPlanModeFailureDoesNotChangeProjection(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	fake := fakeApplication(t, model)
	fake.setModeErr = errors.New("session is unavailable")
	updated, command := model.dispatchCommand(SlashInvocation{Command: SlashPlan})
	message := executeCommand(t, command)
	updated, _ = updated.(fullscreenModel).Update(message)
	model = updated.(fullscreenModel)
	if model.collaboration != CollaborationExecute || !strings.Contains(lastCellContent(model), "session is unavailable") {
		t.Fatalf("mode=%q last=%q", model.collaboration, lastCellContent(model))
	}
}

func TestFullscreenResumeReplaysSnapshotAndRejectsStaleEvents(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.insertHistoryCell(NewNoticeHistoryCell("old transcript"))
	now := time.Now().UTC()
	snapshot := application.ThreadViewSnapshot{Generation: 2, ThreadID: "thread-2", Mode: turn.ModeKindPlan, Model: "next", ContextWindow: 64000, Items: []protocol.TurnItem{
		{ID: "user", Kind: protocol.ItemUserMessage, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: "hello"},
		{ID: "assistant", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: "world"},
	}}
	updated, _ := model.Update(fullscreenAppEventMsg{event: application.ThreadAttached{Snapshot: snapshot}})
	model = updated.(fullscreenModel)
	if model.generation != 2 || model.startup.Session != "thread-2" || len(model.historyCells) != 2 || cellContent(model.historyCells[0]) != "hello" || cellContent(model.historyCells[1]) != "world" {
		t.Fatalf("snapshot not restored: generation=%d session=%s cells=%v", model.generation, model.startup.Session, model.historyCells)
	}
	updated, _ = model.Update(fullscreenAppEventMsg{event: application.SessionEventObserved{Generation: 1, Event: protocol.SessionEvent{ThreadID: "thread-1", Message: protocol.Warning{Message: "stale"}}}})
	model = updated.(fullscreenModel)
	if strings.Contains(lastCellContent(model), "stale") {
		t.Fatal("stale event changed active transcript")
	}
}

func TestFullscreenResumeRestoresComposerFocus(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.input.Blur()
	if model.input.Focused() {
		t.Fatal("test setup left composer focused")
	}
	updated, _ := model.Update(fullscreenAppEventMsg{event: application.ThreadAttached{Snapshot: application.ThreadViewSnapshot{
		Generation: 2, ThreadID: "thread-2", Title: "resumed", Mode: turn.ModeKindDefault,
	}}})
	model = updated.(fullscreenModel)
	if !model.input.Focused() {
		t.Fatal("resume did not restore composer focus")
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if got := updated.(fullscreenModel).input.Value(); got != "x" {
		t.Fatalf("focused composer ignored input: %q", got)
	}
}

func TestFullscreenResumeFlushesCompletedToolBeforeFinalAssistant(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	started := toolStartedMessage("read-resume", "read", "read", "Read docs/design.md", "")
	toolItem := toolCompletedMessage(started, protocol.ItemStatusCompleted, "content", "1ms", false).Item
	now := time.Now().UTC()
	assistant := protocol.TurnItem{
		ID: "assistant-resume", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemStatusCompleted,
		CreatedAt: now, CompletedAt: now, Text: "final answer",
	}
	updated, _ := model.Update(fullscreenAppEventMsg{event: application.ThreadAttached{Snapshot: application.ThreadViewSnapshot{
		Generation: 2, ThreadID: "thread-2", Title: "resumed", Mode: turn.ModeKindDefault,
		Items: []protocol.TurnItem{toolItem, assistant},
	}}})
	model = updated.(fullscreenModel)
	if len(model.historyCells) != 2 {
		t.Fatalf("replayed cells = %d: %#v", len(model.historyCells), model.historyCells)
	}
	if _, ok := model.historyCells[0].(*ToolHistoryCell); !ok {
		t.Fatalf("first replayed cell = %T, want tool activity", model.historyCells[0])
	}
	if _, ok := model.historyCells[1].(AgentMessageCell); !ok {
		t.Fatalf("second replayed cell = %T, want final assistant", model.historyCells[1])
	}
}

func TestFullscreenResumeFailureRestoresComposerFocus(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.input.Blur()
	model.selection = &selectionOverlay{Title: "Resume"}
	model.selectionKind = "resume"
	updated, _ := model.Update(fullscreenAppEventMsg{event: application.ThreadAttachFailed{Error: errors.New("not found")}})
	model = updated.(fullscreenModel)
	if !model.input.Focused() || model.selection != nil {
		t.Fatalf("resume failure focus=%v selection=%v", model.input.Focused(), model.selection != nil)
	}
}

func TestFullscreenCompactHasPendingAndCompletedStates(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	updated, command := model.dispatchCommand(SlashInvocation{Command: SlashCompact})
	model = updated.(fullscreenModel)
	if !model.running || model.status != "compacting context" {
		t.Fatalf("pending compact state running=%v status=%q", model.running, model.status)
	}
	executeCommand(t, command)
	updated, _ = model.Update(fullscreenAppEventMsg{event: application.SessionEventObserved{Generation: 1, Event: protocol.SessionEvent{ThreadID: "thread-1", Message: protocol.ContextCompacted{ItemID: "compact-1"}}}})
	model = updated.(fullscreenModel)
	if lastCellContent(model) != "Context compacted" {
		t.Fatalf("compact result = %q", lastCellContent(model))
	}
	updated, _ = model.Update(fullscreenAppEventMsg{event: application.SessionEventObserved{Generation: 1, Event: protocol.SessionEvent{ThreadID: "thread-1", Message: protocol.Warning{Message: "Heads up: Long threads and multiple compactions can cause the model to be less accurate. Start a new thread when possible to keep threads small and targeted."}}}})
	model = updated.(fullscreenModel)
	if lastCellContent(model) != "⚠ Heads up: Long threads and multiple compactions can cause the model to be less accurate. Start a new thread when possible to keep threads small and targeted." {
		t.Fatalf("compact transcript = %q", renderHistoryCells(model.historyCells, HistoryRenderRaw, noColorRenderContext()))
	}
	if rendered := model.inputBox(); strings.Contains(rendered, "/ for commands") {
		t.Fatalf("compact composer retained removed slash hint: %q", rendered)
	}
}

func TestFullscreenEmptyMCPInventoryIsVisibleAndStaleResultIgnored(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	updated, command := model.dispatchCommand(SlashInvocation{Command: SlashMCP})
	model = updated.(fullscreenModel)
	if model.status != "loading MCP inventory" || len(model.historyCells) != 1 {
		t.Fatalf("MCP pending status=%q history=%#v", model.status, model.historyCells)
	}
	if _, ok := model.historyCells[0].(MCPCommandHistoryCell); !ok {
		t.Fatalf("first MCP cell = %T", model.historyCells[0])
	}
	executeCommand(t, command)
	updated, _ = model.Update(fullscreenAppEventMsg{event: application.MCPInventoryLoaded{RequestID: 1, Generation: 1, ThreadID: "thread-1"}})
	model = updated.(fullscreenModel)
	if got, want := renderHistoryCells(model.historyCells, HistoryRenderRaw, noColorRenderContext()), "/mcp\n\n🔌  MCP Tools\n\n  • No MCP servers configured."; got != want {
		t.Fatalf("MCP output\n got: %q\nwant: %q", got, want)
	}
	before := len(model.historyCells)
	updated, _ = model.Update(fullscreenAppEventMsg{event: application.MCPInventoryLoaded{RequestID: 0, Generation: 1, ThreadID: "thread-1", Error: errors.New("stale")}})
	if len(updated.(fullscreenModel).historyCells) != before {
		t.Fatal("stale MCP result changed history")
	}
}

func TestFullscreenStatusUsesStructuredCell(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	fake := fakeApplication(t, model)
	fake.status = application.StatusSnapshot{ThreadID: "thread-1", Title: "demo", Provider: "openai", Model: "gpt", Phase: "idle"}
	updated, _ := model.dispatchCommand(SlashInvocation{Command: SlashStatus})
	if _, ok := updated.(fullscreenModel).historyCells[0].(StatusHistoryCell); !ok {
		t.Fatalf("status cell = %T", updated.(fullscreenModel).historyCells[0])
	}
}

func TestFullscreenCopyRemainsTUILocal(t *testing.T) {
	copied := ""
	_, model := newTestFullscreen(t, func(options *FullscreenOptions) {
		options.ClipboardWrite = func(value string) error {
			copied = value
			return nil
		}
	})
	model.transcript.LastAgentMarkdown = "**raw**"
	updated, command := model.dispatchCommand(SlashInvocation{Command: SlashCopy})
	model = updated.(fullscreenModel)
	if command == nil || copied != "**raw**" || len(fakeApplication(t, model).submitted) != 0 {
		t.Fatalf("command=%v copied=%q submitted=%v", command != nil, copied, fakeApplication(t, model).submitted)
	}
}

func TestFullscreenSkillsToggleUsesStablePath(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.pendingSkillsView = "manage"
	updated, _ := model.Update(fullscreenAppEventMsg{event: application.SkillsLoaded{Generation: 1, Skills: []application.SkillOption{
		{Name: "review", Path: "/user/review/SKILL.md", Enabled: true},
		{Name: "review", Path: "/project/review/SKILL.md", Enabled: false},
	}}})
	model = updated.(fullscreenModel)
	model.selection.Selected = 1
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(fullscreenModel)
	executeCommand(t, command)
	paths := fakeApplication(t, model).skillPaths
	if len(paths) != 1 || paths[0] != "/project/review/SKILL.md" {
		t.Fatalf("skill paths = %v", paths)
	}
}

func TestFullscreenStaleRenameAndDeleteFailureStayUsable(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.sessionTitle = "current"
	updated, _ := model.Update(fullscreenAppEventMsg{event: application.ThreadNameUpdated{Generation: 0, ThreadID: "old", Name: "stale"}})
	model = updated.(fullscreenModel)
	if model.sessionTitle != "current" {
		t.Fatalf("stale rename changed title to %q", model.sessionTitle)
	}
	updated, command := model.Update(fullscreenAppEventMsg{event: application.ThreadDeleteFailed{Error: errors.New("store busy")}})
	model = updated.(fullscreenModel)
	if command == nil || model.status != "idle" || !strings.Contains(lastCellContent(model), "store busy") {
		t.Fatalf("delete failure status=%q last=%q", model.status, lastCellContent(model))
	}
}

func TestFullscreenClearDropsOldTranscript(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.insertHistoryCell(NewNoticeHistoryCell("old"))
	updated, _ := model.Update(fullscreenAppEventMsg{event: application.ClearUIStarted{}})
	model = updated.(fullscreenModel)
	if len(model.historyCells) != 0 || !model.clearing {
		t.Fatalf("clear state cells=%d clearing=%v", len(model.historyCells), model.clearing)
	}
}

func TestFullscreenApprovalUsesTypedApplicationPort(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	request := policy.ApprovalRequest{ID: "approval-1", ToolName: "execute_command", Command: "go test ./..."}
	updated, _ := model.Update(fullscreenAppEventMsg{event: application.ApprovalRequested{Generation: 1, RequestID: "request-1", Request: request}})
	model = updated.(fullscreenModel)
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(fullscreenModel)
	executeCommand(t, command)
	if got := fakeApplication(t, model).approvals; len(got) != 1 || got[0] != "request-1" {
		t.Fatalf("approvals = %v", got)
	}
}

func TestFullscreenExitWaitsForShutdownFinished(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	updated, command := model.dispatchCommand(SlashInvocation{Command: SlashExit})
	model = updated.(fullscreenModel)
	executeCommand(t, command)
	if fakeApplication(t, model).shutdowns != 1 || !model.shutdownRequested {
		t.Fatal("shutdown was not requested")
	}
	updated, quit := model.Update(fullscreenAppEventMsg{event: application.ShutdownFinished{}})
	if updated.(fullscreenModel).status != "shutting down" || quit == nil {
		t.Fatal("shutdown completion did not quit")
	}
}

func TestFullscreenInvalidSlashInputDoesNotEnterHistory(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.input.SetValue("/unknown")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(fullscreenModel)
	if len(model.history) != 0 || !strings.Contains(lastCellContent(model), "unknown command") {
		t.Fatalf("history=%v last=%q", model.history, lastCellContent(model))
	}
}

func TestFullscreenFlushCommitsCellsOnce(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.insertHistoryCell(NewUserMessageCell("检查项目"))
	command := model.flushHistory()
	if command == nil || len(model.pendingHistoryCells) != 0 || !model.hasEmittedHistoryLines {
		t.Fatal("first flush failed")
	}
	if output := command(); output == nil || !strings.Contains(strings.TrimSpace(fmt.Sprint(output)), "检查项目") {
		t.Fatalf("flush output = %#v", output)
	}
	if model.flushHistory() != nil {
		t.Fatal("second flush duplicated output")
	}
}
