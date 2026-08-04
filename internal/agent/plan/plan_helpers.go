package plan

import "github.com/Godric-W/Amadeus/internal/llm"

func cloneLLMMessages(messages []llm.Message) []llm.Message {
	cloned := append([]llm.Message(nil), messages...)
	for index := range cloned {
		cloned[index].ToolCalls = append([]llm.ToolCall(nil), cloned[index].ToolCalls...)
		for callIndex := range cloned[index].ToolCalls {
			cloned[index].ToolCalls[callIndex].Arguments = append([]byte(nil), cloned[index].ToolCalls[callIndex].Arguments...)
		}
	}
	return cloned
}

func mergeEvidence(existing, returned []Evidence) []Evidence {
	if len(returned) == 0 {
		return cloneEvidence(existing)
	}
	merged := cloneEvidence(existing)
	positions := make(map[EvidenceID]int, len(merged))
	for index, item := range merged {
		positions[item.ID] = index
	}
	for _, item := range cloneEvidence(returned) {
		if index, ok := positions[item.ID]; ok {
			merged[index] = item
			continue
		}
		positions[item.ID] = len(merged)
		merged = append(merged, item)
	}
	return merged
}

func mergeBudget(existing, returned BudgetState) BudgetState {
	if returned.Budget == (Budget{}) {
		returned.Budget = existing.Budget
	}
	return returned
}

func cloneEvidence(evidence []Evidence) []Evidence {
	cloned := append([]Evidence(nil), evidence...)
	for index := range cloned {
		cloned[index].CriterionIDs = append([]string(nil), cloned[index].CriterionIDs...)
		if cloned[index].Artifact != nil {
			artifact := *cloned[index].Artifact
			cloned[index].Artifact = &artifact
		}
	}
	return cloned
}

func graphTask(graph ExecutionGraph, id TaskID) Task {
	for _, task := range graph.Tasks {
		if task.ID == id {
			return task
		}
	}
	return Task{ID: id, Status: TaskStatusFailed}
}

func cloneTaskResult(result *TaskResult) *TaskResult {
	if result == nil {
		return nil
	}
	cloned := *result
	cloned.EvidenceIDs = append([]EvidenceID(nil), result.EvidenceIDs...)
	return &cloned
}
