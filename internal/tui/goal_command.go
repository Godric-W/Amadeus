package tui

import (
	"fmt"
	"strings"

	goalextension "github.com/Godric-W/Amadeus/internal/extension/goal"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/state"
	tea "github.com/charmbracelet/bubbletea"
)

func (model appModel) goalCommand(arguments string) tea.Cmd {
	arguments = strings.TrimSpace(arguments)
	return func() tea.Msg {
		switch strings.ToLower(arguments) {
		case "":
			goal, err := model.app.options.Application.Goal(model.ctx)
			return goalCommandResultMsg{kind: "show", goal: goal, err: err}
		case "clear":
			cleared, err := model.app.options.Application.ClearGoal(model.ctx)
			return goalCommandResultMsg{kind: "clear", cleared: cleared, err: err}
		case "pause":
			status := protocol.ThreadGoalPaused
			goal, err := model.app.options.Application.SetGoal(model.ctx, goalextension.ObjectiveUpdate{}, &status, state.TokenBudgetUpdate{})
			return goalCommandResultMsg{kind: "updated", goal: &goal, err: err}
		case "resume":
			status := protocol.ThreadGoalActive
			goal, err := model.app.options.Application.SetGoal(model.ctx, goalextension.ObjectiveUpdate{}, &status, state.TokenBudgetUpdate{})
			return goalCommandResultMsg{kind: "updated", goal: &goal, err: err}
		case "edit":
			goal, err := model.app.options.Application.Goal(model.ctx)
			return goalCommandResultMsg{kind: "edit", goal: goal, err: err}
		default:
			current, err := model.app.options.Application.Goal(model.ctx)
			if err != nil {
				return goalCommandResultMsg{kind: "set", err: err}
			}
			if current != nil && current.Status != protocol.ThreadGoalComplete {
				return goalCommandResultMsg{kind: "confirm", goal: current, objective: arguments}
			}
			status := protocol.ThreadGoalActive
			goal, err := model.app.options.Application.SetGoal(model.ctx, goalextension.ObjectiveUpdate{Set: true, Value: arguments}, &status, state.TokenBudgetUpdate{})
			return goalCommandResultMsg{kind: "updated", goal: &goal, err: err}
		}
	}
}

func (model appModel) replaceGoal(objective string) tea.Cmd {
	return func() tea.Msg {
		if _, err := model.app.options.Application.ClearGoal(model.ctx); err != nil {
			return goalCommandResultMsg{kind: "replace", err: err}
		}
		status := protocol.ThreadGoalActive
		goal, err := model.app.options.Application.SetGoal(model.ctx, goalextension.ObjectiveUpdate{Set: true, Value: objective}, &status, state.TokenBudgetUpdate{})
		return goalCommandResultMsg{kind: "updated", goal: &goal, err: err}
	}
}

func (model appModel) editGoal(objective string) tea.Cmd {
	return func() tea.Msg {
		current, err := model.app.options.Application.Goal(model.ctx)
		if err != nil {
			return goalCommandResultMsg{kind: "edit", err: err}
		}
		if current == nil {
			return goalCommandResultMsg{kind: "edit", err: fmt.Errorf("no goal is currently set")}
		}
		status := current.Status
		if status == protocol.ThreadGoalComplete || status == protocol.ThreadGoalBudgetLimited {
			status = protocol.ThreadGoalActive
		}
		goal, err := model.app.options.Application.SetGoal(model.ctx, goalextension.ObjectiveUpdate{Set: true, Value: objective}, &status, state.TokenBudgetUpdate{})
		return goalCommandResultMsg{kind: "updated", goal: &goal, err: err}
	}
}

func (model *appModel) handleGoalCommandResult(message goalCommandResultMsg) tea.Cmd {
	model.status = "idle"
	if message.err != nil {
		model.insertHistoryCell(NewErrorHistoryCell("goal: " + message.err.Error()))
		return nil
	}
	switch message.kind {
	case "show":
		if message.goal == nil {
			model.insertHistoryCell(NewInfoHistoryCell("No goal is currently set. Use /goal <objective> to create one."))
		} else {
			model.insertHistoryCell(NewInfoHistoryCell(formatGoalSummary(*message.goal)))
		}
	case "clear":
		model.setGoalSnapshot(nil, model.uiNow())
		if message.cleared {
			model.insertHistoryCell(NewInfoHistoryCell("Goal cleared"))
		} else {
			model.insertHistoryCell(NewInfoHistoryCell("No goal to clear"))
		}
	case "updated":
		if message.goal != nil {
			model.setGoalSnapshot(message.goal, model.uiNow())
		}
	case "edit":
		if message.goal == nil {
			model.insertHistoryCell(NewErrorHistoryCell("No goal is currently set."))
			return nil
		}
		model.selection = &selectionOverlay{Title: "Edit goal", Subtitle: "Type a goal objective and press Enter", Input: true, Value: message.goal.Objective, Hint: "Esc cancel"}
		model.selectionKind = "goal-edit"
		model.input.Blur()
	case "confirm":
		model.pendingGoalObjective = message.objective
		model.selection = &selectionOverlay{Title: "Replace goal?", Subtitle: "New objective: " + truncateGoalText(message.objective), Items: []selectionItem{
			{Name: "Replace current goal", Description: "Reset usage and start the new objective"},
			{Name: "Cancel", Description: "Keep the current goal"},
		}}
		model.selectionKind = "goal-replace"
	}
	return nil
}

func formatGoalSummary(goal protocol.ThreadGoal) string {
	parts := []string{"Goal " + string(goal.Status), "Objective: " + goal.Objective, fmt.Sprintf("Time used: %ds", goal.TimeUsedSeconds), "Tokens used: " + compactTokenCount(goal.TokensUsed)}
	if goal.TokenBudget != nil {
		parts = append(parts, "Token budget: "+compactTokenCount(*goal.TokenBudget))
	}
	return strings.Join(parts, "\n")
}

func truncateGoalText(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > 120 {
		return string(runes[:119]) + "…"
	}
	return string(runes)
}

func (model *appModel) promptResumableGoal() {
	goal := model.session.Goal
	if goal == nil || (goal.Status != protocol.ThreadGoalPaused && goal.Status != protocol.ThreadGoalBlocked && goal.Status != protocol.ThreadGoalUsageLimited) {
		return
	}
	model.selection = &selectionOverlay{Title: "Resume paused goal?", Subtitle: "Goal: " + truncateGoalText(goal.Objective), Items: []selectionItem{
		{Name: "Resume goal", Description: "Mark it active and continue when idle"},
		{Name: "Leave paused", Description: "Keep the current Goal stopped"},
	}}
	model.selectionKind = "goal-resume"
	model.input.Blur()
}
