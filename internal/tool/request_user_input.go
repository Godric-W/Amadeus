package tool

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var userInputQuestionID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)

type RequestUserInputOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type RequestUserInputQuestion struct {
	ID          string                   `json:"id"`
	Header      string                   `json:"header"`
	Question    string                   `json:"question"`
	Options     []RequestUserInputOption `json:"options"`
	MultiSelect bool                     `json:"multi_select,omitempty"`
}

type RequestUserInputArgs struct {
	Questions []RequestUserInputQuestion `json:"questions"`
}

func (args RequestUserInputArgs) Validate() error {
	if len(args.Questions) < 1 || len(args.Questions) > 3 {
		return errors.New("request_user_input requires 1 to 3 questions")
	}
	seen := make(map[string]struct{}, len(args.Questions))
	for index, question := range args.Questions {
		if !userInputQuestionID.MatchString(question.ID) {
			return fmt.Errorf("question %d id %q must be stable snake_case", index, question.ID)
		}
		if _, exists := seen[question.ID]; exists {
			return fmt.Errorf("question id %q is duplicated", question.ID)
		}
		seen[question.ID] = struct{}{}
		if strings.TrimSpace(question.Header) == "" || strings.TrimSpace(question.Question) == "" {
			return fmt.Errorf("question %q header and question are required", question.ID)
		}
		if len(question.Options) < 2 || len(question.Options) > 3 {
			return fmt.Errorf("question %q requires 2 to 3 options", question.ID)
		}
		for optionIndex, option := range question.Options {
			if strings.TrimSpace(option.Label) == "" || strings.TrimSpace(option.Description) == "" {
				return fmt.Errorf("question %q option %d is incomplete", question.ID, optionIndex)
			}
		}
	}
	return nil
}

type RequestUserInputAnswer struct {
	Answers []string `json:"answers"`
}

type RequestUserInputResponse struct {
	Answers map[string]RequestUserInputAnswer `json:"answers"`
}

func (response RequestUserInputResponse) Validate(args RequestUserInputArgs) error {
	if err := args.Validate(); err != nil {
		return err
	}
	if len(response.Answers) != len(args.Questions) {
		return errors.New("request_user_input response does not answer every question")
	}
	for _, question := range args.Questions {
		answer, ok := response.Answers[question.ID]
		if !ok || len(answer.Answers) == 0 {
			return fmt.Errorf("question %q has no answer", question.ID)
		}
		if !question.MultiSelect && len(answer.Answers) != 1 {
			return fmt.Errorf("question %q accepts exactly one answer", question.ID)
		}
		for _, value := range answer.Answers {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("question %q contains an empty answer", question.ID)
			}
		}
	}
	return nil
}

type UserInputRequester interface {
	RequestUserInput(context.Context, string, RequestUserInputArgs) (RequestUserInputResponse, error)
}

type InteractionUnavailableError struct{ Interaction string }

func (err InteractionUnavailableError) Error() string {
	return err.Interaction + " is unavailable in this interface"
}
