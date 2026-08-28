package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/protocol"
)

func TestStreamControllerRoutesOnlyItsItemAndResetsAttempt(t *testing.T) {
	controller, err := newStreamController(protocol.ItemID("assistant-1"), "/workspace", HistoryRenderRich)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Push("assistant-2", "wrong\n", false); err == nil {
		t.Fatal("wrong item ID was accepted")
	}
	if changed, err := controller.Push("assistant-1", "first\n", false); err != nil || !changed {
		t.Fatalf("first delta changed=%v err=%v", changed, err)
	}
	if got := controller.Source.Source(); got != "first\n" {
		t.Fatalf("attempt source = %q", got)
	}
	if _, err := controller.Push("assistant-1", "second\n", true); err != nil {
		t.Fatal(err)
	}
	if got := controller.Source.Source(); got != "second\n" {
		t.Fatalf("reset source = %q", got)
	}
	source := controller.Finalize("canonical final")
	if source.Text != "canonical final" || source.CWD != "/workspace" {
		t.Fatalf("final source = %#v", source)
	}
	if controller.Source.Source() != "" {
		t.Fatal("finalize retained live source")
	}
}

func TestStreamingRenderRetainsStablePrefixWithoutDuplicatingIt(t *testing.T) {
	controller, err := newStreamController(protocol.ItemID("assistant-1"), "/workspace", HistoryRenderRich)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Push("assistant-1", "first paragraph\n\nsecond", false); err != nil {
		t.Fatal(err)
	}
	if controller.Render.StableSourceLen == 0 {
		t.Fatal("completed paragraph was not made stable")
	}
	if _, err := controller.Push("assistant-1", " paragraph\n", false); err != nil {
		t.Fatal(err)
	}
	lines := controller.Render.Lines
	if len(lines) != 3 {
		t.Fatalf("render lines = %#v", lines)
	}
	if got := markdownLineText(lines[0]); got != "first paragraph" {
		t.Fatalf("stable prefix = %q", got)
	}
	if got := markdownLineText(lines[1]); got != "" {
		t.Fatalf("paragraph separator = %q", got)
	}
	if got := markdownLineText(lines[2]); got != "second paragraph" {
		t.Fatalf("mutable block = %q", got)
	}
}

func TestStreamingRenderUsesGoldmarkTopLevelBoundaryInsideFence(t *testing.T) {
	renderer := newMarkdownRenderer()
	source := "intro\n\n```go\nfirst\n\nsecond\n"
	analysis := renderer.Analyze(source)
	want := len("intro\n\n")
	if analysis.LastTopLevelBlockStart != want {
		t.Fatalf("fenced block boundary = %d, want %d", analysis.LastTopLevelBlockStart, want)
	}
	render := StreamingRender{}
	render.Recompute(renderer, newMarkdownSource(source, ""), HistoryRenderRich)
	if render.StableSourceLen != want {
		t.Fatalf("stable source boundary = %d, want %d", render.StableSourceLen, want)
	}
}

func TestStreamingRenderUsesGoldmarkReferenceContext(t *testing.T) {
	renderer := newMarkdownRenderer()
	source := "earlier [reference][id].\n\n[id]:https://example.com/reference\n"
	analysis := renderer.Analyze(source)
	if !analysis.HasReferences {
		t.Fatal("Goldmark reference definition was not detected")
	}
	render := StreamingRender{}
	render.Recompute(renderer, newMarkdownSource(source, ""), HistoryRenderRich)
	if render.StableSourceLen != 0 {
		t.Fatalf("reference source advanced stable boundary to %d", render.StableSourceLen)
	}
}

func TestStreamControllerHoldsPartialLinkUntilCompletedLine(t *testing.T) {
	controller, err := newStreamController(protocol.ItemID("assistant-1"), "/workspace", HistoryRenderRich)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := controller.Push("assistant-1", "[docs](https://exam", false); err != nil || changed {
		t.Fatalf("partial link changed=%v err=%v", changed, err)
	}
	if len(controller.Render.Lines) != 0 || controller.TailCell() != nil {
		t.Fatalf("partial link became visible: %#v", controller.Render.Lines)
	}
	if changed, err := controller.Push("assistant-1", "ple.com)\n", false); err != nil || !changed {
		t.Fatalf("completed link changed=%v err=%v", changed, err)
	}
	if tail := controller.TailCell(); tail == nil || !strings.Contains(strings.Join(tail.RawLines(), "\n"), "https://example.com") {
		t.Fatalf("completed link tail = %#v", tail)
	}
}

