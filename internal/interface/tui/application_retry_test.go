package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	application "github.com/Godric-W/Amadeus/internal/app"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestFullscreenRetryPreservesDraftAndDoesNotCreateHistory(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	startedAt := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	applyFullscreenSessionEvent(t, &model, protocol.TurnStartedEvent{StartedAt: startedAt})
	applyFullscreenSessionEvent(t, &model, protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: startedAt}})
	applyFullscreenSessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "partial"})
	if model.transcript.ActiveCell != nil {
		t.Fatalf("assistant stream created tool activity cell: %T", model.transcript.ActiveCell)
	}
	historyCount := len(model.historyCells)

	details := "idle timeout waiting for provider stream"
	applyFullscreenSessionEvent(t, &model, protocol.StreamErrorEvent{Message: "Reconnecting... 1/5", AdditionalDetails: &details, WillRetry: true})

	if model.draft != "partial" || len(model.historyCells) != historyCount {
		t.Fatalf("retry mutated transcript: draft=%q history=%d want=%d", model.draft, len(model.historyCells), historyCount)
	}
	if model.status != "Reconnecting... 1/5" || model.statusDetails != details || !model.retryStatus.active {
		t.Fatalf("retry status = %q details=%q saved=%#v", model.status, model.statusDetails, model.retryStatus)
	}
}

func TestFullscreenRetryRestoresStatusAndReplacesDraft(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	startedAt := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	applyFullscreenSessionEvent(t, &model, protocol.TurnStartedEvent{StartedAt: startedAt})
	applyFullscreenSessionEvent(t, &model, protocol.ItemStartedEvent{Item: protocol.TurnItem{ID: "assistant-1", Kind: protocol.ItemAssistantMessage, Status: protocol.ItemInProgress, CreatedAt: startedAt}})
	applyFullscreenSessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "partial"})
	applyFullscreenSessionEvent(t, &model, protocol.StreamErrorEvent{Message: "Reconnecting... 1/5", WillRetry: true})
	applyFullscreenSessionEvent(t, &model, protocol.StreamErrorEvent{Message: "Reconnecting... 2/5", WillRetry: true})

	if model.retryStatus.header != "working" {
		t.Fatalf("consecutive retry overwrote saved status: %#v", model.retryStatus)
	}
	applyFullscreenSessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Reset: true})
	applyFullscreenSessionEvent(t, &model, protocol.AgentMessageContentDeltaEvent{ItemID: "assistant-1", Delta: "recovered"})
	if model.status != "working" || model.statusDetails != "" || model.retryStatus.active || model.draft != "recovered" {
		t.Fatalf("recovery state: status=%q details=%q saved=%#v draft=%q", model.status, model.statusDetails, model.retryStatus, model.draft)
	}
}

func TestFullscreenRetryTerminalAndAttachClearTransientState(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.running = true
	model.status = "thinking"
	applyFullscreenSessionEvent(t, &model, protocol.StreamErrorEvent{Message: "Reconnecting... 1/5", WillRetry: true})
	applyFullscreenSessionEvent(t, &model, protocol.StreamErrorEvent{Message: "provider unavailable"})
	if model.retryStatus.active || !strings.Contains(lastCellContent(model), "provider unavailable") {
		t.Fatalf("terminal retry state=%#v history=%q", model.retryStatus, lastCellContent(model))
	}

	applyFullscreenSessionEvent(t, &model, protocol.StreamErrorEvent{Message: "Reconnecting... 1/5", WillRetry: true})
	updated, _ := model.Update(fullscreenAppEventMsg{event: application.ThreadAttached{Snapshot: application.ThreadViewSnapshot{Generation: 2, ThreadID: "thread-2", Model: "test-model"}}})
	model = updated.(fullscreenModel)
	if model.retryStatus.active || model.statusDetails != "" || model.status != "idle" || !model.input.Focused() {
		t.Fatalf("attach retained retry state: status=%q details=%q saved=%#v focused=%v", model.status, model.statusDetails, model.retryStatus, model.input.Focused())
	}
}

func TestFullscreenRetryWorkingLineHandlesHiddenNoColorAndNarrowLayout(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.running = false
	model.width = 24
	model.runStartedAt = time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	model.clock = fixedMotionClock{now: model.runStartedAt.Add(time.Second)}
	details := "idle timeout waiting for a very slow provider stream"
	applyFullscreenSessionEvent(t, &model, protocol.StreamErrorEvent{Message: "Reconnecting... 2/5", AdditionalDetails: &details, WillRetry: true})

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

func applyFullscreenSessionEvent(t *testing.T, model *fullscreenModel, message protocol.EventMsg) {
	t.Helper()
	updated, _ := model.Update(fullscreenAppEventMsg{event: application.SessionEventObserved{
		Generation: model.generation,
		Event:      testProtocolEvent(string(applicationThreadID(model.startup.Session)), "turn-1", message),
	}})
	*model = updated.(fullscreenModel)
}
