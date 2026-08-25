package cli

import (
	"fmt"

	"github.com/Godric-W/Amadeus/internal/buildinfo"
	"github.com/spf13/cobra"
)

func newVersionCommand(info buildinfo.Info) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the Amadeus version",
		Args:  cobra.NoArgs,
		Run: func(command *cobra.Command, _ []string) {
			fmt.Fprintf(
				command.OutOrStdout(),
				"amadeus %s\ncommit: %s\nbuild time: %s\n",
				info.Version,
				info.Commit,
				info.BuildTime,
			)
		},
	}
}
