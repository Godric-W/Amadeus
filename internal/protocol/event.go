package protocol

import (
	"errors"
	"strings"
)

type Event struct {
	ID  EventID
	Msg EventMsg
}

func (event Event) Validate() error {
	if strings.TrimSpace(string(event.ID)) == "" {
		return errors.New("event correlation ID is empty")
	}
	if event.Msg == nil {
		return errors.New("event message is nil")
	}
	if configured, ok := event.Msg.(SessionConfiguredEvent); ok {
		return configured.Validate()
	}
	if tokenCount, ok := event.Msg.(TokenCountEvent); ok {
		return tokenCount.Validate()
	}
	return nil
}

type EventMsg interface{ isEventMsg() }
