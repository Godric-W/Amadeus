package session

import (
	"strings"
	"testing"
)

func TestProposedPlanStreamParserSplitsTagsAcrossDeltas(t *testing.T) {
	var parser ProposedPlanStreamParser
	for _, delta := range []string{"Before\n<pro", "posed_plan># Plan\n", "- Step</proposed_", "plan>\nAfter"} {
		if _, err := parser.Feed(delta); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := parser.Flush(); err != nil {
		t.Fatal(err)
	}
	if parser.AssistantText() != "Before\n\nAfter" || parser.PlanText() != "# Plan\n- Step" {
		t.Fatalf("assistant=%q plan=%q", parser.AssistantText(), parser.PlanText())
	}
}

func TestProposedPlanStreamParserRejectsMalformedBlocks(t *testing.T) {
	for _, value := range []string{"<proposed_plan>unfinished", "<proposed_plan>a<proposed_plan>b</proposed_plan>", "<proposed_plan>a</proposed_plan><proposed_plan>b</proposed_plan>"} {
		var parser ProposedPlanStreamParser
		_, feedErr := parser.Feed(value)
		_, flushErr := parser.Flush()
		if feedErr == nil && flushErr == nil {
			t.Fatalf("malformed block accepted: %s", strings.ReplaceAll(value, "\n", "\\n"))
		}
	}
}
