package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func TestTUIExitUsesShutdownThenFrameDrain(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.session.TokenInfo = &protocol.TokenUsageInfo{TotalTokenUsage: llm.TokenUsage{InputTokens: 7, OutputTokens: 3, TotalTokens: 10}}
	updated, command := model.dispatchCommand(SlashInvocation{Command: SlashExit})
	model = updated.(appModel)
	if command == nil || !model.exit.shuttingDown() {
		t.Fatal("/exit did not enter shutdown-first lifecycle")
	}
	if got := strings.Count(model.View(), "Shutting down…"); got != 1 {
		t.Fatalf("shutdown presentation count = %d, view=%q", got, model.View())
	}
	if strings.Contains(model.View(), inputPlaceholder) || strings.Contains(lastCellContent(model), "Shutting down") {
		t.Fatalf("shutdown leaked into composer/history: view=%q history=%q", model.View(), lastCellContent(model))
	}

	batch, ok := command().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("shutdown command = %#v", command())
	}
	finished := batch[0]()
	if fakeApplication(t, model).shutdowns != 1 {
		t.Fatalf("shutdown calls = %d", fakeApplication(t, model).shutdowns)
	}
	updated, drain := model.Update(finished)
	model = updated.(appModel)
	if drain == nil || !model.exit.drainingFrame() || model.View() != "" {
		t.Fatalf("shutdown completion did not clear active frame: phase=%d view=%q", model.exit.phase, model.View())
	}
	updated, quit := model.Update(drain())
	model = updated.(appModel)
	if quit == nil || model.exit.phase != exitPhaseFinished || model.View() != "" {
		t.Fatalf("frame drain did not reach quit: phase=%d view=%q", model.exit.phase, model.View())
	}
	info := model.appExitInfo()
	if info.ExitReason != ExitReasonUserRequested || info.TokenUsage.TotalTokens != 10 || info.ResumeHint != "amadeus --resume "+testThreadID(1).String() {
		t.Fatalf("exit info = %#v", info)
	}
}

func TestTUIExitIsIdempotentAndIgnoresInput(t *testing.T) {
	_, model := newTestModel(t, nil)
	updated, first := model.dispatchCommand(SlashInvocation{Command: SlashExit})
	model = updated.(appModel)
	updated, second := model.dispatchCommand(SlashInvocation{Command: SlashExit})
	model = updated.(appModel)
	if first == nil || second != nil {
		t.Fatalf("exit commands first=%v second=%v", first != nil, second != nil)
	}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ignored")})
	model = updated.(appModel)
	if command != nil || model.input.Value() != "" {
		t.Fatalf("shutdown accepted input value=%q command=%v", model.input.Value(), command != nil)
	}
}

func TestTUIExitRemainsAvailableDuringTask(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.running = true
	commands := FilterSlashCommands("/e", true)
	if len(commands) != 1 || commands[0] != SlashExit {
		t.Fatalf("running /e commands = %#v", commands)
	}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	model = updated.(appModel)
	if command == nil || !model.exit.shuttingDown() {
		t.Fatal("Ctrl+D did not request shutdown while a task was active")
	}
}

func TestTUIExitCapturesFinalUsageWithoutRenderingEvents(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.requestExit(ExitModeShutdownFirst, ExitReasonUserRequested, nil)
	event := application.SessionEventObserved{
		Generation: model.session.Generation,
		Event: protocol.Event{Msg: protocol.ScopeEventMsg(
			protocol.NewTokenCountEvent(llm.TokenUsage{InputTokens: 11, OutputTokens: 4, TotalTokens: 15}, 128_000, 1), model.session.ThreadID, "turn-1",
		)},
	}
	updated, command := model.Update(appEventMsg{event: event})
	model = updated.(appModel)
	if command != nil || model.session.totalTokenUsage().TotalTokens != 15 || len(model.historyCells) != 0 {
		t.Fatalf("exit event projection usage=%#v cells=%d command=%v", model.session.totalTokenUsage(), len(model.historyCells), command != nil)
	}
}

func TestTUIExitTimeoutDrainsWithWarning(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.requestExit(ExitModeShutdownFirst, ExitReasonUserRequested, nil)
	updated, drain := model.Update(shutdownTimeoutMsg{})
	model = updated.(appModel)
	if drain == nil || model.exit.reason != ExitReasonUserRequested || !errors.Is(model.exit.err, errShutdownTimedOut) {
		t.Fatalf("timeout exit = %#v", model.exit)
	}
}

func TestTUIFatalShutdownPreservesThreadDiagnostic(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.requestExit(ExitModeShutdownFirst, ExitReasonUserRequested, nil)
	shutdownErr := errors.New("flush failed")
	updated, drain := model.Update(shutdownFinishedMsg{err: shutdownErr})
	model = updated.(appModel)
	if drain == nil {
		t.Fatal("fatal shutdown did not start frame drain")
	}
	info := model.appExitInfo()
	if info.ExitReason != ExitReasonFatal || !errors.Is(info.Error, shutdownErr) || info.ThreadID != testThreadID(1) {
		t.Fatalf("fatal exit info = %#v", info)
	}
}

func TestTUIShutdownJoinWithTimeoutAndFailureIsFatal(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.requestExit(ExitModeShutdownFirst, ExitReasonUserRequested, nil)
	shutdownErr := errors.Join(context.DeadlineExceeded, errors.New("flush failed"))
	updated, _ := model.Update(shutdownFinishedMsg{err: shutdownErr})
	model = updated.(appModel)
	if model.exit.reason != ExitReasonFatal {
		t.Fatalf("joined shutdown reason = %d", model.exit.reason)
	}
}

func TestTUIDeletedThreadUsesImmediateDrainWithoutResumeHint(t *testing.T) {
	_, model := newTestModel(t, nil)
	updated, drain := model.Update(appEventMsg{event: application.ThreadDeleted{ThreadID: testThreadID(1)}})
	model = updated.(appModel)
	if drain == nil || !model.exit.drainingFrame() || model.appExitInfo().ResumeHint != "" {
		t.Fatalf("deleted exit phase=%d info=%#v", model.exit.phase, model.appExitInfo())
	}
}
