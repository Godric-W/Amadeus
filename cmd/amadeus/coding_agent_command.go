package main

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
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/engine"
	bootstrap "github.com/Godric-W/Amadeus/internal/app/bootstrap"
	"github.com/Godric-W/Amadeus/internal/audit"
	"github.com/Godric-W/Amadeus/internal/config"
	agentcontext "github.com/Godric-W/Amadeus/internal/context"
	"github.com/Godric-W/Amadeus/internal/instruction"
	interfacecli "github.com/Godric-W/Amadeus/internal/interface/cli"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/render"
	"github.com/spf13/cobra"
)

const envXDGStateHome = "XDG_STATE_HOME"

var agentRunSequence atomic.Uint64

type auditSinkFactory func() (audit.Sink, io.Closer, error)

type codingAgentCommand struct {
	command *cobra.Command
	flags   *configFlags
	runtime commandRuntime
}

func defaultAgentCommandFactory(command *cobra.Command, flags *configFlags, runtime commandRuntime) (agentCommand, error) {
	if command == nil {
		return nil, errors.New("Coding Agent Cobra command is nil")
	}
	if flags == nil {
		return nil, errors.New("Coding Agent config flags are nil")
	}
	return &codingAgentCommand{command: command, flags: flags, runtime: runtime}, nil
}

func (runner *codingAgentCommand) Run(ctx context.Context, invocation agentInvocation) error {
	if runner == nil || runner.command == nil || runner.flags == nil {
		return errors.New("Coding Agent command is nil")
	}
	if ctx == nil {
		return errors.New("Coding Agent context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	switch invocation.Mode {
	case agentInvocationInteractive:
		return runner.runInteractive(ctx, invocation)
	case agentInvocationOnce:
		runCtx, cancel, err := runner.newRunContext(ctx)
		if err != nil {
			return err
		}
		defer cancel()
		return runner.runOnce(runCtx, invocation)
	default:
		return fmt.Errorf("unsupported Coding Agent invocation mode %q", invocation.Mode)
	}
}

func (runner *codingAgentCommand) runInteractive(ctx context.Context, invocation agentInvocation) error {
	if invocation.Input == nil || invocation.Output == nil || invocation.ErrorOutput == nil {
		return errors.New("interactive Coding Agent streams are nil")
	}
	reader := bufio.NewReader(invocation.Input)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		fmt.Fprint(invocation.ErrorOutput, "amadeus> ")
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("read interactive task: %w", err)
		}
		if len(line) > maxRootTaskBytes {
			fmt.Fprintf(invocation.ErrorOutput, "error: interactive task exceeds %d bytes\n", maxRootTaskBytes)
		} else {
			task := strings.TrimSpace(line)
			switch task {
			case "":
			case "/exit":
				fmt.Fprintln(invocation.ErrorOutput, "session: closed")
				return nil
			default:
				runInvocation := invocation
				runInvocation.Mode = agentInvocationOnce
				runInvocation.Task = task
				runInvocation.Input = reader
				runCtx, cancel, contextErr := runner.newRunContext(ctx)
				if contextErr != nil {
					return contextErr
				}
				runErr := runner.runOnce(runCtx, runInvocation)
				cancel()
				if runErr != nil && !errorAlreadyReported(runErr) {
					fmt.Fprintf(invocation.ErrorOutput, "error: %v\n", runErr)
				}
			}
		}
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(invocation.ErrorOutput, "session: closed")
			return nil
		}
	}
}

func (runner *codingAgentCommand) newRunContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	factory := runner.runtime.agentContextFactory
	if factory == nil {
		factory = interruptibleTurnContext
	}
	ctx, cancel := factory(parent)
	if ctx == nil || cancel == nil {
		if cancel != nil {
			cancel()
		}
		return nil, nil, errors.New("Coding Agent Run context factory returned nil")
	}
	return ctx, cancel, nil
}

