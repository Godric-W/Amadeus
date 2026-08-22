package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/llm"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFullscreenExitUsesShutdownThenFrameDrain(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.session.Usage = llm.Usage{InputTokens: 7, OutputTokens: 3, TotalTokens: 10}
	updated, command := model.dispatchCommand(SlashInvocation{Command: SlashExit})
	model = updated.(fullscreenModel)
	if command == nil || !model.exit.shuttingDown() {
		t.Fatal("/exit did not enter shutdown-first lifecycle")
	}
	if got := strings.Count(model.View(), "Shutting down…"); got != 1 {
		t.Fatalf("shutdown presentation count = %d, view=%q", got, model.View())
	}
	if strings.Contains(model.View(), fullscreenInputPlaceholder) || strings.Contains(lastCellContent(model), "Shutting down") {
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
	model = updated.(fullscreenModel)
	if drain == nil || !model.exit.drainingFrame() || model.View() != "" {
		t.Fatalf("shutdown completion did not clear active frame: phase=%d view=%q", model.exit.phase, model.View())
	}
	updated, quit := model.Update(drain())
	model = updated.(fullscreenModel)
	if quit == nil || model.exit.phase != fullscreenExitPhaseFinished || model.View() != "" {
		t.Fatalf("frame drain did not reach quit: phase=%d view=%q", model.exit.phase, model.View())
	}
	info := model.appExitInfo()
	if info.ExitReason != ExitReasonUserRequested || info.TokenUsage.TotalTokens != 10 || info.ResumeHint != "amadeus --resume thread-1" {
		t.Fatalf("exit info = %#v", info)
	}
}

func TestFullscreenExitIsIdempotentAndIgnoresInput(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	updated, first := model.dispatchCommand(SlashInvocation{Command: SlashExit})
	model = updated.(fullscreenModel)
	updated, second := model.dispatchCommand(SlashInvocation{Command: SlashExit})
	model = updated.(fullscreenModel)
	if first == nil || second != nil {
		t.Fatalf("exit commands first=%v second=%v", first != nil, second != nil)
	}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ignored")})
	model = updated.(fullscreenModel)
	if command != nil || model.input.Value() != "" {
		t.Fatalf("shutdown accepted input value=%q command=%v", model.input.Value(), command != nil)
	}
}

func TestFullscreenExitRemainsAvailableDuringTask(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.running = true
	commands := FilterSlashCommands("/e", true)
	if len(commands) != 1 || commands[0] != SlashExit {
		t.Fatalf("running /e commands = %#v", commands)
	}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	model = updated.(fullscreenModel)
	if command == nil || !model.exit.shuttingDown() {
		t.Fatal("Ctrl+D did not request shutdown while a task was active")
	}
}

func TestFullscreenExitCapturesFinalUsageWithoutRenderingEvents(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.requestExit(ExitModeShutdownFirst, ExitReasonUserRequested, nil)
	event := application.SessionEventObserved{
		Generation: model.session.Generation,
		Event: protocol.Event{Msg: protocol.TokenCountEvent{
			ThreadID: model.session.ThreadID,
			Usage:    llm.Usage{InputTokens: 11, OutputTokens: 4, TotalTokens: 15},
		}},
	}
	updated, command := model.Update(fullscreenAppEventMsg{event: event})
	model = updated.(fullscreenModel)
	if command != nil || model.session.Usage.TotalTokens != 15 || len(model.historyCells) != 0 {
		t.Fatalf("exit event projection usage=%#v cells=%d command=%v", model.session.Usage, len(model.historyCells), command != nil)
	}
}

func TestFullscreenExitTimeoutDrainsWithWarning(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.requestExit(ExitModeShutdownFirst, ExitReasonUserRequested, nil)
	updated, drain := model.Update(fullscreenShutdownTimeoutMsg{})
	model = updated.(fullscreenModel)
	if drain == nil || model.exit.reason != ExitReasonUserRequested || !errors.Is(model.exit.err, errFullscreenShutdownTimedOut) {
		t.Fatalf("timeout exit = %#v", model.exit)
	}
}

func TestFullscreenFatalShutdownPreservesThreadDiagnostic(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.requestExit(ExitModeShutdownFirst, ExitReasonUserRequested, nil)
	shutdownErr := errors.New("flush failed")
	updated, drain := model.Update(fullscreenShutdownFinishedMsg{err: shutdownErr})
	model = updated.(fullscreenModel)
	if drain == nil {
		t.Fatal("fatal shutdown did not start frame drain")
	}
	info := model.appExitInfo()
	if info.ExitReason != ExitReasonFatal || !errors.Is(info.Error, shutdownErr) || info.ThreadID != "thread-1" {
		t.Fatalf("fatal exit info = %#v", info)
	}
}

func TestFullscreenShutdownJoinWithTimeoutAndFailureIsFatal(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.requestExit(ExitModeShutdownFirst, ExitReasonUserRequested, nil)
	shutdownErr := errors.Join(context.DeadlineExceeded, errors.New("flush failed"))
	updated, _ := model.Update(fullscreenShutdownFinishedMsg{err: shutdownErr})
	model = updated.(fullscreenModel)
	if model.exit.reason != ExitReasonFatal {
		t.Fatalf("joined shutdown reason = %d", model.exit.reason)
	}
}

func TestFullscreenDeletedThreadUsesImmediateDrainWithoutResumeHint(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	updated, drain := model.Update(fullscreenAppEventMsg{event: application.ThreadDeleted{ThreadID: "thread-1"}})
	model = updated.(fullscreenModel)
	if drain == nil || !model.exit.drainingFrame() || model.appExitInfo().ResumeHint != "" {
		t.Fatalf("deleted exit phase=%d info=%#v", model.exit.phase, model.appExitInfo())
	}
}
