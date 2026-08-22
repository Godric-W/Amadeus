package protocol

import "github.com/Godric-W/Amadeus/internal/agent/protocol/identity"

type SessionID = identity.SessionID

func NewThreadID() (ThreadID, error) { return identity.NewThreadID() }

func ParseThreadID(value string) (ThreadID, error) { return identity.ParseThreadID(value) }

func ParseSessionID(value string) (SessionID, error) { return identity.ParseSessionID(value) }

func SessionIDFromThreadID(id ThreadID) SessionID { return identity.SessionIDFromThreadID(id) }
