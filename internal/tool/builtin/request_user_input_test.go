package builtin

import (
	"context"
	"testing"

	"github.com/Godric-W/Amadeus/internal/tool"
)

type testUserInputRequester struct {
	callID string
	args   tool.RequestUserInputArgs
}

func (requester *testUserInputRequester) RequestUserInput(_ context.Context, callID string, args tool.RequestUserInputArgs) (tool.RequestUserInputResponse, error) {
	requester.callID, requester.args = callID, args
	return tool.RequestUserInputResponse{Answers: map[string]tool.RequestUserInputAnswer{"scope": {Answers: []string{"Minimal"}}}}, nil
}

func TestRequestUserInputUsesInteractionCapability(t *testing.T) {
	candidate := NewRequestUserInput()
	invocation := tool.Invocation{TurnID: "turn-1", Call: tool.NewCall("call-1", "request_user_input", []byte(`{"questions":[{"id":"scope","header":"Scope","question":"How broad?","options":[{"label":"Minimal","description":"Only basics"},{"label":"Broad","description":"Add extras"}]}]}`))}
	toolContext := tool.ToolUseContext{Context: context.Background(), Invocation: invocation, Interactions: &testUserInputRequester{}}
	if err := candidate.ValidateInput(toolContext, invocation); err != nil {
		t.Fatal(err)
	}
	prepared, err := candidate.Prepare(toolContext, invocation)
	if err != nil {
		t.Fatal(err)
	}
	result, err := candidate.Execute(toolContext, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if result.ToolName != "request_user_input" || result.Text != `{"answers":{"scope":{"answers":["Minimal"]}}}` {
		t.Fatalf("result = %#v", result)
	}
}

func TestRequestUserInputRejectsInvalidQuestionsAndUnavailableInterface(t *testing.T) {
	candidate := NewRequestUserInput()
	bad := tool.Invocation{Call: tool.NewCall("call-1", "request_user_input", []byte(`{"questions":[{"id":"Not Stable","header":"H","question":"Q","options":[]}]}`))}
	if err := candidate.ValidateInput(tool.ToolUseContext{}, bad); err == nil {
		t.Fatal("invalid questions were accepted")
	}
	invocation := tool.Invocation{Call: tool.NewCall("call-2", "request_user_input", []byte(`{"questions":[{"id":"scope","header":"Scope","question":"How broad?","options":[{"label":"Minimal","description":"Only basics"},{"label":"Broad","description":"Add extras"}]}]}`))}
	prepared, err := candidate.Prepare(tool.ToolUseContext{}, invocation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := candidate.Execute(tool.ToolUseContext{Context: context.Background(), Invocation: invocation}, prepared); err == nil {
		t.Fatal("missing interaction capability was accepted")
	}
}
