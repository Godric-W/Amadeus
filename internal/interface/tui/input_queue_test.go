package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/agent/turn"
	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
)

func queuedInput(content string, mode turn.ModeKind, threadID protocol.ThreadID, generation uint64) QueuedUserMessage {
	return QueuedUserMessage{Message: UserMessage{Text: content}, Mode: mode, ThreadID: threadID, AttachmentGeneration: generation}
}

func TestNextTurnQueueFIFOAndStartHandshake(t *testing.T) {
	threadID := testThreadID(1)
	queue := NextTurnQueue{}
	for _, content := range []string{"second", "third"} {
		if err := queue.Enqueue(queuedInput(content, turn.ModeKindDefault, threadID, 1)); err != nil {
			t.Fatal(err)
		}
	}

	first, ok, err := queue.Begin(threadID, 1, turn.ModeKindDefault)
	if err != nil || !ok || first.Message.Text != "second" {
		t.Fatalf("first begin = %#v, %v, %v", first, ok, err)
	}
	if _, ok, err := queue.Begin(threadID, 1, turn.ModeKindDefault); err != nil || ok {
		t.Fatalf("second begin while in flight = %v, %v", ok, err)
	}
	if !queue.ConfirmStarted(threadID, 1) {
		t.Fatal("matching TurnStarted did not clear InFlight")
	}
	second, ok, err := queue.Begin(threadID, 1, turn.ModeKindDefault)
	if err != nil || !ok || second.Message.Text != "third" {
		t.Fatalf("second begin = %#v, %v, %v", second, ok, err)
	}
}

func TestNextTurnQueueRejectsStaleAttachmentAndMode(t *testing.T) {
	threadID := testThreadID(1)
	queue := NextTurnQueue{}
	if err := queue.Enqueue(queuedInput("next", turn.ModeKindPlan, threadID, 2)); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := queue.Begin(testThreadID(2), 2, turn.ModeKindPlan); ok || !errors.Is(err, errNextTurnQueueAttachmentChanged) {
		t.Fatalf("attachment begin = ok:%v err:%v", ok, err)
	}
	if _, ok, err := queue.Begin(threadID, 2, turn.ModeKindDefault); ok || !errors.Is(err, errNextTurnQueueModeChanged) {
		t.Fatalf("mode begin = ok:%v err:%v", ok, err)
	}
	if len(queue.Pending) != 1 || queue.InFlight != nil {
		t.Fatalf("failed begin mutated queue: %#v", queue)
	}
}

func TestNextTurnQueueRejectsIncompleteInput(t *testing.T) {
	threadID := testThreadID(1)
	for _, input := range []QueuedUserMessage{
		queuedInput("", turn.ModeKindDefault, threadID, 1),
		queuedInput("content", "invalid", threadID, 1),
		queuedInput("content", turn.ModeKindDefault, protocol.ThreadID{}, 1),
		queuedInput("content", turn.ModeKindDefault, threadID, 0),
	} {
		queue := NextTurnQueue{}
		if err := queue.Enqueue(input); err == nil || queue.HasQueuedFollowUp() {
			t.Fatalf("incomplete input was queued: %#v err=%v", input, err)
		}
	}
}

func TestFullscreenTabQueuesWithoutRuntimeSubmission(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.running = true
	model.status = "thinking"
	model.input.SetValue("inspect the next issue")

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(fullscreenModel)
	if command != nil {
		t.Fatal("Tab queue returned a Runtime command")
	}
	if submitted := fakeApplication(t, model).submitted; len(submitted) != 0 {
		t.Fatalf("Tab queue submitted to Runtime: %v", submitted)
	}
	if len(model.nextTurnQueue.Pending) != 1 || model.nextTurnQueue.Pending[0].Message.Text != "inspect the next issue" {
		t.Fatalf("queue = %#v", model.nextTurnQueue)
	}
	if model.input.Value() != "" || len(model.historyCells) != 0 || len(model.optimisticUserMessages) != 0 {
		t.Fatalf("enqueue leaked into composer/history: input=%q history=%d optimistic=%v", model.input.Value(), len(model.historyCells), model.optimisticUserMessages)
	}
	if len(model.history) != 1 || model.history[0] != "inspect the next issue" {
		t.Fatalf("enqueue input recall = %v", model.history)
	}
	if preview := xansi.Strip(model.composerAuxiliaryView()); !strings.Contains(preview, "Queued (1)") || !strings.Contains(preview, "inspect the next issue") {
		t.Fatalf("queued preview = %q", preview)
	}
}

