package tui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/testutil"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestTUIRetryPreservesStreamSourceAndDoesNotCreateHistory(t *testing.T) {
	_, model := newTestModel(t, nil)
	startedAt := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	applySessionEvent(t, &model, protocol.TurnStartedEvent{StartedAt: startedAt})
	applySessionEvent(t, &model, protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: startedAt}})
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "partial"})
	if model.transcript.ActiveCell != nil {
		t.Fatalf("assistant stream created tool activity cell: %T", model.transcript.ActiveCell)
	}
	historyCount := len(model.historyCells)

	details := "idle timeout waiting for provider stream"
	applySessionEvent(t, &model, protocol.StreamErrorEvent{Message: "Reconnecting... 1/5", AdditionalDetails: &details, WillRetry: true})

	if model.markdownStreams.assistant == nil || model.markdownStreams.assistant.Source.Source() != "partial" || len(model.historyCells) != historyCount {
		t.Fatalf("retry mutated transcript: stream=%#v history=%d want=%d", model.markdownStreams.assistant, len(model.historyCells), historyCount)
	}
	if model.status != "Reconnecting... 1/5" || model.statusDetails != details || !model.retryStatus.active {
		t.Fatalf("retry status = %q details=%q saved=%#v", model.status, model.statusDetails, model.retryStatus)
	}
}

func TestTUIRetryRestoresStatusAndResetsStreamAttempt(t *testing.T) {
	_, model := newTestModel(t, nil)
	startedAt := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	applySessionEvent(t, &model, protocol.TurnStartedEvent{StartedAt: startedAt})
	applySessionEvent(t, &model, protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: startedAt}})
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "partial"})
	applySessionEvent(t, &model, protocol.StreamErrorEvent{Message: "Reconnecting... 1/5", WillRetry: true})
	applySessionEvent(t, &model, protocol.StreamErrorEvent{Message: "Reconnecting... 2/5", WillRetry: true})

	if model.retryStatus.header != "working" {
		t.Fatalf("consecutive retry overwrote saved status: %#v", model.retryStatus)
	}
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Reset: true})
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "recovered"})
	if model.status != "working" || model.statusDetails != "" || model.retryStatus.active || model.markdownStreams.assistant == nil || model.markdownStreams.assistant.Source.Source() != "recovered" {
		t.Fatalf("recovery state: status=%q details=%q saved=%#v stream=%#v", model.status, model.statusDetails, model.retryStatus, model.markdownStreams.assistant)
	}
}

func TestTUICompletedAssistantItemReplacesLiveAttempt(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.session.Configuration.CWD = "/original"
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	applySessionEvent(t, &model, protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: now}})
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "lost transport text\n"})
	model.session.Configuration.CWD = "/later"
	applySessionEvent(t, &model, protocol.ItemCompletedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: "canonical final"}})
	if model.markdownStreams.assistant != nil {
		t.Fatal("completion retained active stream")
	}
	cell, ok := model.historyCells[len(model.historyCells)-1].(*AgentMarkdownCell)
	if !ok || cell.Source.Text != "canonical final" || cell.Source.CWD != "/original" {
		t.Fatalf("completed cell = %#v", model.historyCells[len(model.historyCells)-1])
	}
}

func TestTUIStreamStableRunConsolidatesIntoFinalCell(t *testing.T) {
	_, model := newTestModel(t, nil)
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	applySessionEvent(t, &model, protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: now}})
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "stable paragraph\n\nmutable\n"})
	if len(model.historyCells) != 1 {
		t.Fatalf("stable run cells = %#v", model.historyCells)
	}
	if _, ok := model.historyCells[0].(*AgentMessageCell); !ok {
		t.Fatalf("stable run cell = %T", model.historyCells[0])
	}
	if model.TranscriptSurface.activeMarkdownTail == nil {
		t.Fatal("mutable tail is missing")
	}
	applySessionEvent(t, &model, protocol.ItemCompletedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: "stable paragraph\n\ncanonical final"}})
	if len(model.historyCells) != 1 || model.TranscriptSurface.activeMarkdownTail != nil {
		t.Fatalf("consolidated transcript = %#v tail=%#v", model.historyCells, model.TranscriptSurface.activeMarkdownTail)
	}
	cell, ok := model.historyCells[0].(*AgentMarkdownCell)
	if !ok || cell.Source.Text != "stable paragraph\n\ncanonical final" {
		t.Fatalf("final cell = %#v", model.historyCells[0])
	}
}

