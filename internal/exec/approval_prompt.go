package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/Godric-W/Amadeus/internal/policy"
)

const maxApprovalInputBytes = 1024

type TerminalDetector func(io.Reader) bool

type TerminalApprovalOptions struct {
	Input      io.Reader
	Output     io.Writer
	IsTerminal TerminalDetector
}

type TerminalApprovalPrompt struct {
	input      io.Reader
	output     io.Writer
	isTerminal TerminalDetector
	mutex      sync.Mutex
}

func NewTerminalApprovalPrompt(options TerminalApprovalOptions) (*TerminalApprovalPrompt, error) {
	if options.Input == nil {
		return nil, errors.New("terminal approval input is nil")
	}
	if options.Output == nil {
		return nil, errors.New("terminal approval output is nil")
	}
	if options.IsTerminal == nil {
		options.IsTerminal = isTerminalReader
	}
	return &TerminalApprovalPrompt{
		input:      options.Input,
		output:     options.Output,
		isTerminal: options.IsTerminal,
	}, nil
}

func (toolImpl *TerminalApprovalPrompt) Decide(ctx context.Context, request policy.ApprovalRequest) (policy.ApprovalDecision, error) {
	if toolImpl == nil {
		return policy.ApprovalDecision{}, errors.New("terminal approval prompt is nil")
	}
	if ctx == nil {
		return policy.ApprovalDecision{}, errors.New("terminal approval context is nil")
	}
	if err := ctx.Err(); err != nil {
		return policy.ApprovalDecision{}, err
	}
	if err := request.Validate(); err != nil {
		return policy.ApprovalDecision{}, fmt.Errorf("validate terminal approval request: %w", err)
	}

	toolImpl.mutex.Lock()
	defer toolImpl.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return policy.ApprovalDecision{}, err
	}
	if !toolImpl.isTerminal(toolImpl.input) {
		return policyApprovalDecision(policy.ApprovalDeny, "approval requires a TTY; non-interactive input was denied"), nil
	}
	return toolImpl.interactiveDecision(ctx, request)
}

func (toolImpl *TerminalApprovalPrompt) interactiveDecision(ctx context.Context, request policy.ApprovalRequest) (policy.ApprovalDecision, error) {
	if err := writeApprovalPrompt(toolImpl.output, request); err != nil {
		return policy.ApprovalDecision{}, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return policy.ApprovalDecision{}, err
		}
		line, err := readApprovalLine(toolImpl.input)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return policyApprovalDecision(policy.ApprovalDeny, "approval input closed; request denied"), nil
			}
			return policy.ApprovalDecision{}, fmt.Errorf("read terminal approval: %w", err)
		}
		if decision, ok := policy.ResolveApprovalInput(request, line); ok {
			return decision, nil
		}
		if _, err := io.WriteString(toolImpl.output, "Invalid choice. Enter 1, 2, 3, y, s, or n: "); err != nil {
			return policy.ApprovalDecision{}, fmt.Errorf("write terminal approval retry prompt: %w", err)
		}
	}
}

func writeApprovalPrompt(writer io.Writer, request policy.ApprovalRequest) error {
	title := strings.TrimSpace(request.Presentation.Title)
	if title == "" {
		title = "Tool use"
	}
	if _, err := fmt.Fprintln(writer, sanitizeApprovalText(title)); err != nil {
		return fmt.Errorf("write approval title: %w", err)
	}
	for _, detail := range request.Presentation.Details {
		if _, err := fmt.Fprintf(writer, "  %s\n", sanitizeApprovalText(detail)); err != nil {
			return fmt.Errorf("write approval detail: %w", err)
		}
	}
	if len(request.Presentation.Details) == 0 {
		if _, err := fmt.Fprintf(writer, "  Tool: %s\n", sanitizeApprovalText(request.ToolName)); err != nil {
			return fmt.Errorf("write approval tool: %w", err)
		}
	}
	question := strings.TrimSpace(request.Presentation.Question)
	if question == "" {
		question = "Do you want to proceed?"
	}
	if _, err := fmt.Fprintln(writer, sanitizeApprovalText(question)); err != nil {
		return fmt.Errorf("write approval question: %w", err)
	}
	for index, option := range request.Presentation.Options {
		if _, err := fmt.Fprintf(writer, "  %d. %s\n", index+1, sanitizeApprovalText(option.Label)); err != nil {
			return fmt.Errorf("write approval option: %w", err)
		}
		if description := strings.TrimSpace(option.Description); description != "" {
			if _, err := fmt.Fprintf(writer, "     %s\n", sanitizeApprovalText(description)); err != nil {
				return fmt.Errorf("write approval option description: %w", err)
			}
		}
	}
	if _, err := io.WriteString(writer, "Choose [1-3] ([y] once / [s] session / [n] no): "); err != nil {
		return fmt.Errorf("write approval choice prompt: %w", err)
	}
	return nil
}

func parseApprovalChoice(input string) (policy.ApprovalDecision, bool) {
	return parseApprovalChoiceForPurpose(input, policy.ApprovalPurposeCommand)
}

func parseApprovalChoiceForPurpose(input string, purpose policy.ApprovalPurpose) (policy.ApprovalDecision, bool) {
	firstScope, firstReason := policy.ApprovalOnce, "user approved once"
	_ = purpose
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "y", "yes":
		return userApprovalDecision(policy.ApprovalAllow, firstScope, firstReason), true
	case "s", "session":
		return userApprovalDecision(policy.ApprovalAllow, policy.ApprovalSession, "user approved for the session"), true
	case "n", "no", "deny":
		return userApprovalDecision(policy.ApprovalDeny, firstScope, "user denied the request"), true
	default:
		return policy.ApprovalDecision{}, false
	}
}

func userApprovalDecision(outcome policy.ApprovalOutcome, scope policy.ApprovalScope, reason string) policy.ApprovalDecision {
	return policy.ApprovalDecision{Outcome: outcome, Scope: scope, Source: policy.ApprovalSourceUser, Reason: reason}
}

func policyApprovalDecision(outcome policy.ApprovalOutcome, reason string) policy.ApprovalDecision {
	return policy.ApprovalDecision{Outcome: outcome, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourcePolicy, Reason: reason}
}

func readApprovalLine(reader io.Reader) (string, error) {
	var content strings.Builder
	var buffer [1]byte
	for content.Len() <= maxApprovalInputBytes {
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
	return "", fmt.Errorf("input exceeds %d byte limit", maxApprovalInputBytes)
}

func sanitizeApprovalText(value string) string {
	const maxRunes = 256
	var sanitized strings.Builder
	count := 0
	for len(value) > 0 && count < maxRunes {
		r, size := utf8.DecodeRuneInString(value)
		value = value[size:]
		if r == utf8.RuneError && size == 1 {
			r = '?'
		}
		if unicode.IsControl(r) {
			r = ' '
		}
		sanitized.WriteRune(r)
		count++
	}
	if value != "" {
		sanitized.WriteString("…")
	}
	return sanitized.String()
}

func isTerminalReader(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

var _ policy.ApprovalPort = (*TerminalApprovalPrompt)(nil)