func TestFullscreenEmptyTabDoesNotQueue(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.running = true
	model.input.SetValue("   ")
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(fullscreenModel)
	if command != nil || model.nextTurnQueue.HasQueuedFollowUp() || len(fakeApplication(t, model).submitted) != 0 {
		t.Fatalf("empty Tab changed queue: command=%v queue=%#v submitted=%v", command != nil, model.nextTurnQueue, fakeApplication(t, model).submitted)
	}
}

func TestFullscreenTabKeepsSlashCompletionPriority(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.running = true
	model.input.SetValue("/res")

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(fullscreenModel)
	if command != nil || model.input.Value() != "/resume " || model.nextTurnQueue.HasQueuedFollowUp() {
		t.Fatalf("slash Tab result: command=%v input=%q queue=%#v", command != nil, model.input.Value(), model.nextTurnQueue)
	}
}

func TestFullscreenQueuedInputsDrainOnePerTurn(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.running = true
	for _, content := range []string{"second", "third"} {
		model.input.SetValue(content)
		updated, command := model.Update(tea.KeyMsg{Type: tea.KeyTab})
		model = updated.(fullscreenModel)
		if command != nil {
			t.Fatal("enqueue returned command")
		}
	}

	command := model.applyEvent(testProtocolEvent(model.session.ThreadID, "turn-1", protocol.TurnCompleteEvent{
		Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeCompleted, FinishedAt: time.Now().UTC(),
	}))
	if command == nil || model.nextTurnQueue.InFlight == nil || len(model.nextTurnQueue.Pending) != 1 {
		t.Fatalf("first drain state: command=%v queue=%#v", command != nil, model.nextTurnQueue)
	}
	if duplicate := model.applyEvent(testProtocolEvent(model.session.ThreadID, "turn-1", protocol.TurnCompleteEvent{
		Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeCompleted, FinishedAt: time.Now().UTC(),
	})); duplicate != nil {
		t.Fatal("duplicate terminal submitted a second queued input")
	}
	message := command()
	admitted, ok := message.(fullscreenUserMessageAdmittedMsg)
	if !ok {
		t.Fatalf("first drain message = %T", message)
	}
	model.handleUserMessageAdmission(admitted)
	if got := fakeApplication(t, model).submitted; len(got) != 1 || got[0] != "second" {
		t.Fatalf("first submissions = %v", got)
	}
	if !model.nextTurnQueue.StartPending() {
		t.Fatal("Started admission cleared queue before TurnStarted")
	}

	model.applyEvent(testProtocolEvent(model.session.ThreadID, "turn-2", protocol.TurnStartedEvent{StartedAt: time.Now().UTC()}))
	if model.nextTurnQueue.StartPending() {
		t.Fatal("TurnStarted did not clear start-pending gate")
	}
	command = model.applyEvent(testProtocolEvent(model.session.ThreadID, "turn-2", protocol.TurnCompleteEvent{
		Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeCompleted, FinishedAt: time.Now().UTC(),
	}))
	if command == nil {
		t.Fatal("second terminal did not drain third input")
	}
	if _, ok := command().(fullscreenUserMessageAdmittedMsg); !ok {
		t.Fatal("second drain did not submit user input")
	}
	if got := fakeApplication(t, model).submitted; len(got) != 2 || got[1] != "third" {
		t.Fatalf("all submissions = %v", got)
	}
}

