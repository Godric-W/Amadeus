package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestToolsListShowsTargetExposureAndMigrationMetadata(t *testing.T) {
	var output bytes.Buffer
	command := newRootCommand()
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"tools", "list"})

	if err := command.Execute(); err != nil {
		t.Fatalf("execute tools list: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 17 || !strings.Contains(lines[0], "EXPOSURE") || !strings.Contains(lines[0], "CONDITION") {
		t.Fatalf("unexpected tools list shape: %q", output.String())
	}
	wantNames := []string{"read", "edit", "write", "glob", "grep", "execute_command", "update_plan", "write_stdin", "view_image", "read_skill", "web_search", "web_fetch", "mcp_list_tools", "mcp_call", "mcp_list_resources", "mcp_read_resource"}
	for index, name := range wantNames {
		if !strings.HasPrefix(strings.TrimSpace(lines[index+1]), name+" ") {
			t.Fatalf("unexpected tool at row %d: %q", index+1, lines[index+1])
		}
	}
	wantMetadata := map[string][]string{
		"write":      {"direct", "-", "available", "write"},
		"view_image": {"conditional", "provider.images", "available", "read"},
		"mcp_call":   {"deferred", "mcp.catalog", "available", "network"},
	}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if want, exists := wantMetadata[fields[0]]; exists {
			if strings.Join(fields[1:], ",") != strings.Join(want, ",") {
				t.Fatalf("unexpected metadata for %q: %q", fields[0], line)
			}
			delete(wantMetadata, fields[0])
		}
	}
	if len(wantMetadata) != 0 {
		t.Fatalf("missing tool metadata rows: %#v", wantMetadata)
	}
}
