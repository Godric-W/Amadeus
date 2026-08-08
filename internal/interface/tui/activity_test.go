package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestRenderIterationActivitiesGroupsReadsAndKeepsActions(t *testing.T) {
	iteration := newIterationActivity(1)
	iteration.CallIDs = []string{"read", "search", "command"}
	iteration.Tools["read"] = &toolActivity{CallID: "read", Kind: activityExplore, Title: "Read planner.go", Completed: true, Success: true}
	iteration.Tools["search"] = &toolActivity{CallID: "search", Kind: activityExplore, Title: "Search Replan in internal", Completed: true, Success: true}
	iteration.Tools["command"] = &toolActivity{CallID: "command", Kind: activityRun, Title: "Ran command", Detail: "go test ./...", Result: "ok", Duration: time.Second, Completed: true, Success: true}
	entries := renderIterationActivities(iteration)
	if len(entries) != 3 || entries[0].kind != "activity" || entries[1].kind != "activity" || entries[2].kind != "separator" {
		t.Fatalf("entries = %#v", entries)
	}
	if !entries[0].successful || !entries[1].successful {
		t.Fatalf("successful tools did not mark activity headings: %#v", entries)
	}
	for _, fragment := range []string{"• Explored", "Read planner.go", "Search Replan", "• Ran command", "go test ./...", "ok"} {
		content := entries[0].content + entries[1].content
		if !strings.Contains(content, fragment) {
			t.Fatalf("activity omitted %q: %s", fragment, content)
		}
	}
}

func TestCompletedActivityBatchKeepsSeparatorNewlineAndHighlightsReadSearch(t *testing.T) {
	app, err := NewFullscreenApplication(FullscreenOptions{
		Input: strings.NewReader(""), Output: &strings.Builder{},
		Task: func(context.Context, TaskSubmission) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newFullscreenModel(context.Background(), app)
	entries := []fullscreenEntry{
		{kind: "activity", content: "• Explored\n  └ Read planner.go\n    Search Replan", successful: true},
		{kind: "activity", content: "• Ran command\n  └ ok", successful: true},
		{kind: "separator"},
	}
	rendered := model.renderCommittedEntries(entries, "")
	if !strings.HasSuffix(rendered, "\n") || !strings.Contains(rendered, strings.Repeat("─", 12)) {
		t.Fatalf("completed activity batch omitted separator or trailing newline: %q", rendered)
	}
	if got := xansi.Strip(renderActivityContent(entries[0].content, 80, true)); got != entries[0].content {
		t.Fatalf("activity highlighting changed content: %q", got)
	}
	color, ok := fullscreenActivityVerbStyle.GetForeground().(lipgloss.AdaptiveColor)
	if !ok || color.Light != "#0E7490" || color.Dark != "#67E8F9" {
		t.Fatalf("activity verb color = %#v", fullscreenActivityVerbStyle.GetForeground())
	}
	successColor, ok := fullscreenActivityOKStyle.GetForeground().(lipgloss.AdaptiveColor)
	if !ok || successColor.Light != "#15803D" || successColor.Dark != "#86EFAC" {
		t.Fatalf("activity success color = %#v", fullscreenActivityOKStyle.GetForeground())
	}
}

func TestActivityFromStartedUsesSafeEventProjection(t *testing.T) {
	activity := activityFromStarted(event.ToolCallStarted{CallID: "call", Iteration: 2, SideEffect: "execute", ActionSummary: "Ran command", Detail: "go test ./..."}, 3)
	if activity.Kind != activityRun || activity.Title != "Ran command" || activity.Detail != "go test ./..." || activity.Sequence != 3 {
		t.Fatalf("activity = %#v", activity)
	}
}

func TestRenderIterationActivitiesPreservesOriginalCallOrder(t *testing.T) {
	iteration := newIterationActivity(1)
	iteration.CallIDs = []string{"first", "second", "third"}
	iteration.Tools["third"] = &toolActivity{CallID: "third", Kind: activityRun, Title: "Ran third", Completed: true, Success: true}
	iteration.Tools["first"] = &toolActivity{CallID: "first", Kind: activityRun, Title: "Ran first", Completed: true, Success: true}
	iteration.Tools["second"] = &toolActivity{CallID: "second", Kind: activityRun, Title: "Ran second", Completed: true, Success: true}
	entries := renderIterationActivities(iteration)
	content := entries[0].content + entries[1].content + entries[2].content
	first := strings.Index(content, "Ran first")
	second := strings.Index(content, "Ran second")
	third := strings.Index(content, "Ran third")
	if first < 0 || second <= first || third <= second {
		t.Fatalf("activity order changed: %q", content)
	}
}

func TestUnknownToolActivityUsesSafeSideEffectFallback(t *testing.T) {
	read := activityFromStarted(event.ToolCallStarted{CallID: "read", ToolName: "custom_read", SideEffect: "read", ActionSummary: "Explored custom_read"}, 1)
	network := activityFromStarted(event.ToolCallStarted{CallID: "network", ToolName: "custom_network", SideEffect: "network", ActionSummary: "Called network tool custom_network"}, 2)
	write := activityFromStarted(event.ToolCallStarted{CallID: "write", ToolName: "custom_write", SideEffect: "write", ActionSummary: "Ran tool custom_write"}, 3)
	if read.Kind != activityExplore || network.Kind != activityNetwork || write.Kind != activityRun {
		t.Fatalf("unexpected fallback kinds: read=%s network=%s write=%s", read.Kind, network.Kind, write.Kind)
	}
}

func TestWrapActivityContentKeepsTreeForLongCommand(t *testing.T) {
	wrapped := wrapActivityContent("• Ran "+strings.Repeat("long-command ", 8), 32)
	lines := strings.Split(wrapped, "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "• Ran ") || !strings.HasPrefix(lines[1], "  │ ") {
		t.Fatalf("long activity lost tree continuation: %q", wrapped)
	}
	for _, line := range lines {
		if len([]rune(line)) > 32 {
			t.Fatalf("wrapped line exceeded width: %q", line)
		}
	}
}

func TestRenderIterationActivitiesOmitsEmptySeparator(t *testing.T) {
	if entries := renderIterationActivities(newIterationActivity(1)); len(entries) != 0 {
		t.Fatalf("empty iteration rendered transcript entries: %#v", entries)
	}
}
