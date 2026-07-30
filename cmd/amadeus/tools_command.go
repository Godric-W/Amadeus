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
		Short: "List built-in MVP tools",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			writer := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
			if _, err := fmt.Fprintln(writer, "NAME\tSIDE_EFFECT\tPARALLEL_SAFE\tIDEMPOTENT\tRESOURCE_MODE"); err != nil {
				return err
			}
			for _, spec := range builtin.MVPSpecs() {
				if _, err := fmt.Fprintf(writer, "%s\t%s\t%t\t%t\t%s\n", spec.Name, spec.SideEffect, spec.ParallelSafe, spec.Idempotent, spec.ResourceStrategy.Mode); err != nil {
					return err
				}
			}
			return writer.Flush()
		},
	}
}
