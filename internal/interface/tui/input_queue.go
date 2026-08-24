package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
)

var (
	errNextTurnQueueAttachmentChanged = errors.New("next-turn queue attachment changed")
	errNextTurnQueueModeChanged       = errors.New("next-turn queue collaboration mode changed")
)

const maxQueuedInputPreviewItems = 3

type QueuedUserInput struct {
	Content              string
	Mode                 turn.ModeKind
	ThreadID             protocol.ThreadID
	AttachmentGeneration uint64
}

func (input QueuedUserInput) Validate() error {
	if strings.TrimSpace(input.Content) == "" {
		return errors.New("queued user input is empty")
	}
	if !input.Mode.Valid() {
		return fmt.Errorf("queued user input mode %q is invalid", input.Mode)
	}
	if input.ThreadID.IsZero() {
		return errors.New("queued user input thread ID is empty")
	}
	if input.AttachmentGeneration == 0 {
		return errors.New("queued user input attachment generation is zero")
	}
	return nil
}

type NextTurnQueue struct {
	Pending  []QueuedUserInput
	InFlight *QueuedUserInput
	halted   bool
}

func (queue *NextTurnQueue) Enqueue(input QueuedUserInput) error {
	if queue == nil {
		return errors.New("next-turn queue is nil")
	}
	input.Content = strings.TrimSpace(input.Content)
	if err := input.Validate(); err != nil {
		return err
	}
	queue.Pending = append(queue.Pending, input)
	return nil
}

func (queue *NextTurnQueue) Begin(threadID protocol.ThreadID, generation uint64, mode turn.ModeKind) (QueuedUserInput, bool, error) {
	if queue == nil || queue.halted || queue.InFlight != nil || len(queue.Pending) == 0 {
		return QueuedUserInput{}, false, nil
	}
	next := queue.Pending[0]
	if next.ThreadID != threadID || next.AttachmentGeneration != generation {
		return QueuedUserInput{}, false, errNextTurnQueueAttachmentChanged
	}
	if next.Mode != mode {
		return QueuedUserInput{}, false, errNextTurnQueueModeChanged
	}
	queue.Pending = queue.Pending[1:]
	queue.InFlight = &next
	return next, true, nil
}

func (queue *NextTurnQueue) ConfirmStarted(threadID protocol.ThreadID, generation uint64) bool {
	if queue == nil || queue.InFlight == nil || queue.InFlight.ThreadID != threadID || queue.InFlight.AttachmentGeneration != generation {
		return false
	}
	queue.InFlight = nil
	return true
}

func (queue *NextTurnQueue) RejectInFlight(task TaskSubmission) (QueuedUserInput, bool) {
	if queue == nil || queue.InFlight == nil || !queuedTaskMatches(*queue.InFlight, task) {
		return QueuedUserInput{}, false
	}
	input := *queue.InFlight
	queue.InFlight = nil
	return input, true
}

func (queue *NextTurnQueue) AcceptUnexpectedSteer(task TaskSubmission) bool {
	if queue == nil || queue.InFlight == nil || !queuedTaskMatches(*queue.InFlight, task) {
		return false
	}
	queue.InFlight = nil
	queue.halted = true
	return true
}

func queuedTaskMatches(input QueuedUserInput, task TaskSubmission) bool {
	return task.FromNextTurnQueue && input.Content == task.Content && input.Mode == task.Mode &&
		input.ThreadID == task.OriginThreadID && input.AttachmentGeneration == task.OriginGeneration
}

func (queue *NextTurnQueue) HasPending() bool {
	return queue != nil && len(queue.Pending) > 0
}

func (queue *NextTurnQueue) HasQueuedFollowUp() bool {
	return queue != nil && (len(queue.Pending) > 0 || queue.InFlight != nil)
}

func (queue *NextTurnQueue) StartPending() bool {
	return queue != nil && queue.InFlight != nil
}

func (queue *NextTurnQueue) Halted() bool {
	return queue != nil && queue.halted
}

func (queue *NextTurnQueue) DrainForRestore() []QueuedUserInput {
	if queue == nil {
		return nil
	}
	result := make([]QueuedUserInput, 0, len(queue.Pending)+1)
	if queue.InFlight != nil {
		result = append(result, *queue.InFlight)
	}
	result = append(result, queue.Pending...)
	queue.Clear()
	return result
}

func (queue *NextTurnQueue) Clear() {
	if queue == nil {
		return
	}
	queue.Pending = nil
	queue.InFlight = nil
	queue.halted = false
}

func (model fullscreenModel) queueComposerInput() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(model.input.Value())
	if text == "" {
		return model, nil
	}
	input, err := ParseInput(text)
	if err != nil || input.Command != nil {
		return model, nil
	}
	input.Queue = true
	return model.enqueueInputResult(input)
}

