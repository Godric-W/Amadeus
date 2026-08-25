package tui

import (
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/agent/protocol"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/testutil"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestSessionConfiguredReplacesCompleteStatusLineConfiguration(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	effort := llm.ReasoningEffortHigh
	configuration := protocol.SessionConfiguration{
		Source: protocol.RootSessionSource(), CWD: "/workspace/next", Provider: "openai", Model: "gpt-next",
		ReasoningEffort: &effort, Mode: protocol.ModeKindPlan,
	}

	command := model.applyEvent(testProtocolEvent(testThreadID(1), "", protocol.SessionConfiguredEvent{SessionID: testutil.SessionID(1), ThreadID: testThreadID(1), Configuration: configuration}))
	if command == nil {
		t.Fatal("CWD change did not schedule branch refresh")
	}
	if got := model.session.Configuration; got.CWD != configuration.CWD || got.Provider != configuration.Provider || got.Model != configuration.Model || got.Mode != protocol.ModeKindPlan {
		t.Fatalf("session configuration = %#v, want %#v", got, configuration)
	}
	plain := xansi.Strip(model.footerView())
	for _, expected := range []string{"gpt-next high", "/workspace/next", "Plan mode"} {
		if !strings.Contains(plain, expected) {
			t.Fatalf("footer omitted %q after SessionConfigured: %q", expected, plain)
		}
	}
}

func TestStatusLineOmitsUnavailableItemsWithoutStatusFallback(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.session.Configuration = protocol.SessionConfiguration{}
	model.session.Title = "draft"
	model.session.ContextWindow = 0
	model.workspace = statusLineWorkspaceState{}
	model.status = "working"
	model.refreshStatusLine()

	if got := xansi.Strip(model.footerView()); got != "" {
		t.Fatalf("empty status line rendered fallback content: %q", got)
	}
}

func TestStatusLineBranchUpdateRejectsStaleGenerationAndCurrentDir(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.session.Generation = 7
	model.session.Configuration.CWD = "/workspace/current"
	model.workspace = statusLineWorkspaceState{Generation: 7, CurrentDir: "/workspace/current", Pending: true}
	model.refreshStatusLine()

	model.applyStatusLineBranchUpdate(statusLineBranchUpdatedMsg{Generation: 6, CurrentDir: "/workspace/current", Branch: "old-generation"})
	model.applyStatusLineBranchUpdate(statusLineBranchUpdatedMsg{Generation: 7, CurrentDir: "/workspace/old", Branch: "old-directory"})
	if model.workspace.Branch != "" || !model.workspace.Pending {
		t.Fatalf("stale branch result changed cache: %#v", model.workspace)
	}

	model.applyStatusLineBranchUpdate(statusLineBranchUpdatedMsg{Generation: 7, CurrentDir: "/workspace/current", Branch: "main"})
	if model.workspace.Branch != "main" || model.workspace.Pending {
		t.Fatalf("current branch result was not applied: %#v", model.workspace)
	}
	if plain := xansi.Strip(model.footerView()); !strings.Contains(plain, "main") {
		t.Fatalf("footer omitted current branch: %q", plain)
	}
}

func TestFixedStatusLineItemOrder(t *testing.T) {
	want := [...]statusLineItem{
		statusLineItemModelWithReasoning,
		statusLineItemCurrentDir,
		statusLineItemGitBranch,
		statusLineItemThreadTitle,
		statusLineItemContextUsed,
		statusLineItemContextWindowSize,
	}
	if fixedStatusLineItems != want {
		t.Fatalf("fixed status line order = %#v, want %#v", fixedStatusLineItems, want)
	}
}

func TestStatusLineMarksEstimatedContextUsage(t *testing.T) {
	_, model := newTestFullscreen(t, nil)
	model.session.ContextUsed = 64_000
	model.session.ContextWindow = 128_000
	model.session.ContextEstimated = true
	value, ok := model.statusLineValueForItem(statusLineItemContextUsed)
	if !ok || value != "Context ~50% used" {
		t.Fatalf("estimated context status = %q available=%v", value, ok)
	}
}
