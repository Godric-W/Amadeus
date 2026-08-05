package main

import (
	"fmt"
	"text/tabwriter"

	"github.com/Godric-W/Amadeus/internal/tool/builtin"
	"github.com/spf13/cobra"
)

func newToolsCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "tools",
		Short: "Inspect Amadeus tools",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newToolsListCommand())
	return command
}

func newToolsListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the target Amadeus tool surface",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			writer := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
			if _, err := fmt.Fprintln(writer, "NAME\tEXPOSURE\tCONDITION\tSTATUS\tSIDE_EFFECT"); err != nil {
				return err
			}
			for _, entry := range builtin.TargetCatalog() {
				condition := entry.Condition
				if condition == "" {
					condition = "-"
				}
				if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", entry.Name, entry.Exposure, condition, entry.Status, entry.SideEffect); err != nil {
					return err
				}
			}
			return writer.Flush()
		},
	}
}
