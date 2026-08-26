package tui

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteIdleScreenShowsProjectCommandsAndStatus(t *testing.T) {
	var output bytes.Buffer
	if err := WriteIdleScreen(&output, "/workspace/project", 54); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"Amadeus · Ready",
		"Project: /workspace/project",
		"/resume  /plan  /skills  /status  /mcp  /clear",
		"status: phase=idle task=- tools=0 usage=0/0",
	} {
		if !strings.Contains(output.String(), required) {
			t.Fatalf("idle screen omitted %q:\n%s", required, output.String())
		}
	}
}

func TestWriteIdleScreenBoundsLongProjectAndRejectsNilOutput(t *testing.T) {
	if err := WriteIdleScreen(nil, "/workspace/project", 80); err == nil {
		t.Fatal("expected nil output rejection")
	}
	var output bytes.Buffer
	if err := WriteIdleScreen(&output, strings.Repeat("project/", 20), 48); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		if len([]rune(line)) > 48 {
			t.Fatalf("idle screen line exceeds width: %q", line)
		}
	}
}
