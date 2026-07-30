//go:build !(aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package builtin

import "os/exec"

func configureCommandProcess(command *exec.Cmd) {}
