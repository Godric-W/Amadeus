package tui

import (
	"bytes"
	"io"
	"testing"
)

func TestDetectTerminalCapabilities(t *testing.T) {
	capabilities := DetectTerminalCapabilitiesWithOptions(bytes.NewBufferString("input"), &bytes.Buffer{}, TerminalCapabilityOptions{
		IsTerminal: func(io.Reader) bool { return true },
		LookupEnv:  testTerminalEnv(map[string]string{"TERM": "xterm-256color", "COLUMNS": "120"}),
	})
	if !capabilities.TTY || !capabilities.Color || capabilities.Width != 120 {
		t.Fatalf("unexpected terminal capabilities: %#v", capabilities)
	}
	if !capabilities.Hyperlinks {
		t.Fatalf("xterm capability did not enable hyperlinks: %#v", capabilities)
	}
	if width := terminalWidth(testTerminalEnv(map[string]string{"COLUMNS": "not-a-number"})); width != 80 {
		t.Fatalf("invalid columns produced width %d", width)
	}
}

func TestDetectTerminalCapabilitiesDoesNotUseRemovedPlainEnvironment(t *testing.T) {
	capabilities := DetectTerminalCapabilitiesWithOptions(bytes.NewBufferString("input"), &bytes.Buffer{}, TerminalCapabilityOptions{
		IsTerminal: func(io.Reader) bool { return true },
		LookupEnv:  testTerminalEnv(map[string]string{"TERM": "xterm-256color", "AMADEUS_PLAIN": "1"}),
	})
	if !capabilities.TTY || !capabilities.Color {
		t.Fatalf("removed environment changed capabilities: %#v", capabilities)
	}
}

func TestTerminalHyperlinkCapabilityRejectsDumbAndLinux(t *testing.T) {
	for _, term := range []string{"dumb", "linux"} {
		if terminalSupportsHyperlinks(term, testTerminalEnv(map[string]string{})) {
			t.Fatalf("terminal %q unexpectedly enables OSC-8", term)
		}
	}
}

func testTerminalEnv(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}