func (runner *codingAgentCommand) runOnce(ctx context.Context, invocation agentInvocation) (runErr error) {
	if strings.TrimSpace(invocation.Task) == "" {
		return errors.New("Coding Agent one-shot task is invalid")
	}

	configured, _, err := loadEffectiveConfig(runner.command, runner.flags, runner.runtime)
	if err != nil {
		return err
	}
	if err := config.Validate(configured); err != nil {
		return err
	}
	renderer, err := render.NewAgentRenderer(invocation.Output, invocation.ErrorOutput)
	if err != nil {
		return err
	}
	detectTerminal := runner.runtime.terminalDetector
	if detectTerminal == nil {
		detectTerminal = isTerminalInput
	}
	approvals, err := interfacecli.NewTerminalApprovalHandler(interfacecli.TerminalApprovalOptions{
		Input: invocation.Input, Output: invocation.ErrorOutput, Enabled: configured.Approval.Enabled, Default: configured.Approval.Default,
		IsTerminal: func(input io.Reader) bool { return detectTerminal(input) },
	})
	if err != nil {
		return err
	}
	auditFactory := runner.runtime.auditSinkFactory
	if auditFactory == nil {
		auditFactory = defaultAuditSinkFactory(runner.runtime.lookupEnv, os.UserHomeDir)
	}
	auditSink, auditCloser, err := auditFactory()
	if err != nil {
		return err
	}
	if auditSink == nil {
		return errors.New("Coding Agent audit factory returned nil sink")
	}
	if auditCloser != nil {
		defer func() {
			if closeErr := auditCloser.Close(); closeErr != nil {
				runErr = errors.Join(runErr, fmt.Errorf("close Coding Agent audit sink: %w", closeErr))
			}
		}()
	}

	options := bootstrap.AgentOptions{}
	if runner.runtime.llmClientFactory != nil {
		options.ClientFactory = func(providerName string, provider config.ProviderConfig) (client llm.Client, err error) {
			return runner.runtime.llmClientFactory(providerName, provider)
		}
	}
	agent, err := bootstrap.NewAgentWithOptions(configured, invocation.Project, renderer, approvals, auditSink, options)
	if err != nil {
		return err
	}
	if runner.runtime.rootErr != nil {
		return fmt.Errorf("resolve Amadeus root for user instructions: %w", runner.runtime.rootErr)
	}
	userLoader, err := instruction.NewUserLoader(runner.runtime.amadeusRoot, instruction.UserLoaderOptions{})
	if err != nil {
		return err
	}
	projectLoader, err := instruction.NewProjectLoader(invocation.Project, instruction.ProjectLoaderOptions{})
	if err != nil {
		return err
	}
	resolver, err := instruction.NewLayeredResolver(userLoader, projectLoader)
	if err != nil {
		return err
	}
	request, err := instruction.NewResolveRequest(invocation.Project, ".", instruction.TargetCommandCWD)
	if err != nil {
		return err
	}
	resolved, err := resolver.Resolve(ctx, request)
	if err != nil {
		return err
	}
	envelope, err := agent.ContextBuilder.Build(ctx, agentcontext.BuildInput{
		Prompt: agent.AgentPrompt, InstructionRequest: request, Instructions: resolved,
		Task: invocation.Task, Tools: agent.AvailableTools(),
	})
	if err != nil {
		return err
	}

	goal := engine.Goal{Objective: invocation.Task}
	runIDFactory := runner.runtime.runIDFactory
	if runIDFactory == nil {
		runIDFactory = nextAgentRunID
	}
	runID := strings.TrimSpace(runIDFactory())
	if runID == "" {
		return errors.New("Coding Agent run ID is empty")
	}
	state := engine.NewRun(engine.RunID(runID), goal, engine.NewDirectGraph(goal), configuredAgentBudget(configured.Agent))
	result, err := agent.Engine.Run(ctx, engine.DirectRunInput{
		State: state, Messages: envelope.Messages, AvailableTools: envelope.AvailableTools,
	})
	if err != nil {
		return err
	}
	outcome, code, err := classifyRunResult(result)
	if err != nil {
		return err
	}
	summary := formatRunSummary(outcome, result)
	fmt.Fprintln(invocation.ErrorOutput, summary)
	if code != exitCodeSuccess {
		return &commandExitError{code: code, message: summary, reported: true}
	}
	return nil
}

func configuredAgentBudget(agent config.AgentConfig) engine.Budget {
	return engine.Budget{
		MaxSteps:        agent.MaxSteps,
		MaxToolCalls:    agent.MaxToolCalls,
		MaxInputTokens:  agent.MaxInputTokens,
		MaxOutputTokens: agent.MaxOutputTokens,
		MaxDuration:     agent.MaxDuration,
	}
}

func defaultAuditSinkFactory(lookupEnv config.EnvLookup, userHomeDir func() (string, error)) auditSinkFactory {
	return func() (audit.Sink, io.Closer, error) {
		path, err := resolveAuditPath(lookupEnv, userHomeDir)
		if err != nil {
			return nil, nil, err
		}
		file, err := audit.OpenJSONLFile(path)
		if err != nil {
			return nil, nil, err
		}
		return file, file, nil
	}
}

func resolveAuditPath(lookupEnv config.EnvLookup, userHomeDir func() (string, error)) (string, error) {
	if lookupEnv != nil {
		if stateHome, ok := lookupEnv(envXDGStateHome); ok && strings.TrimSpace(stateHome) != "" {
			return filepath.Join(stateHome, "amadeus", "audit", "audit.jsonl"), nil
		}
	}
	if userHomeDir == nil {
		return "", errors.New("user home resolver is nil")
	}
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home for audit log: %w", err)
	}
	if strings.TrimSpace(home) == "" {
		return "", errors.New("user home for audit log is empty")
	}
	return filepath.Join(home, ".local", "state", "amadeus", "audit", "audit.jsonl"), nil
}

func nextAgentRunID() string {
	return fmt.Sprintf("run-%d-%d", time.Now().UTC().UnixNano(), agentRunSequence.Add(1))
}

var _ agentCommand = (*codingAgentCommand)(nil)
