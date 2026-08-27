package session

import (
	"github.com/Godric-W/Amadeus/internal/contextmanager"
	"github.com/Godric-W/Amadeus/internal/llm"
)

func (session *Session) promptSnapshot(step StepContext) contextmanager.PromptSnapshot {
	return session.Snapshot(step.Model, session.promptShapeForStep(step))
}

func (session *Session) promptShapeForStep(step StepContext) llm.Prompt {
	specs := step.ToolRouter.Specs()
	tools := make([]llm.ToolSpec, len(specs))
	for index, spec := range specs {
		tools[index] = llm.ToolSpec{
			Name: spec.Name, Description: spec.Description,
			InputSchema: append([]byte(nil), spec.InputSchema...), OutputSchema: append([]byte(nil), spec.OutputSchema...), Strict: spec.Strict,
		}
	}
	return llm.Prompt{
		BaseInstructions: session.BaseInstructions(), Tools: tools,
		ParallelToolCalls: step.Model.SupportsParallelToolCalls,
		OutputSchema:      append(llm.OutputSchema(nil), step.Turn.OutputSchema...), OutputSchemaStrict: step.Turn.OutputSchemaStrict,
	}
}
