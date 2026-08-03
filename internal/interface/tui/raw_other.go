//go:build !linux

package tui

import (
	"errors"
	"os"
)

func makeTerminalRaw(*os.File) (func() error, error) {
	return nil, errors.New("terminal raw mode is not supported on this platform")
}