func TestStreamControllerSetextHeadingRewritesOnlyMutableTail(t *testing.T) {
	controller, err := newStreamController(protocol.ItemID("assistant-1"), "/workspace", HistoryRenderRich)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Push("assistant-1", "Title\n", false); err != nil {
		t.Fatal(err)
	}
	if controller.State.EnqueuedStableLen != 0 || controller.TailCell() == nil {
		t.Fatalf("paragraph line escaped mutable tail: %#v", controller.State)
	}
	if _, err := controller.Push("assistant-1", "-----\n", false); err != nil {
		t.Fatal(err)
	}
	if len(controller.Render.Lines) != 1 || markdownLineText(controller.Render.Lines[0]) != "## Title" || len(controller.Render.Lines[0].Spans) == 0 || !controller.Render.Lines[0].Spans[0].Markdown.Bold {
		t.Fatalf("setext heading projection = %#v", controller.Render.Lines)
	}
}

func TestStreamControllerKeepsOpenFenceMutable(t *testing.T) {
	controller, err := newStreamController(protocol.ItemID("assistant-1"), "/workspace", HistoryRenderRich)
	if err != nil {
		t.Fatal(err)
	}
	for _, delta := range []string{"```go\n", "fmt.Println(\"hello\")\n", "\n"} {
		if _, err := controller.Push("assistant-1", delta, false); err != nil {
			t.Fatal(err)
		}
	}
	if controller.State.EnqueuedStableLen != 0 || controller.Render.StableSourceLen != 0 {
		t.Fatalf("open fence escaped mutable region: render=%#v state=%#v", controller.Render, controller.State)
	}
	if tail := controller.TailCell(); tail == nil || !strings.Contains(strings.Join(tail.RawLines(), "\n"), "fmt.Println") {
		t.Fatalf("open fence tail = %#v", tail)
	}
}

func TestAppModelRejectsSecondAssistantController(t *testing.T) {
	model := appModel{historyMode: HistoryRenderRich}
	first := protocol.TurnItem{ID: "assistant-1"}
	if err := model.startAssistantStream(first); err != nil {
		t.Fatal(err)
	}
	if err := model.startAssistantStream(protocol.TurnItem{ID: "assistant-2"}); err == nil {
		t.Fatal("second assistant stream replaced the active controller")
	}
}

func TestPlanStreamUsesDedicatedPresentationController(t *testing.T) {
	controller, err := newPlanStreamController(protocol.ItemID("plan-1"), "/workspace", HistoryRenderRich)
	if err != nil {
		t.Fatal(err)
	}
	if !controller.plan || controller.ItemID != "plan-1" {
		t.Fatalf("plan controller = %#v", controller)
	}
}

func TestStreamControllerHoldsBackPendingTable(t *testing.T) {
	controller, err := newStreamController(protocol.ItemID("assistant-1"), "/workspace", HistoryRenderRich)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Push("assistant-1", "intro\n\n| Step | Owner |\n", false); err != nil {
		t.Fatal(err)
	}
	if controller.State.EnqueuedStableLen != 1 {
		t.Fatalf("pending table stable boundary = %d, want intro only", controller.State.EnqueuedStableLen)
	}
	if _, err := controller.Push("assistant-1", "| --- | --- |\n| A | B |\n", false); err != nil {
		t.Fatal(err)
	}
	if controller.State.EnqueuedStableLen != 1 {
		t.Fatalf("confirmed table escaped holdback: %d", controller.State.EnqueuedStableLen)
	}
}

func TestStreamStateOwnsQueuedStableLines(t *testing.T) {
	controller, err := newStreamController(protocol.ItemID("assistant-1"), "/workspace", HistoryRenderRich)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Push("assistant-1", "stable paragraph\n\nmutable\n", false); err != nil {
		t.Fatal(err)
	}
	if len(controller.State.CommitQueue) == 0 {
		t.Fatal("stable lines were not enqueued")
	}
	cell := controller.DrainStable()
	if cell == nil || len(controller.State.CommitQueue) != 0 || controller.State.EmittedStableLen == 0 {
		t.Fatalf("drain state = %#v cell=%#v", controller.State, cell)
	}
}

func TestStreamingRenderAdvancesStableSourceAcrossManyParagraphs(t *testing.T) {
	controller, err := newStreamController(protocol.ItemID("assistant-1"), "/workspace", HistoryRenderRich)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 100; index++ {
		if _, err := controller.Push("assistant-1", fmt.Sprintf("paragraph %d\n\n", index), false); err != nil {
			t.Fatal(err)
		}
	}
	if controller.Render.StableSourceLen != len(controller.Source.CommittedSource()) {
		t.Fatalf("stable source = %d, committed = %d", controller.Render.StableSourceLen, len(controller.Source.CommittedSource()))
	}
}

func BenchmarkStreamingRenderStableParagraphs(b *testing.B) {
	for iteration := 0; iteration < b.N; iteration++ {
		controller, _ := newStreamController(protocol.ItemID("assistant-1"), "/workspace", HistoryRenderRich)
		for index := 0; index < 200; index++ {
			_, _ = controller.Push("assistant-1", fmt.Sprintf("paragraph %d with stable content\n\n", index), false)
		}
	}
}
