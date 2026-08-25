package exec

import (
	"io"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/bootstrap"
	"github.com/Godric-W/Amadeus/internal/policy"
)

type Options struct {
	Bootstrap bootstrap.WorkspaceOptions
	Target    app.ThreadTarget
	Task      string

	Input       io.Reader
	Output      io.Writer
	ErrorOutput io.Writer
	IsTerminal  TerminalDetector

	EventSink protocol.EventSink
	Approvals policy.ApprovalPort
}

type ExitError struct {
	Code     int
	Message  string
	Reported bool
	Cause    error
}

func (err *ExitError) Error() string {
	if err == nil {
		return ""
	}
	return err.Message
}

func (err *ExitError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

func (err *ExitError) ExitCode() int {
	if err == nil || err.Code == 0 {
		return 1
	}
	return err.Code
}

func (err *ExitError) AlreadyReported() bool {
	return err != nil && err.Reported
}
