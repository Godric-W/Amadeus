package cli

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

type TerminalApprovalHandler struct {
	input      io.Reader
	output     io.Writer
	isTerminal TerminalDetector
	mutex      sync.Mutex
}

func NewTerminalApprovalHandler(options TerminalApprovalOptions) (*TerminalApprovalHandler, error) {
	if options.Input == nil {
		return nil, errors.New("terminal approval input is nil")
	}
	if options.Output == nil {
		return nil, errors.New("terminal approval output is nil")
	}
	if options.IsTerminal == nil {
		options.IsTerminal = isTerminalReader
	}
	return &TerminalApprovalHandler{
		input:      options.Input,
		output:     options.Output,
		isTerminal: options.IsTerminal,
	}, nil
}

func (handler *TerminalApprovalHandler) Decide(ctx context.Context, request policy.ApprovalRequest) (policy.ApprovalDecision, error) {
	if handler == nil {
		return policy.ApprovalDecision{}, errors.New("terminal approval handler is nil")
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

	handler.mutex.Lock()
	defer handler.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return policy.ApprovalDecision{}, err
	}
	if !handler.isTerminal(handler.input) {
		return policyApprovalDecision(policy.ApprovalDeny, "approval requires a TTY; non-interactive input was denied"), nil
	}
	return handler.interactiveDecision(ctx, request)
}

func (handler *TerminalApprovalHandler) interactiveDecision(ctx context.Context, request policy.ApprovalRequest) (policy.ApprovalDecision, error) {
	if err := writeApprovalPrompt(handler.output, request); err != nil {
		return policy.ApprovalDecision{}, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return policy.ApprovalDecision{}, err
		}
		line, err := readApprovalLine(handler.input)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return policyApprovalDecision(policy.ApprovalDeny, "approval input closed; request denied"), nil
			}
			return policy.ApprovalDecision{}, fmt.Errorf("read terminal approval: %w", err)
		}
		if decision, ok := parseApprovalChoiceForPurpose(line, request.Purpose); ok {
			return decision, nil
		}
		if _, err := io.WriteString(handler.output, "Invalid choice. Enter y, s, or n: "); err != nil {
			return policy.ApprovalDecision{}, fmt.Errorf("write terminal approval retry prompt: %w", err)
		}
	}
}

func writeApprovalPrompt(writer io.Writer, request policy.ApprovalRequest) error {
	first := "once"
	if request.Purpose == policy.ApprovalPurposePermission {
		first = "this run"
	}
	_, err := fmt.Fprintf(writer,
		"Approval required\n  request: %s\n  tool: %s\n  risk: %s\n  reason: %s\n  arguments_sha256: %s\nAllow? [y] %s / [s] session / [n] deny: ",
		sanitizeApprovalText(request.ID), sanitizeApprovalText(request.ToolName), request.Risk,
		sanitizeApprovalText(request.Reason), request.ArgumentsSHA256, first,
	)
	if err != nil {
		return fmt.Errorf("write terminal approval prompt: %w", err)
	}
	return nil
}

func parseApprovalChoice(input string) (policy.ApprovalDecision, bool) {
	return parseApprovalChoiceForPurpose(input, policy.ApprovalPurposeCommand)
}

func parseApprovalChoiceForPurpose(input string, purpose policy.ApprovalPurpose) (policy.ApprovalDecision, bool) {
	firstScope, firstReason := policy.ApprovalOnce, "user approved once"
	if purpose == policy.ApprovalPurposePermission {
		firstScope, firstReason = policy.ApprovalRun, "user approved for the run"
	}
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

var _ policy.ApprovalHandler = (*TerminalApprovalHandler)(nil)
