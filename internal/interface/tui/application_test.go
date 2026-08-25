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
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

type fakeFullscreenApplication struct {
	events          chan application.InteractiveEvent
	submitted       []string
	compactCount    int
	modes           []turn.ModeKind
	interrupts      int
	approvals       []string
	userInputs      []protocol.RequestID
	resumed         []protocol.ThreadID
	renamed         []string
	deleted         []uint64
	clears          int
	mcpRequests     []uint64
	loadSkills      int
	skillPaths      []string
	shutdowns       int
	status          application.StatusSnapshot
	submitErr       error
	submitAdmission protocol.UserMessageAdmission
	compactErr      error
	setModeErr      error
	interruptErr    error
	approvalErr     error
	shutdownErr     error
}

func newFakeFullscreenApplication() *fakeFullscreenApplication {
	return &fakeFullscreenApplication{
		events:          make(chan application.InteractiveEvent, 32),
		submitAdmission: protocol.UserMessageAdmission{Kind: protocol.UserMessageAdmissionStarted, TurnID: "turn-1"},
	}
}

func (fake *fakeFullscreenApplication) Events() <-chan application.InteractiveEvent {
	return fake.events
}
func (fake *fakeFullscreenApplication) SubmitUser(_ context.Context, content, _ string, _ protocol.ThreadSettingsOverrides) (protocol.UserMessageAdmission, error) {
	fake.submitted = append(fake.submitted, content)
	return fake.submitAdmission, fake.submitErr
}
func (fake *fakeFullscreenApplication) ResolveUserInput(_ context.Context, requestID protocol.RequestID, _ protocol.RequestUserInputResponse) error {
	fake.userInputs = append(fake.userInputs, requestID)
	return nil
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
func (fake *fakeFullscreenApplication) Resume(_ context.Context, id protocol.ThreadID) {
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
func (fake *fakeFullscreenApplication) Shutdown(context.Context) error {
	fake.shutdowns++
	return fake.shutdownErr
}

func newTestFullscreen(t *testing.T, configure func(*FullscreenOptions)) (*FullscreenApplication, fullscreenModel) {
	t.Helper()
	fake := newFakeFullscreenApplication()
	options := FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{}, Width: 100, DisableAnimations: true,
		Application: fake,
		Snapshot: application.ThreadViewSnapshot{
			Generation: 1, SessionID: protocol.SessionIDFromThreadID(testThreadID(1)), ThreadID: testThreadID(1),
			TokenInfo:     &protocol.TokenUsageInfo{ModelContextWindow: 128000},
			Configuration: protocol.SessionConfiguration{CWD: "/workspace/amadeus", Model: "test-model", Mode: protocol.ModeKindDefault},
		},
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

func TestFullscreenTextareaSoftWrapsChineseInput(t *testing.T) {
	_, model := newTestFullscreen(t, func(options *FullscreenOptions) {
		options.Width = 40
	})
	model.input.SetValue(strings.Repeat("中文输入", 30))
	model.updateInputLayout()
	if got := model.input.Height(); got <= 1 || got > fullscreenMaxInputRows {
		t.Fatalf("input height = %d, want 2..%d", got, fullscreenMaxInputRows)
	}
	if got := lipgloss.Height(model.inputBox()); got != fullscreenMaxInputRows {
		t.Fatalf("rendered input height = %d, want %d", got, fullscreenMaxInputRows)
	}
}

func TestFullscreenTextareaShowsPromptOnlyOnFirstVisualLine(t *testing.T) {
	_, model := newTestFullscreen(t, func(options *FullscreenOptions) { options.Width = 40 })
	model.input.SetValue(strings.Repeat("中文", 10))
	model.updateInputLayout()
	lines := strings.Split(xansi.Strip(model.inputBox()), "\n")
	if len(lines) < 2 {
		t.Fatalf("test input did not wrap: %q", strings.Join(lines, "\n"))
	}
	if count := strings.Count(strings.Join(lines, "\n"), "›"); count != 1 {
		t.Fatalf("wrapped composer prompt count = %d: %q", count, strings.Join(lines, "\n"))
	}
	if !strings.HasPrefix(lines[0], fullscreenInputPrompt) {
		t.Fatalf("first visual line omitted prompt: %q", lines[0])
	}
	for index, line := range lines[1:] {
		if strings.Contains(line, "›") || !strings.HasPrefix(line, strings.Repeat(" ", lipgloss.Width(fullscreenInputPrompt))) {
			t.Fatalf("continuation line %d has wrong gutter: %q", index+1, line)
		}
	}
}

func TestFullscreenTextareaUsesFullTerminalWidthForWrapping(t *testing.T) {
	_, model := newTestFullscreen(t, func(options *FullscreenOptions) { options.Width = 40 })
	model.input.SetValue(strings.Repeat("x", 37))
	model.updateInputLayout()
	if model.input.Height() != 1 {
		t.Fatalf("37 content columns wrapped before prompt and cursor reached 40 columns: height=%d", model.input.Height())
	}
	model.input.SetValue(strings.Repeat("x", 38))
	model.updateInputLayout()
	if model.input.Height() != 2 {
		t.Fatalf("38 content columns did not wrap after prompt and cursor exceeded 40 columns: height=%d", model.input.Height())
	}
}

func TestFullscreenTextareaKeepsTailVisibleAfterMaxRows(t *testing.T) {
	_, model := newTestFullscreen(t, func(options *FullscreenOptions) { options.Width = 40 })
	prefix := "HEAD" + strings.Repeat("x", 38*6)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(prefix + "TAIL")})
	model = updated.(fullscreenModel)
	if model.input.Height() != fullscreenMaxInputRows {
		t.Fatalf("input height = %d, want max %d", model.input.Height(), fullscreenMaxInputRows)
	}
	if rendered := xansi.Strip(model.inputBox()); !strings.Contains(rendered, "TAIL") {
		t.Fatalf("max-height textarea hid cursor tail: %q", rendered)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyHome})
	model = updated.(fullscreenModel)
	if rendered := xansi.Strip(model.inputBox()); !strings.Contains(rendered, "HEAD") || strings.Contains(rendered, "TAIL") {
		t.Fatalf("max-height textarea did not follow cursor to head: %q", rendered)
	}
}

