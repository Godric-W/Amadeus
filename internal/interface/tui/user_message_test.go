package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	application "github.com/Godric-W/Amadeus/internal/app"
	tea "github.com/charmbracelet/bubbletea"
)

func TestCreateInitialUserMessageUsesExactEmptySemantics(t *testing.T) {
	if message := createInitialUserMessage(""); message != nil {
		t.Fatalf("empty prompt created message: %#v", message)
	}
	message := createInitialUserMessage("  keep spacing  ")
	if message == nil || message.Text != "  keep spacing  " {
		t.Fatalf("non-empty prompt = %#v", message)
	}
}

func TestFullscreenInitialUserMessageSubmitsOnceThroughNormalPath(t *testing.T) {
	_, model := newTestFullscreen(t, func(options *FullscreenOptions) {
		options.InitialUserMessage = &UserMessage{Text: "inspect the repository"}
	})

	updated, command := model.Update(fullscreenStartupReadyMsg{})
	model = updated.(fullscreenModel)
	if model.initialUserMessage != nil || command == nil {
		t.Fatalf("initial state after ready: pending=%#v command=%v", model.initialUserMessage, command)
	}
	executeMessage(t, command())
	if submitted := fakeApplication(t, model).submitted; len(submitted) != 1 || submitted[0] != "inspect the repository" {
		t.Fatalf("initial submissions = %#v", submitted)
	}
	if len(model.history) != 1 || model.history[0] != "inspect the repository" || lastCellContent(model) != "inspect the repository" {
		t.Fatalf("initial optimistic state: history=%#v cell=%q", model.history, lastCellContent(model))
	}
	updated, command = model.Update(fullscreenStartupReadyMsg{})
	if command != nil || len(fakeApplication(t, updated.(fullscreenModel)).submitted) != 1 {
		t.Fatal("repeated startup readiness resubmitted the initial message")
	}
}

func TestFullscreenInitialUserMessageFollowsReplayedHistory(t *testing.T) {
	now := time.Now().UTC()
	_, model := newTestFullscreen(t, func(options *FullscreenOptions) {
		options.InitialUserMessage = &UserMessage{Text: "continue here"}
		options.Snapshot.Items = []protocol.TurnItem{{
			ID: "old-user", Kind: protocol.ItemUserMessage, Status: protocol.ItemStatusCompleted,
			CreatedAt: now, CompletedAt: now, Text: "earlier prompt",
		}}
	})
	updated, command := model.Update(fullscreenStartupReadyMsg{})
	model = updated.(fullscreenModel)
	if command == nil || len(model.historyCells) != 2 || cellContent(model.historyCells[0]) != "earlier prompt" || cellContent(model.historyCells[1]) != "continue here" {
		t.Fatalf("replay/initial order = %#v", model.historyCells)
	}
}

func TestFullscreenInitialSlashTextIsLiteralUserMessage(t *testing.T) {
	_, model := newTestFullscreen(t, func(options *FullscreenOptions) {
		options.InitialUserMessage = &UserMessage{Text: "/compact"}
	})
	updated, command := model.Update(fullscreenStartupReadyMsg{})
	model = updated.(fullscreenModel)
	executeMessage(t, command())
	if fake := fakeApplication(t, model); len(fake.submitted) != 1 || fake.submitted[0] != "/compact" || fake.compactCount != 0 {
		t.Fatalf("literal initial prompt was dispatched as command: %#v", fake)
	}
}

func TestFullscreenRejectedInitialUserMessageRestoresComposer(t *testing.T) {
	_, model := newTestFullscreen(t, func(options *FullscreenOptions) {
		options.InitialUserMessage = &UserMessage{Text: "retry me"}
	})
	fakeApplication(t, model).submitErr = errors.New("submission rejected")
	updated, command := model.Update(fullscreenStartupReadyMsg{})
	model = updated.(fullscreenModel)
	message := commandMessageOfType[fullscreenUserMessageRejectedMsg](t, command)
	updated, _ = model.Update(message)
	model = updated.(fullscreenModel)
	if model.input.Value() != "retry me" || model.initialUserMessage != nil || !strings.Contains(lastCellContent(model), "submission rejected") {
		t.Fatalf("rejected initial state: input=%q pending=%#v cell=%q", model.input.Value(), model.initialUserMessage, lastCellContent(model))
	}
}

func TestFullscreenPickerRejectsInitialUserMessage(t *testing.T) {
	fake := newFakeFullscreenApplication()
	_, err := NewFullscreenApplication(FullscreenOptions{
		Input: strings.NewReader(""), Output: &strings.Builder{}, Application: fake,
		OpenSessions: true, InitialUserMessage: &UserMessage{Text: "prompt"},
		Snapshot: application.ThreadViewSnapshot{},
	})
	if err == nil || !strings.Contains(err.Error(), "picker") {
		t.Fatalf("picker + initial message error = %v", err)
	}
}

func commandMessageOfType[T tea.Msg](t *testing.T, command tea.Cmd) T {
	t.Helper()
	var zero T
	if command == nil {
		t.Fatal("command is nil")
	}
	message := command()
	if typed, ok := message.(T); ok {
		return typed
	}
	batch, ok := message.(tea.BatchMsg)
	if !ok {
		t.Fatalf("command message = %T, want %T", message, zero)
	}
	for _, child := range batch {
		if child == nil {
			continue
		}
		if typed, ok := child().(T); ok {
			return typed
		}
	}
	t.Fatalf("batch has no %T", zero)
	return zero
}
