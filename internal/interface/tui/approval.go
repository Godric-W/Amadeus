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
	first := "once"
	if request.Presentation.Title != "" {
		if _, err := fmt.Fprintf(prompt.output, "%s\n", sanitizeInlineEventText(request.Presentation.Title)); err != nil {
			return policy.ApprovalDecision{}, fmt.Errorf("write inline approval title: %w", err)
		}
		for _, detail := range request.Presentation.Details {
			if _, err := fmt.Fprintf(prompt.output, "  %s\n", sanitizeInlineEventText(detail)); err != nil {
				return policy.ApprovalDecision{}, fmt.Errorf("write inline approval detail: %w", err)
			}
		}
		question := request.Presentation.Question
		if question == "" {
			question = "Do you want to proceed?"
		}
		if _, err := fmt.Fprintf(prompt.output, "%s [y] Yes / [s] session / [n] No: ", sanitizeInlineEventText(question)); err != nil {
			return policy.ApprovalDecision{}, fmt.Errorf("write inline approval question: %w", err)
		}
	} else if _, err := fmt.Fprintf(prompt.output, "approval input\n  tool: %s\n  risk: %s\n  reason: %s\n  arguments_sha256: %s\nAllow? [y] %s / [s] session / [n] deny: ", sanitizeInlineEventText(request.ToolName), request.Risk, sanitizeInlineEventText(request.Reason), request.ArgumentsSHA256, first); err != nil {
		return policy.ApprovalDecision{}, fmt.Errorf("write inline approval prompt: %w", err)
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
		if _, err := io.WriteString(prompt.output, "Invalid choice. Enter y, s, or n: "); err != nil {
			return policy.ApprovalDecision{}, fmt.Errorf("write inline approval retry prompt: %w", err)
		}
	}
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