func TestFullscreenViewUsesIntrinsicFrameHeight(t *testing.T) {
	_, model := newTestFullscreen(t, func(options *FullscreenOptions) {
		options.Width = 40
	})
	model.height = 60
	model.input.SetValue("保持在活动输入框中的提示词")
	model.updateInputLayout()
	view := xansi.Strip(model.View())
	if got := lipgloss.Height(view); got >= model.height {
		t.Fatalf("view height = %d, want intrinsic height below terminal height %d", got, model.height)
	}
	if count := strings.Count(view, "保持在活动输入框中的提示词"); count != 1 {
		t.Fatalf("active composer rendered %d times: %q", count, view)
	}
}

func TestFullscreenShiftTabShowsPlanModeAtBottomRight(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	medium := llm.ReasoningEffortMedium
	model.session.Configuration.ReasoningEffort = &medium
	model.workspace = statusLineWorkspaceState{
		Generation: model.session.Generation,
		CurrentDir: model.session.Configuration.CWD,
		Branch:     "main",
	}
	model.refreshStatusLine()
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	model = updated.(fullscreenModel)
	if model.pendingMode != turn.ModeKindPlan {
		t.Fatalf("pending mode = %q, want Plan", model.pendingMode)
	}
	executeCommand(t, command)
	if got := fakeApplication(t, model).modes; len(got) != 1 || got[0] != turn.ModeKindPlan {
		t.Fatalf("requested modes = %v, want Plan", got)
	}
	configuration := model.session.Configuration.Clone()
	configuration.Mode = protocol.ModeKindPlan
	updated, command = model.Update(fullscreenAppEventMsg{event: application.SessionEventObserved{
		Generation: model.session.Generation,
		Event:      testProtocolEvent(testThreadID(1), "turn-1", protocol.ThreadSettingsAppliedEvent{Configuration: configuration}),
	}})
	model = updated.(fullscreenModel)
	if command == nil {
		t.Fatal("settings acknowledgement did not flush mode change history")
	}
	if model.pendingMode.Valid() {
		t.Fatalf("pending mode was not cleared after acknowledgement: %q", model.pendingMode)
	}
	if len(model.historyCells) != 1 || len(model.pendingHistoryCells) != 0 {
		t.Fatalf("settings acknowledgement history=%d pending=%d", len(model.historyCells), len(model.pendingHistoryCells))
	}
	wantNotice := "• Mode changed to Plan."
	if got := lastCellContent(model); got != wantNotice {
		t.Fatalf("mode switch notice = %q, want %q", got, wantNotice)
	}
	if output := fmt.Sprint(command()); !strings.Contains(output, wantNotice) {
		t.Fatalf("mode switch flush = %q", output)
	}
	view := xansi.Strip(model.View())
	if lipgloss.Height(view) >= model.height {
		t.Fatalf("view height = %d, want intrinsic frame below terminal height %d", lipgloss.Height(view), model.height)
	}
	lines := strings.Split(view, "\n")
	footer := lines[len(lines)-1]
	if !strings.Contains(footer, "main") {
		t.Fatalf("mode switch dropped git branch from footer: %q", footer)
	}
	if !strings.HasSuffix(footer, "Plan mode (shift+tab to cycle)  ") || lipgloss.Width(footer) != model.width {
		t.Fatalf("Plan mode is not right-aligned on the footer row: %q", footer)
	}
}

