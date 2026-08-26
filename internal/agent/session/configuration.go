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

func cloneConfiguration(configuration Configuration) Configuration {
	cloned := configuration
	cloned.Runtime = config.Clone(configuration.Runtime)
	cloned.WorkspaceRoots = append([]string(nil), configuration.WorkspaceRoots...)
	cloned.OutputSchema = append([]byte(nil), configuration.OutputSchema...)
	cloned.Source = configuration.Source.Clone()
	return cloned
}
