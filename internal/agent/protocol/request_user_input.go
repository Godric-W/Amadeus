package protocol

import (
	"errors"
	"strings"

	"github.com/Godric-W/Amadeus/internal/tool"
)

type RequestUserInputOption = tool.RequestUserInputOption
type RequestUserInputQuestion = tool.RequestUserInputQuestion
type RequestUserInputArgs = tool.RequestUserInputArgs
type RequestUserInputAnswer = tool.RequestUserInputAnswer
type RequestUserInputResponse = tool.RequestUserInputResponse

type RequestUserInputEvent struct {
	RequestID RequestID `json:"request_id"`
	ThreadID  ThreadID  `json:"thread_id,omitempty"`
	TurnID    TurnID    `json:"turn_id,omitempty"`
	CallID    string    `json:"call_id"`
	RequestUserInputArgs
}

func (RequestUserInputEvent) isEventMsg() {}

func (event RequestUserInputEvent) Validate() error {
	if strings.TrimSpace(string(event.RequestID)) == "" {
		return errors.New("request_user_input request ID is empty")
	}
	if strings.TrimSpace(event.CallID) == "" {
		return errors.New("request_user_input call ID is empty")
	}
	return event.RequestUserInputArgs.Validate()
}

type UserInputAnswerOp struct {
	RequestID RequestID
	Response  RequestUserInputResponse
}

func (UserInputAnswerOp) isOp() {}