func TestFullscreenShiftTabCoalescesPendingModeChange(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	updated, first := model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	model = updated.(fullscreenModel)
	updated, second := model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	model = updated.(fullscreenModel)
	if first == nil || second != nil {
		t.Fatalf("mode commands first=%v second=%v, want one pending request", first != nil, second != nil)
	}
	executeCommand(t, first)
	if got := fakeApplication(t, model).modes; len(got) != 1 || got[0] != turn.ModeKindPlan {
		t.Fatalf("requested modes = %v, want one Plan request", got)
	}
}

func TestFullscreenRepeatedModeChangesEmitOnlyCodexInfoRows(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.input.SetValue("保留在输入框中的提示词")

	for index, mode := range []protocol.ModeKind{protocol.ModeKindPlan, protocol.ModeKindDefault, protocol.ModeKindPlan} {
		configuration := model.session.Configuration.Clone()
		configuration.Mode = mode
		updated, command := model.Update(fullscreenAppEventMsg{event: application.SessionEventObserved{
			Generation: model.session.Generation,
			Event:      testProtocolEvent(testThreadID(1), "turn-1", protocol.ThreadSettingsAppliedEvent{Configuration: configuration}),
		}})
		model = updated.(fullscreenModel)
		if command == nil {
			t.Fatalf("mode change %d did not flush info history", index)
		}
		if len(model.historyCells) != index+1 || len(model.pendingHistoryCells) != 0 {
			t.Fatalf("mode change %d history=%d pending=%d", index, len(model.historyCells), len(model.pendingHistoryCells))
		}
		wantNotice := "• Mode changed to " + collaborationModeName(mode) + "."
		if got := lastCellContent(model); got != wantNotice {
			t.Fatalf("mode change %d notice = %q, want %q", index, got, wantNotice)
		}
		if got := model.input.Value(); got != "保留在输入框中的提示词" {
			t.Fatalf("mode change %d changed composer value to %q", index, got)
		}
		if count := strings.Count(xansi.Strip(model.View()), "保留在输入框中的提示词"); count != 1 {
			t.Fatalf("mode change %d rendered composer %d times", index, count)
		}
	}
}

