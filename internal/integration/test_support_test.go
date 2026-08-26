package integration

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	application "github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/bootstrap"
	"github.com/Godric-W/Amadeus/internal/cli"
	"github.com/Godric-W/Amadeus/internal/interface/tui"
	"github.com/Godric-W/Amadeus/internal/policy"
)

func testAgentRootOptions(amadeusRoot, workingDirectory string, terminal bool) cli.RootOptions {
	environment := bootstrap.Environment{
		AmadeusRoot: amadeusRoot, WorkingDirectory: workingDirectory,
		LookupEnv: emptyEnvLookup,
	}
	dependencies := bootstrap.DefaultDependencies(environment)
	dependencies.AuditFactory = func() (audit.Sink, io.Closer, error) {
		return audit.NewMemorySink(), nil, nil
	}
	return cli.RootOptions{
		Environment: environment,
		Bootstrap:   dependencies,
		TUI:         runInteractiveTestTUI,
		IsTerminal:  func(io.Reader) bool { return terminal },
	}
}

func emptyEnvLookup(string) (string, bool) { return "", false }

func writeCommandConfig(t testingT, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
}

type testingT interface {
	Helper()
	Fatalf(string, ...any)
}

func testNextID(turnID string) func(string) string {
	var sequence atomic.Uint64
	return func(kind string) string {
		if kind == "turn" {
			return turnID
		}
		return fmt.Sprintf("%s-test-%d", kind, sequence.Add(1))
	}
}

// runInteractiveTestTUI is a cross-layer test fixture, not a production
// frontend. It drives the same InteractiveApplication port used by Fullscreen
// and returns after one terminal event so Provider/Tool E2E tests stay bounded.
func runInteractiveTestTUI(ctx context.Context, options tui.RunOptions) (result tui.RunResult, runErr error) {
	bootstrapped, err := bootstrap.OpenWorkspace(ctx, options.Bootstrap)
	if err != nil {
		return result, err
	}
	defer func() { runErr = errors.Join(runErr, bootstrap.CloseWorkspace(bootstrapped.Workspace)) }()
	start, err := bootstrapped.Workspace.PrepareStart(ctx, options.Target, bootstrapped.Configuration)
	if err != nil {
		return result, err
	}
	_ = start
	interactive, err := application.NewInteractiveApplication(ctx, application.InteractiveOptions{
		Workspace: bootstrapped.Workspace, Configuration: bootstrapped.Configuration,
		MaxUserMessageBytes: options.MaxUserMessageBytes,
	})
	if err != nil {
		return result, err
	}
	defer interactive.Close()
	if _, err := interactive.Start(ctx); err != nil {
		return result, err
	}
	if options.Prompt == "" {
		return result, errors.New("test TUI requires an initial prompt")
	}
	if _, err := interactive.SubmitUser(ctx, options.Prompt, "integration-initial", protocol.ThreadSettingsOverrides{}); err != nil {
		return result, err
	}

	renderer := &integrationEventRenderer{stdout: options.Output, stderr: options.ErrorOutput}
	approvals := bufio.NewScanner(options.Input)
	for {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case event := <-interactive.Events():
			switch event := event.(type) {
			case application.SessionEventObserved:
				if err := renderer.Publish(event.Event); err != nil {
					return result, err
				}
				switch message := event.Event.Msg.(type) {
				case protocol.TurnCompleteEvent:
					return result, terminalEventError(message.Error)
				case protocol.TurnAbortedEvent:
					return result, context.Canceled
				case protocol.ErrorEvent:
					return result, errors.New(message.Message)
				}
			case application.ApprovalRequested:
				decision := policy.ApprovalDecision{Outcome: policy.ApprovalDeny, Scope: policy.ApprovalOnce, Source: policy.ApprovalSourcePolicy, Reason: "integration input unavailable"}
				if approvals.Scan() {
					if selected, ok := policy.ResolveApprovalInput(event.Request, approvals.Text()); ok {
						decision = selected
					}
				}
				if err := interactive.ResolveApproval(ctx, event.RequestID, decision); err != nil {
					return result, err
				}
			case application.ApplicationError:
				return result, event.Error
			}
		}
	}
}

type integrationEventRenderer struct {
	stdout   io.Writer
	stderr   io.Writer
	openText bool
}

func (renderer *integrationEventRenderer) Publish(event protocol.Event) error {
	switch message := event.Msg.(type) {
	case protocol.AgentMessageContentDeltaEvent:
		if message.Delta != "" {
			_, err := io.WriteString(renderer.stdout, message.Delta)
			renderer.openText = true
			return err
		}
	case protocol.ItemCompletedEvent:
		if message.Item.Kind == protocol.ItemAssistantMessage {
			return renderer.finishText()
		}
		if message.Item.ToolName != "" {
			return renderer.status("Ran %s: %s", message.Item.ToolName, message.Item.Text)
		}
	case protocol.ItemStartedEvent:
		if message.Item.ToolName != "" {
			return renderer.status("Running %s", message.Item.ToolName)
		}
	case protocol.PlanUpdateEvent:
		if err := renderer.status("Updated Plan"); err != nil {
			return err
		}
		for _, item := range message.Plan {
			if err := renderer.status("%s", item.Step); err != nil {
				return err
			}
		}
	case protocol.WarningEvent:
		return renderer.status("warning: %s", message.Message)
	case protocol.TurnCompleteEvent:
		if message.Summary != "" {
			return renderer.status("%s", message.Summary)
		}
		return renderer.status("result: completed")
	case protocol.TurnAbortedEvent:
		return renderer.status("%s", message.Reason)
	}
	return nil
}

func (renderer *integrationEventRenderer) finishText() error {
	if !renderer.openText {
		return nil
	}
	renderer.openText = false
	_, err := io.WriteString(renderer.stdout, "\n")
	return err
}

func (renderer *integrationEventRenderer) status(format string, values ...any) error {
	if err := renderer.finishText(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(renderer.stderr, format+"\n", values...)
	return err
}

func terminalEventError(message string) error {
	message = strings.TrimSpace(message)
	if message == "" || strings.HasPrefix(message, "result: completed") {
		return nil
	}
	return errors.New(message)
}
