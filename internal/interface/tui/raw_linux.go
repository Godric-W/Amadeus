//go:build linux

package tui

import (
	"fmt"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

func makeTerminalRaw(file *os.File) (func() error, error) {
	if file == nil {
		return nil, fmt.Errorf("terminal input file is nil")
	}
	fileDescriptor := int(file.Fd())
	original, err := unix.IoctlGetTermios(fileDescriptor, unix.TCGETS)
	if err != nil {
		return nil, fmt.Errorf("read terminal state: %w", err)
	}
	raw := *original
	raw.Iflag &^= unix.BRKINT | unix.ICRNL | unix.INPCK | unix.ISTRIP | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Cflag |= unix.CS8
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.IEXTEN | unix.ISIG
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fileDescriptor, unix.TCSETS, &raw); err != nil {
		return nil, fmt.Errorf("enable terminal raw mode: %w", err)
	}
	var once sync.Once
	var restoreErr error
	return func() error {
		once.Do(func() {
			if err := unix.IoctlSetTermios(fileDescriptor, unix.TCSETS, original); err != nil {
				restoreErr = fmt.Errorf("restore terminal state: %w", err)
			}
		})
		return restoreErr
	}, nil
}
