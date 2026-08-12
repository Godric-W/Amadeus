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

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/state"
	"github.com/Godric-W/Amadeus/internal/thread"
	threadmanager "github.com/Godric-W/Amadeus/internal/thread/manager"
)

func (runner *agentController) ensureActiveThread(ctx context.Context, invocation agentInvocation) (*threadmanager.AmadeusThread, config.Config, error) {
	manager, configured, err := runner.ensureThreadManager(ctx, invocation)
	if err != nil {
		return nil, config.Config{}, err
	}
	runner.threadMutex.Lock()
	active := runner.currentThread
	runner.threadMutex.Unlock()
	if active != nil {
		return active, configured, nil
	}
	active, err = manager.StartThread(ctx, threadmanager.StartInput{Configuration: sessionConfiguration(configured, invocation)})
	if err != nil {
		return nil, config.Config{}, err
	}
	runner.threadMutex.Lock()
	runner.currentThread = active
	runner.threadMutex.Unlock()
	return active, configured, nil
}

func (runner *agentController) closeSessionStore() {
	runner.threadMutex.Lock()
	manager := runner.threadManager
	runner.threadManager = nil
	runner.currentThread = nil
	runner.taskFactories = make(map[thread.ID]*codingTaskFactory)
	runner.threadMutex.Unlock()
	if manager != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Close(ctx)
	}
}

func (runner *agentController) writeInteractiveStatus(ctx context.Context, invocation agentInvocation) error {
	active, configured, err := runner.ensureActiveThread(ctx, invocation)
	if err != nil {
		return err
	}
	provider := configured.Providers[configured.DefaultProvider]
	metadata, metadataErr := runner.currentThreadMetadata(ctx)
	title := "draft"
	sessionID := "draft"
	rolloutItems := 0
	if metadataErr == nil {
		sessionID = string(active.ID())
		title = metadata.Title
		rolloutItems = len(active.History())
	}
	runner.threadMutex.Lock()
	factory := runner.taskFactories[active.ID()]
	runner.threadMutex.Unlock()
	writableRoots := 0
	approvalCount := 0
	skillRevision := "unloaded"
	mcpRevision := "unloaded"
	if factory != nil {
		writableRoots = len(factory.sessionPermissions.Snapshot().WritableRoots)
		approvalCount = factory.sessionApprovals.Count()
		if extensions, extensionErr := factory.ensureExtensions(); extensionErr == nil {
			skillRevision = shortRevision(extensions.SkillRevision())
			mcpRevision = shortRevision(extensions.MCPRevision())
		}
	}
	_, err = fmt.Fprintf(invocation.ErrorOutput,
		"project: %s\nsession: %s\ntitle: %s\nprovider: %s\nmodel: %s\nrollout items: %d\nsession writable roots: %d\nsession approvals: %d\nskills revision: %s\nmcp revision: %s\n",
		invocation.Project.Path(), sessionID, title, configured.DefaultProvider, provider.Model, rolloutItems,
		writableRoots, approvalCount, skillRevision, mcpRevision,
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
		manager, configured, err := runner.ensureThreadManager(ctx, invocation)
		if err != nil {
			return err
		}
		threads, err := manager.ListThreads(ctx, state.ListQuery{CWD: invocation.Project.Path(), Limit: 1})
		if err != nil {
			return err
		}
		if len(threads) == 0 {
			fmt.Fprintln(invocation.ErrorOutput, "session: no previous session; using a new draft")
			return nil
		}
		active, err := runner.resumeThread(ctx, manager, configured, invocation, threads[0].ID)
		if err != nil {
			return err
		}
		fmt.Fprintf(invocation.ErrorOutput, "session: continued %s (%s)\n", active.ID(), threads[0].Title)
		return nil
	case sessionStartResume:
		manager, configured, err := runner.ensureThreadManager(ctx, invocation)
		if err != nil {
			return err
		}
		metadata, err := manager.ListThreads(ctx, state.ListQuery{CWD: invocation.Project.Path(), IncludeArchived: true})
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
		if _, err := runner.resumeThread(ctx, manager, configured, invocation, invocation.SessionID); err != nil {
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

func (runner *agentController) resumeThread(ctx context.Context, manager *threadmanager.ThreadManager, configured config.Config, invocation agentInvocation, id thread.ID) (*threadmanager.AmadeusThread, error) {
	runner.threadMutex.Lock()
	current := runner.currentThread
	runner.threadMutex.Unlock()
	if current != nil && current.ID() != id {
		if err := current.Shutdown(ctx); err != nil {
			return nil, err
		}
	}
	active, err := manager.ResumeThread(ctx, id, threadmanager.StartInput{Configuration: sessionConfiguration(configured, invocation)})
	if err != nil {
		return nil, err
	}
	runner.threadMutex.Lock()
	runner.currentThread = active
	runner.threadMutex.Unlock()
	return active, nil
}

func (runner *agentController) newDraft(ctx context.Context) error {
	runner.threadMutex.Lock()
	current := runner.currentThread
	runner.currentThread = nil
	runner.threadMutex.Unlock()
	if current != nil {
		return current.Shutdown(ctx)
	}
	return nil
}

func (runner *agentController) renameCurrent(ctx context.Context, title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", errors.New("session title is empty")
	}
	runner.threadMutex.Lock()
	manager := runner.threadManager
	current := runner.currentThread
	runner.threadMutex.Unlock()
	if manager == nil || current == nil {
		return "", errors.New("there is no active session to rename")
	}
	if err := manager.RenameThread(ctx, current.ID(), title); err != nil {
		return "", err
	}
	return title, nil
}

func (runner *agentController) deleteCurrent(ctx context.Context) (thread.ID, error) {
	runner.threadMutex.Lock()
	manager := runner.threadManager
	current := runner.currentThread
	runner.currentThread = nil
	runner.threadMutex.Unlock()
	if manager == nil || current == nil {
		return "", nil
	}
	if err := manager.DeleteThread(ctx, current.ID()); err != nil {
		return "", err
	}
	return current.ID(), nil
}

func (runner *agentController) listThreads(ctx context.Context, invocation agentInvocation) ([]state.StoredThread, error) {
	manager, _, err := runner.ensureThreadManager(ctx, invocation)
	if err != nil {
		return nil, err
	}
	return manager.ListThreads(ctx, state.ListQuery{CWD: invocation.Project.Path()})
}

func (runner *agentController) selectSession(ctx context.Context, invocation agentInvocation, reader *bufio.Reader) error {
	manager, configured, err := runner.ensureThreadManager(ctx, invocation)
	if err != nil {
		return err
	}
	threads, err := manager.ListThreads(ctx, state.ListQuery{CWD: invocation.Project.Path()})
	if err != nil {
		return err
	}
	if len(threads) == 0 {
		fmt.Fprintln(invocation.ErrorOutput, "session: no previous sessions; current conversation unchanged")
		return nil
	}
	fmt.Fprintln(invocation.ErrorOutput, "Select a session (Esc or empty input cancels):")
	runner.threadMutex.Lock()
	current := runner.currentThread
	runner.threadMutex.Unlock()
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
	active, err := runner.resumeThread(ctx, manager, configured, invocation, selected)
	if err != nil {
		return err
	}
	metadata, _ := runner.currentThreadMetadata(ctx)
	fmt.Fprintf(invocation.ErrorOutput, "session: resumed %s (%s)\n", active.ID(), metadata.Title)
	return nil
}
