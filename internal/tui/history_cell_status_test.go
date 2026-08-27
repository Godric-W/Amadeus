package tui

import (
	"strings"
	"testing"

	application "github.com/Godric-W/Amadeus/internal/app"
)

func TestStatusHistoryCellShowsPromptDiagnostics(t *testing.T) {
	cell := StatusHistoryCell{Snapshot: application.StatusSnapshot{
		BaseProvenance: "model:gpt", WorldStateKind: "known", WorldStateRevision: "world-rev",
		ProviderWireAPI: "responses", InstructionsRevision: "base-rev", CollaborationRevision: "mode-rev",
		MultiAgentRevision: "agent-rev", CompactionRevision: "compact-rev", SummaryPrefixRevision: "prefix-rev",
	}}
	text := strings.Join(cell.RawLines(), "\n")
	for _, required := range []string{"base model:gpt", "world known/world-rev", "wire responses", "base-rev", "mode-rev", "agent-rev", "compact-rev/prefix-rev"} {
		if !strings.Contains(text, required) {
			t.Fatalf("status omitted Prompt diagnostic %q: %s", required, text)
		}
	}
}
