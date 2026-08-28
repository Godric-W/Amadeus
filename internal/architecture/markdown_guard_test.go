package architecture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkdownArchitectureUsesSourceBackedFinalCells(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range []string{
		"internal/tui/transcript_surface.go",
		"internal/tui/history_cell_session.go",
		"internal/tui/markdown_source.go",
		"internal/tui/markdown_stream.go",
		"internal/tui/markdown_stream_host.go",
		"internal/tui/markdown_parse.go",
		"internal/tui/markdown_render.go",
		"internal/tui/markdown_wrap.go",
		"internal/tui/streaming_render.go",
		"internal/tui/stream_controller.go",
		"internal/tui/markdown_render_cache.go",
		"internal/tui/markdown_tables.go",
		"internal/tui/markdown_links.go",
		"internal/tui/transcript_viewport.go",
	} {
		if _, err := os.Stat(filepath.Join(root, relative)); err != nil {
			t.Errorf("required Markdown boundary file is missing: %s: %v", relative, err)
		}
	}

	for _, required := range []string{"TranscriptSurface", "SessionHeaderCell", "MarkdownSource", "MarkdownWriter", "MarkdownStyle", "MarkdownStreamCollector", "StreamingRender", "StreamState", "StreamCore", "StreamController", "PlanStreamController", "AgentMessageCell", "StreamingAgentTailCell", "AgentMarkdownCell"} {
		found := false
		for _, relative := range []string{"internal/tui/transcript_surface.go", "internal/tui/history_cell_session.go", "internal/tui/markdown_source.go", "internal/tui/markdown_stream.go", "internal/tui/markdown_render.go", "internal/tui/streaming_render.go", "internal/tui/stream_controller.go", "internal/tui/history_messages.go", "internal/tui/history_cell_streaming.go"} {
			if strings.Contains(mustReadArchitectureFile(t, root, relative), "type "+required) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Markdown domain type %q is missing", required)
		}
	}
	viewSource := mustReadArchitectureFile(t, root, "internal/tui/application_view.go")
	if !strings.Contains(viewSource, "transcriptViewportHeight(composer, working)") || !strings.Contains(viewSource, "transcriptContent(viewportHeight)") {
		t.Error("Bubble Tea View does not allocate a bounded TranscriptSurface viewport")
	}
	transcriptViewSource := mustReadArchitectureFile(t, root, "internal/tui/transcript_view.go")
	if !strings.Contains(transcriptViewSource, "TranscriptSurface.render") || !strings.Contains(transcriptViewSource, "tea.Println(output)") {
		t.Error("transcript view does not own bounded active projection and native finalized history")
	}
	hostSource := mustReadArchitectureFile(t, root, "internal/tui/markdown_stream_host.go")
	for _, required := range []string{"*StreamController", "*PlanStreamController", "deferredTranscriptProjection", "flushDeferredTranscriptProjections"} {
		if !strings.Contains(hostSource, required) {
			t.Errorf("Markdown stream host boundary is missing %q", required)
		}
	}
	streamSource := mustReadArchitectureFile(t, root, "internal/tui/stream_controller.go")
	for _, required := range []string{"func (controller *StreamController) DrainStable", "func (controller *StreamController) TailCell", "type StreamCore"} {
		if !strings.Contains(streamSource, required) {
			t.Errorf("stream lifecycle boundary is missing %q", required)
		}
	}
	streamCellSource := mustReadArchitectureFile(t, root, "internal/tui/history_cell_streaming.go")
	if strings.Count(streamCellSource, "return !cell.First") != 2 {
		t.Error("stream cells do not derive continuation spacing from First")
	}
	surfaceSource := mustReadArchitectureFile(t, root, "internal/tui/transcript_surface.go")
	for _, required := range []string{"sessionHeader", "historyPrintCursor", "takePrintableCells", "transientStreamHistoryCell", "unprintedCells"} {
		if !strings.Contains(surfaceSource, required) {
			t.Errorf("TranscriptSurface native-history boundary is missing %q", required)
		}
	}

	err := filepath.WalkDir(filepath.Join(root, "internal", "tui"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(source), "tea.Println") && filepath.Base(path) != "transcript_view.go" {
			t.Errorf("native history printing escaped transcript owner in %s", filepath.ToSlash(path[len(root)+1:]))
		}
		for _, forbidden := range []string{
			"charmbracelet/glamour", "model.draft", "proposedPlanDraft", "LastAgentMarkdown",
			"recoverDeltaStart", "NewAgentMessageCell", "styleRendered", "prefixRenderedBlock",
			"pendingHistoryCells", "hasEmittedHistoryLines", "trailingStreamRun",
			"lastMarkdownBlockBoundary", "hasMarkdownReferenceDefinition",
		} {
			if strings.Contains(string(source), forbidden) {
				t.Errorf("legacy Markdown concern %q remains in %s", forbidden, filepath.ToSlash(path[len(root)+1:]))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
