package protocol

import "github.com/Godric-W/Amadeus/internal/protocol/identity"

type SessionID = identity.SessionID
type ThreadID = identity.ThreadID
type TurnID = identity.TurnID
type SubmissionID = identity.SubmissionID
type EventID = identity.EventID
type RequestID = identity.RequestID
type ItemID = identity.ItemID

func NewThreadID() (ThreadID, error) { return identity.NewThreadID() }

func ParseThreadID(value string) (ThreadID, error) { return identity.ParseThreadID(value) }

func ParseSessionID(value string) (SessionID, error) { return identity.ParseSessionID(value) }

func SessionIDFromThreadID(id ThreadID) SessionID { return identity.SessionIDFromThreadID(id) }

func EventIDFromSubmission(id SubmissionID) EventID { return EventID(id) }