func TestTUIStreamCompletionReplacesAttachmentBeforeDeferredWarning(t *testing.T) {
	_, model := newTestModel(t, nil)
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	applySessionEvent(t, &model, protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: now}})
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "stable paragraph\n\nmutable\n"})
	applySessionEvent(t, &model, protocol.WarningEvent{Message: "warning after streamed content"})
	if len(model.historyCells) != 1 || len(model.markdownStreams.deferred) != 1 {
		t.Fatalf("warning interrupted stream: history=%#v deferred=%#v", model.historyCells, model.markdownStreams.deferred)
	}

	applySessionEvent(t, &model, protocol.ItemCompletedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: "canonical final"}})
	if len(model.historyCells) != 2 || len(model.markdownStreams.deferred) != 0 {
		t.Fatalf("completion/deferred projection = history:%#v deferred:%#v", model.historyCells, model.markdownStreams.deferred)
	}
	if cell, ok := model.historyCells[0].(*AgentMarkdownCell); !ok || cell.Source.Text != "canonical final" {
		t.Fatalf("first cell = %#v, want canonical final markdown", model.historyCells[0])
	}
	if warning := cellContent(model.historyCells[1]); !strings.Contains(warning, "warning after streamed content") {
		t.Fatalf("deferred warning = %q", warning)
	}
}

func TestTUIStreamDefersApprovalUntilCompletion(t *testing.T) {
	_, model := newTestModel(t, nil)
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	applySessionEvent(t, &model, protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: now}})
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "partial"})
	request := policy.ApprovalRequest{ID: "approval-1", ToolName: "execute_command", Command: "go test ./..."}
	updated, _ := model.Update(appEventMsg{event: application.ApprovalRequested{Generation: model.session.Generation, RequestID: "request-1", Request: request}})
	model = updated.(appModel)
	if model.approval != nil || len(model.markdownStreams.deferred) != 1 {
		t.Fatalf("approval was not deferred: approval=%#v deferred=%#v", model.approval, model.markdownStreams.deferred)
	}

	applySessionEvent(t, &model, protocol.ItemCompletedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: "done"}})
	if model.approval == nil || model.approval.requestID != "request-1" || len(model.markdownStreams.deferred) != 0 {
		t.Fatalf("deferred approval completion = approval:%#v deferred:%#v", model.approval, model.markdownStreams.deferred)
	}
}

func TestTUIStreamDefersCompleteToolLifecycleInFIFOOrder(t *testing.T) {
	_, model := newTestModel(t, nil)
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	applySessionEvent(t, &model, protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: now}})
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "commentary before tool\n"})
	toolItem := protocol.TurnItem{ID: "tool-1", Kind: protocol.ItemCommandExecution, Status: protocol.ItemInProgress, CreatedAt: now, ToolName: "execute_command", CallID: "call-1", Payload: protocol.CommandExecutionItemPayload{ActionSummary: "Run command", SideEffect: "execute"}}
	applySessionEvent(t, &model, protocol.ItemStartedEvent{Item: toolItem})
	applySessionEvent(t, &model, protocol.CommandOutputDeltaEvent{ItemID: toolItem.ID, Delta: "tool output\n"})
	toolItem.Status = protocol.ItemStatusCompleted
	toolItem.CompletedAt = now
	toolItem.Text = "tool output\n"
	applySessionEvent(t, &model, protocol.ItemCompletedEvent{Item: toolItem})
	if model.transcript.ActiveCell != nil || len(model.markdownStreams.deferred) != 3 {
		t.Fatalf("tool lifecycle interrupted stream: active=%T deferred=%#v", model.transcript.ActiveCell, model.markdownStreams.deferred)
	}

	applySessionEvent(t, &model, protocol.ItemCompletedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: "commentary before tool"}})
	if len(model.historyCells) != 1 {
		t.Fatalf("assistant replacement history = %#v", model.historyCells)
	}
	if _, ok := model.historyCells[0].(*AgentMarkdownCell); !ok {
		t.Fatalf("first completed cell = %T, want AgentMarkdownCell", model.historyCells[0])
	}
	toolCell, ok := model.transcript.ActiveCell.(*ToolHistoryCell)
	if !ok || !toolCell.IsComplete() || !strings.Contains(strings.Join(toolCell.RawLines(), "\n"), "tool output") {
		t.Fatalf("deferred tool lifecycle = %#v", model.transcript.ActiveCell)
	}
}

