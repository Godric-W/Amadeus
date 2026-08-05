//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package process

import (
	"fmt"
	"io"
	"os/exec"

	"github.com/creack/pty"
)

func startPTY(command *exec.Cmd, output io.Writer) (io.WriteCloser, error) {
	terminal, err := pty.Start(command)
	if err != nil {
		return nil, fmt.Errorf("start PTY process: %w", err)
	}
	go func() { _, _ = io.Copy(output, terminal) }()
	return terminal, nil
}
