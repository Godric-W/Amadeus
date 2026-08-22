package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// SessionID identifies the root-owned agent tree shared by root and child threads.
type SessionID struct {
	value uuid.UUID
}

func ParseSessionID(value string) (SessionID, error) {
	parsed, err := parseUUID("session ID", value)
	if err != nil {
		return SessionID{}, err
	}
	return SessionID{value: parsed}, nil
}

func SessionIDFromThreadID(id ThreadID) SessionID {
	return SessionID{value: id.value}
}

func (id SessionID) String() string {
	if id.IsZero() {
		return ""
	}
	return id.value.String()
}

func (id SessionID) IsZero() bool { return id.value == uuid.Nil }

func (id SessionID) MarshalText() ([]byte, error) { return []byte(id.String()), nil }

func (id *SessionID) UnmarshalText(text []byte) error {
	if id == nil {
		return errors.New("unmarshal session ID into nil receiver")
	}
	parsed, err := ParseSessionID(string(text))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

func (id SessionID) MarshalJSON() ([]byte, error) { return json.Marshal(id.String()) }

func (id *SessionID) UnmarshalJSON(data []byte) error {
	if id == nil {
		return errors.New("unmarshal session ID into nil receiver")
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*id = SessionID{}
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("decode session ID: %w", err)
	}
	return id.UnmarshalText([]byte(value))
}
