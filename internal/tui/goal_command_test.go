package tui

import (
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/protocol"
)

func TestGoalSlashCommandCreatesAndProjectsGoal(t *testing.T) {
	_, model := newTestModel(t, nil)
	updated, command := model.dispatchCommand(SlashInvocation{Command: SlashGoal, Args: "finish the benchmark"})
	if command == nil {
		t.Fatal("goal command is nil")
	}
	message := command()
	updated, _ = updated.(appModel).Update(message)
	result := updated.(appModel)
	if result.session.Goal == nil || result.session.Goal.Objective != "finish the benchmark" || result.session.Goal.Status != protocol.ThreadGoalActive {
		t.Fatalf("goal = %#v", result.session.Goal)
	}
	if len(result.historyCells) == 0 || cellContent(result.historyCells[0]) != "/goal finish the benchmark" {
		t.Fatalf("goal command was not visible in transcript: %#v", result.historyCells)
	}
	if result.footer.GoalIndicator != "Pursuing goal (0s)" {
		t.Fatalf("Goal indicator = %q", result.footer.GoalIndicator)
	}
	if strings.Contains(lastCellContent(result), "Objective:") || strings.Contains(lastCellContent(result), "Time used:") {
		t.Fatalf("setting a goal rendered a detail summary: %q", lastCellContent(result))
	}
}

func TestGoalSlashCommandConfirmsReplacement(t *testing.T) {
	_, model := newTestModel(t, nil)
	fake := fakeApplication(t, model)
	fake.goal = &protocol.ThreadGoal{ThreadID: testThreadID(1), Objective: "old objective", Status: protocol.ThreadGoalActive, CreatedAt: 1, UpdatedAt: 1}
	updated, command := model.dispatchCommand(SlashInvocation{Command: SlashGoal, Args: "new objective"})
	updated, _ = updated.(appModel).Update(command())
	result := updated.(appModel)
	if result.selection == nil || result.selectionKind != "goal-replace" || result.pendingGoalObjective != "new objective" {
		t.Fatalf("selection=%#v kind=%q pending=%q", result.selection, result.selectionKind, result.pendingGoalObjective)
	}
}

func TestGoalEventsReplaceAndClearSnapshot(t *testing.T) {
	_, model := newTestModel(t, nil)
	goal := protocol.ThreadGoal{ThreadID: model.session.ThreadID, Objective: "persisted goal", Status: protocol.ThreadGoalPaused, CreatedAt: 1, UpdatedAt: 1}
	if command := model.projectProtocolEvent(protocol.ThreadGoalUpdatedEvent{ThreadID: model.session.ThreadID, Goal: goal}); command != nil {
		_ = command
	}
	if model.session.Goal == nil || model.footer.GoalIndicator != "Goal paused (/goal resume)" {
		t.Fatalf("goal=%#v footer=%q", model.session.Goal, model.footer.GoalIndicator)
	}
	model.projectProtocolEvent(protocol.ThreadGoalClearedEvent{ThreadID: model.session.ThreadID})
	if model.session.Goal != nil || model.footer.GoalIndicator != "" {
		t.Fatalf("cleared goal=%#v footer=%q", model.session.Goal, model.footer.GoalIndicator)
	}
}
