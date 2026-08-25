package cli

import (
	"context"
	"fmt"
	"io"
)

func Run(ctx context.Context, args []string, input io.Reader, output, errorOutput io.Writer) int {
	command := newRootCommand()
	command.SetArgs(args)
	command.SetIn(input)
	command.SetOut(output)
	command.SetErr(errorOutput)
	command.SetContext(ctx)
	if err := command.Execute(); err != nil {
		if !errorAlreadyReported(err) {
			_, _ = fmt.Fprintln(errorOutput, err)
		}
		return exitCode(err)
	}
	return exitCodeSuccess
}
