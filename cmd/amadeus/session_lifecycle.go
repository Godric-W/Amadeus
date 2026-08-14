package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/app"
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/state"
	"github.com/Godric-W/Amadeus/internal/thread"
	threadmanager "github.com/Godric-W/Amadeus/internal/thread/manager"
)

func (runner *agentController) ensureActiveThread(ctx context.Context, invocation agentInvocation) (*threadmanager.AmadeusThread, config.Config, error) {
	workspace, configured, err := runner.ensureWorkspace(ctx, invocation)
	if err != nil {
		return nil, config.Config{}, err
	}
	active, err := workspace.EnsureCurrent(ctx, sessionConfiguration(configured, invocation))
	if err != nil {
		return nil, config.Config{}, err
	}
	return active, configured, nil
}

func (runner *agentController) closeSessionStore() {
	runner.workspaceMu.Lock()
	workspace := runner.workspace
	runner.workspace = nil
	runner.workspaceMu.Unlock()
	if workspace != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = workspace.Close(ctx)
	}
}

func (runner *agentController) writeInteractiveStatus(ctx context.Context, invocation agentInvocation) error {
	active, configured, err := runner.ensureActiveThread(ctx, invocation)
	if err != nil {
		return err
	}
	provider := configured.Providers[configured.DefaultProvider]
	workspace := runner.currentWorkspace()
	metadata, metadataErr := workspace.CurrentMetadata(ctx)
	title := "draft"
	sessionID := "draft"
	rolloutItems := 0
	if metadataErr == nil {
		sessionID = string(active.ID())
		title = metadata.Title
		rolloutItems = len(active.History())
	}
	permissionCount := 0
	skillRevision := "unloaded"
	mcpRevision := "unloaded"
	if capabilities, ok := active.Capabilities(); ok {
		permissionCount = capabilities.PermissionGrantCount()
		skillRevision = shortRevision(capabilities.SkillRevision())
		mcpRevision = shortRevision(capabilities.MCPRevision())
	}
	_, err = fmt.Fprintf(invocation.ErrorOutput,
		"project: %s\nsession: %s\ntitle: %s\nprovider: %s\nmodel: %s\nrollout items: %d\nsession permissions: %d\nskills revision: %s\nmcp revision: %s\n",
		invocation.Project.Path(), sessionID, title, configured.DefaultProvider, provider.Model, rolloutItems,
		permissionCount, skillRevision, mcpRevision,
	)
	return err
}

func shortRevision(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func (runner *agentController) prepareSession(ctx context.Context, invocation agentInvocation, reader *bufio.Reader) error {
	switch invocation.SessionMode {
	case "", sessionStartDraft:
		return nil
	case sessionStartContinue:
		workspace, configured, err := runner.ensureWorkspace(ctx, invocation)
		if err != nil {
			return err
		}
		threads, err := workspace.List(ctx, state.ListQuery{CWD: invocation.Project.Path(), Limit: 1})
		if err != nil {
			return err
		}
		if len(threads) == 0 {
			fmt.Fprintln(invocation.ErrorOutput, "session: no previous session; using a new draft")
			return nil
		}
		active, err := workspace.Resume(ctx, threads[0].ID, sessionConfiguration(configured, invocation))
		if err != nil {
			return err
		}
		fmt.Fprintf(invocation.ErrorOutput, "session: continued %s (%s)\n", active.ID(), threads[0].Title)
		return nil
	case sessionStartResume:
		workspace, configured, err := runner.ensureWorkspace(ctx, invocation)
		if err != nil {
			return err
		}
		metadata, err := workspace.List(ctx, state.ListQuery{CWD: invocation.Project.Path(), IncludeArchived: true})
		if err != nil {
			return err
		}
		title := ""
		for _, candidate := range metadata {
			if candidate.ID == invocation.SessionID {
				title = candidate.Title
				break
			}
		}
		if _, err := workspace.Resume(ctx, invocation.SessionID, sessionConfiguration(configured, invocation)); err != nil {
			return err
		}
		fmt.Fprintf(invocation.ErrorOutput, "session: resumed %s (%s)\n", invocation.SessionID, title)
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

func (runner *agentController) newDraft(ctx context.Context) error {
	workspace := runner.currentWorkspace()
	if workspace == nil {
		return nil
	}
	return workspace.NewDraft(ctx)
}

func (runner *agentController) renameCurrent(ctx context.Context, title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("session title is empty")
	}
	workspace := runner.currentWorkspace()
	if workspace == nil {
		return "", errors.New("there is no active session to rename")
	}
	if err := workspace.RenameCurrent(ctx, title); err != nil {
		return "", err
	}
	return title, nil
}

func (runner *agentController) deleteCurrent(ctx context.Context) (thread.ID, error) {
	workspace := runner.currentWorkspace()
	if workspace == nil {
		return "", nil
	}
	deleted, err := workspace.DeleteCurrent(ctx)
	if errors.Is(err, app.ErrNoActiveThread) {
		return "", nil
	}
	return deleted, err
}

func (runner *agentController) listThreads(ctx context.Context, invocation agentInvocation) ([]state.StoredThread, error) {
	workspace, _, err := runner.ensureWorkspace(ctx, invocation)
	if err != nil {
		return nil, err
	}
	return workspace.List(ctx, state.ListQuery{CWD: invocation.Project.Path()})
}

func (runner *agentController) selectSession(ctx context.Context, invocation agentInvocation, reader *bufio.Reader) error {
	workspace, configured, err := runner.ensureWorkspace(ctx, invocation)
	if err != nil {
		return err
	}
	threads, err := workspace.List(ctx, state.ListQuery{CWD: invocation.Project.Path()})
	if err != nil {
		return err
	}
	if len(threads) == 0 {
		fmt.Fprintln(invocation.ErrorOutput, "session: no previous sessions; current conversation unchanged")
		return nil
	}
	fmt.Fprintln(invocation.ErrorOutput, "Select a session (Esc or empty input cancels):")
	current, _ := workspace.Current()
	for index, metadata := range threads {
		marker := " "
		if current != nil && metadata.ID == current.ID() {
			marker = "*"
		}
		fmt.Fprintf(invocation.ErrorOutput, "%s %d) %s  %s  %s\n", marker, index+1, metadata.ID, metadata.Title, metadata.UpdatedAt.UTC().Format(time.RFC3339))
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
	selected := thread.ID(value)
	if index, parseErr := strconv.Atoi(value); parseErr == nil {
		if index < 1 || index > len(threads) {
			return fmt.Errorf("session selection %d is out of range", index)
		}
		selected = threads[index-1].ID
	}
	active, err := workspace.Resume(ctx, selected, sessionConfiguration(configured, invocation))
	if err != nil {
		return err
	}
	metadata, _ := workspace.CurrentMetadata(ctx)
	fmt.Fprintf(invocation.ErrorOutput, "session: resumed %s (%s)\n", active.ID(), metadata.Title)
	return nil
}