func TestFullscreenFinalReplyPrecedesWorkedForSeparator(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	startedAt := time.Now().Add(-2 * time.Minute)
	model.applyEvent(testProtocolEvent(testThreadID(1), "turn-1", protocol.TurnStartedEvent{StartedAt: startedAt}))
	started := toolStartedMessage("read-1", "read", "read", "Read docs/design.md", "")
	model.applyEvent(testProtocolEvent(testThreadID(1), "turn-1", started))
	model.applyEvent(testProtocolEvent(testThreadID(1), "turn-1", toolCompletedMessage(started, protocol.ItemStatusCompleted, "done", "1s", false)))
	model.applyEvent(testProtocolEvent(testThreadID(1), "turn-1", protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "最终回复", Reset: true}))
	model.applyEvent(testProtocolEvent(testThreadID(1), "turn-1", protocol.TurnCompleteEvent{Status: protocol.TurnStatusCompleted, FinishedAt: time.Now()}))

	if len(model.historyCells) != 4 {
		t.Fatalf("history cell count = %d, want 4", len(model.historyCells))
	}
	if _, ok := model.historyCells[0].(*ToolHistoryCell); !ok {
		t.Fatalf("cell 0 = %T, want *ToolHistoryCell", model.historyCells[0])
	}
	short, ok := model.historyCells[1].(FinalMessageSeparator)
	if !ok || short.Elapsed != 0 {
		t.Fatalf("cell 1 = %#v, want short FinalMessageSeparator", model.historyCells[1])
	}
	if _, ok := model.historyCells[2].(AgentMessageCell); !ok {
		t.Fatalf("cell 2 = %T, want AgentMessageCell", model.historyCells[2])
	}
	worked, ok := model.historyCells[3].(FinalMessageSeparator)
	if !ok || worked.Elapsed <= time.Minute {
		t.Fatalf("cell 3 = %#v, want elapsed FinalMessageSeparator", model.historyCells[3])
	}
}

