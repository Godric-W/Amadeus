package tui

import (
	"strings"
	"testing"
	"time"

	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/testutil"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestTUIRetryPreservesDraftAndDoesNotCreateHistory(t *testing.T) {
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

	if model.draft != "partial" || len(model.historyCells) != historyCount {
		t.Fatalf("retry mutated transcript: draft=%q history=%d want=%d", model.draft, len(model.historyCells), historyCount)
	}
	if model.status != "Reconnecting... 1/5" || model.statusDetails != details || !model.retryStatus.active {
		t.Fatalf("retry status = %q details=%q saved=%#v", model.status, model.statusDetails, model.retryStatus)
	}
}

func TestTUIRetryRestoresStatusAndReplacesDraft(t *testing.T) {
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
	if model.status != "working" || model.statusDetails != "" || model.retryStatus.active || model.draft != "recovered" {
		t.Fatalf("recovery state: status=%q details=%q saved=%#v draft=%q", model.status, model.statusDetails, model.retryStatus, model.draft)
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
