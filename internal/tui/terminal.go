package tui

import (
	"io"
	"os"
	"strconv"
	"strings"
)

type TerminalCapabilities struct {
	TTY       bool
	Color     bool
	Width     int
	Alternate bool
	Mouse     bool
}

type TerminalCapabilityOptions struct {
	IsTerminal func(io.Reader) bool
	LookupEnv  func(string) (string, bool)
}

func DetectTerminalCapabilities(input io.Reader, output io.Writer) TerminalCapabilities {
	return DetectTerminalCapabilitiesWithOptions(input, output, TerminalCapabilityOptions{})
}

func DetectTerminalCapabilitiesWithOptions(input io.Reader, output io.Writer, options TerminalCapabilityOptions) TerminalCapabilities {
	lookupEnv := options.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	capabilities := TerminalCapabilities{Color: true, Width: terminalWidth(lookupEnv)}
	if options.IsTerminal != nil {
		capabilities.TTY = options.IsTerminal(input)
	} else {
		capabilities.TTY = isTerminalPair(input, output)
	}
	term, _ := lookupEnv("TERM")
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		capabilities.Color = false
	}
	return capabilities
}

func isTerminalPair(input io.Reader, output io.Writer) bool {
	inputFile, inputOK := input.(*os.File)
	outputFile, outputOK := output.(*os.File)
	if !inputOK || !outputOK {
		return false
	}
	inputInfo, inputErr := inputFile.Stat()
	outputInfo, outputErr := outputFile.Stat()
	return inputErr == nil && outputErr == nil && inputInfo.Mode()&os.ModeCharDevice != 0 && outputInfo.Mode()&os.ModeCharDevice != 0
}

func terminalWidth(lookupEnv func(string) (string, bool)) int {
	const defaultWidth = 80
	columns, ok := lookupEnv("COLUMNS")
	if !ok {
		return defaultWidth
	}
	width, err := strconv.Atoi(strings.TrimSpace(columns))
	if err != nil || width < 20 || width > 1000 {
		return defaultWidth
	}
	return width
}
