//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package builtin

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureCommandProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		processGroup, err := syscall.Getpgid(command.Process.Pid)
		if err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
		if err := syscall.Kill(-processGroup, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
	command.WaitDelay = 2 * time.Second
}
