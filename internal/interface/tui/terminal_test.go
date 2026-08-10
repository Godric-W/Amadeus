package tui

import (
	"bytes"
	"io"
	"testing"
)

func TestDetectTerminalCapabilitiesDowngradesNonTTY(t *testing.T) {
	capabilities := DetectTerminalCapabilitiesWithOptions(bytes.NewBufferString("input"), &bytes.Buffer{}, TerminalCapabilityOptions{
		IsTerminal: func(io.Reader) bool { return false },
		LookupEnv:  testTerminalEnv(map[string]string{"TERM": "xterm-256color"}),
	})
	if capabilities.TTY || !capabilities.Plain || capabilities.Color || capabilities.Width != 80 {
		t.Fatalf("unexpected non-TTY capabilities: %#v", capabilities)
	}
}

func TestDetectTerminalCapabilitiesKeepsRichTUIColorDespiteInheritedNoColor(t *testing.T) {
	capabilities := DetectTerminalCapabilitiesWithOptions(bytes.NewBufferString("input"), &bytes.Buffer{}, TerminalCapabilityOptions{
		IsTerminal: func(io.Reader) bool { return true },
		LookupEnv:  testTerminalEnv(map[string]string{"TERM": "xterm-256color", "NO_COLOR": "1", "COLUMNS": "120"}),
	})
	if !capabilities.TTY || capabilities.Plain || !capabilities.Color || capabilities.Width != 120 {
		t.Fatalf("unexpected terminal capabilities: %#v", capabilities)
	}

	dumb := DetectTerminalCapabilitiesWithOptions(bytes.NewBufferString("input"), &bytes.Buffer{}, TerminalCapabilityOptions{
		IsTerminal: func(io.Reader) bool { return true },
		LookupEnv:  testTerminalEnv(map[string]string{"TERM": "dumb"}),
	})
	if !dumb.TTY || !dumb.Plain || dumb.Color {
		t.Fatalf("unexpected dumb-terminal capabilities: %#v", dumb)
	}
}

func TestTerminalWidthFallsBackForInvalidValues(t *testing.T) {
	for _, columns := range []string{"19", "1001", "not-a-number"} {
		if width := terminalWidth(testTerminalEnv(map[string]string{"COLUMNS": columns})); width != 80 {
			t.Fatalf("columns %q produced width %d", columns, width)
		}
	}
}

func TestDetectTerminalCapabilitiesKeepsTTYInlineWhenTermIsEmpty(t *testing.T) {
	capabilities := DetectTerminalCapabilitiesWithOptions(bytes.NewBufferString("input"), &bytes.Buffer{}, TerminalCapabilityOptions{
		IsTerminal: func(io.Reader) bool { return true },
		LookupEnv:  testTerminalEnv(nil),
	})
	if !capabilities.TTY || capabilities.Plain || capabilities.Color {
		t.Fatalf("unexpected empty TERM capabilities: %#v", capabilities)
	}
}

func TestDetectTerminalCapabilitiesHonorsExplicitPlainMode(t *testing.T) {
	capabilities := DetectTerminalCapabilitiesWithOptions(bytes.NewBufferString("input"), &bytes.Buffer{}, TerminalCapabilityOptions{
		IsTerminal: func(io.Reader) bool { return true },
		LookupEnv:  testTerminalEnv(map[string]string{"TERM": "xterm-256color"}),
		ForcePlain: true,
	})
	if !capabilities.TTY || !capabilities.Plain || capabilities.Color {
		t.Fatalf("unexpected forced plain capabilities: %#v", capabilities)
	}
}

func TestDetectTerminalCapabilitiesIgnoresRemovedPlainEnvironmentVariable(t *testing.T) {
	capabilities := DetectTerminalCapabilitiesWithOptions(bytes.NewBufferString("input"), &bytes.Buffer{}, TerminalCapabilityOptions{
		IsTerminal: func(io.Reader) bool { return true },
		LookupEnv:  testTerminalEnv(map[string]string{"TERM": "xterm-256color", "AMADEUS_PLAIN": "1"}),
	})
	if !capabilities.TTY || capabilities.Plain || !capabilities.Color {
		t.Fatalf("removed AMADEUS_PLAIN variable still changed capabilities: %#v", capabilities)
	}
}

func testTerminalEnv(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}
