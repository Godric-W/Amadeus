package session

import (
	"testing"

	"github.com/Godric-W/Amadeus/internal/config"
)

func TestPromptDiagnosticsProjectsCurrentOwners(t *testing.T) {
	messages := continuationModelMessages(t)
	provider := continuationProvider(0)
	provider.WireAPI = config.WireAPIResponses
	session := newContinuationTestSession(t, &continuationTestClient{messages: messages}, nil, continuationModelInfo(messages), provider, DefaultTurnBudget())
	diagnostics := session.PromptDiagnostics()
	if diagnostics.BaseProvenance != "model:test-model" || diagnostics.WorldStateKind != "absent" || diagnostics.ProviderWireAPI != string(config.WireAPIResponses) {
		t.Fatalf("Prompt diagnostics identity = %#v", diagnostics)
	}
	for name, revision := range map[string]string{
		"instructions": diagnostics.InstructionsRevision, "collaboration": diagnostics.CollaborationRevision,
		"multi-agent": diagnostics.MultiAgentRevision, "compaction": diagnostics.CompactionRevision,
		"summary-prefix": diagnostics.SummaryPrefixRevision,
	} {
		if revision == "" {
			t.Fatalf("Prompt diagnostics omitted %s revision: %#v", name, diagnostics)
		}
	}
}
