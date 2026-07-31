package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/policy"
)

func TestTerminalApprovalHandlerInteractiveChoices(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		outcome policy.ApprovalOutcome
		scope   policy.ApprovalScope
	}{
		{name: "allow once", input: "y\n", outcome: policy.ApprovalAllow, scope: policy.ApprovalOnce},
		{name: "allow session", input: "SESSION\n", outcome: policy.ApprovalAllow, scope: policy.ApprovalSession},
		{name: "allow always", input: "a\n", outcome: policy.ApprovalAllow, scope: policy.ApprovalAlways},
		{name: "deny", input: "no\n", outcome: policy.ApprovalDeny, scope: policy.ApprovalOnce},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			handler := newTestTerminalApprovalHandler(t, strings.NewReader(test.input), &output, true, true, config.ApprovalAsk)
			decision, err := handler.Decide(context.Background(), testApprovalRequest(t))
			if err != nil {
				t.Fatalf("decide approval: %v", err)
			}
			if decision.Outcome != test.outcome || decision.Scope != test.scope || decision.Source != policy.ApprovalSourceUser {
				t.Fatalf("unexpected decision: %#v", decision)
			}
			if err := decision.Validate(); err != nil {
				t.Fatalf("invalid decision: %v", err)
			}
			if !strings.Contains(output.String(), "Approval required") || !strings.Contains(output.String(), "arguments_sha256:") {
				t.Fatalf("approval prompt is incomplete: %q", output.String())
			}
		})
	}
}

func TestTerminalApprovalHandlerRetriesInvalidChoice(t *testing.T) {
	var output bytes.Buffer
	handler := newTestTerminalApprovalHandler(t, strings.NewReader("maybe\n\ny\n"), &output, true, true, config.ApprovalAsk)
	decision, err := handler.Decide(context.Background(), testApprovalRequest(t))
	if err != nil || !decision.Allowed() {
		t.Fatalf("unexpected retry decision: decision=%#v err=%v", decision, err)
	}
	if strings.Count(output.String(), "Invalid choice") != 2 {
		t.Fatalf("unexpected retry prompt: %q", output.String())
	}
}

func TestTerminalApprovalHandlerNonInteractiveDefaults(t *testing.T) {
	tests := []struct {
		name     string
		default_ config.ApprovalDefault
		outcome  policy.ApprovalOutcome
	}{
		{name: "allow", default_: config.ApprovalAllow, outcome: policy.ApprovalAllow},
		{name: "deny", default_: config.ApprovalDeny, outcome: policy.ApprovalDeny},
		{name: "ask fails closed", default_: config.ApprovalAsk, outcome: policy.ApprovalDeny},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := &countingReader{reader: strings.NewReader("y\n")}
			var output bytes.Buffer
			handler := newTestTerminalApprovalHandler(t, input, &output, false, true, test.default_)
			decision, err := handler.Decide(context.Background(), testApprovalRequest(t))
			if err != nil {
				t.Fatalf("decide approval: %v", err)
			}
			if decision.Outcome != test.outcome || decision.Scope != policy.ApprovalOnce || decision.Source != policy.ApprovalSourceDefault {
				t.Fatalf("unexpected non-interactive decision: %#v", decision)
			}
			if input.reads != 0 || output.Len() != 0 {
				t.Fatalf("non-interactive handler used terminal I/O: reads=%d output=%q", input.reads, output.String())
			}
		})
	}
}

func TestTerminalApprovalHandlerDisabledAllowsWithoutIO(t *testing.T) {
	input := &countingReader{reader: strings.NewReader("n\n")}
	var output bytes.Buffer
	handler := newTestTerminalApprovalHandler(t, input, &output, true, false, config.ApprovalDeny)
	decision, err := handler.Decide(context.Background(), testApprovalRequest(t))
	if err != nil || !decision.Allowed() || decision.Source != policy.ApprovalSourceDefault {
		t.Fatalf("unexpected disabled decision: decision=%#v err=%v", decision, err)
	}
	if input.reads != 0 || output.Len() != 0 {
		t.Fatalf("disabled handler used terminal I/O: reads=%d output=%q", input.reads, output.String())
	}
}