func TestTUITerminalStreamErrorDiscardsAttachmentThenFlushesDeferredOrder(t *testing.T) {
	_, model := newTestModel(t, nil)
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	applySessionEvent(t, &model, protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: now}})
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "partial paragraph\n\nmutable\n"})
	applySessionEvent(t, &model, protocol.WarningEvent{Message: "warning before failure"})
	applySessionEvent(t, &model, protocol.StreamErrorEvent{Message: "terminal stream failure"})
	if model.markdownStreams.assistant != nil || model.TranscriptSurface.activeStream != nil || len(model.markdownStreams.deferred) != 0 {
		t.Fatalf("terminal stream cleanup = host:%#v attachment:%#v", model.markdownStreams, model.TranscriptSurface.activeStream)
	}
	if len(model.historyCells) != 2 || !strings.Contains(cellContent(model.historyCells[0]), "warning before failure") || !strings.Contains(cellContent(model.historyCells[1]), "terminal stream failure") {
		t.Fatalf("terminal failure order = %#v", model.historyCells)
	}
}

func TestTUIClearDropsOldStreamAndDeferredProjection(t *testing.T) {
	_, model := newTestModel(t, nil)
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	applySessionEvent(t, &model, protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: now}})
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "old attachment\n\nmutable\n"})
	applySessionEvent(t, &model, protocol.WarningEvent{Message: "old deferred warning"})
	updated, _ := model.Update(appEventMsg{event: application.ClearUIStarted{}})
	model = updated.(appModel)
	if len(model.historyCells) != 0 || model.markdownStreams.active() || len(model.markdownStreams.deferred) != 0 || model.TranscriptSurface.activeStream != nil || model.TranscriptSurface.activeMarkdownTail != nil {
		t.Fatalf("clear retained old stream state: history=%#v host=%#v surface=%#v", model.historyCells, model.markdownStreams, model.TranscriptSurface)
	}
}

func TestTUIResetDiscardsOnlyTrailingActiveStreamRun(t *testing.T) {
	_, model := newTestModel(t, nil)
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	model.insertHistoryCell(NewInfoHistoryCell("earlier history"))
	applySessionEvent(t, &model, protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: now}})
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "first attempt\n\nmutable\n"})
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Reset: true})
	if len(model.historyCells) != 1 || !strings.Contains(lastCellContent(model), "earlier history") || model.TranscriptSurface.activeMarkdownTail != nil {
		t.Fatalf("reset transcript = %#v tail=%#v", model.historyCells, model.TranscriptSurface.activeMarkdownTail)
	}
}

func TestTUIResizeRebuildsStableRunAndTailFromSource(t *testing.T) {
	_, model := newTestModel(t, nil)
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	applySessionEvent(t, &model, protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: now}})
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "stable paragraph\n\nmutable tail\n"})
	model.width = 18
	model.refreshActiveMarkdownFrames()
	if len(model.historyCells) != 1 {
		t.Fatalf("reflow stable run = %#v", model.historyCells)
	}
	if model.TranscriptSurface.activeMarkdownTail == nil {
		t.Fatal("reflow dropped mutable tail")
	}
	if got := strings.Join(model.historyCells[0].RawLines(), "\n"); !strings.Contains(got, "stable paragraph") {
		t.Fatalf("reflow changed stable source: %q", got)
	}
}