func (model fullscreenModel) hasQueueableDraft() bool {
	if !model.running || model.selection != nil || model.approvalDialog != nil || model.userInputDialog != nil || model.slashPopup.active() {
		return false
	}
	text := strings.TrimSpace(model.input.Value())
	if text == "" {
		return false
	}
	input, err := ParseInput(text)
	return err == nil && input.Command == nil && strings.TrimSpace(input.Text) != ""
}

func (model fullscreenModel) enqueueInputResult(input InputResult) (tea.Model, tea.Cmd) {
	if !input.Queue || strings.TrimSpace(input.Text) == "" || input.Command != nil {
		return model, nil
	}
	err := model.nextTurnQueue.Enqueue(QueuedUserInput{
		Content: strings.TrimSpace(input.Text), Mode: model.session.mode(), ThreadID: model.session.ThreadID,
		AttachmentGeneration: model.session.Generation,
	})
	if err != nil {
		model.insertHistoryCell(NewDiagnosticHistoryCell("queue input: " + err.Error()))
		return model, model.flushHistory()
	}
	model.input.Reset()
	model.slashPopup.dismiss("")
	model.updateInputLayout()
	model.history = append(model.history, strings.TrimSpace(input.Text))
	model.historyPos = -1
	return model, nil
}

func (model *fullscreenModel) maybeSubmitNextQueuedInput() tea.Cmd {
	if model == nil {
		return nil
	}
	queued, ok, err := model.nextTurnQueue.Begin(model.session.ThreadID, model.session.Generation, model.session.mode())
	if err != nil {
		if errors.Is(err, errNextTurnQueueModeChanged) {
			model.restoreQueuedInputsToComposer()
			model.insertHistoryCell(NewErrorHistoryCell("queued input: collaboration mode changed before submission"))
		} else {
			model.nextTurnQueue.Clear()
			model.insertHistoryCell(NewDiagnosticHistoryCell("queued input: active thread changed before submission"))
		}
		return nil
	}
	if !ok {
		return nil
	}
	submission := model.prepareTaskSubmission(queued.Content, queued.Mode, false)
	submission.FromNextTurnQueue = true
	submission.OriginThreadID = queued.ThreadID
	submission.OriginGeneration = queued.AttachmentGeneration
	return model.submitTask(submission)
}

func (model *fullscreenModel) restoreQueuedInputsToComposer() {
	if model == nil {
		return
	}
	queued := model.nextTurnQueue.DrainForRestore()
	if len(queued) == 0 {
		return
	}
	parts := make([]string, 0, len(queued)+1)
	for _, input := range queued {
		if content := strings.TrimSpace(input.Content); content != "" {
			parts = append(parts, content)
		}
	}
	if current := strings.TrimSpace(model.input.Value()); current != "" {
		parts = append(parts, current)
	}
	model.input.SetValue(strings.Join(parts, "\n"))
	model.input.CursorEnd()
	model.updateInputLayout()
}

func (model *fullscreenModel) restoreRejectedQueuedInput(task TaskSubmission) bool {
	if model == nil || !task.FromNextTurnQueue || task.OriginThreadID != model.session.ThreadID || task.OriginGeneration != model.session.Generation {
		return false
	}
	queued, ok := model.nextTurnQueue.RejectInFlight(task)
	if !ok {
		return false
	}
	parts := []string{strings.TrimSpace(queued.Content)}
	if current := strings.TrimSpace(model.input.Value()); current != "" {
		parts = append(parts, current)
	}
	model.input.SetValue(strings.Join(parts, "\n"))
	model.input.CursorEnd()
	model.updateInputLayout()
	return true
}

func (model fullscreenModel) queuedInputPreview() string {
	count := len(model.nextTurnQueue.Pending)
	if count == 0 {
		return ""
	}
	width := maxInt(12, model.width-4)
	lines := []string{model.palette.dim().Render(fmt.Sprintf("Queued (%d)", count))}
	limit := minInt(count, maxQueuedInputPreviewItems)
	for index := 0; index < limit; index++ {
		content := strings.Join(strings.Fields(model.nextTurnQueue.Pending[index].Content), " ")
		prefix := fmt.Sprintf("  %d. ", index+1)
		content = xansi.Truncate(content, maxInt(1, width-len(prefix)), "...")
		lines = append(lines, model.palette.dim().Render(prefix+content))
	}
	if count > limit {
		lines = append(lines, model.palette.dim().Render(fmt.Sprintf("  +%d more", count-limit)))
	}
	return strings.Join(lines, "\n")
}
