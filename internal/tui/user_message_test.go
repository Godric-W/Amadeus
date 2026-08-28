package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/protocol"
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

func TestTUIInitialUserMessageSubmitsOnceThroughNormalPath(t *testing.T) {
	_, model := newTestModel(t, func(options *ApplicationOptions) {
		options.InitialUserMessage = &UserMessage{Text: "inspect the repository"}
	})

	updated, command := model.Update(startupReadyMsg{})
	model = updated.(appModel)
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
	updated, command = model.Update(startupReadyMsg{})
	if command != nil || len(fakeApplication(t, updated.(appModel)).submitted) != 1 {
		t.Fatal("repeated startup readiness resubmitted the initial message")
	}
}

func TestTUIInitialUserMessageFollowsReplayedHistory(t *testing.T) {
	now := time.Now().UTC()
	_, model := newTestModel(t, func(options *ApplicationOptions) {
		options.InitialUserMessage = &UserMessage{Text: "continue here"}
		options.Snapshot.Items = []protocol.TurnItem{{
			ID: "old-user", Kind: protocol.ItemUserMessage, Status: protocol.ItemStatusCompleted,
			CreatedAt: now, CompletedAt: now, Text: "earlier prompt",
		}}
	})
	updated, command := model.Update(startupReadyMsg{})
	model = updated.(appModel)
	if command == nil || len(model.historyCells) != 2 || cellContent(model.historyCells[0]) != "earlier prompt" || cellContent(model.historyCells[1]) != "continue here" {
		t.Fatalf("replay/initial order = %#v", model.historyCells)
	}
}

func TestTUIInitialSlashTextIsLiteralUserMessage(t *testing.T) {
	_, model := newTestModel(t, func(options *ApplicationOptions) {
		options.InitialUserMessage = &UserMessage{Text: "/compact"}
	})
	updated, command := model.Update(startupReadyMsg{})
	model = updated.(appModel)
	executeMessage(t, command())
	if fake := fakeApplication(t, model); len(fake.submitted) != 1 || fake.submitted[0] != "/compact" || fake.compactCount != 0 {
		t.Fatalf("literal initial prompt was dispatched as command: %#v", fake)
	}
}

func TestTUIRejectedInitialUserMessageRestoresComposer(t *testing.T) {
	_, model := newTestModel(t, func(options *ApplicationOptions) {
		options.InitialUserMessage = &UserMessage{Text: "retry me"}
	})
	fakeApplication(t, model).submitErr = errors.New("submission rejected")
	updated, command := model.Update(startupReadyMsg{})
	model = updated.(appModel)
	message := commandMessageOfType[userMessageRejectedMsg](t, command)
	updated, _ = model.Update(message)
	model = updated.(appModel)
	if model.input.Value() != "retry me" || model.initialUserMessage != nil || !strings.Contains(lastCellContent(model), "submission rejected") {
		t.Fatalf("rejected initial state: input=%q pending=%#v cell=%q", model.input.Value(), model.initialUserMessage, lastCellContent(model))
	}
}

func TestTUIPickerRejectsInitialUserMessage(t *testing.T) {
	fake := newFakeApplicationPort()
	_, err := NewApplication(ApplicationOptions{
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
	if typed, ok := findCommandMessage[T](command()); ok {
		return typed
	}
	t.Fatalf("command has no %T", zero)
	return zero
}

func findCommandMessage[T tea.Msg](message tea.Msg) (T, bool) {
	var zero T
	if typed, ok := message.(T); ok {
		return typed, true
	}
	for _, child := range teaCommandChildren(message) {
		if child == nil {
			continue
		}
		if typed, ok := findCommandMessage[T](child()); ok {
			return typed, true
		}
	}
	return zero, false
}