func TestTUIActiveStreamRenderModeChangeRebuildsFromExactSource(t *testing.T) {
	_, model := newTestModel(t, nil)
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	applySessionEvent(t, &model, protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: now}})
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "**strong**\n\nmutable\n"})
	model.historyMode = HistoryRenderRaw
	model.refreshActiveMarkdownFrames()
	if model.markdownStreams.assistant == nil || model.markdownStreams.assistant.Mode != HistoryRenderRaw {
		t.Fatalf("stream mode was not rebuilt: %#v", model.markdownStreams.assistant)
	}
	raw := xansi.Strip(model.transcriptContent(20))
	if !strings.Contains(raw, "**strong**") || !strings.Contains(raw, "mutable") {
		t.Fatalf("raw stream projection = %q", raw)
	}
}

func TestTUIReferenceDefinitionRewindsPreviouslyEmittedStableRun(t *testing.T) {
	_, model := newTestModel(t, nil)
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	applySessionEvent(t, &model, protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: now}})
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "earlier [reference][id].\n\n"})
	if len(model.historyCells) == 0 {
		t.Fatal("reference paragraph did not enter provisional stable run")
	}
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "[id]:https://example.com/reference\n"})
	if len(model.historyCells) != 0 || model.TranscriptSurface.activeMarkdownTail == nil {
		t.Fatalf("reference recompute did not rewind surface: history=%#v tail=%#v", model.historyCells, model.TranscriptSurface.activeMarkdownTail)
	}
	tail := strings.Join(model.TranscriptSurface.activeMarkdownTail.RawLines(), "\n")
	if !strings.Contains(tail, "https://example.com/reference") {
		t.Fatalf("reference recompute tail = %q", tail)
	}
}

func TestTUITranscriptSurfaceRendersStableRunAndTailTogether(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.height = 60
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	applySessionEvent(t, &model, protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: now}})
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "stable paragraph\n\nmutable tail\n"})
	view := xansi.Strip(model.View())
	if !strings.Contains(view, "stable paragraph") || !strings.Contains(view, "mutable tail") {
		t.Fatalf("surface omitted stream regions: %q", view)
	}
}

func TestTUISessionHeaderAndFirstUserMessageAreScheduledForNativeHistory(t *testing.T) {
	_, model := newTestModel(t, nil)
	if model.TranscriptSurface.sessionHeader == nil || !model.TranscriptSurface.sessionHeaderPrinted || model.initialHistoryFlush == nil {
		t.Fatalf("initial session header print state = surface:%#v command:%v", model.TranscriptSurface, model.initialHistoryFlush != nil)
	}
	model.insertHistoryCell(NewUserMessageCell("hello"))
	if command := model.flushHistory(); command == nil || model.TranscriptSurface.historyPrintCursor != 1 {
		t.Fatalf("first user message was not scheduled after header: cursor=%d command=%v", model.TranscriptSurface.historyPrintCursor, command != nil)
	}
	if view := xansi.Strip(model.View()); strings.Contains(view, "› hello") {
		t.Fatalf("printed user history remained duplicated in active frame: %q", view)
	}
}

func TestTUIFirstStreamingReplyKeepsFinalCellSpacing(t *testing.T) {
	ctx := noColorRenderContext()
	ctx.Width = 80
	user := NewUserMessageCell("plz introduce urself")
	renderer := newMarkdownRenderer()
	lines := renderer.Render(newMarkdownSource("I am Amadeus.\n", ""), HistoryRenderRich)
	streaming := renderHistoryCells([]HistoryCell{
		user,
		StreamingAgentTailCell{ItemID: "assistant-1", Lines: lines, First: true},
	}, HistoryRenderRich, ctx)
	completed := renderHistoryCells([]HistoryCell{
		user,
		NewAgentMarkdownCell(newMarkdownSource("I am Amadeus.\n", "")),
	}, HistoryRenderRich, ctx)
	streaming = xansi.Strip(streaming)
	completed = xansi.Strip(completed)
	if streaming != completed || !strings.Contains(streaming, "› plz introduce urself\n\n\n• I am Amadeus.") {
		t.Fatalf("stream/final spacing changed:\nstream=%q\nfinal=%q", streaming, completed)
	}
}

