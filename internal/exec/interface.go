package exec

import (
	"errors"
	"io"
	"os"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/interface/tui"
	"github.com/Godric-W/Amadeus/internal/policy"
	"github.com/Godric-W/Amadeus/internal/render"
)

func resolveInterface(options Options) (protocol.EventSink, policy.ApprovalPort, error) {
	if options.EventSink != nil || options.Approvals != nil {
		if options.EventSink == nil || options.Approvals == nil {
			return nil, nil, errors.New("one-shot external interface requires both event sink and approval port")
		}
		return options.EventSink, options.Approvals, nil
	}
	detectTerminal := options.IsTerminal
	if detectTerminal == nil {
		detectTerminal = isTerminalInput
	}
	capabilities := tui.DetectTerminalCapabilitiesWithOptions(options.Input, options.Output, tui.TerminalCapabilityOptions{
		IsTerminal: func(input io.Reader) bool { return detectTerminal(input) },
	})
	if capabilities.TTY {
		renderer, err := tui.NewInlineRenderer(options.Output, options.ErrorOutput)
		if err != nil {
			return nil, nil, err
		}
		approvals, err := NewTerminalApprovalPrompt(TerminalApprovalOptions{
			Input: options.Input, Output: options.ErrorOutput,
			IsTerminal: func(input io.Reader) bool { return detectTerminal(input) },
		})
		return renderer, approvals, err
	}
	renderer, err := render.NewAgentRenderer(options.Output, options.ErrorOutput)
	if err != nil {
		return nil, nil, err
	}
	approvals, err := NewTerminalApprovalPrompt(TerminalApprovalOptions{
		Input: options.Input, Output: options.ErrorOutput,
		IsTerminal: func(input io.Reader) bool { return detectTerminal(input) },
	})
	return renderer, approvals, err
}

func isTerminalInput(input io.Reader) bool {
	file, ok := input.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