func TestFullscreenBannerUsesRestrainedMetadata(t *testing.T) {
	_, model := newTestFullscreen(t, func(options *FullscreenOptions) {
		options.Startup = FullscreenStartup{Version: "dev"}
		options.Snapshot.Configuration = protocol.SessionConfiguration{Provider: "openai", Model: "gpt", CWD: "/tmp/project", Mode: protocol.ModeKindDefault}
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
	tokenEvent := protocol.NewTokenCountEvent(llm.TokenUsage{InputTokens: 13000, OutputTokens: 800, TotalTokens: 13800}, 128000, 1)
	tokenEvent.ActiveContextTokens = 13000
	model.applyEvent(testProtocolEvent(testThreadID(1), "turn-1", tokenEvent))
	usage := model.session.totalTokenUsage()
	if model.session.ContextUsed != 13000 || usage.InputTokens != 13000 || usage.OutputTokens != 800 {
		t.Fatalf("usage = context %d input %d output %d", model.session.ContextUsed, usage.InputTokens, usage.OutputTokens)
	}
}

func TestFullscreenSubmitsInputDirectlyWhileRunning(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.running = true
	startedAt := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	model.runStartedAt = startedAt
	model.status = "thinking"
	model.input.SetValue("继续检查")
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(fullscreenModel)
	executeCommand(t, command)
	fake := fakeApplication(t, model)
	if len(fake.submitted) != 1 || fake.submitted[0] != "继续检查" || lastCellContent(model) != "继续检查" {
		t.Fatalf("submitted=%v last=%q", fake.submitted, lastCellContent(model))
	}
	if !model.running || model.runStartedAt != startedAt || model.status != "thinking" {
		t.Fatalf("steer reset turn UI: running=%v started=%v status=%q", model.running, model.runStartedAt, model.status)
	}
}

func TestFullscreenRuntimeUserMessageConfirmsOptimisticProjection(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	submission := model.prepareTaskSubmission("continue", turn.ModeKindDefault, false)
	before := len(model.historyCells)
	event := testProtocolEvent(testThreadID(1), "turn-1", protocol.ItemCompletedEvent{Item: protocol.TurnItem{
		ID: "user-1", Kind: protocol.ItemUserMessage, Status: protocol.ItemStatusCompleted,
		CreatedAt: time.Now().UTC(), CompletedAt: time.Now().UTC(), Text: "continue", ClientUserMessageID: submission.ClientUserMessageID,
	}})
	model.applyEvent(event)
	model.applyEvent(event)
	if len(model.historyCells) != before {
		t.Fatalf("runtime confirmation duplicated optimistic user message: before=%d after=%d", before, len(model.historyCells))
	}
	if _, pending := model.optimisticUserMessages[submission.ClientUserMessageID]; pending {
		t.Fatal("optimistic user message was not confirmed")
	}
}

func TestFullscreenRejectedSteerRestoresComposerWithoutStoppingTurn(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.running = true
	model.status = "working"
	submission := model.prepareTaskSubmission("retry this", turn.ModeKindDefault, false)
	model.handleUserMessageRejection(fullscreenUserMessageRejectedMsg{task: submission, err: errors.New("active compact turn is not steerable")})
	if model.input.Value() != "retry this" || !model.running || model.status != "working" {
		t.Fatalf("rejection state = input %q running %v status %q", model.input.Value(), model.running, model.status)
	}
	if !strings.Contains(lastCellContent(model), "not steerable") {
		t.Fatalf("rejection error = %q", lastCellContent(model))
	}
}

func TestFullscreenPlanTaskSubmitsAtomically(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	updated, command := model.dispatchCommand(SlashInvocation{Command: SlashPlan, Args: "inspect the repository"})
	model = updated.(fullscreenModel)
	executeCommand(t, command)
	fake := fakeApplication(t, model)
	if len(fake.modes) != 0 || len(fake.submitted) != 1 || model.running || model.session.mode() != turn.ModeKindDefault {
		t.Fatalf("atomic plan submission modes=%v submitted=%v running=%v mode=%q", fake.modes, fake.submitted, model.running, model.session.mode())
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
	if model.session.mode() != turn.ModeKindDefault || !strings.Contains(lastCellContent(model), "session is unavailable") {
		t.Fatalf("mode=%q last=%q", model.session.mode(), lastCellContent(model))
	}
}

func TestFullscreenResumeReplaysSnapshotAndRejectsStaleEvents(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.insertHistoryCell(NewNoticeHistoryCell("old transcript"))
	now := time.Now().UTC()
	snapshot := application.ThreadViewSnapshot{Generation: 2, SessionID: protocol.SessionIDFromThreadID(testThreadID(2)), ThreadID: testThreadID(2), Configuration: protocol.SessionConfiguration{CWD: "/workspace/next", Model: "next", Mode: protocol.ModeKindPlan}, TokenInfo: &protocol.TokenUsageInfo{ModelContextWindow: 64000}, Items: []protocol.TurnItem{
		{ID: "user", Kind: protocol.ItemUserMessage, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: "hello"},
		{ID: "assistant", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: "world"},
	}}
	updated, _ := model.Update(fullscreenAppEventMsg{event: application.ThreadAttached{Snapshot: snapshot}})
	model = updated.(fullscreenModel)
	if model.session.Generation != 2 || model.session.ThreadID != testThreadID(2) || len(model.historyCells) != 2 || cellContent(model.historyCells[0]) != "hello" || cellContent(model.historyCells[1]) != "world" {
		t.Fatalf("snapshot not restored: generation=%d session=%s cells=%v", model.session.Generation, model.session.ThreadID, model.historyCells)
	}
	updated, _ = model.Update(fullscreenAppEventMsg{event: application.SessionEventObserved{Generation: 1, Event: testProtocolEvent(testThreadID(1), "", protocol.WarningEvent{Message: "stale"})}})
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
		Generation: 2, SessionID: protocol.SessionIDFromThreadID(testThreadID(2)), ThreadID: testThreadID(2), Title: "resumed", Configuration: protocol.SessionConfiguration{Mode: protocol.ModeKindDefault},
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
		Generation: 2, SessionID: protocol.SessionIDFromThreadID(testThreadID(2)), ThreadID: testThreadID(2), Title: "resumed", Configuration: protocol.SessionConfiguration{Mode: protocol.ModeKindDefault},
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
	now := time.Now().UTC()
	updated, _ = model.Update(fullscreenAppEventMsg{event: application.SessionEventObserved{Generation: 1, Event: testProtocolEvent(testThreadID(1), "turn-1", protocol.ItemCompletedEvent{Item: protocol.TurnItem{
		ID: "compact-1", Kind: protocol.ItemContextCompaction, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now,
		Payload: protocol.ContextCompactionItem{Trigger: protocol.CompactionTriggerManual, Reason: protocol.CompactionReasonUserRequested, Phase: protocol.CompactionPhaseStandaloneTurn},
	}})}})
	model = updated.(fullscreenModel)
	if lastCellContent(model) != "Context compacted" {
		t.Fatalf("compact result = %q", lastCellContent(model))
	}
	updated, _ = model.Update(fullscreenAppEventMsg{event: application.SessionEventObserved{Generation: 1, Event: testProtocolEvent(testThreadID(1), "turn-1", protocol.WarningEvent{Message: "Heads up: Long threads and multiple compactions can cause the model to be less accurate. Start a new thread when possible to keep threads small and targeted."})}})
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
	updated, _ = model.Update(fullscreenAppEventMsg{event: application.MCPInventoryLoaded{RequestID: 1, Generation: 1, ThreadID: testThreadID(1)}})
	model = updated.(fullscreenModel)
	if got, want := renderHistoryCells(model.historyCells, HistoryRenderRaw, noColorRenderContext()), "/mcp\n\n🔌  MCP Tools\n\n  • No MCP servers configured."; got != want {
		t.Fatalf("MCP output\n got: %q\nwant: %q", got, want)
	}
	before := len(model.historyCells)
	updated, _ = model.Update(fullscreenAppEventMsg{event: application.MCPInventoryLoaded{RequestID: 0, Generation: 1, ThreadID: testThreadID(1), Error: errors.New("stale")}})
	if len(updated.(fullscreenModel).historyCells) != before {
		t.Fatal("stale MCP result changed history")
	}
}

func TestFullscreenStatusUsesStructuredCell(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	fake := fakeApplication(t, model)
	effort := llm.ReasoningEffortHigh
	fake.status = application.StatusSnapshot{SessionID: protocol.SessionIDFromThreadID(testThreadID(1)), ThreadID: testThreadID(1), Title: "demo", Provider: "openai", Model: "gpt", ReasoningEffort: &effort, Phase: "idle"}
	updated, _ := model.dispatchCommand(SlashInvocation{Command: SlashStatus})
	cell, ok := updated.(fullscreenModel).historyCells[0].(StatusHistoryCell)
	if !ok {
		t.Fatalf("status cell = %T", updated.(fullscreenModel).historyCells[0])
	}
	if lines := strings.Join(cell.RawLines(), "\n"); !strings.Contains(lines, "Reasoning effort: high") {
		t.Fatalf("status cell omitted effort:\n%s", lines)
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
	model.session.Title = "current"
	updated, _ := model.Update(fullscreenAppEventMsg{event: application.ThreadNameUpdated{Generation: 0, ThreadID: testThreadID(99), Name: "stale"}})
	model = updated.(fullscreenModel)
	if model.session.Title != "current" {
		t.Fatalf("stale rename changed title to %q", model.session.Title)
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

func TestFullscreenInvalidSlashInputDoesNotEnterHistory(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.input.SetValue("/unknown")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(fullscreenModel)
	if len(model.history) != 0 || !strings.Contains(lastCellContent(model), "unknown command") {
		t.Fatalf("history=%v last=%q", model.history, lastCellContent(model))
	}
}

func TestFullscreenResumeRejectsInvalidThreadIDWithoutChangingAttachment(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	original := model.session.ThreadID
	updated, command := model.dispatchCommand(SlashInvocation{Command: SlashResume, Args: "not-a-uuid"})
	model = updated.(fullscreenModel)
	if model.session.ThreadID != original || len(fakeApplication(t, model).resumed) != 0 {
		t.Fatalf("invalid resume changed attachment: session=%q resumed=%#v", model.session.ThreadID, fakeApplication(t, model).resumed)
	}
	if command == nil || !strings.Contains(lastCellContent(model), "Invalid session ID") {
		t.Fatalf("invalid resume feedback = command:%v history:%q", command != nil, lastCellContent(model))
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
