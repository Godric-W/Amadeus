package process

import (
	"fmt"
	"io"
	"os/exec"
)

func startProcess(command *exec.Cmd, tty bool, output io.Writer) (io.WriteCloser, error) {
	if tty {
		return startPTY(command, output)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create process stdin: %w", err)
	}
	command.Stdout, command.Stderr = output, output
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start process: %w", err)
	}
	return stdin, nil
}
