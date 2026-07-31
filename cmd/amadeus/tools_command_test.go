package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestToolsListShowsSevenCoreToolsAndMetadata(t *testing.T) {
	var output bytes.Buffer
	command := newRootCommand()
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"tools", "list"})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute tools list: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 8 || !strings.Contains(lines[0], "SIDE_EFFECT") {
		t.Fatalf("unexpected tools list shape: %q", output.String())
	}
	wantNames := []string{"apply_patch", "execute_command", "glob_files", "grep_code", "list_dir", "read_file", "write_file"}
	for index, name := range wantNames {
		if !strings.HasPrefix(strings.TrimSpace(lines[index+1]), name+" ") {
			t.Fatalf("unexpected tool at row %d: %q", index+1, lines[index+1])
		}
	}
	if !strings.Contains(output.String(), "apply_patch      write        false          false       exclusive") || !strings.Contains(output.String(), "execute_command  execute") || !strings.Contains(output.String(), "read_file        read") {
		t.Fatalf("tool side effect metadata missing: %q", output.String())
	}
}
