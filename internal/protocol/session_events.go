package protocol

import (
	"errors"

	"github.com/Godric-W/Amadeus/internal/llm"
)

type SessionConfiguration struct {
	Source          SessionSource
	CWD             string
	Provider        string
	Model           string
	ReasoningEffort *llm.ReasoningEffort `json:"reasoning_effort,omitempty"`
	Mode            ModeKind
}

func (configuration SessionConfiguration) Clone() SessionConfiguration {
	configuration.Source = configuration.Source.Clone()
	configuration.ReasoningEffort = llm.CloneReasoningEffort(configuration.ReasoningEffort)
	return configuration
}

type SessionConfiguredEvent struct {
	SessionID      SessionID
	ThreadID       ThreadID
	ParentThreadID *ThreadID
	Configuration  SessionConfiguration
}

func (SessionConfiguredEvent) isEventMsg() {}

func (event SessionConfiguredEvent) Validate() error {
	if event.SessionID.IsZero() || event.ThreadID.IsZero() {
		return errors.New("session configured identity is incomplete")
	}
	if err := event.Configuration.Source.Validate(); err != nil {
		return err
	}
	if event.Configuration.Source.IsSubAgent() {
		if event.ParentThreadID == nil || event.ParentThreadID.IsZero() || *event.ParentThreadID != event.Configuration.Source.SubAgent.ParentThreadID {
			return errors.New("sub-agent configured parent identity is inconsistent")
		}
		if event.ThreadID == *event.ParentThreadID {
			return errors.New("sub-agent configured thread cannot be its own parent")
		}
		return nil
	}
	if event.ParentThreadID != nil {
		return errors.New("root configured event has parent thread ID")
	}
	if event.SessionID != SessionIDFromThreadID(event.ThreadID) {
		return errors.New("root configured session ID does not match thread ID")
	}
	return nil
}

type ThreadSettingsAppliedEvent struct {
	ThreadID      ThreadID
	Configuration SessionConfiguration
}

func (ThreadSettingsAppliedEvent) isEventMsg() {}

type ShutdownCompleteEvent struct{ ThreadID ThreadID }

func (ShutdownCompleteEvent) isEventMsg() {}
