package tui

import (
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
	"github.com/Godric-W/Amadeus/internal/testutil"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestWindowResizeRefreshesStatusLineProjection(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.footer = footerState{}
	updated, command := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if command != nil {
		t.Fatalf("unchanged initial size scheduled unexpected command: %v", command != nil)
	}
	resized := updated.(appModel)
	if len(resized.footer.StatusLine.Segments) == 0 || resized.footer.CollaborationIndicator != collaborationModeIndicator(0) {
		t.Fatalf("resize did not rebuild statusline projection: %#v", resized.footer)
	}
}

func TestFooterKeepsContextBeforePlanIndicator(t *testing.T) {
	_, model := newTestModel(t, nil)
	model.session.Configuration.Mode = protocol.ModeKindPlan
	model.session.ContextWindow = 128_000
	model.session.ContextUsed = 32_000
	model.refreshStatusLine()
	plain := xansi.Strip(model.footerView())
	contextIndex := strings.Index(plain, "Context 25% used")
	modeIndex := strings.Index(plain, "Plan mode")
	if contextIndex < 0 || modeIndex < 0 || contextIndex > modeIndex {
		t.Fatalf("footer context/mode order = %q", plain)
	}
	if !strings.HasSuffix(plain, "Plan mode  ") && !strings.HasSuffix(plain, "Plan mode (shift+tab to cycle)  ") {
		t.Fatalf("Plan indicator is not right aligned: %q", plain)
	}
}

func TestFooterTruncatesCompleteStatusLineFromRight(t *testing.T) {
	props := footerProps{
		Width: 52,
		State: footerState{StatusLine: statusLineState{Segments: []statusLineSegment{
			{Item: statusLineItemModelWithReasoning, Text: "GPT-TOP high"},
			{Item: statusLineItemCurrentDir, Text: "/AI/hgls/amadeus"},
			{Item: statusLineItemGitBranch, Text: "main"},
			{Item: statusLineItemThreadTitle, Text: "Completed goal"},
			{Item: statusLineItemContextUsed, Text: "Context 25% used"},
			{Item: statusLineItemContextWindowSize, Text: "128K window"},
		}}},
		Palette: terminalPalette{Level: colorLevelNone, NoColor: true}, LeftPadding: footerLeftPadding, RightPadding: footerRightPadding,
	}
	plain := xansi.Strip(renderFooter(props))
	if !strings.HasPrefix(plain, "  GPT-TOP high · /AI/hgls/amadeus") {
		t.Fatalf("statusline did not preserve its left prefix: %q", plain)
	}
	if !strings.HasSuffix(plain, "…") {
		t.Fatalf("statusline did not truncate its right edge with ellipsis: %q", plain)
	}
	if lipgloss.Width(plain) > props.Width {
		t.Fatalf("truncated statusline width = %d, want <= %d: %q", lipgloss.Width(plain), props.Width, plain)
	}
}

func TestSessionConfiguredReplacesCompleteStatusLineConfiguration(t *testing.T) {
	_, model := newTestModel(t, nil)
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
	_, model := newTestModel(t, nil)
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
	_, model := newTestModel(t, nil)
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
	_, model := newTestModel(t, nil)
	model.session.ContextUsed = 64_000
	model.session.ContextWindow = 128_000
	model.session.ContextEstimated = true
	value, ok := model.statusLineValueForItem(statusLineItemContextUsed)
	if !ok || value != "Context ~50% used" {
		t.Fatalf("estimated context status = %q available=%v", value, ok)
	}
}
