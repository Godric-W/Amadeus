//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package process

import (
	"errors"
	"io"
	"os/exec"
)

func startPTY(*exec.Cmd, io.Writer) (io.WriteCloser, error) {
	return nil, errors.New("PTY process execution is unsupported on this platform")
}
