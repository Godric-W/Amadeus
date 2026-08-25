package exec

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
)

type requestUserInputPrompt struct {
	scanner *bufio.Scanner
	output  io.Writer
}

func newRequestUserInputPrompt(input io.Reader, output io.Writer) *requestUserInputPrompt {
	var scanner *bufio.Scanner
	if input != nil {
		scanner = bufio.NewScanner(input)
	}
	return &requestUserInputPrompt{scanner: scanner, output: output}
}

func (prompt *requestUserInputPrompt) Prompt(ctx context.Context, request protocol.RequestUserInputEvent) (protocol.RequestUserInputResponse, error) {
	if prompt == nil || ctx == nil || prompt.scanner == nil || prompt.output == nil {
		return protocol.RequestUserInputResponse{}, errors.New("request_user_input interface is unavailable")
	}
	if err := request.Validate(); err != nil {
		return protocol.RequestUserInputResponse{}, err
	}
	response := protocol.RequestUserInputResponse{Answers: make(map[string]protocol.RequestUserInputAnswer, len(request.Questions))}
	for _, question := range request.Questions {
		if _, err := fmt.Fprintf(prompt.output, "\n%s\n%s\n", question.Header, question.Question); err != nil {
			return protocol.RequestUserInputResponse{}, err
		}
		for index, option := range question.Options {
			if _, err := fmt.Fprintf(prompt.output, "  %d. %s — %s\n", index+1, option.Label, option.Description); err != nil {
				return protocol.RequestUserInputResponse{}, err
			}
		}
		if _, err := fmt.Fprintf(prompt.output, "  %d. Other — type a free-form answer\n> ", len(question.Options)+1); err != nil {
			return protocol.RequestUserInputResponse{}, err
		}
		if !prompt.scanner.Scan() {
			if err := prompt.scanner.Err(); err != nil {
				return protocol.RequestUserInputResponse{}, err
			}
			return protocol.RequestUserInputResponse{}, io.EOF
		}
		answer, err := parsePromptAnswer(strings.TrimSpace(prompt.scanner.Text()), question)
		if err != nil {
			return protocol.RequestUserInputResponse{}, err
		}
		response.Answers[question.ID] = protocol.RequestUserInputAnswer{Answers: answer}
	}
	return response, response.Validate(request.RequestUserInputArgs)
}

func parsePromptAnswer(value string, question protocol.RequestUserInputQuestion) ([]string, error) {
	parts := []string{value}
	if question.MultiSelect {
		parts = strings.Split(value, ",")
	}
	answers := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		index, err := strconv.Atoi(part)
		if err == nil && index >= 1 && index <= len(question.Options) {
			answers = append(answers, question.Options[index-1].Label)
			continue
		}
		if err == nil && index == len(question.Options)+1 {
			return nil, errors.New("Other requires free-form text instead of its number")
		}
		if part == "" {
			return nil, errors.New("answer is empty")
		}
		answers = append(answers, part)
	}
	if !question.MultiSelect && len(answers) != 1 {
		return nil, errors.New("question accepts one answer")
	}
	return answers, nil
}
