package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/Godric-W/Amadeus/internal/interface/tui"
)

const (
	exitCommandCyan            = "\x1b[36m"
	exitCommandForegroundReset = "\x1b[39m"
)

func presentFullscreenExit(info tui.AppExitInfo, output, errorOutput io.Writer, colorEnabled bool) error {
	var presentationErr error
	fatalErr := info.Error
	if info.ExitReason == tui.ExitReasonFatal {
		if fatalErr == nil {
			fatalErr = errors.New("fullscreen application exited fatally")
		}
		if _, err := fmt.Fprintf(errorOutput, "ERROR: %v\n", fatalErr); err != nil {
			presentationErr = errors.Join(presentationErr, err)
		}
	} else if info.Error != nil {
		if _, err := fmt.Fprintf(errorOutput, "WARNING: %v\n", info.Error); err != nil {
			presentationErr = errors.Join(presentationErr, err)
		}
	}

	if usage := info.TokenUsage; usage.TotalTokens != 0 || usage.InputTokens != 0 || usage.OutputTokens != 0 {
		if _, err := fmt.Fprintf(output, "Token usage: total=%d input=%d output=%d\n", usage.TotalTokens, usage.InputTokens, usage.OutputTokens); err != nil {
			presentationErr = errors.Join(presentationErr, err)
		}
	}
	if info.ResumeHint != "" {
		resumeCommand := info.ResumeHint
		if colorEnabled {
			resumeCommand = exitCommandCyan + resumeCommand + exitCommandForegroundReset
		}
		if _, err := fmt.Fprintf(output, "To continue this session, run %s\n", resumeCommand); err != nil {
			presentationErr = errors.Join(presentationErr, err)
		}
	} else if info.ExitReason == tui.ExitReasonFatal && !info.ThreadID.IsZero() {
		if _, err := fmt.Fprintf(output, "Session ID: %s\n", info.ThreadID); err != nil {
			presentationErr = errors.Join(presentationErr, err)
		}
	}

	if info.ExitReason == tui.ExitReasonFatal {
		presentationErr = errors.Join(presentationErr, &commandExitError{
			code: exitCodeFailure, message: fatalErr.Error(), reported: true, cause: fatalErr,
		})
	}
	return presentationErr
}
