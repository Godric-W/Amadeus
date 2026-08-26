package tui

import (
	"errors"

	"github.com/Godric-W/Amadeus/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

// UserMessage is user-authored TUI intent before it crosses the Runtime
// submission boundary. Pending initial and queued messages use this type.
type UserMessage struct {
	Text string
}

func (message UserMessage) Validate() error {
	if message.Text == "" {
		return errors.New("user message is empty")
	}
	return nil
}

func createInitialUserMessage(prompt string) *UserMessage {
	if prompt == "" {
		return nil
	}
	return &UserMessage{Text: prompt}
}

func cloneUserMessage(message *UserMessage) *UserMessage {
	if message == nil {
		return nil
	}
	cloned := *message
	return &cloned
}

type UserMessageSubmission struct {
	Message             UserMessage
	ClientUserMessageID string
	Mode                protocol.ModeKind
	OverrideMode        bool
	FromNextTurnQueue   bool
	OriginThreadID      protocol.ThreadID
	OriginGeneration    uint64
}

func (model *appModel) recordUserMessageHistory(message UserMessage) {
	if model == nil || message.Text == "" {
		return
	}
	model.history = append(model.history, message.Text)
	model.historyPos = -1
}

func (model *appModel) submitInitialUserMessageIfPending() tea.Cmd {
	if model == nil || model.initialUserMessage == nil || model.running || model.selection != nil || model.approvalDialog != nil || model.userInputDialog != nil {
		return nil
	}
	message := *model.initialUserMessage
	if err := message.Validate(); err != nil {
		model.initialUserMessage = nil
		model.insertHistoryCell(NewErrorHistoryCell("initial user message: " + err.Error()))
		return model.flushHistory()
	}
	if model.session.ThreadID.IsZero() || model.session.Generation == 0 {
		return nil
	}
	model.initialUserMessage = nil
	model.recordUserMessageHistory(message)
	submission := model.prepareUserMessageSubmission(message, model.session.mode(), false)
	return tea.Batch(model.flushHistory(), model.submitUserMessage(submission))
}