func TestTUIPrintedHistoryKeepsCodexSpacingBeforeWorkingAndComposer(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.height = 30
	model.insertHistoryCell(NewUserMessageCell("hello"))
	if model.flushHistory() == nil {
		t.Fatal("user history was not scheduled for native output")
	}
	model.running = true
	model.runStartedAt = time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	model.status = "working"
	view := strings.Split(xansi.Strip(model.View()), "\n")
	if len(view) < 6 || view[0] != "" || view[1] != "" || !strings.Contains(view[2], "Working") || view[3] != "" || view[4] != "" || !strings.HasPrefix(view[5], "› ") {
		t.Fatalf("printed history/working/composer spacing = %#v", view)
	}

	model.running = false
	view = strings.Split(xansi.Strip(model.View()), "\n")
	if len(view) < 3 || view[0] != "" || view[1] != "" || !strings.HasPrefix(view[2], "› ") {
		t.Fatalf("printed final/composer spacing = %#v", view)
	}
}

func TestTUIStreamingOutputWorkingAndComposerUseTwoBlankBoundaries(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.height = 30
	model.insertHistoryCell(NewUserMessageCell("hello"))
	model.flushHistory()
	now := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	applySessionEvent(t, &model, protocol.TurnStartedEvent{StartedAt: now})
	applySessionEvent(t, &model, protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: now}})
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "streaming reply\n"})
	view := strings.Split(xansi.Strip(model.View()), "\n")
	indices := map[string]int{"reply": -1, "working": -1, "composer": -1}
	for index, line := range view {
		switch {
		case strings.Contains(line, "streaming reply"):
			indices["reply"] = index
		case strings.Contains(line, "esc to interrupt"):
			indices["working"] = index
		case strings.HasPrefix(line, "› "):
			indices["composer"] = index
		}
	}
	if indices["reply"] < 2 || indices["working"] != indices["reply"]+3 || indices["composer"] != indices["working"]+3 || view[0] != "" || view[1] != "" {
		t.Fatalf("stream/working/composer spacing indices=%v view=%#v", indices, view)
	}
}

func TestTUIImmutableHistoryPrintsOnceAndLeavesBoundedActiveFrame(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.height = 12
	for index := 0; index < 20; index++ {
		model.insertHistoryCell(NewAgentMarkdownCell(newMarkdownSource("message "+strconv.Itoa(index), "")))
	}
	if command := model.flushHistory(); command == nil || model.TranscriptSurface.historyPrintCursor != 20 {
		t.Fatalf("immutable history print state = cursor:%d command:%v", model.TranscriptSurface.historyPrintCursor, command != nil)
	}
	view := xansi.Strip(model.View())
	if height := lipgloss.Height(view); height > model.height {
		t.Fatalf("main frame height = %d, terminal height = %d", height, model.height)
	}
	if strings.Contains(view, "message 0") || strings.Contains(view, "message 19") {
		t.Fatalf("native history was duplicated in active frame: %q", view)
	}
}

func TestTUITranscriptPageKeysPreserveComposerAndFollowState(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.height = 16
	model.input.SetValue("keep draft")
	for index := 0; index < 20; index++ {
		model.insertHistoryCell(NewAgentMarkdownCell(newMarkdownSource("message "+strconv.Itoa(index), "")))
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(appModel)
	if model.input.Value() != "keep draft" || len(model.historyCells) != 20 || model.TranscriptSurface.viewport.followBottom {
		t.Fatalf("page up state = input:%q history=%d viewport=%#v", model.input.Value(), len(model.historyCells), model.TranscriptSurface.viewport)
	}
	model.insertHistoryCell(NewAgentMarkdownCell(newMarkdownSource("new while detached", "")))
	if detached := xansi.Strip(model.View()); strings.Contains(detached, "new while detached") {
		t.Fatalf("new content stole detached viewport: %q", detached)
	}
	for range 10 {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		model = updated.(appModel)
	}
	if model.input.Value() != "keep draft" || len(model.historyCells) != 21 || !model.TranscriptSurface.viewport.followBottom {
		t.Fatalf("page down state = input:%q history=%d viewport=%#v", model.input.Value(), len(model.historyCells), model.TranscriptSurface.viewport)
	}
	if bottom := xansi.Strip(model.View()); !strings.Contains(bottom, "new while detached") {
		t.Fatalf("bottom page omitted appended content: %q", bottom)
	}
}

func TestTUIDeltaWithoutStartedItemStaysDiagnostic(t *testing.T) {
	_, model := newTestModel(t, nil)
	applySessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "unknown", Delta: "must not recover"})
	if model.markdownStreams.assistant != nil {
		t.Fatal("delta without ItemStarted created a controller")
	}
	if !strings.Contains(lastCellContent(model), "unknown item") {
		t.Fatalf("missing strict reducer diagnostic: %q", lastCellContent(model))
	}
}

