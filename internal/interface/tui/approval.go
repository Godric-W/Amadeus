package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/Godric-W/Amadeus/internal/policy"
)

const maxInlineApprovalInputBytes = 1024

type InlineApprovalPromptOptions struct {
	Input      io.Reader
	Output     io.Writer
	IsTerminal func(io.Reader) bool
}

type InlineApprovalPrompt struct {
	input      io.Reader
	output     io.Writer
	isTerminal func(io.Reader) bool
	mutex      sync.Mutex
}

func NewInlineApprovalPrompt(options InlineApprovalPromptOptions) (*InlineApprovalPrompt, error) {
	if options.Input == nil {
		return nil, errors.New("inline approval input is nil")
	}
	if options.Output == nil {
		return nil, errors.New("inline approval output is nil")
	}
	if options.IsTerminal == nil {
		return nil, errors.New("inline approval terminal detector is nil")
	}
	return &InlineApprovalPrompt{input: options.Input, output: options.Output, isTerminal: options.IsTerminal}, nil
}

func (prompt *InlineApprovalPrompt) Decide(ctx context.Context, request policy.ApprovalRequest) (policy.ApprovalDecision, error) {
	if prompt == nil {
		return policy.ApprovalDecision{}, errors.New("inline approval prompt is nil")
	}
	if ctx == nil {
		return policy.ApprovalDecision{}, errors.New("inline approval context is nil")
	}
	if err := ctx.Err(); err != nil {
		return policy.ApprovalDecision{}, err
	}
	if err := request.Validate(); err != nil {
		return policy.ApprovalDecision{}, fmt.Errorf("validate inline approval request: %w", err)
	}

	prompt.mutex.Lock()
	defer prompt.mutex.Unlock()
	if !prompt.isTerminal(prompt.input) {
		return inlinePolicyDecision("approval requires a TTY; non-interactive input was denied"), nil
	}
	if err := writeInlineApprovalPrompt(prompt.output, request); err != nil {
		return policy.ApprovalDecision{}, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return policy.ApprovalDecision{}, err
		}
		line, err := readInlineApprovalLine(prompt.input)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return inlinePolicyDecision("approval input closed; request denied"), nil
			}
			return policy.ApprovalDecision{}, fmt.Errorf("read inline approval: %w", err)
		}
		if decision, ok := policy.ResolveApprovalInput(request, line); ok {
			return decision, nil
		}
		if _, err := io.WriteString(prompt.output, "Invalid choice. Enter 1, 2, 3, y, s, or n: "); err != nil {
			return policy.ApprovalDecision{}, fmt.Errorf("write inline approval retry prompt: %w", err)
		}
	}
}

func writeInlineApprovalPrompt(writer io.Writer, request policy.ApprovalRequest) error {
	title := strings.TrimSpace(request.Presentation.Title)
	if title == "" {
		title = "Tool use"
	}
	if _, err := fmt.Fprintln(writer, sanitizeInlineEventText(title)); err != nil {
		return fmt.Errorf("write inline approval title: %w", err)
	}
	for _, detail := range request.Presentation.Details {
		if _, err := fmt.Fprintf(writer, "  %s\n", sanitizeInlineEventText(detail)); err != nil {
			return fmt.Errorf("write inline approval detail: %w", err)
		}
	}
	if len(request.Presentation.Details) == 0 {
		if _, err := fmt.Fprintf(writer, "  Tool: %s\n", sanitizeInlineEventText(request.ToolName)); err != nil {
			return fmt.Errorf("write inline approval tool: %w", err)
		}
	}
	question := strings.TrimSpace(request.Presentation.Question)
	if question == "" {
		question = "Do you want to proceed?"
	}
	if _, err := fmt.Fprintln(writer, sanitizeInlineEventText(question)); err != nil {
		return fmt.Errorf("write inline approval question: %w", err)
	}
	for index, option := range request.Presentation.Options {
		if _, err := fmt.Fprintf(writer, "  %d. %s\n", index+1, sanitizeInlineEventText(option.Label)); err != nil {
			return fmt.Errorf("write inline approval option: %w", err)
		}
		if description := strings.TrimSpace(option.Description); description != "" {
			if _, err := fmt.Fprintf(writer, "     %s\n", sanitizeInlineEventText(description)); err != nil {
				return fmt.Errorf("write inline approval option description: %w", err)
			}
		}
	}
	_, err := io.WriteString(writer, "Choose [1-3] ([y] once / [s] session / [n] no): ")
	if err != nil {
		return fmt.Errorf("write inline approval choice prompt: %w", err)
	}
	return nil
}

func parseInlineApprovalChoice(input string) (policy.ApprovalDecision, bool) {
	return parseInlineApprovalChoiceForPurpose(input, policy.ApprovalPurposeCommand)
}

func parseInlineApprovalChoiceForPurpose(input string, purpose policy.ApprovalPurpose) (policy.ApprovalDecision, bool) {
	firstScope, firstReason := policy.ApprovalOnce, "user approved once"
	_ = purpose
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "y", "yes":
		return inlineUserDecision(policy.ApprovalAllow, firstScope, firstReason), true
	case "s", "session":
		return inlineUserDecision(policy.ApprovalAllow, policy.ApprovalSession, "user approved for the session"), true
	case "n", "no", "deny":
		return inlineUserDecision(policy.ApprovalDeny, firstScope, "user denied the request"), true
	default:
		return policy.ApprovalDecision{}, false
	}
}

func inlineUserDecision(outcome policy.ApprovalOutcome, scope policy.ApprovalScope, reason string) policy.ApprovalDecision {
	return policy.ApprovalDecision{Outcome: outcome, Scope: scope, Source: policy.ApprovalSourceUser, Reason: reason}
}

func inlinePolicyDecision(reason string) policy.ApprovalDecision {
	return policy.ApprovalDecision{Outcome: policy.ApprovalDeny, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourcePolicy, Reason: reason}
}

func readInlineApprovalLine(reader io.Reader) (string, error) {
	var content strings.Builder
	var buffer [1]byte
	for content.Len() <= maxInlineApprovalInputBytes {
		count, err := reader.Read(buffer[:])
		if count > 0 {
			if buffer[0] == '\n' {
				return strings.TrimSuffix(content.String(), "\r"), nil
			}
			content.WriteByte(buffer[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) && content.Len() > 0 {
				return strings.TrimSuffix(content.String(), "\r"), nil
			}
			return "", err
		}
		if count == 0 {
			return "", io.ErrNoProgress
		}
	}
	return "", fmt.Errorf("input exceeds %d byte limit", maxInlineApprovalInputBytes)
}

var _ policy.ApprovalPort = (*InlineApprovalPrompt)(nil)