func TestFullscreenEnterQueuesBehindPendingStart(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	if err := model.nextTurnQueue.Enqueue(queuedInput("starting", turn.ModeKindDefault, model.session.ThreadID, model.session.Generation)); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := model.nextTurnQueue.Begin(model.session.ThreadID, model.session.Generation, model.session.mode()); err != nil || !ok {
		t.Fatalf("begin queued start = %v, %v", ok, err)
	}
	model.input.SetValue("after pending start")

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(fullscreenModel)
	if command != nil || len(fakeApplication(t, model).submitted) != 0 {
		t.Fatal("Enter raced the queued start with another Runtime submission")
	}
	if len(model.nextTurnQueue.Pending) != 1 || model.nextTurnQueue.Pending[0].Message.Text != "after pending start" {
		t.Fatalf("pending-start queue = %#v", model.nextTurnQueue)
	}
}

func TestFullscreenErrorWaitsForFailedTerminalBeforeQueueDrain(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.running = true
	if err := model.nextTurnQueue.Enqueue(queuedInput("after failure", turn.ModeKindDefault, model.session.ThreadID, model.session.Generation)); err != nil {
		t.Fatal(err)
	}
	if command := model.applyEvent(testProtocolEvent(model.session.ThreadID, "turn-1", protocol.ErrorEvent{Code: "turn_failed", Message: "failed", At: time.Now().UTC()})); command != nil {
		t.Fatal("ErrorEvent drained next-turn queue before terminal")
	}
	if !model.running || len(fakeApplication(t, model).submitted) != 0 || len(model.nextTurnQueue.Pending) != 1 {
		t.Fatalf("ErrorEvent state: running=%v submitted=%v queue=%#v", model.running, fakeApplication(t, model).submitted, model.nextTurnQueue)
	}
	command := model.applyEvent(testProtocolEvent(model.session.ThreadID, "turn-1", protocol.TurnCompleteEvent{
		Status: protocol.TurnStatusFailed, Outcome: protocol.TurnOutcomeFailed, Error: "failed", FinishedAt: time.Now().UTC(),
	}))
	if command == nil {
		t.Fatal("failed terminal did not drain next-turn queue")
	}
	if _, ok := command().(fullscreenUserMessageAdmittedMsg); !ok {
		t.Fatal("failed terminal queue drain did not submit user input")
	}
}

func TestFullscreenBlockedAndAbortedTurnsRestoreQueuedInputs(t *testing.T) {
	for _, test := range []struct {
		name    string
		message protocol.EventMsg
	}{
		{name: "blocked", message: protocol.TurnCompleteEvent{Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeBlocked, FinishedAt: time.Now().UTC()}},
		{name: "aborted", message: protocol.TurnAbortedEvent{Reason: "interrupted", FinishedAt: time.Now().UTC()}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, model := newTestFullscreen(t, nil)
			model.running = true
			for _, content := range []string{"second", "third"} {
				if err := model.nextTurnQueue.Enqueue(queuedInput(content, turn.ModeKindDefault, model.session.ThreadID, model.session.Generation)); err != nil {
					t.Fatal(err)
				}
			}
			if command := model.applyEvent(testProtocolEvent(model.session.ThreadID, "turn-1", test.message)); command != nil {
				t.Fatal("restore terminal returned a submission command")
			}
			if model.input.Value() != "second\nthird" || model.nextTurnQueue.HasQueuedFollowUp() {
				t.Fatalf("restored input=%q queue=%#v", model.input.Value(), model.nextTurnQueue)
			}
			if submitted := fakeApplication(t, model).submitted; len(submitted) != 0 {
				t.Fatalf("restore terminal submitted %v", submitted)
			}
		})
	}
}

func TestFullscreenQueuedSubmissionFailureRestoresHeadAndKeepsTail(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	fake := fakeApplication(t, model)
	fake.submitErr = errors.New("provider unavailable")
	model.running = true
	for _, content := range []string{"second", "third"} {
		if err := model.nextTurnQueue.Enqueue(queuedInput(content, turn.ModeKindDefault, model.session.ThreadID, model.session.Generation)); err != nil {
			t.Fatal(err)
		}
	}
	command := model.applyEvent(testProtocolEvent(model.session.ThreadID, "turn-1", protocol.TurnCompleteEvent{
		Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeCompleted, FinishedAt: time.Now().UTC(),
	}))
	message, ok := command().(fullscreenUserMessageRejectedMsg)
	if !ok {
		t.Fatalf("submission result = %T", command())
	}
	model.handleUserMessageRejection(message)
	if model.input.Value() != "second" || len(model.nextTurnQueue.Pending) != 1 || model.nextTurnQueue.Pending[0].Message.Text != "third" {
		t.Fatalf("rejection input=%q queue=%#v", model.input.Value(), model.nextTurnQueue)
	}
}