func TestTUICompletedPlanItemReplacesLiveAttempt(t *testing.T) {
	_, model := newTestModel(t, nil)
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	applySessionEvent(t, &model, protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "plan-1", Kind: protocol.ItemPlan, Status: protocol.ItemInProgress, CreatedAt: now}})
	applySessionEvent(t, &model, protocol.PlanDeltaEvent{ItemID: "plan-1", Delta: "live plan\n"})
	applySessionEvent(t, &model, protocol.ItemCompletedEvent{Item: protocol.TurnItem{ID: "plan-1", Kind: protocol.ItemPlan, Status: protocol.ItemStatusCompleted, CreatedAt: now, CompletedAt: now, Text: "canonical plan"}})
	cell, ok := model.historyCells[len(model.historyCells)-1].(*ProposedPlanCell)
	if !ok || cell.Source.Text != "canonical plan" {
		t.Fatalf("completed plan cell = %#v", model.historyCells[len(model.historyCells)-1])
	}
}

func TestTUIRetryTerminalAndAttachClearTransientState(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.running = true
	model.status = "thinking"
	applySessionEvent(t, &model, protocol.StreamErrorEvent{Message: "Reconnecting... 1/5", WillRetry: true})
	applySessionEvent(t, &model, protocol.StreamErrorEvent{Message: "provider unavailable"})
	if model.retryStatus.active || !strings.Contains(lastCellContent(model), "provider unavailable") {
		t.Fatalf("terminal retry state=%#v history=%q", model.retryStatus, lastCellContent(model))
	}

	applySessionEvent(t, &model, protocol.StreamErrorEvent{Message: "Reconnecting... 1/5", WillRetry: true})
	updated, _ := model.Update(appEventMsg{event: application.ThreadAttached{Snapshot: application.ThreadViewSnapshot{Generation: 2, SessionID: testutil.SessionID(2), ThreadID: testThreadID(2), Configuration: protocol.SessionConfiguration{Model: "test-model", Mode: protocol.ModeKindDefault}}}})
	model = updated.(appModel)
	if model.retryStatus.active || model.statusDetails != "" || model.status != "idle" || !model.input.Focused() {
		t.Fatalf("attach retained retry state: status=%q details=%q saved=%#v focused=%v", model.status, model.statusDetails, model.retryStatus, model.input.Focused())
	}
}

func TestTUIRetryWorkingLineHandlesHiddenNoColorAndNarrowLayout(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.running = false
	model.width = 24
	model.runStartedAt = time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	model.clock = fixedMotionClock{now: model.runStartedAt.Add(time.Second)}
	details := "idle timeout waiting for a very slow provider stream"
	applySessionEvent(t, &model, protocol.StreamErrorEvent{Message: "Reconnecting... 2/5", AdditionalDetails: &details, WillRetry: true})

	line := model.workingLine()
	parts := strings.Split(line, "\n")
	if len(parts) != 2 || !strings.Contains(parts[0], "Reconnecting") || !strings.Contains(parts[1], "└") {
		t.Fatalf("retry working line = %q", line)
	}
	for _, part := range parts {
		if strings.Contains(part, "\x1b[") || xansi.StringWidth(part) > model.width {
			t.Fatalf("unsafe narrow no-color line width=%d line=%q", xansi.StringWidth(part), part)
		}
	}
}

func applySessionEvent(t *testing.T, model *appModel, message protocol.EventMsg) {
	t.Helper()
	updated, _ := model.Update(appEventMsg{event: application.SessionEventObserved{
		Generation: model.session.Generation,
		Event:      testProtocolEvent(model.session.ThreadID, "turn-1", message),
	}})
	*model = updated.(appModel)
}
