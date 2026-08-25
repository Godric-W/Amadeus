package cli

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/Godric-W/Amadeus/internal/bootstrap"
	"github.com/Godric-W/Amadeus/internal/state"
	"github.com/spf13/cobra"
)

func newSessionsCommand(projectFlags *projectFlags, options RootOptions) *cobra.Command {
	command := &cobra.Command{Use: "sessions", Short: "Manage conversation sessions"}
	command.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List sessions for the current project",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			root, err := projectFlags.resolve(command, options.Environment)
			if err != nil {
				return err
			}
			if options.Environment.AmadeusRootError != nil {
				return fmt.Errorf("resolve Amadeus root for thread store: %w", options.Environment.AmadeusRootError)
			}
			threads, err := bootstrap.ListThreads(command.Context(), options.Environment.AmadeusRoot, options.Bootstrap, state.ListQuery{CWD: root.Path()})
			if err != nil {
				return err
			}
			return writeSessionList(command.OutOrStdout(), threads)
		},
	})
	return command
}

func writeSessionList(output io.Writer, threads []state.StoredThread) error {
	if output == nil {
		return errors.New("session list output is nil")
	}
	if len(threads) == 0 {
		_, err := fmt.Fprintln(output, "No sessions for the current project.")
		return err
	}
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "ID\tTITLE\tSTATUS\tUPDATED"); err != nil {
		return err
	}
	for _, metadata := range threads {
		status := "active"
		if metadata.Archived {
			status = "archived"
		}
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", metadata.ID, metadata.Title, status, metadata.UpdatedAt.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return writer.Flush()
}
