package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	extensionruntime "github.com/Godric-W/Amadeus/internal/extension"
	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
)

func (runner *agentController) ensureSessionRuntime(ctx context.Context, invocation agentInvocation) (*sessiondomain.SessionRuntime, error) {
	if runner.sessionRuntime != nil {
		if runner.sessionProject != invocation.Project.Path() {
			return nil, errors.New("Coding Agent SessionRuntime project changed")
		}
		return runner.sessionRuntime, nil
	}
	if runner.runtime.rootErr != nil {
		return nil, fmt.Errorf("resolve Amadeus root for session store: %w", runner.runtime.rootErr)
	}
	factory := runner.runtime.sessionStoreFactory
	if factory == nil {
		factory = defaultSessionStoreFactory
	}
	store, closer, err := factory(ctx, runner.runtime.amadeusRoot)
	if err != nil {
		return nil, err
	}
	idFactory := runner.runtime.persistentIDFactory
	if idFactory == nil {
		idFactory = nextPersistentID
	}
	baseIDFactory := idFactory
	idFactory = func(kind string) string {
		if kind == "run" && runner.runtime.runIDFactory != nil {
			return runner.runtime.runIDFactory()
		}
		return baseIDFactory(kind)
	}
	clock := runner.runtime.now
	if clock == nil {
		clock = time.Now
	}
	coordinator, err := sessiondomain.NewCoordinator(store, invocation.Project.Path(), filepath.Base(invocation.Project.Path()), sessiondomain.CoordinatorOptions{IDFactory: idFactory, Clock: clock})
	if err != nil {
		if closer != nil {
			_ = closer.Close()
		}
		return nil, err
	}
	sessionRuntime, err := sessiondomain.NewSessionRuntimeWithOptions(coordinator, sessiondomain.SessionRuntimeOptions{
		ExtensionFactory: func() (io.Closer, error) {
			return extensionruntime.New(runner.runtime.amadeusRoot, invocation.Project, extensionruntime.Options{MCPClientFactory: runner.runtime.mcpClientFactory})
		},
	})
	if err != nil {
		if closer != nil {
			_ = closer.Close()
		}
		return nil, err
	}
	runner.sessionRuntime = sessionRuntime
	runner.sessionCloser = closer
	runner.sessionProject = invocation.Project.Path()
	return sessionRuntime, nil
}

func (runner *agentController) closeSessionStore() {
	if runner.sessionRuntime != nil {
		_ = runner.sessionRuntime.Close()
	}
	if runner.sessionCloser != nil {
		_ = runner.sessionCloser.Close()
	}
	runner.sessionCloser = nil
	runner.sessionRuntime = nil
	runner.sessionProject = ""
}

func (runner *agentController) writeInteractiveStatus(ctx context.Context, invocation agentInvocation) error {
	sessionRuntime, err := runner.ensureSessionRuntime(ctx, invocation)
	if err != nil {
		return err
	}
	current := sessionRuntime.CurrentSessionID()
	if current == "" {
		current = "draft"
	}
	_, err = fmt.Fprintf(invocation.ErrorOutput, "status: project=%s session=%s\n", invocation.Project.Path(), current)
	return err
}

func (runner *agentController) prepareSession(ctx context.Context, invocation agentInvocation, reader *bufio.Reader) error {
	switch invocation.SessionMode {
	case "", sessionStartDraft:
		return nil
	case sessionStartContinue:
		sessionRuntime, err := runner.ensureSessionRuntime(ctx, invocation)
		if err != nil {
			return err
		}
		conversation, err := sessionRuntime.Continue(ctx)
		if errors.Is(err, sessiondomain.ErrNotFound) {
			fmt.Fprintln(invocation.ErrorOutput, "session: no previous session; using a new draft")
			return nil
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(invocation.ErrorOutput, "session: continued %s (%s)\n", conversation.ID, conversation.Title)
		return nil
	case sessionStartResume:
		sessionRuntime, err := runner.ensureSessionRuntime(ctx, invocation)
		if err != nil {
			return err
		}
		conversation, err := sessionRuntime.Resume(ctx, invocation.SessionID)
		if err != nil {
			return err
		}
		fmt.Fprintf(invocation.ErrorOutput, "session: resumed %s (%s)\n", conversation.ID, conversation.Title)
		return nil
	case sessionStartSelect:
		if reader == nil {
			return errors.New("session selector requires interactive input")
		}
		return runner.selectSession(ctx, invocation, reader)
	default:
		return fmt.Errorf("unsupported session start mode %q", invocation.SessionMode)
	}
}

func (runner *agentController) selectSession(ctx context.Context, invocation agentInvocation, reader *bufio.Reader) error {
	sessionRuntime, err := runner.ensureSessionRuntime(ctx, invocation)
	if err != nil {
		return err
	}
	sessions, err := sessionRuntime.ListSessions(ctx)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Fprintln(invocation.ErrorOutput, "session: no previous sessions; current conversation unchanged")
		return nil
	}
	fmt.Fprintln(invocation.ErrorOutput, "Select a session (Esc or empty input cancels):")
	for index, conversation := range sessions {
		marker := " "
		if conversation.ID == sessionRuntime.CurrentSessionID() {
			marker = "*"
		}
		fmt.Fprintf(invocation.ErrorOutput, "%s %d) %s  %s  %s\n", marker, index+1, conversation.ID, conversation.Title, conversation.UpdatedAt.UTC().Format(time.RFC3339))
	}
	fmt.Fprint(invocation.ErrorOutput, "resume> ")
	line, readErr := reader.ReadString('\n')
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return fmt.Errorf("read session selection: %w", readErr)
	}
	value := strings.TrimSpace(line)
	if value == "" || value == "\x1b" {
		fmt.Fprintln(invocation.ErrorOutput, "session: selection cancelled")
		return nil
	}
	selected := sessiondomain.SessionID(value)
	if index, parseErr := strconv.Atoi(value); parseErr == nil {
		if index < 1 || index > len(sessions) {
			return fmt.Errorf("session selection %d is out of range", index)
		}
		selected = sessions[index-1].ID
	}
	conversation, err := sessionRuntime.Resume(ctx, selected)
	if err != nil {
		return err
	}
	fmt.Fprintf(invocation.ErrorOutput, "session: resumed %s (%s)\n", conversation.ID, conversation.Title)
	return nil
}
