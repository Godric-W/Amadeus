package plan

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/tool"
)

func (graph ExecutionGraph) Validate() error {
	if !graph.Kind.Valid() {
		return fmt.Errorf("execution graph kind %q is invalid", graph.Kind)
	}
	if graph.Version < 1 {
		return errors.New("execution graph version must be at least one")
	}
	if len(graph.Tasks) == 0 {
		return errors.New("execution graph tasks are empty")
	}
	positions := make(map[TaskID]int, len(graph.Tasks))
	for index, task := range graph.Tasks {
		if err := validateGraphTask(task); err != nil {
			return fmt.Errorf("execution graph task %d: %w", index, err)
		}
		if _, duplicate := positions[task.ID]; duplicate {
			return fmt.Errorf("execution graph task ID %q is duplicated", task.ID)
		}
		positions[task.ID] = index
	}
	for _, task := range graph.Tasks {
		seenDependencies := make(map[TaskID]struct{}, len(task.Dependencies))
		for _, dependency := range task.Dependencies {
			if dependency == task.ID {
				return fmt.Errorf("execution graph task %q depends on itself", task.ID)
			}
			position, exists := positions[dependency]
			if !exists {
				return fmt.Errorf("execution graph task %q has missing dependency %q", task.ID, dependency)
			}
			if _, duplicate := seenDependencies[dependency]; duplicate {
				return fmt.Errorf("execution graph task %q repeats dependency %q", task.ID, dependency)
			}
			seenDependencies[dependency] = struct{}{}
			if task.Status == TaskStatusCompleted && graph.Tasks[position].Status != TaskStatusCompleted {
				return fmt.Errorf("completed task %q depends on incomplete task %q", task.ID, dependency)
			}
		}
	}
	if cycle := graphCycle(graph, positions); len(cycle) != 0 {
		return fmt.Errorf("execution graph contains dependency cycle: %s", strings.Join(cycle, " -> "))
	}
	return nil
}

func validateGraphTask(task Task) error {
	if strings.TrimSpace(string(task.ID)) == "" || strings.ContainsAny(string(task.ID), " \t\r\n") {
		return errors.New("task ID is empty or contains whitespace")
	}
	if strings.TrimSpace(task.Objective) == "" {
		return fmt.Errorf("task %q objective is empty", task.ID)
	}
	if !task.Status.Valid() {
		return fmt.Errorf("task %q status %q is invalid", task.ID, task.Status)
	}
	if task.Attempts < 0 {
		return fmt.Errorf("task %q attempts cannot be negative", task.ID)
	}
	if task.SideEffect != "" && !task.SideEffect.Valid() {
		return fmt.Errorf("task %q side effect %q is invalid", task.ID, task.SideEffect)
	}
	if task.SideEffect == tool.SideEffectNone && len(task.Resources) != 0 {
		return fmt.Errorf("task %q with no side effect cannot reserve resources", task.ID)
	}
	seenCriteria := make(map[string]struct{}, len(task.AcceptanceCriteria))
	for index, criterion := range task.AcceptanceCriteria {
		if strings.TrimSpace(criterion.ID) == "" || strings.TrimSpace(criterion.Description) == "" {
			return fmt.Errorf("task %q criterion %d requires ID and description", task.ID, index)
		}
		if _, duplicate := seenCriteria[criterion.ID]; duplicate {
			return fmt.Errorf("task %q criterion ID %q is duplicated", task.ID, criterion.ID)
		}
		seenCriteria[criterion.ID] = struct{}{}
	}
	if task.Budget.MaxIterations < 0 || task.Budget.MaxToolCalls < 0 || task.Budget.MaxInputTokens < 0 || task.Budget.MaxOutputTokens < 0 || task.Budget.MaxDuration < 0 {
		return fmt.Errorf("task %q budget cannot be negative", task.ID)
	}
	seenResources := make(map[string]struct{}, len(task.Resources))
	for _, resource := range task.Resources {
		resource = strings.TrimSpace(resource)
		if resource == "" {
			return fmt.Errorf("task %q contains an empty resource", task.ID)
		}
		if _, duplicate := seenResources[resource]; duplicate {
			return fmt.Errorf("task %q repeats resource %q", task.ID, resource)
		}
		seenResources[resource] = struct{}{}
	}
	return nil
}

func graphCycle(graph ExecutionGraph, positions map[TaskID]int) []string {
	const (
		unvisited = iota
		visiting
		visited
	)
	states := make([]int, len(graph.Tasks))
	stack := make([]TaskID, 0, len(graph.Tasks))
	var visit func(int) []string
	visit = func(index int) []string {
		states[index] = visiting
		stack = append(stack, graph.Tasks[index].ID)
		for _, dependency := range graph.Tasks[index].Dependencies {
			position := positions[dependency]
			if states[position] == visiting {
				cycle := []string{string(dependency)}
				for cursor := len(stack) - 1; cursor >= 0 && stack[cursor] != dependency; cursor-- {
					cycle = append(cycle, string(stack[cursor]))
				}
				cycle = append(cycle, string(dependency))
				return cycle
			}
			if states[position] == unvisited {
				if cycle := visit(position); len(cycle) != 0 {
					return cycle
				}
			}
		}
		stack = stack[:len(stack)-1]
		states[index] = visited
		return nil
	}
	for index := range graph.Tasks {
		if states[index] == unvisited {
			if cycle := visit(index); len(cycle) != 0 {
				return cycle
			}
		}
	}
	return nil
}
