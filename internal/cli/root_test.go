package cli

import (
	"bytes"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	var output bytes.Buffer
	command := newRootCommand()
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"version"})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute version command: %v", err)
	}

	const expected = "amadeus dev\ncommit: unknown\nbuild time: unknown\n"
	if output.String() != expected {
		t.Fatalf("unexpected version output: got %q, want %q", output.String(), expected)
	}
}
