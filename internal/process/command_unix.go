//go:build unix

package process

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureManagedCommand(command *exec.Cmd, tty bool) {
	if !tty {
		if command.SysProcAttr == nil {
			command.SysProcAttr = &syscall.SysProcAttr{}
		}
		command.SysProcAttr.Setpgid = true
	}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		processGroup, err := syscall.Getpgid(command.Process.Pid)
		if err == nil {
			if killErr := syscall.Kill(-processGroup, syscall.SIGKILL); killErr == nil || errors.Is(killErr, os.ErrProcessDone) {
				return nil
			}
		}
		return command.Process.Kill()
	}
	command.WaitDelay = 2 * time.Second
}
