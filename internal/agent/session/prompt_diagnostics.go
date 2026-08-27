package session

import "strings"

type PromptDiagnostics struct {
	BaseProvenance        string
	WorldStateKind        string
	WorldStateRevision    string
	InstructionsRevision  string
	CollaborationRevision string
	MultiAgentRevision    string
	CompactionRevision    string
	SummaryPrefixRevision string
	ProviderWireAPI       string
}

func (session *Session) PromptDiagnostics() PromptDiagnostics {
	if session == nil || session.state.Context == nil {
		return PromptDiagnostics{}
	}
	base := session.BaseInstructions()
	provenance := string(base.Provenance.Type)
	if model := strings.TrimSpace(base.Provenance.Model); model != "" {
		provenance += ":" + model
	}
	worldState, _ := session.state.Context.WorldStateBaseline()
	result := PromptDiagnostics{
		BaseProvenance: provenance, WorldStateKind: string(session.state.Context.WorldStateBaselineKind()),
		WorldStateRevision: worldState.Revision(), ProviderWireAPI: string(session.services.provider.WireAPI),
	}
	if messages, err := session.services.ModelMessages(session.services.ModelInfo()); err == nil {
		result.InstructionsRevision = messages.InstructionsRevision
		result.CollaborationRevision = messages.CollaborationRevision
		result.MultiAgentRevision = messages.MultiAgentRevision
	}
	if session.services.compaction != nil {
		result.CompactionRevision = session.services.compaction.Assets.SummarizationRevision
		result.SummaryPrefixRevision = session.services.compaction.Assets.SummaryPrefixRevision
	}
	return result
}