func TestTerminalApprovalHandlerEOFAndContext(t *testing.T) {
	handler := newTestTerminalApprovalHandler(t, strings.NewReader(""), io.Discard, true, true, config.ApprovalAsk)
	decision, err := handler.Decide(context.Background(), testApprovalRequest(t))
	if err != nil || decision.Allowed() || decision.Source != policy.ApprovalSourceDefault {
		t.Fatalf("unexpected EOF decision: decision=%#v err=%v", decision, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if decision, err := handler.Decide(ctx, testApprovalRequest(t)); !errors.Is(err, context.Canceled) || decision != (policy.ApprovalDecision{}) {
		t.Fatalf("unexpected canceled decision: decision=%#v err=%v", decision, err)
	}
	if decision, err := handler.Decide(nil, testApprovalRequest(t)); err == nil || decision != (policy.ApprovalDecision{}) {
		t.Fatalf("unexpected nil-context decision: decision=%#v err=%v", decision, err)
	}
}

func TestTerminalApprovalHandlerReportsIOFailures(t *testing.T) {
	request := testApprovalRequest(t)
	writeFailure := errors.New("write failed")
	handler := newTestTerminalApprovalHandler(t, strings.NewReader("y\n"), errorWriter{err: writeFailure}, true, true, config.ApprovalAsk)
	if _, err := handler.Decide(context.Background(), request); !errors.Is(err, writeFailure) {
		t.Fatalf("unexpected prompt write error: %v", err)
	}

	readFailure := errors.New("read failed")
	handler = newTestTerminalApprovalHandler(t, errorReader{err: readFailure}, io.Discard, true, true, config.ApprovalAsk)
	if _, err := handler.Decide(context.Background(), request); !errors.Is(err, readFailure) {
		t.Fatalf("unexpected read error: %v", err)
	}
}

func TestTerminalApprovalHandlerDoesNotRenderArguments(t *testing.T) {
	secret := "do-not-print-secret"
	request, err := policy.NewApprovalRequest("request\n\x1b[31m", "execute\tcommand", []byte(`{"command":"`+secret+`"}`), policy.CommandRiskHigh, "network\r\ncommand")
	if err != nil {
		t.Fatalf("create approval request: %v", err)
	}
	var output bytes.Buffer
	handler := newTestTerminalApprovalHandler(t, strings.NewReader("n\n"), &output, true, true, config.ApprovalAsk)
	if _, err := handler.Decide(context.Background(), request); err != nil {
		t.Fatalf("decide approval: %v", err)
	}
	rendered := output.String()
	if strings.Contains(rendered, secret) || strings.Contains(rendered, "\x1b") || strings.Contains(rendered, "request\n") || strings.Contains(rendered, "network\r\n") {
		t.Fatalf("approval prompt exposed unsafe content: %q", rendered)
	}
}

func TestNewTerminalApprovalHandlerValidatesOptions(t *testing.T) {
	tests := []struct {
		name    string
		options TerminalApprovalOptions
	}{
		{name: "nil input", options: TerminalApprovalOptions{Output: io.Discard, Default: config.ApprovalAsk}},
		{name: "nil output", options: TerminalApprovalOptions{Input: strings.NewReader(""), Default: config.ApprovalAsk}},
		{name: "invalid default", options: TerminalApprovalOptions{Input: strings.NewReader(""), Output: io.Discard, Default: "invalid"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if handler, err := NewTerminalApprovalHandler(test.options); err == nil || handler != nil {
				t.Fatalf("unexpected constructor result: handler=%#v err=%v", handler, err)
			}
		})
	}

	var handler *TerminalApprovalHandler
	if decision, err := handler.Decide(context.Background(), testApprovalRequest(t)); err == nil || decision != (policy.ApprovalDecision{}) {
		t.Fatalf("unexpected nil handler result: decision=%#v err=%v", decision, err)
	}
}

func TestReadApprovalLineBoundsInput(t *testing.T) {
	if _, err := readApprovalLine(strings.NewReader(strings.Repeat("x", maxApprovalInputBytes+1))); err == nil {
		t.Fatal("expected oversized approval input to fail")
	}
	if line, err := readApprovalLine(strings.NewReader("yes")); err != nil || line != "yes" {
		t.Fatalf("unexpected final EOF line: line=%q err=%v", line, err)
	}
}

func newTestTerminalApprovalHandler(t *testing.T, input io.Reader, output io.Writer, terminal, enabled bool, default_ config.ApprovalDefault) *TerminalApprovalHandler {
	t.Helper()
	handler, err := NewTerminalApprovalHandler(TerminalApprovalOptions{
		Input: input, Output: output, Enabled: enabled, Default: default_,
		IsTerminal: func(io.Reader) bool { return terminal },
	})
	if err != nil {
		t.Fatalf("create terminal approval handler: %v", err)
	}
	return handler
}

func testApprovalRequest(t *testing.T) policy.ApprovalRequest {
	t.Helper()
	request, err := policy.NewApprovalRequest("approval-1", "execute_command", []byte(`{"command":"go test ./..."}`), policy.CommandRiskHigh, "command executes project code")
	if err != nil {
		t.Fatalf("create approval request: %v", err)
	}
	return request
}

type countingReader struct {
	reader io.Reader
	reads  int
}

func (reader *countingReader) Read(buffer []byte) (int, error) {
	reader.reads++
	return reader.reader.Read(buffer)
}

type errorWriter struct{ err error }

func (writer errorWriter) Write([]byte) (int, error) { return 0, writer.err }
