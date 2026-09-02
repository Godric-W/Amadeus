package session

import (
	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
)

func (session *Session) Configuration() Configuration {
	if session == nil {
		return Configuration{}
	}
	session.configMu.RLock()
	defer session.configMu.RUnlock()
	return cloneConfiguration(session.state.Configuration)
}

func (session *Session) ProtocolConfiguration() protocol.SessionConfiguration {
	configuration := session.Configuration()
	return protocol.SessionConfiguration{
		Source:          configuration.Source.Clone(),
		CWD:             configuration.CWD,
		Provider:        configuration.Runtime.ModelProvider,
		Model:           configuration.Runtime.Model,
		ReasoningEffort: llm.CloneReasoningEffort(configuration.Runtime.ModelReasoningEffort),
		Mode:            protocol.ModeKind(configuration.Mode),
	}
}

func (session *Session) setMode(mode ModeKind) {
	session.configMu.Lock()
	session.state.Configuration.Mode = mode
	session.configMu.Unlock()
}

func (session *Session) applyMode(submissionID protocol.SubmissionID, mode ModeKind) {
	previous := session.Configuration()
	session.setMode(mode)
	current := session.Configuration()
	for _, contributor := range session.services.extensionRegistry().ConfigContributors() {
		if err := contributor.OnConfigChanged(session.ctx, session.services.sessionExtensions, session.services.threadExtensions, previous.Runtime, current.Runtime); err != nil {
			session.publish(protocol.Event{ID: protocol.EventIDFromSubmission(submissionID), Msg: protocol.WarningEvent{ThreadID: session.threadID, Message: "config extension failed: " + err.Error()}})
		}
	}
	session.publish(protocol.Event{ID: protocol.EventIDFromSubmission(submissionID), Msg: protocol.ThreadSettingsAppliedEvent{
		ThreadID: session.threadID, Configuration: session.ProtocolConfiguration(),
	}})
}

func cloneConfiguration(configuration Configuration) Configuration {
	cloned := configuration
	cloned.Runtime = config.Clone(configuration.Runtime)
	cloned.WorkspaceRoots = append([]string(nil), configuration.WorkspaceRoots...)
	cloned.OutputSchema = append([]byte(nil), configuration.OutputSchema...)
	cloned.Source = configuration.Source.Clone()
	return cloned
}
