package policy

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/filechange"
)

func TestCommandApprovalPresentationDescribesExactSessionScope(t *testing.T) {
	presentation := CommandApprovalPresentation("go test ./...", "Run the project test suite", "/workspace/amadeus")
	assertApprovalPresentationOptions(t, presentation)
	if presentation.Title != "Bash command" || presentation.Question != "Do you want to proceed?" {
		t.Fatalf("unexpected command presentation: %#v", presentation)
	}
	assertStringsEqual(t, presentation.Details, []string{"Command: go test ./...", "Description: Run the project test suite", "Working directory: /workspace/amadeus"})
	if !strings.Contains(presentation.Options[1].Label, "this exact command during this session") {
		t.Fatalf("session option does not describe exact-command scope: %#v", presentation.Options[1])
	}
	if !strings.Contains(presentation.Options[1].Description, "/workspace/amadeus") {
		t.Fatalf("session option does not describe cwd scope: %#v", presentation.Options[1])
	}
	assertPresentationExcludesInternalTerms(t, presentation)
}

func TestFileApprovalPresentationsDescribeOperationAndDirectoryScope(t *testing.T) {
	path := filepath.Join("/workspace", "pkg", "agent.go")
	change := &filechange.Preview{Stats: filechange.DiffStats{Insertions: 4, Deletions: 2}}
	tests := []struct {
		operation string
		title     string
		question  string
	}{
		{operation: "edit", title: "Edit file", question: "Do you want to make this edit to agent.go?"},
		{operation: "write-new", title: "Create file", question: "Do you want to create agent.go?"},
		{operation: "write-overwrite", title: "Overwrite file", question: "Do you want to overwrite agent.go?"},
	}
	for _, test := range tests {
		t.Run(test.operation, func(t *testing.T) {
			presentation := FileApprovalPresentation(test.operation, path, change)
			assertApprovalPresentationOptions(t, presentation)
			if presentation.Title != test.title || presentation.Question != test.question {
				t.Fatalf("unexpected file presentation: %#v", presentation)
			}
			assertStringsEqual(t, presentation.Details, []string{"File: " + path, "Changes: +4 -2"})
			if !strings.Contains(presentation.Options[1].Label, "pkg/") || !strings.Contains(presentation.Options[1].Description, filepath.Dir(path)) {
				t.Fatalf("session option does not describe directory scope: %#v", presentation.Options[1])
			}
		})
	}
}

func TestExternalApprovalPresentationsDescribeActualGrantScope(t *testing.T) {
	tests := []struct {
		name         string
		presentation ApprovalPresentation
		details      []string
		sessionText  string
	}{
		{
			name:         "web fetch",
			presentation: WebFetchApprovalPresentation("https://example.com/docs", "example.com"),
			details:      []string{"URL: https://example.com/docs", "Site: example.com"},
			sessionText:  "example.com",
		},
		{
			name:         "outside workspace read",
			presentation: ReadDirectoryApprovalPresentation("View image", "image", "/shared/images/diagram.png"),
			details:      []string{"File: /shared/images/diagram.png", "Outside the current workspace"},
			sessionText:  "/shared/images",
		},
		{
			name:         "MCP tool",
			presentation: MCPToolApprovalPresentation("github", "create_issue", "Create an issue"),
			details:      []string{"Tool: create_issue", "MCP server: github", "Description: Create an issue"},
			sessionText:  "github/create_issue",
		},
		{
			name:         "MCP resource",
			presentation: MCPResourceApprovalPresentation("docs", "docs://design"),
			details:      []string{"Resource: docs://design", "MCP server: docs"},
			sessionText:  "this resource",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertApprovalPresentationOptions(t, test.presentation)
			assertStringsEqual(t, test.presentation.Details, test.details)
			combinedSessionText := test.presentation.Options[1].Label + " " + test.presentation.Options[1].Description
			if !strings.Contains(combinedSessionText, test.sessionText) {
				t.Fatalf("session option omits grant scope %q: %#v", test.sessionText, test.presentation.Options[1])
			}
			assertPresentationExcludesInternalTerms(t, test.presentation)
		})
	}
}

func assertApprovalPresentationOptions(t *testing.T, presentation ApprovalPresentation) {
	t.Helper()
	if len(presentation.Options) != 3 {
		t.Fatalf("approval presentation must have three options: %#v", presentation.Options)
	}
	want := []struct {
		outcome ApprovalOutcome
		scope   ApprovalScope
	}{{ApprovalAllow, ApprovalOnce}, {ApprovalAllow, ApprovalSession}, {ApprovalDeny, ApprovalOnce}}
	for index, option := range presentation.Options {
		if option.Outcome != want[index].outcome || option.Scope != want[index].scope {
			t.Fatalf("unexpected option order at %d: %#v", index, option)
		}
		if strings.TrimSpace(option.Label) == "" || strings.TrimSpace(option.Description) == "" {
			t.Fatalf("approval option must have clear label and description: %#v", option)
		}
	}
}

func assertPresentationExcludesInternalTerms(t *testing.T, presentation ApprovalPresentation) {
	t.Helper()
	text := strings.ToLower(presentation.Title + " " + presentation.Question + " " + strings.Join(presentation.Details, " "))
	for _, option := range presentation.Options {
		text += " " + strings.ToLower(option.Label+" "+option.Description)
	}
	for _, forbidden := range []string{"unsandbox", "arguments_sha256", "reason:"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("presentation leaks internal term %q: %q", forbidden, text)
		}
	}
}

func assertStringsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected strings:\n got: %#v\nwant: %#v", got, want)
	}
}
