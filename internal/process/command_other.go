//go:build !unix

package process

import "os/exec"

func configureManagedCommand(*exec.Cmd, bool) {}