func TestFullscreenMalformedQueuedAdmissionRestoresHead(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.running = true
	if err := model.nextTurnQueue.Enqueue(queuedInput("retry malformed", turn.ModeKindDefault, model.session.ThreadID, model.session.Generation)); err != nil {
		t.Fatal(err)
	}
	command := model.applyEvent(testProtocolEvent(model.session.ThreadID, "turn-1", protocol.TurnCompleteEvent{
		Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeCompleted, FinishedAt: time.Now().UTC(),
	}))
	message := command().(fullscreenUserMessageAdmittedMsg)
	message.admission = protocol.UserMessageAdmission{}
	model.handleUserMessageAdmission(message)
	if model.input.Value() != "retry malformed" || model.nextTurnQueue.HasQueuedFollowUp() {
		t.Fatalf("malformed admission restore input=%q queue=%#v", model.input.Value(), model.nextTurnQueue)
	}
}

func TestFullscreenQueuedModeMismatchRestoresComposer(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.running = true
	if err := model.nextTurnQueue.Enqueue(queuedInput("planned next", turn.ModeKindPlan, model.session.ThreadID, model.session.Generation)); err != nil {
		t.Fatal(err)
	}
	if command := model.applyEvent(testProtocolEvent(model.session.ThreadID, "turn-1", protocol.TurnCompleteEvent{
		Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeCompleted, FinishedAt: time.Now().UTC(),
	})); command != nil {
		t.Fatal("mode mismatch submitted queued input")
	}
	if model.input.Value() != "planned next" || model.nextTurnQueue.HasQueuedFollowUp() {
		t.Fatalf("mode mismatch input=%q queue=%#v", model.input.Value(), model.nextTurnQueue)
	}
}

func TestFullscreenUnexpectedQueuedSteerStopsDrainWithoutDuplicateRestore(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	fake := fakeApplication(t, model)
	fake.submitAdmission = protocol.UserMessageAdmission{Kind: protocol.UserMessageAdmissionSteered, TurnID: "turn-active"}
	model.running = true
	for _, content := range []string{"second", "third"} {
		if err := model.nextTurnQueue.Enqueue(queuedInput(content, turn.ModeKindDefault, model.session.ThreadID, model.session.Generation)); err != nil {
			t.Fatal(err)
		}
	}
	command := model.applyEvent(testProtocolEvent(model.session.ThreadID, "turn-1", protocol.TurnCompleteEvent{
		Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeCompleted, FinishedAt: time.Now().UTC(),
	}))
	message := command().(fullscreenUserMessageAdmittedMsg)
	model.handleUserMessageAdmission(message)
	if !model.nextTurnQueue.Halted() || model.nextTurnQueue.InFlight != nil {
		t.Fatalf("unexpected steer did not halt queue: %#v", model.nextTurnQueue)
	}
	model.running = true
	if command := model.applyEvent(testProtocolEvent(model.session.ThreadID, "turn-active", protocol.TurnCompleteEvent{
		Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeCompleted, FinishedAt: time.Now().UTC(),
	})); command != nil {
		t.Fatal("halted queue submitted another input")
	}
	if model.input.Value() != "third" || strings.Contains(model.input.Value(), "second") {
		t.Fatalf("halt restore duplicated accepted input: %q", model.input.Value())
	}
}

func TestFullscreenQueuedPlanInputSuppressesImplementationOverlay(t *testing.T) {
	_, model := newTestFullscreen(t, func(options *FullscreenOptions) {
		options.Snapshot.Configuration.Mode = protocol.ModeKindPlan
	})
	model.running = true
	model.completedProposedPlan = true
	if err := model.nextTurnQueue.Enqueue(queuedInput("refine the plan", turn.ModeKindPlan, model.session.ThreadID, model.session.Generation)); err != nil {
		t.Fatal(err)
	}
	command := model.applyEvent(testProtocolEvent(model.session.ThreadID, "turn-1", protocol.TurnCompleteEvent{
		Status: protocol.TurnStatusCompleted, Outcome: protocol.TurnOutcomeCompleted, FinishedAt: time.Now().UTC(),
	}))
	if command == nil || model.selection != nil || model.completedProposedPlan {
		t.Fatalf("plan terminal command=%v selection=%#v completedPlan=%v", command != nil, model.selection, model.completedProposedPlan)
	}
}

