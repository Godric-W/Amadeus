package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"text/tabwriter"
	"time"

	sessiondomain "github.com/Godric-W/Amadeus/internal/session"
	"github.com/spf13/cobra"
)

func newSessionsCommand(projectFlags *projectFlags, runtime commandRuntime) *cobra.Command {
	command := &cobra.Command{Use: "sessions", Short: "Manage conversation sessions"}
	command.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List sessions for the current project",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			root, err := projectFlags.resolve(command, runtime)
			if err != nil {
				return err
			}
			coordinator, closer, err := openSessionCoordinator(command.Context(), runtime, root.Path())
			if err != nil {
				return err
			}
			if closer != nil {
				defer closer.Close()
			}
			sessions, err := coordinator.ListSessions(command.Context())
			if err != nil {
				return err
			}
			return writeSessionList(command.OutOrStdout(), sessions)
		},
	})
	return command
}

func openSessionCoordinator(ctx context.Context, runtime commandRuntime, projectPath string) (*sessiondomain.Coordinator, io.Closer, error) {
	if runtime.rootErr != nil {
		return nil, nil, fmt.Errorf("resolve Amadeus root for session store: %w", runtime.rootErr)
	}
	factory := runtime.sessionStoreFactory
	if factory == nil {
		factory = defaultSessionStoreFactory
	}
	store, closer, err := factory(ctx, runtime.amadeusRoot)
	if err != nil {
		return nil, nil, err
	}
	idFactory := runtime.persistentIDFactory
	if idFactory == nil {
		idFactory = nextPersistentID
	}
	clock := runtime.now
	if clock == nil {
		clock = time.Now
	}
	coordinator, err := sessiondomain.NewCoordinator(store, projectPath, filepath.Base(projectPath), sessiondomain.CoordinatorOptions{IDFactory: idFactory, Clock: clock})
	if err != nil {
		if closer != nil {
			_ = closer.Close()
		}
		return nil, nil, err
	}
	return coordinator, closer, nil
}

func writeSessionList(output io.Writer, sessions []sessiondomain.Session) error {
	if output == nil {
		return errors.New("session list output is nil")
	}
	if len(sessions) == 0 {
		_, err := fmt.Fprintln(output, "No sessions for the current project.")
		return err
	}
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "ID\tTITLE\tSTATUS\tUPDATED"); err != nil {
		return err
	}
	for _, conversation := range sessions {
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", conversation.ID, conversation.Title, conversation.Status, conversation.UpdatedAt.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return writer.Flush()
}
