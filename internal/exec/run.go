package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/bootstrap"
)

func Run(ctx context.Context, options Options) (runErr error) {
	if ctx == nil {
		return errors.New("one-shot context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	objective := strings.TrimSpace(options.Task)
	if objective == "" {
		return errors.New("Coding Agent task is empty")
	}
	if options.Input == nil || options.Output == nil || options.ErrorOutput == nil {
		return errors.New("one-shot streams are nil")
	}
	if err := options.Target.Validate(); err != nil {
		return err
	}

	bootstrapped, err := bootstrap.OpenWorkspace(ctx, options.Bootstrap)
	if err != nil {
		return err
	}
	defer func() {
		runErr = errors.Join(runErr, bootstrap.CloseWorkspace(bootstrapped.Workspace))
	}()

	start, err := bootstrapped.Workspace.PrepareStart(ctx, options.Target, bootstrapped.Configuration)
	if err != nil {
		return err
	}
	if err := presentThreadStart(options.ErrorOutput, options.Target, start); err != nil {
		return err
	}
	active := start.Active
	if active == nil {
		active, err = bootstrapped.Workspace.EnsureCurrent(ctx, bootstrapped.Configuration)
		if err != nil {
			return err
		}
	}

	renderer, approvals, err := resolveInterface(options)
	if err != nil {
		return err
	}
	if err := active.Submit(ctx, protocol.UserInputOp{
		Content: objective,
		ThreadSettings: protocol.ThreadSettingsOverrides{
			CollaborationMode: &protocol.CollaborationMode{Mode: protocol.ModeKind(bootstrapped.Configuration.Mode)},
		},
	}); err != nil {
		return err
	}
	return ProcessEvents(ctx, active, EventProcessorOptions{
		Renderer: renderer, Approvals: approvals,
		Input: options.Input, Output: options.Output,
	})
}

func presentThreadStart(output io.Writer, target app.ThreadTarget, start app.ThreadStartResult) error {
	switch target.Kind {
	case app.ThreadTargetLatest:
		if start.Draft {
			_, err := fmt.Fprintln(output, "session: no previous session; using a new draft")
			return err
		}
		if start.Active != nil && start.Metadata != nil {
			_, err := fmt.Fprintf(output, "session: continued %s (%s)\n", start.Active.ID(), start.Metadata.Title)
			return err
		}
	case app.ThreadTargetResume:
		title := ""
		if start.Metadata != nil {
			title = start.Metadata.Title
		}
		_, err := fmt.Fprintf(output, "session: resumed %s (%s)\n", target.ThreadID, title)
		return err
	}
	return nil
}