func TestFullscreenAttachmentReplacementClearsQueuedInput(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	if err := model.nextTurnQueue.Enqueue(queuedInput("old thread", turn.ModeKindDefault, model.session.ThreadID, model.session.Generation)); err != nil {
		t.Fatal(err)
	}
	model.clearInteractiveState()
	if model.nextTurnQueue.HasQueuedFollowUp() || model.queuedInputPreview() != "" {
		t.Fatalf("attachment clear retained queue: %#v", model.nextTurnQueue)
	}
}

func TestFullscreenStaleQueuedSubmissionResultCannotRestoreIntoNewAttachment(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	oldThreadID := model.session.ThreadID
	if err := model.nextTurnQueue.Enqueue(queuedInput("old thread", turn.ModeKindDefault, oldThreadID, model.session.Generation)); err != nil {
		t.Fatal(err)
	}
	queued, ok, err := model.nextTurnQueue.Begin(oldThreadID, model.session.Generation, model.session.mode())
	if err != nil || !ok {
		t.Fatalf("begin old queued input = %v, %v", ok, err)
	}
	submission := UserMessageSubmission{
		Message: queued.Message, Mode: queued.Mode, FromNextTurnQueue: true,
		OriginThreadID: queued.ThreadID, OriginGeneration: queued.AttachmentGeneration,
	}
	model.clearInteractiveState()
	model.session.ThreadID = testThreadID(2)
	model.session.Generation = 2
	model.handleUserMessageRejection(fullscreenUserMessageRejectedMsg{submission: submission, err: errors.New("late")})
	if model.input.Value() != "" || model.nextTurnQueue.HasQueuedFollowUp() {
		t.Fatalf("stale result polluted new attachment: input=%q queue=%#v", model.input.Value(), model.nextTurnQueue)
	}
}

func TestQueuedInputPreviewIsBounded(t *testing.T) {
	_, model := newTestFullscreen(t, func(options *FullscreenOptions) { options.Width = 40 })
	for _, content := range []string{
		"a very long queued message that must be truncated to fit the terminal",
		"second", "third", "fourth",
	} {
		if err := model.nextTurnQueue.Enqueue(queuedInput(content, turn.ModeKindDefault, model.session.ThreadID, model.session.Generation)); err != nil {
			t.Fatal(err)
		}
	}
	preview := xansi.Strip(model.queuedInputPreview())
	lines := strings.Split(preview, "\n")
	if len(lines) != maxQueuedInputPreviewItems+2 || !strings.Contains(lines[len(lines)-1], "+1 more") {
		t.Fatalf("preview lines = %#v", lines)
	}
	for _, line := range lines {
		if len([]rune(line)) > model.width {
			t.Fatalf("preview line exceeds width %d: %q", model.width, line)
		}
	}
}

func TestQueuedInputPreviewYieldsToPopupAndOverlay(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	if err := model.nextTurnQueue.Enqueue(queuedInput("next", turn.ModeKindDefault, model.session.ThreadID, model.session.Generation)); err != nil {
		t.Fatal(err)
	}
	model.input.SetValue("/e")
	model.slashPopup.sync(model.input.Value(), model.running)
	if auxiliary := xansi.Strip(model.composerAuxiliaryView()); strings.Contains(auxiliary, "Queued (1)") || !strings.Contains(auxiliary, "/exit") {
		t.Fatalf("popup auxiliary = %q", auxiliary)
	}
	model.slashPopup.dismiss(model.input.Value())
	model.selection = &selectionOverlay{Title: "Selection", Items: []selectionItem{{Name: "one"}}}
	if auxiliary := model.composerAuxiliaryView(); auxiliary != "" {
		t.Fatalf("overlay auxiliary = %q", auxiliary)
	}
}
