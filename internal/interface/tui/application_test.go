package tui

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/policy"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

func newTestFullscreen(t *testing.T, configure func(*FullscreenOptions)) (*FullscreenApplication, fullscreenModel) {
	t.Helper()
	options := FullscreenOptions{
		Input: &bytes.Buffer{}, Output: &bytes.Buffer{}, Width: 100, DisableAnimations: true,
		Task: func(context.Context, TaskSubmission) error { return nil },
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

func cellContent(cell transcriptCell) string {
	if cell == nil {
		return ""
	}
	return strings.Join(cell.RawLines(), "\n")
}

func lastCellContent(model fullscreenModel) string {
	if len(model.transcript.Cells) == 0 {
		return ""
	}
	return cellContent(model.transcript.Cells[len(model.transcript.Cells)-1])
}

func TestFullscreenTextareaPreservesChineseAndBackspace(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("请检查中文")})
	model = updated.(fullscreenModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	model = updated.(fullscreenModel)
	if got := model.input.Value(); got != "请检查中" {
		t.Fatalf("Chinese input = %q", got)
	}
}

func TestFullscreenBannerUsesLogoAndRestrainedMetadata(t *testing.T) {
	_, model := newTestFullscreen(t, func(options *FullscreenOptions) {
		options.Startup = FullscreenStartup{Version: "dev", Provider: "openai", Model: "gpt", Project: "/tmp/project", Branch: "main", Session: "session"}
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
	if maxLineWidth(banner) >= model.width {
		t.Fatalf("banner touches terminal edge: width=%d\n%s", model.width, banner)
	}
}

func TestFullscreenAcceptsAndQueuesInputWhileRunning(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.running = true
	model.input.SetValue("继续检查")
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(fullscreenModel)
	if command == nil || len(model.queuedTasks) != 1 || model.queuedTasks[0].Content != "继续检查" || lastCellContent(model) != "继续检查" {
		t.Fatalf("queued state: tasks=%#v last=%q", model.queuedTasks, lastCellContent(model))
	}
}

func TestFullscreenPlanAndShiftTabModes(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	updated, _ := model.submitCommand("/plan")
	model = updated.(fullscreenModel)
	if model.collaboration != CollaborationPlan {
		t.Fatalf("plan mode = %q", model.collaboration)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	model = updated.(fullscreenModel)
	if model.collaboration != CollaborationExecute {
		t.Fatalf("shift-tab mode = %q", model.collaboration)
	}
	model.running = true
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	model = updated.(fullscreenModel)
	if model.collaboration != CollaborationExecute || !strings.Contains(lastCellContent(model), "cannot change") {
		t.Fatalf("running mode changed")
	}
}

func TestFullscreenFlushCommitsCellsOnce(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.appendCell(cellUser, "检查项目")
	command := model.flushTranscript()
	if command == nil || model.transcript.Committed != 1 {
		t.Fatalf("first flush failed")
	}
	if output := fmt.Sprint(command()); !strings.Contains(output, "检查项目") {
		t.Fatalf("flush output = %q", output)
	}
	if command := model.flushTranscript(); command != nil {
		t.Fatal("second flush repeated history")
	}
}

func TestFullscreenWorkingUsesFixedClockAndDoesNotCommitHistory(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	start := time.Unix(100, 0)
	model.running = true
	model.runStartedAt = start
	model.motionStartedAt = start
	model.clock = fixedMotionClock{now: start.Add(65 * time.Second)}
	before := len(model.transcript.Cells)
	line := xansi.Strip(model.workingLine())
	if !strings.Contains(line, "• Working (1m 05s • esc to interrupt)") {
		t.Fatalf("working line = %q", line)
	}
	updated, command := model.Update(fullscreenWorkingTickMsg(start.Add(time.Second)))
	model = updated.(fullscreenModel)
	if command != nil || len(model.transcript.Cells) != before {
		t.Fatalf("working tick affected transcript")
	}
}

func TestFullscreenToolEventsAreVisibleBeforeIterationCompletion(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.applyEvent(event.IterationStarted{Iteration: 1})
	model.applyEvent(event.ToolCallStarted{Iteration: 1, CallID: "read", ToolName: "read_file", SideEffect: "read", ActionSummary: "Read docs/design.md"})
	if model.transcript.ActiveCell == nil || !strings.Contains(xansi.Strip(model.renderActiveCell()), "Exploring") {
		t.Fatalf("tool was not immediately visible")
	}
	count := len(model.transcript.Cells)
	model.applyEvent(event.IterationCompleted{Iteration: 1})
	if len(model.transcript.Cells) != count {
		t.Fatal("iteration completion changed transcript layout")
	}
}

func TestFullscreenSeparatorFollowsToolAndAssistantBoundary(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.applyEvent(event.ToolCallStarted{CallID: "exec", ToolName: "execute_command", SideEffect: "write", Detail: "go test ./..."})
	model.applyEvent(event.ToolCallCompleted{CallID: "exec", ToolName: "execute_command", Success: true})
	model.applyEvent(event.TextDelta{Delta: "完成。"})
	if len(model.transcript.Cells) != 2 || model.transcript.Cells[0].Kind() != cellTool || model.transcript.Cells[1].Kind() != cellSeparator {
		t.Fatalf("tool/final boundary = %#v", model.transcript.Cells)
	}
	model.finishDraft()
	if model.transcript.Cells[2].Kind() != cellAssistant {
		t.Fatalf("assistant order incorrect")
	}
}

func TestFullscreenTaskCompletionUsesShortAndLongSeparator(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.running = true
	model.runStartedAt = time.Now().Add(-30 * time.Second)
	model.transcript.HadWorkActivity = true
	model.transcript.NeedsFinalMessageSeparator = true
	updated, _ := model.Update(fullscreenTaskDoneMsg{})
	model = updated.(fullscreenModel)
	if len(model.transcript.Cells) != 1 || model.transcript.Cells[0].Kind() != cellSeparator || strings.Contains(cellContent(model.transcript.Cells[0]), "Worked") {
		t.Fatalf("short separator = %#v", model.transcript.Cells)
	}
	separator := finalMessageSeparatorCell{elapsed: 65 * time.Second}
	if got := xansi.Strip(separator.Render(model.transcriptRenderContext())); !strings.Contains(got, "Worked for 1m 05s") {
		t.Fatalf("long separator = %q", got)
	}
}

func TestFullscreenErrorAfterToolKeepsFinalSeparator(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	start := time.Unix(100, 0)
	model.running = true
	model.runStartedAt = start
	model.clock = fixedMotionClock{now: start.Add(61 * time.Second)}
	model.transcript.HadWorkActivity = true
	model.transcript.NeedsFinalMessageSeparator = true
	updated, _ := model.Update(fullscreenTaskDoneMsg{err: context.Canceled})
	model = updated.(fullscreenModel)
	if len(model.transcript.Cells) != 2 || model.transcript.Cells[0].Kind() != cellSeparator || model.transcript.Cells[1].Kind() != cellNotice {
		t.Fatalf("cancel boundary = %#v", model.transcript.Cells)
	}
	if !strings.Contains(cellContent(model.transcript.Cells[0]), "Worked for 1m 01s") {
		t.Fatalf("cancel duration missing")
	}
}

func TestFullscreenModelRendersAgentEvents(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.applyEvent(event.LLMCallStarted{Model: llm.ModelInfo{Name: "gpt-test"}})
	model.applyEvent(event.PlanUpdated{Revision: 1, Items: []event.PlanItem{{Step: "Inspect", Status: "in_progress"}}})
	model.applyEvent(event.DiagnosticPublished{Severity: "warning", Code: "W1", Message: "notice"})
	model.applyEvent(event.ErrorOccurred{Error: event.ErrorInfo{Message: "boom"}})
	if model.model != "gpt-test" || len(model.transcript.Cells) != 3 || model.transcript.Cells[0].Kind() != cellPlan || model.transcript.Cells[2].Kind() != cellError {
		t.Fatalf("event projection = %#v", model.transcript.Cells)
	}
}

func TestFullscreenPlanUpdateSuppressesGenericToolActivity(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.applyEvent(event.ToolCallStarted{CallID: "plan-1", ToolName: "update_plan", SideEffect: "none", ActionSummary: "Ran tool update_plan"})
	model.applyEvent(event.PlanUpdated{Revision: 1, Items: []event.PlanItem{{Step: "Inspect", Status: "in_progress"}}})
	model.applyEvent(event.ToolCallCompleted{CallID: "plan-1", ToolName: "update_plan", Success: true})
	if model.transcript.ActiveCell != nil || len(model.transcript.Cells) != 1 || model.transcript.Cells[0].Kind() != cellPlan {
		t.Fatalf("update_plan should render only the plan cell: active=%#v cells=%#v", model.transcript.ActiveCell, model.transcript.Cells)
	}
	if rendered := xansi.Strip(model.renderCell(model.transcript.Cells[0])); !strings.Contains(rendered, "Updated Plan") || strings.Contains(rendered, "Explored") {
		t.Fatalf("unexpected update_plan projection: %q", rendered)
	}
	if rendered := xansi.Strip(model.renderCell(model.transcript.Cells[0])); !strings.Contains(rendered, "\n  └ □ Inspect") || strings.Contains(rendered, "revision") || strings.Contains(rendered, "plan-1") {
		t.Fatalf("plan cell should follow Codex checklist layout: %q", rendered)
	}
}

func TestFullscreenDiffEventsUpdateStatusWithoutTranscriptNoise(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.applyEvent(event.RunDiffUpdated{Revision: 1, Changes: []event.RunDiffChange{{Path: "/work/a.go", Kind: "updated"}}})
	if len(model.transcript.Cells) != 0 || !model.runDiffExact || len(model.runDiffChanges) != 1 {
		t.Fatalf("exact Patch Diff should update state only: cells=%#v exact=%t changes=%#v", model.transcript.Cells, model.runDiffExact, model.runDiffChanges)
	}
	if status := model.commandStatus(); !strings.Contains(status, "patch diff: 1 file(s)") {
		t.Fatalf("status omitted exact Patch Diff count: %q", status)
	}
	model.applyEvent(event.RunDiffInvalidated{Revision: 2, Reason: "malformed Patch delta"})
	if len(model.transcript.Cells) != 0 || model.runDiffExact || len(model.runDiffChanges) != 0 {
		t.Fatalf("invalid Patch Diff should clear state without transcript noise: cells=%#v exact=%t changes=%#v", model.transcript.Cells, model.runDiffExact, model.runDiffChanges)
	}
	if status := model.commandStatus(); !strings.Contains(status, "patch diff: unavailable") || strings.Contains(status, "attribution") {
		t.Fatalf("status exposed incorrect Diff semantics: %q", status)
	}
}

func TestFullscreenNoColorAndWidthMatrix(t *testing.T) {
	for _, width := range []int{40, 59, 60, 80, 100} {
		_, model := newTestFullscreen(t, func(options *FullscreenOptions) {
			options.Width = width
			options.NoColor = true
			options.Startup = FullscreenStartup{Model: "模型", Project: "/项目/很长/路径"}
		})
		model.width = width
		model.running = true
		model.runStartedAt = time.Now()
		model.motionStartedAt = model.runStartedAt
		for name, rendered := range map[string]string{"banner": model.banner(), "view": model.View(), "user": model.renderCell(newTextCell(cellUser, "中文 🎼 内容"))} {
			if strings.Contains(rendered, "\x1b[") {
				t.Fatalf("width %d %s contains ANSI", width, name)
			}
			if maxLineWidth(rendered) > width {
				t.Fatalf("width %d %s overflow: %d", width, name, maxLineWidth(rendered))
			}
		}
	}
}

func TestFullscreenApprovalUsesKeyboardDecision(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	response := make(chan fullscreenApprovalResult, 1)
	request := policy.ApprovalRequest{ID: "approval", ToolName: "write_file", Risk: policy.CommandRiskHigh, Reason: "writes a file"}
	updated, _ := model.Update(fullscreenApprovalMsg{prompt: &fullscreenApproval{request: request, response: response}})
	model = updated.(fullscreenModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(fullscreenModel)
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(fullscreenModel)
	decision := (<-response).decision
	if decision.Outcome != policy.ApprovalAllow || decision.Scope != policy.ApprovalSession || model.approval != nil || command == nil {
		t.Fatalf("approval result = %#v", decision)
	}
}

func TestFullscreenResumeOverlayAndCopyClear(t *testing.T) {
	copied := ""
	_, model := newTestFullscreen(t, func(options *FullscreenOptions) {
		options.ClipboardWrite = func(value string) error { copied = value; return nil }
		options.Sessions = func(context.Context) ([]SessionOption, error) {
			return []SessionOption{{ID: "session-1", Title: "Current", Current: true}}, nil
		}
		options.Resume = func(context.Context, string) (string, error) { return "resumed", nil }
	})
	model.transcript.LastAssistantMarkdown = "**done**"
	updated, _ := model.submitCommand("/copy")
	model = updated.(fullscreenModel)
	if copied != "**done**" || !strings.Contains(lastCellContent(model), "Copied") {
		t.Fatalf("copy failed")
	}
	updated, command := model.submitCommand("/resume")
	model = updated.(fullscreenModel)
	message := command()
	updated, _ = model.Update(message)
	model = updated.(fullscreenModel)
	if model.selection == nil || model.selectionKind != "resume" {
		t.Fatalf("resume overlay missing")
	}
	model.collaboration = CollaborationPlan
	updated, _ = model.Update(fullscreenCommandDoneMsg{command: "/clear"})
	model = updated.(fullscreenModel)
	if len(model.transcript.Cells) != 0 || model.collaboration != CollaborationExecute {
		t.Fatalf("clear did not reset state")
	}
}

func TestPrefixRenderedBlockSkipsANSIOnlyBlankLines(t *testing.T) {
	input := "\x1b[0m\nhello\n\x1b[0m\n"
	if got := xansi.Strip(prefixRenderedBlock(input, "• ")); got != "• hello" {
		t.Fatalf("prefixed block = %q", got)
	}
}

func maxLineWidth(value string) int {
	maximum := 0
	for _, line := range strings.Split(value, "\n") {
		if width := lipgloss.Width(line); width > maximum {
			maximum = width
		}
	}
	return maximum
}
